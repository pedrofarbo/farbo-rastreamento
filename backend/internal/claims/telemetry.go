package claims

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// A telemetria da ocorrência: o que o rastreador do veículo registrou em
// volta do horário declarado — onde estava, a velocidade, a ignição, as
// freadas bruscas, o corte de energia, o silêncio do rastreador, quanto o
// veículo andou antes e depois e o modo roubo — e o que confere ou não com o
// que foi declarado. É a análise que a equipe da organização lê antes de
// aprovar; o relatório detalhado (o trajeto e o gráfico) continua na tela.

const (
	// telemetryWindow: o trajeto antes e depois do horário declarado.
	telemetryWindow = 2 * time.Hour
	// nearWindow: a posição que vale como "na hora".
	nearWindow = 15 * time.Minute
	// harshWindow: as freadas e arrancadas bruscas em volta do horário.
	harshWindow = 10 * time.Minute
	// theftLookback e theftLookahead: o modo roubo ligado em volta do horário.
	theftLookback  = 24 * time.Hour
	theftLookahead = 48 * time.Hour
	// harshBrakeKmhPerS e harshMaxGap: a freada brusca (a mesma conta dos relatórios).
	harshBrakeKmhPerS = 12.0
	harshMaxGap       = 15 * time.Second
	// movingKmh: acima disso, em movimento.
	movingKmh = 5.0
	// silenceGap: sem posição por mais que isso, o rastreador ficou em silêncio.
	silenceGap = 20 * time.Minute
)

// Os níveis do que a análise achou.
const (
	FindingOK   = "ok"
	FindingWarn = "warn"
	FindingInfo = "info"
)

// Finding é uma conclusão da análise.
type Finding struct {
	Level string `json:"level"`
	Text  string `json:"text"`
}

// TelemetryPoint é uma posição do rastreador.
type TelemetryPoint struct {
	At        time.Time `json:"at"`
	Latitude  float64   `json:"latitude"`
	Longitude float64   `json:"longitude"`
	SpeedKmh  float64   `json:"speedKmh"`
	Ignition  *bool     `json:"ignition"`
}

// Telemetry é a análise da ocorrência.
type Telemetry struct {
	// Available: o veículo tem rastreador.
	Available  bool      `json:"available"`
	OccurredAt time.Time `json:"occurredAt"`
	// At: a posição mais perto do horário (até 15 min); LastBefore e
	// FirstAfter, as de antes e de depois, quando não houve.
	At         *TelemetryPoint `json:"at"`
	LastBefore *TelemetryPoint `json:"lastBefore"`
	FirstAfter *TelemetryPoint `json:"firstAfter"`
	// Now: a última posição do veículo.
	Now *TelemetryPoint `json:"now"`
	// KmBefore e KmAfter: o que o veículo andou nas 2 horas antes e depois.
	KmBefore float64 `json:"kmBefore"`
	KmAfter  float64 `json:"kmAfter"`
	// MaxSpeedNear: a maior velocidade a até 10 minutos do horário.
	MaxSpeedNear float64     `json:"maxSpeedNear"`
	HarshBrakes  []time.Time `json:"harshBrakes"`
	PowerCuts    []time.Time `json:"powerCuts"`
	// TheftActivatedAt: o modo roubo ligado em volta do horário.
	TheftActivatedAt *time.Time `json:"theftActivatedAt"`
	TheftOutcome     string     `json:"theftOutcome"`
	Findings         []Finding  `json:"findings"`
}

// SetLocation: o fuso em que a análise escreve as horas.
func (s *Service) SetLocation(loc *time.Location) {
	if loc != nil {
		s.loc = loc
	}
}

func (s *Service) clock(t time.Time) string { return t.In(s.loc).Format("15h04") }

func (s *Service) clockDay(t time.Time, ref time.Time) string {
	if t.In(s.loc).Format("2006-01-02") == ref.In(s.loc).Format("2006-01-02") {
		return s.clock(t)
	}
	return t.In(s.loc).Format("02/01 às 15h04")
}

func minutesText(d time.Duration) string {
	m := int(math.Round(math.Abs(d.Minutes())))
	switch {
	case m == 0:
		return "no mesmo minuto"
	case m == 1:
		return "1 min"
	case m < 120:
		return fmt.Sprintf("%d min", m)
	}
	return fmt.Sprintf("%d h", m/60)
}

func kmText(km float64) string {
	if km < 10 {
		return strings.Replace(fmt.Sprintf("%.1f km", km), ".", ",", 1)
	}
	return fmt.Sprintf("%.0f km", km)
}

// haversineM é a distância em metros entre duas posições.
func haversineM(a, b TelemetryPoint) float64 {
	const earth = 6_371_000.0
	rad := math.Pi / 180
	dLat, dLon := (b.Latitude-a.Latitude)*rad, (b.Longitude-a.Longitude)*rad
	h := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(a.Latitude*rad)*math.Cos(b.Latitude*rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * earth * math.Asin(math.Min(1, math.Sqrt(h)))
}

// distanceKm é o que o veículo andou entre as posições: os trechos com
// movimento (o GPS parado tremendo não conta).
func distanceKm(points []TelemetryPoint) float64 {
	total := 0.0
	for i := 1; i < len(points); i++ {
		d := haversineM(points[i-1], points[i])
		if points[i].SpeedKmh >= movingKmh || points[i-1].SpeedKmh >= movingKmh || d > 200 {
			total += d
		}
	}
	return total / 1000
}

// Telemetry é a análise da ocorrência pelo rastreador do veículo.
func (s *Service) Telemetry(ctx context.Context, c *Claim) (*Telemetry, error) {
	out := &Telemetry{OccurredAt: c.OccurredAt, HarshBrakes: []time.Time{}, PowerCuts: []time.Time{}, Findings: []Finding{}}
	if c.VehicleID == nil {
		out.Findings = append(out.Findings, Finding{FindingInfo, "A ocorrência não é de um veículo com rastreador."})
		return out, nil
	}
	var device *uuid.UUID
	if err := s.db.QueryRow(ctx, `SELECT device_id FROM vehicles WHERE id = $1`, *c.VehicleID).Scan(&device); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return out, nil
		}
		return nil, database.MapError(err)
	}
	if device == nil {
		out.Findings = append(out.Findings, Finding{FindingInfo, "O veículo não tem rastreador instalado: não há dados para conferir."})
		return out, nil
	}
	out.Available = true
	t := c.OccurredAt

	rows, err := s.db.Query(ctx, `
		SELECT gps_timestamp, latitude, longitude, speed_kmh, acc FROM positions
		WHERE device_id = $1 AND gps_timestamp BETWEEN $2 AND $3 AND COALESCE(gps_valid, TRUE)
		ORDER BY gps_timestamp LIMIT 20000`, *device, t.Add(-telemetryWindow), t.Add(telemetryWindow))
	if err != nil {
		return nil, database.MapError(err)
	}
	var points []TelemetryPoint
	for rows.Next() {
		var p TelemetryPoint
		if err := rows.Scan(&p.At, &p.Latitude, &p.Longitude, &p.SpeedKmh, &p.Ignition); err != nil {
			rows.Close()
			return nil, database.MapError(err)
		}
		points = append(points, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, database.MapError(err)
	}

	var now TelemetryPoint
	if err := s.db.QueryRow(ctx, `
		SELECT gps_timestamp, latitude, longitude, speed_kmh, acc FROM positions WHERE device_id = $1
		ORDER BY gps_timestamp DESC LIMIT 1`, *device).Scan(&now.At, &now.Latitude, &now.Longitude, &now.SpeedKmh, &now.Ignition); err == nil {
		out.Now = &now
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, database.MapError(err)
	}

	erows, err := s.db.Query(ctx, `
		SELECT timestamp FROM vehicle_events WHERE device_id = $1 AND type = 'POWER_LOSS' AND timestamp BETWEEN $2 AND $3
		ORDER BY timestamp`, *device, t.Add(-telemetryWindow), t.Add(telemetryWindow))
	if err != nil {
		return nil, database.MapError(err)
	}
	for erows.Next() {
		var at time.Time
		if err := erows.Scan(&at); err != nil {
			erows.Close()
			return nil, database.MapError(err)
		}
		out.PowerCuts = append(out.PowerCuts, at)
	}
	erows.Close()

	var theftAt time.Time
	var outcome *string
	switch err := s.db.QueryRow(ctx, `
		SELECT activated_at, outcome FROM theft_modes WHERE vehicle_id = $1 AND activated_at BETWEEN $2 AND $3
		ORDER BY abs(extract(epoch FROM activated_at - $4::timestamptz)) LIMIT 1`,
		*c.VehicleID, t.Add(-theftLookback), t.Add(theftLookahead), t).Scan(&theftAt, &outcome); {
	case err == nil:
		out.TheftActivatedAt = &theftAt
		if outcome != nil {
			out.TheftOutcome = *outcome
		}
	case !errors.Is(err, pgx.ErrNoRows):
		return nil, database.MapError(err)
	}

	s.analyze(out, c, points)
	return out, nil
}

// analyze tira as conclusões das posições em volta do horário.
func (s *Service) analyze(out *Telemetry, c *Claim, points []TelemetryPoint) {
	t := c.OccurredAt
	add := func(level, format string, args ...any) {
		out.Findings = append(out.Findings, Finding{level, fmt.Sprintf(format, args...)})
	}
	var before, after []TelemetryPoint
	for i, p := range points {
		if !p.At.After(t) {
			before = append(before, p)
		} else {
			after = append(after, p)
		}
		if gap := p.At.Sub(t); math.Abs(gap.Minutes()) <= nearWindow.Minutes() {
			if out.At == nil || math.Abs(gap.Seconds()) < math.Abs(out.At.At.Sub(t).Seconds()) {
				q := points[i]
				out.At = &q
			}
		}
		if math.Abs(p.At.Sub(t).Minutes()) <= harshWindow.Minutes() {
			out.MaxSpeedNear = math.Max(out.MaxSpeedNear, p.SpeedKmh)
			if i > 0 {
				prev := points[i-1]
				dt := p.At.Sub(prev.At)
				if dt > 0 && dt <= harshMaxGap && (p.SpeedKmh-prev.SpeedKmh)/dt.Seconds() <= -harshBrakeKmhPerS {
					out.HarshBrakes = append(out.HarshBrakes, p.At)
				}
			}
		}
	}
	if len(before) > 0 {
		out.KmBefore = math.Round(distanceKm(append(before, firstOrNone(after)...))*10) / 10
	}
	if len(after) > 0 {
		out.KmAfter = math.Round(distanceKm(append(lastOrNone(before), after...))*10) / 10
	}

	// Os dados na hora.
	if out.At != nil {
		add(FindingOK, "O rastreador mandou posição às %s (%s do horário declarado).", s.clock(out.At.At), minutesText(out.At.At.Sub(t)))
	} else {
		if len(before) > 0 {
			p := before[len(before)-1]
			out.LastBefore = &p
		}
		if len(after) > 0 {
			p := after[0]
			out.FirstAfter = &p
		}
		switch {
		case out.LastBefore != nil && out.FirstAfter != nil:
			add(FindingWarn, "O rastreador ficou sem mandar posição perto do horário declarado: a última antes foi às %s e a primeira depois, às %s.",
				s.clock(out.LastBefore.At), s.clockDay(out.FirstAfter.At, t))
		case out.LastBefore != nil:
			add(FindingWarn, "O rastreador parou de mandar posição às %s, antes do horário declarado, e não mandou mais nas 2 horas seguintes.",
				s.clock(out.LastBefore.At))
		case out.FirstAfter != nil:
			add(FindingWarn, "O rastreador não mandou posição nas 2 horas antes do horário declarado; a primeira depois foi às %s.",
				s.clockDay(out.FirstAfter.At, t))
		default:
			add(FindingWarn, "O rastreador não mandou nenhuma posição nas 2 horas antes e depois do horário declarado.")
		}
	}

	// O silêncio e o corte de energia em volta.
	for i := 1; i < len(points); i++ {
		if gap := points[i].At.Sub(points[i-1].At); gap > silenceGap && points[i-1].At.After(t.Add(-telemetryWindow)) {
			if out.At != nil && points[i-1].At.Before(t) && points[i].At.After(t) {
				continue
			}
			add(FindingInfo, "O rastreador ficou %s sem mandar posição, das %s às %s.", minutesText(gap), s.clock(points[i-1].At), s.clockDay(points[i].At, t))
			break
		}
	}
	for _, cut := range out.PowerCuts {
		when := "depois"
		if cut.Before(t) {
			when = "antes"
		}
		add(FindingInfo, "A energia do veículo foi cortada às %s (%s %s do horário declarado).", s.clock(cut), minutesText(cut.Sub(t)), when)
	}

	moving := out.At != nil && out.At.SpeedKmh >= movingKmh
	ignitionOff := out.At != nil && out.At.Ignition != nil && !*out.At.Ignition
	switch c.Kind {
	case "collision":
		switch {
		case moving:
			add(FindingOK, "Na hora declarada, o veículo estava em movimento, a %.0f km/h.", out.At.SpeedKmh)
		case out.At != nil && ignitionOff && out.MaxSpeedNear < movingKmh:
			add(FindingInfo, "Na hora declarada, o veículo estava parado, com a ignição desligada: a batida teria sido com ele estacionado.")
		case out.At != nil && out.MaxSpeedNear < movingKmh:
			add(FindingInfo, "O veículo ficou parado nos 10 minutos em volta do horário declarado.")
		case out.At != nil:
			add(FindingInfo, "Perto do horário declarado, o veículo chegou a %.0f km/h.", out.MaxSpeedNear)
		}
		if len(out.HarshBrakes) > 0 {
			add(FindingOK, "Freada brusca às %s, perto do horário declarado.", s.clock(out.HarshBrakes[0]))
		} else if out.MaxSpeedNear >= movingKmh {
			add(FindingInfo, "Nenhuma freada brusca registrada a até 10 minutos do horário declarado.")
		}
	case "theft", "robbery":
		switch {
		case len(after) == 0:
		case out.KmAfter >= 1:
			add(FindingOK, "Depois do horário declarado, o veículo andou %s nas 2 horas seguintes.", kmText(out.KmAfter))
		default:
			add(FindingWarn, "Depois do horário declarado, o veículo ficou parado no mesmo lugar nas 2 horas seguintes.")
		}
		if out.TheftActivatedAt == nil {
			add(FindingInfo, "O modo roubo não foi ligado em volta do horário declarado.")
		} else if out.TheftActivatedAt.Before(t) {
			add(FindingWarn, "O modo roubo foi ligado às %s, antes do horário declarado (%s antes).",
				s.clockDay(*out.TheftActivatedAt, t), minutesText(out.TheftActivatedAt.Sub(t)))
		} else {
			add(FindingOK, "O modo roubo foi ligado às %s, %s depois do horário declarado.",
				s.clockDay(*out.TheftActivatedAt, t), minutesText(out.TheftActivatedAt.Sub(t)))
		}
		if out.TheftOutcome == "RECOVERED" {
			add(FindingInfo, "O veículo foi marcado como recuperado.")
		}
	}
	if len(before) > 0 && out.KmBefore > 0 {
		add(FindingInfo, "Nas 2 horas antes, o veículo andou %s.", kmText(out.KmBefore))
	}
}

func firstOrNone(points []TelemetryPoint) []TelemetryPoint {
	if len(points) == 0 {
		return nil
	}
	return points[:1]
}

func lastOrNone(points []TelemetryPoint) []TelemetryPoint {
	if len(points) == 0 {
		return nil
	}
	return points[len(points)-1:]
}
