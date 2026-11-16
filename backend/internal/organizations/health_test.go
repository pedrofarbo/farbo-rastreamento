package organizations

import (
	"testing"
	"time"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
)

func TestIssueOf(t *testing.T) {
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	ago := func(d time.Duration) *time.Time { v := now.Add(-d); return &v }
	cases := []struct {
		name      string
		seen, cut *time.Time
		want      string
	}{
		{"em dia", ago(time.Minute), nil, ""},
		{"nunca", nil, nil, IssueNever},
		{"um dia", ago(25 * time.Hour), nil, IssueSilent},
		{"três dias", ago(73 * time.Hour), nil, IssueSilentLong},
		{"corte e silêncio", ago(3 * time.Hour), ago(5 * time.Hour), IssueRemoved},
		{"corte e silêncio longo", ago(80 * time.Hour), ago(85 * time.Hour), IssueRemoved},
		{"corte que voltou", ago(time.Minute), ago(26 * time.Hour), IssuePowerCut},
		{"corte, ainda na bateria interna", ago(30 * time.Minute), ago(time.Hour), IssuePowerCut},
		{"silêncio que veio muito depois do corte", ago(30 * time.Hour), ago(6 * 24 * time.Hour), IssueSilent},
	}
	for _, c := range cases {
		if got := issueOf(now, c.seen, c.cut); got != c.want {
			t.Errorf("%s: %q, quero %q", c.name, got, c.want)
		}
	}
}

func TestInvoiceDescription(t *testing.T) {
	due := billing.NewDate(2026, time.November, 10)
	monthly := Item{Kind: ItemMonthly}
	inspection := Item{Kind: ItemInspection}
	if got := invoiceDescription(due, 2, []Item{monthly, monthly, inspection}); got != "Rastreamento — novembro/2026 · 2 veículos · 1 vistoria" {
		t.Errorf("com veículos = %q", got)
	}
	if got := invoiceDescription(due, 0, []Item{inspection, inspection}); got != "Vistorias — novembro/2026 · 2 vistorias" {
		t.Errorf("só vistorias = %q", got)
	}
	if got := invoiceDescription(due, 1, []Item{monthly}); got != "Rastreamento — novembro/2026 · 1 veículo" {
		t.Errorf("sem vistorias = %q", got)
	}
}
