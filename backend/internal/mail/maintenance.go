package mail

import (
	"bytes"
	"context"
	"fmt"
)

// MaintenanceMailer avisa das manutenções que ficaram perto ou venceram
// (internal/maintenance).
type MaintenanceMailer struct {
	sender Sender
	appURL string
}

func NewMaintenanceMailer(sender Sender, appURL string) *MaintenanceMailer {
	return &MaintenanceMailer{sender: sender, appURL: appURL}
}

// MaintenanceDigest são as manutenções de uma pessoa que pedem atenção.
type MaintenanceDigest struct {
	To    string
	Name  string
	Lines []MaintenanceLine
}

// MaintenanceLine é uma manutenção: o veículo, o que é e quanto falta (ou
// passou), já por extenso.
type MaintenanceLine struct {
	Vehicle string
	Plate   string
	Item    string
	Due     bool
	Detail  string
}

type maintenanceData struct {
	MaintenanceDigest
	FirstName string
	Due, Soon int
	AppURL    string
	ActionURL string
}

func (m *MaintenanceMailer) Digest(ctx context.Context, d MaintenanceDigest) error {
	msg, err := renderMaintenance(d, m.appURL)
	if err != nil {
		return err
	}
	return m.sender.Send(ctx, msg)
}

func renderMaintenance(d MaintenanceDigest, appURL string) (Message, error) {
	data := maintenanceData{MaintenanceDigest: d, FirstName: firstName(d.Name), AppURL: appURL, ActionURL: appURL + "/manutencao"}
	for _, l := range d.Lines {
		if l.Due {
			data.Due++
		} else {
			data.Soon++
		}
	}
	var text, html bytes.Buffer
	if err := maintenanceTemplates.text.Execute(&text, data); err != nil {
		return Message{}, fmt.Errorf("montando o aviso de manutenção (texto): %w", err)
	}
	if err := maintenanceTemplates.html.ExecuteTemplate(&html, "layout", data); err != nil {
		return Message{}, fmt.Errorf("montando o aviso de manutenção (HTML): %w", err)
	}
	var subject string
	switch {
	case data.Due > 0 && data.Soon > 0:
		vencidas := "vencidas"
		if data.Due == 1 {
			vencidas = "vencida"
		}
		subject = fmt.Sprintf("Manutenção: %d %s e %d perto de vencer", data.Due, vencidas, data.Soon)
	case data.Due == 1:
		subject = "Manutenção vencida: " + d.Lines[0].Item + " · " + d.Lines[0].Vehicle
	case data.Due > 1:
		subject = fmt.Sprintf("Manutenção: %d vencidas", data.Due)
	case data.Soon == 1:
		subject = "Manutenção perto de vencer: " + d.Lines[0].Item + " · " + d.Lines[0].Vehicle
	default:
		subject = fmt.Sprintf("Manutenção: %d perto de vencer", data.Soon)
	}
	return Message{To: d.To, ToName: d.Name, Subject: subject, Text: text.String(), HTML: html.String()}, nil
}

const maintenanceHTML = `{{define "content"}}
<p style="margin:0 0 8px;font-size:12px;font-weight:800;letter-spacing:3px;text-transform:uppercase;color:#b45309;">Manutenção</p>
<h1 style="margin:0 0 16px;font-size:22px;line-height:1.3;color:#0d130e;">{{if .Due}}{{.Due}} {{if eq .Due 1}}manutenção venceu{{else}}manutenções venceram{{end}}{{if .Soon}} e {{.Soon}} {{if eq .Soon 1}}está perto{{else}}estão perto{{end}}{{end}}{{else}}{{.Soon}} {{if eq .Soon 1}}manutenção está perto{{else}}manutenções estão perto{{end}} de vencer{{end}}</h1>
<p style="margin:0 0 20px;font-size:15px;line-height:1.6;color:#334155;">Olá{{with .FirstName}}, {{.}}{{end}}! Estas pedem atenção:</p>
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="margin:0 0 16px;border:1px solid #e2e8f0;border-radius:12px;">
<tr><td style="padding:14px 16px;">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0">
{{range .Lines}}<tr><td style="padding:6px 0;font-size:14px;color:#0d130e;"><strong>{{.Item}}</strong> · {{.Vehicle}}{{with .Plate}} <span style="color:#64748b;font-size:12px;">{{.}}</span>{{end}}<br><span style="font-size:13px;color:{{if .Due}}#b91c1c{{else}}#b45309{{end}};">{{.Detail}}</span></td></tr>{{end}}
</table>
</td></tr></table>
{{template "button" (button .ActionURL "Ver a manutenção da frota")}}
<p style="margin:0;font-size:12px;line-height:1.6;color:#64748b;">Depois de fazer, registre em Manutenção, no painel: a contagem recomeça dali. Cada manutenção é avisada uma vez quando fica perto e outra quando vence.</p>
{{end}}`

const maintenanceText = `Olá{{with .FirstName}}, {{.}}{{end}}!

Estas manutenções pedem atenção:
{{range .Lines}}
- {{.Item}} · {{.Vehicle}}{{with .Plate}} ({{.}}){{end}}: {{.Detail}}{{end}}

Ver a manutenção da frota: {{.ActionURL}}

Depois de fazer, registre em Manutenção, no painel: a contagem recomeça dali.

— Farbo Rastreadores
`

var maintenanceTemplates = mustTemplates(maintenanceText, maintenanceHTML)
