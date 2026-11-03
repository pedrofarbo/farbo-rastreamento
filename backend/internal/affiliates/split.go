package affiliates

import (
	"context"
	"errors"
	"math"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/payments"
)

// O split: a parte do afiliado sai no próprio pagamento do cliente indicado,
// pela Pagar.me (payments/split.go). Aqui se calcula a parte — a comissão das
// faturas da cobrança, pela regra do afiliado (fixa ou percentual), e as
// acumuladas que ainda cabem — e se registra o que o split pagou. Só o
// afiliado com o recebedor ativo na Pagar.me recebe pelo split; o resto
// acumula para o próximo.

// splitPaidStatuses são as cobranças cujo split saiu (a parte do afiliado
// foi para ele): paga, ou já aprovada no cartão, ou em disputa.
const splitPaidStatuses = `('PAID', 'APPROVED', 'UNDER_DISPUTE')`

// SetSplit liga o split: as comissões passam a ser pagas no pagamento do
// cliente (as acumuladas, no próximo), e o fechamento do mês para.
func (s *Service) SetSplit(on bool) { s.split = on }

// SplitEnabled diz se as comissões saem pelo split.
func (s *Service) SplitEnabled() bool { return s.split }

// PlanSplit monta a parte do afiliado do cliente numa cobrança (nil: sem
// split) e reserva para ela as comissões acumuladas que cabem até maxCents.
func (s *Service) PlanSplit(ctx context.Context, chargeID, customerID uuid.UUID, invoices []*billing.Invoice, maxCents int) (*payments.SplitPlan, error) {
	var plan payments.SplitPlan
	var kind string
	var fixedCents, bps int
	var since time.Time
	err := s.db.QueryRow(ctx, `
		SELECT a.id, a.recipient_id, a.commission_type, a.commission_cents, a.commission_bps,
			date_trunc('month', r.created_at AT TIME ZONE $2)::date
		FROM affiliate_referrals r JOIN affiliates a ON a.id = r.affiliate_id
		WHERE r.customer_id = $1 AND a.active AND a.recipient_id <> '' AND a.recipient_status = 'active'`,
		customerID, s.loc.String()).Scan(&plan.AffiliateID, &plan.RecipientID, &kind, &fixedCents, &bps, &since)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, database.MapError(err)
	}

	items, err := s.splitItems(ctx, plan.AffiliateID, invoices, kind, fixedCents, bps, since)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if plan.AmountCents+item.AmountCents > maxCents {
			continue // não coube: nasce depois, sem split, e acumula
		}
		plan.Items = append(plan.Items, item)
		plan.AmountCents += item.AmountCents
	}

	// As acumuladas, da mais antiga, enquanto couberem.
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id, amount_cents FROM affiliate_commissions
			WHERE affiliate_id = $1 AND payout_id IS NULL AND split_charge_id IS NULL
			ORDER BY month, created_at
			FOR UPDATE SKIP LOCKED`, plan.AffiliateID)
		if err != nil {
			return err
		}
		var pick []uuid.UUID
		for rows.Next() {
			var id uuid.UUID
			var cents int
			if err := rows.Scan(&id, &cents); err != nil {
				rows.Close()
				return err
			}
			if plan.AmountCents+cents <= maxCents {
				pick = append(pick, id)
				plan.AmountCents += cents
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil || len(pick) == 0 {
			return err
		}
		plan.Reserved = pick
		_, err = tx.Exec(ctx, `
			UPDATE affiliate_commissions SET split_charge_id = $2, split_reserved_at = NOW()
			WHERE id = ANY($1) AND split_charge_id IS NULL`, pick, chargeID)
		return err
	})
	if err != nil {
		return nil, database.MapError(err)
	}
	if plan.AmountCents <= 0 {
		return nil, nil
	}
	return &plan, nil
}

// splitItems são as comissões das faturas da cobrança (as de veículo, a
// partir do mês da indicação), pela regra do afiliado: uma por veículo e
// mês; no percentual, sobre a mensalidade sem a parcela do rastreador. As
// que já existem (o mês já rendeu) ficam de fora.
func (s *Service) splitItems(ctx context.Context, affiliateID uuid.UUID, invoices []*billing.Invoice, kind string,
	fixedCents, bps int, since time.Time) ([]payments.SplitItem, error) {
	type key struct {
		sub   uuid.UUID
		month time.Time
	}
	var ids []uuid.UUID
	for _, inv := range invoices {
		ids = append(ids, inv.ID)
	}
	installments := map[uuid.UUID]int{}
	rows, err := s.db.Query(ctx, `
		SELECT invoice_id, sum(amount_cents)::int FROM equipment_installments WHERE invoice_id = ANY($1) GROUP BY 1`, ids)
	if err != nil {
		return nil, database.MapError(err)
	}
	for rows.Next() {
		var id uuid.UUID
		var cents int
		if err := rows.Scan(&id, &cents); err != nil {
			rows.Close()
			return nil, err
		}
		installments[id] = cents
	}
	rows.Close()

	bases := map[key]int{}
	var keys []key
	var subs []uuid.UUID
	for _, inv := range invoices {
		if inv.SubscriptionID == nil || inv.AmountCents <= 0 || inv.DueDate.Before(billing.Date{Time: since}) {
			continue
		}
		k := key{*inv.SubscriptionID, time.Date(inv.DueDate.Year(), inv.DueDate.Month(), 1, 0, 0, 0, 0, time.UTC)}
		if _, ok := bases[k]; !ok {
			keys = append(keys, k)
			subs = append(subs, k.sub)
		}
		bases[k] += max(inv.AmountCents-installments[inv.ID], 0)
	}
	if len(keys) == 0 {
		return nil, nil
	}
	existing := map[key]bool{}
	rows, err = s.db.Query(ctx, `
		SELECT subscription_id, month FROM affiliate_commissions WHERE affiliate_id = $1 AND subscription_id = ANY($2)`,
		affiliateID, subs)
	if err != nil {
		return nil, database.MapError(err)
	}
	for rows.Next() {
		var k key
		if err := rows.Scan(&k.sub, &k.month); err != nil {
			rows.Close()
			return nil, err
		}
		existing[key{k.sub, time.Date(k.month.Year(), k.month.Month(), 1, 0, 0, 0, 0, time.UTC)}] = true
	}
	rows.Close()

	sort.SliceStable(keys, func(i, j int) bool { return keys[i].month.Before(keys[j].month) })
	var out []payments.SplitItem
	for _, k := range keys {
		if existing[k] {
			continue
		}
		amount := fixedCents
		if kind == CommissionPercent {
			amount = int(math.Round(float64(bases[k]) * float64(bps) / 10000))
		}
		if amount > 0 {
			out = append(out, payments.SplitItem{SubscriptionID: k.sub, Month: k.month, AmountCents: amount})
		}
	}
	return out, nil
}

// CommitSplit grava a parte do afiliado na cobrança já gravada.
func (s *Service) CommitSplit(ctx context.Context, chargeID uuid.UUID, plan *payments.SplitPlan) error {
	return database.MapError(pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO payment_charge_splits (charge_id, affiliate_id, recipient_id, amount_cents) VALUES ($1, $2, $3, $4)`,
			chargeID, plan.AffiliateID, plan.RecipientID, plan.AmountCents); err != nil {
			return err
		}
		for _, item := range plan.Items {
			if _, err := tx.Exec(ctx, `
				INSERT INTO payment_charge_split_items (charge_id, subscription_id, month, amount_cents) VALUES ($1, $2, $3, $4)`,
				chargeID, item.SubscriptionID, item.Month, item.AmountCents); err != nil {
				return err
			}
		}
		return nil
	}))
}

// ReleaseSplit solta as acumuladas que a cobrança (que não nasceu) reservou.
func (s *Service) ReleaseSplit(ctx context.Context, chargeID uuid.UUID) error {
	_, err := s.db.Exec(ctx, `
		UPDATE affiliate_commissions SET split_charge_id = NULL, split_reserved_at = NULL
		WHERE split_charge_id = $1 AND payout_id IS NULL`, chargeID)
	return database.MapError(err)
}

// SplitPaid registra as comissões das faturas da cobrança paga, já pagas pelo
// split (com o valor que foi para o afiliado). As acumuladas já apontam para
// ela. A conferência (Reconcile) refaz isto, se falhar aqui.
func (s *Service) SplitPaid(ctx context.Context, chargeID uuid.UUID) error {
	_, err := s.db.Exec(ctx, insertSplitCommissions+` WHERE i.charge_id = $1`+onSplitConflict, chargeID)
	return database.MapError(err)
}

// insertSplitCommissions cria as comissões dos itens de split; onSplitConflict
// liga a que já existia (aberta) ao split, com o valor dele.
const insertSplitCommissions = `
	INSERT INTO affiliate_commissions (affiliate_id, customer_id, subscription_id, month, amount_cents, split_charge_id)
	SELECT sp.affiliate_id, sub.customer_id, i.subscription_id, i.month, i.amount_cents, sp.charge_id
	FROM payment_charge_split_items i
	JOIN payment_charge_splits sp ON sp.charge_id = i.charge_id
	JOIN subscriptions sub ON sub.id = i.subscription_id`

const onSplitConflict = `
	ON CONFLICT (affiliate_id, subscription_id, month) DO UPDATE
		SET split_charge_id = EXCLUDED.split_charge_id, amount_cents = EXCLUDED.amount_cents
		WHERE affiliate_commissions.payout_id IS NULL AND affiliate_commissions.split_charge_id IS NULL`
