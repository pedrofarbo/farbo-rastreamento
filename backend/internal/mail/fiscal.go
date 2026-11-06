package mail

import (
	"bytes"
	"context"
	"fmt"
	"strings"
)

// FiscalMailer manda ao cliente a nota fiscal de serviço (NFS-e) emitida.
type FiscalMailer struct {
	sender Sender
	appURL string
}

func NewFiscalMailer(sender Sender, appURL string) *FiscalMailer {
	return &FiscalMailer{sender: sender, appURL: strings.TrimRight(appURL, "/")}
}

// FiscalDocument é a NFS-e de uma fatura paga. PDFURL: o DANFSE (vazio: a
// nota fica em Faturas, no app).
type FiscalDocument struct {
	To          string
	Name        string
	Number      string
	AmountCents int
	Description string
	PDFURL      string
}

type fiscalData struct {
	FiscalDocument
	FirstName string
	Amount    string
	AppURL    string
	ActionURL string
	Label     string
}

func (m *FiscalMailer) FiscalIssued(ctx context.Context, d FiscalDocument) error {
	msg, err := renderFiscalIssued(d, m.appURL)
	if err != nil {
		return err
	}
	return m.sender.Send(ctx, msg)
}

func renderFiscalIssued(d FiscalDocument, appURL string) (Message, error) {
	data := fiscalData{FiscalDocument: d, FirstName: firstName(d.Name), Amount: brl(d.AmountCents), AppURL: appURL,
		ActionURL: appURL + "/faturas", Label: "Ver nas faturas"}
	if d.PDFURL != "" {
		data.ActionURL, data.Label = d.PDFURL, "Baixar a nota (PDF)"
	}
	var text, html bytes.Buffer
	if err := fiscalTemplates.text.Execute(&text, data); err != nil {
		return Message{}, fmt.Errorf("montando a nota fiscal (texto): %w", err)
	}
	if err := fiscalTemplates.html.ExecuteTemplate(&html, "layout", data); err != nil {
		return Message{}, fmt.Errorf("montando a nota fiscal (HTML): %w", err)
	}
	subject := "Sua nota fiscal da Farbo Rastreadores"
	if d.Number != "" {
		subject = "Nota fiscal nº " + d.Number + " da Farbo Rastreadores"
	}
	return Message{To: d.To, ToName: d.Name, Subject: subject, Text: text.String(), HTML: html.String()}, nil
}

const fiscalHTML = `{{define "content"}}
<p style="margin:0 0 8px;font-size:12px;font-weight:800;letter-spacing:3px;text-transform:uppercase;color:#16a34a;">Nota fiscal</p>
<h1 style="margin:0 0 16px;font-size:22px;line-height:1.3;color:#0d130e;">{{if .Number}}NFS-e nº {{.Number}}{{else}}Sua NFS-e{{end}} de {{.Amount}}</h1>
<p style="margin:0 0 20px;font-size:15px;line-height:1.6;color:#334155;">Olá{{with .FirstName}}, {{.}}{{end}}! Aqui está a nota fiscal de serviço do pagamento de “{{.Description}}”. Obrigado por estar com a Farbo.</p>
{{template "button" (button .ActionURL .Label)}}
<p style="margin:0;font-size:12px;line-height:1.6;color:#64748b;">A nota também fica em Faturas, no app, junto do pagamento.</p>
{{end}}`

const fiscalText = `Olá{{with .FirstName}}, {{.}}{{end}}!

Aqui está a nota fiscal de serviço{{if .Number}} (NFS-e nº {{.Number}}){{end}} de {{.Amount}}, do pagamento de "{{.Description}}".

{{.Label}}: {{.ActionURL}}

A nota também fica em Faturas, no app, junto do pagamento.

— Farbo Rastreadores
`

var fiscalTemplates = mustTemplates(fiscalText, fiscalHTML)
