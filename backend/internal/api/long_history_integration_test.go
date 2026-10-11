package api

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/retention"
)

// O histórico longo, no banco: num prazo de 1 ano, os últimos 30 dias ficam
// inteiros; mais velho que isso, andando fica um ponto por minuto e parado um
// a cada 15 minutos (compactar de novo não muda nada); depois do prazo, nada.
// Num prazo de 30 dias, nada é compactado. Precisa de FARBO_TEST_DATABASE_URL.
func TestLongHistoryCompaction(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	var owner, longDevice, shortDevice uuid.UUID
	if err := db.QueryRow(ctx, `INSERT INTO users (email, name, role, password_hash, history_retention_days)
		VALUES ('longo@historico.test', 'Longo', 'customer', 'x', 365) RETURNING id`).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	for _, d := range []*uuid.UUID{&longDevice, &shortDevice} {
		if err := db.QueryRow(ctx, `INSERT INTO devices (imei) VALUES ($1) RETURNING id`, uuid.NewString()[:15]).Scan(d); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(ctx, `INSERT INTO vehicles (name, device_id, owner_id) VALUES ('Van longa', $1, $2)`, longDevice, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO vehicles (name, device_id, owner_id, history_retention_days) VALUES ('Van curta', $1, $2, 30)`,
		shortDevice, owner); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Hour)
	// Uma hora a cada 10 s: 30 min andando (5 por minuto), 30 parado.
	hour := func(device uuid.UUID, start time.Time) {
		t.Helper()
		if _, err := db.Exec(ctx, `
			INSERT INTO positions (device_id, gps_timestamp, latitude, longitude, speed_kmh)
			SELECT $1, $2::timestamptz + make_interval(secs => g * 10), -23.55, -46.63, CASE WHEN g < 180 THEN 40 ELSE 0 END
			FROM generate_series(0, 359) g`, device, start); err != nil {
			t.Fatal(err)
		}
	}
	recent, old, expired := now.Add(-10*24*time.Hour), now.Add(-31*24*time.Hour), now.Add(-400*24*time.Hour)
	for _, start := range []time.Time{recent, old, expired} {
		hour(longDevice, start)
	}
	hour(shortDevice, now.Add(-20*24*time.Hour))
	count := func(device uuid.UUID, from time.Time) int {
		var n int
		if err := db.QueryRow(ctx, `SELECT count(*) FROM positions WHERE device_id = $1 AND gps_timestamp >= $2 AND gps_timestamp < $3`,
			device, from, from.Add(time.Hour)).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	svc := retention.NewService(db, 30, slog.New(slog.NewTextHandler(io.Discard, nil)))
	res, err := svc.Cleanup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Andando, 30 min = 30 pontos; parado, 30 min = 2 pontos (um a cada 15).
	if got := count(longDevice, old); got != 32 {
		t.Errorf("a hora de 31 dias atrás ficou com %d pontos, quer 32", got)
	}
	if got := count(longDevice, recent); got != 360 {
		t.Errorf("os últimos 30 dias perderam pontos: %d", got)
	}
	if got := count(longDevice, expired); got != 0 {
		t.Errorf("passou do prazo de 1 ano e ficou: %d", got)
	}
	if got := count(shortDevice, now.Add(-20*24*time.Hour)); got != 360 {
		t.Errorf("prazo de 30 dias não compacta: %d", got)
	}
	if res.Compacted != 360-32 || res.Positions != 360 {
		t.Errorf("limpeza = %+v", res)
	}
	// Compactar de novo não muda nada.
	if res, err := svc.Cleanup(ctx); err != nil || res.Compacted != 0 || count(longDevice, old) != 32 {
		t.Errorf("segunda limpeza = %+v %v", res, err)
	}
}
