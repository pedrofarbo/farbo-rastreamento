package main

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// testDevices são os rastreadores de teste: o mesmo prefixo dos IMEIs que a
// carga usa.
const testDevices = `SELECT id, imei FROM devices WHERE imei LIKE $1 || '%'`

// setup cadastra os rastreadores de teste (com um veículo cada, do cliente
// owner ou da central) e, com historyDays, gera o histórico deles: o que uma
// frota acumula antes de o teste começar.
func setup(ctx context.Context, db *pgxpool.Pool, prefix string, devices int, owner string, historyDays int) error {
	var ownerID any
	if owner != "" {
		ownerID = owner
	}
	tag, err := db.Exec(ctx, `
		WITH d AS (
			INSERT INTO devices (imei, model, protocol)
			SELECT $1 || lpad(i::text, 15 - length($1), '0'), 'J16', 'GT06' FROM generate_series(1, $2) AS i
			ON CONFLICT (imei) DO NOTHING
			RETURNING id, imei)
		INSERT INTO vehicles (name, plate, device_id, owner_id)
		SELECT 'Carga ' || right(imei, 5), 'LD' || right(imei, 5), id, $3::uuid FROM d`, prefix, devices, ownerID)
	if err != nil {
		return fmt.Errorf("cadastro dos rastreadores: %w", err)
	}
	fmt.Printf("rastreadores de teste: %d novos\n", tag.RowsAffected())

	for k := historyDays; k >= 1; k-- {
		began := time.Now()
		day := time.Now().AddDate(0, 0, -k).Format(time.DateOnly)
		n, err := historyDay(ctx, db, prefix, day, k)
		if err != nil {
			return fmt.Errorf("histórico de %s: %w", day, err)
		}
		fmt.Printf("histórico de %s: %d posições (%s)\n", day, n, time.Since(began).Round(time.Second))
	}
	if historyDays == 0 {
		return nil
	}

	// O histórico gerado já tem o resumo diário (acima): o cálculo dos
	// relatórios só pega o que a carga mandar.
	if _, err := db.Exec(ctx, `
		UPDATE report_progress SET last_position_id = (SELECT COALESCE(max(id), 0) FROM positions)
		WHERE name = 'daily_stats'`); err != nil {
		return fmt.Errorf("marcador dos relatórios: %w", err)
	}
	began := time.Now()
	if _, err := db.Exec(ctx, `VACUUM (ANALYZE) positions`); err != nil {
		return fmt.Errorf("vacuum: %w", err)
	}
	var size string
	_ = db.QueryRow(ctx, `SELECT pg_size_pretty(pg_total_relation_size('positions'))`).Scan(&size)
	fmt.Printf("vacuum das posições: %s; tabela com índices: %s\n", time.Since(began).Round(time.Second), size)
	return nil
}

// historyDay gera um dia de uma frota de entregas, no ritmo do J16: andando
// das 7h às 10h e das 14h às 17h (uma posição a cada 30 s), parado no resto
// (uma por hora). Cada veículo dá voltas perto da própria base. Vêm junto a
// ignição, um excesso de velocidade a cada 5 veículos e o resumo do dia.
func historyDay(ctx context.Context, db *pgxpool.Pool, prefix, day string, k int) (int64, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	const base = `
		WITH d AS (
			SELECT id, imei, -23.75 + ('x' || substr(md5(imei), 1, 6))::bit(24)::int / 16777215.0 * 0.4 AS lat0,
			       -46.83 + ('x' || substr(md5(imei), 7, 6))::bit(24)::int / 16777215.0 * 0.4 AS lon0
			FROM devices WHERE imei LIKE $1 || '%'),
		day AS (SELECT ($2::date)::timestamp AT TIME ZONE 'America/Sao_Paulo' AS start)`
	tag, err := tx.Exec(ctx, base+`,
		t AS (
			SELECT s AS at, true AS moving, extract(epoch FROM s - (day.start + interval '7 hours')) AS secs
			FROM day, generate_series(day.start + interval '7 hours', day.start + interval '9 hours 59 minutes 30 seconds', interval '30 seconds') s
			UNION ALL
			SELECT s, true, extract(epoch FROM s - (day.start + interval '14 hours'))
			FROM day, generate_series(day.start + interval '14 hours', day.start + interval '16 hours 59 minutes 30 seconds', interval '30 seconds') s
			UNION ALL
			SELECT s, false, 0 FROM day, generate_series(day.start, day.start + interval '23 hours', interval '1 hour') s
			WHERE extract(hour FROM s - day.start) NOT IN (7, 8, 9, 14, 15, 16))
		INSERT INTO positions (device_id, gps_timestamp, received_at, latitude, longitude, speed_kmh, heading,
			gps_valid, satellites, acc, protocol, source)
		SELECT d.id, t.at, t.at + interval '2 seconds',
			d.lat0 + 0.03 * sin(t.secs * 0.00292), d.lon0 + 0.03 * (cos(t.secs * 0.00292) - 1),
			CASE WHEN t.moving THEN 35 ELSE 0 END, CASE WHEN t.moving THEN 90 END,
			true, 9, t.moving, 'GT06', 'gps'
		FROM d CROSS JOIN t
		ORDER BY t.at, d.id`, prefix, day)
	if err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, base+`
		INSERT INTO vehicle_events (device_id, type, timestamp, latitude, longitude, speed_kmh, metadata)
		SELECT d.id, e.type, day.start + e.at, d.lat0, d.lon0, e.speed, e.meta::jsonb
		FROM d CROSS JOIN day CROSS JOIN (VALUES
			('IGNITION_ON', interval '7 hours', 0, '{"protocol":"GT06"}', false),
			('IGNITION_OFF', interval '10 hours', 0, '{"protocol":"GT06"}', false),
			('IGNITION_ON', interval '14 hours', 0, '{"protocol":"GT06"}', false),
			('IGNITION_OFF', interval '17 hours', 0, '{"protocol":"GT06"}', false),
			('OVERSPEED', interval '8 hours', 95, '{"limitKmh":80}', true),
			('OVERSPEED_END', interval '8 hours 2 minutes', 60, '{"limitKmh":80}', true)
		) AS e(type, at, speed, meta, speeding)
		WHERE NOT e.speeding OR (right(d.imei, 5)::int + $3) % 5 = 0`, prefix, day, k); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, base+`
		INSERT INTO vehicle_daily_stats (device_id, day, vehicle_id, distance_m, moving_s, idle_s, trips,
			max_speed_kmh, overspeeds, first_move_at, last_move_at, points)
		SELECT d.id, $2::date, v.id, 105000, 21600, 600, 2, 52,
			CASE WHEN (right(d.imei, 5)::int + $3) % 5 = 0 THEN 1 ELSE 0 END,
			day.start + interval '7 hours', day.start + interval '17 hours', 738
		FROM d CROSS JOIN day LEFT JOIN vehicles v ON v.device_id = d.id
		ON CONFLICT (device_id, day) DO NOTHING`, prefix, day, k); err != nil {
		return 0, err
	}
	return tag.RowsAffected(), tx.Commit(ctx)
}

// teardown apaga os rastreadores de teste, os veículos deles e, em cascata,
// as posições e os eventos.
func teardown(ctx context.Context, db *pgxpool.Pool, prefix string) error {
	if _, err := db.Exec(ctx, `DELETE FROM vehicles WHERE device_id IN (SELECT id FROM (`+testDevices+`) d)`, prefix); err != nil {
		return fmt.Errorf("veículos de teste: %w", err)
	}
	tag, err := db.Exec(ctx, `DELETE FROM devices WHERE imei LIKE $1 || '%'`, prefix)
	if err != nil {
		return fmt.Errorf("rastreadores de teste: %w", err)
	}
	fmt.Printf("rastreadores de teste apagados: %d\n", tag.RowsAffected())
	return nil
}
