package mail

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// Cada etapa da régua: o assunto, a manchete e o link de pagamento no corpo.
func TestInvoiceReminderTexts(t *testing.T) {
	one := []InvoiceLine{{Description: "Plano Mensal — outubro/2026", AmountCents: 6990, DueDate: "10/10/2026", PayURL: "https://farbo.test/pagar/abc"}}
	two := append([]InvoiceLine{{Description: "Plano Mensal — Carro", AmountCents: 123456, DueDate: "10/10/2026", PayURL: "https://farbo.test/pagar/def"}}, one...)
	for _, tc := range []struct {
		r       InvoiceReminder
		subject string
		body    string
	}{
		{InvoiceReminder{Kind: ReminderIssued, Invoices: one}, "Sua fatura de R$ 69,90 está disponível", "vence em 10/10/2026"},
		{InvoiceReminder{Kind: ReminderDueSoon, Invoices: two, DaysToDue: 3}, "Suas 2 faturas vencem em 3 dias: R$ 1.304,46", "Plano Mensal — Carro"},
		{InvoiceReminder{Kind: ReminderDueToday, Invoices: one}, "Sua fatura vence hoje: R$ 69,90", "Pague agora com Pix"},
		{InvoiceReminder{Kind: ReminderOverdue, Invoices: one, DaysLate: 3}, "Fatura em atraso: R$ 69,90", "vencida há 3 dias"},
		{InvoiceReminder{Kind: ReminderSuspensionSoon, Invoices: one, DaysLate: 9, SuspendIn: 2, SuspendOn: "21/10/2026"},
			"Seu acesso ao rastreamento será suspenso em 2 dias", "a partir de 21/10/2026"},
		{InvoiceReminder{Kind: ReminderSuspensionSoon, Invoices: one, DaysLate: 10, SuspendIn: 1}, "será suspenso amanhã", "vencida há 10 dias"},
		{InvoiceReminder{Kind: ReminderManual, Invoices: one, DaysLate: 6}, "Lembrete: fatura em atraso de R$ 69,90", "vencida há 6 dias"},
		{InvoiceReminder{Kind: ReminderManual, Invoices: one, DaysToDue: 5}, "Lembrete: sua fatura de R$ 69,90", "está em aberto"},
		{InvoiceReminder{Kind: ReminderManual, Invoices: one}, "Lembrete: sua fatura vence hoje", "Pague agora"},
		{InvoiceReminder{Kind: ReminderOverdue, Invoices: two, DaysLate: 3}, "Faturas em atraso: R$ 1.304,46", "Suas 2 faturas estão vencidas há 3 dias"},
	} {
		sender := &captureSender{}
		if err := NewInvoiceMailer(sender, "https://farbo.test").Reminder(context.Background(), "lia@x.test", "Lia Souza", tc.r); err != nil {
			t.Fatal(err)
		}
		m := sender.last
		if !strings.Contains(m.Subject, tc.subject) {
			t.Errorf("%s: assunto %q, quer %q", tc.r.Kind, m.Subject, tc.subject)
		}
		// Uma fatura: o link dela; duas no mesmo dia: um link só, o de todas.
		link := "https://farbo.test/pagar/abc"
		if len(tc.r.Invoices) > 1 {
			link = "https://farbo.test/pagar/def?todas=1"
		}
		for _, part := range []string{m.Text, m.HTML} {
			if !strings.Contains(part, tc.body) || !strings.Contains(part, link) || !strings.Contains(part, "Olá, Lia!") {
				t.Errorf("%s: corpo sem %q, o link %s ou a saudação:\n%s", tc.r.Kind, tc.body, link, part)
			}
		}
	}
	title, body := InvoiceReminder{Kind: ReminderDueToday, Invoices: one}.PushText()
	if title != "Sua fatura vence hoje" || body != "R$ 69,90 · toque para pagar com Pix." {
		t.Errorf("push = %q / %q", title, body)
	}
}

// A fatura do mês de uma frota: um cartão por vencimento com o total e um
// botão só, algumas linhas e "e mais"; muitos vencimentos, o resto no app.
func TestInvoiceReminderFleet(t *testing.T) {
	var lines []InvoiceLine
	for i := range 1000 {
		lines = append(lines, InvoiceLine{Description: fmt.Sprintf("Plano Frota — Van %04d", i+1), AmountCents: 4990,
			DueDate: "13/11/2026", PayURL: fmt.Sprintf("https://farbo.test/pagar/v%d", i+1)})
	}
	// Mais 7 vencimentos com uma fatura cada: 6 dias aparecem, 2 ficam no app.
	for d := range 7 {
		lines = append(lines, InvoiceLine{Description: fmt.Sprintf("Plano Mensal — Moto %d", d+1), AmountCents: 6990,
			DueDate: fmt.Sprintf("%02d/11/2026", 14+d), PayURL: fmt.Sprintf("https://farbo.test/pagar/m%d", d+1)})
	}
	sender := &captureSender{}
	if err := NewInvoiceMailer(sender, "https://farbo.test").Reminder(context.Background(), "frota@x.test", "Frota Ltda",
		InvoiceReminder{Kind: ReminderIssued, Invoices: lines}); err != nil {
		t.Fatal(err)
	}
	m := sender.last
	for name, part := range map[string]string{"texto": m.Text, "HTML": m.HTML} {
		for _, want := range []string{
			"1000 faturas", "R$ 49.900,00", "https://farbo.test/pagar/v1?todas=1", "Plano Frota — Van 0010",
			"e mais 990 faturas", "https://farbo.test/pagar/m5", "E mais 2 faturas em outros vencimentos", "https://farbo.test/app/faturas",
		} {
			if !strings.Contains(part, want) {
				t.Errorf("%s sem %q", name, want)
			}
		}
		for _, not := range []string{"Van 0011", "pagar/v2", "Moto 6", "pagar/m6"} {
			if strings.Contains(part, not) {
				t.Errorf("%s não devia ter %q", name, not)
			}
		}
	}
	if len(m.HTML) > 60_000 {
		t.Errorf("e-mail de %d bytes (o Gmail corta acima de ~100 KB)", len(m.HTML))
	}
}
