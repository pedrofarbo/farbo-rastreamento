package billing

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// O MRR (receita recorrente mensal), pelo que é cobrado: em cada mês, as
// mensalidades das faturas que vencem nele — no atual, também as que o
// gerador ainda vai gerar até o fim do mês, pela mesma regra de datas e
// preços. A assinatura conta no mês em que a mensalidade dela vence, não
// antes (a contratada em outubro com a 1ª mensalidade em 20/11 entra em
// novembro). As grátis (mensalidade zero) não somam nem contam, e as faturas
// avulsas (o equipamento à vista, o frete) ficam de fora. Ao lado, as
// parcelas do rastreador que vencem no mês e o total das duas; a previsão do
// mês que vem segue a mesma conta.

// MRRMonth é um mês do resumo.
type MRRMonth struct {
	// Month: AAAA-MM.
	Month string `json:"month"`
	// MRRCents e Subscriptions: as mensalidades do mês e quantas são.
	MRRCents      int64 `json:"mrrCents"`
	Subscriptions int   `json:"subscriptions"`
	// As assinaturas com a 1ª mensalidade no mês.
	NewCents int64 `json:"newCents"`
	NewCount int   `json:"newCount"`
	// As encerradas no mês, pela mensalidade do encerramento.
	ChurnCents int64 `json:"churnCents"`
	ChurnCount int   `json:"churnCount"`
	// ChangeCents é a variação sobre o mês anterior; OtherCents, o que ela
	// tem além de novas e encerradas (fim de promoção, reajuste, assinatura
	// sem mensalidade no mês).
	ChangeCents int64 `json:"changeCents"`
	OtherCents  int64 `json:"otherCents"`
	// As parcelas do rastreador das faturas que vencem no mês e quanto delas
	// já foi pago. TotalCents: mensalidades + parcelas.
	InstallmentsCents     int64 `json:"installmentsCents"`
	InstallmentsPaidCents int64 `json:"installmentsPaidCents"`
	TotalCents            int64 `json:"totalCents"`
}

// MRR é o resumo: o mês atual e os anteriores, do atual para trás.
type MRR struct {
	// As mensalidades do mês atual, quantas são, as parcelas e o total.
	CurrentCents      int64      `json:"currentCents"`
	Subscriptions     int        `json:"subscriptions"`
	InstallmentsCents int64      `json:"installmentsCents"`
	TotalCents        int64      `json:"totalCents"`
	Months            []MRRMonth `json:"months"`
	// Forecast: o que vai ser cobrado no mês que vem.
	Forecast *MRRForecast `json:"forecast"`
}

// mrrSub é o preço de uma assinatura e quando ela foi encerrada.
type mrrSub struct {
	priceCents int
	promoCents *int
	promoUntil *Date
	nextCents  *int
	nextFrom   *Date
	canceled   time.Time
	isCanceled bool
}

func (m mrrSub) priceOn(day Date) int {
	amount, _ := priceOn(m.priceCents, m.nextCents, m.nextFrom, m.promoCents, m.promoUntil, day)
	return amount
}

// mrrBill é o que as faturas de um mês cobram.
type mrrBill struct {
	monthly, newCents    int64
	monthlies, newCount  int
	parcels, parcelsPaid int64
}

// mrrChurn: as assinaturas encerradas num mês.
type mrrChurn struct {
	cents int64
	count int
}

// churnByMonth agrupa as encerradas pelo mês do encerramento (no fuso loc),
// pela mensalidade do dia; as grátis não contam.
func churnByMonth(subs []mrrSub, loc *time.Location) map[string]mrrChurn {
	out := map[string]mrrChurn{}
	for _, s := range subs {
		if !s.isCanceled {
			continue
		}
		day := DateIn(s.canceled, loc)
		if p := s.priceOn(day); p > 0 {
			c := out[day.Format("2006-01")]
			c.cents += int64(p)
			c.count++
			out[day.Format("2006-01")] = c
		}
	}
	return out
}

// assembleMRR monta `months` meses, do de cur para trás: as faturas de cada
// mês (billed), o mês atual inteiro (current: as geradas e as que ainda vão
// sair) e as encerradas.
func assembleMRR(billed map[string]mrrBill, current MRRForecast, churn map[string]mrrChurn, cur Date, months int) *MRR {
	out := &MRR{Months: make([]MRRMonth, 0, months)}
	for i := 0; i < months; i++ {
		start := NewDate(cur.Year(), cur.Month()-time.Month(i), 1)
		key := start.Format("2006-01")
		b := billed[key]
		m := MRRMonth{Month: key, MRRCents: b.monthly, Subscriptions: b.monthlies, NewCents: b.newCents, NewCount: b.newCount,
			InstallmentsCents: b.parcels, InstallmentsPaidCents: b.parcelsPaid}
		if i == 0 {
			m.MRRCents, m.Subscriptions, m.InstallmentsCents = current.MonthlyCents, current.Monthlies, current.InstallmentsCents
		}
		c := churn[key]
		m.ChurnCents, m.ChurnCount = c.cents, c.count
		prev := billed[NewDate(start.Year(), start.Month()-1, 1).Format("2006-01")].monthly
		m.ChangeCents = m.MRRCents - prev
		m.OtherCents = m.ChangeCents - (m.NewCents - m.ChurnCents)
		m.TotalCents = m.MRRCents + m.InstallmentsCents
		out.Months = append(out.Months, m)
	}
	if len(out.Months) > 0 {
		m := out.Months[0]
		out.CurrentCents, out.Subscriptions, out.InstallmentsCents, out.TotalCents = m.MRRCents, m.Subscriptions, m.InstallmentsCents, m.TotalCents
	}
	return out
}

// MRR é o resumo dos últimos `months` meses (o atual incluído) e a previsão
// do mês que vem.
func (s *Service) MRR(ctx context.Context, months int) (*MRR, error) {
	today := s.Today()
	cur := NewDate(today.Year(), today.Month(), 1)
	next := NewDate(today.Year(), today.Month()+1, 1)
	// Um mês antes do primeiro: a variação dele precisa do anterior.
	from := NewDate(today.Year(), today.Month()-time.Month(months), 1)
	billed, err := s.repo.mrrBilled(ctx, from, next)
	if err != nil {
		return nil, err
	}
	subs, generated, err := s.repo.forecastData(ctx, cur, next)
	if err != nil {
		return nil, err
	}
	canceled, err := s.repo.mrrCanceled(ctx, time.Date(from.Year(), from.Month(), 1, 0, 0, 0, 0, s.loc))
	if err != nil {
		return nil, err
	}
	out := assembleMRR(billed, computeForecast(subs, generated, cur, next), churnByMonth(canceled, s.loc), cur, months)
	if out.Forecast, err = s.Forecast(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

// mrrBilled soma, por mês de vencimento em [from, to), as faturas de
// clientes (menos as canceladas): as mensalidades (a fatura da assinatura
// menos as parcelas que leva; as de valor zero não contam), as que são a 1ª
// da assinatura e as parcelas do rastreador (de qualquer fatura).
func (r *Repository) mrrBilled(ctx context.Context, from, to Date) (map[string]mrrBill, error) {
	rows, err := r.db.Query(ctx, `
		WITH inv AS (
			SELECT i.subscription_id, i.due_date, i.status, i.amount_cents::bigint AS amount,
				COALESCE((SELECT sum(e.amount_cents) FROM equipment_installments e WHERE e.invoice_id = i.id), 0)::bigint AS parcel,
				i.subscription_id IS NOT NULL AND NOT EXISTS (
					SELECT 1 FROM invoices p WHERE p.subscription_id = i.subscription_id AND p.status <> 'CANCELED'
						AND p.due_date < i.due_date) AS first
			FROM invoices i JOIN users u ON u.id = i.customer_id
			WHERE i.status <> 'CANCELED' AND u.role = 'customer' AND i.due_date >= $1::date AND i.due_date < $2::date)
		SELECT to_char(due_date, 'YYYY-MM'),
			COALESCE(sum(amount - parcel) FILTER (WHERE subscription_id IS NOT NULL AND amount > parcel), 0)::bigint,
			count(*) FILTER (WHERE subscription_id IS NOT NULL AND amount > parcel),
			COALESCE(sum(amount - parcel) FILTER (WHERE first AND amount > parcel), 0)::bigint,
			count(*) FILTER (WHERE first AND amount > parcel),
			COALESCE(sum(parcel), 0)::bigint,
			COALESCE(sum(parcel) FILTER (WHERE status = 'PAID'), 0)::bigint
		FROM inv GROUP BY 1`, from.String(), to.String())
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	out := map[string]mrrBill{}
	for rows.Next() {
		var month string
		var b mrrBill
		if err := rows.Scan(&month, &b.monthly, &b.monthlies, &b.newCents, &b.newCount, &b.parcels, &b.parcelsPaid); err != nil {
			return nil, err
		}
		out[month] = b
	}
	return out, database.MapError(rows.Err())
}

// mrrCanceled: as assinaturas de clientes encerradas desde from.
func (r *Repository) mrrCanceled(ctx context.Context, from time.Time) ([]mrrSub, error) {
	rows, err := r.db.Query(ctx, `
		SELECT s.price_cents, s.promo_price_cents, s.promo_until, s.next_price_cents, s.next_price_from,
			COALESCE(s.canceled_at, s.updated_at)
		FROM subscriptions s JOIN users u ON u.id = s.customer_id
		WHERE u.role = 'customer' AND s.status = 'CANCELED' AND COALESCE(s.canceled_at, s.updated_at) >= $1`, from)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	var out []mrrSub
	for rows.Next() {
		var m mrrSub
		var promoUntil, nextFrom *time.Time
		if err := rows.Scan(&m.priceCents, &m.promoCents, &promoUntil, &m.nextCents, &nextFrom, &m.canceled); err != nil {
			return nil, err
		}
		m.isCanceled = true
		if promoUntil != nil {
			m.promoUntil = &Date{*promoUntil}
		}
		if nextFrom != nil {
			m.nextFrom = &Date{*nextFrom}
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// MRRForecast é a previsão do mês que vem: o que vai ser cobrado nele, as
// mensalidades e as parcelas do rastreador das faturas que vencem no mês —
// as já geradas e as que o gerador ainda vai gerar, pela mesma regra de datas,
// preços e parcelas. Sem as grátis (mensalidade zero) e sem as faturas
// avulsas (o equipamento à vista, o frete).
type MRRForecast struct {
	// Month: AAAA-MM.
	Month string `json:"month"`
	// MonthlyCents e Monthlies: as mensalidades e quantas são.
	MonthlyCents      int64 `json:"monthlyCents"`
	Monthlies         int   `json:"monthlies"`
	InstallmentsCents int64 `json:"installmentsCents"`
	TotalCents        int64 `json:"totalCents"`
}

// forecastSub é uma assinatura ativa com o que o gerador vai fazer dela.
type forecastSub struct {
	id uuid.UUID
	mrrSub
	dueDay  int
	next    Date
	pending []int64 // as parcelas por cobrar, em ordem
}

// forecastInvoice é uma mensalidade já gerada que vence no mês.
type forecastInvoice struct {
	subscription uuid.UUID
	due          Date
	monthly      int64
	installment  int64
}

// computeForecast monta a previsão do mês [start, end).
func computeForecast(subs []forecastSub, generated []forecastInvoice, start, end Date) MRRForecast {
	out := MRRForecast{Month: start.Format("2006-01")}
	add := func(monthly, installment int64) {
		if monthly > 0 {
			out.MonthlyCents += monthly
			out.Monthlies++
		}
		out.InstallmentsCents += installment
	}
	exists := map[uuid.UUID]map[Date]bool{}
	for _, inv := range generated {
		add(inv.monthly, inv.installment)
		if exists[inv.subscription] == nil {
			exists[inv.subscription] = map[Date]bool{}
		}
		exists[inv.subscription][inv.due] = true
	}
	for _, s := range subs {
		// As que o gerador ainda vai fazer, desde a próxima: cada uma leva a
		// próxima parcela (a que já existe no mesmo dia não sai de novo).
		parcel := 0
		for d, i := s.next, 0; d.Before(end) && i < maxCatchUp; d, i = followingDue(d, s.dueDay), i+1 {
			if exists[s.id][d] {
				continue
			}
			var installment int64
			if parcel < len(s.pending) {
				installment = s.pending[parcel]
				parcel++
			}
			if !d.Before(start) {
				add(int64(s.priceOn(d)), installment)
			}
		}
	}
	out.TotalCents = out.MonthlyCents + out.InstallmentsCents
	return out
}

// Forecast é a previsão do mês seguinte ao de hoje.
func (s *Service) Forecast(ctx context.Context) (*MRRForecast, error) {
	today := s.Today()
	start := NewDate(today.Year(), today.Month()+1, 1)
	end := NewDate(today.Year(), today.Month()+2, 1)
	subs, generated, err := s.repo.forecastData(ctx, start, end)
	if err != nil {
		return nil, err
	}
	out := computeForecast(subs, generated, start, end)
	return &out, nil
}

// forecastData: as assinaturas ativas de clientes com mensalidade a gerar
// antes de end e as mensalidades já geradas que vencem em [start, end).
func (r *Repository) forecastData(ctx context.Context, start, end Date) ([]forecastSub, []forecastInvoice, error) {
	rows, err := r.db.Query(ctx, `
		SELECT s.id, s.due_day, s.next_due_date, s.price_cents, s.promo_price_cents, s.promo_until, s.next_price_cents, s.next_price_from,
			COALESCE((SELECT array_agg(e.amount_cents::bigint ORDER BY e.number) FROM equipment_installments e
				WHERE e.subscription_id = s.id AND e.invoice_id IS NULL AND e.waived_at IS NULL), '{}')
		FROM subscriptions s JOIN users u ON u.id = s.customer_id
		WHERE s.status = 'ACTIVE' AND u.role = 'customer' AND s.next_due_date < $1::date`, end.String())
	if err != nil {
		return nil, nil, database.MapError(err)
	}
	var subs []forecastSub
	for rows.Next() {
		var f forecastSub
		var next time.Time
		var promoUntil, nextFrom *time.Time
		if err := rows.Scan(&f.id, &f.dueDay, &next, &f.priceCents, &f.promoCents, &promoUntil, &f.nextCents, &nextFrom, &f.pending); err != nil {
			rows.Close()
			return nil, nil, err
		}
		f.next = Date{next}
		if promoUntil != nil {
			f.promoUntil = &Date{*promoUntil}
		}
		if nextFrom != nil {
			f.nextFrom = &Date{*nextFrom}
		}
		subs = append(subs, f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, database.MapError(err)
	}

	rows, err = r.db.Query(ctx, `
		SELECT i.subscription_id, i.due_date, i.amount_cents::bigint - p.cents, p.cents
		FROM invoices i
		JOIN users u ON u.id = i.customer_id
		CROSS JOIN LATERAL (SELECT COALESCE(sum(e.amount_cents), 0)::bigint AS cents FROM equipment_installments e WHERE e.invoice_id = i.id) p
		WHERE i.subscription_id IS NOT NULL AND i.status <> 'CANCELED' AND u.role = 'customer'
			AND i.due_date >= $1::date AND i.due_date < $2::date`, start.String(), end.String())
	if err != nil {
		return nil, nil, database.MapError(err)
	}
	defer rows.Close()
	var generated []forecastInvoice
	for rows.Next() {
		var f forecastInvoice
		var due time.Time
		if err := rows.Scan(&f.subscription, &due, &f.monthly, &f.installment); err != nil {
			return nil, nil, err
		}
		f.due = Date{due}
		generated = append(generated, f)
	}
	return subs, generated, database.MapError(rows.Err())
}
