// Package commands trata o ciclo de vida dos comandos enviados ao rastreador:
// pedido, validação de segurança, envio, ACK, timeout e auditoria.
package commands

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/devices"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/protocols"
)

// Estados de um comando (§13).
const (
	StatusPending      = "PENDING"
	StatusSending      = "SENDING"
	StatusSent         = "SENT"
	StatusAcknowledged = "ACKNOWLEDGED"
	StatusFailed       = "FAILED"
	StatusTimeout      = "TIMEOUT"
	StatusRejected     = "REJECTED"
)

// Terminal diz se o estado é final.
func Terminal(status string) bool {
	switch status {
	case StatusAcknowledged, StatusFailed, StatusTimeout, StatusRejected:
		return true
	}
	return false
}

type Command struct {
	ID       uuid.UUID `json:"id"`
	DeviceID uuid.UUID `json:"deviceId"`

	Command string `json:"command"`
	// Payload é o texto enviado ao aparelho, com as credenciais (senha de
	// comando e senha APN) trocadas por *** — o pacote real só vai para o
	// socket do rastreador; nada que é gravado ou exibido carrega a senha.
	Payload string `json:"payload"`

	Status string `json:"status"`

	// CorrelationKey viaja dentro do pacote quando o protocolo permite,
	// tornando o casamento do ACK exato em vez de heurístico (§16).
	CorrelationKey uint32 `json:"correlationKey"`

	RequestedBy *uuid.UUID `json:"requestedBy"`
	RequestedAt time.Time  `json:"requestedAt"`
	// UserAgent é o navegador da sessão que pediu (de qual aparelho saiu);
	// ConfirmedWith, como a pessoa confirmou (biometric ou password; vazio
	// para a central e os comandos sem confirmação).
	UserAgent     string `json:"userAgent"`
	ConfirmedWith string `json:"confirmedWith"`

	SentAt         *time.Time `json:"sentAt"`
	AcknowledgedAt *time.Time `json:"acknowledgedAt"`
	TimeoutAt      *time.Time `json:"timeoutAt"`

	Response string `json:"response"`
	Error    string `json:"error"`

	CreatedAt time.Time `json:"createdAt"`
}

// Redact troca as credenciais do aparelho por *** no texto enviado e na
// resposta recebida.
func (c *Command) Redact(secrets []string) {
	c.Payload = devices.RedactText(c.Payload, secrets)
	c.Response = devices.RedactText(c.Response, secrets)
	c.Error = devices.RedactText(c.Error, secrets)
}

const columns = `id, device_id, command, COALESCE(payload, ''), status, correlation_key,
	requested_by, requested_at, user_agent, confirmed_with, sent_at, acknowledged_at, timeout_at,
	COALESCE(response, ''), COALESCE(error, ''), created_at`

type Repository struct{ db *database.DB }

func NewRepository(db *database.DB) *Repository { return &Repository{db: db} }

func scan(row database.Scanner) (*Command, error) {
	var c Command
	var correlation int64
	err := row.Scan(&c.ID, &c.DeviceID, &c.Command, &c.Payload, &c.Status, &correlation,
		&c.RequestedBy, &c.RequestedAt, &c.UserAgent, &c.ConfirmedWith, &c.SentAt, &c.AcknowledgedAt, &c.TimeoutAt,
		&c.Response, &c.Error, &c.CreatedAt)
	if err != nil {
		return nil, database.MapError(err)
	}
	c.CorrelationKey = uint32(correlation)
	return &c, nil
}

func (r *Repository) Create(ctx context.Context, c *Command) error {
	return database.MapError(r.db.QueryRow(ctx, `
		INSERT INTO device_commands (device_id, command, payload, status, correlation_key, requested_by, user_agent, confirmed_with)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, requested_at, created_at`,
		c.DeviceID, c.Command, c.Payload, c.Status, int64(c.CorrelationKey), c.RequestedBy, c.UserAgent, c.ConfirmedWith,
	).Scan(&c.ID, &c.RequestedAt, &c.CreatedAt))
}

func (r *Repository) Get(ctx context.Context, id uuid.UUID) (*Command, error) {
	return scan(r.db.QueryRow(ctx, `SELECT `+columns+` FROM device_commands WHERE id = $1`, id))
}

func (r *Repository) ListByDevice(ctx context.Context, deviceID uuid.UUID, limit int) ([]*Command, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.db.Query(ctx, `SELECT `+columns+` FROM device_commands
		WHERE device_id = $1 ORDER BY created_at DESC LIMIT $2`, deviceID, limit)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()

	out := []*Command{}
	for rows.Next() {
		c, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// MarkSending trava o comando para envio, evitando envio duplicado.
func (r *Repository) MarkSending(ctx context.Context, id uuid.UUID) (*Command, error) {
	return scan(r.db.QueryRow(ctx, `
		UPDATE device_commands SET status = 'SENDING'
		WHERE id = $1 AND status = 'PENDING'
		RETURNING `+columns, id))
}

func (r *Repository) MarkSent(ctx context.Context, id uuid.UUID, timeoutAt time.Time) (*Command, error) {
	return scan(r.db.QueryRow(ctx, `
		UPDATE device_commands SET status = 'SENT', sent_at = NOW(), timeout_at = $2
		WHERE id = $1 AND status IN ('PENDING', 'SENDING')
		RETURNING `+columns, id, timeoutAt))
}

func (r *Repository) MarkAcknowledged(ctx context.Context, id uuid.UUID, response string) (*Command, error) {
	return scan(r.db.QueryRow(ctx, `
		UPDATE device_commands SET status = 'ACKNOWLEDGED', acknowledged_at = NOW(), response = $2
		WHERE id = $1 AND status IN ('PENDING', 'SENDING', 'SENT')
		RETURNING `+columns, id, response))
}

// MarkFinal aplica um estado final de falha (FAILED, TIMEOUT ou REJECTED).
func (r *Repository) MarkFinal(ctx context.Context, id uuid.UUID, status, reason string) (*Command, error) {
	return scan(r.db.QueryRow(ctx, `
		UPDATE device_commands SET status = $2, error = $3
		WHERE id = $1 AND status NOT IN ('ACKNOWLEDGED', 'FAILED', 'TIMEOUT', 'REJECTED')
		RETURNING `+columns, id, status, reason))
}

// FindOpenByCorrelation localiza o comando aberto com a chave informada.
func (r *Repository) FindOpenByCorrelation(ctx context.Context, deviceID uuid.UUID, key uint32) (*Command, error) {
	return scan(r.db.QueryRow(ctx, `
		SELECT `+columns+` FROM device_commands
		WHERE device_id = $1 AND correlation_key = $2
		  AND status IN ('PENDING', 'SENDING', 'SENT')
		ORDER BY created_at DESC LIMIT 1`, deviceID, int64(key)))
}

// FindOldestOpen é o caminho para protocolos de texto, que não carregam chave
// de correlação: casa a resposta com o comando aberto mais antigo.
func (r *Repository) FindOldestOpen(ctx context.Context, deviceID uuid.UUID) (*Command, error) {
	return scan(r.db.QueryRow(ctx, `
		SELECT `+columns+` FROM device_commands
		WHERE device_id = $1 AND status IN ('SENDING', 'SENT')
		ORDER BY created_at LIMIT 1`, deviceID))
}

// ExpireTimedOut marca como TIMEOUT os comandos que passaram do prazo (§16).
func (r *Repository) ExpireTimedOut(ctx context.Context) ([]*Command, error) {
	rows, err := r.db.Query(ctx, `
		UPDATE device_commands
		SET status = 'TIMEOUT', error = 'rastreador não respondeu dentro do prazo'
		WHERE status IN ('SENDING', 'SENT') AND timeout_at IS NOT NULL AND timeout_at < NOW()
		RETURNING `+columns)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()

	out := []*Command{}
	for rows.Next() {
		c, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// IsEngineCommand diz se o comando aciona o relé — usados para eventos e para
// a regra de segurança.
func IsEngineCommand(cmd string) bool {
	return cmd == string(protocols.CommandEngineCut) || cmd == string(protocols.CommandEngineResume)
}
