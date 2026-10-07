package mail

import (
	"context"
	"strings"
	"testing"
	"time"
)

// O aviso do reajuste: o índice, o período, a data e cada mensalidade (de →
// para); o cancelamento; o resumo dos admins (com e sem reajuste); e o aviso
// do contrato novo.
func TestPriceAdjustmentMails(t *testing.T) {
	sender := &captureSender{}
	m := NewPriceAdjustmentMailer(sender, "https://painel.farbo.test")
	ctx := context.Background()
	n := PriceAdjustmentNotice{
		Year: 2027, Rate: "4,91%", Period: "junho de 2026 a maio de 2027", EffectiveFrom: time.Date(2027, 8, 1, 0, 0, 0, 0, time.UTC),
		Lines: []PriceAdjustmentLine{{Vehicle: "Moto da Ana", Plan: "Plano Mensal", OldCents: 6990, NewCents: 7333}},
	}
	if err := m.Notice(ctx, "ana@farbo.test", "Ana Lima", n); err != nil {
		t.Fatal(err)
	}
	msg := sender.last
	for _, want := range []string{"4,91%", "junho de 2026 a maio de 2027", "01/08/2027", "Moto da Ana (Plano Mensal)", "R$ 69,90 → R$ 73,33", "cláusula 5"} {
		if !strings.Contains(msg.Text, want) {
			t.Errorf("aviso sem %q:\n%s", want, msg.Text)
		}
	}
	if !strings.Contains(msg.HTML, "R$ 73,33") || !strings.Contains(msg.HTML, "Olá, Ana!") || !strings.Contains(msg.Subject, "Reajuste anual") {
		t.Errorf("aviso (HTML) = %s / %s", msg.Subject, msg.HTML)
	}

	if err := m.Canceled(ctx, "ana@farbo.test", "Ana", n); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sender.last.Text, "não vai acontecer") || !strings.Contains(sender.last.Text, "R$ 69,90") ||
		strings.Contains(sender.last.Text, "R$ 73,33") {
		t.Errorf("cancelamento = %s", sender.last.Text)
	}

	s := PriceAdjustmentSummary{Year: 2027, Rate: "4,91%", Period: n.Period, EffectiveFrom: n.EffectiveFrom,
		CancelUntil: time.Date(2027, 7, 21, 0, 0, 0, 0, time.UTC), Customers: 3, Subscriptions: 4, MonthlyDiffCents: 1372}
	if err := m.Summary(ctx, "admin@farbo.test", s); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sender.last.Text, "3 clientes (4 mensalidades)") || !strings.Contains(sender.last.Text, "R$ 13,72") ||
		!strings.Contains(sender.last.Text, "até 21/07/2027") {
		t.Errorf("resumo = %s", sender.last.Text)
	}
	s.NoChange, s.Rate = true, "-0,31%"
	if err := m.Summary(ctx, "admin@farbo.test", s); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sender.last.Subject, "Sem reajuste em 2027") || !strings.Contains(sender.last.Text, "ficam como estão") {
		t.Errorf("sem reajuste = %s / %s", sender.last.Subject, sender.last.Text)
	}

	c := NewContractMailer(sender, "https://painel.farbo.test")
	if err := c.ContractUpdated(ctx, "ana@farbo.test", "Ana", ContractUpdate{Title: "Contrato", Version: "2",
		EffectiveDate: "7 de outubro de 2026", Changes: "Entrou o reajuste anual."}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sender.last.Text, "Entrou o reajuste anual.") || !strings.Contains(sender.last.Text, "sem multa") ||
		!strings.Contains(sender.last.HTML, "https://painel.farbo.test/contrato") {
		t.Errorf("contrato novo = %s", sender.last.Text)
	}
}
