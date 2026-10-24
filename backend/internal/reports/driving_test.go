package reports

import (
	"testing"
	"time"
)

// A nota: perfeita sem nada; cada coisa tira pontos por 100 km, com teto;
// com poucos km, sem nota.
func TestScore(t *testing.T) {
	if s, _ := Score(DrivingRow{DistanceKm: 10, Overspeeds: 3}); s != nil {
		t.Errorf("com 10 km não há nota: %d", *s)
	}
	if s, p := Score(DrivingRow{DistanceKm: 500, MovingSeconds: 36000}); *s != 100 || p != (Penalties{}) {
		t.Errorf("sem nada = %d %+v", *s, p)
	}
	// 200 km: 2 excessos (4 pts), 3 bruscas (4,5), 5% acima de 120 (5), 30% à noite (3).
	s, p := Score(DrivingRow{DistanceKm: 200, MovingSeconds: 20000, Overspeeds: 2, HarshBrakes: 2, HarshAccels: 1,
		HighSpeedSeconds: 1000, NightPercent: 30})
	if *s != 84 || p != (Penalties{Overspeed: 4, Harsh: 4.5, HighSpeed: 5, Night: 3}) {
		t.Errorf("nota = %d %+v", *s, p)
	}
	// Os tetos: muita coisa em poucos km não passa de 90 pontos tirados.
	if s, p := Score(DrivingRow{DistanceKm: 25, MovingSeconds: 3600, Overspeeds: 40, HarshBrakes: 40,
		HighSpeedSeconds: 3600, NightPercent: 100}); *s != 10 || p.Overspeed != 35 || p.Harsh != 25 || p.HighSpeed != 20 {
		t.Errorf("tetos = %d %+v", *s, p)
	}
}

// O dia ganha os km à noite, as bruscas e o tempo acima de 120 km/h.
func TestDayStatsDriving(t *testing.T) {
	night := time.Date(2026, 10, 12, 23, 0, 0, 0, time.UTC)
	pts := []Point{}
	add := func(at time.Time, lat, speed float64) {
		on := true
		pts = append(pts, Point{At: at, Lat: lat, Lon: -46.63, SpeedKmh: speed, Acc: &on})
	}
	// De dia: 0 → 60 em 5 s (arrancada), segue a 130 por 1 min, 130 → 40 em 5 s (freada).
	at := t0
	add(at, -23.55, 0)
	at = at.Add(5 * time.Second)
	add(at, -23.5499, 60)
	for i := 1; i <= 6; i++ {
		at = at.Add(10 * time.Second)
		add(at, -23.5499+float64(i)*0.0036, 130)
	}
	at = at.Add(5 * time.Second)
	add(at, -23.5499+6*0.0036+0.0001, 40)
	// De noite: 2 km devagar.
	add(night, -23.40, 40)
	add(night.Add(time.Minute), -23.40+2000/111_320.0, 40)

	day := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	s := DayStats(pts, day, day.AddDate(0, 0, 1))
	if s.HarshAccels != 1 || s.HarshBrakes != 1 || s.HighSpeedS != 50 || !near(s.NightM, 2000, 5) {
		t.Errorf("direção = arrancadas %d, freadas %d, acima de 120 %d s, noite %.0f m", s.HarshAccels, s.HarshBrakes, s.HighSpeedS, s.NightM)
	}
}
