package alerts

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/events"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/mail"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/push"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/tracking"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/webpush"
)

// --- Dublês -----------------------------------------------------------------

type memStore struct {
	mu      sync.Mutex
	now     func() time.Time
	targets map[uuid.UUID]*Target
	offline map[uuid.UUID]bool
	rows    []*Notification
	nextID  int64
}

func (m *memStore) Target(_ context.Context, deviceID uuid.UUID) (*Target, error) {
	return m.targets[deviceID], nil
}

func (m *memStore) match(recipient string, deviceID uuid.UUID, kind string, statuses ...string) []*Notification {
	out := []*Notification{}
	for _, n := range m.rows {
		if n.Recipient == recipient && n.DeviceID != nil && *n.DeviceID == deviceID && n.Kind == kind {
			for _, st := range statuses {
				if n.Status == st {
					out = append(out, n)
				}
			}
		}
	}
	return out
}

func (m *memStore) LastSent(_ context.Context, recipient string, deviceID uuid.UUID, kind string) (time.Time, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rows := m.match(recipient, deviceID, kind, StatusPending, StatusSent)
	if len(rows) == 0 {
		return time.Time{}, false, nil
	}
	return rows[len(rows)-1].CreatedAt, true, nil
}

func (m *memStore) CountSentSince(_ context.Context, recipient string, since time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, r := range m.rows {
		if r.Recipient == recipient && !r.CreatedAt.Before(since) && r.Kind != KindTest &&
			(r.Status == StatusPending || r.Status == StatusSent) {
			n++
		}
	}
	return n, nil
}

func (m *memStore) CountSuppressedSinceLastSent(_ context.Context, recipient string, deviceID uuid.UUID, kind string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var last time.Time
	if sent := m.match(recipient, deviceID, kind, StatusPending, StatusSent); len(sent) > 0 {
		last = sent[len(sent)-1].CreatedAt
	}
	n := 0
	for _, r := range m.match(recipient, deviceID, kind, StatusSuppressed) {
		if r.CreatedAt.After(last) || last.IsZero() {
			n++
		}
	}
	return n, nil
}

func (m *memStore) Insert(_ context.Context, n *Notification) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	n.ID, n.CreatedAt = m.nextID, m.now()
	copied := *n
	m.rows = append(m.rows, &copied)
	return nil
}

func (m *memStore) set(id int64, status, errText string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.rows {
		if r.ID == id {
			r.Status, r.Error = status, errText
		}
	}
}

func (m *memStore) MarkSent(_ context.Context, id int64, _ time.Time) error {
	m.set(id, StatusSent, "")
	return nil
}

func (m *memStore) MarkFailed(_ context.Context, id int64, reason string) error {
	m.set(id, StatusFailed, reason)
	return nil
}

func (m *memStore) MarkPushed(_ context.Context, id int64, count int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.rows {
		if r.ID == id {
			r.PushSent = count
		}
	}
	return nil
}

func (m *memStore) DeviceOffline(_ context.Context, id uuid.UUID) (bool, error) {
	return m.offline[id], nil
}
func (m *memStore) Prune(context.Context, time.Time) (int64, error) { return 0, nil }

func (m *memStore) LastTest(_ context.Context, userID uuid.UUID) (time.Time, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := len(m.rows) - 1; i >= 0; i-- {
		if r := m.rows[i]; r.Kind == KindTest && r.UserID != nil && *r.UserID == userID {
			return r.CreatedAt, true, nil
		}
	}
	return time.Time{}, false, nil
}

func (m *memStore) statuses(kind string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []string{}
	for _, r := range m.rows {
		if r.Kind == kind {
			out = append(out, r.Status+"/"+r.Reason)
		}
	}
	return out
}

type memMailer struct {
	mu   sync.Mutex
	sent []mail.Alert
}

func (m *memMailer) Alert(_ context.Context, a mail.Alert) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, a)
	return nil
}

func (m *memMailer) take() []mail.Alert {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := m.sent
	m.sent = nil
	return out
}

type memStates map[uuid.UUID]*tracking.State

func (m memStates) Get(id uuid.UUID) *tracking.State { return m[id] }

// --- Montagem ---------------------------------------------------------------

type harness struct {
	t      *testing.T
	e      *Engine
	store  *memStore
	mailer *memMailer
	states memStates
	now    time.Time
	ctx    context.Context
}

var (
	ownerID   = uuid.MustParse("11111111-1111-4111-8111-111111111111")
	vehicleID = uuid.MustParse("22222222-2222-4222-8222-222222222222")
	deviceID  = uuid.MustParse("33333333-3333-4333-8333-333333333333")
	sp, _     = time.LoadLocation("America/Sao_Paulo")
)

func newHarness(t *testing.T, mutate func(*config.Alerts)) *harness {
	t.Helper()
	cfg := config.Alerts{
		Enabled: true, Cooldown: 30 * time.Minute, MaxEventAge: 10 * time.Minute, MaxPerHour: 20,
		OfflineParkedAfter: 2 * time.Hour, TowingDistanceMeters: 300, Timezone: "America/Sao_Paulo",
	}
	if mutate != nil {
		mutate(&cfg)
	}
	h := &harness{t: t, mailer: &memMailer{}, states: memStates{}, ctx: context.Background(),
		// Uma quarta-feira às 02:14 em São Paulo: dentro da vigilância padrão.
		now: time.Date(2026, 9, 30, 2, 14, 0, 0, sp)}
	h.store = &memStore{now: func() time.Time { return h.now }, offline: map[uuid.UUID]bool{},
		targets: map[uuid.UUID]*Target{deviceID: {
			VehicleID: &vehicleID, VehicleName: "Moto da Ana", Plate: "ABC1D23",
			Owner: &Owner{UserID: ownerID, Email: "ana@cliente.test", Name: "Ana Souza", Active: true,
				Settings: DefaultSettings()},
		}}}
	h.e = NewEngine(cfg, "https://painel.farbo.test", h.store, h.mailer, h.states, nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	h.e.now = func() time.Time { return h.now }
	return h
}

func (h *harness) owner() *Owner { return h.store.targets[deviceID].Owner }

// event processa um evento como o Run faria e entrega os e-mails.
func (h *harness) event(typ string, at time.Time, meta map[string]any) []mail.Alert {
	h.t.Helper()
	h.e.handleEvent(h.ctx, &events.Event{ID: 7, DeviceID: deviceID, Type: typ, Timestamp: at, Metadata: meta})
	return h.flush()
}

func (h *harness) flush() []mail.Alert {
	for {
		select {
		case c := <-h.e.candidates:
			h.e.process(h.ctx, c)
		case d := <-h.e.sends:
			h.e.send(h.ctx, d)
		default:
			return h.mailer.take()
		}
	}
}

func (h *harness) position(lat, lon, speed float64, acc bool, at time.Time) []mail.Alert {
	h.t.Helper()
	valid := true
	h.e.OnPosition(deviceID, &tracking.Position{DeviceID: deviceID, GPSTimestamp: at, Latitude: lat,
		Longitude: lon, SpeedKmh: speed, GPSValid: &valid}, &acc)
	return h.flush()
}

func ptr[T any](v T) *T { return &v }

// --- Testes -----------------------------------------------------------------

func TestIgnitionAlertsFollowGuardWindow(t *testing.T) {
	h := newHarness(t, nil)
	got := h.event(events.IgnitionOn, h.now, nil)
	if len(got) != 1 || got[0].Title != "Ignição ligada no horário de vigilância" {
		t.Fatalf("02:14 está na vigilância (22:00–06:00): esperava 1 alerta, veio %+v", got)
	}
	if !strings.Contains(got[0].Summary, "02:14") || !strings.Contains(got[0].Summary, "22:00 às 06:00") {
		t.Errorf("o e-mail deve dizer a hora e o horário de vigilância: %q", got[0].Summary)
	}

	h.now = time.Date(2026, 9, 30, 14, 0, 0, 0, sp)
	if got := h.event(events.IgnitionOn, h.now, nil); len(got) != 0 {
		t.Fatalf("14:00 fora da vigilância, com 'ignição a qualquer hora' desligada: não devia avisar, veio %d", len(got))
	}

	h.owner().Settings.Kinds[KindIgnition] = true
	got = h.event(events.IgnitionOn, h.now, nil)
	if len(got) != 1 || got[0].Title != "Ignição ligada" {
		t.Fatalf("com 'ignição a qualquer hora' ligada: esperava o aviso simples, veio %+v", got)
	}
}

func TestGuardWindowCustomAndWrapping(t *testing.T) {
	s := Settings{GuardStart: 22 * 60, GuardEnd: 6 * 60}
	for minute, want := range map[int]bool{22 * 60: true, 23*60 + 59: true, 0: true, 5*60 + 59: true, 6 * 60: false, 12 * 60: false, 21*60 + 59: false} {
		if s.InGuard(minute) != want {
			t.Errorf("22:00–06:00, minuto %s: esperava %v", FormatClock(minute), want)
		}
	}
	s = Settings{GuardStart: 8 * 60, GuardEnd: 18 * 60}
	if !s.InGuard(12*60) || s.InGuard(19*60) || s.InGuard(7*60) {
		t.Error("janela no mesmo dia (08:00–18:00) calculada errado")
	}
}

func TestOldEventsNeverBecomeEmail(t *testing.T) {
	h := newHarness(t, nil)
	// O rastreador voltou do sem-sinal e descarregou um SOS de 40 min atrás.
	if got := h.event(events.SOS, h.now.Add(-40*time.Minute), nil); len(got) != 0 {
		t.Fatalf("evento antigo não pode virar e-mail, veio %d", len(got))
	}
	if got := h.event(events.SOS, h.now.Add(-2*time.Minute), nil); len(got) != 1 {
		t.Fatalf("evento recente vira e-mail, veio %d", len(got))
	}
}

func TestCooldownHoldsRepeatsAndSummarizesThem(t *testing.T) {
	h := newHarness(t, nil)
	if got := h.event(events.SOS, h.now, nil); len(got) != 1 || got[0].Suppressed != 0 {
		t.Fatalf("primeiro SOS: 1 e-mail sem repetidos, veio %+v", got)
	}
	for range 3 {
		h.now = h.now.Add(5 * time.Minute)
		if got := h.event(events.SOS, h.now, nil); len(got) != 0 {
			t.Fatalf("SOS repetido dentro de 30 min não pode sair, veio %d", len(got))
		}
	}
	h.now = h.now.Add(20 * time.Minute) // 35 min depois do primeiro
	got := h.event(events.SOS, h.now, nil)
	if len(got) != 1 || got[0].Suppressed != 3 {
		t.Fatalf("depois do intervalo: 1 e-mail resumindo 3 repetidos, veio %+v", got)
	}
	if got[0].Cooldown != "30 minutos" {
		t.Errorf("o e-mail explica o intervalo: %q", got[0].Cooldown)
	}
	want := []string{"SENT/", "SUPPRESSED/cooldown", "SUPPRESSED/cooldown", "SUPPRESSED/cooldown", "SENT/"}
	if st := h.store.statuses(KindSOS); strings.Join(st, ",") != strings.Join(want, ",") {
		t.Errorf("histórico: %v, esperado %v", st, want)
	}
}

func TestHourlyCapPerRecipient(t *testing.T) {
	h := newHarness(t, func(c *config.Alerts) { c.MaxPerHour = 2 })
	h.event(events.SOS, h.now, nil)
	h.event(events.PowerLoss, h.now, nil)
	if got := h.event(events.LowBattery, h.now, nil); len(got) != 0 {
		t.Fatalf("terceiro alerta na mesma hora passa do teto de 2, veio %d", len(got))
	}
	if st := h.store.statuses(KindLowBattery); len(st) != 1 || st[0] != "SUPPRESSED/hourly_cap" {
		t.Errorf("devia ficar registrado como segurado pelo teto: %v", st)
	}
	h.now = h.now.Add(61 * time.Minute)
	if got := h.event(events.LowBattery, h.now, nil); len(got) != 1 {
		t.Fatalf("na hora seguinte volta a sair, veio %d", len(got))
	}
}

func TestTowingNeedsRealDisplacement(t *testing.T) {
	h := newHarness(t, nil)
	const lat, lon = -23.550500, -46.633300
	at := func() time.Time { h.now = h.now.Add(time.Minute); return h.now }

	h.position(lat, lon, 0, false, at())                                   // estacionou: vira o ponto de referência
	if got := h.position(lat+0.0009, lon, 0, false, at()); len(got) != 0 { // ~100 m de ruído
		t.Fatalf("ruído de GPS de 100 m não é reboque, veio %d", len(got))
	}
	if got := h.position(lat+0.0040, lon, 0, false, at()); len(got) != 0 { // um salto de ~450 m, parado
		t.Fatalf("um único salto sem velocidade pode ser erro do GPS, veio %d", len(got))
	}
	if got := h.position(lat, lon, 0, false, at()); len(got) != 0 { // voltou: zera
		t.Fatalf("voltou ao lugar, veio %d", len(got))
	}
	h.position(lat+0.0040, lon, 0, false, at())
	got := h.position(lat+0.0050, lon, 0, false, at()) // dois pontos seguidos fora do raio
	if len(got) != 1 || got[0].Title != "Veículo em movimento com a ignição desligada" || got[0].Severity != mail.SeverityCritical {
		t.Fatalf("dois pontos seguidos fora do raio com a ignição desligada: reboque, veio %+v", got)
	}
	if !strings.Contains(got[0].Summary, "cerca de 556 m") {
		t.Errorf("o e-mail diz a distância: %q", got[0].Summary)
	}
	if got := h.position(lat+0.0200, lon, 30, false, at()); len(got) != 0 {
		t.Fatalf("um alerta por estacionamento, veio %d", len(got))
	}

	// Ignição ligada encerra o estacionamento; o próximo começa do zero.
	// (Passado o intervalo mínimo: dentro dele o segundo reboque seria
	// segurado como repetição.)
	h.position(lat+0.02, lon, 20, true, at())
	h.now = h.now.Add(31 * time.Minute)
	h.position(lat+0.03, lon, 0, false, at())
	if got := h.position(lat+0.035, lon, 25, false, at()); len(got) != 1 {
		t.Fatalf("com velocidade, um ponto fora do raio já basta, veio %d", len(got))
	}
}

func TestTowingIgnoresBufferedPositions(t *testing.T) {
	h := newHarness(t, nil)
	old := h.now.Add(-time.Hour)
	h.position(-23.55, -46.63, 0, false, old)
	h.position(-23.50, -46.63, 40, false, old.Add(time.Minute))
	if got := h.flush(); len(got) != 0 {
		t.Fatalf("posições descarregadas depois do sem-sinal não geram reboque, veio %d", len(got))
	}
	if len(h.e.parked) != 0 {
		t.Error("posição antiga nem vira ponto de referência")
	}
}

func TestSignalLostMovingVersusParked(t *testing.T) {
	h := newHarness(t, nil)
	h.states[deviceID] = &tracking.State{ACC: ptr(true)}
	if got := h.event(events.DeviceDisconnected, h.now, nil); len(got) != 0 {
		t.Fatalf("andando, a queda espera o prazo antes de avisar, veio %d", len(got))
	}
	h.now = h.now.Add(signalLostGrace - time.Second)
	h.e.sweepLost(h.ctx)
	if got := h.flush(); len(got) != 0 {
		t.Fatalf("dentro do prazo ainda não, veio %d", len(got))
	}
	h.now = h.now.Add(time.Second)
	h.e.sweepLost(h.ctx)
	got := h.flush()
	if len(got) != 1 || got[0].Title != "Rastreador sem sinal com o veículo em movimento" {
		t.Fatalf("sinal caiu com a ignição ligada e não voltou: alerta, veio %+v", got)
	}

	// Parado: só avisa se continuar sem sinal pelo prazo configurado.
	h2 := newHarness(t, nil)
	h2.states[deviceID] = &tracking.State{ACC: ptr(false)}
	if got := h2.event(events.DeviceDisconnected, h2.now, nil); len(got) != 0 {
		t.Fatalf("parado, sem sinal: não avisa na hora, veio %d", len(got))
	}
	h2.store.offline[deviceID] = true
	h2.now = h2.now.Add(time.Hour)
	h2.e.sweepOffline(h2.ctx)
	if got := h2.flush(); len(got) != 0 {
		t.Fatalf("1 h sem sinal parado ainda não é alerta, veio %d", len(got))
	}
	h2.now = h2.now.Add(61 * time.Minute)
	h2.e.sweepOffline(h2.ctx)
	got = h2.flush()
	if len(got) != 1 || got[0].Title != "Rastreador sem comunicação há mais de 2 horas" {
		t.Fatalf("2 h sem sinal parado: alerta, veio %+v", got)
	}

	// Voltou a comunicar antes do prazo: nada.
	h3 := newHarness(t, nil)
	h3.states[deviceID] = &tracking.State{ACC: ptr(false)}
	h3.event(events.DeviceDisconnected, h3.now, nil)
	h3.event(events.DeviceConnected, h3.now.Add(30*time.Minute), nil)
	h3.now = h3.now.Add(3 * time.Hour)
	h3.e.sweepOffline(h3.ctx)
	if got := h3.flush(); len(got) != 0 {
		t.Fatalf("reconectou antes do prazo: sem alerta, veio %d", len(got))
	}
}

// Conexão que cai e volta logo (troca de antena, reinício do servidor) não
// é sinal perdido.
func TestSignalLostIgnoresQuickReconnect(t *testing.T) {
	h := newHarness(t, nil)
	h.states[deviceID] = &tracking.State{ACC: ptr(true)}
	h.event(events.DeviceDisconnected, h.now, nil)
	h.now = h.now.Add(20 * time.Second)
	h.event(events.DeviceConnected, h.now, nil)
	h.now = h.now.Add(2 * signalLostGrace)
	h.e.sweepLost(h.ctx)
	if got := h.flush(); len(got) != 0 {
		t.Fatalf("voltou em 20 s: sem alerta, veio %d", len(got))
	}

	// Uma posição nova também conta como volta.
	h.event(events.DeviceDisconnected, h.now, nil)
	h.now = h.now.Add(10 * time.Second)
	h.position(-23.55, -46.63, 40, true, h.now)
	h.now = h.now.Add(2 * signalLostGrace)
	h.e.sweepLost(h.ctx)
	if got := h.flush(); len(got) != 0 {
		t.Fatalf("mandou posição depois da queda: sem alerta, veio %d", len(got))
	}
}

// Muitas quedas juntas são o servidor ou a rede: ninguém recebe "sinal
// perdido"; quem não voltar entra no alerta de sem comunicação.
func TestMassDisconnectHoldsSignalLost(t *testing.T) {
	dropOthers := func(h *harness, n int) {
		for range n {
			h.now = h.now.Add(200 * time.Millisecond)
			h.e.handleEvent(h.ctx, &events.Event{DeviceID: uuid.New(), Type: events.DeviceDisconnected, Timestamp: h.now})
		}
	}

	h := newHarness(t, nil)
	h.e.SetConnectedCount(func() int { return 30 })
	h.states[deviceID] = &tracking.State{ACC: ptr(true)}
	h.event(events.DeviceDisconnected, h.now, nil)
	dropOthers(h, massMinimum) // 21 quedas em ~4 s
	h.now = h.now.Add(signalLostGrace)
	h.e.sweepLost(h.ctx)
	if got := h.flush(); len(got) != 0 {
		t.Fatalf("queda em massa: sem sinal perdido, veio %+v", got)
	}
	if !h.e.massLast.IsZero() {
		t.Error("a queda em massa devia ter terminado depois da janela")
	}
	// Continuou fora pelo prazo longo: aí avisa.
	h.store.offline[deviceID] = true
	h.now = h.now.Add(2 * time.Hour)
	h.e.sweepOffline(h.ctx)
	if got := h.flush(); len(got) != 1 || got[0].Title != "Rastreador sem comunicação há mais de 2 horas" {
		t.Fatalf("sem voltar pelo prazo longo: alerta de sem comunicação, veio %+v", got)
	}

	// As mesmas 21 quedas numa frota de 1000 conectados são o normal do dia:
	// o veículo andando que não voltou é avisado.
	h2 := newHarness(t, nil)
	h2.e.SetConnectedCount(func() int { return 1000 })
	h2.states[deviceID] = &tracking.State{ACC: ptr(true)}
	h2.event(events.DeviceDisconnected, h2.now, nil)
	dropOthers(h2, massMinimum)
	h2.now = h2.now.Add(signalLostGrace)
	h2.e.sweepLost(h2.ctx)
	if got := h2.flush(); len(got) != 1 {
		t.Fatalf("21 quedas em 1000 não é massa: alerta, veio %d", len(got))
	}
}

func TestRecipients(t *testing.T) {
	central := func(c *config.Alerts) { c.CentralEmails = []string{"central@farbo.test"} }

	h := newHarness(t, central)
	got := h.event(events.SOS, h.now, nil)
	if len(got) != 2 {
		t.Fatalf("SOS: dono e central, veio %d", len(got))
	}
	if got := h.event(events.LowBattery, h.now, nil); len(got) != 1 || got[0].To != "ana@cliente.test" {
		t.Fatalf("alerta que não é de segurança fica só com o dono, veio %+v", got)
	}

	h = newHarness(t, central)
	h.owner().Suspended = true
	got = h.event(events.PowerLoss, h.now, nil)
	if len(got) != 1 || got[0].To != "central@farbo.test" || got[0].SettingsURL != "" {
		t.Fatalf("cliente suspenso não recebe; a central recebe o de segurança (sem link de preferências), veio %+v", got)
	}

	h = newHarness(t, central)
	h.owner().Active = false
	if got := h.event(events.LowBattery, h.now, nil); len(got) != 0 {
		t.Fatalf("cliente inativo não recebe, veio %d", len(got))
	}

	h = newHarness(t, central)
	delete(h.owner().Settings.Kinds, KindSOS)
	if got := h.event(events.SOS, h.now, nil); len(got) != 1 || got[0].To != "central@farbo.test" {
		t.Fatalf("SOS desligado pelo cliente: só a central, veio %+v", got)
	}

	h = newHarness(t, central)
	h.store.targets[deviceID].Owner = nil // veículo da central
	if got := h.event(events.LowBattery, h.now, nil); len(got) != 1 || got[0].To != "central@farbo.test" {
		t.Fatalf("veículo da central: a central recebe os alertas padrão, veio %+v", got)
	}

	h = newHarness(t, func(c *config.Alerts) { c.CentralEmails = []string{"ANA@cliente.test"} })
	if got := h.event(events.SOS, h.now, nil); len(got) != 1 {
		t.Fatalf("mesmo endereço no dono e na central: um e-mail só, veio %d", len(got))
	}

	h = newHarness(t, nil)
	delete(h.store.targets, deviceID) // rastreador em estoque
	if got := h.event(events.SOS, h.now, nil); len(got) != 0 {
		t.Fatalf("rastreador sem veículo: ninguém para avisar, veio %d", len(got))
	}
}

func TestEngineBlockAndResumeAreDifferentAlerts(t *testing.T) {
	h := newHarness(t, nil)
	if got := h.event(events.EngineCutAck, h.now, nil); len(got) != 1 || got[0].Title != "Motor bloqueado" {
		t.Fatalf("bloqueio confirmado, veio %+v", got)
	}
	h.now = h.now.Add(2 * time.Minute)
	if got := h.event(events.EngineResumeAck, h.now, nil); len(got) != 1 || got[0].Title != "Motor liberado" {
		t.Fatalf("liberar logo depois é outro alerta, não repetição, veio %+v", got)
	}
}

func TestOverspeedCarriesSpeedAndLimit(t *testing.T) {
	h := newHarness(t, nil)
	h.e.handleEvent(h.ctx, &events.Event{DeviceID: deviceID, Type: events.Overspeed, Timestamp: h.now,
		SpeedKmh: ptr(127.4), Latitude: ptr(-23.5), Longitude: ptr(-46.6), Metadata: map[string]any{"limitKmh": 100.0}})
	got := h.flush()
	if len(got) != 1 || *got[0].Speed != 127.4 || *got[0].Limit != 100 {
		t.Fatalf("excesso: velocidade e limite no e-mail, veio %+v", got)
	}
	if !strings.Contains(got[0].Summary, "limite de 100 km/h") || !strings.Contains(got[0].Summary, "127 km/h") {
		t.Errorf("resumo: %q", got[0].Summary)
	}
	if got[0].ActionURL != "https://painel.farbo.test/veiculos/"+vehicleID.String() {
		t.Errorf("o botão abre o veículo: %q", got[0].ActionURL)
	}
}

func TestIgnoredEventsAndDisplacementAlarm(t *testing.T) {
	h := newHarness(t, nil)
	for _, typ := range []string{events.IgnitionOff, events.Vibration, events.GPSLost, events.OverspeedEnd} {
		if got := h.event(typ, h.now, nil); len(got) != 0 {
			t.Errorf("%s não vira alerta, veio %d", typ, len(got))
		}
	}
	if got := h.event(events.DeviceAlarm, h.now, map[string]any{"alarm": "UNKNOWN_X"}); len(got) != 0 {
		t.Errorf("alarme desconhecido não vira alerta, veio %d", len(got))
	}
	got := h.event(events.DeviceAlarm, h.now, map[string]any{"alarm": "DISPLACEMENT"})
	if len(got) != 1 || got[0].Title != "Veículo em movimento com a ignição desligada" {
		t.Errorf("alarme de deslocamento do aparelho = reboque, veio %+v", got)
	}
}

func TestHooksNeverBlockIngestion(t *testing.T) {
	h := newHarness(t, nil)
	done := make(chan struct{})
	go func() {
		// Sem Run consumindo, as filas enchem: o gancho descarta e segue.
		for range eventQueue + 100 {
			h.e.OnEvent(&events.Event{DeviceID: deviceID, Type: events.SOS, Timestamp: h.now})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("OnEvent bloqueou com a fila cheia")
	}
	if h.e.dropped.Load() != 100 {
		t.Errorf("descartados: %d, esperado 100", h.e.dropped.Load())
	}
}

func TestSendTestIsRateLimited(t *testing.T) {
	h := newHarness(t, nil)
	if err := h.e.SendTest(h.ctx, ownerID, "ana@cliente.test", "Ana"); err != nil {
		t.Fatal(err)
	}
	if got := h.mailer.take(); len(got) != 1 || !strings.HasPrefix(got[0].Subject, "Teste:") {
		t.Fatalf("e-mail de teste, veio %+v", got)
	}
	h.now = h.now.Add(20 * time.Second)
	if err := h.e.SendTest(h.ctx, ownerID, "ana@cliente.test", "Ana"); err != ErrTestTooSoon {
		t.Fatalf("segundo teste em 20 s: esperava ErrTestTooSoon, veio %v", err)
	}
}

func TestNewSettingsValidation(t *testing.T) {
	if _, err := NewSettings([]string{KindSOS, "INVENTADO"}, "22:00", "06:00"); err == nil {
		t.Error("tipo desconhecido devia ser recusado")
	}
	for _, bad := range [][2]string{{"24:00", "06:00"}, {"22:0", "06:00"}, {"22h", "06:00"}, {"08:00", "08:00"}, {"", "06:00"}} {
		if _, err := NewSettings(nil, bad[0], bad[1]); err == nil {
			t.Errorf("horário %v devia ser recusado", bad)
		}
	}
	s, err := NewSettings([]string{KindIgnition, KindSOS}, "23:30", "5:15")
	if err != nil || s.GuardStart != 23*60+30 || s.GuardEnd != 5*60+15 || !s.Enabled(KindSOS) || s.Enabled(KindTowing) {
		t.Fatalf("configuração válida mal lida: %+v %v", s, err)
	}
	if got := strings.Join(s.EnabledList(), ","); got != "SOS,IGNITION" {
		t.Errorf("lista na ordem do catálogo: %s", got)
	}
}

func TestDefaultsTurnOnEverythingButPlainIgnition(t *testing.T) {
	d := DefaultSettings()
	for _, k := range Catalog {
		if d.Enabled(k.Kind) != (k.Kind != KindIgnition) {
			t.Errorf("padrão de %s: %v", k.Kind, d.Enabled(k.Kind))
		}
	}
}

// --- Notificação no celular ---------------------------------------------------

type memPusher struct {
	mu   sync.Mutex
	sent map[uuid.UUID][]push.Notification
}

func (m *memPusher) Notify(_ context.Context, userID uuid.UUID, n push.Notification) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sent == nil {
		m.sent = map[uuid.UUID][]push.Notification{}
	}
	m.sent[userID] = append(m.sent[userID], n)
	return 2, nil // dois aparelhos inscritos
}

type failingMailer struct{}

func (failingMailer) Alert(context.Context, mail.Alert) error { return errors.New("smtp fora do ar") }

func TestAlertAlsoGoesToCustomerPhone(t *testing.T) {
	h := newHarness(t, func(c *config.Alerts) { c.CentralEmails = []string{"central@farbo.test"} })
	p := &memPusher{}
	h.e.SetPusher(p)

	h.event(events.SOS, h.now, nil)
	got := p.sent[ownerID]
	if len(got) != 1 {
		t.Fatalf("SOS: 1 notificação no celular do dono, veio %d", len(got))
	}
	n := got[0]
	if n.URL != "/app/veiculos/"+vehicleID.String() || n.Urgency != webpush.UrgencyHigh || n.Severity != mail.SeverityCritical {
		t.Errorf("SOS abre o veículo no app, com urgência alta: %+v", n)
	}
	if !strings.Contains(n.Title, "SOS") || !strings.Contains(n.Body, "Moto da Ana") || n.Tag != "SOS:"+vehicleID.String() {
		t.Errorf("conteúdo: %+v", n)
	}
	if len(n.Topic) > 32 || strings.ContainsAny(n.Topic, "+/=:") {
		t.Errorf("Topic precisa de até 32 caracteres base64url: %q", n.Topic)
	}
	if len(p.sent) != 1 {
		t.Error("a central não tem app: não recebe notificação no celular")
	}
	if rows := h.store.rows; rows[0].PushSent != 2 {
		t.Errorf("histórico registra em quantos celulares chegou: %d", rows[0].PushSent)
	}

	h.now = h.now.Add(5 * time.Minute)
	h.event(events.SOS, h.now, nil)
	if len(p.sent[ownerID]) != 1 {
		t.Error("repetido segurado pelo intervalo também não vai para o celular")
	}

	h.now = h.now.Add(10 * time.Minute)
	h.event(events.LowBattery, h.now, nil)
	if n := p.sent[ownerID][1]; n.Urgency != webpush.UrgencyNormal || n.TTL != time.Hour {
		t.Errorf("alerta comum: urgência normal e validade de 1 h: %+v", n)
	}
}

func TestPhoneStillGetsAlertWhenEmailFails(t *testing.T) {
	h := newHarness(t, nil)
	p := &memPusher{}
	h.e.SetPusher(p)
	h.e.mailer = failingMailer{}
	h.e.handleEvent(h.ctx, &events.Event{DeviceID: deviceID, Type: events.PowerLoss, Timestamp: h.now})
	for {
		select {
		case d := <-h.e.sends:
			// Sem esperar o segundo envio (5 s): basta o primeiro canal.
			ctx, cancel := context.WithCancel(h.ctx)
			cancel()
			h.e.send(ctx, d)
			continue
		default:
		}
		break
	}
	if len(p.sent[ownerID]) != 1 {
		t.Fatalf("SMTP fora do ar não pode impedir a notificação no celular, veio %d", len(p.sent[ownerID]))
	}
	if st := h.store.statuses(KindPowerCut); len(st) != 1 || st[0] != "FAILED/" {
		t.Errorf("o e-mail fica registrado como não entregue: %v", st)
	}
}

func TestGeofenceAlertsGoOnlyToTheFenceOwner(t *testing.T) {
	fenceA, fenceB := uuid.New(), uuid.New()
	meta := func(fence uuid.UUID, name string, owner uuid.UUID, notify bool) map[string]any {
		return map[string]any{"geofenceId": fence, "geofenceName": name, "geofenceOwnerId": owner.String(), "notify": notify}
	}
	h := newHarness(t, func(c *config.Alerts) { c.CentralEmails = []string{"central@farbo.test"} })
	p := &memPusher{}
	h.e.SetPusher(p)

	got := h.event(events.GeofenceEnter, h.now, meta(fenceA, "Casa", ownerID, true))
	if len(got) != 1 || got[0].To != "ana@cliente.test" || got[0].Title != "Entrou na cerca Casa" ||
		!strings.Contains(got[0].Summary, "Moto da Ana entrou na cerca Casa") || got[0].Severity != mail.SeverityInfo {
		t.Fatalf("entrada: só o dono, com o nome da cerca, veio %+v", got)
	}
	if n := p.sent[ownerID]; len(n) != 1 || n[0].Title != "Entrou na cerca Casa" || len(n[0].Topic) > 32 ||
		strings.ContainsAny(n[0].Topic, "+/=:") {
		t.Fatalf("no celular também, com Topic válido: %+v", n)
	}

	// Saída logo depois: outro alerta (não é repetição da entrada).
	h.now = h.now.Add(3 * time.Minute)
	if got := h.event(events.GeofenceExit, h.now, meta(fenceA, "Casa", ownerID, true)); len(got) != 1 || got[0].Title != "Saiu da cerca Casa" {
		t.Fatalf("saída, veio %+v", got)
	}
	// Outra cerca dentro do intervalo: também não é repetição.
	h.now = h.now.Add(3 * time.Minute)
	if got := h.event(events.GeofenceEnter, h.now, meta(fenceB, "Trabalho", ownerID, true)); len(got) != 1 || got[0].Title != "Entrou na cerca Trabalho" {
		t.Fatalf("cerca diferente tem intervalo próprio, veio %+v", got)
	}
	// A mesma cerca de novo dentro do intervalo: segurado.
	h.now = h.now.Add(3 * time.Minute)
	if got := h.event(events.GeofenceEnter, h.now, meta(fenceA, "Casa", ownerID, true)); len(got) != 0 {
		t.Fatalf("mesma cerca dentro do intervalo é repetição, veio %+v", got)
	}

	// Lado sem aviso, cerca da central e cerca de outro dono: ninguém recebe.
	for name, m := range map[string]map[string]any{
		"aviso desligado":  meta(uuid.New(), "Escola", ownerID, false),
		"cerca da central": {"geofenceId": uuid.New(), "geofenceName": "Pátio"},
		"outro dono":       meta(uuid.New(), "Alheia", uuid.New(), true),
	} {
		if got := h.event(events.GeofenceEnter, h.now, m); len(got) != 0 {
			t.Errorf("%s: ninguém recebe, veio %+v", name, got)
		}
	}

	h = newHarness(t, nil)
	h.owner().Suspended = true
	if got := h.event(events.GeofenceEnter, h.now, meta(fenceA, "Casa", ownerID, true)); len(got) != 0 {
		t.Fatalf("cliente suspenso não recebe, veio %+v", got)
	}
}
