package payments

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/mail"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/payments/abacatepay"
)

// O débito automático no cartão: no vencimento, as faturas em aberto do dia
// de cada conta com o débito ligado saem numa cobrança só no cartão salvo
// (uma vez por vencimento: a fatura avulsa lançada depois para um dia já
// debitado segue a régua normal).
// Recusado, tenta de novo nos dois dias seguintes (uma vez por dia) e avisa
// por e-mail a cada recusa; depois disso, a fatura segue a régua normal (Pix
// ou boleto). Cada tentativa é reservada numa linha de card_debits antes de
// cobrar: duas instâncias não cobram a mesma.

const (
	// debitAttempts: no vencimento e nos dois dias seguintes.
	debitAttempts = 3
	// O débito roda a cada debitEvery, entre debitFrom e debitUntil (hora
	// local), não de madrugada.
	debitEvery = 15 * time.Minute
	debitFrom  = 8
	debitUntil = 20
	// debitStuck: a tentativa reservada que não chegou a gravar a cobrança
	// (a instância caiu no meio) é dada como falha depois disso.
	debitStuck = 10 * time.Minute
)

// Situações de uma tentativa de débito.
const (
	debitRunning  = "RUNNING"
	debitApproved = "APPROVED"
	debitPending  = "PENDING"
	debitDeclined = "DECLINED"
	debitUnknown  = "UNKNOWN"
	debitFailed   = "FAILED"
)

// CardNotifier avisa o cliente quando o cartão recusa o débito
// (mail.CardMailer).
type CardNotifier interface {
	Declined(ctx context.Context, d mail.CardDeclined) error
}

// DebitConfig liga o débito automático: os dados do pagador (os quatro do
// cartão), quem avisa a recusa e o fuso do horário comercial.
type DebitConfig struct {
	Payer    func(ctx context.Context, customerID uuid.UUID) (*Payer, error)
	Notifier CardNotifier
	Location *time.Location
}

// SetDebit configura o débito automático.
func (s *Service) SetDebit(cfg DebitConfig) { s.debit = cfg }

// RunDebits roda o débito automático no horário comercial.
func (s *Service) RunDebits(ctx context.Context) {
	loc := s.debit.Location
	if loc == nil {
		loc = time.FixedZone("BRT", -3*60*60)
	}
	ticker := time.NewTicker(debitEvery)
	defer ticker.Stop()
	for {
		if hour := time.Now().In(loc).Hour(); hour >= debitFrom && hour < debitUntil {
			if n, err := s.DebitRound(ctx); err != nil && ctx.Err() == nil {
				s.log.Error("falha no débito automático", "err", err)
			} else if n > 0 {
				s.log.Info("débitos automáticos feitos", "charges", n)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// debitGroup são as faturas em aberto de uma conta que vencem no mesmo dia.
type debitGroup struct {
	customerID uuid.UUID
	due        time.Time
	invoiceIDs []uuid.UUID
	cents      int64
}

// DebitRound acompanha as tentativas em andamento e cobra as faturas que
// venceram hoje ou nos dois dias anteriores e ainda não têm tentativa de
// hoje. Devolve quantas cobranças saíram.
func (s *Service) DebitRound(ctx context.Context) (int, error) {
	if !s.CardEnabled() || s.debit.Payer == nil {
		return 0, nil
	}
	if err := s.followDebits(ctx); err != nil {
		return 0, err
	}
	today := s.invoices.Today()
	rows, err := s.repo.db.Query(ctx, `
		SELECT i.customer_id, i.due_date, array_agg(i.id ORDER BY i.id), sum(i.amount_cents)
		FROM invoices i
		JOIN customer_cards cc ON cc.customer_id = i.customer_id AND cc.auto_debit
		JOIN users u ON u.id = i.customer_id AND u.active
		WHERE i.status = 'OPEN' AND i.amount_cents > 0 AND i.due_date BETWEEN $1 AND $2
			AND NOT EXISTS (SELECT 1 FROM card_debits d WHERE d.customer_id = i.customer_id AND d.due_date = i.due_date
				AND d.status IN ('RUNNING', 'APPROVED', 'PENDING', 'UNKNOWN'))
		GROUP BY i.customer_id, i.due_date
		ORDER BY i.due_date
		LIMIT 200`, today.AddDays(-(debitAttempts - 1)).Time, today.Time)
	if err != nil {
		return 0, database.MapError(err)
	}
	var groups []debitGroup
	for rows.Next() {
		var g debitGroup
		if err := rows.Scan(&g.customerID, &g.due, &g.invoiceIDs, &g.cents); err != nil {
			rows.Close()
			return 0, database.MapError(err)
		}
		groups = append(groups, g)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, database.MapError(err)
	}

	charged := 0
	for _, g := range groups {
		attempt := int(today.Sub(g.due).Hours() / 24)
		var id uuid.UUID
		err := s.repo.db.QueryRow(ctx, `
			INSERT INTO card_debits (customer_id, due_date, attempt, status, amount_cents) VALUES ($1, $2, $3, 'RUNNING', $4)
			ON CONFLICT (customer_id, due_date, attempt) DO NOTHING RETURNING id`,
			g.customerID, g.due, attempt, g.cents).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			continue // a tentativa de hoje já foi feita (ou outra instância pegou)
		}
		if err != nil {
			return charged, database.MapError(err)
		}
		if s.runDebit(ctx, id, g, attempt) {
			charged++
		}
	}
	return charged, nil
}

// runDebit cobra a tentativa reservada e grava o resultado. Devolve se a
// cobrança saiu (aprovada ou à espera do banco).
func (s *Service) runDebit(ctx context.Context, id uuid.UUID, g debitGroup, attempt int) bool {
	payer, err := s.debit.Payer(ctx, g.customerID)
	if err != nil {
		s.finishDebit(ctx, id, debitFailed, nil, "dados do pagador: "+err.Error())
		return false
	}
	charge, err := s.CardForInvoices(ctx, g.customerID, g.invoiceIDs,
		CardInput{UseSaved: true, Metadata: map[string]string{"debitId": id.String()}}, payer)
	var chargeID *uuid.UUID
	if charge != nil {
		chargeID = &charge.ID
	}
	var apiErr *abacatepay.APIError
	switch {
	case errors.As(err, &apiErr):
		s.finishDebit(ctx, id, debitDeclined, chargeID, apiErr.Message)
		s.declined(ctx, g.customerID, g.due, int(g.cents), attempt, apiErr.Message)
		return false
	case errors.Is(err, ErrCardUnknown):
		s.finishDebit(ctx, id, debitUnknown, chargeID, "")
		return false
	case err != nil:
		s.finishDebit(ctx, id, debitFailed, chargeID, err.Error())
		s.log.Warn("débito automático não cobrado", "customer", g.customerID, "due", g.due.Format(time.DateOnly), "err", err)
		return false
	}
	status := s.debitStatus(charge)
	s.finishDebit(ctx, id, status, chargeID, "")
	switch status {
	case debitApproved:
		s.cardWorked(ctx, g.customerID)
	case debitDeclined:
		s.declined(ctx, g.customerID, g.due, int(g.cents), attempt, "recusado pelo banco")
	}
	return status == debitApproved || status == debitPending
}

// followDebits leva as tentativas em andamento à situação da cobrança delas
// (à espera do banco ou sem resposta da AbacatePay) e encerra as que ficaram
// presas sem cobrança.
func (s *Service) followDebits(ctx context.Context) error {
	rows, err := s.repo.db.Query(ctx, `
		SELECT d.id, d.customer_id, d.due_date, d.attempt, d.amount_cents, d.status, d.charge_id, d.created_at
		FROM card_debits d WHERE d.status IN ('RUNNING', 'UNKNOWN', 'PENDING')`)
	if err != nil {
		return database.MapError(err)
	}
	type open struct {
		id, customer uuid.UUID
		due          time.Time
		attempt      int
		cents        int
		status       string
		charge       *uuid.UUID
		created      time.Time
	}
	var list []open
	for rows.Next() {
		var o open
		if err := rows.Scan(&o.id, &o.customer, &o.due, &o.attempt, &o.cents, &o.status, &o.charge, &o.created); err != nil {
			rows.Close()
			return database.MapError(err)
		}
		list = append(list, o)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return database.MapError(err)
	}
	for _, o := range list {
		if o.charge == nil {
			if time.Since(o.created) > debitStuck {
				s.finishDebit(ctx, o.id, debitFailed, nil, "interrompido antes de cobrar")
			}
			continue
		}
		charge, err := s.repo.Get(ctx, *o.charge)
		if err != nil {
			continue
		}
		status := s.debitStatus(charge)
		if status == o.status || (status == debitPending && charge.Status == StatusCreating) {
			continue
		}
		s.finishDebit(ctx, o.id, status, o.charge, "")
		switch status {
		case debitApproved:
			s.cardWorked(ctx, o.customer)
		case debitDeclined:
			s.declined(ctx, o.customer, o.due, o.cents, o.attempt, "recusado pelo banco")
		}
	}
	return nil
}

// debitStatus é a situação da tentativa pela cobrança dela.
func (s *Service) debitStatus(charge *Charge) string {
	switch {
	case paidStatus(charge.Method, charge.Status):
		return debitApproved
	case charge.Status == StatusCreating:
		return debitUnknown
	case charge.Status == abacatepay.StatusPending:
		return debitPending
	}
	return debitDeclined
}

func (s *Service) finishDebit(ctx context.Context, id uuid.UUID, status string, chargeID *uuid.UUID, reason string) {
	if _, err := s.repo.db.Exec(ctx, `
		UPDATE card_debits SET status = $2, charge_id = COALESCE($3, charge_id), error = $4, updated_at = NOW() WHERE id = $1`,
		id, status, chargeID, reason); err != nil {
		s.log.Error("falha ao gravar a tentativa de débito", "debit", id, "err", err)
	}
}

// cardWorked limpa a última recusa do cartão.
func (s *Service) cardWorked(ctx context.Context, customerID uuid.UUID) {
	if _, err := s.repo.db.Exec(ctx, `
		UPDATE customer_cards SET failed_at = NULL, last_error = '', updated_at = NOW() WHERE customer_id = $1`, customerID); err != nil {
		s.log.Error("falha ao limpar a recusa do cartão", "customer", customerID, "err", err)
	}
}

// declined marca a recusa no cartão e avisa o cliente (com nova tentativa
// amanhã, ou a última).
func (s *Service) declined(ctx context.Context, customerID uuid.UUID, due time.Time, cents, attempt int, reason string) {
	var brand, last4 string
	if err := s.repo.db.QueryRow(ctx, `
		UPDATE customer_cards SET failed_at = NOW(), last_error = $2, updated_at = NOW() WHERE customer_id = $1
		RETURNING brand, last4`, customerID, reason).Scan(&brand, &last4); err != nil {
		s.log.Error("falha ao marcar a recusa do cartão", "customer", customerID, "err", err)
	}
	s.log.Info("débito automático recusado", "customer", customerID, "due", due.Format(time.DateOnly), "attempt", attempt+1)
	if s.debit.Notifier == nil {
		return
	}
	payer, err := s.debit.Payer(ctx, customerID)
	if err != nil || payer == nil || payer.Email == "" {
		return
	}
	if err := s.debit.Notifier.Declined(ctx, mail.CardDeclined{
		To: payer.Email, Name: payer.Name, Brand: brand, Last4: last4, AmountCents: cents,
		DueDate: billing.Date{Time: due}.Format("02/01/2006"), Retry: attempt < debitAttempts-1,
	}); err != nil {
		s.log.Warn("aviso de cartão recusado não saiu", "customer", customerID, "err", err)
	}
}
