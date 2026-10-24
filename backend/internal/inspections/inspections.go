// Package inspections é a vistoria digital: a associação ou seguradora pede
// a vistoria de um veículo de associado; ele tira, pelo app, a foto de cada
// ângulo (frente, traseira, laterais, painel com o hodômetro e chassi) e
// manda; a equipe aprova ou devolve para refazer, com o motivo. O associado é
// avisado no app a cada passo.
package inspections

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
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

// Slots são os ângulos, na ordem da tela.
var Slots = []string{"front", "rear", "left", "right", "dashboard", "chassis"}

// SlotLabels: como a tela escreve cada ângulo.
var SlotLabels = map[string]string{
	"front": "Frente", "rear": "Traseira", "left": "Lateral esquerda", "right": "Lateral direita",
	"dashboard": "Painel (hodômetro)", "chassis": "Número do chassi",
}

// As situações.
const (
	StatusPending   = "pending"
	StatusSubmitted = "submitted"
	StatusApproved  = "approved"
	StatusRejected  = "rejected"
)

const (
	MaxNote = 500
	// MaxPhotoBytes: a foto do celular (o app reduz antes).
	MaxPhotoBytes = 6 << 20
)

// InputError é um pedido recusado; a mensagem vai para a tela.
type InputError struct{ Message string }

func (e *InputError) Error() string { return e.Message }

func invalid(format string, args ...any) error { return &InputError{fmt.Sprintf(format, args...)} }

// Inspection é uma vistoria.
type Inspection struct {
	ID             uuid.UUID  `json:"id"`
	OrganizationID uuid.UUID  `json:"organizationId"`
	AssociateID    uuid.UUID  `json:"associateId"`
	AssociateName  string     `json:"associateName"`
	VehicleID      uuid.UUID  `json:"vehicleId"`
	VehicleName    string     `json:"vehicleName"`
	Plate          string     `json:"plate"`
	Status         string     `json:"status"`
	RequestNote    string     `json:"requestNote"`
	ReviewNote     string     `json:"reviewNote"`
	RequestedBy    string     `json:"requestedByName"`
	ReviewedBy     string     `json:"reviewedByName"`
	SubmittedAt    *time.Time `json:"submittedAt"`
	ReviewedAt     *time.Time `json:"reviewedAt"`
	CreatedAt      time.Time  `json:"createdAt"`
	// Photos: os ângulos que já têm foto.
	Photos []Photo `json:"photos"`
}

// Photo é a foto de um ângulo (sem o conteúdo).
type Photo struct {
	Slot      string    `json:"slot"`
	SizeBytes int       `json:"sizeBytes"`
	Latitude  *float64  `json:"latitude"`
	Longitude *float64  `json:"longitude"`
	TakenAt   time.Time `json:"takenAt"`
}

// Complete diz se todos os ângulos têm foto.
func (in *Inspection) Complete() bool {
	for _, slot := range Slots {
		if !slices.ContainsFunc(in.Photos, func(p Photo) bool { return p.Slot == slot }) {
			return false
		}
	}
	return true
}

// Pusher avisa o associado no app (push.Service).
type Pusher interface {
	Notify(ctx context.Context, userID uuid.UUID, n push.Notification) (int, error)
}

type Service struct {
	db       *database.DB
	pusher   Pusher
	runAsync func(func())
	log      *slog.Logger
}

func NewService(db *database.DB, log *slog.Logger) *Service {
	return &Service{db: db, runAsync: func(f func()) { go f() }, log: log.With("component", "inspections")}
}

// SetPusher liga o aviso no app.
func (s *Service) SetPusher(p Pusher) { s.pusher = p }

// SetSync faz os avisos saírem dentro da chamada (testes).
func (s *Service) SetSync() { s.runAsync = func(f func()) { f() } }

const selectInspection = `
	SELECT i.id, i.organization_id, i.associate_id, a.name, i.vehicle_id, v.name, COALESCE(v.plate, ''), i.status,
		i.request_note, i.review_note, COALESCE(rq.name, ''), COALESCE(rv.name, ''), i.submitted_at, i.reviewed_at,
		i.created_at
	FROM inspections i JOIN users a ON a.id = i.associate_id JOIN vehicles v ON v.id = i.vehicle_id
	LEFT JOIN users rq ON rq.id = i.requested_by LEFT JOIN users rv ON rv.id = i.reviewed_by`

func scan(row database.Scanner) (*Inspection, error) {
	var in Inspection
	if err := row.Scan(&in.ID, &in.OrganizationID, &in.AssociateID, &in.AssociateName, &in.VehicleID, &in.VehicleName,
		&in.Plate, &in.Status, &in.RequestNote, &in.ReviewNote, &in.RequestedBy, &in.ReviewedBy, &in.SubmittedAt,
		&in.ReviewedAt, &in.CreatedAt); err != nil {
		return nil, database.MapError(err)
	}
	in.Photos = []Photo{}
	return &in, nil
}

// photos completa as vistorias com os ângulos que já têm foto.
func (s *Service) photos(ctx context.Context, list []*Inspection) error {
	if len(list) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(list))
	byID := map[uuid.UUID]*Inspection{}
	for i, in := range list {
		ids[i], byID[in.ID] = in.ID, in
	}
	rows, err := s.db.Query(ctx, `
		SELECT inspection_id, slot, size_bytes, latitude, longitude, taken_at FROM inspection_photos
		WHERE inspection_id = ANY($1)`, ids)
	if err != nil {
		return database.MapError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var p Photo
		if err := rows.Scan(&id, &p.Slot, &p.SizeBytes, &p.Latitude, &p.Longitude, &p.TakenAt); err != nil {
			return database.MapError(err)
		}
		byID[id].Photos = append(byID[id].Photos, p)
	}
	for _, in := range list {
		slices.SortFunc(in.Photos, func(a, b Photo) int { return slices.Index(Slots, a.Slot) - slices.Index(Slots, b.Slot) })
	}
	return database.MapError(rows.Err())
}

// Filter escolhe as vistorias: as de uma organização ou as de um associado.
type Filter struct {
	OrganizationID *uuid.UUID
	AssociateID    *uuid.UUID
	Status         string
}

// List são as vistorias, das mais recentes (no máximo 500).
func (s *Service) List(ctx context.Context, f Filter) ([]*Inspection, error) {
	rows, err := s.db.Query(ctx, selectInspection+`
		WHERE ($1::uuid IS NULL OR i.organization_id = $1) AND ($2::uuid IS NULL OR i.associate_id = $2)
			AND ($3 = '' OR i.status = $3)
		ORDER BY i.created_at DESC LIMIT 500`, f.OrganizationID, f.AssociateID, f.Status)
	if err != nil {
		return nil, database.MapError(err)
	}
	out := []*Inspection{}
	for rows.Next() {
		in, err := scan(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, in)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, database.MapError(err)
	}
	return out, s.photos(ctx, out)
}

// Get é a vistoria com as fotos.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (*Inspection, error) {
	in, err := scan(s.db.QueryRow(ctx, selectInspection+` WHERE i.id = $1`, id))
	if err != nil {
		return nil, err
	}
	return in, s.photos(ctx, []*Inspection{in})
}

// Request pede a vistoria do veículo do associado da organização (uma em
// aberto por veículo) e avisa o associado.
func (s *Service) Request(ctx context.Context, orgID, vehicleID, by uuid.UUID, note string) (*Inspection, error) {
	note = strings.TrimSpace(note)
	if utf8.RuneCountInString(note) > MaxNote {
		return nil, invalid("o pedido vai até %d caracteres", MaxNote)
	}
	var associate uuid.UUID
	var active bool
	err := s.db.QueryRow(ctx, `
		SELECT a.id, a.active FROM vehicles v JOIN users a ON a.id = v.owner_id
		WHERE v.id = $1 AND a.organization_id = $2`, vehicleID, orgID).Scan(&associate, &active)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, database.ErrNotFound
	}
	if err != nil {
		return nil, database.MapError(err)
	}
	if !active {
		return nil, invalid("o associado está desligado")
	}
	var id uuid.UUID
	if err := s.db.QueryRow(ctx, `
		INSERT INTO inspections (organization_id, associate_id, vehicle_id, request_note, requested_by)
		VALUES ($1, $2, $3, $4, $5) RETURNING id`, orgID, associate, vehicleID, note, by).Scan(&id); err != nil {
		if errors.Is(database.MapError(err), database.ErrConflict) {
			return nil, invalid("este veículo já tem uma vistoria em aberto")
		}
		return nil, database.MapError(err)
	}
	in, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	s.notify(in, "Vistoria do veículo "+in.VehicleName, "Tire as fotos pelo app: leva poucos minutos.")
	return in, nil
}

// SavePhoto guarda (ou troca) a foto de um ângulo, enquanto a vistoria
// espera as fotos. Só JPEG ou PNG.
func (s *Service) SavePhoto(ctx context.Context, id uuid.UUID, slot string, data []byte, lat, lon *float64) (*Photo, error) {
	if !slices.Contains(Slots, slot) {
		return nil, invalid("ângulo inválido")
	}
	if len(data) == 0 || len(data) > MaxPhotoBytes {
		return nil, invalid("a foto vai até %d MB", MaxPhotoBytes>>20)
	}
	contentType := finance.AttachmentType(data)
	if contentType != "image/jpeg" && contentType != "image/png" {
		return nil, invalid("a foto precisa ser JPEG ou PNG")
	}
	if lat != nil && lon != nil && (math.Abs(*lat) > 90 || math.Abs(*lon) > 180) {
		lat, lon = nil, nil
	}
	var p Photo
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var status string
		if err := tx.QueryRow(ctx, `SELECT status FROM inspections WHERE id = $1 FOR UPDATE`, id).Scan(&status); err != nil {
			return err
		}
		if status != StatusPending {
			return invalid("a vistoria já foi enviada")
		}
		return tx.QueryRow(ctx, `
			INSERT INTO inspection_photos (inspection_id, slot, content_type, size_bytes, data, latitude, longitude)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (inspection_id, slot) DO UPDATE SET content_type = EXCLUDED.content_type,
				size_bytes = EXCLUDED.size_bytes, data = EXCLUDED.data, latitude = EXCLUDED.latitude,
				longitude = EXCLUDED.longitude, taken_at = NOW()
			RETURNING slot, size_bytes, latitude, longitude, taken_at`,
			id, slot, contentType, len(data), data, lat, lon).Scan(&p.Slot, &p.SizeBytes, &p.Latitude, &p.Longitude, &p.TakenAt)
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return &p, nil
}

// PhotoData é a foto de um ângulo, com o tipo.
func (s *Service) PhotoData(ctx context.Context, id uuid.UUID, slot string) (string, []byte, error) {
	var contentType string
	var data []byte
	err := s.db.QueryRow(ctx, `SELECT content_type, data FROM inspection_photos WHERE inspection_id = $1 AND slot = $2`,
		id, slot).Scan(&contentType, &data)
	return contentType, data, database.MapError(err)
}

// Submit manda a vistoria para a revisão (com a foto de todos os ângulos).
func (s *Service) Submit(ctx context.Context, id uuid.UUID) (*Inspection, error) {
	in, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if in.Status != StatusPending {
		return nil, invalid("a vistoria já foi enviada")
	}
	if !in.Complete() {
		var missing []string
		for _, slot := range Slots {
			if !slices.ContainsFunc(in.Photos, func(p Photo) bool { return p.Slot == slot }) {
				missing = append(missing, strings.ToLower(SlotLabels[slot]))
			}
		}
		return nil, invalid("faltam as fotos: %s", strings.Join(missing, ", "))
	}
	tag, err := s.db.Exec(ctx, `
		UPDATE inspections SET status = 'submitted', submitted_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND status = 'pending'`, id)
	if err != nil {
		return nil, database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return nil, invalid("a vistoria já foi enviada")
	}
	return s.Get(ctx, id)
}

// Review aprova ou devolve a vistoria enviada (devolver pede o motivo e
// volta para o associado refazer as fotos).
func (s *Service) Review(ctx context.Context, id, by uuid.UUID, approve bool, note string) (*Inspection, error) {
	note = strings.TrimSpace(note)
	if utf8.RuneCountInString(note) > MaxNote {
		return nil, invalid("a nota vai até %d caracteres", MaxNote)
	}
	if !approve && note == "" {
		return nil, invalid("diga o que refazer: o associado vê o motivo no app")
	}
	status := StatusApproved
	if !approve {
		status = StatusRejected
	}
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE inspections SET status = $2, review_note = $3, reviewed_by = $4, reviewed_at = NOW(), updated_at = NOW()
			WHERE id = $1 AND status = 'submitted'`, id, status, note, by)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return invalid("só dá para revisar uma vistoria enviada")
		}
		if approve {
			return nil
		}
		// Devolvida: uma vistoria nova para refazer, com as fotos que ficaram boas.
		var next uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO inspections (organization_id, associate_id, vehicle_id, request_note, requested_by)
			SELECT organization_id, associate_id, vehicle_id, $2, $3 FROM inspections WHERE id = $1 RETURNING id`,
			id, note, by).Scan(&next); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO inspection_photos (inspection_id, slot, content_type, size_bytes, data, latitude, longitude, taken_at)
			SELECT $2, slot, content_type, size_bytes, data, latitude, longitude, taken_at FROM inspection_photos
			WHERE inspection_id = $1`, id, next)
		return err
	})
	if err != nil {
		return nil, mapErr(err)
	}
	in, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if approve {
		s.notify(in, "Vistoria aprovada: "+in.VehicleName, "Tudo certo com as fotos do veículo.")
	} else {
		s.notify(in, "Refaça a vistoria: "+in.VehicleName, note)
	}
	return in, nil
}

func (s *Service) notify(in *Inspection, title, body string) {
	if s.pusher == nil {
		return
	}
	s.runAsync(func() {
		if _, err := s.pusher.Notify(context.Background(), in.AssociateID, push.Notification{
			Title: title, Body: body, URL: "/app/vistorias", Tag: "inspection:" + in.VehicleID.String(),
			Severity: mail.SeverityInfo,
		}); err != nil {
			s.log.Warn("aviso da vistoria não chegou ao associado", "inspection", in.ID, "err", err)
		}
	})
}

func mapErr(err error) error {
	var input *InputError
	if errors.As(err, &input) {
		return input
	}
	return database.MapError(err)
}
