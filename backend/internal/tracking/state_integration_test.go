package tracking

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// stateDB sobe um schema descartável com as migrations. Precisa de um
// Postgres em FARBO_TEST_DATABASE_URL; sem ele, o teste é pulado.
func stateDB(t *testing.T) *database.DB {
	t.Helper()
	dsn := os.Getenv("FARBO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("defina FARBO_TEST_DATABASE_URL (Postgres descartável) para rodar o teste com banco")
	}
	ctx := context.Background()
	schema := "test_states_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		_ = admin.Close(context.Background())
	})
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	db := &database.DB{Pool: pool}
	if err := db.Migrate(ctx, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatal(err)
	}
	return db
}

// Com o Run, as mudanças de estado ficam na memória na hora e vão ao banco
// em lote: três mudanças do mesmo rastreador viram uma gravação (a última),
// e o encerramento grava o que faltava.
func TestStateStoreFlushesInBatches(t *testing.T) {
	db := stateDB(t)
	ctx := context.Background()
	var a, b uuid.UUID
	for _, dest := range []*uuid.UUID{&a, &b} {
		if err := db.QueryRow(ctx, `INSERT INTO devices (imei) VALUES ($1) RETURNING id`,
			"8690000"+uuid.NewString()[:8]).Scan(dest); err != nil {
			t.Fatal(err)
		}
	}
	repo := NewStateRepository(db)
	store := NewStateStore(repo)
	runCtx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		store.Run(runCtx, slog.New(slog.NewTextHandler(io.Discard, nil)))
		close(done)
	}()
	for !store.running.Load() {
		time.Sleep(time.Millisecond)
	}

	on, off := true, false
	store.Update(ctx, a, func(st *State) { st.ACC = &on })
	store.Update(ctx, a, func(st *State) { st.Overspeed = true })
	store.Update(ctx, a, func(st *State) { st.ACC = &off })
	store.Update(ctx, b, func(st *State) { st.RelayOn = &on })
	if got := store.Get(a); got == nil || got.ACC == nil || *got.ACC || !got.Overspeed {
		t.Fatalf("na memória: %+v", got)
	}
	if err := store.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	saved, err := repo.LoadAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st := saved[a]; st == nil || st.ACC == nil || *st.ACC || !st.Overspeed || saved[b] == nil {
		t.Fatalf("no banco: %+v", saved)
	}

	// O que muda depois da última gravação vai no encerramento.
	store.Update(ctx, b, func(st *State) { st.RelayOn = &off })
	stop()
	<-done
	saved, _ = repo.LoadAll(ctx)
	if st := saved[b]; st == nil || st.RelayOn == nil || *st.RelayOn {
		t.Fatalf("no encerramento: %+v", saved[b])
	}
}
