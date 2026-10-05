package finance

import (
	"context"
	"strconv"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/mail"
)

// Mailer manda o resumo dos vencimentos (mail.FinanceMailer).
type Mailer interface {
	PayablesDue(ctx context.Context, to []string, d mail.PayablesDue) error
}

const (
	// reminderHour: o resumo sai a partir das 8h (Brasília).
	reminderHour = 8
	// reminderAhead: o aviso antecipado, dias antes do vencimento.
	reminderAhead  = 3
	maxDigestBills = 30
)

// SendReminders manda, uma vez por dia, o resumo das contas a pagar que
// vencem hoje e daqui a 3 dias (com o total das vencidas) para os
// administradores ativos. Chamado de hora em hora; várias instâncias não
// mandam dois. Se o envio falhar, tenta de novo na próxima hora.
func (s *Service) SendReminders(ctx context.Context) {
	if s.mailer == nil || s.now().In(s.loc).Hour() < reminderHour {
		return
	}
	today := s.Today()
	// Reserva o dia antes de mandar: a outra instância encontra a linha.
	tag, err := s.db.Exec(ctx, `INSERT INTO finance_reminders (day) VALUES ($1) ON CONFLICT DO NOTHING`, today.Time)
	if err != nil {
		s.log.Error("falha ao reservar o aviso de vencimentos", "err", err)
		return
	}
	if tag.RowsAffected() == 0 {
		return
	}
	digest, bills, err := s.digest(ctx, today)
	if err == nil && bills > 0 {
		var to []string
		if to, err = s.adminEmails(ctx); err == nil && len(to) > 0 {
			err = s.mailer.PayablesDue(ctx, to, digest)
		}
	}
	if err != nil {
		s.log.Warn("aviso de vencimentos não enviado; nova tentativa na próxima hora", "err", err)
		_, _ = s.db.Exec(context.WithoutCancel(ctx), `DELETE FROM finance_reminders WHERE day = $1`, today.Time)
		return
	}
	_, _ = s.db.Exec(ctx, `UPDATE finance_reminders SET bills = $2 WHERE day = $1`, today.Time, bills)
	if bills > 0 {
		s.log.Info("aviso de vencimentos enviado", "contas", bills)
	}
}

// digest monta o resumo: as que vencem hoje, as que vencem em 3 dias e o
// total das vencidas.
func (s *Service) digest(ctx context.Context, today billing.Date) (mail.PayablesDue, int, error) {
	ahead := today.AddDays(reminderAhead)
	d := mail.PayablesDue{Today: []mail.DueBill{}, Soon: []mail.DueBill{}, SoonDate: ahead.Format("02/01")}
	entries, err := s.Entries(ctx, EntryFilter{Kind: KindPayable, Status: "open", From: &today, To: &ahead})
	if err != nil {
		return d, 0, err
	}
	bills := 0
	for _, e := range entries {
		if bills >= maxDigestBills {
			break
		}
		bill := mail.DueBill{Description: e.Description, Supplier: e.SupplierName, Amount: BRL(e.AmountCents),
			Installment: installmentLabel(e)}
		switch {
		case e.DueDate.Equal(today.Time):
			d.Today = append(d.Today, bill)
		case e.DueDate.Equal(ahead.Time):
			d.Soon = append(d.Soon, bill)
		default:
			continue
		}
		bills++
	}
	var overdueCents int64
	if err := s.db.QueryRow(ctx, `
		SELECT count(*), COALESCE(sum(amount_cents), 0)::bigint FROM finance_entries
		WHERE kind = 'PAYABLE' AND status = 'OPEN' AND due_date < $1`, today.Time).
		Scan(&d.OverdueCount, &overdueCents); err != nil {
		return d, 0, database.MapError(err)
	}
	d.OverdueAmount = BRL(overdueCents)
	return d, bills, nil
}

func installmentLabel(e *Entry) string {
	if e.Installment == nil || e.Installments == nil {
		return ""
	}
	return strconv.Itoa(*e.Installment) + "/" + strconv.Itoa(*e.Installments)
}

func (s *Service) adminEmails(ctx context.Context) ([]string, error) {
	rows, err := s.db.Query(ctx, `SELECT email FROM users WHERE role = 'admin' AND active ORDER BY created_at`)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var email string
		if err := rows.Scan(&email); err != nil {
			return nil, err
		}
		out = append(out, email)
	}
	return out, rows.Err()
}
