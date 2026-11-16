// Package inspections é a vistoria digital: a associação ou seguradora pede
// a vistoria de um veículo — de um associado, ou de quem ainda vai entrar,
// só com o nome, o celular e a placa — e manda o link (sem login) para a
// pessoa ou para um técnico. Quem abre tira, pela câmera ao vivo, a foto de
// cada ângulo do roteiro do tipo de vistoria (adesão, revistoria, sinistro,
// troca de veículo), declara as avarias que já existem e envia; no envio,
// as conferências (o lugar e a hora das fotos, a foto repetida, a posição
// do rastreador). A equipe aprova ou devolve para refazer, com o motivo — o
// mesmo link abre a refação. O associado com conta também faz pelo app, e é
// avisado nele a cada passo. A vistoria enviada entra na fatura da
// organização, pelo preço dela.
package inspections

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/finance"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/mail"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/phone"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/push"
)

// As situações.
const (
	StatusPending   = "pending"
	StatusSubmitted = "submitted"
	StatusApproved  = "approved"
	StatusRejected  = "rejected"
	StatusCanceled  = "canceled"
)

// De onde a foto veio: a câmera ao vivo da vistoria ou o seletor de
// arquivos (o celular sem a câmera no navegador), que pode trazer uma foto
// antiga da galeria.
const (
	SourceCamera = "camera"
	SourceFile   = "file"
)

const (
	MaxNote    = 500
	MaxDamages = 1000
	// MaxPhotoBytes: a foto do celular (a tela reduz antes).
	MaxPhotoBytes = 6 << 20
	// LinkTTL: quanto tempo o link abre (a associação renova).
	LinkTTL = 7 * 24 * time.Hour
)

// InputError é um pedido recusado; a mensagem vai para a tela.
type InputError struct{ Message string }

func (e *InputError) Error() string { return e.Message }

func invalid(format string, args ...any) error { return &InputError{fmt.Sprintf(format, args...)} }

// Inspection é uma vistoria.
type Inspection struct {
	ID               uuid.UUID `json:"id"`
	OrganizationID   uuid.UUID `json:"organizationId"`
	OrganizationName string    `json:"organizationName"`
	// AssociateID: o associado com conta (vazio: quem ainda vai entrar, pelo
	// link); AssociateName e Phone são dele ou os do pedido.
	AssociateID   *uuid.UUID `json:"associateId"`
	AssociateName string     `json:"associateName"`
	Phone         string     `json:"phone"`
	// VehicleID: o veículo do associado (vazio: só a placa do pedido).
	VehicleID   *uuid.UUID `json:"vehicleId"`
	VehicleName string     `json:"vehicleName"`
	Plate       string     `json:"plate"`
	VehicleType string     `json:"vehicleType"`
	Kind        string     `json:"kind"`
	Mode        string     `json:"mode"`
	Status      string     `json:"status"`
	RequestNote string     `json:"requestNote"`
	ReviewNote  string     `json:"reviewNote"`
	RequestedBy string     `json:"requestedByName"`
	ReviewedBy  string     `json:"reviewedByName"`
	SubmittedAt *time.Time `json:"submittedAt"`
	ReviewedAt  *time.Time `json:"reviewedAt"`
	// ExpiresAt: até quando o link abre.
	ExpiresAt *time.Time `json:"expiresAt"`
	// RedoOf: a vistoria original, na refação.
	RedoOf *uuid.UUID `json:"redoOf"`
	// PriceCents: o que a organização paga por ela (zero na refação); Billed:
	// já entrou numa fatura.
	PriceCents  int        `json:"priceCents"`
	Billed      bool       `json:"billed"`
	DamagesNote string     `json:"damagesNote"`
	AcceptedAt  *time.Time `json:"acceptedAt"`
	Checks      []Check    `json:"checks"`
	CreatedAt   time.Time  `json:"createdAt"`
	// Slots: o roteiro das fotos; Photos, os ângulos que já têm foto.
	Slots  []Slot  `json:"slots"`
	Photos []Photo `json:"photos"`
	// Link: o endereço sem login, enquanto espera as fotos.
	Link string `json:"link,omitempty"`
}

// Photo é a foto de um ângulo (sem o conteúdo).
type Photo struct {
	Slot      string    `json:"slot"`
	SizeBytes int       `json:"sizeBytes"`
	Latitude  *float64  `json:"latitude"`
	Longitude *float64  `json:"longitude"`
	AccuracyM *float64  `json:"accuracyM"`
	Source    string    `json:"source"`
	TakenAt   time.Time `json:"takenAt"`
}

// Missing são os ângulos obrigatórios do roteiro ainda sem foto.
func (in *Inspection) Missing() []Slot {
	var out []Slot
	for _, slot := range in.Slots {
		if !slot.Optional && !slices.ContainsFunc(in.Photos, func(p Photo) bool { return p.Slot == slot.ID }) {
			out = append(out, slot)
		}
	}
	return out
}

// Complete diz se todos os ângulos obrigatórios têm foto.
func (in *Inspection) Complete() bool { return len(in.Missing()) == 0 }

// Expired diz se o link da vistoria que espera as fotos já venceu.
func (in *Inspection) Expired(now time.Time) bool {
	return in.Status == StatusPending && in.ExpiresAt != nil && !now.Before(*in.ExpiresAt)
}

// Root é a vistoria original da série (o link é o dela).
func (in *Inspection) Root() uuid.UUID {
	if in.RedoOf != nil {
		return *in.RedoOf
	}
	return in.ID
}

// Pusher avisa o associado no app (push.Service).
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

// NewService: appURL é o painel (onde o link abre); secret assina o link.
func NewService(db *database.DB, appURL string, secret []byte, log *slog.Logger) *Service {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte("farbo-inspection-link-v1"))
	return &Service{db: db, appURL: strings.TrimRight(appURL, "/"), key: mac.Sum(nil), now: time.Now,
		runAsync: func(f func()) { go f() }, log: log.With("component", "inspections")}
}

// SetPusher liga o aviso no app.
func (s *Service) SetPusher(p Pusher) { s.pusher = p }

// SetSync faz os avisos saírem dentro da chamada (testes).
func (s *Service) SetSync() { s.runAsync = func(f func()) { f() } }

// SetClock: os testes.
func (s *Service) SetClock(now func() time.Time) { s.now = now }

// Now é o relógio do serviço (o vencimento do link).
func (s *Service) Now() time.Time { return s.now() }

// ---------------------------------------------------------------------------
// O link
// ---------------------------------------------------------------------------

var encoding = base64.RawURLEncoding

func (s *Service) sign(id uuid.UUID) string {
	mac := hmac.New(sha256.New, s.key)
	mac.Write(id[:])
	return encoding.EncodeToString(append(id[:], mac.Sum(nil)[:16]...))
}

func (s *Service) verify(token string) (uuid.UUID, bool) {
	raw, err := encoding.DecodeString(strings.TrimSpace(token))
	if err != nil || len(raw) != 32 {
		return uuid.Nil, false
	}
	id, err := uuid.FromBytes(raw[:16])
	if err != nil || !hmac.Equal([]byte(s.sign(id)), []byte(strings.TrimSpace(token))) {
		return uuid.Nil, false
	}
	return id, true
}

// PublicURL é o link sem login da série da vistoria (a original e as refações).
func (s *Service) PublicURL(in *Inspection) string {
	return s.appURL + "/vistoria/" + s.sign(in.Root())
}

// ByToken é a vistoria mais nova da série do link.
func (s *Service) ByToken(ctx context.Context, token string) (*Inspection, error) {
	root, ok := s.verify(token)
	if !ok {
		return nil, database.ErrNotFound
	}
	return s.Latest(ctx, root)
}

// Latest é a vistoria mais nova da série que começou em root (a original
// ou a última refação).
func (s *Service) Latest(ctx context.Context, root uuid.UUID) (*Inspection, error) {
	var id uuid.UUID
	err := s.db.QueryRow(ctx, `
		SELECT id FROM inspections WHERE id = $1 OR redo_of = $1 ORDER BY created_at DESC, id DESC LIMIT 1`, root).Scan(&id)
	if err != nil {
		return nil, database.MapError(err)
	}
	return s.Get(ctx, id)
}

// TechnicianLink é o link da vistoria com técnico que espera as fotos do
// veículo (vazio: nenhuma) — o instalador faz junto com a instalação.
func (s *Service) TechnicianLink(ctx context.Context, vehicleID uuid.UUID) string {
	var id uuid.UUID
	var redoOf *uuid.UUID
	err := s.db.QueryRow(ctx, `
		SELECT id, redo_of FROM inspections
		WHERE vehicle_id = $1 AND mode = 'technician' AND status = 'pending' AND (expires_at IS NULL OR expires_at > $2)
		ORDER BY created_at DESC LIMIT 1`, vehicleID, s.now()).Scan(&id, &redoOf)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			s.log.Warn("falha ao procurar a vistoria do técnico", "vehicle", vehicleID, "err", err)
		}
		return ""
	}
	return s.PublicURL(&Inspection{ID: id, RedoOf: redoOf})
}

// ---------------------------------------------------------------------------
// Leitura
// ---------------------------------------------------------------------------

const selectInspection = `
	SELECT i.id, i.organization_id, ou.name, i.associate_id, COALESCE(a.name, i.person_name),
		COALESCE(NULLIF(a.phone, ''), i.person_phone), i.vehicle_id, COALESCE(v.name, NULLIF(i.vehicle_desc, ''), i.plate),
		COALESCE(NULLIF(v.plate, ''), i.plate), i.vehicle_type, i.kind, i.mode, i.status, i.request_note, i.review_note,
		COALESCE(rq.name, ''), COALESCE(rv.name, ''), i.submitted_at, i.reviewed_at, i.expires_at, i.redo_of,
		i.price_cents, i.billed, i.damages_note, i.accepted_at, i.checks, i.created_at
	FROM inspections i JOIN users ou ON ou.id = i.organization_id
	LEFT JOIN users a ON a.id = i.associate_id LEFT JOIN vehicles v ON v.id = i.vehicle_id
	LEFT JOIN users rq ON rq.id = i.requested_by LEFT JOIN users rv ON rv.id = i.reviewed_by`

func (s *Service) scan(row database.Scanner) (*Inspection, error) {
	var in Inspection
	if err := row.Scan(&in.ID, &in.OrganizationID, &in.OrganizationName, &in.AssociateID, &in.AssociateName, &in.Phone,
		&in.VehicleID, &in.VehicleName, &in.Plate, &in.VehicleType, &in.Kind, &in.Mode, &in.Status, &in.RequestNote,
		&in.ReviewNote, &in.RequestedBy, &in.ReviewedBy, &in.SubmittedAt, &in.ReviewedAt, &in.ExpiresAt, &in.RedoOf,
		&in.PriceCents, &in.Billed, &in.DamagesNote, &in.AcceptedAt, &in.Checks, &in.CreatedAt); err != nil {
		return nil, database.MapError(err)
	}
	if in.Checks == nil {
		in.Checks = []Check{}
	}
	in.Slots = Route(in.Kind, in.VehicleType)
	in.Photos = []Photo{}
	if in.Status == StatusPending {
		in.Link = s.PublicURL(&in)
	}
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
		SELECT inspection_id, slot, size_bytes, latitude, longitude, accuracy_m, source, taken_at FROM inspection_photos
		WHERE inspection_id = ANY($1)`, ids)
	if err != nil {
		return database.MapError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var p Photo
		if err := rows.Scan(&id, &p.Slot, &p.SizeBytes, &p.Latitude, &p.Longitude, &p.AccuracyM, &p.Source, &p.TakenAt); err != nil {
			return database.MapError(err)
		}
		byID[id].Photos = append(byID[id].Photos, p)
	}
	for _, in := range list {
		order := func(slot string) int {
			return slices.IndexFunc(in.Slots, func(s Slot) bool { return s.ID == slot })
		}
		slices.SortFunc(in.Photos, func(a, b Photo) int { return order(a.Slot) - order(b.Slot) })
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
		in, err := s.scan(rows)
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
	in, err := s.scan(s.db.QueryRow(ctx, selectInspection+` WHERE i.id = $1`, id))
	if err != nil {
		return nil, err
	}
	return in, s.photos(ctx, []*Inspection{in})
}

// ---------------------------------------------------------------------------
// O pedido
// ---------------------------------------------------------------------------

// RequestInput é o pedido da vistoria: o veículo do associado ou, de quem
// ainda vai entrar, o nome, o celular, a placa e o veículo.
type RequestInput struct {
	VehicleID   *uuid.UUID `json:"vehicleId"`
	Kind        string     `json:"kind"`
	Mode        string     `json:"mode"`
	VehicleType string     `json:"vehicleType"`
	PersonName  string     `json:"personName"`
	PersonPhone string     `json:"personPhone"`
	Plate       string     `json:"plate"`
	VehicleDesc string     `json:"vehicleDesc"`
	Note        string     `json:"note"`
}

// platePattern: a placa antiga (ABC1234) e a do Mercosul (ABC1D23).
var platePattern = regexp.MustCompile(`^[A-Z]{3}[0-9][A-Z0-9][0-9]{2}$`)

// ValidPlate diz se a placa (já normalizada) é a antiga ou a do Mercosul.
func ValidPlate(plate string) bool { return platePattern.MatchString(plate) }

// NormalizePlate: só as letras e os números, em maiúsculas.
func NormalizePlate(raw string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r - 'a' + 'A'
		case (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'):
			return r
		}
		return -1
	}, raw)
}

// Request pede a vistoria (uma em aberto por veículo ou placa) e avisa o
// associado com conta. O link vale LinkTTL.
func (s *Service) Request(ctx context.Context, orgID, by uuid.UUID, in RequestInput) (*Inspection, error) {
	in.Note = strings.TrimSpace(in.Note)
	if utf8.RuneCountInString(in.Note) > MaxNote {
		return nil, invalid("a observação vai até %d caracteres", MaxNote)
	}
	if in.Kind == "" {
		in.Kind = KindPeriodic
	}
	if in.Mode == "" {
		in.Mode = ModeSelf
	}
	if !validKind(in.Kind) {
		return nil, invalid("escolha o tipo: adesão, revistoria, sinistro ou troca de veículo")
	}
	if in.Mode != ModeSelf && in.Mode != ModeTechnician {
		return nil, invalid("escolha quem tira as fotos: o associado ou um técnico")
	}

	var associate *uuid.UUID
	var personName, personPhone, plate, desc string
	vehicleType := in.VehicleType
	if in.VehicleID != nil {
		var id uuid.UUID
		var active bool
		var kind string
		err := s.db.QueryRow(ctx, `
			SELECT a.id, a.active, v.kind FROM vehicles v JOIN users a ON a.id = v.owner_id
			WHERE v.id = $1 AND a.organization_id = $2`, *in.VehicleID, orgID).Scan(&id, &active, &kind)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, database.ErrNotFound
		}
		if err != nil {
			return nil, database.MapError(err)
		}
		if !active {
			return nil, invalid("o associado está desligado")
		}
		associate = &id
		vehicleType = VehicleCar
		if kind == "MOTORCYCLE" {
			vehicleType = VehicleMotorcycle
		}
	} else {
		personName = strings.Join(strings.Fields(in.PersonName), " ")
		personPhone = phone.Format(in.PersonPhone)
		plate = NormalizePlate(in.Plate)
		desc = strings.Join(strings.Fields(in.VehicleDesc), " ")
		if vehicleType == "" {
			vehicleType = VehicleCar
		}
		switch n := utf8.RuneCountInString(personName); {
		case n < 2 || n > 120:
			return nil, invalid("informe o nome de quem vai fazer a vistoria (ou do dono do veículo)")
		case !platePattern.MatchString(plate):
			return nil, invalid("informe a placa do veículo (ABC1D23 ou ABC1234)")
		case utf8.RuneCountInString(desc) > 80:
			return nil, invalid("a descrição do veículo vai até 80 caracteres")
		case utf8.RuneCountInString(personPhone) > 30:
			return nil, invalid("o celular parece errado")
		case !validVehicle(vehicleType):
			return nil, invalid("escolha o veículo: carro ou moto")
		}
	}

	var price int
	if err := s.db.QueryRow(ctx, `
		SELECT CASE WHEN $2 = 'technician' THEN inspection_tech_cents ELSE inspection_cents END FROM organizations WHERE id = $1`,
		orgID, in.Mode).Scan(&price); err != nil {
		return nil, database.MapError(err)
	}
	var id uuid.UUID
	err := s.db.QueryRow(ctx, `
		INSERT INTO inspections (organization_id, associate_id, vehicle_id, request_note, requested_by, kind, mode, vehicle_type,
			person_name, person_phone, plate, vehicle_desc, expires_at, price_cents)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14) RETURNING id`,
		orgID, associate, in.VehicleID, in.Note, nullable(by), in.Kind, in.Mode, vehicleType,
		personName, personPhone, plate, desc, s.now().Add(LinkTTL), price).Scan(&id)
	if err != nil {
		if errors.Is(database.MapError(err), database.ErrConflict) {
			if in.VehicleID == nil {
				return nil, invalid("esta placa já tem uma vistoria em aberto")
			}
			return nil, invalid("este veículo já tem uma vistoria em aberto")
		}
		return nil, database.MapError(err)
	}
	out, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if out.Mode == ModeSelf {
		s.notify(out, "Vistoria do veículo "+out.VehicleName, "Tire as fotos pelo app: leva poucos minutos.")
	}
	return out, nil
}

func nullable(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

// ---------------------------------------------------------------------------
// As fotos e o envio
// ---------------------------------------------------------------------------

// PhotoMeta é o que vem com a foto: a posição do celular (se a pessoa
// deixou), a precisão dela e de onde a foto veio.
type PhotoMeta struct {
	Latitude, Longitude, AccuracyM *float64
	Source                         string
}

// editable: a vistoria ainda aceita fotos e o envio.
func (s *Service) editable(status string, expires *time.Time) error {
	switch {
	case status == StatusCanceled:
		return invalid("a vistoria foi cancelada")
	case status != StatusPending:
		return invalid("a vistoria já foi enviada")
	case expires != nil && !s.now().Before(*expires):
		return invalid("o link venceu: peça um novo a quem mandou")
	}
	return nil
}

// SavePhoto guarda (ou troca) a foto de um ângulo do roteiro, enquanto a
// vistoria espera as fotos. Só JPEG ou PNG.
func (s *Service) SavePhoto(ctx context.Context, id uuid.UUID, slot string, data []byte, meta PhotoMeta) (*Photo, error) {
	if len(data) == 0 || len(data) > MaxPhotoBytes {
		return nil, invalid("a foto vai até %d MB", MaxPhotoBytes>>20)
	}
	contentType := finance.AttachmentType(data)
	if contentType != "image/jpeg" && contentType != "image/png" {
		return nil, invalid("a foto precisa ser JPEG ou PNG")
	}
	lat, lon, accuracy := meta.Latitude, meta.Longitude, meta.AccuracyM
	if lat == nil || lon == nil || math.Abs(*lat) > 90 || math.Abs(*lon) > 180 || (*lat == 0 && *lon == 0) {
		lat, lon, accuracy = nil, nil, nil
	}
	if accuracy != nil && (*accuracy < 0 || *accuracy > 100_000) {
		accuracy = nil
	}
	source := meta.Source
	if source != SourceCamera {
		source = SourceFile
	}
	sum := sha256.Sum256(data)
	var p Photo
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var status, kind, vehicleType string
		var expires *time.Time
		if err := tx.QueryRow(ctx, `SELECT status, kind, vehicle_type, expires_at FROM inspections WHERE id = $1 FOR UPDATE`, id).
			Scan(&status, &kind, &vehicleType, &expires); err != nil {
			return err
		}
		if err := s.editable(status, expires); err != nil {
			return err
		}
		if !slices.ContainsFunc(Route(kind, vehicleType), func(s Slot) bool { return s.ID == slot }) {
			return invalid("ângulo inválido")
		}
		return tx.QueryRow(ctx, `
			INSERT INTO inspection_photos (inspection_id, slot, content_type, size_bytes, data, latitude, longitude, accuracy_m,
				source, sha256)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			ON CONFLICT (inspection_id, slot) DO UPDATE SET content_type = EXCLUDED.content_type,
				size_bytes = EXCLUDED.size_bytes, data = EXCLUDED.data, latitude = EXCLUDED.latitude,
				longitude = EXCLUDED.longitude, accuracy_m = EXCLUDED.accuracy_m, source = EXCLUDED.source,
				sha256 = EXCLUDED.sha256, taken_at = NOW()
			RETURNING slot, size_bytes, latitude, longitude, accuracy_m, source, taken_at`,
			id, slot, contentType, len(data), data, lat, lon, accuracy, source, sum[:]).
			Scan(&p.Slot, &p.SizeBytes, &p.Latitude, &p.Longitude, &p.AccuracyM, &p.Source, &p.TakenAt)
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

// SubmitInput é o envio: as avarias que o veículo já tem (o que a pessoa
// declara) e o aceite da declaração de que as fotos são do veículo e foram
// tiradas agora.
type SubmitInput struct {
	DamagesNote string `json:"damagesNote"`
	Accept      bool   `json:"accept"`
}

// Submit manda a vistoria para a revisão (com a foto de todos os ângulos
// obrigatórios), com as conferências.
func (s *Service) Submit(ctx context.Context, id uuid.UUID, input SubmitInput) (*Inspection, error) {
	in, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.editable(in.Status, in.ExpiresAt); err != nil {
		return nil, err
	}
	if missing := in.Missing(); len(missing) > 0 {
		labels := make([]string, len(missing))
		for i, slot := range missing {
			labels[i] = strings.ToLower(slot.Label)
		}
		return nil, invalid("faltam as fotos: %s", strings.Join(labels, ", "))
	}
	damages := strings.TrimSpace(input.DamagesNote)
	if utf8.RuneCountInString(damages) > MaxDamages {
		return nil, invalid("as avarias vão até %d caracteres", MaxDamages)
	}
	if !input.Accept {
		return nil, invalid("confirme a declaração de que as fotos são deste veículo e foram tiradas agora")
	}
	photos := make([]photoCheck, len(in.Photos))
	for i, p := range in.Photos {
		// Aproveitada da vistoria devolvida: tirada antes desta existir.
		photos[i] = photoCheck{Photo: p, kept: in.RedoOf != nil && p.TakenAt.Before(in.CreatedAt)}
	}
	checks, err := s.runChecks(ctx, in, photos)
	if err != nil {
		return nil, err
	}
	if checks == nil {
		checks = []Check{}
	}
	tag, err := s.db.Exec(ctx, `
		UPDATE inspections SET status = 'submitted', submitted_at = NOW(), damages_note = $2, accepted_at = NOW(),
			checks = $3, updated_at = NOW()
		WHERE id = $1 AND status = 'pending'`, id, damages, checks)
	if err != nil {
		return nil, database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return nil, invalid("a vistoria já foi enviada")
	}
	return s.Get(ctx, id)
}

// ---------------------------------------------------------------------------
// A equipe
// ---------------------------------------------------------------------------

// Review aprova ou devolve a vistoria enviada (devolver pede o motivo e abre
// a refação, com as fotos que ficaram boas, no mesmo link; ela não é
// cobrada de novo).
func (s *Service) Review(ctx context.Context, id, by uuid.UUID, approve bool, note string) (*Inspection, error) {
	note = strings.TrimSpace(note)
	if utf8.RuneCountInString(note) > MaxNote {
		return nil, invalid("a nota vai até %d caracteres", MaxNote)
	}
	if !approve && note == "" {
		return nil, invalid("diga o que refazer: quem fez a vistoria vê o motivo")
	}
	status := StatusApproved
	if !approve {
		status = StatusRejected
	}
	var redo uuid.UUID
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE inspections SET status = $2, review_note = $3, reviewed_by = $4, reviewed_at = NOW(), updated_at = NOW()
			WHERE id = $1 AND status = 'submitted'`, id, status, note, nullable(by))
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return invalid("só dá para revisar uma vistoria enviada")
		}
		if approve {
			return nil
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO inspections (organization_id, associate_id, vehicle_id, request_note, requested_by, kind, mode,
				vehicle_type, person_name, person_phone, plate, vehicle_desc, expires_at, redo_of, price_cents)
			SELECT organization_id, associate_id, vehicle_id, $2, $3, kind, mode, vehicle_type, person_name, person_phone,
				plate, vehicle_desc, $4, COALESCE(redo_of, id), 0
			FROM inspections WHERE id = $1 RETURNING id`,
			id, note, nullable(by), s.now().Add(LinkTTL)).Scan(&redo); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO inspection_photos (inspection_id, slot, content_type, size_bytes, data, latitude, longitude,
				accuracy_m, source, sha256, taken_at)
			SELECT $2, slot, content_type, size_bytes, data, latitude, longitude, accuracy_m, source, sha256, taken_at
			FROM inspection_photos WHERE inspection_id = $1`, id, redo)
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
	} else if in.Mode == ModeSelf {
		s.notify(in, "Refaça a vistoria: "+in.VehicleName, note)
	}
	return in, nil
}

// Cancel cancela a vistoria que ainda espera as fotos (o link para de abrir).
func (s *Service) Cancel(ctx context.Context, id uuid.UUID) (*Inspection, error) {
	tag, err := s.db.Exec(ctx, `UPDATE inspections SET status = 'canceled', updated_at = NOW() WHERE id = $1 AND status = 'pending'`, id)
	if err != nil {
		return nil, database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return nil, invalid("só dá para cancelar a vistoria que espera as fotos")
	}
	return s.Get(ctx, id)
}

// Renew dá mais LinkTTL ao link da vistoria que espera as fotos.
func (s *Service) Renew(ctx context.Context, id uuid.UUID) (*Inspection, error) {
	tag, err := s.db.Exec(ctx, `UPDATE inspections SET expires_at = $2, updated_at = NOW() WHERE id = $1 AND status = 'pending'`,
		id, s.now().Add(LinkTTL))
	if err != nil {
		return nil, database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return nil, invalid("só dá para renovar o link da vistoria que espera as fotos")
	}
	return s.Get(ctx, id)
}

func (s *Service) notify(in *Inspection, title, body string) {
	if s.pusher == nil || in.AssociateID == nil {
		return
	}
	tag := "inspection:" + in.Root().String()
	associate := *in.AssociateID
	s.runAsync(func() {
		if _, err := s.pusher.Notify(context.Background(), associate, push.Notification{
			Title: title, Body: body, URL: "/app/vistorias", Tag: tag, Severity: mail.SeverityInfo,
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
