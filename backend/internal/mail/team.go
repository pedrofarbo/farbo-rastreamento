package mail

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// TeamMailer escreve os e-mails da conta empresa: o convite de quem entra
// na equipe de um cliente.
type TeamMailer struct {
	sender Sender
	appURL string
}

func NewTeamMailer(sender Sender, appURL string) *TeamMailer {
	return &TeamMailer{sender: sender, appURL: strings.TrimRight(appURL, "/")}
}

// TeamNotice é o acesso à conta como os e-mails contam.
type TeamNotice struct {
	OwnerName  string
	MemberName string
	// Role: manager (Gestor) ou viewer (Visualizador).
	Role string
}

type teamData struct {
	TeamNotice
	Name      string
	Owner     string
	RoleLabel string
	// What: o que o papel deixa fazer, para o texto.
	What      string
	AppURL    string
	ActionURL string
	Validity  string
}

func (m *TeamMailer) data(n TeamNotice) teamData {
	owner := strings.TrimSpace(n.OwnerName)
	if owner == "" {
		owner = "Um cliente da Farbo"
	}
	label, what := "Visualizador", "acompanhar os veículos da conta no mapa, com o histórico, os eventos e as cercas"
	if n.Role == "manager" {
		label, what = "Gestor", "acompanhar e operar os veículos da conta: mapa, histórico, cercas, bloqueio do motor e faturas"
	}
	return teamData{TeamNotice: n, Name: firstName(n.MemberName), Owner: owner, RoleLabel: label, What: what, AppURL: m.appURL}
}

func (m *TeamMailer) send(ctx context.Context, t templatePair, to, subject string, data teamData) error {
	var text, html bytes.Buffer
	if err := t.text.Execute(&text, data); err != nil {
		return fmt.Errorf("montando e-mail (texto): %w", err)
	}
	if err := t.html.ExecuteTemplate(&html, "layout", data); err != nil {
		return fmt.Errorf("montando e-mail (HTML): %w", err)
	}
	return m.sender.Send(ctx, Message{To: to, Subject: subject, Text: text.String(), HTML: html.String()})
}

// MemberInvited: a pessoa ainda não tinha conta. O link cria a senha.
func (m *TeamMailer) MemberInvited(ctx context.Context, to string, n TeamNotice, token string, ttl time.Duration) error {
	data := m.data(n)
	data.ActionURL = m.appURL + ResetPath + "#token=" + url.QueryEscape(token) + "&boasvindas=1"
	data.Validity = humanizeDuration(ttl)
	return m.send(ctx, memberInviteTemplates, to, data.Owner+" adicionou você à equipe na Farbo — crie sua senha", data)
}

// MemberAdded: a pessoa já tinha conta; a conta do titular passa a aparecer
// no login dela.
func (m *TeamMailer) MemberAdded(ctx context.Context, to string, n TeamNotice) error {
	data := m.data(n)
	data.ActionURL = m.appURL + "/login"
	return m.send(ctx, memberAddedTemplates, to, data.Owner+" adicionou você à equipe na Farbo", data)
}

const memberInviteHTML = `{{define "content"}}
<p style="margin:0 0 8px;font-size:12px;font-weight:800;letter-spacing:3px;text-transform:uppercase;color:#15803d;">Conta empresa</p>
<h1 style="margin:0 0 16px;font-size:22px;line-height:1.3;color:#0d130e;">{{.Owner}} adicionou você à equipe</h1>
<p style="margin:0 0 12px;font-size:15px;line-height:1.6;color:#334155;">Olá{{with .Name}}, {{.}}{{end}}!</p>
<p style="margin:0;font-size:15px;line-height:1.6;color:#334155;">Você entrou na equipe da conta de <strong>{{.Owner}}</strong> na Farbo Rastreadores como <strong>{{.RoleLabel}}</strong>: vai poder {{.What}}. Para começar, crie a sua senha:</p>
{{template "button" (button .ActionURL "Criar minha senha")}}
<p style="margin:0 0 20px;font-size:14px;line-height:1.6;color:#475569;">O link vale por <strong>{{.Validity}}</strong>. Depois disso, use "Esqueci minha senha" na tela de login para receber outro.</p>
<p style="margin:0;font-size:12px;line-height:1.6;color:#64748b;">Não conhece {{.Owner}}? Ignore este e-mail. Se o botão não funcionar, copie e cole este endereço no navegador:<br><a href="{{.ActionURL}}" style="color:#15803d;word-break:break-all;">{{.ActionURL}}</a></p>
{{end}}`

const memberInviteText = `Olá{{with .Name}}, {{.}}{{end}}!

{{.Owner}} adicionou você à equipe da conta na Farbo Rastreadores como {{.RoleLabel}}: você vai poder {{.What}}.

Para começar, crie a sua senha neste link:
{{.ActionURL}}

O link vale por {{.Validity}}. Depois disso, use "Esqueci minha senha" na tela de login para receber outro.

Não conhece {{.Owner}}? Ignore este e-mail.

— Farbo Rastreadores
`

const memberAddedHTML = `{{define "content"}}
<p style="margin:0 0 8px;font-size:12px;font-weight:800;letter-spacing:3px;text-transform:uppercase;color:#15803d;">Conta empresa</p>
<h1 style="margin:0 0 16px;font-size:22px;line-height:1.3;color:#0d130e;">{{.Owner}} adicionou você à equipe</h1>
<p style="margin:0 0 12px;font-size:15px;line-height:1.6;color:#334155;">Olá{{with .Name}}, {{.}}{{end}}!</p>
<p style="margin:0;font-size:15px;line-height:1.6;color:#334155;">Você entrou na equipe da conta de <strong>{{.Owner}}</strong> como <strong>{{.RoleLabel}}</strong>: agora pode {{.What}}. Entre com o seu e-mail e a sua senha de sempre.</p>
{{template "button" (button .ActionURL "Entrar")}}
{{end}}`

const memberAddedText = `Olá{{with .Name}}, {{.}}{{end}}!

{{.Owner}} adicionou você à equipe da conta na Farbo Rastreadores como {{.RoleLabel}}: agora você pode {{.What}}.

Entre com o seu e-mail e a sua senha de sempre:
{{.ActionURL}}

— Farbo Rastreadores
`

var (
	memberInviteTemplates = mustTemplates(memberInviteText, memberInviteHTML)
	memberAddedTemplates  = mustTemplates(memberAddedText, memberAddedHTML)
)
