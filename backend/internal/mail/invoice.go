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

// O lembrete junta as faturas por vencimento: as de um mesmo dia (a fatura
// do mês de uma frota, uma por veículo) saem num cartão com o total e um
// botão só, o Pix de todas. Numa frota são centenas: lista algumas de cada
// dia e alguns dias; o resto fica no app.
const (
	reminderLinesPerDay = 10
	reminderMaxDays     = 6
)

// reminderDay são as faturas do lembrete que vencem num mesmo dia.
type reminderDay struct {
	DueDate string
	Count   int
	Total   string
	// PayURL abre o Pix: o da fatura, se é uma só; o de todas as do dia, se
	// são várias (a página do link junta as do vencimento).
	PayURL string
	Lines  []invoiceLineData
	// Hidden: quantas do dia ficaram fora da lista.
	Hidden int
}

type invoiceData struct {
	InvoiceReminder
	Name     string
	Kicker   string
	Headline string
	Lead     string
	Total    string
	Count    int
	Days     []reminderDay
	// HiddenInvoices: as de outros vencimentos além dos mostrados (no app).
	HiddenInvoices int
	Danger         bool
	AppURL         string
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
	for _, inv := range r.Invoices {
		total += inv.AmountCents
	}
	subject, kicker, headline, lead, danger := r.texts(brl(total))
	data := invoiceData{
		InvoiceReminder: r, Name: firstName(name), Kicker: kicker, Headline: headline, Lead: lead,
		Total: brl(total), Count: len(r.Invoices), Danger: danger, AppURL: m.appURL,
	}
	data.Days, data.HiddenInvoices = reminderDays(r.Invoices)
	var text, html bytes.Buffer
	if err := invoiceTemplates.text.Execute(&text, data); err != nil {
		return fmt.Errorf("montando e-mail (texto): %w", err)
	}
	if err := invoiceTemplates.html.ExecuteTemplate(&html, "layout", data); err != nil {
		return fmt.Errorf("montando e-mail (HTML): %w", err)
	}
	return m.sender.Send(ctx, Message{To: to, Subject: subject, Text: text.String(), HTML: html.String()})
}

// reminderDays junta as faturas por vencimento, na ordem em que vieram, e
// devolve os primeiros dias e quantas faturas ficaram de fora deles.
func reminderDays(invoices []InvoiceLine) ([]reminderDay, int) {
	var days []reminderDay
	totals := []int{}
	index := map[string]int{}
	for _, inv := range invoices {
		i, ok := index[inv.DueDate]
		if !ok {
			i = len(days)
			index[inv.DueDate] = i
			days = append(days, reminderDay{DueDate: inv.DueDate, PayURL: inv.PayURL})
			totals = append(totals, 0)
		}
		d := &days[i]
		d.Count++
		totals[i] += inv.AmountCents
		if len(d.Lines) < reminderLinesPerDay {
			d.Lines = append(d.Lines, invoiceLineData{InvoiceLine: inv, Amount: brl(inv.AmountCents)})
		} else {
			d.Hidden++
		}
	}
	hidden := 0
	for i := range days {
		days[i].Total = brl(totals[i])
		if days[i].Count > 1 {
			days[i].PayURL = withAll(days[i].PayURL)
		}
		if i >= reminderMaxDays {
			hidden += days[i].Count
		}
	}
	if len(days) > reminderMaxDays {
		days = days[:reminderMaxDays]
	}
	return days, hidden
}

// withAll: o link de uma fatura abrindo o Pix de todas as do mesmo vencimento.
func withAll(payURL string) string {
	if strings.Contains(payURL, "?") {
		return payURL + "&todas=1"
	}
	return payURL + "?todas=1"
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
{{range .Days}}{{if eq .Count 1}}{{with index .Lines 0}}<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="margin:0 0 12px;border:1px solid #e2e8f0;border-radius:12px;">
<tr><td style="padding:14px 16px;">
<p style="margin:0;font-size:15px;font-weight:700;color:#0d130e;">{{.Description}}</p>
<p style="margin:4px 0 12px;font-size:14px;color:#475569;"><strong style="color:#0d130e;">{{.Amount}}</strong> · vencimento {{.DueDate}}</p>
<table role="presentation" cellpadding="0" cellspacing="0"><tr><td style="border-radius:999px;background:#3be558;">
<a href="{{.PayURL}}" style="display:inline-block;padding:11px 22px;font-size:15px;font-weight:700;color:#060907;text-decoration:none;border-radius:999px;">Pagar com Pix</a>
</td></tr></table>
</td></tr></table>{{end}}{{else}}<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="margin:0 0 12px;border:1px solid #e2e8f0;border-radius:12px;">
<tr><td style="padding:14px 16px;">
<p style="margin:0;font-size:15px;font-weight:700;color:#0d130e;">{{.Count}} faturas · vencimento {{.DueDate}} · total {{.Total}}</p>
<p style="margin:4px 0 12px;font-size:14px;color:#475569;">Pague todas de uma vez, com um Pix só.</p>
<table role="presentation" cellpadding="0" cellspacing="0"><tr><td style="border-radius:999px;background:#3be558;">
<a href="{{.PayURL}}" style="display:inline-block;padding:11px 22px;font-size:15px;font-weight:700;color:#060907;text-decoration:none;border-radius:999px;">Pagar {{.Total}} com Pix</a>
</td></tr></table>
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="margin:14px 0 0;border-top:1px solid #e2e8f0;">
{{range .Lines}}<tr><td style="padding:8px 0 0;font-size:13px;color:#475569;">{{.Description}}</td><td align="right" style="padding:8px 0 0;font-size:13px;color:#0d130e;white-space:nowrap;">{{.Amount}}</td></tr>{{end}}
{{if .Hidden}}<tr><td colspan="2" style="padding:8px 0 0;font-size:13px;color:#64748b;">e mais {{.Hidden}} {{if eq .Hidden 1}}fatura{{else}}faturas{{end}}</td></tr>{{end}}
</table>
</td></tr></table>{{end}}{{end}}
{{if .HiddenInvoices}}<p style="margin:0 0 12px;font-size:14px;line-height:1.6;color:#334155;">E mais {{.HiddenInvoices}} {{if eq .HiddenInvoices 1}}fatura{{else}}faturas{{end}} em outros vencimentos: <a href="{{.AppURL}}/app/faturas" style="color:#15803d;font-weight:700;">veja e pague todas no app</a>.</p>{{end}}
<p style="margin:16px 0 0;font-size:13px;line-height:1.6;color:#64748b;">O botão abre o QR Code e o Pix copia-e-cola, sem precisar entrar no painel; se preferir, lá mesmo dá para gerar o boleto. Já pagou? Pode desconsiderar: a baixa é automática.</p>
{{end}}`

const invoiceText = `Olá{{with .Name}}, {{.}}{{end}}!

{{.Headline}}. {{.Lead}}
{{range .Days}}{{if eq .Count 1}}{{with index .Lines 0}}
- {{.Description}}: {{.Amount}}, vencimento {{.DueDate}}
  Pagar com Pix: {{.PayURL}}
{{end}}{{else}}
{{.Count}} faturas com vencimento {{.DueDate}} ({{.Total}}). Pague todas de uma vez, com um Pix só:
{{.PayURL}}
{{range .Lines}}
- {{.Description}}: {{.Amount}}{{end}}{{if .Hidden}}
- e mais {{.Hidden}} {{if eq .Hidden 1}}fatura{{else}}faturas{{end}}{{end}}
{{end}}{{end}}{{if .HiddenInvoices}}
E mais {{.HiddenInvoices}} {{if eq .HiddenInvoices 1}}fatura{{else}}faturas{{end}} em outros vencimentos: veja e pague todas no app, {{.AppURL}}/app/faturas
{{end}}
O link abre o QR Code e o Pix copia-e-cola, sem precisar entrar no painel. Já pagou? Pode desconsiderar: a baixa é automática.

— Farbo Rastreadores
`

var invoiceTemplates = mustTemplates(invoiceText, invoiceHTML)
