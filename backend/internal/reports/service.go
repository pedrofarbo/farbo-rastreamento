package reports

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// MaxDays é o período mais longo de um relatório.
const MaxDays = 31

// Vehicle é um veículo da conta nos relatórios.
type Vehicle struct {
	ID        uuid.UUID  `json:"id"`
	Name      string     `json:"name"`
	Plate     string     `json:"plate"`
	DeviceID  *uuid.UUID `json:"-"`
	CreatedAt time.Time  `json:"-"`
}

// SummaryRow é o resumo de um veículo no período.
type SummaryRow struct {
	VehicleID     uuid.UUID  `json:"vehicleId"`
	Name          string     `json:"name"`
	Plate         string     `json:"plate"`
	DistanceKm    float64    `json:"distanceKm"`
	MovingSeconds int        `json:"movingSeconds"`
	IdleSeconds   int        `json:"idleSeconds"`
	Trips         int        `json:"trips"`
	MaxSpeedKmh   float64    `json:"maxSpeedKmh"`
	Overspeeds    int        `json:"overspeeds"`
	ActiveDays    int        `json:"activeDays"`
	FirstMoveAt   *time.Time `json:"firstMoveAt"`
	LastMoveAt    *time.Time `json:"lastMoveAt"`
}

// DailyRow é um dia de um veículo.
type DailyRow struct {
	VehicleID     uuid.UUID  `json:"vehicleId"`
	Day           string     `json:"day"`
	DistanceKm    float64    `json:"distanceKm"`
	MovingSeconds int        `json:"movingSeconds"`
	IdleSeconds   int        `json:"idleSeconds"`
	Trips         int        `json:"trips"`
	MaxSpeedKmh   float64    `json:"maxSpeedKmh"`
	Overspeeds    int        `json:"overspeeds"`
	FirstMoveAt   *time.Time `json:"firstMoveAt"`
	LastMoveAt    *time.Time `json:"lastMoveAt"`
}

// Overspeed é um excesso de velocidade: quando começou e terminou, a
// velocidade máxima no trecho e o limite.
type Overspeed struct {
	VehicleID   uuid.UUID  `json:"vehicleId"`
	Start       time.Time  `json:"start"`
	End         *time.Time `json:"end"`
	Latitude    *float64   `json:"latitude"`
	Longitude   *float64   `json:"longitude"`
	MaxSpeedKmh float64    `json:"maxSpeedKmh"`
	LimitKmh    *float64   `json:"limitKmh"`
}

// Service consulta os relatórios. Os dias são no fuso da central (o mesmo do
// Aggregator).
type Service struct {
	db  *database.DB
	loc *time.Location
	tz  string
}

func NewService(db *database.DB, timezone string) *Service {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		timezone = "America/Sao_Paulo"
		loc = time.FixedZone("BRT", -3*60*60)
	}
	return &Service{db: db, loc: loc, tz: timezone}
}

// Location é o fuso dos dias dos relatórios.
func (s *Service) Location() *time.Location { return s.loc }

// Vehicles são os veículos da conta (os próprios, não os compartilhados).
func (s *Service) Vehicles(ctx context.Context, ownerID uuid.UUID) ([]Vehicle, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, name, COALESCE(plate, ''), device_id, created_at FROM vehicles
		WHERE owner_id = $1 ORDER BY name, id`, ownerID)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	var out []Vehicle
	for rows.Next() {
		var v Vehicle
		if err := rows.Scan(&v.ID, &v.Name, &v.Plate, &v.DeviceID, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func vehicleIDs(vehicles []Vehicle) []uuid.UUID {
	ids := make([]uuid.UUID, len(vehicles))
	for i, v := range vehicles {
		ids[i] = v.ID
	}
	return ids
}

// Os dias de cada veículo no período, a partir do dia em que ele entrou no
// sistema (antes disso, o rastreador podia ser de outro).
const statsFrom = `
	FROM vehicle_daily_stats s JOIN vehicles v ON v.id = s.vehicle_id
	WHERE s.vehicle_id = ANY($1) AND s.day BETWEEN $2 AND $3 AND s.day >= (v.created_at AT TIME ZONE $4)::date`

// Summary é o resumo de cada veículo no período [from, to] (dias). Os sem
// movimento aparecem zerados.
func (s *Service) Summary(ctx context.Context, vehicles []Vehicle, from, to time.Time) ([]SummaryRow, error) {
	rows, err := s.db.Query(ctx, `
		SELECT s.vehicle_id, sum(s.distance_m), sum(s.moving_s), sum(s.idle_s), sum(s.trips), max(s.max_speed_kmh),
			sum(s.overspeeds), count(*) FILTER (WHERE s.moving_s > 0), min(s.first_move_at), max(s.last_move_at)`+statsFrom+`
		GROUP BY s.vehicle_id`, vehicleIDs(vehicles), from.Format(time.DateOnly), to.Format(time.DateOnly), s.tz)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	byVehicle := map[uuid.UUID]SummaryRow{}
	for rows.Next() {
		var r SummaryRow
		var meters float64
		if err := rows.Scan(&r.VehicleID, &meters, &r.MovingSeconds, &r.IdleSeconds, &r.Trips, &r.MaxSpeedKmh,
			&r.Overspeeds, &r.ActiveDays, &r.FirstMoveAt, &r.LastMoveAt); err != nil {
			return nil, err
		}
		r.DistanceKm = meters / 1000
		byVehicle[r.VehicleID] = r
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]SummaryRow, 0, len(vehicles))
	for _, v := range vehicles {
		r := byVehicle[v.ID]
		r.VehicleID, r.Name, r.Plate = v.ID, v.Name, v.Plate
		out = append(out, r)
	}
	return out, nil
}

// Daily são os dias de cada veículo no período (só os dias com posições).
func (s *Service) Daily(ctx context.Context, vehicles []Vehicle, from, to time.Time) ([]DailyRow, error) {
	rows, err := s.db.Query(ctx, `
		SELECT s.vehicle_id, s.day, s.distance_m, s.moving_s, s.idle_s, s.trips, s.max_speed_kmh, s.overspeeds,
			s.first_move_at, s.last_move_at`+statsFrom+`
		ORDER BY v.name, s.vehicle_id, s.day`, vehicleIDs(vehicles), from.Format(time.DateOnly), to.Format(time.DateOnly), s.tz)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	out := []DailyRow{}
	for rows.Next() {
		var r DailyRow
		var day time.Time
		var meters float64
		if err := rows.Scan(&r.VehicleID, &day, &meters, &r.MovingSeconds, &r.IdleSeconds, &r.Trips, &r.MaxSpeedKmh,
			&r.Overspeeds, &r.FirstMoveAt, &r.LastMoveAt); err != nil {
			return nil, err
		}
		r.Day, r.DistanceKm = day.Format(time.DateOnly), meters/1000
		out = append(out, r)
	}
	return out, rows.Err()
}

// Trips são as viagens de um veículo entre from e to (instantes), a partir
// das posições do rastreador dele.
func (s *Service) Trips(ctx context.Context, v Vehicle, from, to time.Time) ([]Trip, error) {
	if v.DeviceID == nil {
		return []Trip{}, nil
	}
	if from.Before(v.CreatedAt) {
		from = v.CreatedAt
	}
	rows, err := s.db.Query(ctx, `
		SELECT gps_timestamp, latitude, longitude, speed_kmh, acc, gps_valid FROM positions
		WHERE device_id = $1 AND gps_timestamp >= $2 AND gps_timestamp < $3
		ORDER BY gps_timestamp`, *v.DeviceID, from, to)
	if err != nil {
		return nil, database.MapError(err)
	}
	points, err := scanPoints(rows)
	if err != nil {
		return nil, err
	}
	trips := Trips(points)
	if trips == nil {
		trips = []Trip{}
	}
	return trips, nil
}

// MaxOverspeeds limita a lista de excessos de um relatório.
const MaxOverspeeds = 5000

// Overspeeds são os excessos de velocidade dos veículos entre from e to
// (instantes): cada início com o fim seguinte e a velocidade máxima no
// trecho (pelas posições, enquanto existirem; senão, a da entrada).
func (s *Service) Overspeeds(ctx context.Context, vehicles []Vehicle, from, to time.Time) ([]Overspeed, error) {
	deviceOf := map[uuid.UUID]Vehicle{}
	devices := []uuid.UUID{}
	for _, v := range vehicles {
		if v.DeviceID != nil {
			deviceOf[*v.DeviceID] = v
			devices = append(devices, *v.DeviceID)
		}
	}
	if len(devices) == 0 {
		return []Overspeed{}, nil
	}
	rows, err := s.db.Query(ctx, `
		WITH ev AS (
			SELECT device_id, type, timestamp, latitude, longitude, speed_kmh,
				NULLIF(metadata->>'limitKmh', '')::double precision AS limit_kmh,
				LEAD(type) OVER w AS next_type, LEAD(timestamp) OVER w AS next_at
			FROM vehicle_events
			WHERE device_id = ANY($1) AND type IN ('OVERSPEED', 'OVERSPEED_END') AND timestamp >= $2 AND timestamp < $3
			WINDOW w AS (PARTITION BY device_id ORDER BY timestamp))
		SELECT e.device_id, e.timestamp, CASE WHEN e.next_type = 'OVERSPEED_END' THEN e.next_at END,
			e.latitude, e.longitude,
			GREATEST(COALESCE(e.speed_kmh, 0), COALESCE((
				SELECT max(p.speed_kmh) FROM positions p
				WHERE p.device_id = e.device_id AND p.speed_kmh <= $4
				  AND p.gps_timestamp >= e.timestamp
				  AND p.gps_timestamp <= COALESCE(CASE WHEN e.next_type = 'OVERSPEED_END' THEN e.next_at END,
				      e.timestamp + interval '10 minutes')), 0)),
			e.limit_kmh
		FROM ev e WHERE e.type = 'OVERSPEED'
		ORDER BY e.timestamp LIMIT $5`, devices, from, to, MaxKmh, MaxOverspeeds)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	out := []Overspeed{}
	for rows.Next() {
		var o Overspeed
		var device uuid.UUID
		if err := rows.Scan(&device, &o.Start, &o.End, &o.Latitude, &o.Longitude, &o.MaxSpeedKmh, &o.LimitKmh); err != nil {
			return nil, err
		}
		v := deviceOf[device]
		if o.Start.Before(v.CreatedAt) {
			continue
		}
		o.VehicleID = v.ID
		out = append(out, o)
	}
	return out, rows.Err()
}
