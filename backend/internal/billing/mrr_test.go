package billing

import (
	"testing"
	"time"
)

func TestComputeMRR(t *testing.T) {
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		t.Fatal(err)
	}
	at := func(y int, m time.Month, d, h, min int) time.Time { return time.Date(y, m, d, h, min, 0, 0, loc) }
	cents := func(v int) *int { return &v }
	date := func(y int, m time.Month, d int) *Date { v := NewDate(y, m, d); return &v }
	canceled := func(s mrrSub, when time.Time) mrrSub { s.canceled, s.isCanceled = when, true; return s }

	subs := []mrrSub{
		// Na promoção o tempo todo.
		{priceCents: 6990, promoCents: cents(3490), promoUntil: date(2027, 10, 1), created: at(2026, 8, 5, 10, 0)},
		// Começou em setembro e foi encerrada em outubro.
		canceled(mrrSub{priceCents: 3990, created: at(2026, 9, 10, 10, 0)}, at(2026, 10, 3, 9, 0)),
		// Grátis: nunca conta.
		{priceCents: 0, created: at(2026, 7, 1, 10, 0)},
		// A promoção acabou em 10/10: em outubro sobe para o preço cheio.
		{priceCents: 6990, promoCents: cents(3490), promoUntil: date(2026, 10, 10), created: at(2025, 10, 1, 10, 0)},
		// Nova em outubro.
		{priceCents: 4990, created: at(2026, 10, 15, 10, 0)},
		// 1º de outubro, 00:30 em Brasília (03:30 UTC): é de outubro.
		{priceCents: 2000, created: at(2026, 10, 1, 0, 30)},
		// Encerrada em 30/09 às 23:30 em Brasília (já 1º/10 em UTC): é de setembro.
		canceled(mrrSub{priceCents: 1000, created: at(2026, 8, 1, 10, 0)}, at(2026, 9, 30, 23, 30)),
	}
	got := computeMRR(subs, at(2026, 10, 16, 12, 0), loc, 3)

	want := []MRRMonth{
		{Month: "2026-10", MRRCents: 17470, Subscriptions: 4, NewCents: 6990, NewCount: 2, ChurnCents: 3990, ChurnCount: 1,
			ChangeCents: 6500, OtherCents: 3500},
		{Month: "2026-09", MRRCents: 10970, Subscriptions: 3, NewCents: 3990, NewCount: 1, ChurnCents: 1000, ChurnCount: 1,
			ChangeCents: 2990, OtherCents: 0},
		{Month: "2026-08", MRRCents: 7980, Subscriptions: 3, NewCents: 4490, NewCount: 2, ChangeCents: 4490},
	}
	if len(got.Months) != len(want) {
		t.Fatalf("meses = %+v", got.Months)
	}
	for i := range want {
		if got.Months[i] != want[i] {
			t.Errorf("mês %d = %+v, quer %+v", i, got.Months[i], want[i])
		}
	}
	if got.CurrentCents != 17470 || got.Subscriptions != 4 {
		t.Errorf("hoje = %d (%d assinaturas)", got.CurrentCents, got.Subscriptions)
	}
}
