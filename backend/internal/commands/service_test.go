package commands

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

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/devices"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/events"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/protocols"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/protocols/gt06"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/telemetry"
)

// ---------------------------------------------------------------------------
// Dublês
// ---------------------------------------------------------------------------

type memoryStore struct {
	mu       sync.Mutex
	commands map[uuid.UUID]*Command
	order    []uuid.UUID
}

func newMemoryStore() *memoryStore {
	return &memoryStore{commands: map[uuid.UUID]*Command{}}
}

func (m *memoryStore) Create(_ context.Context, cmd *Command) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cmd.ID = uuid.New()
	cmd.RequestedAt = time.Now()
	cmd.CreatedAt = cmd.RequestedAt
	stored := *cmd
	m.commands[cmd.ID] = &stored
	m.order = append(m.order, cmd.ID)
	return nil
}

func (m *memoryStore) Get(_ context.Context, id uuid.UUID) (*Command, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cmd, ok := m.commands[id]
	if !ok {
		return nil, errors.New("não encontrado")
	}
	copied := *cmd
	return &copied, nil
}

func (m *memoryStore) ListByDevice(_ context.Context, deviceID uuid.UUID, _ int) ([]*Command, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []*Command{}
	for _, id := range m.order {
		if cmd := m.commands[id]; cmd.DeviceID == deviceID {
			copied := *cmd
			out = append(out, &copied)
		}
	}
	return out, nil
}

func (m *memoryStore) update(id uuid.UUID, mutate func(*Command)) (*Command, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cmd, ok := m.commands[id]
	if !ok {
		return nil, errors.New("não encontrado")
	}
	mutate(cmd)
	copied := *cmd
	return &copied, nil
}

func (m *memoryStore) MarkSending(_ context.Context, id uuid.UUID) (*Command, error) {
	return m.update(id, func(c *Command) { c.Status = StatusSending })
}

func (m *memoryStore) MarkSent(_ context.Context, id uuid.UUID, timeoutAt time.Time) (*Command, error) {
	return m.update(id, func(c *Command) {
		now := time.Now()
		c.Status = StatusSent
		c.SentAt = &now
		c.TimeoutAt = &timeoutAt
	})
}

func (m *memoryStore) MarkAcknowledged(_ context.Context, id uuid.UUID, response string) (*Command, error) {
	return m.update(id, func(c *Command) {
		now := time.Now()
		c.Status = StatusAcknowledged
		c.AcknowledgedAt = &now
		c.Response = response
	})
}

func (m *memoryStore) MarkFinal(_ context.Context, id uuid.UUID, status, reason string) (*Command, error) {
	return m.update(id, func(c *Command) {
		if Terminal(c.Status) {
			return
		}
		c.Status = status
		c.Error = reason
	})
}

func (m *memoryStore) FindOpenByCorrelation(_ context.Context, deviceID uuid.UUID, key uint32) (*Command, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, id := range m.order {
		cmd := m.commands[id]
		if cmd.DeviceID == deviceID && cmd.CorrelationKey == key && !Terminal(cmd.Status) {
			copied := *cmd
			return &copied, nil
		}
	}
	return nil, errors.New("não encontrado")
}

func (m *memoryStore) FindOldestOpen(_ context.Context, deviceID uuid.UUID) (*Command, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, id := range m.order {
		cmd := m.commands[id]
		if cmd.DeviceID == deviceID && (cmd.Status == StatusSent || cmd.Status == StatusSending) {
			copied := *cmd
			return &copied, nil
		}
	}
	return nil, errors.New("não encontrado")
}

func (m *memoryStore) ExpireTimedOut(_ context.Context) ([]*Command, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []*Command{}
	for _, id := range m.order {
		cmd := m.commands[id]
		if (cmd.Status == StatusSent || cmd.Status == StatusSending) &&
			cmd.TimeoutAt != nil && cmd.TimeoutAt.Before(time.Now()) {
			cmd.Status = StatusTimeout
			cmd.Error = "rastreador não respondeu dentro do prazo"
			copied := *cmd
			out = append(out, &copied)
		}
	}
	return out, nil
}

type recordingSender struct {
	mu      sync.Mutex
	sent    [][]byte
	lastIME string
	err     error
}

func (s *recordingSender) Send(imei string, payload []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	s.lastIME = imei
	s.sent = append(s.sent, append([]byte(nil), payload...))
	return nil
}

func (s *recordingSender) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sent)
}

func (s *recordingSender) last() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.sent) == 0 {
		return nil
	}
	return s.sent[len(s.sent)-1]
}

type fixedTelemetry struct {
	snapshot Snapshot
	err      error
}

func (f fixedTelemetry) Snapshot(context.Context, uuid.UUID) (Snapshot, error) {
	return f.snapshot, f.err
}

type recordingEvents struct {
	mu    sync.Mutex
	types []string
}

func (r *recordingEvents) Record(_ context.Context, e *events.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.types = append(r.types, e.Type)
}

func (r *recordingEvents) has(eventType string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, t := range r.types {
		if t == eventType {
			return true
		}
	}
	return false
}

type recordingAudit struct {
	mu      sync.Mutex
	entries []*audit.Entry
}

func (r *recordingAudit) Record(_ context.Context, e *audit.Entry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	copied := *e
	r.entries = append(r.entries, &copied)
}

func (r *recordingAudit) actions() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.entries))
	for _, e := range r.entries {
		out = append(out, e.Action)
	}
	return out
}

type recordingPublisher struct {
	mu     sync.Mutex
	topics []string
	data   []any
}

func (r *recordingPublisher) PublishFor(eventType string, _, _ *uuid.UUID, data any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.topics = append(r.topics, eventType)
	r.data = append(r.data, data)
}

// ---------------------------------------------------------------------------
// Montagem
// ---------------------------------------------------------------------------

type harness struct {
	service   *Service
	store     *memoryStore
	sender    *recordingSender
	events    *recordingEvents
	audit     *recordingAudit
	publisher *recordingPublisher
	device    *devices.Device
}

func newHarness(t *testing.T, snapshot Snapshot, tune func(*config.Commands)) *harness {
	t.Helper()

	cfg := config.Commands{
		AckTimeout:              15 * time.Second,
		EngineCutMaxSpeedKmh:    5,
		EngineCutMaxPositionAge: 10 * time.Minute,
		SweepInterval:           time.Second,
	}
	if tune != nil {
		tune(&cfg)
	}

	h := &harness{
		store:     newMemoryStore(),
		sender:    &recordingSender{},
		events:    &recordingEvents{},
		audit:     &recordingAudit{},
		publisher: &recordingPublisher{},
		device: &devices.Device{
			ID:               uuid.New(),
			IMEI:             "869247061234567",
			Protocol:         gt06.Name,
			CommandOverrides: map[string]string{},
		},
	}

	h.service = NewService(
		h.store,
		protocols.NewRegistry(gt06.New(false)),
		h.sender,
		fixedTelemetry{snapshot: snapshot},
		h.events, h.audit, h.publisher,
		cfg, telemetry.NewMetrics(),
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	return h
}

func stoppedVehicle() Snapshot {
	return Snapshot{HasPosition: true, SpeedKmh: 0, Timestamp: time.Now()}
}

// ---------------------------------------------------------------------------
// Regra de segurança do corte de motor (§14)
// ---------------------------------------------------------------------------

func TestEngineCutAllowedWhenVehicleIsStopped(t *testing.T) {
	h := newHarness(t, stoppedVehicle(), nil)

	cmd, err := h.service.Send(context.Background(), Request{
		Device: h.device, Type: protocols.CommandEngineCut,
	})
	if err != nil {
		t.Fatalf("corte deveria ser permitido: %v", err)
	}
	if cmd.Status != StatusSent {
		t.Fatalf("esperava SENT, recebi %q (%s)", cmd.Status, cmd.Error)
	}
	if h.sender.count() != 1 {
		t.Fatalf("esperava 1 envio, houve %d", h.sender.count())
	}
	if h.sender.lastIME != h.device.IMEI {
		t.Fatalf("enviado para o IMEI errado: %q", h.sender.lastIME)
	}

	// O pacote precisa carregar a mesma chave gravada no registro, senão o
	// ACK não casa com o comando (§16).
	flag, text, err := gt06.DecodeServerCommand(h.sender.last())
	if err != nil {
		t.Fatalf("pacote gerado é inválido: %v", err)
	}
	if flag != cmd.CorrelationKey {
		t.Fatalf("chave de correlação divergente: pacote %d, registro %d", flag, cmd.CorrelationKey)
	}
	if text != "DYD#" {
		t.Fatalf("texto do comando: %q", text)
	}

	if !h.events.has(events.EngineCutRequested) || !h.events.has(events.EngineCutSent) {
		t.Fatalf("faltou evento de corte: %v", h.events.types)
	}
	if !contains(h.audit.actions(), audit.ActionCommandRequested) ||
		!contains(h.audit.actions(), audit.ActionCommandSent) {
		t.Fatalf("auditoria incompleta: %v", h.audit.actions())
	}
}

func TestEngineCutRejectedWhenMoving(t *testing.T) {
	h := newHarness(t, Snapshot{
		HasPosition: true, SpeedKmh: 42, Timestamp: time.Now(),
	}, nil)

	cmd, err := h.service.Send(context.Background(), Request{
		Device: h.device, Type: protocols.CommandEngineCut,
	})
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("esperava recusa, recebi %v", err)
	}
	if cmd.Status != StatusRejected {
		t.Fatalf("esperava REJECTED, recebi %q", cmd.Status)
	}
	if !strings.Contains(cmd.Error, "42") {
		t.Fatalf("o motivo deveria citar a velocidade: %q", cmd.Error)
	}

	// O ponto central: nada chega ao veículo.
	if h.sender.count() != 0 {
		t.Fatal("nenhum byte pode ser enviado quando o corte é recusado")
	}
	if !contains(h.audit.actions(), audit.ActionCommandRejected) {
		t.Fatalf("a recusa precisa ser auditada: %v", h.audit.actions())
	}
}

func TestEngineCutRejectedWithoutPosition(t *testing.T) {
	h := newHarness(t, Snapshot{HasPosition: false}, nil)

	cmd, err := h.service.Send(context.Background(), Request{
		Device: h.device, Type: protocols.CommandEngineCut,
	})
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("esperava recusa, recebi %v", err)
	}
	if cmd.Status != StatusRejected || h.sender.count() != 0 {
		t.Fatal("sem posição conhecida o corte não pode sair")
	}
}

func TestEngineCutRejectedWithStalePosition(t *testing.T) {
	h := newHarness(t, Snapshot{
		HasPosition: true, SpeedKmh: 0, Timestamp: time.Now().Add(-2 * time.Hour),
	}, nil)

	cmd, err := h.service.Send(context.Background(), Request{
		Device: h.device, Type: protocols.CommandEngineCut,
	})
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("esperava recusa, recebi %v", err)
	}
	if !strings.Contains(cmd.Error, "última posição") {
		t.Fatalf("o motivo deveria citar a idade da posição: %q", cmd.Error)
	}
	if h.sender.count() != 0 {
		t.Fatal("posição velha não autoriza corte")
	}
}

// A checagem sem envio diz o motivo com um código: é com ele que o painel e o
// app decidem pedir uma posição nova antes do corte.
func TestCheckEngineCutExplainsWhy(t *testing.T) {
	cases := []struct {
		name     string
		snapshot Snapshot
		allowed  bool
		code     string
		age      bool
	}{
		{"parado e recente", stoppedVehicle(), true, "", true},
		{"sem posição", Snapshot{HasPosition: false}, false, CutNoPosition, false},
		{"posição antiga", Snapshot{HasPosition: true, Timestamp: time.Now().Add(-42 * time.Minute)}, false, CutStalePosition, true},
		{"andando", Snapshot{HasPosition: true, SpeedKmh: 40, Timestamp: time.Now()}, false, CutTooFast, true},
	}
	for _, c := range cases {
		h := newHarness(t, c.snapshot, nil)
		got := h.service.CheckEngineCut(context.Background(), h.device)
		if got.Allowed != c.allowed || got.Code != c.code || (got.PositionAgeSeconds != nil) != c.age ||
			got.MaxPositionAgeSeconds != 600 || (c.allowed == (got.Reason != "")) {
			t.Errorf("%s: %+v", c.name, got)
		}
		if h.sender.count() != 0 || len(h.store.commands) != 0 {
			t.Errorf("%s: a checagem não envia nem grava nada", c.name)
		}
	}
}

func TestEngineCutLimitIsConfigurable(t *testing.T) {
	// Com o limite em 60 km/h, 42 km/h passa — o que prova que a regra usa a
	// configuração e não um número fixo no código.
	h := newHarness(t, Snapshot{HasPosition: true, SpeedKmh: 42, Timestamp: time.Now()},
		func(cfg *config.Commands) { cfg.EngineCutMaxSpeedKmh = 60 })

	cmd, err := h.service.Send(context.Background(), Request{
		Device: h.device, Type: protocols.CommandEngineCut,
	})
	if err != nil {
		t.Fatalf("com limite de 60 km/h o corte deveria passar: %v", err)
	}
	if cmd.Status != StatusSent {
		t.Fatalf("esperava SENT, recebi %q", cmd.Status)
	}
}

func TestEngineResumeIsNotBlockedBySpeed(t *testing.T) {
	// Liberar o motor nunca é perigoso; a trava vale só para o corte.
	h := newHarness(t, Snapshot{HasPosition: true, SpeedKmh: 120, Timestamp: time.Now()}, nil)

	cmd, err := h.service.Send(context.Background(), Request{
		Device: h.device, Type: protocols.CommandEngineResume,
	})
	if err != nil {
		t.Fatalf("liberação deveria passar: %v", err)
	}
	if cmd.Status != StatusSent {
		t.Fatalf("esperava SENT, recebi %q", cmd.Status)
	}
}

func TestOtherCommandsDoNotRequirePosition(t *testing.T) {
	h := newHarness(t, Snapshot{HasPosition: false}, nil)

	for _, cmdType := range []protocols.CommandType{
		protocols.CommandRequestPosition, protocols.CommandRequestStatus,
	} {
		cmd, err := h.service.Send(context.Background(), Request{
			Device: h.device, Type: cmdType,
		})
		if err != nil {
			t.Fatalf("%s: %v", cmdType, err)
		}
		if cmd.Status != StatusSent {
			t.Fatalf("%s: esperava SENT, recebi %q", cmdType, cmd.Status)
		}
	}
}

// ---------------------------------------------------------------------------
// Envio, ACK e timeout
// ---------------------------------------------------------------------------

func TestCommandFailsWhenDeviceIsNotConnected(t *testing.T) {
	h := newHarness(t, stoppedVehicle(), nil)
	h.sender.err = ErrNotConnected

	cmd, err := h.service.Send(context.Background(), Request{
		Device: h.device, Type: protocols.CommandEngineCut,
	})
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if cmd.Status != StatusFailed {
		t.Fatalf("esperava FAILED, recebi %q", cmd.Status)
	}
	if !strings.Contains(cmd.Error, "não está conectado") {
		t.Fatalf("motivo inesperado: %q", cmd.Error)
	}
}

func TestAckMatchesCommandByCorrelationKey(t *testing.T) {
	h := newHarness(t, stoppedVehicle(), nil)
	ctx := context.Background()

	first, err := h.service.Send(ctx, Request{Device: h.device, Type: protocols.CommandRequestStatus})
	if err != nil {
		t.Fatal(err)
	}
	second, err := h.service.Send(ctx, Request{Device: h.device, Type: protocols.CommandEngineCut})
	if err != nil {
		t.Fatal(err)
	}

	// Responde ao SEGUNDO comando: a chave precisa decidir qual foi.
	h.service.HandleAck(ctx, h.device, nil, second.CorrelationKey, "DYD=Success!", true)

	updated, _ := h.store.Get(ctx, second.ID)
	if updated.Status != StatusAcknowledged {
		t.Fatalf("o comando respondido deveria estar ACKNOWLEDGED, está %q", updated.Status)
	}
	if updated.Response != "DYD=Success!" {
		t.Fatalf("resposta não gravada: %q", updated.Response)
	}

	untouched, _ := h.store.Get(ctx, first.ID)
	if untouched.Status != StatusSent {
		t.Fatalf("o outro comando não podia ser alterado, está %q", untouched.Status)
	}
	if !h.events.has(events.EngineCutAck) {
		t.Fatalf("faltou o evento de confirmação do corte: %v", h.events.types)
	}
}

func TestAckWithFailureResponseMarksFailed(t *testing.T) {
	h := newHarness(t, stoppedVehicle(), nil)
	ctx := context.Background()

	cmd, err := h.service.Send(ctx, Request{Device: h.device, Type: protocols.CommandEngineCut})
	if err != nil {
		t.Fatal(err)
	}

	h.service.HandleAck(ctx, h.device, nil, cmd.CorrelationKey, "DYD=Fail! Speed too high", false)

	updated, _ := h.store.Get(ctx, cmd.ID)
	if updated.Status != StatusFailed {
		t.Fatalf("esperava FAILED, recebi %q", updated.Status)
	}
}

func TestAckWithoutCorrelationFallsBackToOldestOpen(t *testing.T) {
	// Protocolos de texto não carregam chave: a chave 0 força o fallback.
	h := newHarness(t, stoppedVehicle(), nil)
	ctx := context.Background()

	first, err := h.service.Send(ctx, Request{Device: h.device, Type: protocols.CommandRequestStatus})
	if err != nil {
		t.Fatal(err)
	}

	h.service.HandleAck(ctx, h.device, nil, 0, "OK", true)

	updated, _ := h.store.Get(ctx, first.ID)
	if updated.Status != StatusAcknowledged {
		t.Fatalf("esperava ACKNOWLEDGED, recebi %q", updated.Status)
	}
	// O registro precisa deixar claro que a correlação foi por ordem, não exata.
	if !strings.Contains(updated.Response, "correlação por ordem de envio") {
		t.Fatalf("a resposta deveria marcar a correlação aproximada: %q", updated.Response)
	}
}

func TestSweepTimeoutsClosesUnansweredCommands(t *testing.T) {
	h := newHarness(t, stoppedVehicle(), func(cfg *config.Commands) {
		cfg.AckTimeout = -time.Second // já nasce vencido
	})
	ctx := context.Background()

	cmd, err := h.service.Send(ctx, Request{Device: h.device, Type: protocols.CommandEngineCut})
	if err != nil {
		t.Fatal(err)
	}

	h.service.SweepTimeouts(ctx)

	updated, _ := h.store.Get(ctx, cmd.ID)
	if updated.Status != StatusTimeout {
		t.Fatalf("esperava TIMEOUT, recebi %q", updated.Status)
	}
}

func TestDeviceOverrideChangesPayload(t *testing.T) {
	h := newHarness(t, stoppedVehicle(), nil)
	// Firmware que usa RELAY em vez de DYD: resolve-se no cadastro, sem
	// recompilar nada.
	h.device.CommandOverrides["ENGINE_CUT"] = "RELAY,1#"

	cmd, err := h.service.Send(context.Background(), Request{
		Device: h.device, Type: protocols.CommandEngineCut,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cmd.Payload, "RELAY,1#") {
		t.Fatalf("o override deveria aparecer no payload auditado: %q", cmd.Payload)
	}

	_, text, err := gt06.DecodeServerCommand(h.sender.last())
	if err != nil {
		t.Fatal(err)
	}
	if text != "RELAY,1#" {
		t.Fatalf("o aparelho deveria receber o override, recebeu %q", text)
	}
}

func TestUnknownCommandTypeIsRefused(t *testing.T) {
	h := newHarness(t, stoppedVehicle(), nil)

	if _, err := h.service.Send(context.Background(), Request{
		Device: h.device, Type: protocols.CommandType("DROP_TABLE"),
	}); err == nil {
		t.Fatal("comando fora do domínio deveria ser recusado")
	}
	if h.sender.count() != 0 {
		t.Fatal("nada pode ser enviado para um comando desconhecido")
	}
}

func TestDeviceWithoutProtocolIsRefused(t *testing.T) {
	h := newHarness(t, stoppedVehicle(), nil)
	h.device.Protocol = ""

	if _, err := h.service.Send(context.Background(), Request{
		Device: h.device, Type: protocols.CommandRequestStatus,
	}); err == nil {
		t.Fatal("sem protocolo definido não há como montar o comando")
	}
}

func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
