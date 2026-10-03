package mail

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
)

// LeadMailer avisa a equipe de cada pré-cliente novo da landing.
type LeadMailer struct {
	sender Sender
	appURL string
}

func NewLeadMailer(sender Sender, appURL string) *LeadMailer {
	return &LeadMailer{sender: sender, appURL: strings.TrimRight(appURL, "/")}
}

// Lead é o que o aviso mostra.
type Lead struct {
	Name         string
	Email        string
	Phone        string
	City         string
	Plan         string
	VehicleType  string
	VehicleCount int
	Message      string
}

type leadData struct {
	Lead
	AppURL    string
	ActionURL string
}

// NewLead manda o aviso para cada endereço da equipe.
func (m *LeadMailer) NewLead(ctx context.Context, to []string, l Lead) error {
	data := leadData{Lead: l, AppURL: m.appURL, ActionURL: m.appURL + "/clientes?aba=pre-clientes"}
	var text, html bytes.Buffer
	if err := leadTemplates.text.Execute(&text, data); err != nil {
		return fmt.Errorf("montando e-mail (texto): %w", err)
	}
	if err := leadTemplates.html.ExecuteTemplate(&html, "layout", data); err != nil {
		return fmt.Errorf("montando e-mail (HTML): %w", err)
	}
	var errs []error
	for _, addr := range to {
		errs = append(errs, m.sender.Send(ctx, Message{
			To: addr, Subject: "Novo pré-cliente: " + l.Name, Text: text.String(), HTML: html.String(),
		}))
	}
	return errors.Join(errs...)
}

const leadHTML = `{{define "content"}}
<p style="margin:0 0 8px;font-size:12px;font-weight:800;letter-spacing:3px;text-transform:uppercase;color:#15803d;">Pré-cliente</p>
<h1 style="margin:0 0 16px;font-size:22px;line-height:1.3;color:#0d130e;">{{.Name}} quer um rastreador</h1>
<p style="margin:0 0 4px;font-size:15px;line-height:1.6;color:#334155;">E-mail: <a href="mailto:{{.Email}}" style="color:#15803d;">{{.Email}}</a></p>
{{with .Phone}}<p style="margin:0 0 4px;font-size:15px;line-height:1.6;color:#334155;">Telefone: {{.}}</p>{{end}}
{{with .City}}<p style="margin:0 0 4px;font-size:15px;line-height:1.6;color:#334155;">Cidade: {{.}}</p>{{end}}
{{with .Plan}}<p style="margin:0 0 4px;font-size:15px;line-height:1.6;color:#334155;">Plano: {{.}}</p>{{end}}
<p style="margin:0 0 4px;font-size:15px;line-height:1.6;color:#334155;">Veículos: {{.VehicleCount}}{{with .VehicleType}} ({{.}}){{end}}</p>
{{with .Message}}<p style="margin:12px 0 0;font-size:15px;line-height:1.6;color:#334155;white-space:pre-wrap;">“{{.}}”</p>{{end}}
{{template "button" (button .ActionURL "Ver pré-clientes")}}
{{end}}`

const leadText = `{{.Name}} deixou o interesse na landing.

E-mail: {{.Email}}
{{with .Phone}}Telefone: {{.}}
{{end}}{{with .City}}Cidade: {{.}}
{{end}}{{with .Plan}}Plano: {{.}}
{{end}}Veículos: {{.VehicleCount}}{{with .VehicleType}} ({{.}}){{end}}
{{with .Message}}
Mensagem: {{.}}
{{end}}
Veja no painel:
{{.ActionURL}}

— Farbo Rastreadores
`

var leadTemplates = mustTemplates(leadText, leadHTML)
