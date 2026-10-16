package billing

import (
	"context"
	"time"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// O MRR (receita recorrente mensal): a soma das mensalidades das assinaturas
// ativas, pelo preço que a fatura do dia teria (o da promoção enquanto vale,
// o do reajuste depois que começa). As grátis não somam nada nem contam. Os
// meses passados usam os preços gravados hoje: uma mensalidade editada à mão
// não volta no tempo. Ao lado, as parcelas do rastreador que vencem em cada
// mês (o equipamento parcelado na mensalidade) e o total das duas.

// MRRMonth é um mês do resumo.
type MRRMonth struct {
	// Month: AAAA-MM.
	Month string `json:"month"`
	// MRRCents e Subscriptions: no fim do mês (no atual, hoje).
	MRRCents      int64 `json:"mrrCents"`
	Subscriptions int   `json:"subscriptions"`
	// As assinaturas que começaram no mês, pela mensalidade do início.
	NewCents int64 `json:"newCents"`
	NewCount int   `json:"newCount"`
	// As encerradas no mês, pela mensalidade do encerramento.
	ChurnCents int64 `json:"churnCents"`
	ChurnCount int   `json:"churnCount"`
	// ChangeCents é a variação sobre o mês anterior; OtherCents, o que ela
	// tem além de novas e encerradas (fim de promoção, reajuste).
	ChangeCents int64 `json:"changeCents"`
	OtherCents  int64 `json:"otherCents"`
	// As parcelas do rastreador das faturas que vencem no mês (no atual,
	// também as das mensalidades que ainda vão ser geradas) e quanto delas já
	// foi pago. TotalCents: mensalidades + parcelas.
	InstallmentsCents     int64 `json:"installmentsCents"`
	InstallmentsPaidCents int64 `json:"installmentsPaidCents"`
	TotalCents            int64 `json:"totalCents"`
}

// MRR é o resumo: o de hoje e os meses, do atual para trás.
type MRR struct {
	CurrentCents  int64 `json:"currentCents"`
	Subscriptions int   `json:"subscriptions"`
	// As parcelas do rastreador do mês atual e o total (mensalidades + parcelas).
	InstallmentsCents int64      `json:"installmentsCents"`
	TotalCents        int64      `json:"totalCents"`
	Months            []MRRMonth `json:"months"`
}

// mrrSub é o que o cálculo precisa de cada assinatura.
type mrrSub struct {
	priceCents        int
	promoCents        *int
	promoUntil        *Date
	nextCents         *int
	nextFrom          *Date
	created, canceled time.Time
	isCanceled        bool
}

func (m mrrSub) priceOn(day Date) int {
	amount, _ := priceOn(m.priceCents, m.nextCents, m.nextFrom, m.promoCents, m.promoUntil, day)
	return amount
}

// activeAt: já tinha começado e ainda não tinha sido encerrada no instante.
func (m mrrSub) activeAt(at time.Time) bool {
	return m.created.Before(at) && (!m.isCanceled || !m.canceled.Before(at))
}

// computeMRR monta o resumo de `months` meses até o de now, no fuso loc.
func computeMRR(subs []mrrSub, now time.Time, loc *time.Location, months int) *MRR {
	local := now.In(loc)
	current := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, loc)
	// O fim de um mês: o primeiro instante do seguinte (no atual, agora) e o
	// dia que dá o preço (o último do mês, ou hoje).
	end := func(start time.Time) (time.Time, Date) {
		next := start.AddDate(0, 1, 0)
		if next.After(now) {
			return now, DateIn(now, loc)
		}
		return next, DateIn(next.Add(-time.Nanosecond), loc)
	}
	snapshot := func(start time.Time) (cents int64, count int) {
		at, day := end(start)
		for _, s := range subs {
			if !s.activeAt(at) {
				continue
			}
			if p := s.priceOn(day); p > 0 {
				cents += int64(p)
				count++
			}
		}
		return cents, count
	}

	out := &MRR{Months: make([]MRRMonth, 0, months)}
	for i := 0; i < months; i++ {
		start := current.AddDate(0, -i, 0)
		next := start.AddDate(0, 1, 0)
		m := MRRMonth{Month: start.Format("2006-01")}
		m.MRRCents, m.Subscriptions = snapshot(start)
		prev, _ := snapshot(start.AddDate(0, -1, 0))
		for _, s := range subs {
			if !s.created.Before(start) && s.created.Before(next) {
				if p := s.priceOn(DateIn(s.created, loc)); p > 0 {
					m.NewCents += int64(p)
					m.NewCount++
				}
			}
			if s.isCanceled && !s.canceled.Before(start) && s.canceled.Before(next) {
				if p := s.priceOn(DateIn(s.canceled, loc)); p > 0 {
					m.ChurnCents += int64(p)
					m.ChurnCount++
				}
			}
		}
		m.ChangeCents = m.MRRCents - prev
		m.OtherCents = m.ChangeCents - (m.NewCents - m.ChurnCents)
		out.Months = append(out.Months, m)
	}
	if len(out.Months) > 0 {
		out.CurrentCents, out.Subscriptions = out.Months[0].MRRCents, out.Months[0].Subscriptions
	}
	return out
}

// MRR é o resumo dos últimos `months` meses (o atual incluído).
func (s *Service) MRR(ctx context.Context, months int) (*MRR, error) {
	now := s.now()
	local := now.In(s.loc)
	// Um mês antes do primeiro: a variação dele precisa do anterior.
	from := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, s.loc).AddDate(0, -months, 0)
	subs, err := s.repo.mrrSubscriptions(ctx, from)
	if err != nil {
		return nil, err
	}
	out := computeMRR(subs, now, s.loc, months)
	if len(out.Months) == 0 {
		return out, nil
	}
	first, err := time.Parse("2006-01", out.Months[len(out.Months)-1].Month)
	if err != nil {
		return nil, err
	}
	billed, err := s.repo.mrrInstallments(ctx, first, s.Today())
	if err != nil {
		return nil, err
	}
	for i := range out.Months {
		m := &out.Months[i]
		if b, ok := billed[m.Month]; ok {
			m.InstallmentsCents, m.InstallmentsPaidCents = b.cents, b.paid
		}
		m.TotalCents = m.MRRCents + m.InstallmentsCents
	}
	out.InstallmentsCents, out.TotalCents = out.Months[0].InstallmentsCents, out.Months[0].TotalCents
	return out, nil
}

type monthInstallments struct{ cents, paid int64 }

// mrrInstallments soma as parcelas do rastreador por mês de vencimento, desde
// o mês de from: as das faturas (menos as canceladas) e, no mês de today, as
// que ainda vão entrar nas mensalidades dele (a próxima de cada assinatura
// ativa que vence no mês e ainda não tem fatura).
func (r *Repository) mrrInstallments(ctx context.Context, from time.Time, today Date) (map[string]monthInstallments, error) {
	rows, err := r.db.Query(ctx, `
		SELECT to_char(i.due_date, 'YYYY-MM'), sum(e.amount_cents)::bigint,
			COALESCE(sum(e.amount_cents) FILTER (WHERE i.status = 'PAID'), 0)::bigint
		FROM equipment_installments e
		JOIN invoices i ON i.id = e.invoice_id
		JOIN users u ON u.id = i.customer_id
		WHERE i.status <> 'CANCELED' AND i.due_date >= $1::date AND u.role = 'customer'
		GROUP BY 1
		UNION ALL
		SELECT to_char($2::date, 'YYYY-MM'), COALESCE(sum(p.amount_cents), 0)::bigint, 0::bigint
		FROM subscriptions s
		JOIN users u ON u.id = s.customer_id
		JOIN LATERAL (
			SELECT e.amount_cents FROM equipment_installments e
			WHERE e.subscription_id = s.id AND e.invoice_id IS NULL AND e.waived_at IS NULL
			ORDER BY e.number LIMIT 1) p ON TRUE
		WHERE s.status = 'ACTIVE' AND u.role = 'customer'
			AND date_trunc('month', s.next_due_date) = date_trunc('month', $2::date)`,
		from.Format(time.DateOnly), today.String())
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	out := map[string]monthInstallments{}
	for rows.Next() {
		var month string
		var cents, paid int64
		if err := rows.Scan(&month, &cents, &paid); err != nil {
			return nil, err
		}
		m := out[month]
		m.cents += cents
		m.paid += paid
		out[month] = m
	}
	return out, rows.Err()
}

// mrrSubscriptions: as assinaturas de clientes ativas ou encerradas desde from.
func (r *Repository) mrrSubscriptions(ctx context.Context, from time.Time) ([]mrrSub, error) {
	rows, err := r.db.Query(ctx, `
		SELECT s.price_cents, s.promo_price_cents, s.promo_until, s.next_price_cents, s.next_price_from, s.created_at,
			CASE WHEN s.status = 'CANCELED' THEN COALESCE(s.canceled_at, s.updated_at) END
		FROM subscriptions s JOIN users u ON u.id = s.customer_id
		WHERE u.role = 'customer'
			AND (s.status = 'ACTIVE' OR COALESCE(s.canceled_at, s.updated_at) >= $1)`, from)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	var out []mrrSub
	for rows.Next() {
		var m mrrSub
		var promoUntil, nextFrom, canceled *time.Time
		if err := rows.Scan(&m.priceCents, &m.promoCents, &promoUntil, &m.nextCents, &nextFrom, &m.created, &canceled); err != nil {
			return nil, err
		}
		if promoUntil != nil {
			m.promoUntil = &Date{*promoUntil}
		}
		if nextFrom != nil {
			m.nextFrom = &Date{*nextFrom}
		}
		if canceled != nil {
			m.canceled, m.isCanceled = *canceled, true
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
