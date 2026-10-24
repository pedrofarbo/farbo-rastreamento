package reports

import (
	"testing"
	"time"
)

// A jornada: separada por 8 h de descanso; direção contínua zera com 30 min
// parado; passa do limite contínuo, do diário e do descanso entre jornadas.
func TestJourneys(t *testing.T) {
	at := func(day, hour, min int) time.Time { return time.Date(2026, 10, day, hour, min, 0, 0, time.UTC) }
	trip := func(start, end time.Time) Trip {
		return Trip{Start: start, End: end, DistanceM: end.Sub(start).Hours() * 60_000}
	}
	trips := []Trip{
		// Dia 12: 3 h, parada de 20 min (não descansa), mais 3 h → 6 h contínuas;
		// depois 40 min parado e mais 5 h: 11 h ao volante.
		trip(at(12, 6, 0), at(12, 9, 0)),
		trip(at(12, 9, 20), at(12, 12, 20)),
		trip(at(12, 13, 0), at(12, 18, 0)),
		// Dia 13: começa 9 h depois (descanso curto), 4 h dirigindo.
		trip(at(13, 3, 0), at(13, 7, 0)),
		// Dia 14: 20 h depois, 2 h.
		trip(at(14, 3, 0), at(14, 5, 0)),
	}
	got := Journeys(trips, time.UTC, ContinuousCargo)
	if len(got) != 3 {
		t.Fatalf("jornadas = %+v", got)
	}
	first := got[0]
	if first.Trips != 3 || first.DrivingSeconds != 11*3600 || first.LongestSeconds != 6*3600 || first.RestBeforeSeconds != nil ||
		!sameViolations(first.Violations, JourneyDaily, JourneyContinuous) || first.DistanceKm != 660 {
		t.Errorf("1ª = %+v", first)
	}
	if second := got[1]; *second.RestBeforeSeconds != 9*3600 || !sameViolations(second.Violations, JourneyInterjourney) || second.Day != "2026-10-13" {
		t.Errorf("2ª = %+v", second)
	}
	if third := got[2]; len(third.Violations) != 0 {
		t.Errorf("3ª = %+v", third)
	}
	// Passageiros: 4 h contínuas já passam.
	if got := Journeys(trips[3:4], time.UTC, ContinuousPassengers); len(got[0].Violations) != 0 {
		t.Errorf("4 h certas não passam do limite: %+v", got[0])
	}
	if got := Journeys([]Trip{trip(at(13, 3, 0), at(13, 7, 1))}, time.UTC, ContinuousPassengers); !sameViolations(got[0].Violations, JourneyContinuous) {
		t.Errorf("passageiros = %+v", got[0])
	}
}

func sameViolations(got []string, want ...string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
