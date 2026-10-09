package billing

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// Acertos que a central faz na cobrança do cliente: mudar o vencimento de
// uma fatura, parcelar o rastreador de um pedido feito à vista e mudar o dia
// de vencimento da assinatura.

var (
	// ErrDueDateTaken: a assinatura já tem fatura nesse vencimento.
	ErrDueDateTaken = ValidationError{"esta assinatura já tem uma fatura com esse vencimento"}
	// ErrAlreadyFinanced: o rastreador da assinatura já está parcelado.
	ErrAlreadyFinanced = ValidationError{"o rastreador desta assinatura já está parcelado"}
)

// withDay é a data do mesmo mês com outro dia (até 28: existe em todo mês).
func withDay(d Date, day int) Date { return NewDate(d.Year(), d.Month(), day) }

// ChangeInvoiceDueDate muda o vencimento da fatura em aberto (para hoje ou
// depois). O lembrete e a suspensão seguem o vencimento novo.
func (s *Service) ChangeInvoiceDueDate(ctx context.Context, id uuid.UUID, due Date) (*Invoice, error) {
	if due.IsZero() {
		return nil, ValidationError{"informe o vencimento"}
	}
	if due.Before(s.Today()) {
		return nil, ValidationError{"o vencimento não pode ser no passado"}
	}
	inv, err := s.repo.transitionInvoice(ctx, id, `due_date = $2`, due.Time)
	if errors.Is(err, database.ErrConflict) {
		return nil, ErrDueDateTaken
	}
	return s.decorated(inv, err)
}

// FinanceInvoice parcela o rastreador de uma fatura avulsa ainda não paga (o
// pedido feito à vista): equipmentCents é a parte da fatura que é o
// rastreador, em n vezes sem juros na assinatura (o veículo). O rastreador
// sai da fatura, que fica só com o resto (o frete) — ou é cancelada, se não
// sobrar nada —, e as parcelas entram nas mensalidades em aberto que ainda
// não venceram (a 1ª na primeira delas) e, depois, nas seguintes, uma por
// mês. A assinatura fica ativa até a última.
func (s *Service) FinanceInvoice(ctx context.Context, invoiceID, subscriptionID uuid.UUID, n, equipmentCents, maxInstallments int) (*Invoice, *Subscription, error) {
	if n < 2 || n > maxInstallments || n > MaxInstallments {
		return nil, nil, ValidationError{fmt.Sprintf("o rastreador pode ser parcelado de 2 a %d vezes", min(maxInstallments, MaxInstallments))}
	}
	today := s.Today()
	var inv *Invoice
	var sub *Subscription
	err := pgx.BeginFunc(ctx, s.repo.db, func(tx pgx.Tx) error {
		var err error
		if inv, err = scanInvoice(tx.QueryRow(ctx, `SELECT `+invoiceColumns+` FROM invoices WHERE id = $1 FOR UPDATE`, invoiceID)); err != nil {
			return err
		}
		switch {
		case inv.Status != InvoiceOpen:
			return ErrNotOpen
		case inv.SubscriptionID != nil:
			return ValidationError{"é uma mensalidade: só a fatura do rastreador (avulsa) pode ser parcelada"}
		case equipmentCents <= 0 || equipmentCents > inv.AmountCents:
			return ValidationError{"o valor do rastreador precisa estar entre R$ 0,01 e o valor da fatura"}
		}
		var parcel bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM equipment_installments WHERE invoice_id = $1)`, invoiceID).Scan(&parcel); err != nil {
			return err
		}
		if parcel {
			return ValidationError{"esta fatura já é uma parcela do rastreador"}
		}
		if sub, err = scanSubscription(tx.QueryRow(ctx, `SELECT `+subscriptionColumns+` FROM subscriptions WHERE id = $1 FOR UPDATE`, subscriptionID)); err != nil {
			return err
		}
		switch {
		case sub.CustomerID != inv.CustomerID:
			return database.ErrNotFound
		case sub.Status != SubscriptionActive:
			return ValidationError{"a assinatura está encerrada"}
		case sub.Installments > 0:
			return ErrAlreadyFinanced
		}

		amounts := SplitInstallments(equipmentCents, n)
		description := inv.Description + fmt.Sprintf(" · rastreador de %s em %dx sem juros, nas mensalidades", moneyText(equipmentCents), n)
		rest := inv.AmountCents - equipmentCents
		status := InvoiceOpen
		if rest == 0 {
			// Só tinha o rastreador: a fatura não cobra mais nada.
			status, rest = InvoiceCanceled, inv.AmountCents
		}
		if inv, err = scanInvoice(tx.QueryRow(ctx, `
			UPDATE invoices SET amount_cents = $2, description = $3, status = $4, updated_at = NOW() WHERE id = $1
			RETURNING `+invoiceColumns, invoiceID, rest, description, status)); err != nil {
			return err
		}
		if err := InsertInstallments(ctx, tx, sub.ID, amounts); err != nil {
			return err
		}

		// As mensalidades já geradas e ainda não vencidas levam as próximas.
		rows, err := tx.Query(ctx, `
			SELECT id, due_date FROM invoices
			WHERE subscription_id = $1 AND status = 'OPEN' AND due_date >= $2
			ORDER BY due_date FOR UPDATE`, sub.ID, today.Time)
		if err != nil {
			return err
		}
		type open struct {
			id  uuid.UUID
			due time.Time
		}
		var opens []open
		for rows.Next() {
			var o open
			if err := rows.Scan(&o.id, &o.due); err != nil {
				rows.Close()
				return err
			}
			opens = append(opens, o)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		remaining := n
		var last *Date
		for _, o := range opens {
			p, err := nextPending(ctx, tx, sub.ID)
			if err != nil {
				return err
			}
			if p == nil {
				break
			}
			if _, err := tx.Exec(ctx, `
				UPDATE invoices SET amount_cents = amount_cents + $2, description = description || $3, updated_at = NOW()
				WHERE id = $1`, o.id, p.cents, installmentLabel(p.number, n)); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE equipment_installments SET invoice_id = $2 WHERE id = $1`, p.id, o.id); err != nil {
				return err
			}
			due := Date{o.due}
			last, remaining = &due, remaining-1
		}
		// As que sobram vão nas mensalidades seguintes, uma por mês.
		if remaining > 0 {
			end := sub.NextDueDate
			for i := 1; i < remaining; i++ {
				end = nextMonthly(end, sub.DueDay)
			}
			last = &end
		}
		sub, err = scanSubscription(tx.QueryRow(ctx, `
			UPDATE subscriptions SET installments = $2, commitment_until = $3, updated_at = NOW() WHERE id = $1
			RETURNING `+subscriptionColumns, sub.ID, n, last.Time))
		return err
	})
	if err != nil {
		return nil, nil, database.MapError(err)
	}
	s.decorate(inv)
	return inv, sub, s.repo.fillInstallments(ctx, sub)
}

// ChangeDueDay muda o dia de vencimento da assinatura: as mensalidades em
// aberto que ainda não venceram vão para o dia novo no mesmo mês (se ele
// ainda não passou), e as próximas já saem nele. As vencidas ficam como
// estão. O fim da promoção e da permanência acompanham o dia novo.
func (s *Service) ChangeDueDay(ctx context.Context, id uuid.UUID, day int) (*Subscription, error) {
	if day < 1 || day > 28 {
		return nil, ValidationError{"o dia de vencimento vai de 1 a 28"}
	}
	today := s.Today()
	var sub *Subscription
	err := pgx.BeginFunc(ctx, s.repo.db, func(tx pgx.Tx) error {
		var err error
		if sub, err = scanSubscription(tx.QueryRow(ctx, `SELECT `+subscriptionColumns+` FROM subscriptions WHERE id = $1 FOR UPDATE`, id)); err != nil {
			return err
		}
		if sub.Status != SubscriptionActive {
			return ErrAlreadyCanceled
		}
		if sub.DueDay == day {
			return nil
		}
		rows, err := tx.Query(ctx, `
			SELECT id, due_date FROM invoices WHERE subscription_id = $1 AND status = 'OPEN' AND due_date >= $2
			ORDER BY due_date`, id, today.Time)
		if err != nil {
			return err
		}
		moves := map[uuid.UUID]Date{}
		for rows.Next() {
			var invID uuid.UUID
			var due time.Time
			if err := rows.Scan(&invID, &due); err != nil {
				rows.Close()
				return err
			}
			if moved := withDay(Date{due}, day); !moved.Before(today) {
				moves[invID] = moved
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for invID, due := range moves {
			if _, err := tx.Exec(ctx, `UPDATE invoices SET due_date = $2, updated_at = NOW() WHERE id = $1`, invID, due.Time); err != nil {
				if errors.Is(database.MapError(err), database.ErrConflict) {
					return ErrDueDateTaken
				}
				return err
			}
		}
		next := withDay(sub.NextDueDate, day)
		if next.Before(today) {
			next = nextMonthly(next, day)
		}
		shift := func(d *Date) *time.Time {
			if d == nil {
				return nil
			}
			moved := withDay(*d, day)
			return &moved.Time
		}
		sub, err = scanSubscription(tx.QueryRow(ctx, `
			UPDATE subscriptions SET due_day = $2, next_due_date = $3, promo_until = $4, commitment_until = $5, updated_at = NOW()
			WHERE id = $1 RETURNING `+subscriptionColumns,
			id, day, next.Time, shift(sub.PromoUntil), shift(sub.CommitmentUntil)))
		return err
	})
	if err != nil {
		return nil, database.MapError(err)
	}
	return sub, s.repo.fillInstallments(ctx, sub)
}

// moneyText: 15000 → "R$ 150,00".
func moneyText(cents int) string {
	return fmt.Sprintf("R$ %d,%02d", cents/100, cents%100)
}
