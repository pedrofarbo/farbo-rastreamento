package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/devices"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/events"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/geofences"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/protocols"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/protocols/gt06"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/retention"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/tcp"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/telemetry"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/tracking"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/vehicles"
	ws "github.com/pedrofarbo/farbo-rastreamento/backend/internal/websocket"
)

// Cercas do cliente de ponta a ponta: API, Postgres e ingestão real.
// Precisa de FARBO_TEST_DATABASE_URL (ver integrationDB).

type fenceEnv struct {
	*credEnv
	ingestor *tracking.Ingestor
	states   *tracking.StateStore
	conns    map[uuid.UUID]*tcp.DeviceConnection

	mu     sync.Mutex
	events []*events.Event
}

func newFenceEnv(t *testing.T) *fenceEnv {
	t.Helper()
	db := integrationDB(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	metrics := telemetry.NewMetrics()
	cfg := &config.Config{
		HTTP: config.HTTP{RateLimitRPS: 1000, RateLimitBurst: 1000},
		Auth: config.Auth{
			JWTSecret:      []byte("segredo-de-teste-integracao-0123456789abcdef"),
			AccessTokenTTL: time.Hour, RefreshTokenTTL: time.Hour, BcryptCost: bcrypt.MinCost,
		},
		Tracking: config.Tracking{StaleAfter: 5 * time.Minute, OfflineAfter: time.Hour, HistoryRetentionDays: 30},
		Billing:  config.Billing{Timezone: "UTC"},
	}

	env := &fenceEnv{credEnv: &credEnv{t: t, db: db}, conns: map[uuid.UUID]*tcp.DeviceConnection{}}
	hub := ws.NewHub(log, nil)
	registry := protocols.NewRegistry(gt06.New(false))
	authSvc := auth.NewService(auth.NewRepository(db), cfg.Auth, nil, log)
	billingSvc, err := billing.NewService(billing.NewRepository(db), cfg.Billing, log)
	if err != nil {
		t.Fatal(err)
	}
	env.devices = devices.NewService(devices.NewRepository(db), registry)
	env.vehicles = vehicles.NewService(vehicles.NewRepository(db))
	env.owners = vehicles.NewOwnerIndex(db, log)
	env.positions = tracking.NewRepository(db)
	eventSvc := events.NewService(events.NewRepository(db), hub, log)
	eventSvc.SetObserver(func(e *events.Event) {
		env.mu.Lock()
		env.events = append(env.events, e)
		env.mu.Unlock()
	})
	env.states = tracking.NewStateStore(tracking.NewStateRepository(db))
	fences := geofences.NewService(geofences.NewRepository(db))
	if err := fences.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	env.ingestor = tracking.NewIngestor(env.devices, env.vehicles, env.positions, env.states, eventSvc,
		fences, nil, nil, hub, cfg.Tracking, metrics, log)

	server := NewServer(Deps{
		Config: cfg, Log: log, Metrics: metrics, DB: db, Auth: authSvc,
		Devices: env.devices, Vehicles: env.vehicles, Geofences: fences, Events: eventSvc,
		Audit: audit.NewService(audit.NewRepository(db), log), Billing: billingSvc,
		Retention: retention.NewService(db, cfg.Tracking.HistoryRetentionDays, log),
		Owners:    env.owners, Positions: env.positions, States: env.states, Ingestor: env.ingestor,
		Conns: tcp.NewManager(nil), Registry: registry, WS: ws.NewHandler(hub, nil), Hub: hub,
	})
	env.srv = httptest.NewServer(server.Handler())
	t.Cleanup(env.srv.Close)
	return env
}

// at manda uma posição do rastreador, como se viesse pelo TCP, e devolve os
// eventos de cerca que ela gerou.
func (e *fenceEnv) at(dev *devices.Device, lat, lon float64) []*events.Event {
	e.t.Helper()
	conn, ok := e.conns[dev.ID]
	if !ok {
		client, server := net.Pipe()
		e.t.Cleanup(func() { _ = client.Close(); _ = server.Close() })
		conn = &tcp.DeviceConnection{ID: uuid.New(), Conn: server}
		e.conns[dev.ID] = conn
	}
	e.mu.Lock()
	start := len(e.events)
	e.mu.Unlock()
	err := e.ingestor.HandleMessage(context.Background(), conn, protocols.TrackerMessage{
		IMEI: dev.IMEI, Kind: protocols.KindPosition, Protocol: "gt06", Timestamp: time.Now().UTC(),
		Latitude: lat, Longitude: lon, GPSValid: true, HasLocation: true,
	})
	if err != nil {
		e.t.Fatal(err)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	out := []*events.Event{}
	for _, ev := range e.events[start:] {
		if ev.Type == events.GeofenceEnter || ev.Type == events.GeofenceExit {
			out = append(out, ev)
		}
	}
	return out
}

func decodeFence(t *testing.T, body []byte) geofences.Geofence {
	t.Helper()
	var g geofences.Geofence
	if err := json.Unmarshal(body, &g); err != nil {
		t.Fatalf("cerca: %v: %s", err, body)
	}
	return g
}

func TestCustomerGeofencesEndToEnd(t *testing.T) {
	env := newFenceEnv(t)
	ctx := context.Background()

	authSvc := auth.NewService(auth.NewRepository(env.db), config.Auth{BcryptCost: bcrypt.MinCost}, nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	user := func(email, role string) *auth.User {
		u, err := authSvc.CreateUser(ctx, email, email, role, userPassword)
		if err != nil {
			t.Fatal(err)
		}
		return u
	}
	ana, bia := user("ana@cercas.test", auth.RoleCustomer), user("bia@cercas.test", auth.RoleCustomer)
	user("admin@cercas.test", auth.RoleAdmin)
	anaToken, biaToken, admin := env.login("ana@cercas.test"), env.login("bia@cercas.test"), env.login("admin@cercas.test")

	vehicle := func(name, imei string, owner uuid.UUID) (*vehicles.Vehicle, *devices.Device) {
		dev, err := env.devices.Create(ctx, devices.Input{IMEI: imei, Protocol: "gt06"})
		if err != nil {
			t.Fatal(err)
		}
		v, err := env.vehicles.Create(ctx, vehicles.Input{Name: name, DeviceID: &dev.ID, OwnerID: &owner})
		if err != nil {
			t.Fatal(err)
		}
		return v, dev
	}
	carro, devCarro := vehicle("Carro da Ana", "869247061239001", ana.ID)
	moto, devMoto := vehicle("Moto da Ana", "869247061239002", ana.ID)
	carroBia, devBia := vehicle("Carro da Bia", "869247061239003", bia.ID)
	if err := env.owners.Refresh(ctx); err != nil {
		t.Fatal(err)
	}

	const homeLat, homeLon = -23.5505, -46.6333
	const awayLat = -23.5700 // ~2 km ao sul

	// O carro e a moto da Ana estão em casa antes de a cerca existir.
	if len(env.at(devCarro, homeLat, homeLon)) != 0 || len(env.at(devMoto, homeLat, homeLon)) != 0 {
		t.Fatal("sem cerca, nenhum evento")
	}

	// --- Cadastro: só os veículos dela, dentro dos limites ------------------
	input := map[string]any{"name": "Casa", "latitude": homeLat, "longitude": homeLon, "radiusMeters": 200,
		"vehicleIds": []uuid.UUID{carro.ID}}
	casa := decodeFence(t, env.must(anaToken, http.MethodPost, "/api/geofences", input, http.StatusCreated))
	if casa.OwnerID == nil || *casa.OwnerID != ana.ID || !slices.Equal(casa.VehicleIDs, []uuid.UUID{carro.ID}) ||
		!casa.NotifyEnter || !casa.NotifyExit || !casa.Active {
		t.Fatalf("cerca da Ana, com o carro e os dois avisos ligados: %+v", casa)
	}
	for name, body := range map[string]map[string]any{
		"veículo da Bia":    {"name": "X", "latitude": homeLat, "longitude": homeLon, "radiusMeters": 200, "vehicleIds": []uuid.UUID{carroBia.ID}},
		"sem veículo":       {"name": "X", "latitude": homeLat, "longitude": homeLon, "radiusMeters": 200},
		"raio de 30 m":      {"name": "X", "latitude": homeLat, "longitude": homeLon, "radiusMeters": 30, "vehicleIds": []uuid.UUID{carro.ID}},
		"veículo inventado": {"name": "X", "latitude": homeLat, "longitude": homeLon, "radiusMeters": 200, "vehicleIds": []uuid.UUID{uuid.New()}},
	} {
		if status, out := env.do(anaToken, http.MethodPost, "/api/geofences", body); status != http.StatusBadRequest {
			t.Errorf("%s: esperava 400, veio %d: %s", name, status, out)
		}
	}

	// --- Cada um vê e mexe só nas suas ----------------------------------------
	list := func(token string) []geofences.Geofence {
		var out []geofences.Geofence
		if err := json.Unmarshal(env.must(token, http.MethodGet, "/api/geofences", nil, http.StatusOK), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	if got := list(anaToken); len(got) != 1 || got[0].ID != casa.ID {
		t.Fatalf("a Ana vê a cerca dela: %+v", got)
	}
	if got := list(biaToken); len(got) != 0 {
		t.Fatalf("a Bia não vê a cerca da Ana: %+v", got)
	}
	if got := list(admin); len(got) != 0 {
		t.Fatalf("a lista da central só tem as cercas da central: %+v", got)
	}
	path := "/api/geofences/" + casa.ID.String()
	env.must(biaToken, http.MethodPatch, path, input, http.StatusNotFound)
	env.must(biaToken, http.MethodDelete, path, nil, http.StatusNotFound)
	env.must(admin, http.MethodDelete, path, nil, http.StatusNotFound)

	// --- Quem já estava dentro não "entra" ------------------------------------
	if st := env.states.Get(devCarro.ID); st == nil || !slices.Contains(st.InsideFences, casa.ID) {
		t.Fatalf("o carro já estava em casa: marcado como dentro ao criar a cerca, veio %+v", st)
	}
	if got := env.at(devCarro, homeLat, homeLon); len(got) != 0 {
		t.Fatalf("parado em casa depois de criar a cerca: sem evento, veio %d", len(got))
	}

	// --- Saída e entrada de verdade ---------------------------------------------
	got := env.at(devCarro, awayLat, homeLon)
	if len(got) != 1 || got[0].Type != events.GeofenceExit || got[0].Metadata["geofenceName"] != "Casa" ||
		got[0].Metadata["geofenceOwnerId"] != ana.ID.String() || got[0].Metadata["notify"] != true {
		t.Fatalf("saída da cerca da Ana, com aviso: %+v", got)
	}
	if got := env.at(devCarro, homeLat, homeLon); len(got) != 1 || got[0].Type != events.GeofenceEnter {
		t.Fatalf("voltou: entrada, veio %+v", got)
	}
	if got := env.at(devMoto, homeLat+0.0001, homeLon); len(got) != 0 {
		t.Fatalf("a moto não foi escolhida para a cerca: nada, veio %+v", got)
	}
	if got := env.at(devBia, homeLat, homeLon); len(got) != 0 {
		t.Fatalf("o carro da Bia não é vigiado pela cerca da Ana: nada, veio %+v", got)
	}

	// --- Editar: incluir a moto (já em casa) e desligar o aviso de saída -----
	input["vehicleIds"] = []uuid.UUID{carro.ID, moto.ID}
	input["notifyExit"] = false
	input["radiusMeters"] = 300
	casa = decodeFence(t, env.must(anaToken, http.MethodPatch, path, input, http.StatusOK))
	if len(casa.VehicleIDs) != 2 || casa.NotifyExit || !casa.NotifyEnter || casa.RadiusMeters != 300 {
		t.Fatalf("cerca editada: %+v", casa)
	}
	if got := env.at(devMoto, homeLat, homeLon); len(got) != 0 {
		t.Fatalf("a moto entrou na cerca já estando em casa: sem evento, veio %+v", got)
	}
	if got := env.at(devCarro, awayLat, homeLon); len(got) != 1 || got[0].Metadata["notify"] != false {
		t.Fatalf("saída continua sendo evento, mas sem aviso: %+v", got)
	}

	// --- Apagar: nada de saída fantasma -------------------------------------------
	env.at(devCarro, homeLat, homeLon)
	env.must(anaToken, http.MethodDelete, path, nil, http.StatusNoContent)
	if got := env.at(devCarro, awayLat, homeLon); len(got) != 0 {
		t.Fatalf("cerca apagada não gera saída, veio %+v", got)
	}

	// --- Limite por conta (com poucos veículos, o mínimo) -------------------------
	var limit struct {
		Limit int `json:"limit"`
		Used  int `json:"used"`
	}
	_ = json.Unmarshal(env.must(anaToken, http.MethodGet, "/api/geofences/limit", nil, http.StatusOK), &limit)
	if limit.Limit != geofences.MinPerCustomer {
		t.Fatalf("limite da Ana = %+v", limit)
	}
	for i := limit.Used; i < limit.Limit; i++ {
		input["name"] = fmt.Sprintf("Cerca %02d", i)
		env.must(anaToken, http.MethodPost, "/api/geofences", input, http.StatusCreated)
	}
	if status, out := env.do(anaToken, http.MethodPost, "/api/geofences", input); status != http.StatusBadRequest {
		t.Fatalf("passou do limite de %d cercas: esperava 400, veio %d: %s", limit.Limit, status, out)
	}
	// Uma frota de 15 veículos ativos: 30 cercas.
	if _, err := env.db.Exec(context.Background(), `
		INSERT INTO subscriptions (customer_id, plan_name, price_cents, due_day, next_due_date)
		SELECT $1, 'Plano Frota', 4990, 10, '2026-11-10' FROM generate_series(1, 15)`, ana.ID); err != nil {
		t.Fatal(err)
	}
	var active int
	if err := env.db.QueryRow(context.Background(), `SELECT count(*) FROM subscriptions WHERE customer_id = $1 AND status = 'ACTIVE'`,
		ana.ID).Scan(&active); err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(env.must(anaToken, http.MethodGet, "/api/geofences/limit", nil, http.StatusOK), &limit)
	if active < 15 || limit.Limit != 2*active || limit.Used != geofences.MinPerCustomer {
		t.Errorf("limite da frota de %d = %+v", active, limit)
	}
	env.must(anaToken, http.MethodPost, "/api/geofences", input, http.StatusCreated)
	// A Bia continua podendo criar as dela.
	env.must(biaToken, http.MethodPost, "/api/geofences", map[string]any{"name": "Trabalho", "latitude": homeLat,
		"longitude": homeLon, "radiusMeters": 500, "vehicleIds": []uuid.UUID{carroBia.ID}}, http.StatusCreated)
}

// A área desenhada e o horário dos avisos, de ponta a ponta: o polígono
// vigia só a área (o canto vazio do "L" não conta) e, fora do horário, a
// entrada fica no histórico sem aviso. Precisa de FARBO_TEST_DATABASE_URL.
func TestPolygonAndScheduledFences(t *testing.T) {
	env := newFenceEnv(t)
	ctx := context.Background()
	authSvc := auth.NewService(auth.NewRepository(env.db), config.Auth{BcryptCost: bcrypt.MinCost}, nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	ana, err := authSvc.CreateUser(ctx, "ana@area.test", "Ana", auth.RoleCustomer, userPassword)
	if err != nil {
		t.Fatal(err)
	}
	token := env.login("ana@area.test")
	dev, err := env.devices.Create(ctx, devices.Input{IMEI: "869247061239101", Protocol: "gt06"})
	if err != nil {
		t.Fatal(err)
	}
	carro, err := env.vehicles.Create(ctx, vehicles.Input{Name: "Van", DeviceID: &dev.ID, OwnerID: &ana.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := env.owners.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	env.at(dev, -23.5600, -46.6300) // longe, antes da cerca

	// O pátio em "L"; avisa só fora do horário de agora (amanhã o dia todo).
	tomorrow := (int(time.Now().In(time.FixedZone("BRT", -3*60*60)).Weekday()) + 1) % 7
	patio := decodeFence(t, env.must(token, http.MethodPost, "/api/geofences", map[string]any{
		"name": "Pátio", "shape": "polygon", "vehicleIds": []uuid.UUID{carro.ID}, "scheduleDays": []int{tomorrow},
		"polygon": [][2]float64{{-23.5500, -46.6300}, {-23.5500, -46.6260}, {-23.5520, -46.6260}, {-23.5520, -46.6280},
			{-23.5540, -46.6280}, {-23.5540, -46.6300}},
	}, http.StatusCreated))
	if patio.Shape != "polygon" || len(patio.Polygon) != 6 || patio.RadiusMeters < 200 || !slices.Equal(patio.ScheduleDays, []int{tomorrow}) {
		t.Fatalf("pátio = %+v", patio)
	}
	if got := env.at(dev, -23.5530, -46.6270); len(got) != 0 {
		t.Fatalf("o canto vazio do L não é o pátio: %+v", got)
	}
	got := env.at(dev, -23.5530, -46.6290)
	if len(got) != 1 || got[0].Type != events.GeofenceEnter || got[0].Metadata["notify"] != false {
		t.Fatalf("entrou no pátio fora do horário: no histórico, sem aviso: %+v", got)
	}

	// Sem horário: avisa. O app, ao editar só o nome, manda o desenho de volta.
	patio = decodeFence(t, env.must(token, http.MethodPatch, "/api/geofences/"+patio.ID.String(), map[string]any{
		"name": "Pátio da empresa", "shape": "polygon", "polygon": patio.Polygon, "vehicleIds": []uuid.UUID{carro.ID},
		"latitude": patio.Latitude, "longitude": patio.Longitude, "radiusMeters": patio.RadiusMeters,
	}, http.StatusOK))
	if patio.Shape != "polygon" || len(patio.ScheduleDays) != 0 || patio.ScheduleStart != nil {
		t.Fatalf("pátio sem horário = %+v", patio)
	}
	if got := env.at(dev, -23.5600, -46.6300); len(got) != 1 || got[0].Type != events.GeofenceExit || got[0].Metadata["notify"] != true {
		t.Fatalf("saiu do pátio, com aviso: %+v", got)
	}
	// Desenho inválido: recusado.
	env.must(token, http.MethodPost, "/api/geofences", map[string]any{
		"name": "X", "shape": "polygon", "vehicleIds": []uuid.UUID{carro.ID}, "polygon": [][2]float64{{-23.55, -46.63}, {-23.56, -46.63}},
	}, http.StatusBadRequest)
}
