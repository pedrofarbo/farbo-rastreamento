package reports

import (
	"context"
	"math"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// A nota de direção e os dados de uso (o que a seguradora olha): de 0 a 100,
// tirando pontos pelos excessos de velocidade (o limite do veículo), pelo
// tempo acima de 120 km/h, pelas freadas e arrancadas bruscas e pela parte
// rodada de noite. Com poucos km no período, não há nota.

// MinScoreKm: abaixo disso, a nota não diz nada.
const MinScoreKm = 20.0

// DrivingRow é a direção de um veículo no período.
type DrivingRow struct {
	VehicleID        uuid.UUID  `json:"vehicleId"`
	Name             string     `json:"name"`
	Plate            string     `json:"plate"`
	GroupID          *uuid.UUID `json:"groupId"`
	OwnerName        string     `json:"ownerName,omitempty"`
	DistanceKm       float64    `json:"distanceKm"`
	MovingSeconds    int        `json:"movingSeconds"`
	ActiveDays       int        `json:"activeDays"`
	MaxSpeedKmh      float64    `json:"maxSpeedKmh"`
	Overspeeds       int        `json:"overspeeds"`
	HighSpeedSeconds int        `json:"highSpeedSeconds"`
	HarshBrakes      int        `json:"harshBrakes"`
	HarshAccels      int        `json:"harshAccels"`
	NightKm          float64    `json:"nightKm"`
	NightPercent     float64    `json:"nightPercent"`
	// Score: nulo com menos de MinScoreKm.
	Score *int `json:"score"`
	// Penalties: os pontos tirados por cada coisa (para a tela explicar).
	Penalties Penalties `json:"penalties"`
}

// Penalties são os pontos tirados da nota.
type Penalties struct {
	Overspeed float64 `json:"overspeed"`
	HighSpeed float64 `json:"highSpeed"`
	Harsh     float64 `json:"harsh"`
	Night     float64 `json:"night"`
}

// Score calcula a nota e os pontos tirados. Por 100 km: cada excesso de
// velocidade tira 4 (até 35) e cada freada ou arrancada brusca, 3 (até 25);
// cada 1% do tempo andando acima de 120 km/h tira 1 (até 20); cada 10% dos
// km rodados de noite, 1 (até 10).
func Score(r DrivingRow) (*int, Penalties) {
	var p Penalties
	if r.DistanceKm < MinScoreKm {
		return nil, p
	}
	per100 := 100 / r.DistanceKm
	p.Overspeed = math.Min(35, float64(r.Overspeeds)*per100*4)
	p.Harsh = math.Min(25, float64(r.HarshBrakes+r.HarshAccels)*per100*3)
	if r.MovingSeconds > 0 {
		p.HighSpeed = math.Min(20, 100*float64(r.HighSpeedSeconds)/float64(r.MovingSeconds))
	}
	p.Night = math.Min(10, r.NightPercent/10)
	p.Overspeed, p.Harsh, p.HighSpeed, p.Night = round1(p.Overspeed), round1(p.Harsh), round1(p.HighSpeed), round1(p.Night)
	score := int(math.Round(math.Max(0, 100-p.Overspeed-p.Harsh-p.HighSpeed-p.Night)))
	return &score, p
}

// Driving é a direção de cada veículo no período [from, to] (dias).
func (s *Service) Driving(ctx context.Context, vehicles []Vehicle, from, to time.Time) ([]DrivingRow, error) {
	rows, err := s.db.Query(ctx, `
		SELECT s.vehicle_id, sum(s.distance_m), sum(s.moving_s), count(*) FILTER (WHERE s.moving_s > 0),
			max(s.max_speed_kmh), sum(s.overspeeds), sum(s.high_speed_s), sum(s.harsh_brakes), sum(s.harsh_accels),
			sum(s.night_m)`+statsFrom+`
		GROUP BY s.vehicle_id`, vehicleIDs(vehicles), from.Format(time.DateOnly), to.Format(time.DateOnly), s.tz)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	byVehicle := map[uuid.UUID]DrivingRow{}
	for rows.Next() {
		var r DrivingRow
		var meters, nightMeters float64
		if err := rows.Scan(&r.VehicleID, &meters, &r.MovingSeconds, &r.ActiveDays, &r.MaxSpeedKmh, &r.Overspeeds,
			&r.HighSpeedSeconds, &r.HarshBrakes, &r.HarshAccels, &nightMeters); err != nil {
			return nil, err
		}
		r.DistanceKm, r.NightKm = meters/1000, nightMeters/1000
		if meters > 0 {
			r.NightPercent = round1(100 * nightMeters / meters)
		}
		byVehicle[r.VehicleID] = r
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]DrivingRow, 0, len(vehicles))
	for _, v := range vehicles {
		r := byVehicle[v.ID]
		r.VehicleID, r.Name, r.Plate, r.GroupID, r.OwnerName = v.ID, v.Name, v.Plate, v.GroupID, v.OwnerName
		r.Score, r.Penalties = Score(r)
		out = append(out, r)
	}
	return out, nil
}

// Driving é a planilha da direção (os dados de uso, para a seguradora).
func (s Sheet) Driving(rows []DrivingRow, vehicles []Vehicle) [][]string {
	byID := vehicleIndex(vehicles)
	out := [][]string{append(append([]string{"Veículo", "Placa"}, s.ExtraHeader()...), "Nota", "Km", "Dias com movimento",
		"Excessos de velocidade", "Tempo acima de 120 km/h", "Freadas bruscas", "Arrancadas bruscas", "Km à noite",
		"À noite (%)", "Velocidade máxima (km/h)")}
	for _, row := range rows {
		score := ""
		if row.Score != nil {
			score = strconv.Itoa(*row.Score)
		}
		out = append(out, append(append([]string{row.Name, row.Plate}, s.ExtraCells(byID[row.VehicleID])...),
			score, Decimal(row.DistanceKm, 1), strconv.Itoa(row.ActiveDays), strconv.Itoa(row.Overspeeds),
			Hours(row.HighSpeedSeconds), strconv.Itoa(row.HarshBrakes), strconv.Itoa(row.HarshAccels),
			Decimal(row.NightKm, 1), Decimal(row.NightPercent, 1), Decimal(row.MaxSpeedKmh, 0)))
	}
	return out
}
