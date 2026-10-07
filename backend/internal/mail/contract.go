package mail

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"
)

// ContractMailer manda ao cliente a cópia do contrato que ele aceitou.
type ContractMailer struct {
	sender Sender
	appURL string
}

func NewContractMailer(sender Sender, appURL string) *ContractMailer {
	return &ContractMailer{sender: sender, appURL: strings.TrimRight(appURL, "/")}
}

// ContractBlock é um parágrafo (Text) ou uma lista (Items).
type ContractBlock struct {
	Text  string
	Items []string
}

// ContractSection é uma cláusula.
type ContractSection struct {
	Title  string
	Blocks []ContractBlock
}

// ContractCopy é o contrato aceito, com o registro do aceite.
type ContractCopy struct {
	Title         string
	Version       string
	EffectiveDate string
	SHA256        string
	Intro         []ContractBlock
	Sections      []ContractSection
	Name          string
	// Document já formatado (123.456.789-09).
	Document   string
	AcceptedAt time.Time
	IP         string
}

type contractData struct {
	ContractCopy
	FirstName string
	When      string
	AppURL    string
	ActionURL string
}

// ContractAccepted manda a cópia do contrato aceito.
func (m *ContractMailer) ContractAccepted(ctx context.Context, to, name string, c ContractCopy) error {
	data := contractData{
		ContractCopy: c, FirstName: firstName(name), When: formatBrazilTime(c.AcceptedAt),
		AppURL: m.appURL, ActionURL: m.appURL + "/contrato",
	}
	var text, html bytes.Buffer
	if err := contractTemplates.text.Execute(&text, data); err != nil {
		return fmt.Errorf("montando e-mail (texto): %w", err)
	}
	if err := contractTemplates.html.ExecuteTemplate(&html, "layout", data); err != nil {
		return fmt.Errorf("montando e-mail (HTML): %w", err)
	}
	return m.sender.Send(ctx, Message{To: to, Subject: "Sua cópia do contrato — " + c.Title, Text: text.String(), HTML: html.String()})
}

// ContractUpdate é o aviso de uma versão nova do contrato.
type ContractUpdate struct {
	Title         string
	Version       string
	EffectiveDate string
	Changes       string
}

// ContractUpdated avisa quem aceitou a versão anterior que o contrato mudou:
// o que mudou e que o novo aceite é pedido no próximo acesso.
func (m *ContractMailer) ContractUpdated(ctx context.Context, to, name string, u ContractUpdate) error {
	data := struct {
		ContractUpdate
		FirstName, AppURL, ActionURL string
	}{u, firstName(name), m.appURL, m.appURL + "/contrato"}
	var text, html bytes.Buffer
	if err := contractUpdatedTemplates.text.Execute(&text, data); err != nil {
		return fmt.Errorf("montando e-mail (texto): %w", err)
	}
	if err := contractUpdatedTemplates.html.ExecuteTemplate(&html, "layout", data); err != nil {
		return fmt.Errorf("montando e-mail (HTML): %w", err)
	}
	return m.sender.Send(ctx, Message{To: to, ToName: name, Subject: "Atualizamos o contrato — " + u.Title,
		Text: text.String(), HTML: html.String()})
}

var contractUpdatedTemplates = mustTemplates(`Olá{{with .FirstName}}, {{.}}{{end}}!

Atualizamos o contrato de prestação de serviços (versão {{.Version}}, de {{.EffectiveDate}}).

O que mudou: {{.Changes}}

No seu próximo acesso, o app pede o aceite da versão nova. Se você não concordar, pode encerrar a assinatura sem multa, mesmo dentro da permanência mínima (cláusula 14).

Leia o contrato completo: {{.ActionURL}}

— Farbo Rastreadores
`, `{{define "content"}}
<p style="margin:0 0 8px;font-size:12px;font-weight:800;letter-spacing:3px;text-transform:uppercase;color:#15803d;">Contrato</p>
<h1 style="margin:0 0 16px;font-size:22px;line-height:1.3;color:#0d130e;">Atualizamos o contrato</h1>
<p style="margin:0 0 12px;font-size:15px;line-height:1.6;color:#334155;">Olá{{with .FirstName}}, {{.}}{{end}}!</p>
<p style="margin:0 0 12px;font-size:15px;line-height:1.6;color:#334155;">O contrato de prestação de serviços tem uma versão nova (versão {{.Version}}, de {{.EffectiveDate}}).</p>
<p style="margin:0 0 12px;padding:14px 16px;border-radius:12px;background:#f0fdf4;border:1px solid #bbf7d0;font-size:15px;line-height:1.6;color:#0d130e;"><strong>O que mudou:</strong> {{.Changes}}</p>
<p style="margin:0;font-size:14px;line-height:1.6;color:#475569;">No seu próximo acesso, o app pede o aceite da versão nova. Se você não concordar, pode encerrar a assinatura sem multa, mesmo dentro da permanência mínima (cláusula 14).</p>
{{template "button" (button .ActionURL "Ler o contrato")}}
{{end}}`)

const contractHTML = `{{define "content"}}
<p style="margin:0 0 8px;font-size:12px;font-weight:800;letter-spacing:3px;text-transform:uppercase;color:#15803d;">Contrato</p>
<h1 style="margin:0 0 16px;font-size:22px;line-height:1.3;color:#0d130e;">Sua cópia do contrato</h1>
<p style="margin:0 0 12px;font-size:15px;line-height:1.6;color:#334155;">Olá{{with .FirstName}}, {{.}}{{end}}!</p>
<p style="margin:0 0 16px;font-size:15px;line-height:1.6;color:#334155;">Você aceitou o contrato abaixo em {{.When}}. Guarde este e-mail: é a sua cópia. Ele também fica disponível na plataforma.</p>
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="margin:0 0 20px;border:1px solid #e2e8f0;border-radius:12px;">
<tr><td style="padding:14px 16px;font-size:13px;line-height:1.7;color:#475569;">
<strong style="color:#0d130e;">Registro do aceite</strong><br>
Contratante: {{.Name}} · CPF/CNPJ {{.Document}}<br>
Data e hora: {{.When}}{{with .IP}} · IP {{.}}{{end}}<br>
Versão {{.Version}} ({{.EffectiveDate}}) · código {{.SHA256}}
</td></tr></table>
<h2 style="margin:24px 0 8px;font-size:18px;line-height:1.3;color:#0d130e;">{{.Title}}</h2>
{{range .Intro}}{{template "contractBlock" .}}{{end}}
{{range .Sections}}<h3 style="margin:20px 0 6px;font-size:15px;line-height:1.4;color:#0d130e;">{{.Title}}</h3>
{{range .Blocks}}{{template "contractBlock" .}}{{end}}{{end}}
{{template "button" (button .ActionURL "Ver o contrato na plataforma")}}
{{end}}
{{define "contractBlock"}}{{if .Items}}<ul style="margin:0 0 10px;padding-left:20px;font-size:14px;line-height:1.6;color:#334155;">{{range .Items}}<li style="margin:0 0 4px;">{{.}}</li>{{end}}</ul>{{else}}<p style="margin:0 0 10px;font-size:14px;line-height:1.6;color:#334155;">{{.Text}}</p>{{end}}{{end}}`

const contractText = `Olá{{with .FirstName}}, {{.}}{{end}}!

Você aceitou o contrato abaixo em {{.When}}. Guarde este e-mail: é a sua cópia.

Registro do aceite
Contratante: {{.Name}} · CPF/CNPJ {{.Document}}
Data e hora: {{.When}}{{with .IP}} · IP {{.}}{{end}}
Versão {{.Version}} ({{.EffectiveDate}}) · código {{.SHA256}}

{{.Title}}
{{range .Intro}}
{{if .Items}}{{range .Items}}- {{.}}
{{end}}{{else}}{{.Text}}
{{end}}{{end}}{{range .Sections}}
{{.Title}}
{{range .Blocks}}{{if .Items}}{{range .Items}}- {{.}}
{{end}}{{else}}{{.Text}}
{{end}}{{end}}{{end}}
Ver o contrato na plataforma: {{.ActionURL}}

— Farbo Rastreadores
`

var contractTemplates = mustTemplates(contractText, contractHTML)
