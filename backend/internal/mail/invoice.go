package mail

import (
	"bytes"
	"context"
	"fmt"
	"strings"
)

// InvoiceMailer escreve os lembretes de fatura (a régua de cobrança): cada
// fatura com o botão que abre o Pix dela, sem precisar entrar no painel.
type InvoiceMailer struct {
	sender Sender
	appURL string
}

func NewInvoiceMailer(sender Sender, appURL string) *InvoiceMailer {
	return &InvoiceMailer{sender: sender, appURL: strings.TrimRight(appURL, "/")}
}

// As etapas da régua (a mesma lista do pacote dunning).
const (
	ReminderIssued         = "ISSUED"
	ReminderDueSoon        = "DUE_SOON"
	ReminderDueToday       = "DUE_TODAY"
	ReminderOverdue        = "OVERDUE"
	ReminderSuspensionSoon = "SUSPENSION_SOON"
	ReminderManual         = "MANUAL"
)

// InvoiceLine é uma fatura no lembrete.
type InvoiceLine struct {
	Description string
	AmountCents int
	// DueDate já formatada (10/10/2026).
	DueDate string
	// PayURL abre o Pix da fatura, sem login.
	PayURL string
}

// InvoiceReminder é o lembrete de uma ou mais faturas do mesmo cliente.
type InvoiceReminder struct {
	Kind     string
	Invoices []InvoiceLine
	// DaysToDue: faltam tantos dias (DUE_SOON); DaysLate: venceu há tantos
	// (OVERDUE, SUSPENSION_SOON e o manual de fatura vencida).
	DaysToDue int
	DaysLate  int
	// SuspendOn: o dia em que o acesso é suspenso (SUSPENSION_SOON), já
	// formatado; SuspendIn, em quantos dias.
	SuspendOn string
	SuspendIn int
}

type invoiceData struct {
	InvoiceReminder
	Name     string
	Kicker   string
	Headline string
	Lead     string
	Total    string
	Lines    []invoiceLineData
	Danger   bool
	AppURL   string
}

type invoiceLineData struct {
	InvoiceLine
	Amount string
}

// brl formata centavos: 6990 → R$ 69,90; 123456 → R$ 1.234,56.
func brl(cents int) string {
	sign := ""
	if cents < 0 {
		sign, cents = "-", -cents
	}
	reais := cents / 100
	var groups []string
	for reais >= 1000 {
		groups = append([]string{fmt.Sprintf("%03d", reais%1000)}, groups...)
		reais /= 1000
	}
	groups = append([]string{fmt.Sprint(reais)}, groups...)
	return fmt.Sprintf("%sR$ %s,%02d", sign, strings.Join(groups, "."), cents%100)
}

func days(n int) string { return plural(n, "dia", "dias") }

// texts escreve o assunto, a manchete e a frase de cada etapa (no singular
// ou no plural, conforme quantas faturas).
func (r InvoiceReminder) texts(total string) (subject, kicker, headline, lead string, danger bool) {
	one := len(r.Invoices) == 1
	pick := func(singular, plural string) string {
		if one {
			return singular
		}
		return plural
	}
	sua := pick("Sua fatura", fmt.Sprintf("Suas %d faturas", len(r.Invoices)))
	lower := strings.ToLower(sua[:1]) + sua[1:]
	vence, esta := pick("vence", "vencem"), pick("está", "estão")
	vencida, a := pick("vencida", "vencidas"), pick("A fatura", "As faturas")
	due := ""
	if one {
		due = r.Invoices[0].DueDate
	}
	switch r.Kind {
	case ReminderIssued:
		return fmt.Sprintf("%s de %s %s disponível", sua, total, esta), "Nova fatura",
			fmt.Sprintf("%s de %s já %s", sua, total, pick("pode ser paga", "podem ser pagas")),
			fmt.Sprintf("Pague com Pix pelo botão abaixo%s: leva menos de um minuto.", withDue(due)), false
	case ReminderDueSoon:
		return fmt.Sprintf("%s %s em %s: %s", sua, vence, days(r.DaysToDue), total), "Lembrete",
			fmt.Sprintf("%s %s em %s", sua, vence, days(r.DaysToDue)),
			fmt.Sprintf("Para não esquecer: são %s%s. Pague com Pix pelo botão abaixo.", total, withDue(due)), false
	case ReminderDueToday:
		return fmt.Sprintf("%s %s hoje: %s", sua, vence, total), "Vence hoje",
			fmt.Sprintf("%s %s hoje", sua, vence),
			fmt.Sprintf("Pague agora com Pix (%s) e mantenha o rastreamento em dia.", total), false
	case ReminderSuspensionSoon:
		when := "em " + days(r.SuspendIn)
		if r.SuspendIn == 1 {
			when = "amanhã"
		}
		return fmt.Sprintf("Seu acesso ao rastreamento será suspenso %s", when), "Atenção",
			fmt.Sprintf("Seu acesso será suspenso %s", when),
			fmt.Sprintf("%s %s %s há %s. Sem o pagamento, o acesso ao mapa e aos alertas fica suspenso%s, até a fatura ser paga (o rastreador continua gravando). Pague agora com Pix: a baixa é na hora.",
				a, esta, vencida, days(r.DaysLate), since(r.SuspendOn)), true
	case ReminderOverdue:
		return fmt.Sprintf("%s em atraso: %s", pick("Fatura", "Faturas"), total), pick("Fatura em atraso", "Faturas em atraso"),
			fmt.Sprintf("%s %s %s há %s", sua, esta, vencida, days(r.DaysLate)),
			fmt.Sprintf("Pague com Pix pelo botão abaixo (%s) e evite a suspensão do acesso.", total), true
	default: // MANUAL: conforme a situação da fatura
		if r.DaysLate > 0 {
			return fmt.Sprintf("Lembrete: %s em atraso de %s", pick("fatura", "faturas"), total), pick("Fatura em atraso", "Faturas em atraso"),
				fmt.Sprintf("%s %s %s há %s", sua, esta, vencida, days(r.DaysLate)),
				fmt.Sprintf("Pague com Pix pelo botão abaixo (%s): a baixa é na hora.", total), true
		}
		if r.DaysToDue == 0 {
			return fmt.Sprintf("Lembrete: %s %s hoje", lower, vence), "Vence hoje",
				fmt.Sprintf("%s %s hoje", sua, vence), fmt.Sprintf("Pague agora com Pix (%s).", total), false
		}
		return fmt.Sprintf("Lembrete: %s de %s", lower, total), "Lembrete",
			fmt.Sprintf("%s de %s %s em aberto", sua, total, esta),
			fmt.Sprintf("Pague com Pix pelo botão abaixo%s.", withDue(due)), false
	}
}

func withDue(due string) string {
	if due == "" {
		return ""
	}
	return " (vence em " + due + ")"
}

func since(suspendOn string) string {
	if suspendOn == "" {
		return ""
	}
	return " a partir de " + suspendOn
}

// Reminder manda o lembrete.
func (m *InvoiceMailer) Reminder(ctx context.Context, to, name string, r InvoiceReminder) error {
	total := 0
	lines := make([]invoiceLineData, 0, len(r.Invoices))
	for _, inv := range r.Invoices {
		total += inv.AmountCents
		lines = append(lines, invoiceLineData{InvoiceLine: inv, Amount: brl(inv.AmountCents)})
	}
	subject, kicker, headline, lead, danger := r.texts(brl(total))
	data := invoiceData{
		InvoiceReminder: r, Name: firstName(name), Kicker: kicker, Headline: headline, Lead: lead,
		Total: brl(total), Lines: lines, Danger: danger, AppURL: m.appURL,
	}
	var text, html bytes.Buffer
	if err := invoiceTemplates.text.Execute(&text, data); err != nil {
		return fmt.Errorf("montando e-mail (texto): %w", err)
	}
	if err := invoiceTemplates.html.ExecuteTemplate(&html, "layout", data); err != nil {
		return fmt.Errorf("montando e-mail (HTML): %w", err)
	}
	return m.sender.Send(ctx, Message{To: to, Subject: subject, Text: text.String(), HTML: html.String()})
}

// PushText é o lembrete no celular: o título e a frase.
func (r InvoiceReminder) PushText() (title, body string) {
	total := 0
	for _, inv := range r.Invoices {
		total += inv.AmountCents
	}
	_, _, headline, _, _ := r.texts(brl(total))
	if r.Kind == ReminderSuspensionSoon {
		return headline, fmt.Sprintf("Fatura vencida (%s). Toque para pagar com Pix.", brl(total))
	}
	return headline, fmt.Sprintf("%s · toque para pagar com Pix.", brl(total))
}

const invoiceHTML = `{{define "content"}}
<p style="margin:0 0 8px;font-size:12px;font-weight:800;letter-spacing:3px;text-transform:uppercase;color:{{if .Danger}}#b91c1c{{else}}#15803d{{end}};">{{.Kicker}}</p>
<h1 style="margin:0 0 16px;font-size:22px;line-height:1.3;color:#0d130e;">{{.Headline}}</h1>
<p style="margin:0 0 12px;font-size:15px;line-height:1.6;color:#334155;">Olá{{with .Name}}, {{.}}{{end}}!</p>
<p style="margin:0 0 16px;font-size:15px;line-height:1.6;color:#334155;">{{.Lead}}</p>
{{range .Lines}}<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="margin:0 0 12px;border:1px solid #e2e8f0;border-radius:12px;">
<tr><td style="padding:14px 16px;">
<p style="margin:0;font-size:15px;font-weight:700;color:#0d130e;">{{.Description}}</p>
<p style="margin:4px 0 12px;font-size:14px;color:#475569;"><strong style="color:#0d130e;">{{.Amount}}</strong> · vencimento {{.DueDate}}</p>
<table role="presentation" cellpadding="0" cellspacing="0"><tr><td style="border-radius:999px;background:#3be558;">
<a href="{{.PayURL}}" style="display:inline-block;padding:11px 22px;font-size:15px;font-weight:700;color:#060907;text-decoration:none;border-radius:999px;">Pagar com Pix</a>
</td></tr></table>
</td></tr></table>{{end}}
<p style="margin:16px 0 0;font-size:13px;line-height:1.6;color:#64748b;">O botão abre o QR Code e o Pix copia-e-cola da fatura, sem precisar entrar no painel. Já pagou? Pode desconsiderar: a baixa é automática.</p>
{{end}}`

const invoiceText = `Olá{{with .Name}}, {{.}}{{end}}!

{{.Headline}}. {{.Lead}}
{{range .Lines}}
- {{.Description}}: {{.Amount}}, vencimento {{.DueDate}}
  Pagar com Pix: {{.PayURL}}
{{end}}
O link abre o QR Code e o Pix copia-e-cola da fatura, sem precisar entrar no painel. Já pagou? Pode desconsiderar: a baixa é automática.

— Farbo Rastreadores
`

var invoiceTemplates = mustTemplates(invoiceText, invoiceHTML)
