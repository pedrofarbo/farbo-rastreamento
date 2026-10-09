package mail

import (
	"bytes"
	"context"
	"fmt"
	htmltemplate "html/template"
	"net/url"
	"strings"
	texttemplate "text/template"
	"time"
)

// ResetPath é a rota do painel que recebe o link de redefinição. O token vai
// no fragmento (#), que o navegador nunca manda ao servidor: assim ele não
// aparece em log de proxy nem em cabeçalho Referer.
const ResetPath = "/redefinir-senha"

// AccountMailer escreve e envia os e-mails da conta do usuário.
type AccountMailer struct {
	sender Sender
	appURL string
}

func NewAccountMailer(sender Sender, appURL string) *AccountMailer {
	return &AccountMailer{sender: sender, appURL: strings.TrimRight(appURL, "/")}
}

// PasswordReset manda o link de redefinição de senha.
func (m *AccountMailer) PasswordReset(ctx context.Context, to, name, token string, ttl time.Duration) error {
	data := accountData{
		Name:      firstName(name),
		AppURL:    m.appURL,
		ActionURL: m.appURL + ResetPath + "#token=" + url.QueryEscape(token),
		Validity:  humanizeDuration(ttl),
	}
	msg, err := render(resetTemplates, data)
	if err != nil {
		return err
	}
	msg.To, msg.ToName = to, name
	msg.Subject = "Redefinição de senha — Farbo Rastreadores"
	return m.sender.Send(ctx, msg)
}

// teamRoles: o perfil de quem é da equipe, como o convite apresenta.
var teamRoles = map[string]struct{ Label, Access string }{
	"admin":    {"Administrador", "acesso completo: clientes, cobranças, rastreadores, prestadores e usuários"},
	"operator": {"Operador", "acompanha a frota, envia comandos aos veículos e cuida dos pedidos e do atendimento"},
	"viewer":   {"Visualização", "acompanha a frota, os eventos e as cercas, sem alterar nada"},
}

// Invite manda o link para o usuário novo criar a senha: as boas-vindas ao
// cliente, ou, para alguém da equipe (role), o acesso com o perfil dele. O
// link é o mesmo da redefinição; o parâmetro boasvindas só muda os textos da
// tela.
func (m *AccountMailer) Invite(ctx context.Context, to, name, role, token string, ttl time.Duration) error {
	data := accountData{
		Name:      firstName(name),
		AppURL:    m.appURL,
		ActionURL: m.appURL + ResetPath + "#token=" + url.QueryEscape(token) + "&boasvindas=1",
		Validity:  humanizeDuration(ttl),
	}
	templates, subject := inviteTemplates, "Bem-vindo à Farbo Rastreadores — crie sua senha"
	if team, ok := teamRoles[role]; ok {
		data.Role, data.Access = team.Label, team.Access
		templates, subject = teamInviteTemplates, "Seu acesso à equipe da Farbo Rastreadores — crie sua senha"
	}
	msg, err := render(templates, data)
	if err != nil {
		return err
	}
	msg.To, msg.ToName = to, name
	msg.Subject = subject
	return m.sender.Send(ctx, msg)
}

// PasswordChanged avisa que a senha foi trocada, para o dono da conta
// perceber caso não tenha sido ele.
func (m *AccountMailer) PasswordChanged(ctx context.Context, to, name string, at time.Time) error {
	data := accountData{
		Name:      firstName(name),
		AppURL:    m.appURL,
		ActionURL: m.appURL + "/login",
		ChangedAt: formatBrazilTime(at),
	}
	msg, err := render(changedTemplates, data)
	if err != nil {
		return err
	}
	msg.To, msg.ToName = to, name
	msg.Subject = "Sua senha foi alterada — Farbo Rastreadores"
	return m.sender.Send(ctx, msg)
}

type accountData struct {
	Name      string
	AppURL    string
	ActionURL string
	Validity  string
	ChangedAt string
	// Role e Access: o perfil de quem é da equipe e o que ele permite.
	Role   string
	Access string
}

type templatePair struct {
	text *texttemplate.Template
	html *htmltemplate.Template
}

func render(t templatePair, data accountData) (Message, error) {
	var text, html bytes.Buffer
	if err := t.text.Execute(&text, data); err != nil {
		return Message{}, fmt.Errorf("montando e-mail (texto): %w", err)
	}
	if err := t.html.ExecuteTemplate(&html, "layout", data); err != nil {
		return Message{}, fmt.Errorf("montando e-mail (HTML): %w", err)
	}
	return Message{Text: text.String(), HTML: html.String()}, nil
}

// firstName usa só o primeiro nome na saudação; sem nome, fica "Olá!".
func firstName(name string) string {
	if fields := strings.Fields(name); len(fields) > 0 {
		return fields[0]
	}
	return ""
}

// humanizeDuration escreve a validade do link por extenso: "3 dias",
// "1 hora", "30 minutos".
func humanizeDuration(d time.Duration) string {
	const day = 24 * time.Hour
	if d >= day && d%day == 0 {
		return plural(int(d/day), "dia", "dias")
	}
	if d >= time.Hour && d%time.Hour == 0 {
		return plural(int(d/time.Hour), "hora", "horas")
	}
	minutes := int(d.Round(time.Minute) / time.Minute)
	if minutes < 1 {
		minutes = 1
	}
	return plural(minutes, "minuto", "minutos")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// formatBrazilTime escreve o horário de Brasília. O Brasil não tem mais
// horário de verão, então UTC-3 fixo serve se o tzdata faltar.
func formatBrazilTime(t time.Time) string {
	location, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		location = time.FixedZone("BRT", -3*60*60)
	}
	return t.In(location).Format("02/01/2006 às 15:04") + " (horário de Brasília)"
}

// ---------------------------------------------------------------------------
// Modelos
// ---------------------------------------------------------------------------

// layoutHTML é a moldura comum: faixa escura com a logo, cartão branco e
// botão verde, nas cores da landing. Tabelas e estilo inline porque é o que
// os clientes de e-mail respeitam. A faixa do topo é uma imagem só, com o
// fundo escuro dentro (email-header.png): no modo escuro, o Gmail do iPhone
// inverte as cores do HTML — o fundo escuro viraria claro e a logo branca
// sumiria —, mas não mexe nas imagens.
const layoutHTML = `{{define "layout"}}<!DOCTYPE html>
<html lang="pt-BR">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="light">
<title>Farbo Rastreadores</title>
</head>
<body style="margin:0;padding:0;background:#f3f6f4;font-family:Inter,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;color:#0d130e;">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="background:#f3f6f4;padding:32px 16px;">
<tr><td align="center">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="max-width:560px;border-radius:16px;overflow:hidden;background:#ffffff;border:1px solid #dce4de;">
<tr><td bgcolor="#060907" style="background:#060907;padding:0;font-size:0;line-height:0;">
<img src="{{.AppURL}}/assets/email-header.png" width="560" alt="Farbo Rastreadores" style="display:block;width:100%;max-width:560px;height:auto;border:0;color:#ffffff;font-size:18px;font-weight:800;line-height:84px;">
</td></tr>
<tr><td style="padding:32px;">
{{template "content" .}}
</td></tr>
<tr><td style="padding:20px 32px;background:#f8faf9;border-top:1px solid #e8eee9;font-size:12px;line-height:1.6;color:#64748b;">
Você recebeu este e-mail porque há uma conta com este endereço no painel da Farbo Rastreadores. Esta caixa não recebe respostas.
</td></tr>
</table>
</td></tr>
</table>
</body>
</html>{{end}}

{{define "button"}}<table role="presentation" cellpadding="0" cellspacing="0" style="margin:28px 0;">
<tr><td style="border-radius:999px;background:#3be558;">
<a href="{{.URL}}" style="display:inline-block;padding:14px 28px;font-size:15px;font-weight:700;color:#060907;text-decoration:none;border-radius:999px;">{{.Label}}</a>
</td></tr>
</table>{{end}}`

const resetHTML = `{{define "content"}}
<p style="margin:0 0 8px;font-size:12px;font-weight:800;letter-spacing:3px;text-transform:uppercase;color:#15803d;">Área do cliente</p>
<h1 style="margin:0 0 16px;font-size:22px;line-height:1.3;color:#0d130e;">Redefinição de senha</h1>
<p style="margin:0 0 12px;font-size:15px;line-height:1.6;color:#334155;">Olá{{with .Name}}, {{.}}{{end}}!</p>
<p style="margin:0;font-size:15px;line-height:1.6;color:#334155;">Recebemos um pedido para redefinir a senha da sua conta. Clique no botão abaixo para criar uma nova senha.</p>
{{template "button" (button .ActionURL "Criar nova senha")}}
<p style="margin:0 0 12px;font-size:14px;line-height:1.6;color:#475569;">O link vale por <strong>{{.Validity}}</strong> e só pode ser usado uma vez.</p>
<p style="margin:0 0 20px;font-size:14px;line-height:1.6;color:#475569;">Se você não pediu a redefinição, pode ignorar este e-mail: sua senha continua a mesma.</p>
<p style="margin:0;font-size:12px;line-height:1.6;color:#64748b;">Se o botão não funcionar, copie e cole este endereço no navegador:<br><a href="{{.ActionURL}}" style="color:#15803d;word-break:break-all;">{{.ActionURL}}</a></p>
{{end}}`

const resetText = `Olá{{with .Name}}, {{.}}{{end}}!

Recebemos um pedido para redefinir a senha da sua conta no painel da Farbo Rastreadores.

Para criar uma nova senha, abra o link abaixo:
{{.ActionURL}}

O link vale por {{.Validity}} e só pode ser usado uma vez.

Se você não pediu a redefinição, pode ignorar este e-mail: sua senha continua a mesma.

— Farbo Rastreadores
`

const changedHTML = `{{define "content"}}
<p style="margin:0 0 8px;font-size:12px;font-weight:800;letter-spacing:3px;text-transform:uppercase;color:#15803d;">Segurança da conta</p>
<h1 style="margin:0 0 16px;font-size:22px;line-height:1.3;color:#0d130e;">Sua senha foi alterada</h1>
<p style="margin:0 0 12px;font-size:15px;line-height:1.6;color:#334155;">Olá{{with .Name}}, {{.}}{{end}}!</p>
<p style="margin:0 0 12px;font-size:15px;line-height:1.6;color:#334155;">A senha da sua conta foi alterada em <strong>{{.ChangedAt}}</strong>.</p>
<p style="margin:0;font-size:15px;line-height:1.6;color:#334155;">Por segurança, encerramos as sessões abertas em outros aparelhos. Entre de novo com a nova senha.</p>
{{template "button" (button .ActionURL "Entrar no painel")}}
<p style="margin:0;font-size:14px;line-height:1.6;color:#475569;"><strong>Não foi você?</strong> Peça uma nova redefinição de senha agora mesmo e fale com o nosso suporte.</p>
{{end}}`

const changedText = `Olá{{with .Name}}, {{.}}{{end}}!

A senha da sua conta no painel da Farbo Rastreadores foi alterada em {{.ChangedAt}}.

Por segurança, encerramos as sessões abertas em outros aparelhos. Entre de novo com a nova senha:
{{.ActionURL}}

Não foi você? Peça uma nova redefinição de senha agora mesmo e fale com o nosso suporte.

— Farbo Rastreadores
`

const inviteHTML = `{{define "content"}}
<p style="margin:0 0 8px;font-size:12px;font-weight:800;letter-spacing:3px;text-transform:uppercase;color:#15803d;">Área do cliente</p>
<h1 style="margin:0 0 16px;font-size:22px;line-height:1.3;color:#0d130e;">Bem-vindo à Farbo Rastreadores</h1>
<p style="margin:0 0 12px;font-size:15px;line-height:1.6;color:#334155;">Olá{{with .Name}}, {{.}}{{end}}!</p>
<p style="margin:0;font-size:15px;line-height:1.6;color:#334155;">Sua conta no painel foi criada. É por ele que você acompanha seus veículos em tempo real, vê o histórico de rotas e consulta suas faturas. Para começar, crie a sua senha:</p>
{{template "button" (button .ActionURL "Criar minha senha")}}
<p style="margin:0 0 12px;font-size:14px;line-height:1.6;color:#475569;">O link vale por <strong>{{.Validity}}</strong> e serve <strong>uma vez só</strong>, para criar a senha. Depois disso, use "Esqueci minha senha" na tela de login para receber outro.</p>
<p style="margin:0 0 20px;font-size:14px;line-height:1.6;color:#475569;">Com a senha criada, entre sempre por <a href="{{.AppURL}}/login" style="color:#15803d;">{{.AppURL}}/login</a> (ou pelo app instalado no celular), com o seu e-mail e a senha.</p>
<p style="margin:0;font-size:12px;line-height:1.6;color:#64748b;">Se o botão não funcionar, copie e cole este endereço no navegador:<br><a href="{{.ActionURL}}" style="color:#15803d;word-break:break-all;">{{.ActionURL}}</a></p>
{{end}}`

const inviteText = `Olá{{with .Name}}, {{.}}{{end}}!

Sua conta no painel da Farbo Rastreadores foi criada. É por ele que você acompanha seus veículos em tempo real, vê o histórico de rotas e consulta suas faturas.

Para começar, crie a sua senha neste link:
{{.ActionURL}}

O link vale por {{.Validity}} e serve uma vez só, para criar a senha. Depois disso, use "Esqueci minha senha" na tela de login para receber outro.

Com a senha criada, entre sempre por {{.AppURL}}/login (ou pelo app instalado no celular), com o seu e-mail e a senha.

— Farbo Rastreadores
`

const teamInviteHTML = `{{define "content"}}
<p style="margin:0 0 8px;font-size:12px;font-weight:800;letter-spacing:3px;text-transform:uppercase;color:#15803d;">Equipe Farbo</p>
<h1 style="margin:0 0 16px;font-size:22px;line-height:1.3;color:#0d130e;">Seu acesso ao painel</h1>
<p style="margin:0 0 12px;font-size:15px;line-height:1.6;color:#334155;">Olá{{with .Name}}, {{.}}{{end}}!</p>
<p style="margin:0;font-size:15px;line-height:1.6;color:#334155;">Você foi adicionado à equipe da Farbo Rastreadores com o perfil <strong>{{.Role}}</strong> ({{.Access}}). Para começar, crie a sua senha:</p>
{{template "button" (button .ActionURL "Criar minha senha")}}
<p style="margin:0 0 20px;font-size:14px;line-height:1.6;color:#475569;">O link vale por <strong>{{.Validity}}</strong>. Depois disso, use "Esqueci minha senha" na tela de login para receber outro.</p>
<p style="margin:0;font-size:12px;line-height:1.6;color:#64748b;">Se o botão não funcionar, copie e cole este endereço no navegador:<br><a href="{{.ActionURL}}" style="color:#15803d;word-break:break-all;">{{.ActionURL}}</a></p>
{{end}}`

const teamInviteText = `Olá{{with .Name}}, {{.}}{{end}}!

Você foi adicionado à equipe da Farbo Rastreadores com o perfil {{.Role}} ({{.Access}}).

Para começar, crie a sua senha neste link:
{{.ActionURL}}

O link vale por {{.Validity}}. Depois disso, use "Esqueci minha senha" na tela de login para receber outro.

— Farbo Rastreadores
`

type buttonData struct {
	URL   string
	Label string
}

var (
	resetTemplates   = mustTemplates(resetText, resetHTML)
	changedTemplates = mustTemplates(changedText, changedHTML)
	inviteTemplates  = mustTemplates(inviteText, inviteHTML)
	// teamInviteTemplates: o convite de quem entra na equipe da central.
	teamInviteTemplates = mustTemplates(teamInviteText, teamInviteHTML)
)

func mustTemplates(text, html string) templatePair {
	funcs := htmltemplate.FuncMap{
		"button": func(url, label string) buttonData { return buttonData{URL: url, Label: label} },
	}
	return templatePair{
		text: texttemplate.Must(texttemplate.New("text").Parse(text)),
		html: htmltemplate.Must(htmltemplate.New("html").Funcs(funcs).Parse(layoutHTML + html)),
	}
}
