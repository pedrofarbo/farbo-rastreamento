package mail

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"
)

// TwoFactorMailer manda o código da verificação em duas etapas e os avisos
// de mudança (ativada, desativada, redefinida, código de recuperação usado).
type TwoFactorMailer struct {
	sender Sender
	appURL string
}

func NewTwoFactorMailer(sender Sender, appURL string) *TwoFactorMailer {
	return &TwoFactorMailer{sender: sender, appURL: strings.TrimRight(appURL, "/")}
}

type twoFactorData struct {
	Name      string
	AppURL    string
	ActionURL string
	Code      string
	Validity  string
	Title     string
	Lead      string
	When      string
	Warning   string
}

// O texto de cada uso do código.
var codePurposes = map[string]struct{ Subject, Title, Lead string }{
	"login": {"Seu código para entrar", "Seu código para entrar",
		"Use este código para concluir a entrada na sua conta da Farbo Rastreadores:"},
	"setup": {"Confirme a verificação em duas etapas", "Confirme que é você",
		"Use este código para ativar a verificação em duas etapas na sua conta:"},
	"action": {"Confirme a mudança na sua conta", "Confirme a mudança",
		"Use este código para confirmar a mudança na verificação em duas etapas da sua conta:"},
}

// Code manda o código de 6 dígitos.
func (m *TwoFactorMailer) Code(ctx context.Context, to, name, code string, ttl time.Duration, purpose string) error {
	p, ok := codePurposes[purpose]
	if !ok {
		p = codePurposes["login"]
	}
	data := twoFactorData{
		Name: firstName(name), AppURL: m.appURL, Code: code, Validity: humanizeDuration(ttl), Title: p.Title, Lead: p.Lead,
	}
	msg, err := renderTwoFactor(twoFactorCodeTemplates, data)
	if err != nil {
		return err
	}
	msg.To, msg.ToName = to, name
	msg.Subject = fmt.Sprintf("%s: %s — Farbo Rastreadores", p.Subject, code)
	return m.sender.Send(ctx, msg)
}

// O texto de cada aviso.
var twoFactorEvents = map[string]struct{ Subject, Title, Lead, Warning string }{
	"enabled_totp": {"Verificação em duas etapas ativada", "Verificação em duas etapas ativada",
		"A verificação em duas etapas foi ativada na sua conta, com o app autenticador.",
		"Guarde os códigos de recuperação em lugar seguro: são eles que abrem a conta se você perder o celular."},
	"enabled_email": {"Verificação em duas etapas ativada", "Verificação em duas etapas ativada",
		"A verificação em duas etapas foi ativada na sua conta: ao entrar, mandaremos um código para este e-mail.",
		"Guarde os códigos de recuperação em lugar seguro."},
	"disabled": {"Verificação em duas etapas desativada", "Verificação em duas etapas desativada",
		"A verificação em duas etapas foi desativada na sua conta. Agora, para entrar, basta a senha.",
		"Não foi você? Troque a senha agora mesmo (Esqueci minha senha) e fale com o nosso suporte."},
	"reset": {"Verificação em duas etapas redefinida", "Verificação em duas etapas redefinida",
		"A nossa equipe redefiniu a verificação em duas etapas da sua conta, a pedido. Os códigos de recuperação e os aparelhos confiáveis antigos deixaram de valer.",
		"Não pediu? Troque a senha agora mesmo (Esqueci minha senha) e fale com o nosso suporte."},
	"recovery_used": {"Código de recuperação usado", "Um código de recuperação foi usado",
		"Alguém entrou na sua conta com um dos códigos de recuperação. Cada código vale uma vez só.",
		"Não foi você? Troque a senha agora mesmo e gere códigos novos em Minha conta → Segurança."},
}

// Changed avisa o dono da conta de uma mudança na verificação.
func (m *TwoFactorMailer) Changed(ctx context.Context, to, name, event string, at time.Time) error {
	e, ok := twoFactorEvents[event]
	if !ok {
		return fmt.Errorf("aviso desconhecido: %s", event)
	}
	data := twoFactorData{
		Name: firstName(name), AppURL: m.appURL, ActionURL: m.appURL + "/login", Title: e.Title, Lead: e.Lead,
		When: formatBrazilTime(at), Warning: e.Warning,
	}
	msg, err := renderTwoFactor(twoFactorChangedTemplates, data)
	if err != nil {
		return err
	}
	msg.To, msg.ToName = to, name
	msg.Subject = e.Subject + " — Farbo Rastreadores"
	return m.sender.Send(ctx, msg)
}

func renderTwoFactor(t templatePair, data twoFactorData) (Message, error) {
	var text, html bytes.Buffer
	if err := t.text.Execute(&text, data); err != nil {
		return Message{}, fmt.Errorf("montando e-mail (texto): %w", err)
	}
	if err := t.html.ExecuteTemplate(&html, "layout", data); err != nil {
		return Message{}, fmt.Errorf("montando e-mail (HTML): %w", err)
	}
	return Message{Text: text.String(), HTML: html.String()}, nil
}

const twoFactorCodeHTML = `{{define "content"}}
<p style="margin:0 0 8px;font-size:12px;font-weight:800;letter-spacing:3px;text-transform:uppercase;color:#15803d;">Segurança da conta</p>
<h1 style="margin:0 0 16px;font-size:22px;line-height:1.3;color:#0d130e;">{{.Title}}</h1>
<p style="margin:0 0 12px;font-size:15px;line-height:1.6;color:#334155;">Olá{{with .Name}}, {{.}}{{end}}!</p>
<p style="margin:0 0 16px;font-size:15px;line-height:1.6;color:#334155;">{{.Lead}}</p>
<p style="margin:0 0 16px;padding:16px;border-radius:12px;background:#f0fdf4;border:1px solid #bbf7d0;text-align:center;font-size:32px;font-weight:800;letter-spacing:8px;color:#0d130e;font-family:'SFMono-Regular',Consolas,monospace;">{{.Code}}</p>
<p style="margin:0 0 12px;font-size:14px;line-height:1.6;color:#475569;">O código vale por <strong>{{.Validity}}</strong>. Nunca o passe a ninguém: a nossa equipe não pede este código.</p>
<p style="margin:0;font-size:14px;line-height:1.6;color:#475569;"><strong>Não foi você?</strong> Alguém sabe a sua senha: troque-a agora mesmo (Esqueci minha senha).</p>
{{end}}`

const twoFactorCodeText = `Olá{{with .Name}}, {{.}}{{end}}!

{{.Lead}}

{{.Code}}

O código vale por {{.Validity}}. Nunca o passe a ninguém: a nossa equipe não pede este código.

Não foi você? Alguém sabe a sua senha: troque-a agora mesmo (Esqueci minha senha).

— Farbo Rastreadores
`

const twoFactorChangedHTML = `{{define "content"}}
<p style="margin:0 0 8px;font-size:12px;font-weight:800;letter-spacing:3px;text-transform:uppercase;color:#15803d;">Segurança da conta</p>
<h1 style="margin:0 0 16px;font-size:22px;line-height:1.3;color:#0d130e;">{{.Title}}</h1>
<p style="margin:0 0 12px;font-size:15px;line-height:1.6;color:#334155;">Olá{{with .Name}}, {{.}}{{end}}!</p>
<p style="margin:0 0 12px;font-size:15px;line-height:1.6;color:#334155;">{{.Lead}}</p>
<p style="margin:0 0 12px;font-size:14px;line-height:1.6;color:#475569;">Quando: {{.When}}.</p>
<p style="margin:0;font-size:14px;line-height:1.6;color:#475569;">{{.Warning}}</p>
{{template "button" (button .ActionURL "Entrar na minha conta")}}
{{end}}`

const twoFactorChangedText = `Olá{{with .Name}}, {{.}}{{end}}!

{{.Lead}}

Quando: {{.When}}.

{{.Warning}}

{{.ActionURL}}

— Farbo Rastreadores
`

var (
	twoFactorCodeTemplates    = mustTemplates(twoFactorCodeText, twoFactorCodeHTML)
	twoFactorChangedTemplates = mustTemplates(twoFactorChangedText, twoFactorChangedHTML)
)
