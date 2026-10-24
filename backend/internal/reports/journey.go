package reports

import (
	"context"
	"time"
)

// A jornada do motorista profissional (Lei 13.103/2015 e o art. 67-C do
// CTB), pelo que o rastreador vê — as viagens. As jornadas são separadas
// por um descanso de ao menos JourneyBreak; dentro de cada uma, a direção
// contínua é a soma das viagens sem uma parada de RestStop (as paradas mais
// curtas não zeram a conta). O que passa vira alerta para a empresa
// conferir: o rastreador não sabe quem dirigia nem o tempo de espera e de
// carga e descarga.
const (
	// RestStop: a parada que conta como descanso na direção (30 min).
	RestStop = 30 * time.Minute
	// JourneyBreak: o descanso que separa uma jornada da outra.
	JourneyBreak = 8 * time.Hour
	// MinInterjourney: o descanso mínimo entre jornadas (11 h em 24 h).
	MinInterjourney = 11 * time.Hour
	// MaxDailyDriving: 8 h de jornada e até 2 h extras, ao volante.
	MaxDailyDriving = 10 * time.Hour
	// ContinuousCargo e ContinuousPassengers: a direção contínua máxima (5h30
	// no transporte de cargas; 4 h no de passageiros).
	ContinuousCargo      = 5*time.Hour + 30*time.Minute
	ContinuousPassengers = 4 * time.Hour
)

// Os alertas da jornada.
const (
	JourneyContinuous   = "continuous"
	JourneyDaily        = "daily"
	JourneyInterjourney = "interjourney"
)

// Journey é uma jornada de um veículo.
type Journey struct {
	Day               string    `json:"day"`
	Start             time.Time `json:"start"`
	End               time.Time `json:"end"`
	Trips             int       `json:"trips"`
	DrivingSeconds    int       `json:"drivingSeconds"`
	LongestSeconds    int       `json:"longestSeconds"`
	RestBeforeSeconds *int      `json:"restBeforeSeconds"`
	DistanceKm        float64   `json:"distanceKm"`
	Violations        []string  `json:"violations"`
}

// Journeys junta as viagens (em ordem) em jornadas e confere os limites.
func Journeys(trips []Trip, loc *time.Location, continuous time.Duration) []Journey {
	out := []Journey{}
	var cur *Journey
	var prevEnd time.Time
	var stretchDriving time.Duration
	var lastJourneyEnd *time.Time
	finish := func() {
		if cur == nil {
			return
		}
		if time.Duration(cur.DrivingSeconds)*time.Second > MaxDailyDriving {
			cur.Violations = append(cur.Violations, JourneyDaily)
		}
		if time.Duration(cur.LongestSeconds)*time.Second > continuous {
			cur.Violations = append(cur.Violations, JourneyContinuous)
		}
		end := cur.End
		lastJourneyEnd = &end
		out = append(out, *cur)
		cur = nil
	}
	for _, t := range trips {
		driving := t.End.Sub(t.Start)
		if cur != nil && t.Start.Sub(cur.End) >= JourneyBreak {
			finish()
		}
		if cur == nil {
			cur = &Journey{Day: t.Start.In(loc).Format(time.DateOnly), Start: t.Start, Violations: []string{}}
			if lastJourneyEnd != nil {
				rest := int(t.Start.Sub(*lastJourneyEnd).Seconds())
				cur.RestBeforeSeconds = &rest
				if t.Start.Sub(*lastJourneyEnd) < MinInterjourney {
					cur.Violations = append(cur.Violations, JourneyInterjourney)
				}
			}
			stretchDriving, prevEnd = 0, t.Start
		}
		// Uma parada de RestStop (ou mais) zera a direção contínua.
		if t.Start.Sub(prevEnd) >= RestStop {
			stretchDriving = 0
		}
		stretchDriving += driving
		if s := int(stretchDriving.Seconds()); s > cur.LongestSeconds {
			cur.LongestSeconds = s
		}
		cur.Trips++
		cur.DrivingSeconds += int(driving.Seconds())
		cur.DistanceKm += t.DistanceM / 1000
		if t.End.After(cur.End) {
			cur.End = t.End
		}
		prevEnd = t.End
	}
	finish()
	for i := range out {
		out[i].DistanceKm = round1(out[i].DistanceKm)
	}
	return out
}

// Journey é a jornada de um veículo no período [from, to) (instantes). A
// véspera entra só para achar o descanso antes da primeira jornada.
func (s *Service) Journey(ctx context.Context, v Vehicle, from, to time.Time, continuous time.Duration) ([]Journey, error) {
	trips, err := s.Trips(ctx, v, from.Add(-24*time.Hour), to)
	if err != nil {
		return nil, err
	}
	all := Journeys(trips, s.loc, continuous)
	out := []Journey{}
	for _, j := range all {
		if !j.Start.Before(from) {
			out = append(out, j)
		}
	}
	return out, nil
}
