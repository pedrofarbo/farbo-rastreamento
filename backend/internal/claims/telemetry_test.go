package claims

import (
	"strings"
	"testing"
	"time"
)

// trip são as posições a cada 10 s entre from e to (relativos a t), andando
// a speed km/h para o norte.
func trip(t time.Time, from, to time.Duration, speed float64, lat float64) []TelemetryPoint {
	var out []TelemetryPoint
	on := true
	for d := from; d <= to; d += 10 * time.Second {
		out = append(out, TelemetryPoint{At: t.Add(d), Latitude: lat, Longitude: -48.27, SpeedKmh: speed, Ignition: &on})
		lat += speed / 3600 * 10 / 111
	}
	return out
}

func findings(out *Telemetry) string {
	var b strings.Builder
	for _, f := range out.Findings {
		b.WriteString(f.Level + ": " + f.Text + "\n")
	}
	return b.String()
}

func TestAnalyze(t *testing.T) {
	s := &Service{loc: time.UTC}
	at := time.Date(2026, 11, 15, 14, 0, 0, 0, time.UTC)

	// A batida: em movimento, com a freada brusca de 60 para 20 km/h.
	points := trip(at, -30*time.Minute, -10*time.Second, 60, -18.9)
	points = append(points, TelemetryPoint{At: at, Latitude: -18.89, Longitude: -48.27, SpeedKmh: 60},
		TelemetryPoint{At: at.Add(3 * time.Second), Latitude: -18.89, Longitude: -48.27, SpeedKmh: 20})
	out := &Telemetry{HarshBrakes: nil}
	s.analyze(out, &Claim{Kind: "collision", OccurredAt: at}, points)
	got := findings(out)
	for _, want := range []string{"ok: O rastreador mandou posição às 14h00 (no mesmo minuto", "ok: Na hora declarada, o veículo estava em movimento, a 60 km/h.",
		"ok: Freada brusca às 14h00", "info: Nas 2 horas antes, o veículo andou"} {
		if !strings.Contains(got, want) {
			t.Errorf("batida: falta %q em\n%s", want, got)
		}
	}

	// O roubo: andou depois; o modo roubo ligado 20 min depois.
	theft := at.Add(20 * time.Minute)
	out = &Telemetry{TheftActivatedAt: &theft}
	s.analyze(out, &Claim{Kind: "robbery", OccurredAt: at}, trip(at, -5*time.Minute, 40*time.Minute, 50, -18.9))
	got = findings(out)
	if !strings.Contains(got, "ok: Depois do horário declarado, o veículo andou") || !strings.Contains(got, "ok: O modo roubo foi ligado às 14h20, 20 min depois") {
		t.Errorf("roubo:\n%s", got)
	}

	// O roubo declarado com o veículo parado no mesmo lugar e o modo roubo ligado antes.
	early := at.Add(-time.Hour)
	out = &Telemetry{TheftActivatedAt: &early}
	s.analyze(out, &Claim{Kind: "theft", OccurredAt: at}, trip(at, -10*time.Minute, 2*time.Hour, 0, -18.9))
	got = findings(out)
	if !strings.Contains(got, "warn: Depois do horário declarado, o veículo ficou parado no mesmo lugar") ||
		!strings.Contains(got, "warn: O modo roubo foi ligado às 13h00, antes do horário declarado") {
		t.Errorf("roubo suspeito:\n%s", got)
	}

	// O silêncio: nada perto do horário; a última antes e a primeira depois.
	points = append(trip(at, -time.Hour, -40*time.Minute, 0, -18.9), trip(at, 50*time.Minute, time.Hour, 0, -18.9)...)
	out = &Telemetry{}
	s.analyze(out, &Claim{Kind: "glass", OccurredAt: at}, points)
	got = findings(out)
	if out.At != nil || !strings.Contains(got, "warn: O rastreador ficou sem mandar posição perto do horário declarado: a última antes foi às 13h20 e a primeira depois, às 14h50.") {
		t.Errorf("silêncio:\n%s", got)
	}

	// A batida com o veículo estacionado, a ignição desligada.
	off := false
	out = &Telemetry{}
	s.analyze(out, &Claim{Kind: "collision", OccurredAt: at}, []TelemetryPoint{{At: at.Add(-time.Minute), Latitude: -18.9, Longitude: -48.27, Ignition: &off}})
	if got := findings(out); !strings.Contains(got, "info: Na hora declarada, o veículo estava parado, com a ignição desligada") {
		t.Errorf("estacionado:\n%s", got)
	}
}

func TestDistanceKm(t *testing.T) {
	at := time.Date(2026, 11, 15, 14, 0, 0, 0, time.UTC)
	if km := distanceKm(trip(at, 0, time.Hour, 60, -18.9)); km < 59 || km > 61 {
		t.Errorf("uma hora a 60 km/h = %.1f km", km)
	}
	// Parado, o GPS tremendo poucos metros não conta.
	still := []TelemetryPoint{{At: at, Latitude: -18.9, Longitude: -48.27}, {At: at.Add(time.Minute), Latitude: -18.90003, Longitude: -48.27002}}
	if km := distanceKm(still); km != 0 {
		t.Errorf("parado = %.3f km", km)
	}
}
