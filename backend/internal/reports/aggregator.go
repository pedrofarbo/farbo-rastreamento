package reports

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// O ritmo do cálculo: a cada aggregateEvery, as posições novas, em lotes de
// batchPositions (a primeira rodada, com o histórico todo, vai aos poucos),
// até alcançar a última ou passar roundBudget.
const (
	aggregateEvery = 5 * time.Minute
	batchPositions = 200_000
	roundBudget    = 2 * time.Minute
	settleAfter    = 30 * time.Second
	progressName   = "daily_stats"
)

// Aggregator mantém vehicle_daily_stats em dia: a cada rodada, refaz os dias
// (de cada rastreador) que receberam posições novas desde a anterior — as
// atrasadas da memória do rastreador também. Refazer é idempotente: duas
// instâncias ao mesmo tempo só repetem trabalho.
type Aggregator struct {
	db  *database.DB
	loc *time.Location
	// tz é o nome do fuso para o Postgres (o dia de cada posição).
	tz  string
	now func() time.Time
	log *slog.Logger
}

func NewAggregator(db *database.DB, timezone string, log *slog.Logger) *Aggregator {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		timezone = "America/Sao_Paulo"
		loc = time.FixedZone("BRT", -3*60*60)
	}
	return &Aggregator{db: db, loc: loc, tz: timezone, now: time.Now, log: log.With("component", "reports")}
}

// Run calcula na largada e a cada aggregateEvery, até o contexto acabar.
func (a *Aggregator) Run(ctx context.Context) {
	ticker := time.NewTicker(aggregateEvery)
	defer ticker.Stop()
	for {
		if n, err := a.Round(ctx); err != nil && ctx.Err() == nil {
			a.log.Error("falha ao calcular o resumo diário da frota", "err", err)
		} else if n > 0 {
			a.log.Info("resumo diário da frota atualizado", "dias", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Round processa as posições novas e devolve quantos dias refez. O alvo é
// fixado na largada (a mais nova assentada naquele instante): com posições
// chegando sem parar, perseguir as que assentam durante a rodada refazia os
// mesmos dias de novo até o fim do roundBudget.
func (a *Aggregator) Round(ctx context.Context) (int, error) {
	deadline := a.now().Add(roundBudget)
	target, err := a.settled(ctx)
	if err != nil {
		return 0, err
	}
	done := 0
	for ctx.Err() == nil {
		var last int64
		if err := a.db.QueryRow(ctx, `SELECT last_position_id FROM report_progress WHERE name = $1`, progressName).Scan(&last); err != nil {
			return done, database.MapError(err)
		}
		if last >= target {
			return done, nil
		}
		// Os lotes contam a partir da primeira posição depois do marcador:
		// um buraco na numeração (posições apagadas) é pulado de uma vez.
		var first *int64
		if err := a.db.QueryRow(ctx, `SELECT min(id) FROM positions WHERE id > $1 AND id <= $2`, last, target).Scan(&first); err != nil {
			return done, database.MapError(err)
		}
		upto := target
		if first != nil {
			upto = min(target, *first-1+batchPositions)
		}
		days, err := a.touched(ctx, last, upto)
		if err != nil {
			return done, err
		}
		for _, d := range days {
			if err := a.Recompute(ctx, d.device, d.day); err != nil {
				return done, fmt.Errorf("rastreador %s, dia %s: %w", d.device, d.day.Format(time.DateOnly), err)
			}
			done++
		}
		tag, err := a.db.Exec(ctx, `
			UPDATE report_progress SET last_position_id = $3, updated_at = NOW()
			WHERE name = $1 AND last_position_id = $2`, progressName, last, upto)
		if err != nil {
			return done, database.MapError(err)
		}
		if tag.RowsAffected() == 0 || a.now().After(deadline) {
			// Outra instância avançou (ou o tempo da rodada acabou): a próxima segue.
			return done, nil
		}
	}
	return done, ctx.Err()
}

// settled é o id da posição mais nova recebida há mais de settleAfter (uma
// gravação ainda em curso, com id menor, não fica para trás do marcador).
// Pelo índice do id, de trás para frente: só passa pelas posições dos
// últimos settleAfter. A procura pela hora de recebimento lia a tabela
// inteira — 4 s com 26 milhões de posições, a cada volta da rodada.
func (a *Aggregator) settled(ctx context.Context) (int64, error) {
	var id int64
	err := a.db.QueryRow(ctx, `SELECT id FROM positions WHERE received_at < $1 ORDER BY id DESC LIMIT 1`,
		a.now().Add(-settleAfter)).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return id, database.MapError(err)
}

type deviceDay struct {
	device uuid.UUID
	day    time.Time
}

// touched são os dias (no fuso da central) de cada rastreador com posições
// entre os ids (after, upto].
func (a *Aggregator) touched(ctx context.Context, after, upto int64) ([]deviceDay, error) {
	rows, err := a.db.Query(ctx, `
		SELECT DISTINCT device_id, (gps_timestamp AT TIME ZONE $3)::date
		FROM positions WHERE id > $1 AND id <= $2`, after, upto, a.tz)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	var out []deviceDay
	for rows.Next() {
		var d deviceDay
		if err := rows.Scan(&d.device, &d.day); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// Recompute refaz o resumo de um dia de um rastreador.
func (a *Aggregator) Recompute(ctx context.Context, device uuid.UUID, day time.Time) error {
	start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, a.loc)
	end := start.AddDate(0, 0, 1)
	// A posição de antes do dia (até MaxGap) liga o primeiro trecho.
	rows, err := a.db.Query(ctx, `
		(SELECT gps_timestamp, latitude, longitude, speed_kmh, acc, gps_valid FROM positions
		 WHERE device_id = $1 AND gps_timestamp < $2 AND gps_timestamp >= $4
		 ORDER BY gps_timestamp DESC LIMIT 1)
		UNION ALL
		(SELECT gps_timestamp, latitude, longitude, speed_kmh, acc, gps_valid FROM positions
		 WHERE device_id = $1 AND gps_timestamp >= $2 AND gps_timestamp < $3
		 ORDER BY gps_timestamp)`, device, start, end, start.Add(-MaxGap))
	if err != nil {
		return database.MapError(err)
	}
	points, err := scanPoints(rows)
	if err != nil {
		return err
	}
	s := DayStats(points, start, end)

	var overspeeds int
	if err := a.db.QueryRow(ctx, `
		SELECT count(*) FROM vehicle_events
		WHERE device_id = $1 AND type = 'OVERSPEED' AND timestamp >= $2 AND timestamp < $3`,
		device, start, end).Scan(&overspeeds); err != nil {
		return database.MapError(err)
	}
	var vehicle *uuid.UUID
	if err := a.db.QueryRow(ctx, `SELECT id FROM vehicles WHERE device_id = $1`, device).Scan(&vehicle); err != nil &&
		!errors.Is(err, pgx.ErrNoRows) {
		return database.MapError(err)
	}
	_, err = a.db.Exec(ctx, `
		INSERT INTO vehicle_daily_stats (device_id, day, vehicle_id, distance_m, moving_s, idle_s, trips,
			max_speed_kmh, overspeeds, first_move_at, last_move_at, points, night_m, harsh_brakes, harsh_accels,
			high_speed_s, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, NOW())
		ON CONFLICT (device_id, day) DO UPDATE SET vehicle_id = COALESCE(EXCLUDED.vehicle_id, vehicle_daily_stats.vehicle_id),
			distance_m = EXCLUDED.distance_m, moving_s = EXCLUDED.moving_s, idle_s = EXCLUDED.idle_s,
			trips = EXCLUDED.trips, max_speed_kmh = EXCLUDED.max_speed_kmh, overspeeds = EXCLUDED.overspeeds,
			first_move_at = EXCLUDED.first_move_at, last_move_at = EXCLUDED.last_move_at,
			points = EXCLUDED.points, night_m = EXCLUDED.night_m, harsh_brakes = EXCLUDED.harsh_brakes,
			harsh_accels = EXCLUDED.harsh_accels, high_speed_s = EXCLUDED.high_speed_s, updated_at = NOW()`,
		device, start.Format(time.DateOnly), vehicle, s.DistanceM, s.MovingS, s.IdleS, s.Trips, s.MaxSpeed,
		overspeeds, s.FirstMove, s.LastMove, s.Points, s.NightM, s.HarshBrakes, s.HarshAccels, s.HighSpeedS)
	return database.MapError(err)
}

func scanPoints(rows pgx.Rows) ([]Point, error) {
	defer rows.Close()
	var out []Point
	for rows.Next() {
		var p Point
		if err := rows.Scan(&p.At, &p.Lat, &p.Lon, &p.SpeedKmh, &p.Acc, &p.Valid); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
