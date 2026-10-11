package mail

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
)

// AlertMailer envia os alertas dos veículos (internal/alerts).
type AlertMailer struct {
	sender Sender
}

func NewAlertMailer(sender Sender) *AlertMailer { return &AlertMailer{sender: sender} }

// Severidade do alerta: muda a cor da faixa do e-mail.
const (
	SeverityCritical = "critical"
	SeverityWarning  = "warning"
	SeverityInfo     = "info"
)

// Alert é o que o e-mail de alerta mostra. As frases já vêm prontas do
// pacote alerts; aqui só se monta a mensagem.
type Alert struct {
	To       string
	Name     string
	Subject  string
	Severity string
	// Title é a manchete ("Ignição ligada no horário de vigilância").
	Title string
	// Summary é a frase que explica o que aconteceu.
	Summary string
	Vehicle string
	Plate   string
	// When é a data e hora já formatadas no fuso da central.
	When string
	// Speed e Limit em km/h; nil quando não se aplicam.
	Speed *float64
	Limit *float64
	// Latitude/Longitude do evento, quando o rastreador mandou posição.
	Latitude  *float64
	Longitude *float64
	Address   string
	// Suppressed: ocorrências iguais seguradas desde o último e-mail.
	Suppressed int
	// Cooldown por extenso ("30 minutos"): explica o resumo dos repetidos.
	Cooldown    string
	ActionURL   string
	SettingsURL string
	AppURL      string
}

type alertData struct {
	Alert
	FirstName   string
	Accent      string
	Label       string
	SpeedText   string
	LimitText   string
	MapURL      string
	Coordinates string
}

func (m *AlertMailer) Alert(ctx context.Context, a Alert) error {
	msg, err := renderAlert(a)
	if err != nil {
		return err
	}
	return m.sender.Send(ctx, msg)
}

func renderAlert(a Alert) (Message, error) {
	d := alertData{Alert: a, FirstName: firstName(a.Name)}
	switch a.Severity {
	case SeverityCritical:
		d.Accent, d.Label = "#b3261e", "Alerta de segurança"
	case SeverityWarning:
		d.Accent, d.Label = "#b45309", "Alerta"
	default:
		d.Accent, d.Label = "#15803d", "Aviso"
	}
	if a.Speed != nil {
		d.SpeedText = strconv.FormatFloat(*a.Speed, 'f', 0, 64) + " km/h"
	}
	if a.Limit != nil {
		d.LimitText = strconv.FormatFloat(*a.Limit, 'f', 0, 64) + " km/h"
	}
	if a.Latitude != nil && a.Longitude != nil {
		lat, lon := *a.Latitude, *a.Longitude
		d.Coordinates = fmt.Sprintf("%.5f, %.5f", lat, lon)
		d.MapURL = fmt.Sprintf("https://www.openstreetmap.org/?mlat=%.6f&mlon=%.6f#map=17/%.6f/%.6f", lat, lon, lat, lon)
	}

	var text, html bytes.Buffer
	if err := alertTemplates.text.Execute(&text, d); err != nil {
		return Message{}, fmt.Errorf("montando e-mail de alerta (texto): %w", err)
	}
	if err := alertTemplates.html.ExecuteTemplate(&html, "layout", d); err != nil {
		return Message{}, fmt.Errorf("montando e-mail de alerta (HTML): %w", err)
	}
	return Message{To: a.To, ToName: a.Name, Subject: a.Subject, Text: text.String(), HTML: html.String()}, nil
}

const alertHTML = `{{define "content"}}
<p style="margin:0 0 8px;font-size:12px;font-weight:800;letter-spacing:3px;text-transform:uppercase;color:{{.Accent}};">{{.Label}}</p>
<h1 style="margin:0 0 16px;font-size:22px;line-height:1.3;color:#0d130e;">{{.Title}}</h1>
<p style="margin:0 0 12px;font-size:15px;line-height:1.6;color:#334155;">Olá{{with .FirstName}}, {{.}}{{end}}!</p>
<p style="margin:0 0 20px;font-size:15px;line-height:1.6;color:#334155;">{{.Summary}}</p>
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="border-left:4px solid {{.Accent}};background:#f8faf9;border-radius:8px;">
<tr><td style="padding:16px 20px;font-size:14px;line-height:1.8;color:#334155;">
<strong>Veículo:</strong> {{.Vehicle}}{{with .Plate}} ({{.}}){{end}}<br>
<strong>Quando:</strong> {{.When}}
{{- with .SpeedText}}<br><strong>Velocidade:</strong> {{.}}{{end}}
{{- with .LimitText}}<br><strong>Limite:</strong> {{.}}{{end}}
{{- if .Address}}<br><strong>Onde:</strong> {{.Address}}{{else if .Coordinates}}<br><strong>Onde:</strong> {{.Coordinates}}{{end}}
</td></tr>
</table>
{{if .Suppressed}}<p style="margin:16px 0 0;font-size:13px;line-height:1.6;color:#475569;">Este alerta se repetiu mais <strong>{{.Suppressed}}</strong> {{if eq .Suppressed 1}}vez{{else}}vezes{{end}} desde o último e-mail. Para não lotar sua caixa, o mesmo alerta do mesmo veículo sai no máximo uma vez a cada {{.Cooldown}}.</p>{{end}}
{{template "button" (button .ActionURL "Ver no painel")}}
{{if .MapURL}}<p style="margin:0 0 20px;font-size:14px;line-height:1.6;color:#475569;"><a href="{{.MapURL}}" style="color:#15803d;">Abrir o local no mapa</a></p>{{end}}
{{if .SettingsURL}}<p style="margin:0;font-size:12px;line-height:1.6;color:#64748b;">Você escolhe quais alertas recebe e o horário de vigilância em <a href="{{.SettingsURL}}" style="color:#15803d;">Alertas</a>, no painel.</p>{{end}}
{{end}}`

const alertText = `Olá{{with .FirstName}}, {{.}}{{end}}!

{{.Title}}
{{.Summary}}

Veículo: {{.Vehicle}}{{with .Plate}} ({{.}}){{end}}
Quando: {{.When}}
{{- with .SpeedText}}
Velocidade: {{.}}{{end}}
{{- with .LimitText}}
Limite: {{.}}{{end}}
{{- if .Address}}
Onde: {{.Address}}{{else if .Coordinates}}
Onde: {{.Coordinates}}{{end}}
{{- with .MapURL}}
Mapa: {{.}}{{end}}
{{if .Suppressed}}
Este alerta se repetiu mais {{.Suppressed}} {{if eq .Suppressed 1}}vez{{else}}vezes{{end}} desde o último e-mail. O mesmo alerta do mesmo veículo sai no máximo uma vez a cada {{.Cooldown}}.
{{end}}
Ver no painel: {{.ActionURL}}
{{with .SettingsURL}}
Escolha quais alertas receber em: {{.}}
{{end}}
— Farbo Rastreadores
`

var alertTemplates = mustTemplates(alertText, alertHTML)

// AlertDigest é o resumo dos alertas de um destinatário que passaram do
// teto por hora (numa frota, dezenas): quantos de cada tipo e de quais
// veículos, num e-mail só.
type AlertDigest struct {
	To    string
	Name  string
	Count int
	// Kinds e Vehicles vêm do mais frequente para o menos.
	Kinds        []DigestLine
	Vehicles     []DigestLine
	MoreVehicles int
	// From e Until: o primeiro e o último, já formatados no fuso da central.
	From        string
	Until       string
	MaxPerHour  int
	ActionURL   string
	SettingsURL string
	AppURL      string
}

// DigestLine é uma linha do resumo: um tipo de alerta ou um veículo.
type DigestLine struct {
	Label string
	Count int
}

type digestData struct {
	AlertDigest
	FirstName string
}

// Digest manda o resumo dos alertas que passaram do teto por hora.
func (m *AlertMailer) Digest(ctx context.Context, d AlertDigest) error {
	msg, err := renderDigest(d)
	if err != nil {
		return err
	}
	return m.sender.Send(ctx, msg)
}

func renderDigest(d AlertDigest) (Message, error) {
	data := digestData{AlertDigest: d, FirstName: firstName(d.Name)}
	var text, html bytes.Buffer
	if err := digestTemplates.text.Execute(&text, data); err != nil {
		return Message{}, fmt.Errorf("montando o resumo de alertas (texto): %w", err)
	}
	if err := digestTemplates.html.ExecuteTemplate(&html, "layout", data); err != nil {
		return Message{}, fmt.Errorf("montando o resumo de alertas (HTML): %w", err)
	}
	subject := fmt.Sprintf("Resumo: %d alertas da última hora", d.Count)
	if d.Count == 1 {
		subject = "Resumo: 1 alerta da última hora"
	}
	return Message{To: d.To, ToName: d.Name, Subject: subject, Text: text.String(), HTML: html.String()}, nil
}

const digestHTML = `{{define "content"}}
<p style="margin:0 0 8px;font-size:12px;font-weight:800;letter-spacing:3px;text-transform:uppercase;color:#b45309;">Resumo de alertas</p>
<h1 style="margin:0 0 16px;font-size:22px;line-height:1.3;color:#0d130e;">{{.Count}} {{if eq .Count 1}}alerta ficou{{else}}alertas ficaram{{end}} para o resumo</h1>
<p style="margin:0 0 12px;font-size:15px;line-height:1.6;color:#334155;">Olá{{with .FirstName}}, {{.}}{{end}}!</p>
<p style="margin:0 0 20px;font-size:15px;line-height:1.6;color:#334155;">Para não lotar sua caixa, mandamos um a um até {{.MaxPerHour}} alertas por hora. Os de segurança (botão de pânico, bateria do veículo desconectada e movimento com a ignição desligada) saem sempre, na hora. Estes passaram do limite entre {{.From}} e {{.Until}}:</p>
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="margin:0 0 16px;border:1px solid #e2e8f0;border-radius:12px;">
<tr><td style="padding:14px 16px;">
<p style="margin:0 0 6px;font-size:13px;font-weight:700;color:#0d130e;">Por tipo</p>
<table role="presentation" width="100%" cellpadding="0" cellspacing="0">
{{range .Kinds}}<tr><td style="padding:4px 0;font-size:14px;color:#475569;">{{.Label}}</td><td align="right" style="padding:4px 0;font-size:14px;font-weight:700;color:#0d130e;">{{.Count}}</td></tr>{{end}}
</table>
</td></tr></table>
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="margin:0 0 8px;border:1px solid #e2e8f0;border-radius:12px;">
<tr><td style="padding:14px 16px;">
<p style="margin:0 0 6px;font-size:13px;font-weight:700;color:#0d130e;">Veículos com mais alertas</p>
<table role="presentation" width="100%" cellpadding="0" cellspacing="0">
{{range .Vehicles}}<tr><td style="padding:4px 0;font-size:14px;color:#475569;">{{.Label}}</td><td align="right" style="padding:4px 0;font-size:14px;font-weight:700;color:#0d130e;">{{.Count}}</td></tr>{{end}}
{{if .MoreVehicles}}<tr><td colspan="2" style="padding:4px 0;font-size:13px;color:#64748b;">e mais {{.MoreVehicles}} {{if eq .MoreVehicles 1}}veículo{{else}}veículos{{end}}</td></tr>{{end}}
</table>
</td></tr></table>
{{template "button" (button .ActionURL "Abrir o painel")}}
{{if .SettingsURL}}<p style="margin:0;font-size:12px;line-height:1.6;color:#64748b;">Recebendo alertas demais? Escolha quais você quer em <a href="{{.SettingsURL}}" style="color:#15803d;">Alertas</a>, no painel.</p>{{end}}
{{end}}`

const digestText = `Olá{{with .FirstName}}, {{.}}{{end}}!

{{.Count}} {{if eq .Count 1}}alerta ficou{{else}}alertas ficaram{{end}} para o resumo.

Para não lotar sua caixa, mandamos um a um até {{.MaxPerHour}} alertas por hora. Os de segurança (botão de pânico, bateria do veículo desconectada e movimento com a ignição desligada) saem sempre, na hora. Estes passaram do limite entre {{.From}} e {{.Until}}:

Por tipo:
{{range .Kinds}}- {{.Label}}: {{.Count}}
{{end}}
Veículos com mais alertas:
{{range .Vehicles}}- {{.Label}}: {{.Count}}
{{end}}{{if .MoreVehicles}}- e mais {{.MoreVehicles}} {{if eq .MoreVehicles 1}}veículo{{else}}veículos{{end}}
{{end}}
Abrir o painel: {{.ActionURL}}
{{with .SettingsURL}}Escolha quais alertas você recebe: {{.}}
{{end}}
— Farbo Rastreadores
`

var digestTemplates = mustTemplates(digestText, digestHTML)
