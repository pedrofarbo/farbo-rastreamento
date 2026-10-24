// Package events registra os acontecimentos derivados da telemetria e das ações
// do operador.
package events

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// Tipos de evento (§11).
const (
	IgnitionOn  = "IGNITION_ON"
	IgnitionOff = "IGNITION_OFF"

	Overspeed    = "OVERSPEED"
	OverspeedEnd = "OVERSPEED_END"

	GeofenceEnter = "GEOFENCE_ENTER"
	GeofenceExit  = "GEOFENCE_EXIT"

	Vibration  = "VIBRATION"
	PowerLoss  = "POWER_LOSS"
	LowBattery = "LOW_BATTERY"
	SOS        = "SOS"

	// VehicleBatteryLow: com a ignição desligada, a tensão da bateria do
	// veículo caiu abaixo do que costuma dar partida (ver tracking).
	VehicleBatteryLow = "VEHICLE_BATTERY_LOW"

	GPSLost      = "GPS_LOST"
	GPSRecovered = "GPS_RECOVERED"
	GSMLost      = "GSM_LOST"
	GSMRecovered = "GSM_RECOVERED"

	EngineCutRequested = "ENGINE_CUT_REQUESTED"
	EngineCutSent      = "ENGINE_CUT_SENT"
	EngineCutAck       = "ENGINE_CUT_ACK"

	EngineResumeRequested = "ENGINE_RESUME_REQUESTED"
	EngineResumeSent      = "ENGINE_RESUME_SENT"
	EngineResumeAck       = "ENGINE_RESUME_ACK"

	DeviceConnected    = "DEVICE_CONNECTED"
	DeviceDisconnected = "DEVICE_DISCONNECTED"
	DeviceStale        = "DEVICE_STALE"

	// Alarme reportado pelo aparelho sem mapeamento canônico. Os detalhes
	// ficam em metadata, sem tentativa de interpretação (§38).
	DeviceAlarm = "DEVICE_ALARM"
)

type Event struct {
	ID        int64     `json:"id"`
	DeviceID  uuid.UUID `json:"deviceId"`
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`

	Latitude  *float64 `json:"latitude"`
	Longitude *float64 `json:"longitude"`
	SpeedKmh  *float64 `json:"speedKmh"`

	Metadata map[string]any `json:"metadata"`

	CreatedAt time.Time `json:"createdAt"`
}

const columns = `id, device_id, type, timestamp, latitude, longitude, speed_kmh, metadata, created_at`

type Repository struct{ db *database.DB }

func NewRepository(db *database.DB) *Repository { return &Repository{db: db} }

func scan(row database.Scanner) (*Event, error) {
	var e Event
	err := row.Scan(&e.ID, &e.DeviceID, &e.Type, &e.Timestamp, &e.Latitude, &e.Longitude,
		&e.SpeedKmh, &e.Metadata, &e.CreatedAt)
	if err != nil {
		return nil, database.MapError(err)
	}
	if e.Metadata == nil {
		e.Metadata = map[string]any{}
	}
	return &e, nil
}

func (r *Repository) Create(ctx context.Context, e *Event) error {
	if e.Metadata == nil {
		e.Metadata = map[string]any{}
	}
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}
	return database.MapError(r.db.QueryRow(ctx, `
		INSERT INTO vehicle_events (device_id, type, timestamp, latitude, longitude, speed_kmh, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at`,
		e.DeviceID, e.Type, e.Timestamp, e.Latitude, e.Longitude, e.SpeedKmh, e.Metadata,
	).Scan(&e.ID, &e.CreatedAt))
}

type Query struct {
	DeviceID uuid.UUID
	From     time.Time
	To       time.Time
	Types    []string
	Limit    int
}

func (r *Repository) List(ctx context.Context, q Query) ([]*Event, error) {
	if q.Limit <= 0 || q.Limit > 1000 {
		q.Limit = 200
	}
	sql := `SELECT ` + columns + ` FROM vehicle_events
		WHERE device_id = $1 AND timestamp >= $2 AND timestamp <= $3`
	args := []any{q.DeviceID, q.From, q.To}

	if len(q.Types) > 0 {
		args = append(args, q.Types)
		sql += ` AND type = ANY($4)`
	}
	args = append(args, q.Limit)
	sql += ` ORDER BY timestamp DESC LIMIT $` + strconv.Itoa(len(args))

	rows, err := r.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()

	out := []*Event{}
	for rows.Next() {
		e, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ListRecent devolve os últimos eventos de todos os dispositivos.
func (r *Repository) ListRecent(ctx context.Context, limit int) ([]*Event, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.db.Query(ctx,
		`SELECT `+columns+` FROM vehicle_events ORDER BY timestamp DESC LIMIT $1`, limit)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()

	out := []*Event{}
	for rows.Next() {
		e, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Publisher entrega o evento ao WebSocket. Implementado por websocket.Hub.
// O rastreador vai no envelope para o hub entregar o evento só a quem pode
// vê-lo.
type Publisher interface {
	PublishFor(eventType string, vehicleID, deviceID *uuid.UUID, data any)
}

type Service struct {
	repo      *Repository
	publisher Publisher
	log       *slog.Logger

	// observer recebe cada evento gravado (os alertas por e-mail). Precisa
	// voltar na hora: roda dentro da ingestão.
	observer func(*Event)
}

func NewService(repo *Repository, publisher Publisher, log *slog.Logger) *Service {
	return &Service{repo: repo, publisher: publisher, log: log.With("component", "events")}
}

// SetObserver registra quem recebe cada evento gravado. Chame antes de a
// ingestão começar.
func (s *Service) SetObserver(fn func(*Event)) { s.observer = fn }

// Record grava o evento e o publica no WebSocket.
//
// A falha ao gravar é registrada mas não interrompe a ingestão: perder um
// evento é ruim, parar de receber posições é pior.
func (s *Service) Record(ctx context.Context, e *Event) {
	if err := s.repo.Create(ctx, e); err != nil {
		s.log.Error("falha ao gravar evento", "type", e.Type, "device", e.DeviceID, "err", err)
		return
	}
	if s.publisher != nil {
		s.publisher.PublishFor("vehicle.event", nil, &e.DeviceID, e)
	}
	if s.observer != nil {
		s.observer(e)
	}
}

func (s *Service) List(ctx context.Context, q Query) ([]*Event, error) {
	return s.repo.List(ctx, q)
}

func (s *Service) ListRecent(ctx context.Context, limit int) ([]*Event, error) {
	return s.repo.ListRecent(ctx, limit)
}
