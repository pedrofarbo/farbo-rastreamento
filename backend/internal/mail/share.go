package mail

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// ShareMailer escreve os e-mails dos acessos de terceiros a um veículo.
type ShareMailer struct {
	sender Sender
	appURL string
}

func NewShareMailer(sender Sender, appURL string) *ShareMailer {
	return &ShareMailer{sender: sender, appURL: strings.TrimRight(appURL, "/")}
}

// ShareNotice é o acesso como os e-mails contam.
type ShareNotice struct {
	VehicleID    string
	VehicleName  string
	VehiclePlate string
	OwnerName    string
	GuestName    string
	GuestEmail   string
	CanBlock     bool
	// At é a hora do bloqueio (só no aviso de bloqueio).
	At time.Time
}

type shareData struct {
	ShareNotice
	Name      string
	Owner     string
	Vehicle   string
	AppURL    string
	ActionURL string
	Validity  string
	When      string
}

func (m *ShareMailer) data(n ShareNotice, greet string) shareData {
	vehicle := n.VehicleName
	if n.VehiclePlate != "" {
		vehicle += " (" + n.VehiclePlate + ")"
	}
	owner := strings.TrimSpace(n.OwnerName)
	if owner == "" {
		owner = "Um cliente da Farbo"
	}
	return shareData{ShareNotice: n, Name: firstName(greet), Owner: owner, Vehicle: vehicle, AppURL: m.appURL}
}

func (m *ShareMailer) send(ctx context.Context, t templatePair, to, subject string, data shareData) error {
	var text, html bytes.Buffer
	if err := t.text.Execute(&text, data); err != nil {
		return fmt.Errorf("montando e-mail (texto): %w", err)
	}
	if err := t.html.ExecuteTemplate(&html, "layout", data); err != nil {
		return fmt.Errorf("montando e-mail (HTML): %w", err)
	}
	return m.sender.Send(ctx, Message{To: to, Subject: subject, Text: text.String(), HTML: html.String()})
}

// GuestInvited: a pessoa ainda não tinha conta. O link cria a senha e já
// leva ao app.
func (m *ShareMailer) GuestInvited(ctx context.Context, to string, n ShareNotice, token string, ttl time.Duration) error {
	data := m.data(n, n.GuestName)
	data.ActionURL = m.appURL + ResetPath + "#token=" + url.QueryEscape(token) + "&boasvindas=1"
	data.Validity = humanizeDuration(ttl)
	return m.send(ctx, shareInviteTemplates, to, data.Owner+" compartilhou um veículo com você — crie sua senha", data)
}

// GuestAdded: a pessoa já tinha conta; o veículo aparece no app dela.
func (m *ShareMailer) GuestAdded(ctx context.Context, to string, n ShareNotice) error {
	data := m.data(n, n.GuestName)
	data.ActionURL = m.appURL + "/app/"
	return m.send(ctx, shareAddedTemplates, to, data.Owner+" compartilhou um veículo com você", data)
}

// OwnerShared é o aviso de segurança ao dono: se não foi ele, remove o
// acesso e troca a senha.
func (m *ShareMailer) OwnerShared(ctx context.Context, to string, n ShareNotice) error {
	data := m.data(n, n.OwnerName)
	data.ActionURL = m.appURL + "/meus-veiculos"
	return m.send(ctx, shareOwnerTemplates, to, "Acesso ao seu veículo "+n.VehicleName, data)
}

// GuestBlocked avisa o dono de que quem tem acesso bloqueou o motor.
func (m *ShareMailer) GuestBlocked(ctx context.Context, to string, n ShareNotice) error {
	data := m.data(n, n.OwnerName)
	data.ActionURL = m.appURL + "/app/veiculos/" + url.PathEscape(n.VehicleID)
	data.When = formatBrazilTime(n.At)
	return m.send(ctx, shareBlockedTemplates, to, "Motor bloqueado: "+n.VehicleName, data)
}

// O que a permissão dá, nos textos dos e-mails.
const shareWhatText = `acompanhar a localização ao vivo{{if .CanBlock}} e, numa emergência, bloquear o motor{{end}}`

const shareInviteHTML = `{{define "content"}}
<p style="margin:0 0 8px;font-size:12px;font-weight:800;letter-spacing:3px;text-transform:uppercase;color:#15803d;">Acesso compartilhado</p>
<h1 style="margin:0 0 16px;font-size:22px;line-height:1.3;color:#0d130e;">{{.Owner}} compartilhou {{.VehicleName}} com você</h1>
<p style="margin:0 0 12px;font-size:15px;line-height:1.6;color:#334155;">Olá{{with .Name}}, {{.}}{{end}}!</p>
<p style="margin:0;font-size:15px;line-height:1.6;color:#334155;">Com o app da Farbo Rastreadores você vai poder ` + shareWhatText + ` de <strong>{{.Vehicle}}</strong>. Para começar, crie a sua senha:</p>
{{template "button" (button .ActionURL "Criar minha senha")}}
<p style="margin:0 0 20px;font-size:14px;line-height:1.6;color:#475569;">O link vale por <strong>{{.Validity}}</strong>. Depois disso, use "Esqueci minha senha" na tela de login para receber outro.</p>
<p style="margin:0;font-size:12px;line-height:1.6;color:#64748b;">Não conhece {{.Owner}}? Ignore este e-mail. Se o botão não funcionar, copie e cole este endereço no navegador:<br><a href="{{.ActionURL}}" style="color:#15803d;word-break:break-all;">{{.ActionURL}}</a></p>
{{end}}`

const shareInviteText = `Olá{{with .Name}}, {{.}}{{end}}!

{{.Owner}} compartilhou {{.Vehicle}} com você. Com o app da Farbo Rastreadores você vai poder ` + shareWhatText + `.

Para começar, crie a sua senha neste link:
{{.ActionURL}}

O link vale por {{.Validity}}. Depois disso, use "Esqueci minha senha" na tela de login para receber outro.

Não conhece {{.Owner}}? Ignore este e-mail.

— Farbo Rastreadores
`

const shareAddedHTML = `{{define "content"}}
<p style="margin:0 0 8px;font-size:12px;font-weight:800;letter-spacing:3px;text-transform:uppercase;color:#15803d;">Acesso compartilhado</p>
<h1 style="margin:0 0 16px;font-size:22px;line-height:1.3;color:#0d130e;">{{.Owner}} compartilhou {{.VehicleName}} com você</h1>
<p style="margin:0 0 12px;font-size:15px;line-height:1.6;color:#334155;">Olá{{with .Name}}, {{.}}{{end}}!</p>
<p style="margin:0;font-size:15px;line-height:1.6;color:#334155;">Agora você pode ` + shareWhatText + ` de <strong>{{.Vehicle}}</strong>. Ele já aparece no seu app, junto com os seus veículos.</p>
{{template "button" (button .ActionURL "Abrir o app")}}
{{end}}`

const shareAddedText = `Olá{{with .Name}}, {{.}}{{end}}!

{{.Owner}} compartilhou {{.Vehicle}} com você. Agora você pode ` + shareWhatText + `. Ele já aparece no seu app:
{{.ActionURL}}

— Farbo Rastreadores
`

const shareOwnerHTML = `{{define "content"}}
<p style="margin:0 0 8px;font-size:12px;font-weight:800;letter-spacing:3px;text-transform:uppercase;color:#15803d;">Segurança</p>
<h1 style="margin:0 0 16px;font-size:22px;line-height:1.3;color:#0d130e;">{{.GuestName}} tem acesso a {{.VehicleName}}</h1>
<p style="margin:0 0 12px;font-size:15px;line-height:1.6;color:#334155;">Olá{{with .Name}}, {{.}}{{end}}!</p>
<p style="margin:0 0 12px;font-size:15px;line-height:1.6;color:#334155;"><strong>{{.GuestName}}</strong> ({{.GuestEmail}}) pode ` + shareWhatText + ` de <strong>{{.Vehicle}}</strong>.</p>
<p style="margin:0;font-size:15px;line-height:1.6;color:#334155;">Se não foi você quem deu esse acesso, remova agora em Meus veículos e troque a sua senha.</p>
{{template "button" (button .ActionURL "Ver os acessos")}}
{{end}}`

const shareOwnerText = `Olá{{with .Name}}, {{.}}{{end}}!

{{.GuestName}} ({{.GuestEmail}}) pode ` + shareWhatText + ` de {{.Vehicle}}.

Se não foi você quem deu esse acesso, remova agora em Meus veículos e troque a sua senha:
{{.ActionURL}}

— Farbo Rastreadores
`

const shareBlockedHTML = `{{define "content"}}
<p style="margin:0 0 8px;font-size:12px;font-weight:800;letter-spacing:3px;text-transform:uppercase;color:#b91c1c;">Bloqueio do motor</p>
<h1 style="margin:0 0 16px;font-size:22px;line-height:1.3;color:#0d130e;">{{.GuestName}} bloqueou {{.VehicleName}}</h1>
<p style="margin:0 0 12px;font-size:15px;line-height:1.6;color:#334155;">Olá{{with .Name}}, {{.}}{{end}}!</p>
<p style="margin:0 0 12px;font-size:15px;line-height:1.6;color:#334155;"><strong>{{.GuestName}}</strong> pediu o bloqueio do motor de <strong>{{.Vehicle}}</strong>{{with .When}} em {{.}}{{end}}, pelo acesso de emergência que você deu.</p>
<p style="margin:0;font-size:15px;line-height:1.6;color:#334155;">Para desbloquear, use o app, com a sua biometria ou senha.</p>
{{template "button" (button .ActionURL "Ver o veículo")}}
{{end}}`

const shareBlockedText = `Olá{{with .Name}}, {{.}}{{end}}!

{{.GuestName}} pediu o bloqueio do motor de {{.Vehicle}}{{with .When}} em {{.}}{{end}}, pelo acesso de emergência que você deu.

Para desbloquear, use o app, com a sua biometria ou senha:
{{.ActionURL}}

— Farbo Rastreadores
`

var (
	shareInviteTemplates  = mustTemplates(shareInviteText, shareInviteHTML)
	shareAddedTemplates   = mustTemplates(shareAddedText, shareAddedHTML)
	shareOwnerTemplates   = mustTemplates(shareOwnerText, shareOwnerHTML)
	shareBlockedTemplates = mustTemplates(shareBlockedText, shareBlockedHTML)
)
