package mail

import (
	"bytes"
	"context"
	"fmt"
	htmltemplate "html/template"
	"strings"
	texttemplate "text/template"
	"time"
)

// PriceAdjustmentMailer manda o aviso do reajuste anual ao cliente (e o de
// cancelamento) e o resumo aos admins.
type PriceAdjustmentMailer struct {
	sender Sender
	appURL string
}

func NewPriceAdjustmentMailer(sender Sender, appURL string) *PriceAdjustmentMailer {
	return &PriceAdjustmentMailer{sender: sender, appURL: strings.TrimRight(appURL, "/")}
}

// PriceAdjustmentLine é uma mensalidade reajustada.
type PriceAdjustmentLine struct {
	Vehicle  string
	Plan     string
	OldCents int
	NewCents int
}

// PriceAdjustmentNotice é o reajuste de um cliente.
type PriceAdjustmentNotice struct {
	Year int
	// Rate e Period já escritos: "4,72%", "junho de 2026 a maio de 2027".
	Rate          string
	Period        string
	EffectiveFrom time.Time
	Lines         []PriceAdjustmentLine
}

// PriceAdjustmentSummary é o resumo do ano para os admins.
type PriceAdjustmentSummary struct {
	Year          int
	Rate          string
	Period        string
	EffectiveFrom time.Time
	CancelUntil   time.Time
	Customers     int
	Subscriptions int
	// MonthlyDiffCents é quanto a receita mensal sobe.
	MonthlyDiffCents int
	// NoChange: índice zero ou negativo, sem reajuste.
	NoChange bool
}

type adjustmentData struct {
	Name, AppURL, ActionURL string
	Year                    int
	Rate, Period            string
	From, CancelUntil       string
	Lines                   []adjustmentLineView
	Customers               int
	Subscriptions           int
	MonthlyDiff             string
	NoChange                bool
}

type adjustmentLineView struct {
	Label, Old, New string
}

func (m *PriceAdjustmentMailer) lines(n PriceAdjustmentNotice) []adjustmentLineView {
	out := make([]adjustmentLineView, 0, len(n.Lines))
	for _, l := range n.Lines {
		label := l.Plan
		if l.Vehicle != "" {
			label = l.Vehicle + " (" + l.Plan + ")"
		}
		out = append(out, adjustmentLineView{Label: label, Old: brl(l.OldCents), New: brl(l.NewCents)})
	}
	return out
}

// Notice avisa o cliente do reajuste (30 dias antes).
func (m *PriceAdjustmentMailer) Notice(ctx context.Context, to, name string, n PriceAdjustmentNotice) error {
	data := adjustmentData{
		Name: firstName(name), AppURL: m.appURL, ActionURL: m.appURL + "/faturas", Year: n.Year, Rate: n.Rate,
		Period: n.Period, From: n.EffectiveFrom.Format("02/01/2006"), Lines: m.lines(n),
	}
	return m.send(ctx, to, name, "Reajuste anual da sua mensalidade — Farbo Rastreadores", adjustmentNoticeTemplates, data)
}

// Canceled avisa que o reajuste anunciado não vai acontecer.
func (m *PriceAdjustmentMailer) Canceled(ctx context.Context, to, name string, n PriceAdjustmentNotice) error {
	data := adjustmentData{
		Name: firstName(name), AppURL: m.appURL, ActionURL: m.appURL + "/faturas", Year: n.Year,
		From: n.EffectiveFrom.Format("02/01/2006"), Lines: m.lines(n),
	}
	return m.send(ctx, to, name, "Reajuste cancelado: sua mensalidade continua a mesma — Farbo Rastreadores",
		adjustmentCanceledTemplates, data)
}

// Summary manda aos admins o resumo do reajuste do ano.
func (m *PriceAdjustmentMailer) Summary(ctx context.Context, to string, s PriceAdjustmentSummary) error {
	data := adjustmentData{
		AppURL: m.appURL, ActionURL: m.appURL + "/clientes", Year: s.Year, Rate: s.Rate, Period: s.Period,
		From: s.EffectiveFrom.Format("02/01/2006"), CancelUntil: s.CancelUntil.Format("02/01/2006"),
		Customers: s.Customers, Subscriptions: s.Subscriptions, MonthlyDiff: brl(s.MonthlyDiffCents), NoChange: s.NoChange,
	}
	subject := fmt.Sprintf("Reajuste anual de %d: IPCA %s — Farbo Rastreadores", s.Year, s.Rate)
	if s.NoChange {
		subject = fmt.Sprintf("Sem reajuste em %d: IPCA %s — Farbo Rastreadores", s.Year, s.Rate)
	}
	return m.send(ctx, to, "", subject, adjustmentSummaryTemplates, data)
}

func (m *PriceAdjustmentMailer) send(ctx context.Context, to, name, subject string, t adjustmentTemplates, data adjustmentData) error {
	var text, html bytes.Buffer
	if err := t.text.Execute(&text, data); err != nil {
		return fmt.Errorf("montando e-mail (texto): %w", err)
	}
	if err := t.html.ExecuteTemplate(&html, "layout", data); err != nil {
		return fmt.Errorf("montando e-mail (HTML): %w", err)
	}
	return m.sender.Send(ctx, Message{To: to, ToName: name, Subject: subject, Text: text.String(), HTML: html.String()})
}

type adjustmentTemplates struct {
	text *texttemplate.Template
	html *htmltemplate.Template
}

func adjustmentTemplatesOf(text, html string) adjustmentTemplates {
	funcs := htmltemplate.FuncMap{
		"button": func(url, label string) buttonData { return buttonData{URL: url, Label: label} },
	}
	return adjustmentTemplates{
		text: texttemplate.Must(texttemplate.New("text").Parse(text)),
		html: htmltemplate.Must(htmltemplate.New("html").Funcs(funcs).Parse(layoutHTML + html)),
	}
}

const adjustmentLinesHTML = `<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="margin:0 0 16px;border:1px solid #e2e8f0;border-radius:12px;">
{{range .Lines}}<tr><td style="padding:12px 16px;font-size:14px;line-height:1.5;color:#334155;border-bottom:1px solid #f1f5f9;">{{.Label}}</td>
<td style="padding:12px 16px;font-size:14px;line-height:1.5;color:#334155;text-align:right;white-space:nowrap;border-bottom:1px solid #f1f5f9;">{{.Old}} → <strong style="color:#0d130e;">{{.New}}</strong></td></tr>{{end}}
</table>`

var adjustmentNoticeTemplates = adjustmentTemplatesOf(`Olá{{with .Name}}, {{.}}{{end}}!

Como prevê o contrato (cláusula 5), a mensalidade é reajustada uma vez por ano, em agosto, pelo IPCA (IBGE) acumulado nos 12 meses até maio. O IPCA de {{.Period}} foi de {{.Rate}}.

A partir das faturas que vencem em {{.From}}:
{{range .Lines}}
- {{.Label}}: {{.Old}} → {{.New}}{{end}}

Nada muda na forma de pagar: as faturas continuam no app, com o Pix. Dúvidas? Fale com a gente pelo WhatsApp ou pelo app.

{{.ActionURL}}

— Farbo Rastreadores
`, `{{define "content"}}
<p style="margin:0 0 8px;font-size:12px;font-weight:800;letter-spacing:3px;text-transform:uppercase;color:#15803d;">Reajuste anual</p>
<h1 style="margin:0 0 16px;font-size:22px;line-height:1.3;color:#0d130e;">Sua mensalidade a partir de {{.From}}</h1>
<p style="margin:0 0 12px;font-size:15px;line-height:1.6;color:#334155;">Olá{{with .Name}}, {{.}}{{end}}!</p>
<p style="margin:0 0 16px;font-size:15px;line-height:1.6;color:#334155;">Como prevê o contrato (cláusula 5), a mensalidade é reajustada uma vez por ano, em agosto, pelo IPCA (IBGE) acumulado nos 12 meses até maio. O IPCA de {{.Period}} foi de <strong>{{.Rate}}</strong>. A partir das faturas que vencem em <strong>{{.From}}</strong>:</p>
`+adjustmentLinesHTML+`
<p style="margin:0;font-size:14px;line-height:1.6;color:#475569;">Nada muda na forma de pagar: as faturas continuam no app, com o Pix. Dúvidas? Fale com a gente pelo WhatsApp ou pelo app.</p>
{{template "button" (button .ActionURL "Ver minhas faturas")}}
{{end}}`)

var adjustmentCanceledTemplates = adjustmentTemplatesOf(`Olá{{with .Name}}, {{.}}{{end}}!

O reajuste anual que avisamos para as faturas a partir de {{.From}} não vai acontecer. A sua mensalidade continua a mesma:
{{range .Lines}}
- {{.Label}}: {{.Old}}{{end}}

— Farbo Rastreadores
`, `{{define "content"}}
<p style="margin:0 0 8px;font-size:12px;font-weight:800;letter-spacing:3px;text-transform:uppercase;color:#15803d;">Reajuste anual</p>
<h1 style="margin:0 0 16px;font-size:22px;line-height:1.3;color:#0d130e;">Reajuste cancelado</h1>
<p style="margin:0 0 12px;font-size:15px;line-height:1.6;color:#334155;">Olá{{with .Name}}, {{.}}{{end}}!</p>
<p style="margin:0 0 16px;font-size:15px;line-height:1.6;color:#334155;">O reajuste anual que avisamos para as faturas a partir de {{.From}} não vai acontecer. A sua mensalidade continua a mesma:</p>
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="margin:0 0 16px;border:1px solid #e2e8f0;border-radius:12px;">
{{range .Lines}}<tr><td style="padding:12px 16px;font-size:14px;color:#334155;">{{.Label}}</td><td style="padding:12px 16px;font-size:14px;text-align:right;color:#0d130e;"><strong>{{.Old}}</strong></td></tr>{{end}}
</table>
{{template "button" (button .ActionURL "Ver minhas faturas")}}
{{end}}`)

var adjustmentSummaryTemplates = adjustmentTemplatesOf(`{{if .NoChange}}O IPCA de {{.Period}} foi de {{.Rate}}: não há reajuste em {{.Year}}. As mensalidades ficam como estão.{{else}}Reajuste anual de {{.Year}}: IPCA de {{.Period}} = {{.Rate}}.

{{.Customers}} clientes ({{.Subscriptions}} mensalidades) foram avisados por e-mail. O preço novo vale para as faturas que vencem a partir de {{.From}}; a receita mensal sobe {{.MonthlyDiff}}.

Dá para cancelar o reajuste deste ano em Clientes → Reajuste anual até {{.CancelUntil}} (os clientes recebem o aviso de cancelamento).{{end}}

{{.ActionURL}}
`, `{{define "content"}}
<p style="margin:0 0 8px;font-size:12px;font-weight:800;letter-spacing:3px;text-transform:uppercase;color:#15803d;">Reajuste anual · {{.Year}}</p>
{{if .NoChange}}
<h1 style="margin:0 0 16px;font-size:22px;line-height:1.3;color:#0d130e;">Sem reajuste este ano</h1>
<p style="margin:0;font-size:15px;line-height:1.6;color:#334155;">O IPCA de {{.Period}} foi de <strong>{{.Rate}}</strong>. Como o contrato prevê, índice zero ou negativo mantém a mensalidade.</p>
{{else}}
<h1 style="margin:0 0 16px;font-size:22px;line-height:1.3;color:#0d130e;">IPCA de {{.Rate}}</h1>
<p style="margin:0 0 12px;font-size:15px;line-height:1.6;color:#334155;">O IPCA de {{.Period}} foi de <strong>{{.Rate}}</strong>. <strong>{{.Customers}} clientes</strong> ({{.Subscriptions}} mensalidades) foram avisados por e-mail.</p>
<p style="margin:0 0 12px;font-size:15px;line-height:1.6;color:#334155;">O preço novo vale para as faturas que vencem a partir de <strong>{{.From}}</strong>; a receita mensal sobe <strong>{{.MonthlyDiff}}</strong>.</p>
<p style="margin:0;font-size:14px;line-height:1.6;color:#475569;">Dá para cancelar o reajuste deste ano em Clientes → Reajuste anual até <strong>{{.CancelUntil}}</strong>; os clientes recebem o aviso de cancelamento.</p>
{{end}}
{{template "button" (button .ActionURL "Abrir Clientes")}}
{{end}}`)
