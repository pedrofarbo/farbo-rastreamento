package mail

import (
	"context"
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
		for _, part := range []string{m.Text, m.HTML} {
			if !strings.Contains(part, tc.body) || !strings.Contains(part, "https://farbo.test/pagar/abc") || !strings.Contains(part, "Olá, Lia!") {
				t.Errorf("%s: corpo sem %q, o link ou a saudação:\n%s", tc.r.Kind, tc.body, part)
			}
		}
	}
	title, body := InvoiceReminder{Kind: ReminderDueToday, Invoices: one}.PushText()
	if title != "Sua fatura vence hoje" || body != "R$ 69,90 · toque para pagar com Pix." {
		t.Errorf("push = %q / %q", title, body)
	}
}
