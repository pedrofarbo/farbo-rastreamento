package mail

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// FleetMailer escreve os avisos da gestão da frota: a locação que passou da
// hora sem devolver.
type FleetMailer struct {
	sender Sender
	appURL string
}

func NewFleetMailer(sender Sender, appURL string) *FleetMailer {
	return &FleetMailer{sender: sender, appURL: strings.TrimRight(appURL, "/")}
}

// RentalNotice é a locação atrasada.
type RentalNotice struct {
	VehicleID   string
	VehicleName string
	Plate       string
	RenterName  string
	RenterPhone string
	EndsAt      time.Time
}

type rentalData struct {
	Name, Vehicle, Renter, Phone, EndsAt, ActionURL string
}

// RentalOverdue avisa que a devolução passou da hora.
func (m *FleetMailer) RentalOverdue(ctx context.Context, to, name string, n RentalNotice) error {
	vehicle := strings.TrimSpace(n.VehicleName)
	if n.Plate != "" {
		vehicle += " (" + n.Plate + ")"
	}
	data := rentalData{
		Name: firstName(name), Vehicle: vehicle, Renter: n.RenterName, Phone: n.RenterPhone,
		EndsAt: formatBrazilTime(n.EndsAt), ActionURL: m.appURL + "/veiculos/" + url.PathEscape(n.VehicleID),
	}
	var text, html bytes.Buffer
	if err := rentalTemplates.text.Execute(&text, data); err != nil {
		return fmt.Errorf("montando e-mail (texto): %w", err)
	}
	if err := rentalTemplates.html.ExecuteTemplate(&html, "layout", data); err != nil {
		return fmt.Errorf("montando e-mail (HTML): %w", err)
	}
	return m.sender.Send(ctx, Message{
		To: to, ToName: name, Text: text.String(), HTML: html.String(),
		Subject: "Locação atrasada: " + vehicle + " não foi devolvido",
	})
}

const rentalHTML = `{{define "content"}}
<p style="margin:0 0 8px;font-size:12px;font-weight:800;letter-spacing:3px;text-transform:uppercase;color:#b45309;">Locação atrasada</p>
<h1 style="margin:0 0 16px;font-size:22px;line-height:1.3;color:#0d130e;">{{.Vehicle}} não foi devolvido</h1>
<p style="margin:0 0 12px;font-size:15px;line-height:1.6;color:#334155;">Olá{{with .Name}}, {{.}}{{end}}! A devolução era até <strong>{{.EndsAt}}</strong> e ainda não foi registrada.</p>
<p style="margin:0 0 4px;font-size:15px;line-height:1.6;color:#334155;"><strong>Locatário:</strong> {{.Renter}}{{with .Phone}} · {{.}}{{end}}</p>
{{template "button" (button .ActionURL "Ver onde o veículo está")}}
<p style="margin:0;font-size:12px;line-height:1.6;color:#64748b;">Se o veículo já voltou, registre a devolução na tela de locações. Se o prazo foi estendido, mude a data de devolução lá.</p>
{{end}}`

const rentalText = `Olá{{with .Name}}, {{.}}{{end}}!

{{.Vehicle}} não foi devolvido: a devolução era até {{.EndsAt}}.

Locatário: {{.Renter}}{{with .Phone}} · {{.}}{{end}}

Ver onde o veículo está:
{{.ActionURL}}

Se o veículo já voltou, registre a devolução na tela de locações. Se o prazo foi estendido, mude a data de devolução lá.

— Farbo Rastreadores
`

var rentalTemplates = mustTemplates(rentalText, rentalHTML)
