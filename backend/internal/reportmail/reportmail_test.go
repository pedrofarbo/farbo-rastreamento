package reportmail

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
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/reports"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/vehicles"
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
	schema := "test_reportmail_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
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

// fakeMailer guarda os relatórios; failTo faz o envio para esse e-mail
// falhar.
type fakeMailer struct {
	mu     sync.Mutex
	sent   []mail.FleetReport
	failTo string
}

func (m *fakeMailer) FleetReport(_ context.Context, r mail.FleetReport) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r.To == m.failTo {
		return errors.New("servidor de e-mail fora")
	}
	m.sent = append(m.sent, r)
	return nil
}

func (m *fakeMailer) take() []mail.FleetReport {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := m.sent
	m.sent = nil
	return out
}

// O relatório agendado de ponta a ponta: as recusas na hora de agendar; na
// segunda às 8h, o titular recebe a semana da frota toda (com as planilhas)
// e o sócio uma cópia; a supervisora restrita ao Norte, só o Norte; a
// segunda procura não repete; o envio que falha volta para a próxima; quem
// sai da conta deixa de receber.
func TestScheduledFleetReports(t *testing.T) {
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
	owner, other := user("frota@relatorio.test", "Frota Ltda"), user("outra@relatorio.test", "Outra")
	supervisor, viewer := user("su@relatorio.test", "Su Supervisora"), user("vi@relatorio.test", "Vi")
	norte := mustID(`INSERT INTO vehicle_groups (owner_id, name) VALUES ($1, 'Norte') RETURNING id`, owner)
	alheio := mustID(`INSERT INTO vehicle_groups (owner_id, name) VALUES ($1, 'Da outra') RETURNING id`, other)
	if _, err := db.Exec(ctx, `
		INSERT INTO account_members (user_id, account_id, role, restricted) VALUES ($1, $3, 'manager', TRUE), ($2, $3, 'viewer', FALSE);
		`, supervisor, viewer, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO account_member_groups (user_id, group_id) VALUES ($1, $2)`, supervisor, norte); err != nil {
		t.Fatal(err)
	}
	vehicle := func(imei, name, plate string, group *uuid.UUID) (uuid.UUID, uuid.UUID) {
		device := mustID(`INSERT INTO devices (imei) VALUES ($1) RETURNING id`, imei)
		id := mustID(`INSERT INTO vehicles (name, plate, device_id, owner_id, group_id, created_at)
			VALUES ($1, $2, $3, $4, $5, '2026-01-01') RETURNING id`, name, plate, device, owner, group)
		return id, device
	}
	vanNorte, devNorte := vehicle("869247061250001", "Van Norte", "NOR1A11", &norte)
	carro, devCarro := vehicle("869247061250002", "Carro da sede", "SED2B22", nil)
	stats := func(device, vehicle uuid.UUID, day string, meters float64, trips int) {
		if _, err := db.Exec(ctx, `
			INSERT INTO vehicle_daily_stats (device_id, day, vehicle_id, distance_m, moving_s, trips) VALUES ($1, $2, $3, $4, 3600, $5)`,
			device, day, vehicle, meters, trips); err != nil {
			t.Fatal(err)
		}
	}
	stats(devNorte, vanNorte, "2026-10-06", 12000, 2)
	stats(devCarro, carro, "2026-10-07", 5000, 1)
	if _, err := db.Exec(ctx, `
		INSERT INTO vehicle_events (device_id, type, timestamp, latitude, longitude, speed_kmh, metadata)
		VALUES ($1, 'OVERSPEED', '2026-10-06 12:00-03', -23.5, -46.6, 95, '{"limitKmh": 80}')`, devNorte); err != nil {
		t.Fatal(err)
	}

	mailer := &fakeMailer{}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := NewService(db, reports.NewService(db, "America/Sao_Paulo"), vehicles.NewService(vehicles.NewRepository(db)), mailer, nil, log)
	loc, _ := time.LoadLocation("America/Sao_Paulo")
	now := time.Date(2026, 10, 12, 6, 0, 0, 0, loc) // segunda, antes das 7h
	svc.SetClock(func() time.Time { return now })

	ownerP := Person{UserID: owner, AccountID: owner, CanAddRecipients: true}
	supP := Person{UserID: supervisor, AccountID: owner, Restricted: true, Groups: []uuid.UUID{norte}, CanAddRecipients: true}
	viewerP := Person{UserID: viewer, AccountID: owner}
	refuse := func(label string, p Person, in Input) {
		t.Helper()
		var input *InputError
		if _, err := svc.Create(ctx, p, in); !errors.As(err, &input) {
			t.Errorf("%s: %v", label, err)
		}
	}
	refuse("frequência", ownerP, Input{Frequency: "daily"})
	refuse("grupo de outra conta", ownerP, Input{Frequency: Weekly, GroupID: &alheio})
	refuse("restrita sem grupo", supP, Input{Frequency: Weekly})
	refuse("visualizador com e-mails a mais", viewerP, Input{Frequency: Weekly, ExtraRecipients: []string{"x@y.test"}})
	refuse("e-mail inválido", ownerP, Input{Frequency: Weekly, ExtraRecipients: []string{"sem arroba"}})

	if _, err := svc.Create(ctx, ownerP, Input{Frequency: Weekly, ExtraRecipients: []string{" Socio@Frota.test ", "socio@frota.test"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, supP, Input{Frequency: Weekly, GroupID: &norte}); err != nil {
		t.Fatal(err)
	}
	monthly, err := svc.Create(ctx, ownerP, Input{Frequency: Monthly})
	if err != nil {
		t.Fatal(err)
	}
	if list, _ := svc.List(ctx, owner); len(list) != 2 || list[0].ExtraRecipients[0] != "socio@frota.test" || len(list[0].ExtraRecipients) != 1 {
		t.Fatalf("agendados do titular = %+v", list)
	}

	// Antes das 7h de segunda, a semana ainda não sai; o mês (desde o dia
	// 1º) sai — e, com o servidor de e-mail fora, volta para a próxima.
	mailer.failTo = "frota@relatorio.test"
	if n, err := svc.Round(ctx); err != nil || n != 0 {
		t.Fatalf("antes das 7h: %d enviados, %v", n, err)
	}
	mailer.failTo = ""
	if n, _ := svc.Round(ctx); n != 1 {
		t.Fatalf("o mensal não foi de novo: %d", n)
	}
	sent := mailer.take()
	if len(sent) != 1 || sent[0].Kind != "mensal" || sent[0].Period != "setembro de 2026" || sent[0].Active != 0 || sent[0].Vehicles != 2 {
		t.Fatalf("mensal = %+v", sent)
	}

	// Segunda às 8h: a semana de 05 a 11/10.
	now = time.Date(2026, 10, 12, 8, 0, 0, 0, loc)
	if n, err := svc.Round(ctx); err != nil || n != 2 {
		t.Fatalf("semanais: %d, %v", n, err)
	}
	byTo := map[string]mail.FleetReport{}
	for _, r := range mailer.take() {
		byTo[r.To] = r
	}
	frota := byTo["frota@relatorio.test"]
	if frota.Kind != "semanal" || frota.Period != "05/10 a 11/10/2026" || frota.Vehicles != 2 || frota.Active != 2 ||
		frota.Km != "17" || frota.Trips != "3" || frota.Moving != "2:00 h" || frota.Overspeeds != 1 || len(frota.Top) != 2 || frota.Top[0].Name != "Van Norte" ||
		len(frota.Attachments) != 2 {
		t.Fatalf("relatório do titular = %+v", frota)
	}
	if csv := string(frota.Attachments[0].Data); !strings.HasPrefix(frota.Attachments[0].Name, "farbo-resumo-2026-10-05") ||
		!strings.Contains(csv, "Van Norte;NOR1A11;Norte;12,0;") || !strings.Contains(csv, "Carro da sede") {
		t.Errorf("planilha do resumo:\n%s", csv)
	}
	if copy := byTo["socio@frota.test"]; copy.ForwardedFrom != "Frota Ltda" || copy.Km != "17" {
		t.Errorf("cópia do sócio = %+v", copy)
	}
	su := byTo["su@relatorio.test"]
	if su.Group != "Norte" || su.Vehicles != 1 || su.Km != "12" || len(su.Top) != 1 || strings.Contains(string(su.Attachments[0].Data), "Carro da sede") {
		t.Errorf("relatório da supervisora = %+v", su)
	}

	// A mesma procura de novo não repete nada.
	if n, _ := svc.Round(ctx); n != 0 {
		t.Errorf("repetiu: %d", n)
	}

	// A supervisora sai da conta: na semana seguinte, o agendamento dela some.
	if _, err := db.Exec(ctx, `DELETE FROM account_members WHERE user_id = $1`, supervisor); err != nil {
		t.Fatal(err)
	}
	now = now.AddDate(0, 0, 7)
	if n, _ := svc.Round(ctx); n != 1 {
		t.Errorf("semana seguinte: %d enviados", n)
	}
	if list, _ := svc.List(ctx, supervisor); len(list) != 0 {
		t.Errorf("o agendamento de quem saiu ficou: %+v", list)
	}
	mailer.take()

	if err := svc.Delete(ctx, supervisor, monthly.ID); !errors.Is(err, database.ErrNotFound) {
		t.Errorf("apagar o de outra pessoa: %v", err)
	}
	if err := svc.Delete(ctx, owner, monthly.ID); err != nil {
		t.Errorf("apagar: %v", err)
	}
}
