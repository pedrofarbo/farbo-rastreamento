package mail

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// TheftMailer escreve os e-mails do modo roubo: para o dono e para quem tem
// acesso ao veículo (sem central, são eles que agem).
type TheftMailer struct {
	sender Sender
	appURL string
}

func NewTheftMailer(sender Sender, appURL string) *TheftMailer {
	return &TheftMailer{sender: sender, appURL: strings.TrimRight(appURL, "/")}
}

// TheftNotice é o modo roubo como os e-mails contam.
type TheftNotice struct {
	VehicleID    string
	VehicleName  string
	VehiclePlate string
	// ActorName: quem ativou ou encerrou (vazio: desligou sozinho).
	ActorName string
	// PublicURL: o link da posição ao vivo, sem login (para a polícia).
	PublicURL   string
	ActivatedAt time.Time
	ExpiresAt   time.Time
	// Recovered: encerrado porque o veículo foi recuperado.
	Recovered bool
}

type theftData struct {
	TheftNotice
	Name      string
	Vehicle   string
	Actor     string
	Since     string
	Until     string
	AppURL    string
	ActionURL string
}

func (m *TheftMailer) data(n TheftNotice, greet string) theftData {
	vehicle := n.VehicleName
	if n.VehiclePlate != "" {
		vehicle += " (" + n.VehiclePlate + ")"
	}
	actor := strings.TrimSpace(n.ActorName)
	if actor == "" {
		actor = "Alguém com acesso ao veículo"
	}
	d := theftData{
		TheftNotice: n, Name: firstName(greet), Vehicle: vehicle, Actor: actor, AppURL: m.appURL,
		ActionURL: m.appURL + "/app/veiculos/" + url.PathEscape(n.VehicleID),
	}
	if !n.ActivatedAt.IsZero() {
		d.Since = formatBrazilTime(n.ActivatedAt)
	}
	if !n.ExpiresAt.IsZero() {
		d.Until = formatBrazilTime(n.ExpiresAt)
	}
	return d
}

func (m *TheftMailer) send(ctx context.Context, t templatePair, to, subject string, data theftData) error {
	var text, html bytes.Buffer
	if err := t.text.Execute(&text, data); err != nil {
		return fmt.Errorf("montando e-mail (texto): %w", err)
	}
	if err := t.html.ExecuteTemplate(&html, "layout", data); err != nil {
		return fmt.Errorf("montando e-mail (HTML): %w", err)
	}
	return m.sender.Send(ctx, Message{To: to, Subject: subject, Text: text.String(), HTML: html.String()})
}

// TheftActivated: o modo roubo foi ligado. Vai para o dono e para quem tem
// acesso, com o link para a polícia e o que fazer.
func (m *TheftMailer) TheftActivated(ctx context.Context, to, name string, n TheftNotice) error {
	return m.send(ctx, theftActivatedTemplates, to, "Modo roubo ativado: "+n.VehicleName, m.data(n, name))
}

// TheftEnded: o modo roubo foi encerrado (recuperado ou desativado).
func (m *TheftMailer) TheftEnded(ctx context.Context, to, name string, n TheftNotice) error {
	subject := "Modo roubo desativado: " + n.VehicleName
	if n.Recovered {
		subject = "Veículo recuperado: " + n.VehicleName
	}
	return m.send(ctx, theftEndedTemplates, to, subject, m.data(n, name))
}

// TheftReminder: o modo roubo continua ligado (uma vez por dia, ao dono).
func (m *TheftMailer) TheftReminder(ctx context.Context, to, name string, n TheftNotice) error {
	return m.send(ctx, theftReminderTemplates, to, "O modo roubo continua ligado: "+n.VehicleName, m.data(n, name))
}

// TheftExpired: o prazo acabou e o modo roubo desligou sozinho.
func (m *TheftMailer) TheftExpired(ctx context.Context, to, name string, n TheftNotice) error {
	return m.send(ctx, theftExpiredTemplates, to, "O modo roubo desligou: "+n.VehicleName, m.data(n, name))
}

// O que fazer, nos e-mails de ativação.
const theftStepsHTML = `<ol style="margin:0 0 16px;padding-left:20px;font-size:15px;line-height:1.7;color:#334155;">
<li><strong>Ligue 190</strong> e informe onde o veículo está.</li>
<li>Mande à polícia o <strong>link da localização ao vivo</strong> (abre sem login).</li>
<li>Com o veículo parado, <strong>bloqueie o motor</strong> pelo app.</li>
<li>Registre o <strong>boletim de ocorrência</strong>: o relatório do trajeto sai no app.</li>
</ol>
<p style="margin:0 0 16px;font-size:14px;line-height:1.6;color:#b91c1c;"><strong>Não tente recuperar o veículo sozinho.</strong></p>`

const theftActivatedHTML = `{{define "content"}}
<p style="margin:0 0 8px;font-size:12px;font-weight:800;letter-spacing:3px;text-transform:uppercase;color:#b91c1c;">Modo roubo</p>
<h1 style="margin:0 0 16px;font-size:22px;line-height:1.3;color:#0d130e;">{{.VehicleName}} está em modo roubo</h1>
<p style="margin:0 0 12px;font-size:15px;line-height:1.6;color:#334155;">Olá{{with .Name}}, {{.}}{{end}}!</p>
<p style="margin:0 0 16px;font-size:15px;line-height:1.6;color:#334155;"><strong>{{.Actor}}</strong> ativou o modo roubo de <strong>{{.Vehicle}}</strong>{{with .Since}} em {{.}}{{end}}. O rastreador passa a mandar a posição com mais frequência, inclusive parado.</p>
` + theftStepsHTML + `
{{with .PublicURL}}<p style="margin:0 0 4px;font-size:14px;line-height:1.6;color:#475569;">Link da localização ao vivo (para a polícia):</p>
<p style="margin:0 0 8px;font-size:14px;line-height:1.6;"><a href="{{.}}" style="color:#15803d;word-break:break-all;">{{.}}</a></p>{{end}}
{{template "button" (button .ActionURL "Abrir o veículo no app")}}
{{with .Until}}<p style="margin:0;font-size:12px;line-height:1.6;color:#64748b;">O modo roubo desliga sozinho em {{.}}, se ninguém desligar antes. O link deixa de funcionar quando ele desligar.</p>{{end}}
{{end}}`

const theftActivatedText = `Olá{{with .Name}}, {{.}}{{end}}!

{{.Actor}} ativou o modo roubo de {{.Vehicle}}{{with .Since}} em {{.}}{{end}}. O rastreador passa a mandar a posição com mais frequência, inclusive parado.

O que fazer:
1. Ligue 190 e informe onde o veículo está.
2. Mande à polícia o link da localização ao vivo (abre sem login).
3. Com o veículo parado, bloqueie o motor pelo app.
4. Registre o boletim de ocorrência: o relatório do trajeto sai no app.

Não tente recuperar o veículo sozinho.
{{with .PublicURL}}
Link da localização ao vivo (para a polícia):
{{.}}
{{end}}
Abrir o veículo no app:
{{.ActionURL}}
{{with .Until}}
O modo roubo desliga sozinho em {{.}}, se ninguém desligar antes. O link deixa de funcionar quando ele desligar.
{{end}}
— Farbo Rastreadores
`

const theftEndedHTML = `{{define "content"}}
<p style="margin:0 0 8px;font-size:12px;font-weight:800;letter-spacing:3px;text-transform:uppercase;color:#15803d;">Modo roubo</p>
<h1 style="margin:0 0 16px;font-size:22px;line-height:1.3;color:#0d130e;">{{if .Recovered}}{{.VehicleName}} foi recuperado{{else}}O modo roubo de {{.VehicleName}} foi desativado{{end}}</h1>
<p style="margin:0 0 12px;font-size:15px;line-height:1.6;color:#334155;">Olá{{with .Name}}, {{.}}{{end}}!</p>
<p style="margin:0;font-size:15px;line-height:1.6;color:#334155;"><strong>{{.Actor}}</strong> {{if .Recovered}}marcou <strong>{{.Vehicle}}</strong> como recuperado{{else}}desativou o modo roubo de <strong>{{.Vehicle}}</strong>{{end}}. O rastreador volta ao intervalo normal e o link da localização deixou de funcionar.</p>
{{template "button" (button .ActionURL "Ver o veículo")}}
{{end}}`

const theftEndedText = `Olá{{with .Name}}, {{.}}{{end}}!

{{.Actor}} {{if .Recovered}}marcou {{.Vehicle}} como recuperado{{else}}desativou o modo roubo de {{.Vehicle}}{{end}}. O rastreador volta ao intervalo normal e o link da localização deixou de funcionar.

Ver o veículo:
{{.ActionURL}}

— Farbo Rastreadores
`

const theftReminderHTML = `{{define "content"}}
<p style="margin:0 0 8px;font-size:12px;font-weight:800;letter-spacing:3px;text-transform:uppercase;color:#b91c1c;">Modo roubo</p>
<h1 style="margin:0 0 16px;font-size:22px;line-height:1.3;color:#0d130e;">O modo roubo de {{.VehicleName}} continua ligado</h1>
<p style="margin:0 0 12px;font-size:15px;line-height:1.6;color:#334155;">Olá{{with .Name}}, {{.}}{{end}}!</p>
<p style="margin:0 0 12px;font-size:15px;line-height:1.6;color:#334155;">O modo roubo de <strong>{{.Vehicle}}</strong> está ligado{{with .Since}} desde {{.}}{{end}}: o rastreador manda a posição com mais frequência, o que gasta mais bateria.</p>
<p style="margin:0;font-size:15px;line-height:1.6;color:#334155;">Se o veículo já foi recuperado, marque no app. {{with .Until}}Se não, ele desliga sozinho em {{.}}.{{end}}</p>
{{template "button" (button .ActionURL "Abrir o veículo no app")}}
{{end}}`

const theftReminderText = `Olá{{with .Name}}, {{.}}{{end}}!

O modo roubo de {{.Vehicle}} está ligado{{with .Since}} desde {{.}}{{end}}: o rastreador manda a posição com mais frequência, o que gasta mais bateria.

Se o veículo já foi recuperado, marque no app. {{with .Until}}Se não, ele desliga sozinho em {{.}}.{{end}}
{{.ActionURL}}

— Farbo Rastreadores
`

const theftExpiredHTML = `{{define "content"}}
<p style="margin:0 0 8px;font-size:12px;font-weight:800;letter-spacing:3px;text-transform:uppercase;color:#b91c1c;">Modo roubo</p>
<h1 style="margin:0 0 16px;font-size:22px;line-height:1.3;color:#0d130e;">O modo roubo de {{.VehicleName}} desligou</h1>
<p style="margin:0 0 12px;font-size:15px;line-height:1.6;color:#334155;">Olá{{with .Name}}, {{.}}{{end}}!</p>
<p style="margin:0;font-size:15px;line-height:1.6;color:#334155;">O prazo do modo roubo de <strong>{{.Vehicle}}</strong> acabou e ele desligou sozinho, para poupar a bateria. O rastreador volta ao intervalo normal e o link da localização deixou de funcionar. Se o veículo ainda não foi recuperado, ative o modo roubo de novo no app.</p>
{{template "button" (button .ActionURL "Abrir o veículo no app")}}
{{end}}`

const theftExpiredText = `Olá{{with .Name}}, {{.}}{{end}}!

O prazo do modo roubo de {{.Vehicle}} acabou e ele desligou sozinho, para poupar a bateria. O rastreador volta ao intervalo normal e o link da localização deixou de funcionar.

Se o veículo ainda não foi recuperado, ative o modo roubo de novo no app:
{{.ActionURL}}

— Farbo Rastreadores
`

var (
	theftActivatedTemplates = mustTemplates(theftActivatedText, theftActivatedHTML)
	theftEndedTemplates     = mustTemplates(theftEndedText, theftEndedHTML)
	theftReminderTemplates  = mustTemplates(theftReminderText, theftReminderHTML)
	theftExpiredTemplates   = mustTemplates(theftExpiredText, theftExpiredHTML)
)
