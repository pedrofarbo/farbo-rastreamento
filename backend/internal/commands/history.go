package commands

import (
	"context"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// O rastreio de bloqueio e desbloqueio: os cortes e as religações do
// veículo, com quem pediu (nome, perfil e a conta de cliente a que pertence),
// de qual aparelho e como confirmou.

// maxUserAgent: o navegador guardado no comando (os de verdade têm ~150).
const maxUserAgent = 400

func truncateUA(ua string) string {
	if len(ua) > maxUserAgent {
		return ua[:maxUserAgent]
	}
	return ua
}

// EngineEntry é um bloqueio ou desbloqueio, com quem pediu.
type EngineEntry struct {
	*Command
	// RequesterName e RequesterRole: quem pediu (vazios: sem usuário).
	RequesterName string
	RequesterRole string
	// RequesterAccount: a conta de cliente de quem pediu (a dele, ou a do
	// titular, para quem é da equipe de uma conta).
	RequesterAccount *uuid.UUID
}

// EngineHistory traz os últimos bloqueios e desbloqueios do rastreador
// (recusados e sem resposta também), do mais novo.
func (r *Repository) EngineHistory(ctx context.Context, deviceID uuid.UUID, limit int) ([]*EngineEntry, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	rows, err := r.db.Query(ctx, `
		SELECT c.id, c.device_id, c.command, COALESCE(c.payload, ''), c.status, c.correlation_key,
			c.requested_by, c.requested_at, c.user_agent, c.confirmed_with, c.sent_at, c.acknowledged_at, c.timeout_at,
			COALESCE(c.response, ''), COALESCE(c.error, ''), c.created_at, COALESCE(u.name, ''), COALESCE(u.role, ''),
			CASE WHEN u.role = 'customer' THEN COALESCE(m.account_id, u.id) END
		FROM device_commands c
		LEFT JOIN users u ON u.id = c.requested_by
		LEFT JOIN account_members m ON m.user_id = u.id
		WHERE c.device_id = $1 AND c.command IN ('ENGINE_CUT', 'ENGINE_RESUME')
		ORDER BY c.requested_at DESC LIMIT $2`, deviceID, limit)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	var out []*EngineEntry
	for rows.Next() {
		var c Command
		var correlation int64
		e := &EngineEntry{Command: &c}
		if err := rows.Scan(&c.ID, &c.DeviceID, &c.Command, &c.Payload, &c.Status, &correlation,
			&c.RequestedBy, &c.RequestedAt, &c.UserAgent, &c.ConfirmedWith, &c.SentAt, &c.AcknowledgedAt, &c.TimeoutAt,
			&c.Response, &c.Error, &c.CreatedAt, &e.RequesterName, &e.RequesterRole, &e.RequesterAccount); err != nil {
			return nil, database.MapError(err)
		}
		c.CorrelationKey = uint32(correlation)
		out = append(out, e)
	}
	return out, database.MapError(rows.Err())
}
