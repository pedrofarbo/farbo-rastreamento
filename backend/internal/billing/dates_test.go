package billing

import (
	"encoding/json"
	"testing"
	"time"
)

func TestFirstDueDate(t *testing.T) {
	cases := []struct {
		name   string
		today  Date
		dueDay int
		want   Date
	}{
		{"vencimento ainda neste mês", NewDate(2026, 9, 5), 10, NewDate(2026, 9, 10)},
		{"vence hoje", NewDate(2026, 9, 10), 10, NewDate(2026, 9, 10)},
		{"dia já passou: mês seguinte", NewDate(2026, 9, 15), 10, NewDate(2026, 10, 10)},
		{"virada de ano", NewDate(2026, 12, 20), 5, NewDate(2027, 1, 5)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := firstDueDate(tc.today, tc.dueDay); got != tc.want {
				t.Fatalf("firstDueDate(%s, %d) = %s, esperado %s", tc.today, tc.dueDay, got, tc.want)
			}
		})
	}
}

func TestNextMonthlyKeepsDayAndCrossesYear(t *testing.T) {
	due := NewDate(2026, 11, 28)
	due = nextMonthly(due, 28)
	if due != NewDate(2026, 12, 28) {
		t.Fatalf("novembro → %s, esperado 2026-12-28", due)
	}
	due = nextMonthly(due, 28)
	if due != NewDate(2027, 1, 28) {
		t.Fatalf("dezembro → %s, esperado 2027-01-28", due)
	}
	// 28 existe inclusive em fevereiro: o vencimento nunca escorrega de mês.
	if feb := nextMonthly(due, 28); feb != NewDate(2027, 2, 28) {
		t.Fatalf("janeiro → %s, esperado 2027-02-28", feb)
	}
}

func TestDateInUsesBillingTimezone(t *testing.T) {
	saoPaulo, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		t.Fatal(err)
	}
	// 01:30 UTC do dia 11 ainda é 22:30 do dia 10 em Brasília: a fatura que
	// vence dia 10 não pode aparecer como vencida nesse horário.
	instant := time.Date(2026, 10, 11, 1, 30, 0, 0, time.UTC)
	if got := DateIn(instant, saoPaulo); got != NewDate(2026, 10, 10) {
		t.Fatalf("DateIn = %s, esperado 2026-10-10", got)
	}
}

func TestDateJSONHasNoTime(t *testing.T) {
	raw, err := json.Marshal(struct {
		Due Date `json:"due"`
	}{NewDate(2026, 10, 10)})
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"due":"2026-10-10"}` {
		t.Fatalf("JSON = %s", raw)
	}

	var decoded struct {
		Due Date `json:"due"`
	}
	if err := json.Unmarshal([]byte(`{"due":"2026-02-28"}`), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Due != NewDate(2026, 2, 28) {
		t.Fatalf("decodificado = %s", decoded.Due)
	}
	if err := json.Unmarshal([]byte(`{"due":"28/02/2026"}`), &decoded); err == nil {
		t.Fatal("data fora do formato AAAA-MM-DD deveria ser recusada")
	}
}

func TestInvoiceDescription(t *testing.T) {
	if got := invoiceDescription("Plano Mensal", NewDate(2026, 3, 10)); got != "Plano Mensal — março/2026" {
		t.Fatalf("descrição = %q", got)
	}
}

func TestSuspensionCutoffAndOverdueDecoration(t *testing.T) {
	svc := &Service{
		cfg: configWithSuspension(10),
		loc: time.UTC,
		now: func() time.Time { return time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC) },
	}
	// Hoje é dia 12: com 10 dias de tolerância, suspende quem tem fatura em
	// aberto que venceu antes do dia 2 (a do dia 1, por exemplo).
	if cutoff := svc.suspensionCutoff(); cutoff != NewDate(2026, 10, 2) {
		t.Fatalf("corte = %s, esperado 2026-10-02", cutoff)
	}

	open := svc.decorate(&Invoice{Status: InvoiceOpen, DueDate: NewDate(2026, 10, 1)})
	if !open.Overdue || open.DaysOverdue != 11 {
		t.Fatalf("fatura do dia 1: overdue=%v dias=%d, esperado true/11", open.Overdue, open.DaysOverdue)
	}
	dueToday := svc.decorate(&Invoice{Status: InvoiceOpen, DueDate: NewDate(2026, 10, 12)})
	if dueToday.Overdue {
		t.Fatal("fatura que vence hoje ainda não está vencida")
	}
	paid := svc.decorate(&Invoice{Status: InvoicePaid, DueDate: NewDate(2026, 9, 1)})
	if paid.Overdue {
		t.Fatal("fatura paga nunca aparece como vencida")
	}
}

func TestValidatePaymentRejectsNonHTTPLinks(t *testing.T) {
	for _, bad := range []string{"javascript:alert(1)", "ftp://x.com/a", "pagamento.com", "https://"} {
		if _, _, err := validatePayment(bad, ""); err == nil {
			t.Errorf("link %q deveria ser recusado", bad)
		}
	}
	link, pix, err := validatePayment("  https://pag.exemplo.com/f/123  ", " 000201pix ")
	if err != nil || link != "https://pag.exemplo.com/f/123" || pix != "000201pix" {
		t.Fatalf("link=%q pix=%q err=%v", link, pix, err)
	}
}

func TestPriceOnLaunchPromo(t *testing.T) {
	day := func(s string) Date {
		d, err := time.Parse(time.DateOnly, s)
		if err != nil {
			t.Fatal(err)
		}
		return Date{d}
	}
	promo, until := 2990, day("2027-10-10")
	cases := map[string]int{
		"2026-10-10": 2990, // primeira mensalidade
		"2027-09-10": 2990, // 12ª
		"2027-10-10": 6990, // 13ª: o plano
		"2028-01-10": 6990,
	}
	for due, want := range cases {
		if got := priceOn(6990, &promo, &until, day(due)); got != want {
			t.Errorf("vencimento %s: %d, quer %d", due, got, want)
		}
	}
	if got := priceOn(6990, nil, nil, day("2026-10-10")); got != 6990 {
		t.Errorf("sem promoção: %d", got)
	}
}
