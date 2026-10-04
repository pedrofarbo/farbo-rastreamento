package devices

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

const columns = `id, imei, COALESCE(model, ''), COALESCE(manufacturer, ''), COALESCE(protocol, ''),
	COALESCE(firmware, ''), COALESCE(phone_number, ''), status, last_seen_at,
	COALESCE(apn, ''), COALESCE(apn_user, ''), COALESCE(apn_password, ''),
	COALESCE(server_host, ''), server_port, report_interval_seconds, heartbeat_interval_seconds,
	COALESCE(command_password, ''), command_overrides, COALESCE(notes, ''), created_at, updated_at`

type Repository struct {
	db      *database.DB
	touchDB *database.DB
}

func NewRepository(db *database.DB) *Repository { return &Repository{db: db, touchDB: db} }

// UseTelemetry manda o "último contato" (uma gravação a cada ~15 s por
// rastreador) para o pool das gravações dos rastreadores.
func (r *Repository) UseTelemetry(db *database.DB) { r.touchDB = db }

func scan(row database.Scanner) (*Device, error) {
	var d Device
	err := row.Scan(
		&d.ID, &d.IMEI, &d.Model, &d.Manufacturer, &d.Protocol, &d.Firmware, &d.PhoneNumber,
		&d.Status, &d.LastSeenAt, &d.APN, &d.APNUser, &d.APNPassword, &d.ServerHost,
		&d.ServerPort, &d.ReportIntervalSeconds, &d.HeartbeatIntervalSeconds,
		&d.CommandPassword, &d.CommandOverrides, &d.Notes, &d.CreatedAt, &d.UpdatedAt,
	)
	if err != nil {
		return nil, database.MapError(err)
	}
	if d.CommandOverrides == nil {
		d.CommandOverrides = map[string]string{}
	}
	return &d, nil
}

func (r *Repository) Create(ctx context.Context, in Input) (*Device, error) {
	if in.CommandOverrides == nil {
		in.CommandOverrides = map[string]string{}
	}
	row := r.db.QueryRow(ctx, `
		INSERT INTO devices (imei, model, manufacturer, protocol, firmware, phone_number,
			apn, apn_user, apn_password, server_host, server_port,
			report_interval_seconds, heartbeat_interval_seconds,
			command_password, command_overrides, notes)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
		RETURNING `+columns,
		strings.TrimSpace(in.IMEI), in.Model, in.Manufacturer, in.Protocol, in.Firmware,
		in.PhoneNumber, in.APN, in.APNUser, in.APNPassword, in.ServerHost, in.ServerPort,
		in.ReportIntervalSeconds, in.HeartbeatIntervalSeconds,
		in.CommandPassword, in.CommandOverrides, in.Notes,
	)
	return scan(row)
}

func (r *Repository) Update(ctx context.Context, id uuid.UUID, in Input) (*Device, error) {
	if in.CommandOverrides == nil {
		in.CommandOverrides = map[string]string{}
	}
	row := r.db.QueryRow(ctx, `
		UPDATE devices SET imei = $2, model = $3, manufacturer = $4, protocol = $5,
			firmware = $6, phone_number = $7, apn = $8, apn_user = $9, apn_password = $10,
			server_host = $11, server_port = $12, report_interval_seconds = $13,
			heartbeat_interval_seconds = $14, command_password = $15,
			command_overrides = $16, notes = $17, updated_at = NOW()
		WHERE id = $1
		RETURNING `+columns,
		id, strings.TrimSpace(in.IMEI), in.Model, in.Manufacturer, in.Protocol, in.Firmware,
		in.PhoneNumber, in.APN, in.APNUser, in.APNPassword, in.ServerHost, in.ServerPort,
		in.ReportIntervalSeconds, in.HeartbeatIntervalSeconds,
		in.CommandPassword, in.CommandOverrides, in.Notes,
	)
	return scan(row)
}

func (r *Repository) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.db.Exec(ctx, `DELETE FROM devices WHERE id = $1`, id)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return database.ErrNotFound
	}
	return nil
}

func (r *Repository) GetByID(ctx context.Context, id uuid.UUID) (*Device, error) {
	return scan(r.db.QueryRow(ctx, `SELECT `+columns+` FROM devices WHERE id = $1`, id))
}

func (r *Repository) GetByIMEI(ctx context.Context, imei string) (*Device, error) {
	return scan(r.db.QueryRow(ctx, `SELECT `+columns+` FROM devices WHERE imei = $1`,
		strings.TrimSpace(imei)))
}

func (r *Repository) List(ctx context.Context) ([]*Device, error) {
	return r.list(ctx, `SELECT `+columns+` FROM devices ORDER BY created_at`)
}

// ListByIDs devolve só os rastreadores pedidos, sem ordem garantida.
func (r *Repository) ListByIDs(ctx context.Context, ids []uuid.UUID) ([]*Device, error) {
	if len(ids) == 0 {
		return []*Device{}, nil
	}
	return r.list(ctx, `SELECT `+columns+` FROM devices WHERE id = ANY($1)`, ids)
}

func (r *Repository) list(ctx context.Context, query string, args ...any) ([]*Device, error) {
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()

	out := []*Device{}
	for rows.Next() {
		d, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// Touch registra o instante do último pacote e coloca o dispositivo ONLINE.
//
// last_seen_at usa GREATEST para não retroceder quando o aparelho descarrega
// posições antigas guardadas offline (§35).
func (r *Repository) Touch(ctx context.Context, id uuid.UUID, seenAt time.Time) error {
	_, err := r.touchDB.Exec(ctx, `
		UPDATE devices
		SET last_seen_at = GREATEST(COALESCE(last_seen_at, $2), $2),
		    status = 'ONLINE', updated_at = NOW()
		WHERE id = $1`, id, seenAt)
	return database.MapError(err)
}

// SetProtocol grava o protocolo efetivamente detectado na conexão.
func (r *Repository) SetProtocol(ctx context.Context, id uuid.UUID, protocol string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE devices SET protocol = $2, updated_at = NOW()
		WHERE id = $1 AND COALESCE(protocol, '') IS DISTINCT FROM $2`, id, protocol)
	return database.MapError(err)
}

// SweepStatuses recalcula ONLINE/STALE/OFFLINE e devolve só quem mudou (§17).
func (r *Repository) SweepStatuses(ctx context.Context, stale, offline time.Duration) ([]StatusChange, error) {
	rows, err := r.db.Query(ctx, `
		WITH computed AS (
			SELECT id, status AS current_status,
				CASE
					WHEN last_seen_at IS NULL OR last_seen_at < NOW() - $2::interval THEN 'OFFLINE'
					WHEN last_seen_at < NOW() - $1::interval THEN 'STALE'
					ELSE 'ONLINE'
				END AS next_status
			FROM devices
		)
		UPDATE devices d
		SET status = c.next_status, updated_at = NOW()
		FROM computed c
		WHERE d.id = c.id AND d.status <> c.next_status
		RETURNING d.id, d.imei, c.current_status, c.next_status`,
		stale.String(), offline.String())
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()

	out := []StatusChange{}
	for rows.Next() {
		var c StatusChange
		if err := rows.Scan(&c.DeviceID, &c.IMEI, &c.From, &c.To); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *Repository) CountOnline(ctx context.Context) (int, error) {
	var n int
	err := r.db.QueryRow(ctx, `SELECT count(*) FROM devices WHERE status = 'ONLINE'`).Scan(&n)
	return n, database.MapError(err)
}
