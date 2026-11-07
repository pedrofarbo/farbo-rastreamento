package billing

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// O rastreador incluso: a Farbo não vende o rastreador. Fora do comodato, ele
// vai junto com a assinatura e fica com o cliente, e a parte dele está nas
// primeiras mensalidades — somada ao preço do plano (os afiliados e os
// Insanos) ou já dentro dele (o plano do site). Encerrada a assinatura antes
// da última dessas mensalidades, o que falta da parte do rastreador vence
// numa fatura só (ou é dispensado: arrependimento, pedido desfeito).

// TrackerBalancePrefix abre a descrição da fatura do que faltava da parte do
// rastreador incluso (a nota fiscal de serviço a reconhece por ele).
const TrackerBalancePrefix = "Rastreador incluso: saldo"

// IncludeTracker marca a assinatura com o rastreador incluso: a parte dele
// (cents) em cada uma das months primeiras mensalidades, somada ao preço
// (added) ou já dentro dele. Zero desliga. Chame depois de definir o 1º
// vencimento.
func (s *Subscription) IncludeTracker(cents, months int, added bool) {
	if cents <= 0 || months <= 0 {
		s.TrackerCents, s.TrackerMonths, s.TrackerUntil, s.TrackerAdded = 0, 0, nil, false
		return
	}
	until := trackerEnd(s.NextDueDate, s.DueDay, months)
	s.TrackerCents, s.TrackerMonths, s.TrackerUntil, s.TrackerAdded = cents, months, &until, added
}

// trackerEnd é o vencimento seguinte à última das n mensalidades a partir
// de first: as que vencem antes dele levam a parte do rastreador.
func trackerEnd(first Date, dueDay, n int) Date {
	end := first
	for i := 0; i < n; i++ {
		end = followingDue(end, dueDay)
	}
	return end
}

// trackerPending é quantas das mensalidades com a parte do rastreador ainda
// não saíram (as que saíram, pagas ou em aberto, contam; as canceladas, não).
func trackerPending(ctx context.Context, q database.Querier, sub *Subscription) (int, error) {
	if sub.TrackerCents <= 0 || sub.TrackerMonths <= 0 || sub.TrackerUntil == nil {
		return 0, nil
	}
	var issued int
	if err := q.QueryRow(ctx, `
		SELECT count(*) FROM invoices WHERE subscription_id = $1 AND status <> 'CANCELED' AND due_date < $2`,
		sub.ID, sub.TrackerUntil.Time).Scan(&issued); err != nil {
		return 0, err
	}
	return max(sub.TrackerMonths-issued, 0), nil
}

// TrackerBalance é o que falta da parte do rastreador incluso se a
// assinatura for encerrada agora (as mensalidades que ainda vão vencer
// caem): o valor e quantas mensalidades. Zero: nada a acertar.
func (r *Repository) TrackerBalance(ctx context.Context, sub *Subscription, today Date) (cents, months int, err error) {
	if sub.TrackerCents <= 0 || sub.TrackerMonths <= 0 || sub.TrackerUntil == nil {
		return 0, 0, nil
	}
	var issued int
	if err := r.db.QueryRow(ctx, `
		SELECT count(*) FROM invoices WHERE subscription_id = $1 AND status <> 'CANCELED' AND due_date < $2
			AND NOT (status = 'OPEN' AND due_date >= $3)`,
		sub.ID, sub.TrackerUntil.Time, today.Time).Scan(&issued); err != nil {
		return 0, 0, database.MapError(err)
	}
	months = max(sub.TrackerMonths-issued, 0)
	return months * sub.TrackerCents, months, nil
}

// settleTracker lança (ou dispensa) o que falta da parte do rastreador
// incluso, na transação do encerramento, depois de cancelar as mensalidades
// que ainda não venceram.
func settleTracker(ctx context.Context, tx pgx.Tx, sub *Subscription, choice string, today Date) (*Invoice, error) {
	pending, err := trackerPending(ctx, tx, sub)
	if err != nil || pending == 0 || choice == BalanceWaive {
		return nil, err
	}
	due, err := ChargeDate(ctx, tx, sub.CustomerID, today, sub.DueDay)
	if err != nil {
		return nil, err
	}
	description := fmt.Sprintf("%s de %d de %d mensalidades (assinatura encerrada antes da %dª)",
		TrackerBalancePrefix, pending, sub.TrackerMonths, sub.TrackerMonths)
	if sub.VehicleID != nil {
		var vehicle string
		if err := tx.QueryRow(ctx, `SELECT name || COALESCE(' · ' || plate, '') FROM vehicles WHERE id = $1`,
			*sub.VehicleID).Scan(&vehicle); err == nil {
			description += ": " + vehicle
		}
	}
	return InsertInvoice(ctx, tx, &Invoice{
		CustomerID: sub.CustomerID, Description: description, AmountCents: pending * sub.TrackerCents, DueDate: due,
	})
}

// shiftTracker acompanha a mudança do dia de vencimento: a parte do
// rastreador que ainda não saiu vai nas próximas mensalidades, no dia novo.
func shiftTracker(ctx context.Context, tx pgx.Tx, sub *Subscription, next Date, day int) (*time.Time, error) {
	if sub.TrackerUntil == nil {
		return nil, nil
	}
	pending, err := trackerPending(ctx, tx, sub)
	if err != nil {
		return nil, err
	}
	if pending == 0 {
		// Todas já saíram: o fim fica onde está (no dia novo, ele poderia
		// pegar uma mensalidade que saiu sem a parte).
		return &sub.TrackerUntil.Time, nil
	}
	end := trackerEnd(next, day, pending)
	return &end.Time, nil
}
