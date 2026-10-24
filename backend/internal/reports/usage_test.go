package reports

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// A utilização: os dias contam do dia em que o veículo entrou no sistema e
// até hoje; a ociosidade é o parado ligado sobre o tempo ligado.
func TestUsage(t *testing.T) {
	svc := &Service{loc: time.UTC}
	old, fresh, idle := uuid.New(), uuid.New(), uuid.New()
	day := func(s string) time.Time { d, _ := time.Parse(time.DateOnly, s); return d }
	vehicles := []Vehicle{
		{ID: old, CreatedAt: day("2026-01-01")},
		{ID: fresh, CreatedAt: day("2026-10-20").Add(15 * time.Hour)},
		{ID: idle, CreatedAt: day("2026-01-01")},
	}
	rows := []SummaryRow{
		{VehicleID: old, DistanceKm: 300, ActiveDays: 5, MovingSeconds: 3 * 3600, IdleSeconds: 3600},
		{VehicleID: fresh, DistanceKm: 40, ActiveDays: 2, MovingSeconds: 3600},
		{VehicleID: idle},
	}
	// Período de 01 a 31/10, hoje 23/10: 23 dias.
	got := svc.Usage(rows, vehicles, day("2026-10-01"), day("2026-10-31"), day("2026-10-23"))
	if u := got[0]; u.PeriodDays != 23 || u.StoppedDays != 18 || u.UsePercent != 21.7 || u.IdlePercent != 25 || u.KmPerActiveDay != 60 {
		t.Errorf("antigo = %+v", u)
	}
	if u := got[1]; u.PeriodDays != 4 || u.StoppedDays != 2 || u.UsePercent != 50 || u.IdlePercent != 0 {
		t.Errorf("novo = %+v", u)
	}
	if u := got[2]; u.PeriodDays != 23 || u.StoppedDays != 23 || u.UsePercent != 0 || u.KmPerActiveDay != 0 {
		t.Errorf("parado = %+v", u)
	}
}
