package mail

import (
	"bytes"
	"context"
	"fmt"
)

// ReportMailer envia o relatório da frota agendado (internal/reportmail).
type ReportMailer struct {
	sender Sender
	appURL string
}

func NewReportMailer(sender Sender, appURL string) *ReportMailer {
	return &ReportMailer{sender: sender, appURL: appURL}
}

// FleetReport é o relatório de um período: os totais, os veículos que mais
// rodaram e as planilhas anexas. Os números já vêm formatados.
type FleetReport struct {
	To   string
	Name string
	// Kind: "semanal" ou "mensal"; Period, o período ("06/10 a 12/10/2026",
	// "setembro de 2026"); Group, o grupo (vazio: a conta toda).
	Kind   string
	Period string
	Group  string
	// Vehicles: quantos veículos entraram; Active, quantos andaram.
	Vehicles   int
	Active     int
	Km         string
	Moving     string
	Trips      string
	Overspeeds int
	Top        []FleetReportLine
	// More: quantos veículos ficaram fora da lista (estão na planilha).
	More        int
	Attachments []Attachment
	// ForwardedFrom: o e-mail é um destinatário a mais; quem o incluiu.
	ForwardedFrom string
}

// FleetReportLine é um veículo da lista dos que mais rodaram.
type FleetReportLine struct {
	Name       string
	Plate      string
	Km         string
	Trips      int
	Overspeeds int
}

type fleetReportData struct {
	FleetReport
	FirstName   string
	AppURL      string
	ActionURL   string
	SettingsURL string
}

func (m *ReportMailer) FleetReport(ctx context.Context, r FleetReport) error {
	msg, err := renderFleetReport(r, m.appURL)
	if err != nil {
		return err
	}
	return m.sender.Send(ctx, msg)
}

func renderFleetReport(r FleetReport, appURL string) (Message, error) {
	data := fleetReportData{FleetReport: r, FirstName: firstName(r.Name), AppURL: appURL,
		ActionURL: appURL + "/relatorios", SettingsURL: appURL + "/relatorios"}
	var text, html bytes.Buffer
	if err := fleetReportTemplates.text.Execute(&text, data); err != nil {
		return Message{}, fmt.Errorf("montando o relatório da frota (texto): %w", err)
	}
	if err := fleetReportTemplates.html.ExecuteTemplate(&html, "layout", data); err != nil {
		return Message{}, fmt.Errorf("montando o relatório da frota (HTML): %w", err)
	}
	subject := fmt.Sprintf("Relatório %s da frota · %s", r.Kind, r.Period)
	if r.Group != "" {
		subject += " · " + r.Group
	}
	return Message{To: r.To, ToName: r.Name, Subject: subject, Text: text.String(), HTML: html.String(),
		Attachments: r.Attachments}, nil
}

const fleetReportHTML = `{{define "content"}}
<p style="margin:0 0 8px;font-size:12px;font-weight:800;letter-spacing:3px;text-transform:uppercase;color:#15803d;">Relatório {{.Kind}}</p>
<h1 style="margin:0 0 16px;font-size:22px;line-height:1.3;color:#0d130e;">Sua frota · {{.Period}}{{with .Group}} · {{.}}{{end}}</h1>
<p style="margin:0 0 20px;font-size:15px;line-height:1.6;color:#334155;">Olá{{with .FirstName}}, {{.}}{{end}}! {{if .ForwardedFrom}}{{.ForwardedFrom}} incluiu este e-mail para receber o relatório da frota. {{end}}Este é o resumo do período; a planilha completa, com todos os veículos, vai anexa.</p>
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="margin:0 0 16px;border:1px solid #e2e8f0;border-radius:12px;">
<tr><td style="padding:14px 16px;">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0">
<tr><td style="padding:4px 0;font-size:14px;color:#475569;">Veículos que andaram</td><td align="right" style="padding:4px 0;font-size:14px;font-weight:700;color:#0d130e;">{{.Active}} de {{.Vehicles}}</td></tr>
<tr><td style="padding:4px 0;font-size:14px;color:#475569;">Km rodados</td><td align="right" style="padding:4px 0;font-size:14px;font-weight:700;color:#0d130e;">{{.Km}}</td></tr>
<tr><td style="padding:4px 0;font-size:14px;color:#475569;">Tempo andando</td><td align="right" style="padding:4px 0;font-size:14px;font-weight:700;color:#0d130e;">{{.Moving}}</td></tr>
<tr><td style="padding:4px 0;font-size:14px;color:#475569;">Viagens</td><td align="right" style="padding:4px 0;font-size:14px;font-weight:700;color:#0d130e;">{{.Trips}}</td></tr>
<tr><td style="padding:4px 0;font-size:14px;color:#475569;">Excessos de velocidade</td><td align="right" style="padding:4px 0;font-size:14px;font-weight:700;color:{{if .Overspeeds}}#b91c1c{{else}}#0d130e{{end}};">{{.Overspeeds}}</td></tr>
</table>
</td></tr></table>
{{if .Top}}<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="margin:0 0 8px;border:1px solid #e2e8f0;border-radius:12px;">
<tr><td style="padding:14px 16px;">
<p style="margin:0 0 6px;font-size:13px;font-weight:700;color:#0d130e;">Os que mais rodaram</p>
<table role="presentation" width="100%" cellpadding="0" cellspacing="0">
<tr><td style="padding:4px 0;font-size:12px;color:#64748b;">Veículo</td><td align="right" style="padding:4px 0;font-size:12px;color:#64748b;">Km</td><td align="right" style="padding:4px 0;font-size:12px;color:#64748b;">Viagens</td><td align="right" style="padding:4px 0;font-size:12px;color:#64748b;">Excessos</td></tr>
{{range .Top}}<tr><td style="padding:4px 0;font-size:14px;color:#0d130e;">{{.Name}}{{with .Plate}} <span style="color:#64748b;font-size:12px;">{{.}}</span>{{end}}</td><td align="right" style="padding:4px 0;font-size:14px;font-weight:700;color:#0d130e;">{{.Km}}</td><td align="right" style="padding:4px 0;font-size:14px;color:#475569;">{{.Trips}}</td><td align="right" style="padding:4px 0;font-size:14px;color:{{if .Overspeeds}}#b91c1c{{else}}#475569{{end}};">{{.Overspeeds}}</td></tr>{{end}}
{{if .More}}<tr><td colspan="4" style="padding:4px 0;font-size:13px;color:#64748b;">e mais {{.More}} {{if eq .More 1}}veículo{{else}}veículos{{end}} na planilha</td></tr>{{end}}
</table>
</td></tr></table>{{end}}
{{template "button" (button .ActionURL "Ver os relatórios no painel")}}
<p style="margin:0;font-size:12px;line-height:1.6;color:#64748b;">{{if .ForwardedFrom}}Para deixar de receber, peça a {{.ForwardedFrom}}.{{else}}Para mudar ou parar o envio, use Receber por e-mail, em <a href="{{.SettingsURL}}" style="color:#15803d;">Relatórios</a>, no painel.{{end}}</p>
{{end}}`

const fleetReportText = `Olá{{with .FirstName}}, {{.}}{{end}}!
{{if .ForwardedFrom}}
{{.ForwardedFrom}} incluiu este e-mail para receber o relatório da frota.
{{end}}
Relatório {{.Kind}} da frota · {{.Period}}{{with .Group}} · {{.}}{{end}}. A planilha completa, com todos os veículos, vai anexa.

Veículos que andaram: {{.Active}} de {{.Vehicles}}
Km rodados: {{.Km}}
Tempo andando: {{.Moving}}
Viagens: {{.Trips}}
Excessos de velocidade: {{.Overspeeds}}
{{if .Top}}
Os que mais rodaram:
{{range .Top}}- {{.Name}}{{with .Plate}} ({{.}}){{end}}: {{.Km}} km, {{.Trips}} {{if eq .Trips 1}}viagem{{else}}viagens{{end}}{{if .Overspeeds}}, {{.Overspeeds}} {{if eq .Overspeeds 1}}excesso{{else}}excessos{{end}}{{end}}
{{end}}{{if .More}}- e mais {{.More}} {{if eq .More 1}}veículo{{else}}veículos{{end}} na planilha
{{end}}{{end}}
Ver os relatórios no painel: {{.ActionURL}}
{{if .ForwardedFrom}}Para deixar de receber, peça a {{.ForwardedFrom}}.{{else}}Para mudar ou parar o envio: Relatórios → Receber por e-mail ({{.SettingsURL}}).{{end}}

— Farbo Rastreadores
`

var fleetReportTemplates = mustTemplates(fleetReportText, fleetReportHTML)
