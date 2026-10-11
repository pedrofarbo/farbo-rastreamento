package reports

import (
	"math"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 12, 8, 0, 0, 0, time.UTC)

// track monta posições a partir de t0: cada passo anda `meters` para o norte
// em `every`, com a velocidade e a ignição dadas.
type track struct {
	points []Point
	at     time.Time
	lat    float64
}

func newTrack() *track { return &track{at: t0, lat: -23.55} }

func (tr *track) add(n int, every time.Duration, meters, speed float64, acc bool) *track {
	for range n {
		tr.at = tr.at.Add(every)
		tr.lat += meters / 111_320
		a := acc
		tr.points = append(tr.points, Point{At: tr.at, Lat: tr.lat, Lon: -46.63, SpeedKmh: speed, Acc: &a})
	}
	return tr
}

func near(got, want, tolerance float64) bool { return math.Abs(got-want) <= tolerance }

func TestDayStatsAndTrips(t *testing.T) {
	// Parado desligado 10 min, anda 10 min a 36 km/h (300 m a cada 30 s),
	// para ligado 6 min (ocioso), desliga e fica 20 min.
	tr := newTrack().
		add(10, time.Minute, 0, 0, false).
		add(20, 30*time.Second, 300, 36, true).
		add(6, time.Minute, 0, 0, true).
		add(4, 5*time.Minute, 0, 0, false)
	s := DayStats(tr.points, t0, t0.Add(24*time.Hour))
	// Andando: os 20 trechos de 30 s, mais o minuto até o primeiro ponto
	// parado. Ocioso: os 5 min ligado parado, mais os 5 até o ponto que já
	// veio desligado (a ignição vale até a posição seguinte).
	if !near(s.DistanceM, 6000, 20) || s.MovingS != 11*60 || s.IdleS != 10*60 || s.Trips != 1 || s.MaxSpeed != 36 {
		t.Errorf("resumo = %+v", s)
	}
	if s.FirstMove == nil || !s.FirstMove.Equal(t0.Add(10*time.Minute+30*time.Second)) || s.LastMove == nil {
		t.Errorf("primeira saída = %v", s.FirstMove)
	}
	trips := Trips(tr.points)
	if len(trips) != 1 {
		t.Fatalf("viagens = %+v", trips)
	}
	trip := trips[0]
	// Sai do último ponto parado e chega no primeiro parado da parada.
	if !trip.Start.Equal(t0.Add(10*time.Minute)) || !trip.End.Equal(t0.Add(21*time.Minute)) || !near(trip.DistanceM, 6000, 20) || trip.Ongoing {
		t.Errorf("viagem = %+v (%d s)", trip, trip.Seconds())
	}
}

func TestNoiseJumpsAndShortTrips(t *testing.T) {
	tr := newTrack().add(5, time.Minute, 0, 0, false)
	// Ruído parado: 30 m para lá e para cá.
	tr.add(1, time.Minute, 30, 0, false).add(1, time.Minute, -30, 0, false)
	// Um salto impossível: 50 km em 1 minuto.
	tr.add(1, time.Minute, 50_000, 0, false).add(1, time.Minute, -50_000, 0, false)
	// Uma manobra de 100 m: não é viagem.
	tr.add(2, 30*time.Second, 50, 10, true).add(12, time.Minute, 0, 0, false)
	// GPS sem sinal: ignorado.
	invalid := false
	tr.points = append(tr.points, Point{At: tr.at.Add(time.Minute), Lat: 0, Lon: 0, SpeedKmh: 90, Valid: &invalid})
	s := DayStats(tr.points, t0, t0.Add(24*time.Hour))
	if !near(s.DistanceM, 100, 1) || s.Trips != 0 || s.MaxSpeed != 10 {
		t.Errorf("resumo com ruído = %+v", s)
	}
	if trips := Trips(tr.points); len(trips) != 0 {
		t.Errorf("manobra virou viagem: %+v", trips)
	}
}

func TestGapsSplitTripsAndDoNotCountTime(t *testing.T) {
	// Anda 5 min, o rastreador some 30 min, volta andando mais 5 min.
	tr := newTrack().add(10, 30*time.Second, 300, 36, true)
	tr.at = tr.at.Add(30 * time.Minute)
	tr.add(10, 30*time.Second, 300, 36, true)
	s := DayStats(tr.points, t0, t0.Add(24*time.Hour))
	// O buraco de 30 min não conta: 9 trechos de 30 s de cada lado.
	if s.MovingS != 2*9*30 || s.Trips != 2 {
		t.Errorf("resumo com buraco = %+v", s)
	}
	trips := Trips(tr.points)
	if len(trips) != 2 || !trips[1].Ongoing || trips[0].Ongoing {
		t.Errorf("viagens com buraco = %+v", trips)
	}
}

func TestTripAcrossMidnightCountsOnTheDayItStarts(t *testing.T) {
	midnight := t0.Add(16 * time.Hour) // 00:00 do dia seguinte
	tr := &track{at: midnight.Add(-5 * time.Minute), lat: -23.55}
	tr.add(20, 30*time.Second, 300, 36, true).add(10, time.Minute, 0, 0, false)
	before := DayStats(tr.points, midnight.Add(-24*time.Hour), midnight)
	after := DayStats(tr.points, midnight, midnight.Add(24*time.Hour))
	if before.Trips != 1 || after.Trips != 0 {
		t.Errorf("viagens: véspera %d, dia %d", before.Trips, after.Trips)
	}
	// A distância (19 trechos de 300 m) se divide entre os dois dias, sem
	// sobra nem falta.
	if !near(before.DistanceM+after.DistanceM, 5700, 20) || before.DistanceM == 0 || after.DistanceM == 0 {
		t.Errorf("distância: véspera %.0f, dia %.0f", before.DistanceM, after.DistanceM)
	}
}
