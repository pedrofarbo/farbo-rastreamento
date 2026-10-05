package mail

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
)

// FinanceMailer avisa os administradores das contas a pagar que vencem.
type FinanceMailer struct {
	sender Sender
	appURL string
}

func NewFinanceMailer(sender Sender, appURL string) *FinanceMailer {
	return &FinanceMailer{sender: sender, appURL: strings.TrimRight(appURL, "/")}
}

// DueBill é uma conta do resumo (os valores já formatados).
type DueBill struct {
	Description string
	Supplier    string
	Amount      string
	// Installment: "2/10" quando parcelada.
	Installment string
}

// PayablesDue é o resumo do dia: o que vence hoje, o que vence em 3 dias
// (SoonDate) e o total das vencidas.
type PayablesDue struct {
	Today         []DueBill
	Soon          []DueBill
	SoonDate      string
	OverdueCount  int
	OverdueAmount string
}

type payablesData struct {
	PayablesDue
	AppURL    string
	ActionURL string
}

// PayablesDue manda o resumo para cada administrador.
func (m *FinanceMailer) PayablesDue(ctx context.Context, to []string, d PayablesDue) error {
	data := payablesData{PayablesDue: d, AppURL: m.appURL, ActionURL: m.appURL + "/empresa?aba=pagar"}
	var text, html bytes.Buffer
	if err := payablesTemplates.text.Execute(&text, data); err != nil {
		return fmt.Errorf("montando e-mail (texto): %w", err)
	}
	if err := payablesTemplates.html.ExecuteTemplate(&html, "layout", data); err != nil {
		return fmt.Errorf("montando e-mail (HTML): %w", err)
	}
	subject := "Contas a pagar: "
	switch {
	case len(d.Today) > 0 && len(d.Soon) > 0:
		subject += fmt.Sprintf("%d vence(m) hoje, %d em 3 dias", len(d.Today), len(d.Soon))
	case len(d.Today) > 0:
		subject += fmt.Sprintf("%d vence(m) hoje", len(d.Today))
	default:
		subject += fmt.Sprintf("%d vence(m) em 3 dias", len(d.Soon))
	}
	var errs []error
	for _, addr := range to {
		errs = append(errs, m.sender.Send(ctx, Message{To: addr, Subject: subject, Text: text.String(), HTML: html.String()}))
	}
	return errors.Join(errs...)
}

const payablesHTML = `{{define "content"}}
<p style="margin:0 0 8px;font-size:12px;font-weight:800;letter-spacing:3px;text-transform:uppercase;color:#15803d;">Gestão da empresa</p>
<h1 style="margin:0 0 16px;font-size:22px;line-height:1.3;color:#0d130e;">Contas a pagar vencendo</h1>
{{if .Today}}
<p style="margin:16px 0 6px;font-size:14px;font-weight:800;color:#b91c1c;">Vencem hoje</p>
{{range .Today}}{{template "bill" .}}{{end}}
{{end}}
{{if .Soon}}
<p style="margin:16px 0 6px;font-size:14px;font-weight:800;color:#b45309;">Vencem em 3 dias ({{.SoonDate}})</p>
{{range .Soon}}{{template "bill" .}}{{end}}
{{end}}
{{if .OverdueCount}}
<p style="margin:16px 0 0;padding:10px 12px;border-radius:8px;background:#fef2f2;font-size:14px;line-height:1.5;color:#7f1d1d;">E ainda há {{.OverdueCount}} conta(s) vencida(s), somando {{.OverdueAmount}}.</p>
{{end}}
{{template "button" (button .ActionURL "Abrir contas a pagar")}}
{{end}}
{{define "bill"}}
<p style="margin:0 0 6px;font-size:15px;line-height:1.5;color:#334155;"><strong style="color:#0d130e;">{{.Amount}}</strong> · {{.Description}}{{with .Installment}} ({{.}}){{end}}{{with .Supplier}} · {{.}}{{end}}</p>
{{end}}`

const payablesText = `Contas a pagar vencendo
{{if .Today}}
Vencem hoje:
{{range .Today}}- {{.Amount}} · {{.Description}}{{with .Installment}} ({{.}}){{end}}{{with .Supplier}} · {{.}}{{end}}
{{end}}{{end}}{{if .Soon}}
Vencem em 3 dias ({{.SoonDate}}):
{{range .Soon}}- {{.Amount}} · {{.Description}}{{with .Installment}} ({{.}}){{end}}{{with .Supplier}} · {{.}}{{end}}
{{end}}{{end}}{{if .OverdueCount}}
E ainda há {{.OverdueCount}} conta(s) vencida(s), somando {{.OverdueAmount}}.
{{end}}
Veja no painel:
{{.ActionURL}}

— Farbo Rastreadores
`

var payablesTemplates = mustTemplates(payablesText, payablesHTML)
