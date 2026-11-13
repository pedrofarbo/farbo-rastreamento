package billing

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// O MRR (receita recorrente mensal). Nos meses passados, pelo que foi
// cobrado: as mensalidades das faturas que venceram em cada um (a do
// semestral e a do anual, divididas pelos meses que cobrem). No mês atual e
// na previsão do mês que vem, pelas assinaturas ativas: cada uma pelo valor
// do mês, também a que não tem fatura nele — quem contrata depois do dia da
// conta paga a 2ª mensalidade só no mês seguinte, e o semestral e o anual
// vencem uma vez por período —, para o mês que vem não parecer menor sem
// ninguém ter saído. As grátis (mensalidade zero) não somam nem contam, e as
// faturas avulsas (o equipamento à vista, o frete) ficam de fora. Ao lado, a
// mensalidade das associações e seguradoras (os veículos das faturas delas
// que vencem no mês; no atual e na previsão, a fatura que ainda não saiu pelos
// veículos que contam hoje), as parcelas do rastreador que vencem no mês e o
// total; a previsão traz também o que vai ser cobrado de fato no mês.

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
	// ChangeCents é a variação das mensalidades sobre o mês anterior;
	// OtherCents, o que ela tem além de novas e encerradas (fim de promoção,
	// reajuste, assinatura sem mensalidade no mês).
	ChangeCents int64 `json:"changeCents"`
	OtherCents  int64 `json:"otherCents"`
	// A mensalidade das associações e seguradoras (os veículos das faturas
	// delas), quantos veículos e a variação sobre o mês anterior.
	OrganizationsCents       int64 `json:"organizationsCents"`
	OrganizationVehicles     int   `json:"organizationVehicles"`
	OrganizationsChangeCents int64 `json:"organizationsChangeCents"`
	// As parcelas do rastreador das faturas que vencem no mês e quanto delas
	// já foi pago. TotalCents: mensalidades + associações + parcelas.
	InstallmentsCents     int64 `json:"installmentsCents"`
	InstallmentsPaidCents int64 `json:"installmentsPaidCents"`
	TotalCents            int64 `json:"totalCents"`
}

// MRR é o resumo: o mês atual e os anteriores, do atual para trás.
type MRR struct {
	// As mensalidades do mês atual, quantas são, as associações, as parcelas
	// e o total.
	CurrentCents         int64      `json:"currentCents"`
	Subscriptions        int        `json:"subscriptions"`
	OrganizationsCents   int64      `json:"organizationsCents"`
	OrganizationVehicles int        `json:"organizationVehicles"`
	InstallmentsCents    int64      `json:"installmentsCents"`
	TotalCents           int64      `json:"totalCents"`
	Months               []MRRMonth `json:"months"`
	// Forecast: o que vai ser cobrado no mês que vem.
	Forecast *MRRForecast `json:"forecast"`
}

// mrrSub é o preço de uma assinatura e quando ela foi encerrada.
type mrrSub struct {
	priceCents   int
	promoCents   *int
	promoUntil   *Date
	nextCents    *int
	nextFrom     *Date
	trackerCents int
	trackerUntil *Date
	trackerAdded bool
	canceled     time.Time
	isCanceled   bool
	// period: de quantos em quantos meses é cobrada (0 ou 1, o mensal).
	period int
}

// priceOn é a fatura do dia (a mensalidade, ou o valor do semestral ou do
// anual), com a parte do rastreador incluso quando ela é somada (como o
// gerador faz).
func (m mrrSub) priceOn(day Date) int {
	amount, _ := priceOn(m.priceCents, m.nextCents, m.nextFrom, m.promoCents, m.promoUntil, day)
	return amount + trackerOn(m.trackerCents, m.trackerAdded, m.trackerUntil, day)
}

// mrrOn é o que a assinatura rende por mês: a mensalidade, ou o valor do
// semestral ou do anual dividido pelos meses.
func (m mrrSub) mrrOn(day Date) int {
	return m.priceOn(day) / max(m.period, 1)
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
		if p := s.mrrOn(day); p > 0 {
			c := out[day.Format("2006-01")]
			c.cents += int64(p)
			c.count++
			out[day.Format("2006-01")] = c
		}
	}
	return out
}

// mrrOrg é a mensalidade das associações e seguradoras num mês.
type mrrOrg struct {
	cents    int64
	vehicles int
}

// assembleMRR monta `months` meses, do de cur para trás: as faturas de cada
// mês (billed e, das organizações, orgs), o mês atual inteiro (current: as
// geradas e as que ainda vão sair) e as encerradas.
func assembleMRR(billed map[string]mrrBill, orgs map[string]mrrOrg, current MRRForecast, churn map[string]mrrChurn, cur Date,
	months int) *MRR {
	out := &MRR{Months: make([]MRRMonth, 0, months)}
	for i := 0; i < months; i++ {
		start := NewDate(cur.Year(), cur.Month()-time.Month(i), 1)
		key := start.Format("2006-01")
		b := billed[key]
		m := MRRMonth{Month: key, MRRCents: b.monthly, Subscriptions: b.monthlies, NewCents: b.newCents, NewCount: b.newCount,
			InstallmentsCents: b.parcels, InstallmentsPaidCents: b.parcelsPaid}
		m.OrganizationsCents, m.OrganizationVehicles = orgs[key].cents, orgs[key].vehicles
		if i == 0 {
			m.MRRCents, m.Subscriptions, m.InstallmentsCents = current.MonthlyCents, current.Monthlies, current.InstallmentsCents
			m.OrganizationsCents, m.OrganizationVehicles = current.OrganizationsCents, current.OrganizationVehicles
		}
		c := churn[key]
		m.ChurnCents, m.ChurnCount = c.cents, c.count
		prevKey := NewDate(start.Year(), start.Month()-1, 1).Format("2006-01")
		m.ChangeCents = m.MRRCents - billed[prevKey].monthly
		m.OtherCents = m.ChangeCents - (m.NewCents - m.ChurnCents)
		m.OrganizationsChangeCents = m.OrganizationsCents - orgs[prevKey].cents
		m.TotalCents = m.MRRCents + m.OrganizationsCents + m.InstallmentsCents
		out.Months = append(out.Months, m)
	}
	if len(out.Months) > 0 {
		m := out.Months[0]
		out.CurrentCents, out.Subscriptions, out.InstallmentsCents, out.TotalCents = m.MRRCents, m.Subscriptions, m.InstallmentsCents, m.TotalCents
		out.OrganizationsCents, out.OrganizationVehicles = m.OrganizationsCents, m.OrganizationVehicles
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
	orgs, err := s.repo.mrrOrgBilled(ctx, from, next)
	if err != nil {
		return nil, err
	}
	// O mês atual: as parcelas do rastreador pelo que é cobrado nele; as
	// mensalidades, pelas assinaturas ativas (a receita recorrente de cada
	// uma no mês, também a que não tem fatura nele; o semestral e o anual
	// divididos pelos meses).
	monthlySubs, monthlyInvoices := monthlyOnly(subs, generated)
	current := computeForecast(monthlySubs, monthlyInvoices, cur, next)
	active, err := s.repo.recurringData(ctx, cur)
	if err != nil {
		return nil, err
	}
	current.MonthlyCents, current.Monthlies = recurringMRR(active, cur)
	if err := s.addOrganizations(ctx, &current, cur, next); err != nil {
		return nil, err
	}
	out := assembleMRR(billed, orgs, current, churnByMonth(canceled, s.loc), cur, months)
	if out.Forecast, err = s.Forecast(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

// mrrBilled soma, por mês em [from, to), as faturas de clientes (menos as
// canceladas): as mensalidades (a fatura da assinatura menos as parcelas que
// leva; as de valor zero não contam), as que são a 1ª da assinatura e as
// parcelas do rastreador (de qualquer fatura). A do semestral e a do anual
// entram em cada mês que cobrem, pelo valor dividido pelos meses.
func (r *Repository) mrrBilled(ctx context.Context, from, to Date) (map[string]mrrBill, error) {
	rows, err := r.db.Query(ctx, `
		WITH inv AS (
			SELECT i.subscription_id, i.due_date, i.status, i.amount_cents::bigint AS amount, i.period_months AS period,
				COALESCE((SELECT sum(e.amount_cents) FROM equipment_installments e WHERE e.invoice_id = i.id), 0)::bigint AS parcel,
				i.subscription_id IS NOT NULL AND NOT EXISTS (
					SELECT 1 FROM invoices p WHERE p.subscription_id = i.subscription_id AND p.status <> 'CANCELED'
						AND p.due_date < i.due_date) AS first
			FROM invoices i JOIN users u ON u.id = i.customer_id
			WHERE i.status <> 'CANCELED' AND u.role = 'customer' AND NOT checkout_pending(u.id)
				AND i.due_date >= ($1::date - interval '11 months') AND i.due_date < $2::date),
		spread AS (
			SELECT (date_trunc('month', due_date) + k * interval '1 month')::date AS month, k, subscription_id, status,
				amount, parcel, first, (amount - parcel) / period AS share
			FROM inv CROSS JOIN LATERAL generate_series(0, period - 1) AS k)
		SELECT to_char(month, 'YYYY-MM'),
			COALESCE(sum(share) FILTER (WHERE subscription_id IS NOT NULL AND amount > parcel), 0)::bigint,
			count(*) FILTER (WHERE subscription_id IS NOT NULL AND amount > parcel),
			COALESCE(sum(share) FILTER (WHERE first AND k = 0 AND amount > parcel), 0)::bigint,
			count(*) FILTER (WHERE first AND k = 0 AND amount > parcel),
			COALESCE(sum(parcel) FILTER (WHERE k = 0), 0)::bigint,
			COALESCE(sum(parcel) FILTER (WHERE k = 0 AND status = 'PAID'), 0)::bigint
		FROM spread WHERE month >= $1::date AND month < $2::date GROUP BY 1`, from.String(), to.String())
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

// mrrOrgBilled soma, por mês de vencimento em [from, to), a mensalidade dos
// veículos nas faturas das organizações (menos as canceladas): só a linha
// "monthly" do extrato — o proporcional, a instalação, o equipamento e a
// desinstalação são avulsos.
func (r *Repository) mrrOrgBilled(ctx context.Context, from, to Date) (map[string]mrrOrg, error) {
	rows, err := r.db.Query(ctx, `
		SELECT to_char(oi.due_date, 'YYYY-MM'), COALESCE(sum((it->>'amountCents')::bigint), 0)::bigint, count(*)
		FROM organization_invoices oi JOIN invoices i ON i.id = oi.invoice_id
		CROSS JOIN LATERAL jsonb_array_elements(oi.items) it
		WHERE i.status <> 'CANCELED' AND it->>'kind' = 'monthly' AND oi.due_date >= $1::date AND oi.due_date < $2::date
		GROUP BY 1`, from.String(), to.String())
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	out := map[string]mrrOrg{}
	for rows.Next() {
		var month string
		var o mrrOrg
		if err := rows.Scan(&month, &o.cents, &o.vehicles); err != nil {
			return nil, err
		}
		out[month] = o
	}
	return out, database.MapError(rows.Err())
}

// orgMonthRow é uma organização no mês: a fatura dele (se já saiu) e, para a
// que ainda não saiu, o preço, o dia do vencimento e os veículos que contam
// hoje.
type orgMonthRow struct {
	active          bool
	priceCents      int
	dueDay          int
	created         time.Time
	counting        int
	invoiced        bool
	invoiceCents    int64
	invoiceVehicles int
}

// computeOrgMonth é a mensalidade das organizações no mês que começa em
// start: a da fatura, quando já saiu; senão, para a organização ativa, os
// veículos que contam hoje no preço dela — a partir do 1º vencimento (o do
// mês da criação, se não for antes dela).
func computeOrgMonth(rows []orgMonthRow, start Date, loc *time.Location) mrrOrg {
	var out mrrOrg
	for _, o := range rows {
		if o.invoiced {
			out.cents += o.invoiceCents
			out.vehicles += o.invoiceVehicles
			continue
		}
		due := NewDate(start.Year(), start.Month(), o.dueDay)
		if !o.active || o.priceCents <= 0 || o.counting == 0 || due.Before(DateIn(o.created, loc)) {
			continue
		}
		out.cents += int64(o.priceCents) * int64(o.counting)
		out.vehicles += o.counting
	}
	return out
}

// addOrganizations põe na previsão do mês [start, end) a mensalidade das
// organizações.
func (s *Service) addOrganizations(ctx context.Context, f *MRRForecast, start, end Date) error {
	rows, err := s.repo.orgMonth(ctx, start, end)
	if err != nil {
		return err
	}
	o := computeOrgMonth(rows, start, s.loc)
	f.OrganizationsCents, f.OrganizationVehicles = o.cents, o.vehicles
	f.TotalCents = f.MonthlyCents + f.OrganizationsCents + f.InstallmentsCents
	return nil
}

// orgMonth: cada organização com a mensalidade da fatura que vence em
// [start, end) (se já saiu) e os veículos que contam hoje.
func (r *Repository) orgMonth(ctx context.Context, start, end Date) ([]orgMonthRow, error) {
	rows, err := r.db.Query(ctx, `
		SELECT u.active, o.price_cents, o.due_day, o.created_at,
			(SELECT count(*) FROM organization_vehicles ov WHERE ov.organization_id = o.id AND ov.removed_on IS NULL),
			inv.id IS NOT NULL,
			COALESCE((SELECT sum((it->>'amountCents')::bigint) FROM jsonb_array_elements(inv.items) it WHERE it->>'kind' = 'monthly'), 0)::bigint,
			COALESCE((SELECT count(*) FROM jsonb_array_elements(inv.items) it WHERE it->>'kind' = 'monthly'), 0)
		FROM organizations o JOIN users u ON u.id = o.id
		LEFT JOIN LATERAL (
			SELECT oi.invoice_id AS id, oi.items FROM organization_invoices oi JOIN invoices i ON i.id = oi.invoice_id
			WHERE oi.organization_id = o.id AND i.status <> 'CANCELED' AND oi.due_date >= $1::date AND oi.due_date < $2::date
			LIMIT 1) inv ON TRUE`, start.String(), end.String())
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	var out []orgMonthRow
	for rows.Next() {
		var o orgMonthRow
		if err := rows.Scan(&o.active, &o.priceCents, &o.dueDay, &o.created, &o.counting, &o.invoiced, &o.invoiceCents,
			&o.invoiceVehicles); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, database.MapError(rows.Err())
}

// mrrCanceled: as assinaturas de clientes encerradas desde from.
func (r *Repository) mrrCanceled(ctx context.Context, from time.Time) ([]mrrSub, error) {
	rows, err := r.db.Query(ctx, `
		SELECT s.price_cents, s.promo_price_cents, s.promo_until, s.next_price_cents, s.next_price_from,
			COALESCE(s.canceled_at, s.updated_at), s.tracker_cents, s.tracker_until, s.tracker_added, s.period_months
		FROM subscriptions s JOIN users u ON u.id = s.customer_id
		WHERE u.role = 'customer' AND s.status = 'CANCELED' AND COALESCE(s.canceled_at, s.updated_at) >= $1`, from)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	var out []mrrSub
	for rows.Next() {
		var m mrrSub
		var promoUntil, nextFrom, trackerUntil *time.Time
		if err := rows.Scan(&m.priceCents, &m.promoCents, &promoUntil, &m.nextCents, &nextFrom, &m.canceled,
			&m.trackerCents, &trackerUntil, &m.trackerAdded, &m.period); err != nil {
			return nil, err
		}
		m.isCanceled = true
		if trackerUntil != nil {
			m.trackerUntil = &Date{*trackerUntil}
		}
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

// MRRForecast é um mês do MRR pelas assinaturas: a receita recorrente das
// assinaturas ativas (cada uma pelo valor do mês, com a promoção, o reajuste e
// a parte do rastreador somada; o semestral e o anual divididos pelos meses),
// também a que não tem fatura no mês — quem contratou depois do dia da conta
// paga a 2ª mensalidade só no mês seguinte —, mais as associações e as
// parcelas do rastreador que vencem no mês. Sem as grátis. BilledCents é o
// que vai ser cobrado de fato no mês (as faturas que vencem nele, já geradas
// e as que o gerador ainda vai gerar, com o semestral e o anual pelo valor
// cheio no mês em que vencem).
type MRRForecast struct {
	// Month: AAAA-MM.
	Month string `json:"month"`
	// MonthlyCents e Monthlies: a receita recorrente das assinaturas e quantas são.
	MonthlyCents int64 `json:"monthlyCents"`
	Monthlies    int   `json:"monthlies"`
	// BilledCents: o que vai ser cobrado no mês (as faturas das assinaturas,
	// as parcelas e as associações).
	BilledCents int64 `json:"billedCents"`
	// A mensalidade das associações e seguradoras e quantos veículos.
	OrganizationsCents   int64 `json:"organizationsCents"`
	OrganizationVehicles int   `json:"organizationVehicles"`
	InstallmentsCents    int64 `json:"installmentsCents"`
	TotalCents           int64 `json:"totalCents"`
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
	// period: os meses que ela cobre (0 ou 1, o mensal).
	period int
}

// monthlyOnly separa as mensais (as do semestral e do anual rendem todo mês
// pelo valor dividido, fora daqui).
func monthlyOnly(subs []forecastSub, generated []forecastInvoice) ([]forecastSub, []forecastInvoice) {
	var s []forecastSub
	for _, x := range subs {
		if x.period <= 1 {
			s = append(s, x)
		}
	}
	var g []forecastInvoice
	for _, x := range generated {
		if x.period <= 1 {
			g = append(g, x)
		}
	}
	return s, g
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
		for d, i := s.next, 0; d.Before(end) && i < maxCatchUp; d, i = followingPeriod(d, s.dueDay, s.period), i+1 {
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
	// O que vai ser cobrado no mês, e as parcelas dele.
	billed := computeForecast(subs, generated, start, end)
	active, err := s.repo.recurringData(ctx, start)
	if err != nil {
		return nil, err
	}
	out := MRRForecast{Month: billed.Month, InstallmentsCents: billed.InstallmentsCents}
	out.MonthlyCents, out.Monthlies = recurringMRR(active, start)
	if err := s.addOrganizations(ctx, &out, start, end); err != nil {
		return nil, err
	}
	out.BilledCents = billed.TotalCents + out.OrganizationsCents
	return &out, nil
}

// recurringSub é uma assinatura ativa no MRR pelas assinaturas: o preço e o
// dia da conta.
type recurringSub struct {
	mrrSub
	dueDay int
}

// recurringMRR é a receita recorrente das assinaturas no mês que começa em
// start: cada uma pelo valor do vencimento dela no mês (com a promoção, o
// reajuste e a parte do rastreador somada), o semestral e o anual divididos
// pelos meses; as grátis não contam.
func recurringMRR(subs []recurringSub, start Date) (int64, int) {
	var cents int64
	var count int
	last := NewDate(start.Year(), start.Month()+1, 0).Day()
	for _, s := range subs {
		day := NewDate(start.Year(), start.Month(), min(max(s.dueDay, 1), last))
		if v := s.mrrOn(day); v > 0 {
			cents += int64(v)
			count++
		}
	}
	return cents, count
}

// recurringData: as assinaturas ativas de clientes (sem a contratação pelo
// site ainda não paga) que seguem ativas no mês que começa em start (o
// semestral ou o anual encerrado antes dele, não).
func (r *Repository) recurringData(ctx context.Context, start Date) ([]recurringSub, error) {
	rows, err := r.db.Query(ctx, `
		SELECT s.price_cents, s.promo_price_cents, s.promo_until, s.next_price_cents, s.next_price_from,
			s.tracker_cents, s.tracker_until, s.tracker_added, s.period_months, s.due_day
		FROM subscriptions s JOIN users u ON u.id = s.customer_id
		WHERE s.status = 'ACTIVE' AND u.role = 'customer' AND NOT checkout_pending(u.id)
			AND (s.ends_on IS NULL OR s.ends_on > $1::date)`, start.String())
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	var out []recurringSub
	for rows.Next() {
		var m recurringSub
		var promoUntil, nextFrom, trackerUntil *time.Time
		if err := rows.Scan(&m.priceCents, &m.promoCents, &promoUntil, &m.nextCents, &nextFrom,
			&m.trackerCents, &trackerUntil, &m.trackerAdded, &m.period, &m.dueDay); err != nil {
			return nil, err
		}
		if promoUntil != nil {
			m.promoUntil = &Date{*promoUntil}
		}
		if nextFrom != nil {
			m.nextFrom = &Date{*nextFrom}
		}
		if trackerUntil != nil {
			m.trackerUntil = &Date{*trackerUntil}
		}
		out = append(out, m)
	}
	return out, database.MapError(rows.Err())
}

// forecastData: as assinaturas ativas de clientes com mensalidade a gerar
// antes de end e as mensalidades já geradas que vencem em [start, end).
func (r *Repository) forecastData(ctx context.Context, start, end Date) ([]forecastSub, []forecastInvoice, error) {
	rows, err := r.db.Query(ctx, `
		SELECT s.id, s.due_day, s.next_due_date, s.price_cents, s.promo_price_cents, s.promo_until, s.next_price_cents, s.next_price_from,
			COALESCE((SELECT array_agg(e.amount_cents::bigint ORDER BY e.number) FROM equipment_installments e
				WHERE e.subscription_id = s.id AND e.invoice_id IS NULL AND e.waived_at IS NULL), '{}'),
			s.tracker_cents, s.tracker_until, s.tracker_added, s.period_months
		FROM subscriptions s JOIN users u ON u.id = s.customer_id
		WHERE s.status = 'ACTIVE' AND u.role = 'customer' AND NOT checkout_pending(u.id) AND s.next_due_date < $1::date
			AND (s.ends_on IS NULL OR s.next_due_date < s.ends_on)`,
		end.String())
	if err != nil {
		return nil, nil, database.MapError(err)
	}
	var subs []forecastSub
	for rows.Next() {
		var f forecastSub
		var next time.Time
		var promoUntil, nextFrom, trackerUntil *time.Time
		if err := rows.Scan(&f.id, &f.dueDay, &next, &f.priceCents, &f.promoCents, &promoUntil, &f.nextCents, &nextFrom, &f.pending,
			&f.trackerCents, &trackerUntil, &f.trackerAdded, &f.period); err != nil {
			rows.Close()
			return nil, nil, err
		}
		f.next = Date{next}
		if trackerUntil != nil {
			f.trackerUntil = &Date{*trackerUntil}
		}
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
		SELECT i.subscription_id, i.due_date, i.amount_cents::bigint - p.cents, p.cents, i.period_months
		FROM invoices i
		JOIN users u ON u.id = i.customer_id
		CROSS JOIN LATERAL (SELECT COALESCE(sum(e.amount_cents), 0)::bigint AS cents FROM equipment_installments e WHERE e.invoice_id = i.id) p
		WHERE i.subscription_id IS NOT NULL AND i.status <> 'CANCELED' AND u.role = 'customer' AND NOT checkout_pending(u.id)
			AND i.due_date >= $1::date AND i.due_date < $2::date`, start.String(), end.String())
	if err != nil {
		return nil, nil, database.MapError(err)
	}
	defer rows.Close()
	var generated []forecastInvoice
	for rows.Next() {
		var f forecastInvoice
		var due time.Time
		if err := rows.Scan(&f.subscription, &due, &f.monthly, &f.installment, &f.period); err != nil {
			return nil, nil, err
		}
		f.due = Date{due}
		generated = append(generated, f)
	}
	return subs, generated, database.MapError(rows.Err())
}
