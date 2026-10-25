package billing

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAssembleMRR(t *testing.T) {
	billed := map[string]mrrBill{
		"2026-08": {monthly: 3490, monthlies: 1, newCents: 3490, newCount: 1},
		"2026-09": {monthly: 7480, monthlies: 2, newCents: 3990, newCount: 1, parcels: 1200, parcelsPaid: 1200},
		// Outubro, só as faturas já geradas.
		"2026-10": {monthly: 2790, monthlies: 1, newCents: 2790, newCount: 1, parcels: 6000},
	}
	// As associações: setembro com 10 veículos; outubro, a fatura que ainda vai sair, com 12.
	orgs := map[string]mrrOrg{"2026-09": {cents: 30000, vehicles: 10}}
	// Outubro inteiro: com a que ainda vai sair (R$ 34,90, com a parcela de R$ 12,00).
	current := MRRForecast{Month: "2026-10", MonthlyCents: 6280, Monthlies: 2, InstallmentsCents: 7200,
		OrganizationsCents: 36000, OrganizationVehicles: 12}
	churn := map[string]mrrChurn{"2026-10": {cents: 3990, count: 1}}

	got := assembleMRR(billed, orgs, current, churn, NewDate(2026, 10, 1), 3)
	want := []MRRMonth{
		{Month: "2026-10", MRRCents: 6280, Subscriptions: 2, NewCents: 2790, NewCount: 1, ChurnCents: 3990, ChurnCount: 1,
			ChangeCents: -1200, OtherCents: 0, OrganizationsCents: 36000, OrganizationVehicles: 12, OrganizationsChangeCents: 6000,
			InstallmentsCents: 7200, TotalCents: 49480},
		{Month: "2026-09", MRRCents: 7480, Subscriptions: 2, NewCents: 3990, NewCount: 1, ChangeCents: 3990,
			OrganizationsCents: 30000, OrganizationVehicles: 10, OrganizationsChangeCents: 30000,
			InstallmentsCents: 1200, InstallmentsPaidCents: 1200, TotalCents: 38680},
		{Month: "2026-08", MRRCents: 3490, Subscriptions: 1, NewCents: 3490, NewCount: 1, ChangeCents: 3490, TotalCents: 3490},
	}
	if len(got.Months) != len(want) {
		t.Fatalf("meses = %+v", got.Months)
	}
	for i := range want {
		if got.Months[i] != want[i] {
			t.Errorf("mês %d = %+v, quer %+v", i, got.Months[i], want[i])
		}
	}
	if got.CurrentCents != 6280 || got.Subscriptions != 2 || got.InstallmentsCents != 7200 || got.TotalCents != 49480 ||
		got.OrganizationsCents != 36000 || got.OrganizationVehicles != 12 {
		t.Errorf("o mês atual = %+v", got)
	}
}

// A mensalidade das associações no mês: a da fatura que já saiu; a que ainda
// não saiu, pelos veículos que contam hoje no preço, só da ativa, com preço e
// a partir do 1º vencimento.
func TestComputeOrgMonth(t *testing.T) {
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		t.Fatal(err)
	}
	created := time.Date(2026, 9, 1, 10, 0, 0, 0, loc)
	got := computeOrgMonth([]orgMonthRow{
		// A fatura de outubro já saiu (com os veículos do dia dela).
		{active: true, priceCents: 3000, dueDay: 10, created: created, counting: 99, invoiced: true, invoiceCents: 30000, invoiceVehicles: 10},
		// Ainda não saiu: 4 veículos de R$ 25,00.
		{active: true, priceCents: 2500, dueDay: 20, created: created, counting: 4},
		// Desativada, parceria (sem preço), sem veículos: nada.
		{active: false, priceCents: 3000, dueDay: 20, created: created, counting: 5},
		{active: true, priceCents: 0, dueDay: 20, created: created, counting: 5},
		{active: true, priceCents: 3000, dueDay: 20, created: created},
		// Criada em 15/10 com vencimento no dia 10: a 1ª fatura é a de novembro.
		{active: true, priceCents: 3000, dueDay: 10, created: time.Date(2026, 10, 15, 9, 0, 0, 0, loc), counting: 3},
		// Criada em 15/10 com vencimento no dia 20: já vence em outubro.
		{active: true, priceCents: 1000, dueDay: 20, created: time.Date(2026, 10, 15, 9, 0, 0, 0, loc), counting: 2},
	}, NewDate(2026, 10, 1), loc)
	if got != (mrrOrg{cents: 30000 + 10000 + 2000, vehicles: 16}) {
		t.Errorf("associações = %+v", got)
	}
}

func TestChurnByMonth(t *testing.T) {
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		t.Fatal(err)
	}
	promo := 2790
	until := NewDate(2027, 10, 1)
	got := churnByMonth([]mrrSub{
		// 30/09 às 23:30 em Brasília (já 1º/10 em UTC): é de setembro, pelo preço da promoção.
		{priceCents: 3990, promoCents: &promo, promoUntil: &until, isCanceled: true, canceled: time.Date(2026, 9, 30, 23, 30, 0, 0, loc)},
		{priceCents: 4990, isCanceled: true, canceled: time.Date(2026, 10, 3, 9, 0, 0, 0, loc)},
		{priceCents: 0, isCanceled: true, canceled: time.Date(2026, 10, 4, 9, 0, 0, 0, loc)}, // grátis
		{priceCents: 4990}, // ativa
	}, loc)
	if len(got) != 2 || got["2026-09"] != (mrrChurn{cents: 2790, count: 1}) || got["2026-10"] != (mrrChurn{cents: 4990, count: 1}) {
		t.Errorf("encerradas = %+v", got)
	}
}

func TestComputeForecast(t *testing.T) {
	cents := func(v int) *int { return &v }
	date := func(y int, m time.Month, d int) Date { return NewDate(y, m, d) }
	until := func(y int, m time.Month, d int) *Date { v := date(y, m, d); return &v }
	parcels := func(n int, v int64) []int64 {
		out := make([]int64, n)
		for i := range out {
			out[i] = v
		}
		return out
	}
	insanos := func(next Date, day int, pending []int64) forecastSub {
		return forecastSub{id: uuid.New(), dueDay: day, next: next, pending: pending,
			mrrSub: mrrSub{priceCents: 3990, promoCents: cents(2790), promoUntil: until(2027, 10, 14)}}
	}
	victor2 := insanos(date(2026, 12, 20), 20, []int64{6000})
	subs := []forecastSub{
		insanos(date(2026, 11, 20), 20, parcels(9, 1200)), // Anderson
		insanos(date(2026, 11, 10), 10, parcels(9, 1200)), // a 1ª moto do Jair
		insanos(date(2026, 12, 10), 10, parcels(9, 1200)), // as outras duas: a 2ª só em dezembro
		insanos(date(2026, 12, 10), 10, parcels(9, 1200)),
		{id: uuid.New(), dueDay: 10, next: date(2026, 11, 10), mrrSub: mrrSub{priceCents: 0}}, // grátis
		insanos(date(2026, 11, 20), 20, parcels(9, 1200)),                                     // Pedro Mauricio
		insanos(date(2026, 11, 20), 20, []int64{6000}),                                        // a 1ª do Victor
		victor2, // a 2ª: a de novembro já saiu
		// Ainda vai sair a de 28/10 (com a 1ª parcela); a de novembro leva a 2ª.
		{id: uuid.New(), dueDay: 28, next: date(2026, 10, 28), pending: []int64{500, 400}, mrrSub: mrrSub{priceCents: 4990}},
	}
	// Já gerada no mesmo dia em que a simulação cairia: não conta duas vezes.
	dup := insanos(date(2026, 11, 5), 5, parcels(2, 300))
	subs = append(subs, dup)
	generated := []forecastInvoice{
		{subscription: victor2.id, due: date(2026, 11, 20), monthly: 2790, installment: 6000},
		{subscription: dup.id, due: date(2026, 11, 5), monthly: 2790, installment: 300},
	}

	got := computeForecast(subs, generated, date(2026, 11, 1), date(2026, 12, 1))
	want := MRRForecast{Month: "2026-11", MonthlyCents: 2790*6 + 4990, Monthlies: 7,
		InstallmentsCents: 1200*3 + 6000*2 + 400 + 300, TotalCents: 2790*6 + 4990 + 1200*3 + 6000*2 + 400 + 300}
	if got != want {
		t.Errorf("previsão = %+v, quer %+v", got, want)
	}
}
