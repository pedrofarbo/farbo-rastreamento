package commands

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/devices"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/events"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/protocols"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/telemetry"
)

// ErrRejected indica recusa por regra de segurança (§14). O comando fica
// gravado com status REJECTED para a auditoria.
var ErrRejected = errors.New("comando recusado pela regra de segurança")

// ErrNotConnected indica que não há sessão TCP aberta com o aparelho.
var ErrNotConnected = errors.New("dispositivo não está conectado")

// Sender escreve bytes na sessão do rastreador. Implementado por tcp.Manager.
type Sender interface {
	Send(imei string, payload []byte) error
}

// Snapshot é o mínimo de telemetria que a regra de segurança precisa.
// Definido aqui (e não importado de tracking) para manter a dependência
// numa direção só.
type Snapshot struct {
	HasPosition bool
	SpeedKmh    float64
	Timestamp   time.Time
}

// TelemetryProvider entrega o snapshot mais recente do dispositivo.
type TelemetryProvider interface {
	Snapshot(ctx context.Context, deviceID uuid.UUID) (Snapshot, error)
}

// Publisher entrega atualizações ao WebSocket.
type Publisher interface {
	PublishFor(eventType string, vehicleID, deviceID *uuid.UUID, data any)
}

// Store é o que o serviço precisa do repositório. Declarar a interface aqui
// (e não usar o tipo concreto) é o que permite testar a regra de segurança do
// corte de motor sem subir um Postgres.
type Store interface {
	Create(ctx context.Context, cmd *Command) error
	Get(ctx context.Context, id uuid.UUID) (*Command, error)
	ListByDevice(ctx context.Context, deviceID uuid.UUID, limit int) ([]*Command, error)
	MarkSending(ctx context.Context, id uuid.UUID) (*Command, error)
	MarkSent(ctx context.Context, id uuid.UUID, timeoutAt time.Time) (*Command, error)
	MarkAcknowledged(ctx context.Context, id uuid.UUID, response string) (*Command, error)
	MarkFinal(ctx context.Context, id uuid.UUID, status, reason string) (*Command, error)
	FindOpenByCorrelation(ctx context.Context, deviceID uuid.UUID, key uint32) (*Command, error)
	FindOldestOpen(ctx context.Context, deviceID uuid.UUID) (*Command, error)
	ExpireTimedOut(ctx context.Context) ([]*Command, error)
}

// EventRecorder e AuditRecorder são as fatias que o serviço usa de events e
// audit — nada além de registrar.
type EventRecorder interface {
	Record(ctx context.Context, e *events.Event)
}

type AuditRecorder interface {
	Record(ctx context.Context, e *audit.Entry)
}

type Service struct {
	repo      Store
	registry  *protocols.ProtocolRegistry
	sender    Sender
	telemetry TelemetryProvider
	eventSvc  EventRecorder
	auditSvc  AuditRecorder
	publisher Publisher
	cfg       config.Commands
	metrics   *telemetry.Metrics
	log       *slog.Logger
}

func NewService(
	repo Store,
	registry *protocols.ProtocolRegistry,
	sender Sender,
	telemetryProvider TelemetryProvider,
	eventSvc EventRecorder,
	auditSvc AuditRecorder,
	publisher Publisher,
	cfg config.Commands,
	metrics *telemetry.Metrics,
	log *slog.Logger,
) *Service {
	return &Service{
		repo: repo, registry: registry, sender: sender, telemetry: telemetryProvider,
		eventSvc: eventSvc, auditSvc: auditSvc, publisher: publisher, cfg: cfg,
		metrics: metrics, log: log.With("component", "commands"),
	}
}

// Request descreve o pedido vindo da API.
type Request struct {
	Device    *devices.Device
	VehicleID *uuid.UUID
	Type      protocols.CommandType
	Params    map[string]string
	// RawOverride só é aceito no comando CUSTOM.
	RawOverride string
	UserID      *uuid.UUID
	IP          string
}

// Send executa o fluxo completo do §14: valida, grava, envia e audita.
//
// O relé nunca é acionado direto pelo frontend: o pedido chega aqui como
// intenção e é o backend que decide, com a última posição conhecida em mãos.
func (s *Service) Send(ctx context.Context, req Request) (*Command, error) {
	if !req.Type.Valid() {
		return nil, fmt.Errorf("comando desconhecido: %q", req.Type)
	}

	proto, ok := s.registry.ByName(req.Device.Protocol)
	if !ok {
		return nil, fmt.Errorf("dispositivo sem protocolo definido; conecte-o ao menos uma vez ou defina o protocolo no cadastro")
	}

	// 1. Regra de segurança, antes de qualquer byte ser gerado.
	if req.Type == protocols.CommandEngineCut {
		if reason := s.engineCutBlocked(ctx, req.Device); reason != "" {
			return s.reject(ctx, req, reason)
		}
	}

	// 2. Tradução para o dialeto do aparelho. A chave de correlação é sorteada
	// antes da codificação porque ela viaja dentro do pacote e precisa ser
	// exatamente a mesma gravada no registro, senão o ACK não casa (§16).
	key := newCorrelationKey()
	payload, err := proto.EncodeCommand(protocols.Command{
		Type:           req.Type,
		UniqueID:       req.Device.IMEI,
		CorrelationKey: key,
		Password:       req.Device.CommandPassword,
		Params:         req.Params,
		Raw:            s.rawFor(req),
	})
	if err != nil {
		return nil, fmt.Errorf("protocolo %s: %w", proto.Name(), err)
	}

	// O aparelho recebe o pacote real; o registro, a auditoria e o
	// WebSocket recebem a versão com as credenciais trocadas por ***.
	cmd := &Command{
		DeviceID:       req.Device.ID,
		Command:        string(req.Type),
		Payload:        printable(devices.RedactBytes(payload, req.Device.Secrets())),
		Status:         StatusPending,
		CorrelationKey: key,
		RequestedBy:    req.UserID,
	}
	if err := s.repo.Create(ctx, cmd); err != nil {
		return nil, err
	}

	s.auditSvc.Record(ctx, &audit.Entry{
		UserID: req.UserID, Action: audit.ActionCommandRequested,
		VehicleID: req.VehicleID, DeviceID: &req.Device.ID,
		Result: StatusPending, IPAddress: req.IP,
		Metadata: map[string]any{
			"command":   cmd.Command,
			"payload":   cmd.Payload,
			"commandId": cmd.ID,
			"protocol":  proto.Name(),
		},
	})
	s.recordEngineEvent(ctx, cmd, requestedEvent(cmd.Command))

	// 3. Envio.
	return s.dispatch(ctx, req, cmd, payload)
}

func (s *Service) rawFor(req Request) string {
	if req.Type == protocols.CommandCustom {
		return req.RawOverride
	}
	return req.Device.CommandOverrides[string(req.Type)]
}

func (s *Service) dispatch(ctx context.Context, req Request, cmd *Command, payload []byte) (*Command, error) {
	locked, err := s.repo.MarkSending(ctx, cmd.ID)
	if err != nil {
		return nil, err
	}
	cmd = locked

	if err := s.sender.Send(req.Device.IMEI, payload); err != nil {
		reason := err.Error()
		if errors.Is(err, ErrNotConnected) {
			reason = "dispositivo não está conectado"
		}
		failed, markErr := s.repo.MarkFinal(ctx, cmd.ID, StatusFailed, reason)
		if markErr != nil {
			return nil, markErr
		}
		s.metrics.CommandsFailed.WithLabelValues(cmd.Command, StatusFailed).Inc()
		s.publish("command.failed", req.VehicleID, &failed.DeviceID, failed)
		s.log.Warn("falha ao enviar comando", "command", cmd.Command,
			"imei", telemetry.IMEI(req.Device.IMEI), "err", err)
		return failed, nil
	}

	sent, err := s.repo.MarkSent(ctx, cmd.ID, time.Now().Add(s.cfg.AckTimeout))
	if err != nil {
		return nil, err
	}

	s.metrics.CommandsSent.WithLabelValues(cmd.Command).Inc()
	s.auditSvc.Record(ctx, &audit.Entry{
		UserID: req.UserID, Action: audit.ActionCommandSent,
		VehicleID: req.VehicleID, DeviceID: &req.Device.ID,
		Result: StatusSent, IPAddress: req.IP,
		Metadata: map[string]any{"command": sent.Command, "commandId": sent.ID},
	})
	s.recordEngineEvent(ctx, sent, sentEvent(sent.Command))
	s.publish("command.sent", req.VehicleID, &sent.DeviceID, sent)

	s.log.Info("comando enviado ao rastreador", "command", sent.Command,
		"imei", telemetry.IMEI(req.Device.IMEI), "commandId", sent.ID)
	return sent, nil
}

// Por que o corte de motor seria recusado agora (EngineCutCheck.Code).
const (
	CutReadFailed    = "READ_FAILED"
	CutNoPosition    = "NO_POSITION"
	CutStalePosition = "STALE_POSITION"
	CutTooFast       = "TOO_FAST"
)

// EngineCutCheck é a regra de segurança do corte avaliada agora, sem enviar
// nada. Posição ausente ou antiga se resolve pedindo uma nova ao rastreador;
// velocidade alta, só esperando o veículo parar.
type EngineCutCheck struct {
	Allowed bool   `json:"allowed"`
	Code    string `json:"code,omitempty"`
	Reason  string `json:"reason,omitempty"`
	// PositionAgeSeconds é a idade da última posição (nula sem posição).
	PositionAgeSeconds *int `json:"positionAgeSeconds"`
	// MaxPositionAgeSeconds é o limite (ENGINE_CUT_MAX_POSITION_AGE).
	MaxPositionAgeSeconds int `json:"maxPositionAgeSeconds"`
}

// CheckEngineCut avalia a regra de segurança do corte.
//
// A checagem é feita sempre no backend, com o dado e o relógio do backend —
// nunca confiando no que o navegador afirma (§14).
func (s *Service) CheckEngineCut(ctx context.Context, dev *devices.Device) EngineCutCheck {
	check := EngineCutCheck{MaxPositionAgeSeconds: int(s.cfg.EngineCutMaxPositionAge.Seconds())}
	refuse := func(code, reason string) EngineCutCheck {
		check.Code, check.Reason = code, reason
		return check
	}

	snapshot, err := s.telemetry.Snapshot(ctx, dev.ID)
	if err != nil {
		return refuse(CutReadFailed, "não foi possível ler a última posição do veículo")
	}
	if !snapshot.HasPosition {
		return refuse(CutNoPosition, "não há posição conhecida para este veículo")
	}

	age := time.Since(snapshot.Timestamp)
	seconds := int(age.Seconds())
	check.PositionAgeSeconds = &seconds
	if age > s.cfg.EngineCutMaxPositionAge {
		return refuse(CutStalePosition, fmt.Sprintf(
			"última posição tem %s, acima do limite de %s: não é possível garantir que o veículo está parado",
			age.Round(time.Second), s.cfg.EngineCutMaxPositionAge))
	}
	if snapshot.SpeedKmh > s.cfg.EngineCutMaxSpeedKmh {
		return refuse(CutTooFast, fmt.Sprintf(
			"veículo a %.1f km/h, acima do limite de segurança de %.1f km/h",
			snapshot.SpeedKmh, s.cfg.EngineCutMaxSpeedKmh))
	}
	check.Allowed = true
	return check
}

// engineCutBlocked devolve o motivo da recusa, ou vazio se o corte é seguro.
func (s *Service) engineCutBlocked(ctx context.Context, dev *devices.Device) string {
	return s.CheckEngineCut(ctx, dev).Reason
}

func (s *Service) reject(ctx context.Context, req Request, reason string) (*Command, error) {
	cmd := &Command{
		DeviceID:       req.Device.ID,
		Command:        string(req.Type),
		Status:         StatusRejected,
		CorrelationKey: newCorrelationKey(),
		RequestedBy:    req.UserID,
		Error:          reason,
	}
	if err := s.repo.Create(ctx, cmd); err != nil {
		return nil, err
	}
	// O motivo entra no registro criado acima via MarkFinal, que também
	// mantém o estado consistente caso o INSERT tenha usado outro status.
	rejected, err := s.repo.MarkFinal(ctx, cmd.ID, StatusRejected, reason)
	if err != nil {
		rejected = cmd
	}

	s.metrics.CommandsFailed.WithLabelValues(rejected.Command, StatusRejected).Inc()
	s.auditSvc.Record(ctx, &audit.Entry{
		UserID: req.UserID, Action: audit.ActionCommandRejected,
		VehicleID: req.VehicleID, DeviceID: &req.Device.ID,
		Result: StatusRejected, IPAddress: req.IP,
		Metadata: map[string]any{
			"command":   rejected.Command,
			"reason":    reason,
			"commandId": rejected.ID,
		},
	})
	s.publish("command.failed", req.VehicleID, &rejected.DeviceID, rejected)
	s.log.Warn("corte de motor recusado", "device", req.Device.ID, "reason", reason)

	return rejected, fmt.Errorf("%w: %s", ErrRejected, reason)
}

// HandleAck casa a resposta do rastreador com o comando correspondente (§16).
//
// Protocolos com chave de correlação casam pelo valor exato. Os de texto, que
// não carregam identificador, casam com o comando aberto mais antigo — e isso
// fica explícito no registro.
//
// Há firmware que ecoa o comando recebido, senha inclusa: a resposta é
// redigida antes de ir para o registro, a auditoria, o log e o WebSocket.
func (s *Service) HandleAck(ctx context.Context, dev *devices.Device, vehicleID *uuid.UUID, key uint32, response string, success bool) {
	var cmd *Command
	var err error
	deviceID := dev.ID
	exact := key != 0
	response = devices.RedactText(response, dev.Secrets())

	if exact {
		cmd, err = s.repo.FindOpenByCorrelation(ctx, deviceID, key)
	}
	if !exact || err != nil {
		cmd, err = s.repo.FindOldestOpen(ctx, deviceID)
	}
	if err != nil || cmd == nil {
		s.log.Debug("resposta sem comando correspondente",
			"device", deviceID, "correlation", key, "response", response)
		return
	}

	if !exact {
		response = "[correlação por ordem de envio] " + response
	}

	var updated *Command
	if success {
		updated, err = s.repo.MarkAcknowledged(ctx, cmd.ID, response)
	} else {
		updated, err = s.repo.MarkFinal(ctx, cmd.ID, StatusFailed, response)
	}
	if err != nil {
		s.log.Error("falha ao atualizar comando com o ACK", "commandId", cmd.ID, "err", err)
		return
	}

	if updated.SentAt != nil {
		s.metrics.CommandLatency.WithLabelValues(updated.Command).
			Observe(time.Since(*updated.SentAt).Seconds())
	}
	if !success {
		s.metrics.CommandsFailed.WithLabelValues(updated.Command, StatusFailed).Inc()
	}

	s.auditSvc.Record(ctx, &audit.Entry{
		UserID: updated.RequestedBy, Action: audit.ActionCommandResult,
		VehicleID: vehicleID, DeviceID: &deviceID, Result: updated.Status,
		Metadata: map[string]any{
			"command":    updated.Command,
			"commandId":  updated.ID,
			"response":   response,
			"exactMatch": exact,
		},
	})
	s.recordEngineEvent(ctx, updated, ackEvent(updated.Command))

	topic := "command.acknowledged"
	if !success {
		topic = "command.failed"
	}
	s.publish(topic, vehicleID, &updated.DeviceID, updated)

	s.log.Info("resposta do rastreador associada ao comando",
		"commandId", updated.ID, "status", updated.Status)
}

// SweepTimeouts roda periodicamente e fecha os comandos sem resposta.
func (s *Service) SweepTimeouts(ctx context.Context) {
	expired, err := s.repo.ExpireTimedOut(ctx)
	if err != nil {
		s.log.Error("falha ao expirar comandos", "err", err)
		return
	}
	for _, cmd := range expired {
		s.metrics.CommandsFailed.WithLabelValues(cmd.Command, StatusTimeout).Inc()
		s.auditSvc.Record(ctx, &audit.Entry{
			UserID: cmd.RequestedBy, Action: audit.ActionCommandResult,
			DeviceID: &cmd.DeviceID, Result: StatusTimeout,
			Metadata: map[string]any{"command": cmd.Command, "commandId": cmd.ID},
		})
		s.publish("command.failed", nil, &cmd.DeviceID, cmd)
		s.log.Warn("comando expirou sem resposta", "commandId", cmd.ID, "command", cmd.Command)
	}
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (*Command, error) {
	return s.repo.Get(ctx, id)
}

// ListByDevice devolve o histórico do aparelho já pronto para sair na API.
//
// Os registros são gravados redigidos; a redação é refeita aqui com as
// credenciais atuais para cobrir o que um backend antigo tenha gravado (a
// migration 0012 limpa o histórico, mas uma instância anterior ainda no ar
// durante a troca de versão pode gravar depois dela).
func (s *Service) ListByDevice(ctx context.Context, dev *devices.Device, limit int) ([]*Command, error) {
	list, err := s.repo.ListByDevice(ctx, dev.ID, limit)
	if err != nil {
		return nil, err
	}
	secrets := dev.Secrets()
	for _, cmd := range list {
		cmd.Redact(secrets)
	}
	return list, nil
}

func (s *Service) publish(topic string, vehicleID, deviceID *uuid.UUID, data any) {
	if s.publisher != nil {
		s.publisher.PublishFor(topic, vehicleID, deviceID, data)
	}
}

func (s *Service) recordEngineEvent(ctx context.Context, cmd *Command, eventType string) {
	if eventType == "" {
		return
	}
	s.eventSvc.Record(ctx, &events.Event{
		DeviceID:  cmd.DeviceID,
		Type:      eventType,
		Timestamp: time.Now().UTC(),
		Metadata: map[string]any{
			"commandId": cmd.ID,
			"command":   cmd.Command,
			"status":    cmd.Status,
		},
	})
}

func requestedEvent(cmd string) string {
	switch protocols.CommandType(cmd) {
	case protocols.CommandEngineCut:
		return events.EngineCutRequested
	case protocols.CommandEngineResume:
		return events.EngineResumeRequested
	}
	return ""
}

func sentEvent(cmd string) string {
	switch protocols.CommandType(cmd) {
	case protocols.CommandEngineCut:
		return events.EngineCutSent
	case protocols.CommandEngineResume:
		return events.EngineResumeSent
	}
	return ""
}

func ackEvent(cmd string) string {
	switch protocols.CommandType(cmd) {
	case protocols.CommandEngineCut:
		return events.EngineCutAck
	case protocols.CommandEngineResume:
		return events.EngineResumeAck
	}
	return ""
}

// newCorrelationKey sorteia uma chave não nula de 32 bits.
func newCorrelationKey() uint32 {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return uint32(time.Now().UnixNano())
	}
	key := binary.BigEndian.Uint32(buf[:])
	if key == 0 {
		return 1
	}
	return key
}

// printable transforma o payload em texto legível para a auditoria, sem
// esconder bytes binários.
func printable(payload []byte) string {
	out := make([]byte, 0, len(payload))
	for _, b := range payload {
		if b >= 0x20 && b <= 0x7E {
			out = append(out, b)
			continue
		}
		out = append(out, []byte(fmt.Sprintf("\\x%02X", b))...)
	}
	return string(out)
}
