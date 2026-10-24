// Package appointments é a agenda de instalação e de desinstalação: a central
// marca o dia e a hora e manda o link ao instalador. Pelo link (sem login: o
// id assinado), o instalador segue o passo a passo no celular. Na
// instalação — chegou, o IMEI conferido, o teste do sinal ao vivo, as fotos —
// a conclusão liga o rastreador ao veículo; na desinstalação — chegou, a foto
// do rastreador retirado — tira o rastreador do veículo (e a associação paga
// a taxa de desinstalação na fatura do mês).
package appointments

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/finance"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/mail"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/push"
)

// Os tipos.
const (
	KindInstall   = "install"
	KindUninstall = "uninstall"
)

// As situações.
const (
	StatusScheduled = "scheduled"
	StatusArrived   = "arrived"
	StatusDone      = "done"
	StatusCanceled  = "canceled"
	StatusNoShow    = "no_show"
)

// Slots são as fotos da instalação.
var Slots = []string{"tracker", "vehicle", "dashboard"}

const (
	// MaxPhotoBytes: a foto do celular (a página reduz antes).
	MaxPhotoBytes = 6 << 20
	// linkGrace: depois de concluída ou cancelada, o link ainda abre (só
	// para ver) por este tempo.
	linkGrace = 24 * time.Hour
)

var (
	// ErrLinkEnded: o link não vale mais.
	ErrLinkEnded = errors.New("este link de instalação não está mais ativo")
	encoding     = base64.RawURLEncoding
)

// InputError é um pedido recusado; a mensagem vai para a tela.
type InputError struct{ Message string }

func (e *InputError) Error() string { return e.Message }

func invalid(format string, args ...any) error { return &InputError{fmt.Sprintf(format, args...)} }

// Appointment é uma instalação agendada.
type Appointment struct {
	ID               uuid.UUID  `json:"id"`
	Kind             string     `json:"kind"`
	VehicleID        uuid.UUID  `json:"vehicleId"`
	VehicleName      string     `json:"vehicleName"`
	Plate            string     `json:"plate"`
	VehicleDetails   string     `json:"vehicleDetails"`
	CustomerID       uuid.UUID  `json:"customerId"`
	CustomerName     string     `json:"customerName"`
	CustomerPhone    string     `json:"customerPhone"`
	OrganizationName string     `json:"organizationName"`
	InstallerID      *uuid.UUID `json:"installerId"`
	InstallerName    string     `json:"installerName"`
	ScheduledFor     time.Time  `json:"scheduledFor"`
	DurationMin      int        `json:"durationMin"`
	Address          string     `json:"address"`
	Notes            string     `json:"notes"`
	Status           string     `json:"status"`
	ArrivedAt        *time.Time `json:"arrivedAt"`
	DeviceID         *uuid.UUID `json:"deviceId"`
	// IMEI: o instalado (instalação concluída) ou o que está no veículo (desinstalação).
	IMEI          string     `json:"imei"`
	CompletedAt   *time.Time `json:"completedAt"`
	InstallerNote string     `json:"installerNote"`
	Photos        []string   `json:"photos"`
	CreatedAt     time.Time  `json:"createdAt"`
	// URL: o link do instalador (só para a central).
	URL string `json:"url,omitempty"`
}

// Input é o agendamento pedido.
type Input struct {
	// Kind: install (vazio) ou uninstall; não muda ao remarcar.
	Kind          string     `json:"kind"`
	VehicleID     uuid.UUID  `json:"vehicleId"`
	InstallerID   *uuid.UUID `json:"installerId"`
	InstallerName string     `json:"installerName"`
	ScheduledFor  time.Time  `json:"scheduledFor"`
	DurationMin   int        `json:"durationMin"`
	Address       string     `json:"address"`
	Notes         string     `json:"notes"`
}

// Pusher avisa o cliente no app (push.Service).
type Pusher interface {
	Notify(ctx context.Context, userID uuid.UUID, n push.Notification) (int, error)
}

type Service struct {
	db       *database.DB
	appURL   string
	key      []byte
	pusher   Pusher
	now      func() time.Time
	runAsync func(func())
	log      *slog.Logger
}

func NewService(db *database.DB, appURL string, secret []byte, log *slog.Logger) *Service {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte("farbo-install-link-v1"))
	return &Service{db: db, appURL: strings.TrimRight(appURL, "/"), key: mac.Sum(nil), now: time.Now,
		runAsync: func(f func()) { go f() }, log: log.With("component", "appointments")}
}

// SetPusher liga o aviso no app do cliente.
func (s *Service) SetPusher(p Pusher) { s.pusher = p }

// SetClock e SetSync: os testes.
func (s *Service) SetClock(now func() time.Time) { s.now = now }
func (s *Service) SetSync()                      { s.runAsync = func(f func()) { f() } }

// Token é a parte secreta do link do instalador.
func (s *Service) Token(id uuid.UUID) string {
	mac := hmac.New(sha256.New, s.key)
	mac.Write(id[:])
	return encoding.EncodeToString(append(id[:], mac.Sum(nil)[:16]...))
}

// PublicURL é o link do instalador.
func (s *Service) PublicURL(id uuid.UUID) string { return s.appURL + "/instalador/" + s.Token(id) }

const selectAppointment = `
	SELECT a.id, a.kind, a.vehicle_id, v.name, COALESCE(v.plate, ''),
		concat_ws(' · ', NULLIF(v.brand, ''), NULLIF(v.model, ''), v.year::text, NULLIF(v.color, '')),
		u.id, u.name, u.phone, COALESCE(o.name, ''), a.installer_id,
		COALESCE(NULLIF(a.installer_name, ''), i.name, ''), a.scheduled_for, a.duration_min, a.address, a.notes, a.status,
		a.arrived_at, a.device_id, COALESCE(d.imei, CASE WHEN a.kind = 'uninstall' THEN vd.imei END, ''), a.completed_at,
		a.installer_note,
		COALESCE((SELECT array_agg(p.slot ORDER BY p.slot) FROM installation_photos p WHERE p.appointment_id = a.id), '{}'),
		a.created_at
	FROM installation_appointments a
	JOIN vehicles v ON v.id = a.vehicle_id
	JOIN users u ON u.id = v.owner_id
	LEFT JOIN users o ON o.id = u.organization_id
	LEFT JOIN installers i ON i.id = a.installer_id
	LEFT JOIN devices d ON d.id = a.device_id
	LEFT JOIN devices vd ON vd.id = v.device_id`

func scan(row database.Scanner) (*Appointment, error) {
	var a Appointment
	if err := row.Scan(&a.ID, &a.Kind, &a.VehicleID, &a.VehicleName, &a.Plate, &a.VehicleDetails, &a.CustomerID, &a.CustomerName,
		&a.CustomerPhone, &a.OrganizationName, &a.InstallerID, &a.InstallerName, &a.ScheduledFor, &a.DurationMin, &a.Address,
		&a.Notes, &a.Status, &a.ArrivedAt, &a.DeviceID, &a.IMEI, &a.CompletedAt, &a.InstallerNote, &a.Photos,
		&a.CreatedAt); err != nil {
		return nil, database.MapError(err)
	}
	return &a, nil
}

func (s *Service) list(ctx context.Context, where string, args ...any) ([]*Appointment, error) {
	rows, err := s.db.Query(ctx, selectAppointment+` WHERE `+where+` ORDER BY a.scheduled_for LIMIT 1000`, args...)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	out := []*Appointment{}
	for rows.Next() {
		a, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, database.MapError(rows.Err())
}

// Between é a agenda da central no período [from, to), com o link de cada um.
func (s *Service) Between(ctx context.Context, from, to time.Time) ([]*Appointment, error) {
	list, err := s.list(ctx, `a.scheduled_for >= $1 AND a.scheduled_for < $2`, from, to)
	for _, a := range list {
		a.URL = s.PublicURL(a.ID)
	}
	return list, err
}

// ForCustomer são as instalações em aberto dos veículos do cliente.
func (s *Service) ForCustomer(ctx context.Context, customerID uuid.UUID) ([]*Appointment, error) {
	return s.list(ctx, `v.owner_id = $1 AND a.status IN ('scheduled', 'arrived')`, customerID)
}

// ForOrganization é a agenda dos veículos dos associados da organização.
func (s *Service) ForOrganization(ctx context.Context, orgID uuid.UUID, from, to time.Time) ([]*Appointment, error) {
	return s.list(ctx, `u.organization_id = $1 AND a.scheduled_for >= $2 AND a.scheduled_for < $3`, orgID, from, to)
}

// Get é o agendamento (com o link do instalador).
func (s *Service) Get(ctx context.Context, id uuid.UUID) (*Appointment, error) {
	a, err := scan(s.db.QueryRow(ctx, selectAppointment+` WHERE a.id = $1`, id))
	if err != nil {
		return nil, err
	}
	a.URL = s.PublicURL(a.ID)
	return a, nil
}

// Awaiting é um veículo que espera a instalação (sem rastreador ligado).
type Awaiting struct {
	VehicleID        uuid.UUID  `json:"vehicleId"`
	VehicleName      string     `json:"vehicleName"`
	Plate            string     `json:"plate"`
	CustomerID       uuid.UUID  `json:"customerId"`
	CustomerName     string     `json:"customerName"`
	CustomerPhone    string     `json:"customerPhone"`
	OrganizationName string     `json:"organizationName"`
	Address          string     `json:"address"`
	ScheduledFor     *time.Time `json:"scheduledFor"`
	CreatedAt        time.Time  `json:"createdAt"`
}

// AwaitingInstall são os veículos de cliente sem rastreador, com o endereço
// de entrega do cliente e o agendamento em aberto (se houver).
func (s *Service) AwaitingInstall(ctx context.Context) ([]Awaiting, error) {
	rows, err := s.db.Query(ctx, `
		SELECT v.id, v.name, COALESCE(v.plate, ''), u.id, u.name, u.phone, COALESCE(o.name, ''),
			COALESCE((SELECT concat_ws(', ', ad.street || ' ' || ad.number, NULLIF(ad.complement, ''), ad.district,
				ad.city || '/' || ad.state)
				FROM customer_addresses ad WHERE ad.customer_id = u.id), ''),
			(SELECT a.scheduled_for FROM installation_appointments a WHERE a.vehicle_id = v.id
				AND a.status IN ('scheduled', 'arrived') LIMIT 1),
			v.created_at
		FROM vehicles v JOIN users u ON u.id = v.owner_id LEFT JOIN users o ON o.id = u.organization_id
		WHERE v.device_id IS NULL AND u.active AND u.role = 'customer'
			-- O rastreador retirado na desinstalação não volta para a fila.
			AND COALESCE((SELECT x.kind FROM installation_appointments x WHERE x.vehicle_id = v.id AND x.status = 'done'
				ORDER BY x.completed_at DESC LIMIT 1), 'install') <> 'uninstall'
		ORDER BY v.created_at LIMIT 500`)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	out := []Awaiting{}
	for rows.Next() {
		var a Awaiting
		if err := rows.Scan(&a.VehicleID, &a.VehicleName, &a.Plate, &a.CustomerID, &a.CustomerName, &a.CustomerPhone,
			&a.OrganizationName, &a.Address, &a.ScheduledFor, &a.CreatedAt); err != nil {
			return nil, database.MapError(err)
		}
		out = append(out, a)
	}
	return out, database.MapError(rows.Err())
}

// AwaitingUninstall é um veículo de associado que espera a retirada do
// rastreador: a associação pediu, ou o associado foi desligado.
type AwaitingUninstall struct {
	VehicleID        uuid.UUID `json:"vehicleId"`
	VehicleName      string    `json:"vehicleName"`
	Plate            string    `json:"plate"`
	IMEI             string    `json:"imei"`
	CustomerID       uuid.UUID `json:"customerId"`
	CustomerName     string    `json:"customerName"`
	CustomerPhone    string    `json:"customerPhone"`
	OrganizationName string    `json:"organizationName"`
	Address          string    `json:"address"`
	// Reason: requested (a associação pediu) ou inactive (o associado foi desligado).
	Reason       string     `json:"reason"`
	Note         string     `json:"note"`
	RequestedAt  *time.Time `json:"requestedAt"`
	ScheduledFor *time.Time `json:"scheduledFor"`
}

// AwaitingUninstall são os veículos com rastreador de associado desligado ou
// com o pedido de desinstalação da associação.
func (s *Service) AwaitingUninstall(ctx context.Context) ([]AwaitingUninstall, error) {
	rows, err := s.db.Query(ctx, `
		SELECT v.id, v.name, COALESCE(v.plate, ''), d.imei, u.id, u.name, u.phone, o.name,
			COALESCE((SELECT concat_ws(', ', ad.street || ' ' || ad.number, NULLIF(ad.complement, ''), ad.district,
				ad.city || '/' || ad.state)
				FROM customer_addresses ad WHERE ad.customer_id = u.id), ''),
			CASE WHEN v.uninstall_requested_at IS NOT NULL THEN 'requested' ELSE 'inactive' END,
			v.uninstall_note, v.uninstall_requested_at,
			(SELECT a.scheduled_for FROM installation_appointments a WHERE a.vehicle_id = v.id
				AND a.status IN ('scheduled', 'arrived') LIMIT 1)
		FROM vehicles v JOIN devices d ON d.id = v.device_id JOIN users u ON u.id = v.owner_id
		JOIN users o ON o.id = u.organization_id
		WHERE v.uninstall_requested_at IS NOT NULL OR NOT u.active
		ORDER BY COALESCE(v.uninstall_requested_at, u.updated_at) LIMIT 500`)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	out := []AwaitingUninstall{}
	for rows.Next() {
		var a AwaitingUninstall
		if err := rows.Scan(&a.VehicleID, &a.VehicleName, &a.Plate, &a.IMEI, &a.CustomerID, &a.CustomerName, &a.CustomerPhone,
			&a.OrganizationName, &a.Address, &a.Reason, &a.Note, &a.RequestedAt, &a.ScheduledFor); err != nil {
			return nil, database.MapError(err)
		}
		out = append(out, a)
	}
	return out, database.MapError(rows.Err())
}

// UninstallRequest é o pedido de desinstalação em aberto de um veículo.
type UninstallRequest struct {
	RequestedAt time.Time `json:"requestedAt"`
	Note        string    `json:"note"`
}

// UninstallRequests são os pedidos em aberto dos veículos do cliente (pelo id do veículo).
func (s *Service) UninstallRequests(ctx context.Context, ownerID uuid.UUID) (map[uuid.UUID]UninstallRequest, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, uninstall_requested_at, uninstall_note FROM vehicles
		WHERE owner_id = $1 AND uninstall_requested_at IS NOT NULL`, ownerID)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	out := map[uuid.UUID]UninstallRequest{}
	for rows.Next() {
		var id uuid.UUID
		var r UninstallRequest
		if err := rows.Scan(&id, &r.RequestedAt, &r.Note); err != nil {
			return nil, database.MapError(err)
		}
		out[id] = r
	}
	return out, database.MapError(rows.Err())
}

// RequestUninstall: a associação pede a retirada do rastreador do veículo
// do associado (o veículo entra na fila da central). note: o motivo.
func (s *Service) RequestUninstall(ctx context.Context, orgID, vehicleID uuid.UUID, note string) error {
	note = strings.TrimSpace(note)
	if utf8.RuneCountInString(note) > 300 {
		return invalid("o motivo vai até 300 caracteres")
	}
	tag, err := s.db.Exec(ctx, `
		UPDATE vehicles v SET uninstall_requested_at = COALESCE(v.uninstall_requested_at, NOW()), uninstall_note = $3,
			updated_at = NOW()
		FROM users u WHERE u.id = v.owner_id AND u.organization_id = $1 AND v.id = $2 AND v.device_id IS NOT NULL`,
		orgID, vehicleID, note)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return invalid("este veículo não tem rastreador para retirar")
	}
	return nil
}

// CancelUninstallRequest: a associação desiste do pedido (antes da retirada).
func (s *Service) CancelUninstallRequest(ctx context.Context, orgID, vehicleID uuid.UUID) error {
	var open bool
	if err := s.db.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM installation_appointments
			WHERE vehicle_id = $1 AND kind = 'uninstall' AND status = 'arrived')`, vehicleID).Scan(&open); err != nil {
		return database.MapError(err)
	}
	if open {
		return invalid("o instalador já está no local retirando o rastreador")
	}
	return database.MapError(pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE vehicles v SET uninstall_requested_at = NULL, uninstall_note = '', updated_at = NOW()
			FROM users u WHERE u.id = v.owner_id AND u.organization_id = $1 AND v.id = $2`, orgID, vehicleID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return database.ErrNotFound
		}
		// A desinstalação já marcada sai da agenda.
		_, err = tx.Exec(ctx, `
			UPDATE installation_appointments SET status = 'canceled', updated_at = NOW()
			WHERE vehicle_id = $1 AND kind = 'uninstall' AND status = 'scheduled'`, vehicleID)
		return err
	}))
}

func (s *Service) check(in *Input) error {
	in.InstallerName = strings.Join(strings.Fields(in.InstallerName), " ")
	in.Address = strings.Join(strings.Fields(in.Address), " ")
	in.Notes = strings.TrimSpace(in.Notes)
	if in.DurationMin == 0 {
		in.DurationMin = 60
	}
	if in.Kind == "" {
		in.Kind = KindInstall
	}
	switch {
	case in.Kind != KindInstall && in.Kind != KindUninstall:
		return invalid("tipo inválido")
	case in.ScheduledFor.IsZero() || in.ScheduledFor.Before(s.now().Add(-24*time.Hour)):
		return invalid("escolha o dia e a hora da instalação")
	case in.DurationMin < 15 || in.DurationMin > 480:
		return invalid("a duração vai de 15 minutos a 8 horas")
	case in.Address == "" || utf8.RuneCountInString(in.Address) > 300:
		return invalid("informe o endereço (até 300 caracteres)")
	case utf8.RuneCountInString(in.InstallerName) > 120 || utf8.RuneCountInString(in.Notes) > 1000:
		return invalid("o nome do instalador vai até 120 caracteres e as observações até 1.000")
	case in.InstallerID == nil && in.InstallerName == "":
		return invalid("escolha o instalador (ou escreva o nome)")
	}
	return nil
}

// what é "Instalação" ou "Desinstalação" (para os avisos).
func what(kind string) string {
	if kind == KindUninstall {
		return "Desinstalação"
	}
	return "Instalação"
}

// Create agenda a instalação (o veículo sem rastreador) ou a desinstalação
// (com rastreador) — um agendamento em aberto por veículo — e avisa o cliente.
func (s *Service) Create(ctx context.Context, by uuid.UUID, in Input) (*Appointment, error) {
	if err := s.check(&in); err != nil {
		return nil, err
	}
	var device *uuid.UUID
	var owner *uuid.UUID
	if err := s.db.QueryRow(ctx, `SELECT device_id, owner_id FROM vehicles WHERE id = $1`, in.VehicleID).Scan(&device, &owner); err != nil {
		return nil, database.MapError(err)
	}
	if in.Kind == KindInstall && device != nil {
		return nil, invalid("este veículo já tem rastreador")
	}
	if in.Kind == KindUninstall && device == nil {
		return nil, invalid("este veículo não tem rastreador para retirar")
	}
	if owner == nil {
		return nil, invalid("o veículo não é de nenhum cliente")
	}
	var id uuid.UUID
	err := s.db.QueryRow(ctx, `
		INSERT INTO installation_appointments (kind, vehicle_id, installer_id, installer_name, scheduled_for, duration_min,
			address, notes, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`,
		in.Kind, in.VehicleID, in.InstallerID, in.InstallerName, in.ScheduledFor, in.DurationMin, in.Address, in.Notes, by).Scan(&id)
	if errors.Is(database.MapError(err), database.ErrConflict) {
		return nil, invalid("este veículo já tem um agendamento em aberto: remarque o que está aberto")
	}
	if err != nil {
		return nil, database.MapError(err)
	}
	a, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	s.notifyCustomer(a, what(a.Kind)+" agendada: "+a.VehicleName,
		"Dia "+a.ScheduledFor.In(brt).Format("02/01 às 15:04")+" em "+a.Address+".")
	return a, nil
}

var brt = time.FixedZone("BRT", -3*60*60)

// Reschedule remarca (o instalador, o horário, o endereço) enquanto não começou.
func (s *Service) Reschedule(ctx context.Context, id uuid.UUID, in Input) (*Appointment, error) {
	if err := s.check(&in); err != nil {
		return nil, err
	}
	tag, err := s.db.Exec(ctx, `
		UPDATE installation_appointments SET installer_id = $2, installer_name = $3, scheduled_for = $4, duration_min = $5,
			address = $6, notes = $7, updated_at = NOW()
		WHERE id = $1 AND status = 'scheduled'`,
		id, in.InstallerID, in.InstallerName, in.ScheduledFor, in.DurationMin, in.Address, in.Notes)
	if err != nil {
		return nil, database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return nil, invalid("só dá para remarcar um agendamento que ainda não começou")
	}
	a, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	s.notifyCustomer(a, what(a.Kind)+" remarcada: "+a.VehicleName,
		"Novo horário: "+a.ScheduledFor.In(brt).Format("02/01 às 15:04")+" em "+a.Address+".")
	return a, nil
}

// Close cancela ou marca que o cliente não estava (o link para).
func (s *Service) Close(ctx context.Context, id uuid.UUID, status string) (*Appointment, error) {
	if status != StatusCanceled && status != StatusNoShow {
		return nil, invalid("situação inválida")
	}
	tag, err := s.db.Exec(ctx, `
		UPDATE installation_appointments SET status = $2, updated_at = NOW() WHERE id = $1 AND status IN ('scheduled', 'arrived')`,
		id, status)
	if err != nil {
		return nil, database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return nil, invalid("o agendamento já foi encerrado")
	}
	return s.Get(ctx, id)
}

// ByToken é o agendamento do link do instalador: em aberto, ou encerrado há
// menos de linkGrace (só para ver).
func (s *Service) ByToken(ctx context.Context, token string) (*Appointment, error) {
	token = strings.TrimSpace(token)
	raw, err := encoding.DecodeString(token)
	if err != nil || len(raw) != 32 {
		return nil, ErrLinkEnded
	}
	id, err := uuid.FromBytes(raw[:16])
	if err != nil || !hmac.Equal([]byte(s.Token(id)), []byte(token)) {
		return nil, ErrLinkEnded
	}
	a, err := scan(s.db.QueryRow(ctx, selectAppointment+` WHERE a.id = $1`, id))
	if errors.Is(err, database.ErrNotFound) {
		return nil, ErrLinkEnded
	}
	if err != nil {
		return nil, err
	}
	if a.Status == StatusCanceled || a.Status == StatusNoShow {
		return nil, ErrLinkEnded
	}
	if a.CompletedAt != nil && s.now().Sub(*a.CompletedAt) > linkGrace {
		return nil, ErrLinkEnded
	}
	return a, nil
}

// Open diz se o agendamento ainda aceita mudanças pelo link.
func (a *Appointment) Open() bool { return a.Status == StatusScheduled || a.Status == StatusArrived }

// Arrive marca que o instalador chegou.
func (s *Service) Arrive(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.Exec(ctx, `
		UPDATE installation_appointments SET status = 'arrived', arrived_at = COALESCE(arrived_at, NOW()), updated_at = NOW()
		WHERE id = $1 AND status IN ('scheduled', 'arrived')`, id)
	return database.MapError(err)
}

// SavePhoto guarda (ou troca) uma foto da instalação.
func (s *Service) SavePhoto(ctx context.Context, id uuid.UUID, slot string, data []byte) error {
	if !slices.Contains(Slots, slot) {
		return invalid("foto inválida")
	}
	if len(data) == 0 || len(data) > MaxPhotoBytes {
		return invalid("a foto vai até %d MB", MaxPhotoBytes>>20)
	}
	contentType := finance.AttachmentType(data)
	if contentType != "image/jpeg" && contentType != "image/png" {
		return invalid("a foto precisa ser JPEG ou PNG")
	}
	_, err := s.db.Exec(ctx, `
		INSERT INTO installation_photos (appointment_id, slot, content_type, size_bytes, data) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (appointment_id, slot) DO UPDATE SET content_type = EXCLUDED.content_type,
			size_bytes = EXCLUDED.size_bytes, data = EXCLUDED.data, taken_at = NOW()`, id, slot, contentType, len(data), data)
	return database.MapError(err)
}

// Photo é uma foto da instalação.
func (s *Service) Photo(ctx context.Context, id uuid.UUID, slot string) (string, []byte, error) {
	var contentType string
	var data []byte
	err := s.db.QueryRow(ctx, `SELECT content_type, data FROM installation_photos WHERE appointment_id = $1 AND slot = $2`,
		id, slot).Scan(&contentType, &data)
	return contentType, data, database.MapError(err)
}

// Complete conclui: liga o rastreador (o IMEI conferido) ao veículo, com a
// foto do rastreador instalado, e avisa o cliente.
func (s *Service) Complete(ctx context.Context, id, deviceID uuid.UUID, note string) (*Appointment, error) {
	note = strings.TrimSpace(note)
	if utf8.RuneCountInString(note) > 500 {
		return nil, invalid("a observação vai até 500 caracteres")
	}
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var vehicleID uuid.UUID
		var status, kind string
		if err := tx.QueryRow(ctx, `SELECT vehicle_id, status, kind FROM installation_appointments WHERE id = $1 FOR UPDATE`, id).
			Scan(&vehicleID, &status, &kind); err != nil {
			return err
		}
		if kind != KindInstall {
			return invalid("este agendamento é de desinstalação")
		}
		if status != StatusScheduled && status != StatusArrived {
			return invalid("a instalação já foi encerrada")
		}
		var photo bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM installation_photos WHERE appointment_id = $1 AND slot = 'tracker')`,
			id).Scan(&photo); err != nil {
			return err
		}
		if !photo {
			return invalid("tire a foto do rastreador instalado antes de concluir")
		}
		var other *uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT id FROM vehicles WHERE device_id = $1`, deviceID).Scan(&other); err != nil &&
			!errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if other != nil && *other != vehicleID {
			return invalid("este rastreador já está em outro veículo: confira o IMEI")
		}
		tag, err := tx.Exec(ctx, `UPDATE vehicles SET device_id = $2, updated_at = NOW() WHERE id = $1 AND device_id IS NULL`,
			vehicleID, deviceID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 && other == nil {
			return invalid("o veículo já recebeu outro rastreador")
		}
		_, err = tx.Exec(ctx, `
			UPDATE installation_appointments SET status = 'done', device_id = $2, completed_at = NOW(), installer_note = $3,
				arrived_at = COALESCE(arrived_at, NOW()), updated_at = NOW()
			WHERE id = $1`, id, deviceID, note)
		return err
	})
	if err != nil {
		var input *InputError
		if errors.As(err, &input) {
			return nil, input
		}
		return nil, database.MapError(err)
	}
	a, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	s.notifyCustomer(a, "Rastreador instalado: "+a.VehicleName, "Já dá para acompanhar o veículo no app.")
	return a, nil
}

// CompleteUninstall conclui a desinstalação: com a foto do rastreador
// retirado, o rastreador sai do veículo (e o pedido da associação, junto).
// A taxa de desinstalação entra na fatura da associação no fechamento.
func (s *Service) CompleteUninstall(ctx context.Context, id uuid.UUID, note string) (*Appointment, error) {
	note = strings.TrimSpace(note)
	if utf8.RuneCountInString(note) > 500 {
		return nil, invalid("a observação vai até 500 caracteres")
	}
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var vehicleID uuid.UUID
		var status, kind string
		if err := tx.QueryRow(ctx, `SELECT vehicle_id, status, kind FROM installation_appointments WHERE id = $1 FOR UPDATE`, id).
			Scan(&vehicleID, &status, &kind); err != nil {
			return err
		}
		if kind != KindUninstall {
			return invalid("este agendamento é de instalação")
		}
		if status != StatusScheduled && status != StatusArrived {
			return invalid("a desinstalação já foi encerrada")
		}
		var photo bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM installation_photos WHERE appointment_id = $1 AND slot = 'tracker')`,
			id).Scan(&photo); err != nil {
			return err
		}
		if !photo {
			return invalid("tire a foto do rastreador retirado antes de concluir")
		}
		// O rastreador que estava no veículo (nulo se a central já tirou).
		var device *uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT device_id FROM vehicles WHERE id = $1 FOR UPDATE`, vehicleID).Scan(&device); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE vehicles SET device_id = NULL, uninstall_requested_at = NULL, uninstall_note = '', updated_at = NOW()
			WHERE id = $1`, vehicleID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			UPDATE installation_appointments SET status = 'done', device_id = $2, completed_at = NOW(), installer_note = $3,
				arrived_at = COALESCE(arrived_at, NOW()), updated_at = NOW()
			WHERE id = $1`, id, device, note)
		return err
	})
	if err != nil {
		var input *InputError
		if errors.As(err, &input) {
			return nil, input
		}
		return nil, database.MapError(err)
	}
	a, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	s.notifyCustomer(a, "Rastreador retirado: "+a.VehicleName, "A desinstalação foi concluída.")
	return a, nil
}

func (s *Service) notifyCustomer(a *Appointment, title, body string) {
	if s.pusher == nil {
		return
	}
	s.runAsync(func() {
		if _, err := s.pusher.Notify(context.Background(), a.CustomerID, push.Notification{
			Title: title, Body: body, URL: "/app/meus-veiculos", Tag: "install:" + a.VehicleID.String(),
			Severity: mail.SeverityInfo,
		}); err != nil {
			s.log.Warn("aviso da instalação não chegou ao cliente", "appointment", a.ID, "err", err)
		}
	})
}
