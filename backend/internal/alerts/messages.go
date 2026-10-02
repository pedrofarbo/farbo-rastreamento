package alerts

import (
	"fmt"
	"strings"
	"time"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/mail"
)

// compose monta o e-mail de um alerta para um destinatário.
func (e *Engine) compose(t *Target, c candidate, r recipient, suppressed int) mail.Alert {
	vehicle := t.VehicleName
	if vehicle == "" {
		vehicle = "sem nome"
	}
	title, summary, severity := e.describe(r.kind, c, vehicle, r.settings)

	a := mail.Alert{
		To: r.email, Name: r.name, Severity: severity, Title: title, Summary: summary,
		Subject: "Alerta: " + title + " — " + vehicle,
		Vehicle: vehicle, Plate: t.Plate, When: e.formatWhen(c.at),
		Latitude: c.lat, Longitude: c.lon, Suppressed: suppressed, Cooldown: humanize(e.cfg.Cooldown),
		AppURL: e.appURL, ActionURL: e.appURL + "/dashboard",
	}
	if t.VehicleID != nil {
		a.ActionURL = e.appURL + "/veiculos/" + t.VehicleID.String()
	}
	// A central não tem tela de preferências: recebe pelo ALERTS_CENTRAL_EMAILS.
	if !r.central {
		a.SettingsURL = e.appURL + "/alertas"
	}
	switch r.kind {
	case KindOverspeed:
		a.Speed, a.Limit = c.speed, c.limit
	case KindTowing:
		a.Speed = c.speed
	}
	return a
}

// describe escreve a manchete, a explicação e a gravidade de cada alerta.
func (e *Engine) describe(kind string, c candidate, vehicle string, s Settings) (title, summary, severity string) {
	clock := c.at.In(e.loc).Format("15:04")
	switch kind {
	case KindSOS:
		return "Botão de pânico (SOS) acionado",
			fmt.Sprintf("O botão SOS do rastreador do veículo %s foi acionado às %s. Confira onde ele está e, se for uma emergência, ligue 190.", vehicle, clock),
			mail.SeverityCritical
	case KindPowerCut:
		return "Bateria do veículo desconectada",
			fmt.Sprintf("O rastreador do veículo %s deixou de receber energia da bateria às %s. Pode ser uma manutenção — ou alguém tentando desligar o rastreador.", vehicle, clock),
			mail.SeverityCritical
	case KindTowing:
		if c.variant == variantAlarm {
			return "Veículo em movimento com a ignição desligada",
				fmt.Sprintf("O rastreador do veículo %s detectou às %s que ele saiu do lugar com a ignição desligada. Pode ser um reboque ou furto.", vehicle, clock),
				mail.SeverityCritical
		}
		return "Veículo em movimento com a ignição desligada",
			fmt.Sprintf("O veículo %s se deslocou %s do lugar onde estava estacionado, com a ignição desligada (%s). Pode ser um reboque ou furto.", vehicle, formatDistance(c.distance), clock),
			mail.SeverityCritical
	case KindIgnitionGuard:
		return "Ignição ligada no horário de vigilância",
			fmt.Sprintf("A ignição do veículo %s foi ligada às %s, dentro do seu horário de vigilância (%s às %s).", vehicle, clock, FormatClock(s.GuardStart), FormatClock(s.GuardEnd)),
			mail.SeverityWarning
	case KindIgnition:
		return "Ignição ligada",
			fmt.Sprintf("A ignição do veículo %s foi ligada às %s.", vehicle, clock),
			mail.SeverityInfo
	case KindOverspeed:
		text := fmt.Sprintf("O veículo %s passou do limite de velocidade às %s.", vehicle, clock)
		if c.speed != nil && c.limit != nil {
			text = fmt.Sprintf("O veículo %s passou do limite de %.0f km/h às %s: registramos %.0f km/h.", vehicle, *c.limit, clock, *c.speed)
		}
		return "Excesso de velocidade", text, mail.SeverityWarning
	case KindSignalLost:
		if c.variant == variantParked {
			return "Rastreador sem comunicação há mais de " + humanize(c.offline),
				fmt.Sprintf("O rastreador do veículo %s está sem se comunicar há mais de %s, com o veículo parado. Pode ser uma garagem sem sinal — ou o rastreador desligado.", vehicle, humanize(c.offline)),
				mail.SeverityWarning
		}
		return "Rastreador sem sinal com o veículo em movimento",
			fmt.Sprintf("O rastreador do veículo %s parou de se comunicar às %s com a ignição ligada ou em movimento. Pode ser uma área sem cobertura — ou um bloqueador de sinal.", vehicle, clock),
			mail.SeverityWarning
	case KindLowBattery:
		return "Bateria do rastreador fraca",
			fmt.Sprintf("A bateria interna do rastreador do veículo %s está acabando (%s). Isso costuma acontecer quando ele fica sem a energia do veículo.", vehicle, clock),
			mail.SeverityWarning
	case KindGeofence:
		name := c.fenceName
		if name == "" {
			name = "sem nome"
		}
		if c.variant == variantExit {
			return "Saiu da cerca " + name,
				fmt.Sprintf("O veículo %s saiu da cerca %s às %s.", vehicle, name, clock),
				mail.SeverityInfo
		}
		return "Entrou na cerca " + name,
			fmt.Sprintf("O veículo %s entrou na cerca %s às %s.", vehicle, name, clock),
			mail.SeverityInfo
	case KindEngineBlock:
		if c.variant == variantResume {
			return "Motor liberado",
				fmt.Sprintf("O rastreador do veículo %s confirmou às %s que o motor foi liberado.", vehicle, clock),
				mail.SeverityInfo
		}
		return "Motor bloqueado",
			fmt.Sprintf("O rastreador do veículo %s confirmou às %s que o motor foi bloqueado.", vehicle, clock),
			mail.SeverityInfo
	}
	return "Alerta do veículo", fmt.Sprintf("Houve um alerta no veículo %s às %s.", vehicle, clock), mail.SeverityInfo
}

// formatWhen escreve data e hora no fuso da central.
func (e *Engine) formatWhen(t time.Time) string {
	local := t.In(e.loc)
	zone := local.Format("MST")
	if e.loc.String() == "America/Sao_Paulo" {
		zone = "horário de Brasília"
	}
	return local.Format("02/01/2006 às 15:04") + " (" + zone + ")"
}

func formatDistance(meters float64) string {
	if meters < 1000 {
		return fmt.Sprintf("cerca de %.0f m", meters)
	}
	return strings.Replace(fmt.Sprintf("cerca de %.1f km", meters/1000), ".", ",", 1)
}

// humanize escreve um intervalo por extenso: "30 minutos", "2 horas".
func humanize(d time.Duration) string {
	switch {
	case d >= time.Hour && d%time.Hour == 0:
		return plural(int(d/time.Hour), "hora", "horas")
	case d >= time.Minute:
		return plural(int(d/time.Minute), "minuto", "minutos")
	default:
		return plural(int(d/time.Second), "segundo", "segundos")
	}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
