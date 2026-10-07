package finance

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// tz é o fuso em que um pagamento de fatura (instante) vira um dia do caixa.
const tz = "America/Sao_Paulo"

// paidFlows são as entradas e saídas pagas, com o dia: as faturas dos
// clientes (no dia do pagamento, em Brasília), as receitas avulsas e as
// contas pagas. $1 é o fuso.
const paidFlows = `(
	SELECT (COALESCE(paid_at, updated_at) AT TIME ZONE $1)::date AS day, 'invoice' AS source, amount_cents::bigint AS cents
	FROM invoices WHERE status = 'PAID'
	UNION ALL
	SELECT paid_on, lower(kind), paid_cents FROM finance_entries WHERE status = 'PAID'
) flows`

// Sum é quantas e quanto.
type Sum struct {
	Count int   `json:"count"`
	Cents int64 `json:"cents"`
}

// Alerts é o que pede atenção agora (o número no menu).
type Alerts struct {
	Overdue  int `json:"overdue"`
	DueToday int `json:"dueToday"`
	LowStock int `json:"lowStock"`
}

func (s *Service) Alerts(ctx context.Context) (*Alerts, error) {
	var out Alerts
	if err := s.db.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE due_date < $1), count(*) FILTER (WHERE due_date = $1)
		FROM finance_entries WHERE kind = 'PAYABLE' AND status = 'OPEN' AND due_date <= $1`, s.Today().Time).
		Scan(&out.Overdue, &out.DueToday); err != nil {
		return nil, database.MapError(err)
	}
	if err := s.db.QueryRow(ctx, `
		SELECT count(*) FROM stock_items WHERE active AND min_quantity > 0 AND quantity < min_quantity`).
		Scan(&out.LowStock); err != nil {
		return nil, database.MapError(err)
	}
	return &out, nil
}

// ---------------------------------------------------------------------------
// Fluxo de caixa
// ---------------------------------------------------------------------------

// CashMonth é um mês do caixa: o que entrou (faturas dos clientes e receitas
// avulsas), o que saiu e o saldo no fim (ou hoje, no mês corrente).
type CashMonth struct {
	Month         string `json:"month"`
	InvoicesCents int64  `json:"invoicesCents"`
	OtherInCents  int64  `json:"otherInCents"`
	InCents       int64  `json:"inCents"`
	OutCents      int64  `json:"outCents"`
	NetCents      int64  `json:"netCents"`
	// EndBalanceCents: nil nos meses antes do saldo inicial.
	EndBalanceCents *int64 `json:"endBalanceCents"`
}

// Projection é o saldo esperado daqui a Days dias: o de hoje, mais o que há
// para receber, menos o que há para pagar até lá — em aberto (inclusive o
// que já venceu), as mensalidades das assinaturas ativas e as contas
// recorrentes que ainda nem foram geradas.
type Projection struct {
	Days         int          `json:"days"`
	Until        billing.Date `json:"until"`
	InCents      int64        `json:"inCents"`
	OutCents     int64        `json:"outCents"`
	BalanceCents int64        `json:"balanceCents"`
}

type CashFlow struct {
	Settings     Settings     `json:"settings"`
	Today        billing.Date `json:"today"`
	BalanceCents int64        `json:"balanceCents"`
	Months       []CashMonth  `json:"months"`
	Projections  []Projection `json:"projections"`
}

// CashFlow monta os últimos months meses (com o corrente) e as projeções.
func (s *Service) CashFlow(ctx context.Context, months int) (*CashFlow, error) {
	if months < 1 || months > 36 {
		months = 12
	}
	settings, err := s.Settings(ctx)
	if err != nil {
		return nil, err
	}
	today := s.Today()
	first := billing.NewDate(today.Year(), today.Month()-time.Month(months-1), 1)
	out := &CashFlow{Settings: *settings, Today: today}

	byMonth := map[string]*CashMonth{}
	for m := first; !today.Before(m); m = AddMonths(m, 1) {
		cm := CashMonth{Month: Month(m)}
		out.Months = append(out.Months, cm)
	}
	for i := range out.Months {
		byMonth[out.Months[i].Month] = &out.Months[i]
	}

	rows, err := s.db.Query(ctx, `
		SELECT to_char(day, 'YYYY-MM'), source, sum(cents)::bigint FROM `+paidFlows+`
		WHERE day BETWEEN $2 AND $3 GROUP BY 1, 2`, tz, first.Time, today.Time)
	if err != nil {
		return nil, database.MapError(err)
	}
	for rows.Next() {
		var month, source string
		var cents int64
		if err := rows.Scan(&month, &source, &cents); err != nil {
			rows.Close()
			return nil, err
		}
		cm := byMonth[month]
		if cm == nil {
			continue
		}
		switch source {
		case "invoice":
			cm.InvoicesCents += cents
		case "receivable":
			cm.OtherInCents += cents
		case "payable":
			cm.OutCents += cents
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// O saldo conta só o que aconteceu a partir da data do saldo inicial.
	afterOpening := map[string]int64{}
	var before int64 // entre o saldo inicial e o primeiro mês da tabela
	rows, err = s.db.Query(ctx, `
		SELECT to_char(day, 'YYYY-MM'), sum(CASE WHEN source = 'payable' THEN -cents ELSE cents END)::bigint
		FROM `+paidFlows+`
		WHERE day BETWEEN $2 AND $3 GROUP BY 1`, tz, settings.OpeningDate.Time, today.Time)
	if err != nil {
		return nil, database.MapError(err)
	}
	for rows.Next() {
		var month string
		var net int64
		if err := rows.Scan(&month, &net); err != nil {
			rows.Close()
			return nil, err
		}
		if month < Month(first) {
			before += net
		} else {
			afterOpening[month] = net
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	running := settings.OpeningBalanceCents + before
	opening := Month(settings.OpeningDate)
	for i := range out.Months {
		cm := &out.Months[i]
		cm.InCents = cm.InvoicesCents + cm.OtherInCents
		cm.NetCents = cm.InCents - cm.OutCents
		running += afterOpening[cm.Month]
		if cm.Month >= opening {
			balance := running
			cm.EndBalanceCents = &balance
		}
	}
	out.BalanceCents = running

	for _, days := range []int{30, 60, 90} {
		p, err := s.project(ctx, today, days, out.BalanceCents)
		if err != nil {
			return nil, err
		}
		out.Projections = append(out.Projections, *p)
	}
	return out, nil
}

// Balance é o saldo de hoje.
func (s *Service) Balance(ctx context.Context) (int64, error) {
	settings, err := s.Settings(ctx)
	if err != nil {
		return 0, err
	}
	var net int64
	err = s.db.QueryRow(ctx, `
		SELECT COALESCE(sum(CASE WHEN source = 'payable' THEN -cents ELSE cents END), 0)::bigint
		FROM `+paidFlows+` WHERE day BETWEEN $2 AND $3`, tz, settings.OpeningDate.Time, s.Today().Time).Scan(&net)
	if err != nil {
		return 0, database.MapError(err)
	}
	return settings.OpeningBalanceCents + net, nil
}

func (s *Service) project(ctx context.Context, today billing.Date, days int, balance int64) (*Projection, error) {
	until := today.AddDays(days)
	p := &Projection{Days: days, Until: until}
	// Em aberto até lá (inclusive o que já venceu).
	if err := s.db.QueryRow(ctx, `
		SELECT
			COALESCE((SELECT sum(amount_cents) FROM invoices WHERE status = 'OPEN' AND due_date <= $1), 0)::bigint
			+ COALESCE((SELECT sum(amount_cents) FROM finance_entries
				WHERE kind = 'RECEIVABLE' AND status = 'OPEN' AND due_date <= $1), 0)::bigint,
			COALESCE((SELECT sum(amount_cents) FROM finance_entries
				WHERE kind = 'PAYABLE' AND status = 'OPEN' AND due_date <= $1), 0)::bigint`, until.Time).
		Scan(&p.InCents, &p.OutCents); err != nil {
		return nil, database.MapError(err)
	}

	// As mensalidades que ainda não viraram fatura.
	rows, err := s.db.Query(ctx, `
		SELECT price_cents, promo_price_cents, promo_until, due_day, next_due_date, next_price_cents, next_price_from
		FROM subscriptions WHERE status = 'ACTIVE' AND next_due_date <= $1`, until.Time)
	if err != nil {
		return nil, database.MapError(err)
	}
	for rows.Next() {
		var price int64
		var promo *int64
		var promoUntil, nextFrom *time.Time
		var nextPrice *int64
		var dueDay int16
		var next time.Time
		if err := rows.Scan(&price, &promo, &promoUntil, &dueDay, &next, &nextPrice, &nextFrom); err != nil {
			rows.Close()
			return nil, err
		}
		for d := (billing.Date{Time: next}); !until.Before(d); d = billing.NewDate(d.Year(), d.Month()+1, int(dueDay)) {
			switch {
			case promo != nil && promoUntil != nil && d.Time.Before(*promoUntil):
				p.InCents += *promo
			case nextPrice != nil && nextFrom != nil && !d.Time.Before(*nextFrom):
				// O reajuste anual já agendado.
				p.InCents += *nextPrice
			default:
				p.InCents += price
			}
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// As contas e receitas recorrentes que ainda não foram geradas.
	rows, err = s.db.Query(ctx, `
		SELECT kind, amount_cents, due_day, next_due_date, ends_on FROM finance_recurrences
		WHERE active AND next_due_date <= $1`, until.Time)
	if err != nil {
		return nil, database.MapError(err)
	}
	for rows.Next() {
		var kind string
		var amount int64
		var dueDay int16
		var next time.Time
		var ends *time.Time
		if err := rows.Scan(&kind, &amount, &dueDay, &next, &ends); err != nil {
			rows.Close()
			return nil, err
		}
		for d := (billing.Date{Time: next}); !until.Before(d) && (ends == nil || !ends.Before(d.Time)); d = billing.NewDate(d.Year(), d.Month()+1, int(dueDay)) {
			if kind == KindPayable {
				p.OutCents += amount
			} else {
				p.InCents += amount
			}
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	p.BalanceCents = balance + p.InCents - p.OutCents
	return p, nil
}

// ---------------------------------------------------------------------------
// Resultado do mês (DRE)
// ---------------------------------------------------------------------------

// CategoryAmount é quanto uma categoria somou no mês.
type CategoryAmount struct {
	CategoryID uuid.UUID `json:"categoryId"`
	Name       string    `json:"name"`
	Group      string    `json:"group"`
	Cents      int64     `json:"cents"`
}

// DREMonth é o resultado de um mês pelo que entrou e saiu do caixa, com uma
// diferença: a compra para o estoque não é custo no mês em que é paga — o
// equipamento vira custo quando é instalado (ou vendido), pelo custo médio.
// Aportes, retiradas e empréstimos ficam fora do resultado.
type DREMonth struct {
	Month string `json:"month"`
	// Receita bruta: as faturas pagas dos clientes e as receitas da operação.
	InvoicesCents int64 `json:"invoicesCents"`
	RevenueCents  int64 `json:"revenueCents"`
	TaxesCents    int64 `json:"taxesCents"`
	// Receita líquida = bruta − impostos.
	NetRevenueCents int64 `json:"netRevenueCents"`
	// Custo do serviço: as contas de custo, o equipamento instalado ou
	// vendido e as perdas e acertos do estoque.
	CostsCents     int64 `json:"costsCents"`
	StockCostCents int64 `json:"stockCostCents"`
	StockLossCents int64 `json:"stockLossCents"`
	// Lucro bruto = líquida − custos.
	GrossProfitCents int64 `json:"grossProfitCents"`
	OperatingCents   int64 `json:"operatingCents"`
	FinancialCents   int64 `json:"financialCents"`
	OtherIncomeCents int64 `json:"otherIncomeCents"`
	// Resultado = bruto − operacionais − financeiras + outras receitas.
	ResultCents int64 `json:"resultCents"`
	// Fora do resultado (só caixa).
	InvestmentsCents int64            `json:"investmentsCents"`
	CapitalInCents   int64            `json:"capitalInCents"`
	CapitalOutCents  int64            `json:"capitalOutCents"`
	Categories       []CategoryAmount `json:"categories"`
}

// DRE monta os últimos months meses (com o corrente), do mais antigo ao atual.
func (s *Service) DRE(ctx context.Context, months int) ([]DREMonth, error) {
	if months < 1 || months > 36 {
		months = 12
	}
	today := s.Today()
	first := billing.NewDate(today.Year(), today.Month()-time.Month(months-1), 1)
	end := AddMonths(billing.NewDate(today.Year(), today.Month(), 1), 1).AddDays(-1)

	out := []DREMonth{}
	byMonth := map[string]*DREMonth{}
	for m := first; !end.Before(m); m = AddMonths(m, 1) {
		out = append(out, DREMonth{Month: Month(m), Categories: []CategoryAmount{}})
	}
	for i := range out {
		byMonth[out[i].Month] = &out[i]
	}

	rows, err := s.db.Query(ctx, `
		SELECT to_char((COALESCE(paid_at, updated_at) AT TIME ZONE $1)::date, 'YYYY-MM'), sum(amount_cents)::bigint
		FROM invoices
		WHERE status = 'PAID' AND (COALESCE(paid_at, updated_at) AT TIME ZONE $1)::date BETWEEN $2 AND $3
		GROUP BY 1`, tz, first.Time, end.Time)
	if err != nil {
		return nil, database.MapError(err)
	}
	for rows.Next() {
		var month string
		var cents int64
		if err := rows.Scan(&month, &cents); err != nil {
			rows.Close()
			return nil, err
		}
		if m := byMonth[month]; m != nil {
			m.InvoicesCents = cents
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = s.db.Query(ctx, `
		SELECT to_char(e.paid_on, 'YYYY-MM'), c.id, c.name, c.dre_group, sum(e.paid_cents)::bigint
		FROM finance_entries e JOIN finance_categories c ON c.id = e.category_id
		WHERE e.status = 'PAID' AND e.paid_on BETWEEN $1 AND $2
		GROUP BY 1, 2, 3, 4 ORDER BY 5 DESC`, first.Time, end.Time)
	if err != nil {
		return nil, database.MapError(err)
	}
	for rows.Next() {
		var month string
		var c CategoryAmount
		if err := rows.Scan(&month, &c.CategoryID, &c.Name, &c.Group, &c.Cents); err != nil {
			rows.Close()
			return nil, err
		}
		m := byMonth[month]
		if m == nil {
			continue
		}
		m.Categories = append(m.Categories, c)
		switch c.Group {
		case GroupRevenue:
			m.RevenueCents += c.Cents
		case GroupOtherIncome:
			m.OtherIncomeCents += c.Cents
		case GroupCapitalIn:
			m.CapitalInCents += c.Cents
		case GroupTax:
			m.TaxesCents += c.Cents
		case GroupCost:
			m.CostsCents += c.Cents
		case GroupOperating:
			m.OperatingCents += c.Cents
		case GroupFinancial:
			m.FinancialCents += c.Cents
		case GroupInvestment:
			m.InvestmentsCents += c.Cents
		case GroupCapitalOut:
			m.CapitalOutCents += c.Cents
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// O equipamento que saiu do estoque, pelo custo médio do momento.
	rows, err = s.db.Query(ctx, `
		SELECT to_char(occurred_on, 'YYYY-MM'), type, sum(-quantity::bigint * unit_cost_cents)::bigint
		FROM stock_movements
		WHERE type <> 'IN' AND occurred_on BETWEEN $1 AND $2
		GROUP BY 1, 2`, first.Time, end.Time)
	if err != nil {
		return nil, database.MapError(err)
	}
	for rows.Next() {
		var month, kind string
		var cents int64
		if err := rows.Scan(&month, &kind, &cents); err != nil {
			rows.Close()
			return nil, err
		}
		m := byMonth[month]
		if m == nil {
			continue
		}
		if kind == MoveOut {
			m.StockCostCents += cents
		} else {
			m.StockLossCents += cents // perdas; acerto a mais conta negativo
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range out {
		m := &out[i]
		m.RevenueCents += m.InvoicesCents
		m.NetRevenueCents = m.RevenueCents - m.TaxesCents
		m.GrossProfitCents = m.NetRevenueCents - m.CostsCents - m.StockCostCents - m.StockLossCents
		m.ResultCents = m.GrossProfitCents - m.OperatingCents - m.FinancialCents + m.OtherIncomeCents
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Visão geral
// ---------------------------------------------------------------------------

type Overview struct {
	Today        billing.Date `json:"today"`
	BalanceCents int64        `json:"balanceCents"`
	// Contas a pagar: vencidas, vencendo hoje e nos próximos 7 dias.
	Overdue  Sum `json:"overdue"`
	DueToday Sum `json:"dueToday"`
	DueWeek  Sum `json:"dueWeek"`
	// A receber vencido: faturas dos clientes e receitas avulsas.
	ReceivableOverdue Sum `json:"receivableOverdue"`
	// O mês corrente: caixa e resultado.
	Month           CashMonth    `json:"month"`
	MonthResult     int64        `json:"monthResultCents"`
	Upcoming        []*Entry     `json:"upcoming"`
	LowStock        []*StockItem `json:"lowStock"`
	StockValueCents int64        `json:"stockValueCents"`
	Projections     []Projection `json:"projections"`
}

func (s *Service) Overview(ctx context.Context) (*Overview, error) {
	today := s.Today()
	out := &Overview{Today: today}
	if err := s.db.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE due_date < $1), COALESCE(sum(amount_cents) FILTER (WHERE due_date < $1), 0)::bigint,
			count(*) FILTER (WHERE due_date = $1), COALESCE(sum(amount_cents) FILTER (WHERE due_date = $1), 0)::bigint,
			count(*) FILTER (WHERE due_date > $1), COALESCE(sum(amount_cents) FILTER (WHERE due_date > $1), 0)::bigint
		FROM finance_entries WHERE kind = 'PAYABLE' AND status = 'OPEN' AND due_date <= $2`,
		today.Time, today.AddDays(7).Time).
		Scan(&out.Overdue.Count, &out.Overdue.Cents, &out.DueToday.Count, &out.DueToday.Cents,
			&out.DueWeek.Count, &out.DueWeek.Cents); err != nil {
		return nil, database.MapError(err)
	}
	if err := s.db.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM invoices WHERE status = 'OPEN' AND due_date < $1)
			+ (SELECT count(*) FROM finance_entries WHERE kind = 'RECEIVABLE' AND status = 'OPEN' AND due_date < $1),
			COALESCE((SELECT sum(amount_cents) FROM invoices WHERE status = 'OPEN' AND due_date < $1), 0)::bigint
			+ COALESCE((SELECT sum(amount_cents) FROM finance_entries
				WHERE kind = 'RECEIVABLE' AND status = 'OPEN' AND due_date < $1), 0)::bigint`, today.Time).
		Scan(&out.ReceivableOverdue.Count, &out.ReceivableOverdue.Cents); err != nil {
		return nil, database.MapError(err)
	}

	flow, err := s.CashFlow(ctx, 1)
	if err != nil {
		return nil, err
	}
	out.BalanceCents, out.Projections = flow.BalanceCents, flow.Projections
	if len(flow.Months) > 0 {
		out.Month = flow.Months[len(flow.Months)-1]
	}
	dre, err := s.DRE(ctx, 1)
	if err != nil {
		return nil, err
	}
	if len(dre) > 0 {
		out.MonthResult = dre[len(dre)-1].ResultCents
	}

	week := today.AddDays(7)
	upcoming, err := s.Entries(ctx, EntryFilter{Kind: KindPayable, Status: "open", To: &week})
	if err != nil {
		return nil, err
	}
	if len(upcoming) > 15 {
		upcoming = upcoming[:15]
	}
	out.Upcoming = upcoming

	items, err := s.StockItems(ctx)
	if err != nil {
		return nil, err
	}
	out.LowStock = []*StockItem{}
	for _, item := range items {
		out.StockValueCents += item.ValueCents
		if item.Low {
			out.LowStock = append(out.LowStock, item)
		}
	}
	return out, nil
}
