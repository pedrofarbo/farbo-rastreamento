package mail

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"
)

// InstallMailer avisa da instalação parada: o horário passou sem a chegada,
// ou o instalador chegou e não concluiu — o rastreador só fica ligado ao
// veículo depois de "Concluir" no link.
type InstallMailer struct {
	sender Sender
	appURL string
}

func NewInstallMailer(sender Sender, appURL string) *InstallMailer {
	return &InstallMailer{sender: sender, appURL: strings.TrimRight(appURL, "/")}
}

// StalledInstall é o que o aviso mostra (os horários já no fuso da central).
type StalledInstall struct {
	// Late: o horário passou e o link não registrou a chegada; sem isso, o
	// instalador chegou e não concluiu.
	Late bool
	// What: "Instalação" ou "Desinstalação".
	What          string
	VehicleName   string
	Plate         string
	CustomerName  string
	InstallerName string
	ScheduledFor  time.Time
	ArrivedAt     *time.Time
	// Done e Missing: o que já foi feito e o que falta no link.
	Done    []string
	Missing []string
	// ForInstaller: o e-mail vai a quem instala (o representante), com o
	// link do instalador; sem isso, à central, com a agenda.
	ForInstaller bool
	ActionURL    string
	ActionLabel  string
}

type stalledData struct {
	StalledInstall
	AppURL   string
	Vehicle  string
	Headline string
	Body     string
	DoneText string
	TodoText string
}

var weekdays = [...]string{"dom.", "seg.", "ter.", "qua.", "qui.", "sex.", "sáb."}

// InstallStalled manda o aviso a um endereço.
func (m *InstallMailer) InstallStalled(ctx context.Context, to string, n StalledInstall) error {
	vehicle := n.VehicleName
	if n.Plate != "" {
		vehicle += " · " + n.Plate
	}
	what := strings.ToLower(n.What)
	data := stalledData{StalledInstall: n, AppURL: m.appURL, Vehicle: vehicle,
		DoneText: strings.Join(n.Done, ", "), TodoText: strings.Join(n.Missing, ", ")}
	when := fmt.Sprintf("%s %s", weekdays[n.ScheduledFor.Weekday()], n.ScheduledFor.Format("02/01 às 15:04"))
	switch {
	case n.Late:
		data.Headline = "A " + what + " não começou pelo link"
		data.Body = fmt.Sprintf("A %s estava marcada para %s e o link do instalador não registrou a chegada.", what, when)
	case n.ArrivedAt != nil:
		data.Headline = "A " + what + " não foi concluída"
		data.Body = fmt.Sprintf("O instalador chegou %s às %s e a %s ainda não foi concluída no link.", dayWord(*n.ArrivedAt, n.ScheduledFor),
			n.ArrivedAt.Format("15:04"), what)
	default:
		data.Headline = "A " + what + " não foi concluída"
		data.Body = fmt.Sprintf("A %s marcada para %s ainda não foi concluída no link.", what, when)
	}
	var text, html bytes.Buffer
	if err := stalledTemplates.text.Execute(&text, data); err != nil {
		return fmt.Errorf("montando e-mail (texto): %w", err)
	}
	if err := stalledTemplates.html.ExecuteTemplate(&html, "layout", data); err != nil {
		return fmt.Errorf("montando e-mail (HTML): %w", err)
	}
	return m.sender.Send(ctx, Message{
		To: to, Subject: n.What + " parada: " + vehicle, Text: text.String(), HTML: html.String(),
	})
}

// dayWord: "em 16/11" quando a chegada não foi no dia marcado (ou nada).
func dayWord(at, scheduled time.Time) string {
	if at.Format("2006-01-02") == scheduled.Format("2006-01-02") {
		return "no dia marcado"
	}
	return "em " + at.Format("02/01")
}

const stalledHTML = `{{define "content"}}
<p style="margin:0 0 8px;font-size:12px;font-weight:800;letter-spacing:3px;text-transform:uppercase;color:#b45309;">{{.What}} parada</p>
<h1 style="margin:0 0 16px;font-size:22px;line-height:1.3;color:#0d130e;">{{.Headline}}</h1>
<p style="margin:0 0 4px;font-size:15px;line-height:1.6;color:#334155;"><strong>{{.Vehicle}}</strong>{{with .CustomerName}} · {{.}}{{end}}</p>
{{with .InstallerName}}<p style="margin:0 0 12px;font-size:15px;line-height:1.6;color:#334155;">Instalador: {{.}}</p>{{end}}
<p style="margin:0 0 12px;font-size:15px;line-height:1.6;color:#334155;">{{.Body}}</p>
{{with .DoneText}}<p style="margin:0 0 4px;font-size:15px;line-height:1.6;color:#334155;">Já feito: {{.}}.</p>{{end}}
{{with .TodoText}}<p style="margin:0 0 12px;font-size:15px;line-height:1.6;color:#334155;">Falta: {{.}}.</p>{{end}}
<p style="margin:0 0 4px;font-size:15px;line-height:1.6;color:#334155;">O rastreador só fica ligado ao veículo, e o cliente só o vê no app, depois de “Concluir” no link.{{if .ForInstaller}} Se já foi feita, abra o link e termine os passos que faltam.{{end}}</p>
{{template "button" (button .ActionURL .ActionLabel)}}
{{end}}`

const stalledText = `{{.Headline}}

{{.Vehicle}}{{with .CustomerName}} · {{.}}{{end}}
{{with .InstallerName}}Instalador: {{.}}
{{end}}
{{.Body}}
{{with .DoneText}}Já feito: {{.}}.
{{end}}{{with .TodoText}}Falta: {{.}}.
{{end}}
O rastreador só fica ligado ao veículo, e o cliente só o vê no app, depois de "Concluir" no link.{{if .ForInstaller}} Se já foi feita, abra o link e termine os passos que faltam.{{end}}

{{.ActionLabel}}:
{{.ActionURL}}

— Farbo Rastreadores
`

var stalledTemplates = mustTemplates(stalledText, stalledHTML)
