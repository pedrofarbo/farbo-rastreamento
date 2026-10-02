package alerts

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

// storeDB sobe um schema descartável com as migrations. Precisa de um
// Postgres em FARBO_TEST_DATABASE_URL; sem ele, o teste é pulado.
func storeDB(t *testing.T) *database.DB {
	t.Helper()
	dsn := os.Getenv("FARBO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("defina FARBO_TEST_DATABASE_URL (Postgres descartável) para rodar o teste com banco")
	}
	ctx := context.Background()
	schema := "test_alerts_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
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

func TestDBStore(t *testing.T) {
	db := storeDB(t)
	ctx := context.Background()

	var ownerID, deviceID, vehicleID, orphanDevice uuid.UUID
	mustScan := func(sql string, dest *uuid.UUID, args ...any) {
		t.Helper()
		if err := db.QueryRow(ctx, sql, args...).Scan(dest); err != nil {
			t.Fatal(err)
		}
	}
	mustScan(`INSERT INTO users (email, name, role, password_hash) VALUES ('ana@cliente.test', 'Ana', 'customer', 'x') RETURNING id`, &ownerID)
	mustScan(`INSERT INTO devices (imei) VALUES ('869000000000001') RETURNING id`, &deviceID)
	mustScan(`INSERT INTO devices (imei) VALUES ('869000000000002') RETURNING id`, &orphanDevice)
	mustScan(`INSERT INTO vehicles (name, plate, device_id, owner_id) VALUES ('Moto da Ana', 'ABC1D23', $1, $2) RETURNING id`,
		&vehicleID, deviceID, ownerID)

	suspended := false
	store := NewDBStore(db, func(context.Context, uuid.UUID) (bool, error) { return suspended, nil })

	// Destinatário: dono, com os padrões enquanto não salvar preferências.
	target, err := store.Target(ctx, deviceID)
	if err != nil || target == nil || target.Owner == nil {
		t.Fatalf("target: %+v %v", target, err)
	}
	if target.VehicleName != "Moto da Ana" || target.Plate != "ABC1D23" || target.Owner.Email != "ana@cliente.test" ||
		!target.Owner.Active || target.Owner.Settings.Custom || !target.Owner.Settings.Enabled(KindSOS) {
		t.Errorf("target mal lido: %+v / %+v", target, target.Owner)
	}
	if orphan, err := store.Target(ctx, orphanDevice); err != nil || orphan != nil {
		t.Errorf("rastreador sem veículo devolve nil: %+v %v", orphan, err)
	}

	// Preferências: gravar, ler e refletir no destinatário.
	custom, err := NewSettings([]string{KindTowing, KindIgnition}, "23:00", "05:30")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSettings(ctx, ownerID, custom); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadSettings(ctx, ownerID)
	if err != nil || !loaded.Custom || loaded.GuardStart != 23*60 || loaded.GuardEnd != 5*60+30 ||
		!loaded.Enabled(KindTowing) || loaded.Enabled(KindSOS) {
		t.Fatalf("preferências: %+v %v", loaded, err)
	}
	suspended = true
	target, _ = store.Target(ctx, deviceID)
	if !target.Owner.Settings.Enabled(KindIgnition) || target.Owner.Settings.Enabled(KindSOS) || !target.Owner.Suspended {
		t.Errorf("o destinatário usa as preferências salvas e a suspensão: %+v", target.Owner)
	}

	// Intervalo mínimo e contagem dos repetidos.
	const email = "ana@cliente.test"
	insert := func(status string) *Notification {
		t.Helper()
		n := &Notification{Recipient: email, UserID: &ownerID, VehicleID: &vehicleID, DeviceID: &deviceID,
			Kind: KindSOS, OccurredAt: time.Now(), Status: status}
		if err := store.Insert(ctx, n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if _, ok, err := store.LastSent(ctx, email, deviceID, KindSOS); ok || err != nil {
		t.Fatalf("sem histórico: nada enviado ainda (%v %v)", ok, err)
	}
	insert(StatusSuppressed) // repetido antes de qualquer envio também conta
	if n, _ := store.CountSuppressedSinceLastSent(ctx, email, deviceID, KindSOS); n != 1 {
		t.Errorf("repetido sem envio anterior: esperava 1, veio %d", n)
	}
	first := insert(StatusPending)
	if err := store.MarkSent(ctx, first.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	last, ok, err := store.LastSent(ctx, email, deviceID, KindSOS)
	if !ok || err != nil || last.Sub(first.CreatedAt).Abs() > time.Second {
		t.Fatalf("último envio: %v %v %v", last, ok, err)
	}
	if n, _ := store.CountSuppressedSinceLastSent(ctx, email, deviceID, KindSOS); n != 0 {
		t.Errorf("depois de um envio a contagem recomeça: %d", n)
	}
	insert(StatusSuppressed)
	insert(StatusSuppressed)
	if n, _ := store.CountSuppressedSinceLastSent(ctx, email, deviceID, KindSOS); n != 2 {
		t.Errorf("dois repetidos depois do envio: %d", n)
	}
	if n, _ := store.CountSentSince(ctx, email, time.Now().Add(-time.Hour)); n != 1 {
		t.Errorf("teto por hora conta só enviados: %d", n)
	}

	failed := insert(StatusPending)
	if err := store.MarkFailed(ctx, failed.ID, "smtp fora"); err != nil {
		t.Fatal(err)
	}

	// Histórico: só enviados, pendentes e falhas, com o veículo.
	history, err := store.History(ctx, ownerID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || history[0].Status != StatusFailed || history[1].Status != StatusSent ||
		history[0].VehicleName != "Moto da Ana" || history[1].Plate != "ABC1D23" {
		t.Errorf("histórico: %+v", history)
	}

	// Alerta de cerca: guardado com o id dela (intervalo próprio por cerca);
	// o histórico devolve o tipo e o nome.
	var fenceID uuid.UUID
	mustScan(`INSERT INTO geofences (name, latitude, longitude, radius_meters, owner_id)
		VALUES ('Casa', -23.55, -46.63, 200, $1) RETURNING id`, &fenceID, ownerID)
	fenceKind := "GEOFENCE_ENTER:" + fenceID.String()
	if err := store.Insert(ctx, &Notification{Recipient: email, UserID: &ownerID, VehicleID: &vehicleID,
		DeviceID: &deviceID, Kind: fenceKind, OccurredAt: time.Now(), Status: StatusSent}); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := store.LastSent(ctx, email, deviceID, fenceKind); !ok {
		t.Error("intervalo da cerca conta pelo tipo com o id dela")
	}
	if _, ok, _ := store.LastSent(ctx, email, deviceID, "GEOFENCE_ENTER:"+uuid.NewString()); ok {
		t.Error("outra cerca tem intervalo próprio")
	}
	history, err = store.History(ctx, ownerID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if history[0].Kind != "GEOFENCE_ENTER" || history[0].Detail != "Casa" || history[1].Detail != "" {
		t.Errorf("histórico da cerca: tipo e nome, veio %+v / %+v", history[0], history[1])
	}

	// Teste por e-mail e rastreador offline.
	if _, ok, _ := store.LastTest(ctx, ownerID); ok {
		t.Error("nenhum teste ainda")
	}
	if err := store.Insert(ctx, &Notification{Recipient: email, UserID: &ownerID, Kind: KindTest,
		OccurredAt: time.Now(), Status: StatusSent}); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := store.LastTest(ctx, ownerID); !ok {
		t.Error("teste registrado")
	}
	if _, err := db.Exec(ctx, `UPDATE devices SET status = 'ONLINE' WHERE id = $1`, deviceID); err != nil {
		t.Fatal(err)
	}
	if off, _ := store.DeviceOffline(ctx, deviceID); off {
		t.Error("rastreador ONLINE não está sem sinal")
	}
	if _, err := db.Exec(ctx, `UPDATE devices SET status = 'OFFLINE' WHERE id = $1`, deviceID); err != nil {
		t.Fatal(err)
	}
	if off, _ := store.DeviceOffline(ctx, deviceID); !off {
		t.Error("rastreador OFFLINE")
	}

	// Limpeza do histórico antigo.
	if _, err := db.Exec(ctx, `UPDATE alert_notifications SET created_at = NOW() - INTERVAL '100 days' WHERE id = $1`, first.ID); err != nil {
		t.Fatal(err)
	}
	if n, err := store.Prune(ctx, time.Now().Add(-90*24*time.Hour)); err != nil || n != 1 {
		t.Errorf("prune: %d %v", n, err)
	}
}
