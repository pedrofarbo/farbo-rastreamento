package alerts

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// Status de um alerta no histórico.
const (
	StatusPending    = "PENDING"
	StatusSent       = "SENT"
	StatusFailed     = "FAILED"
	StatusSuppressed = "SUPPRESSED"
)

// Motivos para segurar um alerta.
const (
	ReasonCooldown  = "cooldown"
	ReasonHourlyCap = "hourly_cap"
)

// Target é o veículo de um rastreador e o dono dele.
type Target struct {
	VehicleID   *uuid.UUID
	VehicleName string
	Plate       string
	Owner       *Owner
}

// Owner é o cliente dono do veículo.
type Owner struct {
	UserID    uuid.UUID
	Email     string
	Name      string
	Active    bool
	Suspended bool
	Settings  Settings
}

// Notification é uma linha do histórico.
type Notification struct {
	ID              int64      `json:"id"`
	Recipient       string     `json:"-"`
	UserID          *uuid.UUID `json:"-"`
	VehicleID       *uuid.UUID `json:"vehicleId"`
	VehicleName     string     `json:"vehicleName"`
	Plate           string     `json:"plate"`
	DeviceID        *uuid.UUID `json:"-"`
	Kind            string     `json:"kind"`
	EventID         *int64     `json:"-"`
	OccurredAt      time.Time  `json:"occurredAt"`
	Status          string     `json:"status"`
	Reason          string     `json:"-"`
	SuppressedCount int        `json:"suppressedCount"`
	// PushSent: em quantos celulares o alerta chegou como notificação.
	PushSent int `json:"pushSent"`
	// Detail completa o tipo no histórico: o nome da cerca.
	Detail    string     `json:"detail"`
	Error     string     `json:"-"`
	CreatedAt time.Time  `json:"createdAt"`
	SentAt    *time.Time `json:"sentAt"`
}

// Store é o que o motor precisa do banco.
type Store interface {
	// Target resolve veículo e dono de um rastreador. Rastreador sem veículo
	// devolve nil.
	Target(ctx context.Context, deviceID uuid.UUID) (*Target, error)
	// LastSent é o último alerta enviado (ou na fila) desse tipo, desse
	// rastreador, para esse destinatário.
	LastSent(ctx context.Context, recipient string, deviceID uuid.UUID, kind string) (time.Time, bool, error)
	// CountSentSince conta os e-mails de alerta de um destinatário desde then.
	CountSentSince(ctx context.Context, recipient string, since time.Time) (int, error)
	// CountSuppressedSinceLastSent conta as ocorrências seguradas desde o
	// último envio do mesmo alerta: elas entram como resumo no próximo e-mail.
	CountSuppressedSinceLastSent(ctx context.Context, recipient string, deviceID uuid.UUID, kind string) (int, error)
	Insert(ctx context.Context, n *Notification) error
	MarkSent(ctx context.Context, id int64, at time.Time) error
	MarkFailed(ctx context.Context, id int64, reason string) error
	// MarkPushed registra em quantos celulares o alerta chegou.
	MarkPushed(ctx context.Context, id int64, count int) error
	// DeviceOffline diz se o rastreador continua sem comunicação.
	DeviceOffline(ctx context.Context, deviceID uuid.UUID) (bool, error)
	// Prune apaga o histórico mais velho que before.
	Prune(ctx context.Context, before time.Time) (int64, error)
	// LastTest é o último e-mail de teste do usuário.
	LastTest(ctx context.Context, userID uuid.UUID) (time.Time, bool, error)
}

// SuspendedFunc diz se o cliente está com o acesso suspenso por atraso.
type SuspendedFunc func(ctx context.Context, customerID uuid.UUID) (bool, error)

// DBStore implementa Store no PostgreSQL.
type DBStore struct {
	db        *database.DB
	suspended SuspendedFunc
}

func NewDBStore(db *database.DB, suspended SuspendedFunc) *DBStore {
	return &DBStore{db: db, suspended: suspended}
}

func (s *DBStore) Target(ctx context.Context, deviceID uuid.UUID) (*Target, error) {
	var (
		t          Target
		vehicleID  uuid.UUID
		ownerID    *uuid.UUID
		email      *string
		name       *string
		active     *bool
		kinds      []string
		guardStart *int
		guardEnd   *int
	)
	err := s.db.QueryRow(ctx, `
		SELECT v.id, v.name, COALESCE(v.plate, ''), v.owner_id, u.email, u.name, u.active,
		       st.kinds, st.guard_start, st.guard_end
		FROM vehicles v
		LEFT JOIN users u ON u.id = v.owner_id
		LEFT JOIN alert_settings st ON st.user_id = v.owner_id
		WHERE v.device_id = $1`, deviceID,
	).Scan(&vehicleID, &t.VehicleName, &t.Plate, &ownerID, &email, &name, &active, &kinds, &guardStart, &guardEnd)
	if errors.Is(database.MapError(err), database.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, database.MapError(err)
	}
	t.VehicleID = &vehicleID
	if ownerID == nil || email == nil {
		return &t, nil
	}

	owner := &Owner{UserID: *ownerID, Email: *email, Name: deref(name), Active: active != nil && *active}
	owner.Settings = settingsFromRow(kinds, guardStart, guardEnd)
	if s.suspended != nil {
		if owner.Suspended, err = s.suspended(ctx, *ownerID); err != nil {
			return nil, err
		}
	}
	t.Owner = owner
	return &t, nil
}

func settingsFromRow(kinds []string, guardStart, guardEnd *int) Settings {
	if kinds == nil || guardStart == nil || guardEnd == nil {
		return DefaultSettings()
	}
	s := Settings{Kinds: map[string]bool{}, GuardStart: *guardStart, GuardEnd: *guardEnd, Custom: true}
	for _, k := range kinds {
		s.Kinds[k] = true
	}
	return s
}

func deref(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func (s *DBStore) LastSent(ctx context.Context, recipient string, deviceID uuid.UUID, kind string) (time.Time, bool, error) {
	var at time.Time
	err := s.db.QueryRow(ctx, `
		SELECT created_at FROM alert_notifications
		WHERE recipient = $1 AND device_id = $2 AND kind = $3 AND status IN ('PENDING', 'SENT')
		ORDER BY created_at DESC LIMIT 1`, recipient, deviceID, kind).Scan(&at)
	if errors.Is(database.MapError(err), database.ErrNotFound) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, database.MapError(err)
	}
	return at, true, nil
}

func (s *DBStore) CountSentSince(ctx context.Context, recipient string, since time.Time) (int, error) {
	var n int
	err := s.db.QueryRow(ctx, `
		SELECT count(*) FROM alert_notifications
		WHERE recipient = $1 AND created_at >= $2 AND status IN ('PENDING', 'SENT') AND kind <> 'TEST'`,
		recipient, since).Scan(&n)
	return n, database.MapError(err)
}

func (s *DBStore) CountSuppressedSinceLastSent(ctx context.Context, recipient string, deviceID uuid.UUID, kind string) (int, error) {
	var n int
	err := s.db.QueryRow(ctx, `
		SELECT count(*) FROM alert_notifications
		WHERE recipient = $1 AND device_id = $2 AND kind = $3 AND status = 'SUPPRESSED'
		  AND created_at > COALESCE((
		      SELECT max(created_at) FROM alert_notifications
		      WHERE recipient = $1 AND device_id = $2 AND kind = $3 AND status IN ('PENDING', 'SENT')
		  ), '-infinity')`, recipient, deviceID, kind).Scan(&n)
	return n, database.MapError(err)
}

func (s *DBStore) Insert(ctx context.Context, n *Notification) error {
	return database.MapError(s.db.QueryRow(ctx, `
		INSERT INTO alert_notifications
		    (recipient, user_id, vehicle_id, device_id, kind, event_id, occurred_at, status, reason, suppressed_count)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id, created_at`,
		n.Recipient, n.UserID, n.VehicleID, n.DeviceID, n.Kind, n.EventID, n.OccurredAt, n.Status, n.Reason,
		n.SuppressedCount,
	).Scan(&n.ID, &n.CreatedAt))
}

func (s *DBStore) MarkSent(ctx context.Context, id int64, at time.Time) error {
	_, err := s.db.Exec(ctx, `UPDATE alert_notifications SET status = 'SENT', sent_at = $2, error = '' WHERE id = $1`, id, at)
	return database.MapError(err)
}

func (s *DBStore) MarkFailed(ctx context.Context, id int64, reason string) error {
	_, err := s.db.Exec(ctx, `UPDATE alert_notifications SET status = 'FAILED', error = $2 WHERE id = $1`, id, reason)
	return database.MapError(err)
}

func (s *DBStore) MarkPushed(ctx context.Context, id int64, count int) error {
	_, err := s.db.Exec(ctx, `UPDATE alert_notifications SET push_sent = $2 WHERE id = $1`, id, count)
	return database.MapError(err)
}

func (s *DBStore) DeviceOffline(ctx context.Context, deviceID uuid.UUID) (bool, error) {
	var status string
	err := s.db.QueryRow(ctx, `SELECT status FROM devices WHERE id = $1`, deviceID).Scan(&status)
	if errors.Is(database.MapError(err), database.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, database.MapError(err)
	}
	return status == "OFFLINE", nil
}

func (s *DBStore) Prune(ctx context.Context, before time.Time) (int64, error) {
	tag, err := s.db.Exec(ctx, `DELETE FROM alert_notifications WHERE created_at < $1`, before)
	if err != nil {
		return 0, database.MapError(err)
	}
	return tag.RowsAffected(), nil
}

// --- Preferências e histórico (tela de alertas) --------------------------

// LoadSettings devolve as preferências do usuário (padrões se nunca salvou).
func (s *DBStore) LoadSettings(ctx context.Context, userID uuid.UUID) (Settings, error) {
	var (
		kinds      []string
		guardStart int
		guardEnd   int
	)
	err := s.db.QueryRow(ctx, `SELECT kinds, guard_start, guard_end FROM alert_settings WHERE user_id = $1`, userID).
		Scan(&kinds, &guardStart, &guardEnd)
	if errors.Is(database.MapError(err), database.ErrNotFound) {
		return DefaultSettings(), nil
	}
	if err != nil {
		return Settings{}, database.MapError(err)
	}
	return settingsFromRow(kinds, &guardStart, &guardEnd), nil
}

// SaveSettings grava as preferências do usuário.
func (s *DBStore) SaveSettings(ctx context.Context, userID uuid.UUID, st Settings) error {
	_, err := s.db.Exec(ctx, `
		INSERT INTO alert_settings (user_id, kinds, guard_start, guard_end)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id) DO UPDATE SET kinds = EXCLUDED.kinds, guard_start = EXCLUDED.guard_start,
		    guard_end = EXCLUDED.guard_end, updated_at = NOW()`,
		userID, st.EnabledList(), st.GuardStart, st.GuardEnd)
	return database.MapError(err)
}

// History devolve os últimos alertas enviados (ou que falharam) ao usuário.
func (s *DBStore) History(ctx context.Context, userID uuid.UUID, limit int) ([]*Notification, error) {
	rows, err := s.db.Query(ctx, `
		SELECT n.id, n.vehicle_id, COALESCE(v.name, ''), COALESCE(v.plate, ''),
		       split_part(n.kind, ':', 1), COALESCE(g.name, ''), n.occurred_at,
		       n.status, n.suppressed_count, n.push_sent, n.created_at, n.sent_at
		FROM alert_notifications n
		LEFT JOIN vehicles v ON v.id = n.vehicle_id
		-- Os alertas de cerca guardam o id dela depois de ":" (intervalo
		-- próprio por cerca); a tela recebe o tipo e o nome.
		LEFT JOIN geofences g ON n.kind LIKE 'GEOFENCE%:%' AND g.id::text = split_part(n.kind, ':', 2)
		WHERE n.user_id = $1 AND n.status IN ('PENDING', 'SENT', 'FAILED')
		ORDER BY n.created_at DESC
		LIMIT $2`, userID, limit)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	out := []*Notification{}
	for rows.Next() {
		n := &Notification{}
		if err := rows.Scan(&n.ID, &n.VehicleID, &n.VehicleName, &n.Plate, &n.Kind, &n.Detail, &n.OccurredAt,
			&n.Status, &n.SuppressedCount, &n.PushSent, &n.CreatedAt, &n.SentAt); err != nil {
			return nil, database.MapError(err)
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// LastTest é o último e-mail de teste do usuário (limite de um por minuto).
func (s *DBStore) LastTest(ctx context.Context, userID uuid.UUID) (time.Time, bool, error) {
	var at time.Time
	err := s.db.QueryRow(ctx, `
		SELECT created_at FROM alert_notifications WHERE user_id = $1 AND kind = 'TEST'
		ORDER BY created_at DESC LIMIT 1`, userID).Scan(&at)
	if errors.Is(database.MapError(err), database.ErrNotFound) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, database.MapError(err)
	}
	return at, true, nil
}
