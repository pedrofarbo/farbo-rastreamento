package maintenance

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/mail"
)

// testDB sobe um schema descartável com as migrations. Precisa de um
// Postgres em FARBO_TEST_DATABASE_URL; sem ele, o teste é pulado.
func testDB(t *testing.T) *database.DB {
	t.Helper()
	dsn := os.Getenv("FARBO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("defina FARBO_TEST_DATABASE_URL (Postgres descartável) para rodar o teste com banco")
	}
	ctx := context.Background()
	schema := "test_maintenance_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
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

type fakeMailer struct {
	mu   sync.Mutex
	sent []mail.MaintenanceDigest
	down bool
}

func (m *fakeMailer) Digest(_ context.Context, d mail.MaintenanceDigest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.down {
		return errors.New("servidor de e-mail fora")
	}
	m.sent = append(m.sent, d)
	return nil
}

func (m *fakeMailer) take() map[string][]mail.MaintenanceLine {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string][]mail.MaintenanceLine{}
	for _, d := range m.sent {
		out[d.To] = d.Lines
	}
	m.sent = nil
	return out
}

// O hodômetro e a manutenção de ponta a ponta: a leitura informada mais os
// km do GPS; a manutenção criada em vários veículos (cada um com o hodômetro
// dele); a situação de cada uma; o aviso por pessoa (a supervisora restrita
// ao Norte só recebe o Norte; o Visualizador, nada), sem repetir, de novo
// quando vence e de novo depois se o e-mail falhar; registrar a feita.
func TestMaintenanceEndToEnd(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	mustID := func(sql string, args ...any) uuid.UUID {
		t.Helper()
		var id uuid.UUID
		if err := db.QueryRow(ctx, sql, args...).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	user := func(email, name string) uuid.UUID {
		return mustID(`INSERT INTO users (email, name, role, password_hash) VALUES ($1, $2, 'customer', 'x') RETURNING id`, email, name)
	}
	owner, supervisor, viewer := user("frota@manutencao.test", "Frota Ltda"), user("su@manutencao.test", "Su"), user("vi@manutencao.test", "Vi")
	norte := mustID(`INSERT INTO vehicle_groups (owner_id, name) VALUES ($1, 'Norte') RETURNING id`, owner)
	if _, err := db.Exec(ctx, `
		INSERT INTO account_members (user_id, account_id, role, restricted) VALUES ($1, $3, 'manager', TRUE), ($2, $3, 'viewer', FALSE)`,
		supervisor, viewer, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO account_member_groups (user_id, group_id) VALUES ($1, $2)`, supervisor, norte); err != nil {
		t.Fatal(err)
	}
	vehicle := func(imei, name string, group *uuid.UUID) (uuid.UUID, uuid.UUID) {
		device := mustID(`INSERT INTO devices (imei) VALUES ($1) RETURNING id`, imei)
		return mustID(`INSERT INTO vehicles (name, device_id, owner_id, group_id) VALUES ($1, $2, $3, $4) RETURNING id`,
			name, device, owner, group), device
	}
	vanA, devA := vehicle("869247061260001", "Van Norte", &norte)
	carB, devB := vehicle("869247061260002", "Carro sede", nil)
	drive := func(device, vehicle uuid.UUID, day string, km float64) {
		t.Helper()
		if _, err := db.Exec(ctx, `
			INSERT INTO vehicle_daily_stats (device_id, day, vehicle_id, distance_m) VALUES ($1, $2, $3, $4)
			ON CONFLICT (device_id, day) DO UPDATE SET distance_m = vehicle_daily_stats.distance_m + EXCLUDED.distance_m`,
			device, day, vehicle, km*1000); err != nil {
			t.Fatal(err)
		}
	}
	drive(devA, vanA, "2026-10-01", 120)
	drive(devB, carB, "2026-10-01", 30)

	svc := NewService(db, "America/Sao_Paulo")
	loc, _ := time.LoadLocation("America/Sao_Paulo")
	svc.SetClock(func() time.Time { return time.Date(2026, 10, 12, 10, 0, 0, 0, loc) })

	// O hodômetro: a leitura de agora; dali em diante, mais os km do GPS.
	if _, err := svc.SetOdometer(ctx, vanA, -1); err == nil {
		t.Error("hodômetro negativo aceito")
	}
	if o, err := svc.SetOdometer(ctx, vanA, 50000); err != nil || o.Km == nil || *o.Km != 50000 || o.TrackedKm != 120 {
		t.Fatalf("hodômetro = %+v, %v", o, err)
	}
	drive(devA, vanA, "2026-10-12", 2)
	odometers, _ := svc.Odometers(ctx, []uuid.UUID{vanA, carB})
	if *odometers[vanA].Km != 50002 || odometers[carB].Km != nil || odometers[carB].TrackedKm != 30 {
		t.Fatalf("hodômetros = %+v", odometers)
	}

	// Em vários veículos, cada um com o próprio hodômetro (o sem hodômetro
	// fica sem km; vale a data).
	if n, err := svc.Create(ctx, []uuid.UUID{vanA, carB}, ItemInput{Name: " Troca  de óleo ", EveryKm: ptr(10000), EveryMonths: ptr(6)}); err != nil || n != 2 {
		t.Fatalf("óleo: %d, %v", n, err)
	}
	if _, err := svc.Create(ctx, []uuid.UUID{vanA}, ItemInput{Name: "Pneus", EveryKm: ptr(40000), LastKm: ptr(12000.0)}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, []uuid.UUID{carB}, ItemInput{Name: "Revisão", EveryMonths: ptr(1), LastDate: "2026-09-01"}); err != nil {
		t.Fatal(err)
	}
	var input *InputError
	if _, err := svc.Create(ctx, []uuid.UUID{vanA, carB}, ItemInput{Name: "X", EveryKm: ptr(1000), LastKm: ptr(1.0)}); !errors.As(err, &input) {
		t.Errorf("km da última vez em vários veículos: %v", err)
	}
	if _, err := svc.Create(ctx, []uuid.UUID{vanA}, ItemInput{Name: "Sem intervalo"}); !errors.As(err, &input) {
		t.Errorf("sem intervalo: %v", err)
	}

	items, err := svc.Items(ctx, []uuid.UUID{vanA, carB})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]*Item{}
	order := []string{}
	for _, it := range items {
		got[it.Name+" · "+it.VehicleName] = it
		order = append(order, it.Name+" · "+it.VehicleName)
	}
	if order[0] != "Revisão · Carro sede" || order[1] != "Pneus · Van Norte" {
		t.Errorf("ordem = %v", order)
	}
	oil := got["Troca de óleo · Van Norte"]
	if oil == nil || *oil.LastKm != 50002 || *oil.RemainingKm != 10000 || oil.Status != StatusOK || oil.LastDate != "2026-10-12" {
		t.Errorf("óleo da van = %+v", oil)
	}
	if b := got["Troca de óleo · Carro sede"]; b.LastKm != nil || b.Status != StatusOK || b.RemainingKm != nil {
		t.Errorf("óleo do carro = %+v", b)
	}
	if p := got["Pneus · Van Norte"]; p.Status != StatusSoon || *p.RemainingKm != 1998 {
		t.Errorf("pneus = %+v", p)
	}

	// O aviso: a titular recebe as duas; a supervisora, só a do Norte.
	mailer := &fakeMailer{}
	notifier := NewNotifier(svc, mailer, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if n, err := notifier.Round(ctx); err != nil || n != 2 {
		t.Fatalf("aviso: %d e-mails, %v", n, err)
	}
	sent := mailer.take()
	if len(sent["frota@manutencao.test"]) != 2 || len(sent["su@manutencao.test"]) != 1 || sent["su@manutencao.test"][0].Item != "Pneus" ||
		sent["vi@manutencao.test"] != nil || !sent["frota@manutencao.test"][0].Due || sent["frota@manutencao.test"][0].Detail != "venceu há 11 dias" {
		t.Fatalf("avisos = %+v", sent)
	}
	if n, _ := notifier.Round(ctx); n != 0 {
		t.Errorf("repetiu o aviso: %d", n)
	}

	// Os pneus vencem: novo aviso (de vencida); com o e-mail fora, fica para
	// a próxima procura.
	drive(devA, vanA, "2026-10-12", 3000)
	mailer.down = true
	if n, _ := notifier.Round(ctx); n != 0 {
		t.Errorf("com o e-mail fora: %d", n)
	}
	mailer.down = false
	if n, _ := notifier.Round(ctx); n != 2 {
		t.Errorf("depois da volta do e-mail: %d", n)
	}
	sent = mailer.take()
	if lines := sent["su@manutencao.test"]; len(lines) != 1 || !lines[0].Due || lines[0].Detail != "passou 1.002 km" {
		t.Errorf("aviso de vencida = %+v", sent)
	}

	// Registrar a feita: o histórico e a contagem recomeçam.
	pneus := got["Pneus · Van Norte"].ID
	if n, err := svc.Done(ctx, DoneInput{ItemIDs: []uuid.UUID{pneus}, Notes: "4 pneus novos"}, supervisor); err != nil || n != 1 {
		t.Fatalf("registrar: %d, %v", n, err)
	}
	items, _ = svc.Items(ctx, []uuid.UUID{vanA})
	for _, it := range items {
		if it.Name == "Pneus" && (it.Status != StatusOK || *it.LastKm != 53002 || it.notified != nil) {
			t.Errorf("pneus depois de feito = %+v", it)
		}
	}
	records, _ := svc.Records(ctx, vanA, 10)
	if len(records) != 1 || records[0].Name != "Pneus" || *records[0].Km != 53002 || records[0].Notes != "4 pneus novos" ||
		records[0].DoneDate != "2026-10-12" {
		t.Errorf("histórico = %+v", records)
	}
	if _, err := svc.Done(ctx, DoneInput{ItemIDs: []uuid.UUID{pneus}, Date: "2026-10-13"}, owner); !errors.As(err, &input) {
		t.Errorf("data no futuro: %v", err)
	}
	if err := svc.Delete(ctx, pneus); err != nil {
		t.Fatal(err)
	}
	if records, _ := svc.Records(ctx, vanA, 10); len(records) != 1 || records[0].ItemID != nil {
		t.Errorf("o histórico não ficou ao apagar: %+v", records)
	}
}

// O horímetro: sem a leitura, valem as horas medidas (andando ou parado com
// a ignição ligada); na primeira leitura, as horas guardadas passam para a
// escala do horímetro; nas seguintes, ficam; registrar a feita guarda as
// horas.
func TestEngineHours(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	var owner, device, truck uuid.UUID
	if err := db.QueryRow(ctx, `INSERT INTO users (email, name, role, password_hash) VALUES ('h@manutencao.test', 'H', 'customer', 'x') RETURNING id`).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `INSERT INTO devices (imei) VALUES ('869247061260009') RETURNING id`).Scan(&device); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `INSERT INTO vehicles (name, device_id, owner_id) VALUES ('Caminhão', $1, $2) RETURNING id`, device, owner).Scan(&truck); err != nil {
		t.Fatal(err)
	}
	run := func(day string, moving, idle float64) {
		t.Helper()
		if _, err := db.Exec(ctx, `
			INSERT INTO vehicle_daily_stats (device_id, day, vehicle_id, moving_s, idle_s) VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (device_id, day) DO UPDATE SET moving_s = vehicle_daily_stats.moving_s + EXCLUDED.moving_s,
				idle_s = vehicle_daily_stats.idle_s + EXCLUDED.idle_s`,
			device, day, truck, int(moving*3600), int(idle*3600)); err != nil {
			t.Fatal(err)
		}
	}
	svc := NewService(db, "America/Sao_Paulo")
	loc, _ := time.LoadLocation("America/Sao_Paulo")
	svc.SetClock(func() time.Time { return time.Date(2026, 10, 12, 10, 0, 0, 0, loc) })
	item := func(name string) *Item {
		t.Helper()
		items, err := svc.Items(ctx, []uuid.UUID{truck})
		if err != nil {
			t.Fatal(err)
		}
		for _, it := range items {
			if it.Name == name {
				return it
			}
		}
		t.Fatalf("%s não encontrada", name)
		return nil
	}

	// Sem a leitura: as horas medidas.
	run("2026-10-01", 80, 20)
	if o, _ := svc.Odometers(ctx, []uuid.UUID{truck}); o[truck].Hours != 100 || o[truck].TrackedHours != 100 || o[truck].HoursSetAt != nil {
		t.Fatalf("horímetro sem leitura = %+v", o[truck])
	}
	if _, err := svc.Create(ctx, []uuid.UUID{truck}, ItemInput{Name: "Filtro de ar", EveryHours: ptr(250)}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, []uuid.UUID{truck}, ItemInput{Name: "Lubrificação", EveryHours: ptr(50)}); err != nil {
		t.Fatal(err)
	}
	if f := item("Filtro de ar"); *f.LastHours != 100 || *f.RemainingHours != 250 || f.Status != StatusOK || f.RemainingKm != nil {
		t.Errorf("filtro = %+v", f)
	}
	if _, err := svc.Done(ctx, DoneInput{ItemIDs: []uuid.UUID{item("Lubrificação").ID}}, owner); err != nil {
		t.Fatal(err)
	}

	// A primeira leitura: o que estava guardado passa para a escala dela.
	var input *InputError
	if _, err := svc.SetEngineHours(ctx, truck, -1); !errors.As(err, &input) {
		t.Errorf("horímetro negativo: %v", err)
	}
	if o, err := svc.SetEngineHours(ctx, truck, 5000); err != nil || o.Hours != 5000 || o.TrackedHours != 100 || o.HoursSetAt == nil {
		t.Fatalf("leitura = %+v, %v", o, err)
	}
	if f := item("Filtro de ar"); *f.LastHours != 5000 || *f.RemainingHours != 250 {
		t.Errorf("filtro depois da leitura = %+v", f)
	}
	if records, _ := svc.Records(ctx, truck, 10); len(records) != 1 || *records[0].Hours != 5000 {
		t.Errorf("histórico depois da leitura = %+v", records)
	}

	// Daí em diante, mais as horas medidas.
	run("2026-10-12", 200, 30)
	if f := item("Filtro de ar"); f.Status != StatusSoon || Describe(f) != "faltam 20 h de motor" {
		t.Errorf("filtro perto = %s %q", f.Status, Describe(f))
	}
	// Uma nova leitura corrige o horímetro, não o que já foi guardado.
	if _, err := svc.SetEngineHours(ctx, truck, 5300); err != nil {
		t.Fatal(err)
	}
	if f := item("Filtro de ar"); *f.LastHours != 5000 || f.Status != StatusDue || Describe(f) != "passou 50 h de motor" {
		t.Errorf("filtro vencido = %+v %q", f, Describe(f))
	}

	// Registrar a feita com as horas informadas (só numa manutenção).
	both := []uuid.UUID{item("Filtro de ar").ID, item("Lubrificação").ID}
	if _, err := svc.Done(ctx, DoneInput{ItemIDs: both, Hours: ptr(5290.0)}, owner); !errors.As(err, &input) {
		t.Errorf("horas em várias: %v", err)
	}
	if _, err := svc.Done(ctx, DoneInput{ItemIDs: both[:1], Hours: ptr(5290.0)}, owner); err != nil {
		t.Fatal(err)
	}
	if f := item("Filtro de ar"); *f.LastHours != 5290 || *f.RemainingHours != 240 || f.Status != StatusOK {
		t.Errorf("filtro feito = %+v", f)
	}
	if records, _ := svc.Records(ctx, truck, 10); len(records) != 2 || *records[0].Hours != 5290 {
		t.Errorf("histórico = %+v", records)
	}
	// Editar troca o intervalo; as horas da última vez ficam.
	f := item("Filtro de ar")
	if err := svc.Update(ctx, f.ID, ItemInput{Name: f.Name, EveryHours: ptr(500), LastDate: f.LastDate}); err != nil {
		t.Fatal(err)
	}
	if f := item("Filtro de ar"); *f.LastHours != 5290 || *f.RemainingHours != 490 {
		t.Errorf("filtro editado = %+v", f)
	}
}
