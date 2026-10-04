package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/commands"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/devices"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/events"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/leakcheck"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/protocols"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/protocols/gt06"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/retention"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/shares"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/stepup"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/tcp"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/telemetry"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/tracking"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/vehicles"
	ws "github.com/pedrofarbo/farbo-rastreamento/backend/internal/websocket"
	"github.com/pedrofarbo/farbo-rastreamento/backend/migrations"
)

// Teste de ponta a ponta da issue #1: servidor real, Postgres e WebSocket.
//
// Dois rastreadores GT06 com credenciais sintéticas diferentes recebem
// comandos que levam a senha (padrão, override, texto livre, eco na
// resposta). Depois cada perfil lê todas as rotas de rastreador, veículo e
// histórico, e cada mensagem que chegou pelo WebSocket é varrida: a senha
// não pode aparecer em forma nenhuma (texto, hex, \xNN, base64, JSON
// escapado — ver leakcheck). O aparelho, por outro lado, precisa receber o
// comando real.
//
// Precisa de um Postgres descartável em FARBO_TEST_DATABASE_URL (ex.:
// postgres://usuario:senha@127.0.0.1:5432/banco?sslmode=disable). O teste
// cria um schema próprio e o apaga no fim; sem a variável, é pulado.

const (
	cmdSecretA = "Zq7Kx2Wm9Rt4"
	apnSecretA = `Ap"<n>&\é'9+/=` // não-ASCII: fica só no cadastro
	apnUserA   = "usuario-apn-7Hq"
	cmdSecretB = "Hv3Pc8Ld5Nw1"
	apnSecretB = `Qb"<k>&\'+/=7x` // ASCII: vai dentro de um comando livre
	// oldSecret é uma senha já trocada, presa no histórico legado.
	oldSecret = "Vel8Ha5Ant1ga"

	userPassword = "senha-de-teste-123"
)

var credentialSecrets = []string{cmdSecretA, apnSecretA, cmdSecretB, apnSecretB}

func integrationDB(t *testing.T) *database.DB {
	t.Helper()
	dsn := os.Getenv("FARBO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("defina FARBO_TEST_DATABASE_URL (Postgres descartável) para rodar o teste com banco")
	}
	ctx := context.Background()
	schema := "test_issue1_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]

	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("conectando no Postgres de teste: %v", err)
	}
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	_ = admin.Close(ctx)

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	cfg.AfterConnect = func(_ context.Context, conn *pgx.Conn) error {
		m := conn.TypeMap()
		m.RegisterDefaultPgType(uuid.UUID{}, "uuid")
		m.RegisterDefaultPgType(&uuid.UUID{}, "uuid")
		m.RegisterDefaultPgType([]uuid.UUID{}, "_uuid")
		return nil
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		conn, err := pgx.Connect(context.Background(), dsn)
		if err != nil {
			t.Logf("não foi possível apagar o schema %s: %v", schema, err)
			return
		}
		defer conn.Close(context.Background())
		if _, err := conn.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Logf("não foi possível apagar o schema %s: %v", schema, err)
		}
	})

	db := &database.DB{Pool: pool}
	if err := db.Migrate(ctx, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	return db
}

// captureSender faz o papel do tcp.Manager: guarda o que "iria para o
// socket" dos aparelhos conectados.
type captureSender struct {
	mu        sync.Mutex
	connected map[string]bool
	frames    map[string][][]byte
}

func (c *captureSender) Send(imei string, payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.connected[imei] {
		return commands.ErrNotConnected
	}
	c.frames[imei] = append(c.frames[imei], append([]byte(nil), payload...))
	return nil
}

func (c *captureSender) texts(t *testing.T, imei string) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := []string{}
	for _, frame := range c.frames[imei] {
		_, text, err := gt06.DecodeServerCommand(frame)
		if err != nil {
			t.Fatalf("pacote inválido: %v", err)
		}
		out = append(out, text)
	}
	return out
}

type stoppedTelemetry struct{}

func (stoppedTelemetry) Snapshot(context.Context, uuid.UUID) (commands.Snapshot, error) {
	return commands.Snapshot{HasPosition: true, Timestamp: time.Now()}, nil
}

type credEnv struct {
	t         *testing.T
	db        *database.DB
	srv       *httptest.Server
	devices   *devices.Service
	vehicles  *vehicles.Service
	commands  *commands.Service
	owners    *vehicles.OwnerIndex
	positions *tracking.Repository
	sender    *captureSender
	stepUp    *stepup.Service

	fwdMu     sync.Mutex
	forwarded [][]byte // o que iria para o Redis (outras instâncias)
}

func newCredEnv(t *testing.T) *credEnv {
	t.Helper()
	return newCredEnvWith(t, credEnvOptions{})
}

type credEnvOptions struct {
	// snapshotsFor escolhe a telemetria da regra do corte; nil é a de um
	// veículo sempre parado e com posição recente.
	snapshotsFor func(*tracking.Repository) commands.TelemetryProvider
	// stepUp liga a confirmação extra (biometria ou senha) do cliente.
	stepUp bool
	// shares liga os acessos de terceiros, com os avisos neste Notifier.
	shares shares.Notifier
}

func newCredEnvWith(t *testing.T, opts credEnvOptions) *credEnv {
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
		Commands: config.Commands{
			AckTimeout: time.Minute, EngineCutMaxSpeedKmh: 5,
			EngineCutMaxPositionAge: 10 * time.Minute, SweepInterval: time.Second,
		},
		Tracking: config.Tracking{StaleAfter: 5 * time.Minute, OfflineAfter: time.Hour, HistoryRetentionDays: 30},
		Billing:  config.Billing{Timezone: "UTC"},
	}

	env := &credEnv{t: t, db: db, sender: &captureSender{
		connected: map[string]bool{}, frames: map[string][][]byte{},
	}}

	hub := ws.NewHub(log, nil)
	hub.SetForwarder(func(msg ws.Message) {
		body, err := json.Marshal(msg)
		if err != nil {
			t.Errorf("mensagem do WebSocket não serializa: %v", err)
		}
		env.fwdMu.Lock()
		env.forwarded = append(env.forwarded, body)
		env.fwdMu.Unlock()
	})

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
	auditSvc := audit.NewService(audit.NewRepository(db), log)
	eventSvc := events.NewService(events.NewRepository(db), hub, log)
	states := tracking.NewStateStore(tracking.NewStateRepository(db))
	var snapshots commands.TelemetryProvider = stoppedTelemetry{}
	if opts.snapshotsFor != nil {
		snapshots = opts.snapshotsFor(env.positions)
	}
	env.commands = commands.NewService(commands.NewRepository(db), registry, env.sender,
		snapshots, eventSvc, auditSvc, hub, cfg.Commands, metrics, log)
	ingestor := tracking.NewIngestor(env.devices, env.vehicles, env.positions, states, eventSvc,
		nil, env.commands, nil, hub, cfg.Tracking, metrics, log)

	var stepUpSvc *stepup.Service
	if opts.stepUp {
		stepUpSvc = stepup.NewService(db, config.StepUp{RPID: "localhost", Origins: []string{"http://localhost"}}, authSvc)
	}
	env.stepUp = stepUpSvc
	var sharesSvc *shares.Service
	if opts.shares != nil {
		sharesSvc = shares.NewService(db, authSvc, opts.shares, log)
		sharesSvc.SetSync()
	}
	server := NewServer(Deps{
		StepUp: stepUpSvc, Shares: sharesSvc,
		Config: cfg, Log: log, Metrics: metrics, DB: db, Auth: authSvc,
		Devices: env.devices, Vehicles: env.vehicles, Events: eventSvc, Commands: env.commands,
		Audit: auditSvc, Billing: billingSvc,
		Retention: retention.NewService(db, cfg.Tracking.HistoryRetentionDays, log),
		Owners:    env.owners, Positions: env.positions, States: states, Ingestor: ingestor,
		Conns: tcp.NewManager(nil), Registry: registry,
		WS: ws.NewHandler(hub, nil), Hub: hub,
	})
	env.srv = httptest.NewServer(server.Handler())
	t.Cleanup(env.srv.Close)
	return env
}

func (e *credEnv) do(token, method, path string, body any) (int, []byte) {
	e.t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			e.t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, e.srv.URL+path, reader)
	if err != nil {
		e.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out
}

// must faz a chamada e exige o status.
func (e *credEnv) must(token, method, path string, body any, want int) []byte {
	e.t.Helper()
	status, out := e.do(token, method, path, body)
	if status != want {
		e.t.Fatalf("%s %s: esperava %d, veio %d: %s", method, path, want, status, out)
	}
	return out
}

func (e *credEnv) login(email string) string {
	e.t.Helper()
	out := e.must("", http.MethodPost, "/api/auth/login",
		map[string]string{"email": email, "password": userPassword}, http.StatusOK)
	var tokens struct {
		AccessToken string `json:"accessToken"`
	}
	if err := json.Unmarshal(out, &tokens); err != nil || tokens.AccessToken == "" {
		e.t.Fatalf("login de %s: %s", email, out)
	}
	return tokens.AccessToken
}

// assertClean varre a resposta atrás das credenciais (e de extras que o
// perfil não pode ver, como o usuário APN).
func assertClean(t *testing.T, label string, body []byte, extra ...string) {
	t.Helper()
	secrets := append(append([]string{}, credentialSecrets...), extra...)
	if found := leakcheck.Find(body, secrets...); len(found) > 0 {
		t.Errorf("%s vazou: %v\n%s", label, found, body)
	}
}

// wsClient guarda tudo o que chega pelo WebSocket.
type wsClient struct {
	mu   sync.Mutex
	msgs [][]byte
}

func (e *credEnv) connectWS(token string) *wsClient {
	e.t.Helper()
	url := "ws" + strings.TrimPrefix(e.srv.URL, "http") + "/ws?token=" + token
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		e.t.Fatalf("WebSocket: %v", err)
	}
	e.t.Cleanup(func() { _ = conn.Close() })
	client := &wsClient{}
	go func() {
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			client.mu.Lock()
			client.msgs = append(client.msgs, msg)
			client.mu.Unlock()
		}
	}()
	return client
}

type wsEnvelope struct {
	Type      string          `json:"type"`
	VehicleID *uuid.UUID      `json:"vehicleId"`
	Data      json.RawMessage `json:"data"`
}

// commandEvents devolve as mensagens command.* recebidas até agora.
func (c *wsClient) commandEvents() []wsEnvelope {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := []wsEnvelope{}
	for _, raw := range c.msgs {
		var env wsEnvelope
		if json.Unmarshal(raw, &env) == nil && strings.HasPrefix(env.Type, "command.") {
			out = append(out, env)
		}
	}
	return out
}

func (c *wsClient) waitCommandEvents(t *testing.T, n int) []wsEnvelope {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		events := c.commandEvents()
		if len(events) >= n || time.Now().After(deadline) {
			if len(events) < n {
				t.Fatalf("esperava %d eventos de comando no WebSocket, chegaram %d", n, len(events))
			}
			return events
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (c *wsClient) all() [][]byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([][]byte(nil), c.msgs...)
}

func TestCredentialsNeverLeakEndToEnd(t *testing.T) {
	env := newCredEnv(t)
	ctx := context.Background()

	// --- Usuários de cada perfil -----------------------------------------
	authSvc := auth.NewService(auth.NewRepository(env.db), config.Auth{BcryptCost: bcrypt.MinCost}, nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	users := map[string]*auth.User{}
	for _, role := range []string{auth.RoleAdmin, auth.RoleOperator, auth.RoleViewer, auth.RoleCustomer} {
		user, err := authSvc.CreateUser(ctx, role+"@issue1.test", role, role, userPassword)
		if err != nil {
			t.Fatal(err)
		}
		users[role] = user
	}
	tokens := map[string]string{}
	for role, user := range users {
		tokens[role] = env.login(user.Email)
	}
	admin, operator, customer := tokens[auth.RoleAdmin], tokens[auth.RoleOperator], tokens[auth.RoleCustomer]

	// --- Cadastro pelo admin: a resposta já não traz senha ---------------
	port, interval := 5023, 30
	var devA, devB devices.View
	out := env.must(admin, http.MethodPost, "/api/devices", map[string]any{
		"imei": "869247061230011", "model": "J16", "protocol": "gt06",
		"apn": "zap.vivo.com.br", "apnUser": apnUserA, "apnPassword": apnSecretA,
		"serverHost": "rastreio.exemplo.com", "serverPort": port, "reportIntervalSeconds": interval,
		"commandPassword":  cmdSecretA,
		"commandOverrides": map[string]string{"ENGINE_RESUME": "HFYD," + cmdSecretA + "#"},
	}, http.StatusCreated)
	assertClean(t, "POST /devices (A)", out)
	if err := json.Unmarshal(out, &devA); err != nil || !devA.CommandPasswordSet || !devA.APNPasswordSet {
		t.Fatalf("o admin deveria ver os indicadores de senha definida: %s", out)
	}
	out = env.must(admin, http.MethodPost, "/api/devices", map[string]any{
		"imei": "869247061230022", "protocol": "gt06", "apnPassword": apnSecretB,
		"commandPassword":  cmdSecretB,
		"commandOverrides": map[string]string{"REBOOT": "RESET," + cmdSecretB + "#"},
	}, http.StatusCreated)
	assertClean(t, "POST /devices (B)", out)
	if err := json.Unmarshal(out, &devB); err != nil {
		t.Fatal(err)
	}

	// Veículo da central com o A; veículo do cliente com o B (o B fica
	// desconectado: os comandos dele terminam em command.failed).
	env.sender.connected["869247061230011"] = true
	var vA vehicles.Vehicle
	out = env.must(admin, http.MethodPost, "/api/vehicles",
		map[string]any{"name": "Frota A", "deviceId": devA.ID}, http.StatusCreated)
	if err := json.Unmarshal(out, &vA); err != nil {
		t.Fatal(err)
	}
	vB, err := env.vehicles.Create(ctx, vehicles.Input{Name: "Carro do cliente", DeviceID: &devB.ID,
		OwnerID: &users[auth.RoleCustomer].ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := env.owners.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	// Telemetria do A, para conferir que o painel continua com ela.
	if err := env.positions.Insert(ctx, &tracking.Position{DeviceID: devA.ID, GPSTimestamp: time.Now().UTC(),
		Latitude: -23.5505, Longitude: -46.6333, SpeedKmh: 12, Protocol: "gt06"}); err != nil {
		t.Fatal(err)
	}

	// --- WebSocket de cada perfil ------------------------------------------
	clients := map[string]*wsClient{}
	for role, token := range tokens {
		clients[role] = env.connectWS(token)
	}
	time.Sleep(100 * time.Millisecond) // o hub registra o cliente depois do upgrade

	// --- Comandos que levam a senha ----------------------------------------
	vehicleA, vehicleB := "/api/vehicles/"+vA.ID.String(), "/api/vehicles/"+vB.ID.String()
	decode := func(body []byte) commands.Command {
		var cmd commands.Command
		if err := json.Unmarshal(body, &cmd); err != nil {
			t.Fatalf("comando: %v: %s", err, body)
		}
		return cmd
	}
	status := decode(env.must(operator, http.MethodPost, vehicleA+"/commands/request-status", nil, http.StatusAccepted))
	resume := decode(env.must(operator, http.MethodPost, vehicleA+"/commands/engine-resume", nil, http.StatusAccepted))
	env.must(admin, http.MethodPost, vehicleA+"/commands",
		map[string]any{"command": "SET_INTERVAL", "params": map[string]string{"seconds": "30"}}, http.StatusAccepted)
	env.must(admin, http.MethodPost, vehicleB+"/commands",
		map[string]any{"command": "CUSTOM", "raw": "APN,zap.vivo.com.br,user," + apnSecretB + "#"}, http.StatusAccepted)
	env.must(customer, http.MethodPost, vehicleB+"/commands/engine-cut", nil, http.StatusAccepted)
	env.must(customer, http.MethodPost, vehicleB+"/commands/request-position", nil, http.StatusAccepted)
	env.must(admin, http.MethodPost, vehicleB+"/commands", map[string]any{"command": "REBOOT"}, http.StatusAccepted)

	// Respostas do aparelho que ecoam a senha (uma em caixa alta).
	fullA, err := env.devices.Get(ctx, devA.ID)
	if err != nil {
		t.Fatal(err)
	}
	env.commands.HandleAck(ctx, fullA, &vA.ID, status.CorrelationKey,
		"STATUS,"+strings.ToUpper(cmdSecretA)+"#: Battery:83%", true)
	env.commands.HandleAck(ctx, fullA, &vA.ID, resume.CorrelationKey,
		"HFYD,"+cmdSecretA+"#: Fail! Password err", false)

	// O aparelho recebeu os comandos reais, com a senha.
	sent := env.sender.texts(t, "869247061230011")
	want := []string{"STATUS," + cmdSecretA + "#", "HFYD," + cmdSecretA + "#", "TIMER," + cmdSecretA + ",30#"}
	if strings.Join(sent, "|") != strings.Join(want, "|") {
		t.Fatalf("o aparelho recebeu %q, esperava %q", sent, want)
	}

	// --- WebSocket: nada de senha para ninguém -----------------------------
	adminEvents := clients[auth.RoleAdmin].waitCommandEvents(t, 9)
	clients[auth.RoleOperator].waitCommandEvents(t, 9)
	clients[auth.RoleViewer].waitCommandEvents(t, 9)
	customerEvents := clients[auth.RoleCustomer].waitCommandEvents(t, 4)
	time.Sleep(100 * time.Millisecond) // eventos atrasados também entram na varredura

	for role, client := range clients {
		for i, msg := range client.all() {
			assertClean(t, fmt.Sprintf("WebSocket %s #%d", role, i), msg)
		}
	}
	env.fwdMu.Lock()
	for i, msg := range env.forwarded {
		assertClean(t, fmt.Sprintf("mensagem para o Redis #%d", i), msg)
	}
	env.fwdMu.Unlock()

	redactedSeen := 0
	for _, ev := range adminEvents {
		if bytes.Contains(ev.Data, []byte(devices.Redacted)) {
			redactedSeen++
		}
	}
	if redactedSeen == 0 {
		t.Error("nenhum evento trouxe payload redigido: os comandos com senha não passaram pelo WebSocket?")
	}
	for _, ev := range customerEvents {
		if ev.VehicleID == nil || *ev.VehicleID != vB.ID {
			t.Errorf("o cliente recebeu evento de outro veículo: %+v", ev)
		}
	}

	// --- REST: cada rota de leitura, para cada perfil -----------------------
	deviceA, deviceB := "/api/devices/"+devA.ID.String(), "/api/devices/"+devB.ID.String()
	staffRoutes := []string{
		"/api/devices", deviceA, deviceB, deviceA + "/status", deviceB + "/status",
		deviceA + "/commands", deviceB + "/commands",
		"/api/vehicles", vehicleA, vehicleB, vehicleA + "/commands", vehicleB + "/commands",
		"/api/events",
	}
	for _, role := range []string{auth.RoleAdmin, auth.RoleOperator, auth.RoleViewer} {
		// O usuário APN é credencial: só o admin vê.
		var hidden []string
		if role != auth.RoleAdmin {
			hidden = []string{apnUserA}
		}
		for _, path := range staffRoutes {
			assertClean(t, role+" GET "+path, env.must(tokens[role], http.MethodGet, path, nil, http.StatusOK), hidden...)
		}

		provisioning := http.StatusForbidden
		auditLogs := http.StatusForbidden
		if role == auth.RoleAdmin {
			provisioning, auditLogs = http.StatusOK, http.StatusOK
		}
		out := env.must(tokens[role], http.MethodGet, deviceA+"/provisioning", nil, provisioning)
		assertClean(t, role+" GET provisioning", out, hidden...)
		out = env.must(tokens[role], http.MethodGet, "/api/diagnostics/audit-logs?limit=500", nil, auditLogs)
		assertClean(t, role+" GET audit-logs", out)
	}

	// O histórico mostra que houve comando com senha — redigido.
	out = env.must(tokens[auth.RoleViewer], http.MethodGet, vehicleA+"/commands", nil, http.StatusOK)
	for _, text := range []string{"STATUS,***#", "HFYD,***#", "TIMER,***,30#", "STATUS,***#: Battery:83%"} {
		if !bytes.Contains(out, []byte(text)) {
			t.Errorf("histórico do veículo A sem %q: %s", text, out)
		}
	}
	out = env.must(tokens[auth.RoleOperator], http.MethodGet, deviceB+"/commands", nil, http.StatusOK)
	for _, text := range []string{"APN,zap.vivo.com.br,user,***#", "DYD,***#", "RESET,***#"} {
		if !bytes.Contains(out, []byte(text)) {
			t.Errorf("histórico do aparelho B sem %q: %s", text, out)
		}
	}

	// O painel continua com a telemetria.
	var views []struct {
		ID           uuid.UUID          `json:"id"`
		Device       *devices.View      `json:"device"`
		LastPosition *tracking.Position `json:"lastPosition"`
	}
	if err := json.Unmarshal(env.must(tokens[auth.RoleViewer], http.MethodGet, "/api/vehicles", nil, http.StatusOK), &views); err != nil {
		t.Fatal(err)
	}
	for _, v := range views {
		if v.ID == vA.ID && (v.LastPosition == nil || v.LastPosition.Latitude != -23.5505 ||
			v.Device == nil || v.Device.IMEI != "869247061230011") {
			t.Errorf("o painel perdeu a telemetria do veículo A: %+v", v)
		}
	}

	// Provisionamento do admin: *** no lugar da senha, com o aviso.
	var prov struct {
		Device   devices.View                  `json:"device"`
		Commands []devices.ProvisioningCommand `json:"commands"`
	}
	if err := json.Unmarshal(env.must(admin, http.MethodGet, deviceA+"/provisioning", nil, http.StatusOK), &prov); err != nil {
		t.Fatal(err)
	}
	foundTimer := false
	for _, cmd := range prov.Commands {
		if cmd.Type == protocols.CommandSetInterval {
			foundTimer = cmd.Redacted && cmd.Text != "" && strings.Contains(cmd.Text, "TIMER,***,30#")
		}
	}
	if !foundTimer || !prov.Device.CommandPasswordSet || prov.Device.APNUser != apnUserA {
		t.Errorf("provisionamento do admin incompleto: %+v", prov)
	}

	// Cliente: só o próprio veículo, e sem rota de rastreador.
	for _, path := range []string{"/api/vehicles", vehicleB, vehicleB + "/commands"} {
		assertClean(t, "cliente GET "+path, env.must(customer, http.MethodGet, path, nil, http.StatusOK),
			apnUserA, "869247061230011")
	}
	for _, path := range []string{vehicleA, vehicleA + "/commands"} {
		env.must(customer, http.MethodGet, path, nil, http.StatusNotFound)
	}
	for _, path := range []string{"/api/devices", deviceB, deviceB + "/commands", deviceB + "/provisioning"} {
		env.must(customer, http.MethodGet, path, nil, http.StatusForbidden)
	}

	// --- Escritas administrativas continuam restritas ----------------------
	for _, role := range []string{auth.RoleOperator, auth.RoleViewer, auth.RoleCustomer} {
		env.must(tokens[role], http.MethodPost, "/api/devices", map[string]any{"imei": "869247061230033"}, http.StatusForbidden)
		env.must(tokens[role], http.MethodPatch, deviceA, map[string]any{"imei": "869247061230011", "commandPassword": "Troca1"}, http.StatusForbidden)
		env.must(tokens[role], http.MethodDelete, deviceA, nil, http.StatusForbidden)
	}
	if dev, _ := env.devices.Get(ctx, devA.ID); dev.CommandPassword != cmdSecretA {
		t.Fatal("perfil sem permissão alterou a senha do aparelho")
	}

	// --- Edição pelo admin sem ler a senha ---------------------------------
	// A tela manda o cadastro sem as senhas e sem os overrides: tudo fica.
	out = env.must(admin, http.MethodPatch, deviceA, map[string]any{
		"imei": "869247061230011", "model": "J16 v2", "protocol": "gt06",
		"apn": "zap.vivo.com.br", "apnUser": apnUserA, "serverHost": "rastreio.exemplo.com",
		"serverPort": port, "reportIntervalSeconds": interval,
	}, http.StatusOK)
	assertClean(t, "PATCH /devices (A)", out)
	dev, err := env.devices.Get(ctx, devA.ID)
	if err != nil {
		t.Fatal(err)
	}
	if dev.Model != "J16 v2" || dev.CommandPassword != cmdSecretA || dev.APNPassword != apnSecretA ||
		dev.CommandOverrides["ENGINE_RESUME"] != "HFYD,"+cmdSecretA+"#" {
		t.Fatalf("a edição sem senha deveria manter as credenciais: %+v", dev)
	}
	// Reenviar os overrides como a leitura mostrou (redigidos) mantém o texto.
	var shown devices.View
	if err := json.Unmarshal(env.must(admin, http.MethodGet, deviceA, nil, http.StatusOK), &shown); err != nil {
		t.Fatal(err)
	}
	env.must(admin, http.MethodPatch, deviceA, map[string]any{
		"imei": "869247061230011", "protocol": "gt06", "commandOverrides": shown.CommandOverrides,
	}, http.StatusOK)
	if dev, _ := env.devices.Get(ctx, devA.ID); dev.CommandOverrides["ENGINE_RESUME"] != "HFYD,"+cmdSecretA+"#" {
		t.Fatalf("override redigido reenviado sem mudança deveria manter o original: %q",
			dev.CommandOverrides["ENGINE_RESUME"])
	}
	// E o aparelho continua recebendo a senha depois da edição.
	env.must(operator, http.MethodPost, vehicleA+"/commands/engine-resume", nil, http.StatusAccepted)
	if sent := env.sender.texts(t, "869247061230011"); sent[len(sent)-1] != "HFYD,"+cmdSecretA+"#" {
		t.Fatalf("depois da edição o aparelho recebeu %q", sent[len(sent)-1])
	}
	// Apagar é explícito.
	env.must(admin, http.MethodPatch, deviceB, map[string]any{
		"imei": "869247061230022", "protocol": "gt06", "clearApnPassword": true,
	}, http.StatusOK)
	if dev, _ := env.devices.Get(ctx, devB.ID); dev.APNPassword != "" || dev.CommandPassword != cmdSecretB {
		t.Fatalf("clearApnPassword deveria apagar só a senha APN: %+v", dev)
	}

	// --- Nada gravado com senha --------------------------------------------
	assertTablesClean(t, env.db, credentialSecrets...)
	assertAuditRecordsCredentialChanges(t, env.db, devA.ID)
}

// assertTablesClean confere o que ficou gravado: histórico de comandos e
// auditoria.
func assertTablesClean(t *testing.T, db *database.DB, secrets ...string) {
	t.Helper()
	rows, err := db.Query(context.Background(), `
		SELECT 'device_commands ' || id::text, concat_ws(' | ', payload, response, error) FROM device_commands
		UNION ALL
		SELECT 'audit_logs ' || id::text, COALESCE(metadata::text, '') FROM audit_logs`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var where, text string
		if err := rows.Scan(&where, &text); err != nil {
			t.Fatal(err)
		}
		if found := leakcheck.Find([]byte(text), secrets...); len(found) > 0 {
			t.Errorf("%s guardou credencial: %v: %s", where, found, text)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func assertAuditRecordsCredentialChanges(t *testing.T, db *database.DB, deviceID uuid.UUID) {
	t.Helper()
	var metadata string
	err := db.QueryRow(context.Background(), `
		SELECT metadata::text FROM audit_logs
		WHERE action = $1 AND device_id = $2 ORDER BY id LIMIT 1`, audit.ActionDeviceCreated, deviceID).Scan(&metadata)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"apnPassword", "commandPassword", "commandOverrides"} {
		if !strings.Contains(metadata, name) {
			t.Errorf("a auditoria do cadastro deveria registrar que %s foi definido: %s", name, metadata)
		}
	}
}

// A migration 0012 limpa o histórico gravado antes da correção: a senha
// atual onde estiver, e a senha antiga (já trocada) na posição conhecida dos
// comandos GT06.
func TestMigrationRedactsLegacyHistory(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()

	var deviceID uuid.UUID
	if err := db.QueryRow(ctx, `
		INSERT INTO devices (imei, protocol, command_password, apn_password)
		VALUES ('869247061230044', 'gt06', $1, $2) RETURNING id`, cmdSecretA, apnSecretB).Scan(&deviceID); err != nil {
		t.Fatal(err)
	}
	legacy := []struct{ payload, response string }{
		{`xx\x11\x80\x0F\x00\x00\x00\x01DYD,` + cmdSecretA + `#\x00\x02\x00\x01\xAB\xCD\r\n`,
			"DYD," + strings.ToUpper(cmdSecretA) + "#=Success!"},
		{`xx\x11\x80\x0F\x00\x00\x00\x02TIMER,` + oldSecret + `,30#\x00\x02`, "TIMER=Success!"},
		{`xx\x11\x80\x0F\x00\x00\x00\x03APN,zap,user,` + apnSecretB + `#\x00\x02`, "whereis," + oldSecret},
		{`xx\x11\x80\x0F\x00\x00\x00\x04WHERE,` + oldSecret + `#\x00\x02`, "Lat:-23.550500"},
	}
	for i, row := range legacy {
		if _, err := db.Exec(ctx, `
			INSERT INTO device_commands (device_id, command, payload, status, correlation_key, response, error)
			VALUES ($1, 'CUSTOM', $2, 'ACKNOWLEDGED', $3, $4, $4)`, deviceID, row.payload, i+1, row.response); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(ctx, `
		INSERT INTO audit_logs (action, device_id, result, metadata) VALUES
		('COMMAND_REQUESTED', $1, 'PENDING', jsonb_build_object('command', 'ENGINE_CUT', 'payload', $2::text)),
		('COMMAND_RESULT', $1, 'ACKNOWLEDGED', jsonb_build_object('command', 'ENGINE_CUT', 'response', $3::text)),
		('COMMAND_REQUESTED', NULL, 'PENDING', jsonb_build_object('payload', $4::text))`,
		deviceID, legacy[0].payload, legacy[0].response, legacy[3].payload); err != nil {
		t.Fatal(err)
	}

	body, err := migrations.FS.ReadFile("0012_redact_device_credentials.sql")
	if err != nil {
		t.Fatal(err)
	}
	// Duas vezes: a segunda não pode mudar nada nem falhar.
	for range 2 {
		if _, err := db.Exec(ctx, string(body)); err != nil {
			t.Fatalf("migration 0012: %v", err)
		}
	}

	// A senha antiga só é reconhecida na posição dos comandos GT06: o eco
	// "whereis,<antiga>" fica (e é por isso que o README pede para trocar as
	// senhas); o resto sai limpo.
	assertTablesClean(t, db, cmdSecretA, apnSecretB)
	var payloads []string
	rows, err := db.Query(ctx, `SELECT payload FROM device_commands ORDER BY correlation_key`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			t.Fatal(err)
		}
		payloads = append(payloads, p)
	}
	rows.Close()
	for i, want := range []string{"DYD,***#", "TIMER,***,30#", "APN,zap,user,***#", "WHERE,***#"} {
		if !strings.Contains(payloads[i], want) || strings.Contains(payloads[i], oldSecret) {
			t.Errorf("payload legado %d: %q (esperava %q)", i, payloads[i], want)
		}
	}
}
