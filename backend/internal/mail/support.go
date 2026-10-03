package mail

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
)

// SupportMailer avisa a equipe das conversas do WhatsApp que a IA passou
// para uma pessoa.
type SupportMailer struct {
	sender Sender
	appURL string
}

func NewSupportMailer(sender Sender, appURL string) *SupportMailer {
	return &SupportMailer{sender: sender, appURL: strings.TrimRight(appURL, "/")}
}

// Handoff é o que o aviso mostra.
type Handoff struct {
	ConversationID string
	Contact        string
	Phone          string
	Reason         string
}

type handoffData struct {
	AppURL    string
	ActionURL string
	Contact   string
	Phone     string
	Reason    string
}

// Handoff manda o aviso para cada endereço da equipe.
func (m *SupportMailer) Handoff(ctx context.Context, to []string, h Handoff) error {
	data := handoffData{
		AppURL: m.appURL, ActionURL: m.appURL + "/atendimento?conversa=" + h.ConversationID,
		Contact: h.Contact, Phone: h.Phone, Reason: h.Reason,
	}
	var text, html bytes.Buffer
	if err := handoffTemplates.text.Execute(&text, data); err != nil {
		return fmt.Errorf("montando e-mail (texto): %w", err)
	}
	if err := handoffTemplates.html.ExecuteTemplate(&html, "layout", data); err != nil {
		return fmt.Errorf("montando e-mail (HTML): %w", err)
	}
	who := h.Contact
	if who == "" {
		who = h.Phone
	}
	var errs []error
	for _, addr := range to {
		errs = append(errs, m.sender.Send(ctx, Message{
			To: addr, Subject: "WhatsApp: " + who + " precisa de atendimento",
			Text: text.String(), HTML: html.String(),
		}))
	}
	return errors.Join(errs...)
}

const handoffHTML = `{{define "content"}}
<p style="margin:0 0 8px;font-size:12px;font-weight:800;letter-spacing:3px;text-transform:uppercase;color:#15803d;">Atendimento</p>
<h1 style="margin:0 0 16px;font-size:22px;line-height:1.3;color:#0d130e;">Conversa no WhatsApp esperando a equipe</h1>
<p style="margin:0 0 12px;font-size:15px;line-height:1.6;color:#334155;">O atendente de IA passou esta conversa para uma pessoa e não responde mais nela.</p>
<p style="margin:0 0 4px;font-size:15px;line-height:1.6;color:#334155;"><strong>{{with .Contact}}{{.}}{{else}}Contato sem nome{{end}}</strong> · {{.Phone}}</p>
<p style="margin:0;font-size:15px;line-height:1.6;color:#334155;">Motivo: {{.Reason}}</p>
{{template "button" (button .ActionURL "Abrir a conversa")}}
<p style="margin:0;font-size:14px;line-height:1.6;color:#475569;">O WhatsApp só aceita resposta em texto livre até 24 horas depois da última mensagem do contato.</p>
{{end}}`

const handoffText = `O atendente de IA passou uma conversa do WhatsApp para a equipe e não responde mais nela.

Contato: {{with .Contact}}{{.}}{{else}}sem nome{{end}} · {{.Phone}}
Motivo: {{.Reason}}

Abra a conversa no painel:
{{.ActionURL}}

O WhatsApp só aceita resposta em texto livre até 24 horas depois da última mensagem do contato.

— Farbo Rastreadores
`

var handoffTemplates = mustTemplates(handoffText, handoffHTML)
