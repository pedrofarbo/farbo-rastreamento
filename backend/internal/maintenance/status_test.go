package maintenance

import (
	"testing"
	"time"
)

func ptr[T any](v T) *T { return &v }

// A situação de cada manutenção: o que vier primeiro (km, horas de motor ou
// data), "perto" a 10% do intervalo (no mínimo 500 km ou 25 h) ou a 15
// dias, e sem hodômetro o km não conta (as horas sempre contam).
func TestEvaluate(t *testing.T) {
	today := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		label    string
		item     Item
		odometer *float64
		hours    float64
		status   string
		detail   string
	}{
		{"em dia", Item{EveryKm: ptr(10000), EveryMonths: ptr(6), LastKm: ptr(40000.0), LastDate: "2026-08-01"},
			ptr(43000.0), 0, StatusOK, "faltam 7.000 km · faltam 112 dias"},
		{"perto pelo km (10%)", Item{EveryKm: ptr(10000), LastKm: ptr(40000.0), LastDate: "2026-08-01"},
			ptr(49100.0), 0, StatusSoon, "faltam 900 km"},
		{"perto pelo km (mínimo de 500)", Item{EveryKm: ptr(2000), LastKm: ptr(1000.0), LastDate: "2026-08-01"},
			ptr(2600.0), 0, StatusSoon, "faltam 400 km"},
		{"vencida pelo km", Item{EveryKm: ptr(10000), EveryMonths: ptr(12), LastKm: ptr(40000.0), LastDate: "2026-08-01"},
			ptr(50320.0), 0, StatusDue, "passou 320 km · faltam 293 dias"},
		{"perto pela data", Item{EveryMonths: ptr(6), LastDate: "2026-04-20"}, nil, 0, StatusSoon, "faltam 8 dias"},
		{"vence hoje", Item{EveryMonths: ptr(6), LastDate: "2026-04-12"}, nil, 0, StatusDue, "vence hoje"},
		{"vencida pela data", Item{EveryMonths: ptr(1), LastDate: "2026-09-01"}, nil, 0, StatusDue, "venceu há 11 dias"},
		{"só km, sem hodômetro", Item{EveryKm: ptr(5000), LastDate: "2026-09-01"}, nil, 0, StatusUnknown, ""},
		{"km e data, sem hodômetro: vale a data", Item{EveryKm: ptr(5000), EveryMonths: ptr(1), LastDate: "2026-09-01"},
			nil, 0, StatusDue, "venceu há 11 dias"},
		{"perto pelas horas (10%)", Item{EveryHours: ptr(500), LastHours: ptr(1000.0), LastDate: "2026-08-01"},
			nil, 1460, StatusSoon, "faltam 40 h de motor"},
		{"perto pelas horas (mínimo de 25)", Item{EveryHours: ptr(100), LastHours: ptr(0.0), LastDate: "2026-08-01"},
			nil, 80, StatusSoon, "faltam 20 h de motor"},
		{"vencida pelas horas, a data em dia", Item{EveryHours: ptr(250), EveryMonths: ptr(12), LastHours: ptr(100.0), LastDate: "2026-08-01"},
			nil, 365.4, StatusDue, "passou 15 h de motor · faltam 293 dias"},
		{"km e horas, sem hodômetro: valem as horas", Item{EveryKm: ptr(5000), EveryHours: ptr(250), LastHours: ptr(0.0), LastDate: "2026-09-01"},
			nil, 100, StatusOK, "faltam 150 h de motor"},
		{"só horas, sem as da última vez", Item{EveryHours: ptr(250), LastDate: "2026-09-01"}, nil, 100, StatusUnknown, ""},
	}
	for _, c := range cases {
		it := c.item
		it.evaluate(Odometer{Km: c.odometer, Hours: c.hours}, today)
		if it.Status != c.status || Describe(&it) != c.detail {
			t.Errorf("%s: %s %q (quer %s %q)", c.label, it.Status, Describe(&it), c.status, c.detail)
		}
	}
}

// As vencidas primeiro, depois as perto, as sem hodômetro e as em dia;
// dentro de cada uma, a que vence antes.
func TestSortByUrgency(t *testing.T) {
	list := []*Item{
		{Name: "em dia", Status: StatusOK, RemainingDays: ptr(90)},
		{Name: "perto, 10 dias", Status: StatusSoon, RemainingDays: ptr(10)},
		{Name: "sem hodômetro", Status: StatusUnknown},
		{Name: "vencida há 1 dia", Status: StatusDue, RemainingDays: ptr(-1)},
		{Name: "perto, 300 km", Status: StatusSoon, RemainingKm: ptr(300.0)},
		{Name: "perto, 40 h", Status: StatusSoon, RemainingHours: ptr(40.0)},
		{Name: "vencida há 2.000 km", Status: StatusDue, RemainingKm: ptr(-2000.0)},
	}
	SortByUrgency(list)
	want := []string{"vencida há 2.000 km", "vencida há 1 dia", "perto, 300 km", "perto, 40 h", "perto, 10 dias", "sem hodômetro", "em dia"}
	for i, it := range list {
		if it.Name != want[i] {
			t.Fatalf("ordem: %d é %q (quer %q)", i, it.Name, want[i])
		}
	}
}
