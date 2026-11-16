// Package adhesions é a adesão digital: o link da associação (ela liga no
// painel), aberto sem login, por onde quem quer entrar manda os dados, o
// endereço e o veículo, faz a autovistoria de adesão (pacote inspections) e
// aceita o termo do associado, com a preferência de dia e hora para a
// instalação. A pessoa acompanha pelo link de acompanhamento. A associação
// aprova — o associado e o veículo são cadastrados, o convite do app vai por
// e-mail e o veículo entra na fila de instalação da central, com o endereço
// e a preferência — ou recusa, com o motivo.
package adhesions

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/addresses"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/inspections"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/organizations"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/payments"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/phone"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/vehicles"
)

// As situações.
const (
	StatusPending   = "pending"   // falta a vistoria ou o termo
	StatusSubmitted = "submitted" // espera a associação
	StatusApproved  = "approved"
	StatusRejected  = "rejected"
)

const (
	MaxPreference = 300
	MaxNote       = 500
)

// InputError é um pedido recusado; a mensagem vai para a tela.
type InputError struct{ Message string }

func (e *InputError) Error() string { return e.Message }

func invalid(format string, args ...any) error { return &InputError{fmt.Sprintf(format, args...)} }

// ErrDisabled: o link de adesão da organização está desligado.
var ErrDisabled = &InputError{"a adesão por este link não está aberta: fale com a associação"}

// Input é o que a pessoa manda pelo link de adesão.
type Input struct {
	Name        string            `json:"name"`
	Document    string            `json:"document"`
	Email       string            `json:"email"`
	Phone       string            `json:"phone"`
	Address     addresses.Address `json:"address"`
	VehicleType string            `json:"vehicleType"`
	Plate       string            `json:"plate"`
	Brand       string            `json:"brand"`
	Model       string            `json:"model"`
	Year        *int              `json:"year"`
	Color       string            `json:"color"`
}

// Adhesion é uma adesão.
type Adhesion struct {
	ID               uuid.UUID         `json:"id"`
	OrganizationID   uuid.UUID         `json:"organizationId"`
	OrganizationName string            `json:"organizationName"`
	Status           string            `json:"status"`
	Name             string            `json:"name"`
	Document         string            `json:"document"`
	Email            string            `json:"email"`
	Phone            string            `json:"phone"`
	Address          addresses.Address `json:"address"`
	VehicleType      string            `json:"vehicleType"`
	Plate            string            `json:"plate"`
	Brand            string            `json:"brand"`
	Model            string            `json:"model"`
	Year             *int              `json:"year"`
	Color            string            `json:"color"`
	InstallPref      string            `json:"installPreference"`
	TermVersion      string            `json:"termVersion"`
	AcceptedAt       *time.Time        `json:"acceptedAt"`
	AssociateID      *uuid.UUID        `json:"associateId"`
	VehicleID        *uuid.UUID        `json:"vehicleId"`
	ReviewNote       string            `json:"reviewNote"`
	ReviewedBy       string            `json:"reviewedByName"`
	ReviewedAt       *time.Time        `json:"reviewedAt"`
	CreatedAt        time.Time         `json:"createdAt"`
	// Inspection: a vistoria de adesão (a mais nova da série).
	Inspection *inspections.Inspection `json:"inspection"`
	// FollowURL: o link de acompanhamento da pessoa.
	FollowURL string `json:"followUrl"`

	inspectionRoot *uuid.UUID
	acceptIP       string
	acceptUA       string
}

// Acceptance: o IP e o navegador do aceite do termo (o registro do aceite
// eletrônico na conta nova).
func (a *Adhesion) Acceptance() (ip, userAgent string) { return a.acceptIP, a.acceptUA }

// VehicleName: "Gol 1.0 · 2015" — o modelo (ou a marca), com o ano.
func (a *Adhesion) VehicleName() string {
	name := strings.TrimSpace(a.Model)
	if name == "" {
		name = strings.TrimSpace(a.Brand)
	}
	if name == "" {
		name = a.Plate
	}
	return name
}

type Service struct {
	db          *database.DB
	inspections *inspections.Service
	orgs        *organizations.Service
	addresses   *addresses.Repository
	appURL      string
	orgKey      []byte
	followKey   []byte
	log         *slog.Logger
}

// NewService: appURL é o painel (onde os links abrem); secret assina os links.
func NewService(db *database.DB, inspectionsSvc *inspections.Service, orgs *organizations.Service, appURL string, secret []byte,
	log *slog.Logger) *Service {
	derive := func(label string) []byte {
		mac := hmac.New(sha256.New, secret)
		mac.Write([]byte(label))
		return mac.Sum(nil)
	}
	return &Service{db: db, inspections: inspectionsSvc, orgs: orgs, addresses: addresses.NewRepository(db),
		appURL: strings.TrimRight(appURL, "/"), orgKey: derive("farbo-adhesion-link-v1"),
		followKey: derive("farbo-adhesion-follow-v1"), log: log.With("component", "adhesions")}
}

// ---------------------------------------------------------------------------
// Os links
// ---------------------------------------------------------------------------

var encoding = base64.RawURLEncoding

func sign(key []byte, id uuid.UUID) string {
	mac := hmac.New(sha256.New, key)
	mac.Write(id[:])
	return encoding.EncodeToString(append(id[:], mac.Sum(nil)[:16]...))
}

func verify(key []byte, token string) (uuid.UUID, bool) {
	token = strings.TrimSpace(token)
	raw, err := encoding.DecodeString(token)
	if err != nil || len(raw) != 32 {
		return uuid.Nil, false
	}
	id, err := uuid.FromBytes(raw[:16])
	if err != nil || !hmac.Equal([]byte(sign(key, id)), []byte(token)) {
		return uuid.Nil, false
	}
	return id, true
}

// LinkURL é o link de adesão da organização (o mesmo sempre).
func (s *Service) LinkURL(orgID uuid.UUID) string {
	return s.appURL + "/adesao/" + sign(s.orgKey, orgID)
}

// FollowURL é o link de acompanhamento da adesão.
func (s *Service) FollowURL(id uuid.UUID) string {
	return s.appURL + "/adesao/acompanhar/" + sign(s.followKey, id)
}

// PublicOrg é o que o link de adesão mostra da organização.
type PublicOrg struct {
	ID      uuid.UUID `json:"-"`
	Name    string    `json:"organizationName"`
	Kind    string    `json:"kind"`
	Enabled bool      `json:"enabled"`
}

// OrgByToken é a organização do link de adesão.
func (s *Service) OrgByToken(ctx context.Context, token string) (*PublicOrg, error) {
	id, ok := verify(s.orgKey, token)
	if !ok {
		return nil, database.ErrNotFound
	}
	var o PublicOrg
	err := s.db.QueryRow(ctx, `
		SELECT o.id, u.name, o.kind, o.adhesion_enabled AND u.active FROM organizations o JOIN users u ON u.id = o.id
		WHERE o.id = $1`, id).Scan(&o.ID, &o.Name, &o.Kind, &o.Enabled)
	if err != nil {
		return nil, database.MapError(err)
	}
	return &o, nil
}

// Settings: o link de adesão ligado e o endereço dele.
type Settings struct {
	Enabled bool   `json:"enabled"`
	URL     string `json:"url"`
}

// SettingsOf é o link de adesão da organização.
func (s *Service) SettingsOf(ctx context.Context, orgID uuid.UUID) (*Settings, error) {
	out := &Settings{URL: s.LinkURL(orgID)}
	if err := s.db.QueryRow(ctx, `SELECT adhesion_enabled FROM organizations WHERE id = $1`, orgID).Scan(&out.Enabled); err != nil {
		return nil, database.MapError(err)
	}
	return out, nil
}

// SetEnabled liga ou desliga o link de adesão.
func (s *Service) SetEnabled(ctx context.Context, orgID uuid.UUID, on bool) (*Settings, error) {
	tag, err := s.db.Exec(ctx, `UPDATE organizations SET adhesion_enabled = $2, updated_at = NOW() WHERE id = $1`, orgID, on)
	if err != nil {
		return nil, database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return nil, database.ErrNotFound
	}
	return s.SettingsOf(ctx, orgID)
}

// ---------------------------------------------------------------------------
// A adesão pelo link
// ---------------------------------------------------------------------------

// clean confere e normaliza o que a pessoa mandou.
func clean(in *Input) error {
	in.Name = strings.Join(strings.Fields(in.Name), " ")
	in.Document = payments.Digits(in.Document)
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	in.Phone = phone.Format(in.Phone)
	in.Plate = inspections.NormalizePlate(in.Plate)
	in.Brand = strings.Join(strings.Fields(in.Brand), " ")
	in.Model = strings.Join(strings.Fields(in.Model), " ")
	in.Color = strings.Join(strings.Fields(in.Color), " ")
	if in.VehicleType == "" {
		in.VehicleType = inspections.VehicleCar
	}
	digits := len(phone.Digits(in.Phone))
	switch {
	case utf8.RuneCountInString(in.Name) < 3 || utf8.RuneCountInString(in.Name) > 120:
		return invalid("informe o seu nome completo")
	case !payments.ValidTaxID(in.Document):
		return invalid("CPF inválido: confira os números (para empresa, informe o CNPJ)")
	case !strings.Contains(in.Email, "@") || strings.ContainsAny(in.Email, " <>") || len(in.Email) > 254:
		return invalid("informe um e-mail válido: é por ele que chega o acesso ao app")
	case digits != 10 && digits != 11:
		return invalid("informe o celular com o DDD")
	case in.VehicleType != inspections.VehicleCar && in.VehicleType != inspections.VehicleMotorcycle:
		return invalid("escolha o veículo: carro ou moto")
	case !inspections.ValidPlate(in.Plate):
		return invalid("informe a placa do veículo (ABC1D23 ou ABC1234)")
	case in.Model == "" || utf8.RuneCountInString(in.Model) > 60:
		return invalid("informe o modelo do veículo")
	case utf8.RuneCountInString(in.Brand) > 40 || utf8.RuneCountInString(in.Color) > 30:
		return invalid("a marca vai até 40 caracteres e a cor, até 30")
	case in.Year != nil && (*in.Year < 1950 || *in.Year > time.Now().Year()+1):
		return invalid("o ano do veículo parece errado")
	}
	address, err := addresses.Normalize(in.Address)
	if err != nil {
		return &InputError{err.Error()}
	}
	in.Address = address
	return nil
}

// Start abre a adesão pelo link da organização: confere os dados, pede a
// vistoria de adesão (o link dela abre na sequência) e devolve o link de
// acompanhamento.
func (s *Service) Start(ctx context.Context, orgToken string, in Input) (*Adhesion, error) {
	org, err := s.OrgByToken(ctx, orgToken)
	if err != nil {
		return nil, err
	}
	if !org.Enabled {
		return nil, ErrDisabled
	}
	if err := clean(&in); err != nil {
		return nil, err
	}
	var taken bool
	if err := s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE lower(email) = $1)`, in.Email).Scan(&taken); err != nil {
		return nil, database.MapError(err)
	}
	if taken {
		return nil, invalid("esse e-mail já tem cadastro na Farbo: fale com a %s para incluir o veículo", org.Name)
	}
	var open bool
	if err := s.db.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM adhesions WHERE organization_id = $1 AND plate = $2 AND status IN ('pending', 'submitted'))`,
		org.ID, in.Plate).Scan(&open); err != nil {
		return nil, database.MapError(err)
	}
	if open {
		return nil, invalid("esta placa já tem uma adesão em andamento: use o link de acompanhamento que você recebeu")
	}
	desc := strings.TrimSpace(strings.Join([]string{in.Brand, in.Model, yearText(in.Year), in.Color}, " "))
	inspection, err := s.inspections.Request(ctx, org.ID, uuid.Nil, inspections.RequestInput{
		Kind: inspections.KindEntry, Mode: inspections.ModeSelf, VehicleType: in.VehicleType, PersonName: in.Name,
		PersonPhone: in.Phone, Plate: in.Plate, VehicleDesc: desc, Note: "Vistoria da adesão pelo link.",
	})
	if err != nil {
		var input *inspections.InputError
		if errors.As(err, &input) {
			return nil, &InputError{input.Message}
		}
		return nil, err
	}
	var id uuid.UUID
	err = s.db.QueryRow(ctx, `
		INSERT INTO adhesions (organization_id, name, document, email, phone, address, vehicle_type, plate, brand, model, year,
			color, inspection_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13) RETURNING id`,
		org.ID, in.Name, in.Document, in.Email, in.Phone, in.Address, in.VehicleType, in.Plate, in.Brand, in.Model, in.Year,
		in.Color, inspection.ID).Scan(&id)
	if err != nil {
		if _, cancelErr := s.inspections.Cancel(ctx, inspection.ID); cancelErr != nil {
			s.log.Warn("a vistoria da adesão que não abriu ficou em aberto", "inspection", inspection.ID, "err", cancelErr)
		}
		if errors.Is(database.MapError(err), database.ErrConflict) {
			return nil, invalid("esta placa já tem uma adesão em andamento: use o link de acompanhamento que você recebeu")
		}
		return nil, database.MapError(err)
	}
	return s.get(ctx, `a.id = $1`, id)
}

func yearText(year *int) string {
	if year == nil {
		return ""
	}
	return fmt.Sprint(*year)
}

// ByFollowToken é a adesão do link de acompanhamento.
func (s *Service) ByFollowToken(ctx context.Context, token string) (*Adhesion, error) {
	id, ok := verify(s.followKey, token)
	if !ok {
		return nil, database.ErrNotFound
	}
	return s.get(ctx, `a.id = $1`, id)
}

// AcceptInput é o aceite do termo do associado (a versão e o hash do texto
// em vigor vêm do servidor), com a preferência para a instalação.
type AcceptInput struct {
	TermVersion, TermSHA256 string
	Preference              string
	IP, UserAgent           string
}

// Accept registra o aceite do termo e a preferência da instalação, depois
// da vistoria enviada: a adesão passa a esperar a associação.
func (s *Service) Accept(ctx context.Context, token string, in AcceptInput) (*Adhesion, error) {
	a, err := s.ByFollowToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if a.Status != StatusPending {
		return nil, invalid("a adesão já foi enviada para a associação")
	}
	if a.Inspection == nil || (a.Inspection.Status != inspections.StatusSubmitted && a.Inspection.Status != inspections.StatusApproved) {
		return nil, invalid("faça a vistoria do veículo antes: o termo vem depois")
	}
	in.Preference = strings.TrimSpace(in.Preference)
	if utf8.RuneCountInString(in.Preference) > MaxPreference {
		return nil, invalid("a preferência vai até %d caracteres", MaxPreference)
	}
	if len(in.UserAgent) > 500 {
		in.UserAgent = in.UserAgent[:500]
	}
	tag, err := s.db.Exec(ctx, `
		UPDATE adhesions SET status = 'submitted', install_preference = $2, term_version = $3, term_sha256 = $4,
			accepted_at = NOW(), accept_ip = $5, accept_user_agent = $6, updated_at = NOW()
		WHERE id = $1 AND status = 'pending'`, a.ID, in.Preference, in.TermVersion, in.TermSHA256, in.IP, in.UserAgent)
	if err != nil {
		return nil, database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return nil, invalid("a adesão já foi enviada para a associação")
	}
	return s.get(ctx, `a.id = $1`, a.ID)
}

// ---------------------------------------------------------------------------
// A associação
// ---------------------------------------------------------------------------

const selectAdhesion = `
	SELECT a.id, a.organization_id, ou.name, a.status, a.name, a.document, a.email, a.phone, a.address, a.vehicle_type,
		a.plate, a.brand, a.model, a.year, a.color, a.install_preference, a.term_version, a.accepted_at, a.associate_id,
		a.vehicle_id, a.review_note, COALESCE(rv.name, ''), a.reviewed_at, a.created_at, a.inspection_id, a.accept_ip,
		a.accept_user_agent
	FROM adhesions a JOIN users ou ON ou.id = a.organization_id LEFT JOIN users rv ON rv.id = a.reviewed_by`

func scan(row database.Scanner) (*Adhesion, error) {
	var a Adhesion
	if err := row.Scan(&a.ID, &a.OrganizationID, &a.OrganizationName, &a.Status, &a.Name, &a.Document, &a.Email, &a.Phone,
		&a.Address, &a.VehicleType, &a.Plate, &a.Brand, &a.Model, &a.Year, &a.Color, &a.InstallPref, &a.TermVersion,
		&a.AcceptedAt, &a.AssociateID, &a.VehicleID, &a.ReviewNote, &a.ReviewedBy, &a.ReviewedAt, &a.CreatedAt,
		&a.inspectionRoot, &a.acceptIP, &a.acceptUA); err != nil {
		return nil, database.MapError(err)
	}
	return &a, nil
}

// decorate completa com a vistoria mais nova e o link de acompanhamento.
func (s *Service) decorate(ctx context.Context, a *Adhesion) error {
	a.FollowURL = s.FollowURL(a.ID)
	if a.inspectionRoot == nil {
		return nil
	}
	in, err := s.inspections.Latest(ctx, *a.inspectionRoot)
	if err != nil && !errors.Is(err, database.ErrNotFound) {
		return err
	}
	a.Inspection = in
	return nil
}

func (s *Service) get(ctx context.Context, where string, args ...any) (*Adhesion, error) {
	a, err := scan(s.db.QueryRow(ctx, selectAdhesion+` WHERE `+where, args...))
	if err != nil {
		return nil, err
	}
	return a, s.decorate(ctx, a)
}

// Get é a adesão da organização.
func (s *Service) Get(ctx context.Context, orgID, id uuid.UUID) (*Adhesion, error) {
	return s.get(ctx, `a.id = $1 AND a.organization_id = $2`, id, orgID)
}

// List são as adesões da organização, das mais novas (no máximo 500).
func (s *Service) List(ctx context.Context, orgID uuid.UUID, status string) ([]*Adhesion, error) {
	rows, err := s.db.Query(ctx, selectAdhesion+` WHERE a.organization_id = $1 AND ($2 = '' OR a.status = $2)
		ORDER BY a.created_at DESC LIMIT 500`, orgID, status)
	if err != nil {
		return nil, database.MapError(err)
	}
	var out []*Adhesion
	for rows.Next() {
		a, err := scan(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, database.MapError(err)
	}
	for _, a := range out {
		if err := s.decorate(ctx, a); err != nil {
			return nil, err
		}
	}
	if out == nil {
		out = []*Adhesion{}
	}
	return out, nil
}

// Approve aprova a adesão enviada: aprova a vistoria (se ainda estiver em
// revisão), cadastra o associado (o convite do app vai por e-mail), o
// veículo e o endereço da instalação, e liga a vistoria ao veículo. Parada
// no meio (o veículo recusado), aprovar de novo continua de onde parou.
func (s *Service) Approve(ctx context.Context, orgID, id, by uuid.UUID) (*Adhesion, error) {
	a, err := s.Get(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	if a.Status != StatusSubmitted {
		return nil, invalid("só dá para aprovar a adesão enviada (com a vistoria e o termo)")
	}
	switch {
	case a.Inspection == nil:
		return nil, invalid("a adesão está sem a vistoria")
	case a.Inspection.Status == inspections.StatusSubmitted:
		if _, err := s.inspections.Review(ctx, a.Inspection.ID, by, true, ""); err != nil {
			return nil, err
		}
	case a.Inspection.Status != inspections.StatusApproved:
		return nil, invalid("a vistoria foi devolvida e ainda não foi refeita: espere a refação para aprovar")
	}

	associate := a.AssociateID
	if associate == nil {
		created, err := s.orgs.CreateAssociate(ctx, orgID, organizations.AssociateInput{
			Name: a.Name, Email: a.Email, Phone: a.Phone, Document: a.Document,
		})
		if err != nil {
			return nil, err
		}
		associate = &created.ID
		if _, err := s.db.Exec(ctx, `UPDATE adhesions SET associate_id = $2, updated_at = NOW() WHERE id = $1`, a.ID, created.ID); err != nil {
			return nil, database.MapError(err)
		}
	}
	vehicle := a.VehicleID
	if vehicle == nil {
		kind := vehicles.KindCar
		if a.VehicleType == inspections.VehicleMotorcycle {
			kind = vehicles.KindMotorcycle
		}
		created, err := s.orgs.RegisterVehicle(ctx, orgID, *associate, vehicles.Input{
			Name: a.VehicleName(), Kind: kind, Plate: a.Plate, Brand: a.Brand, Model: a.Model, Year: a.Year, Color: a.Color,
		})
		if err != nil {
			return nil, err
		}
		vehicle = &created.ID
		if _, err := s.db.Exec(ctx, `UPDATE adhesions SET vehicle_id = $2, updated_at = NOW() WHERE id = $1`, a.ID, created.ID); err != nil {
			return nil, database.MapError(err)
		}
	}
	if _, err := s.addresses.Save(ctx, *associate, a.Address); err != nil {
		return nil, err
	}
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if a.inspectionRoot != nil {
			if _, err := tx.Exec(ctx, `
				UPDATE inspections SET associate_id = $2, vehicle_id = $3, updated_at = NOW()
				WHERE (id = $1 OR redo_of = $1) AND status NOT IN ('pending', 'submitted')`,
				*a.inspectionRoot, *associate, *vehicle); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `
			UPDATE adhesions SET status = 'approved', reviewed_by = $2, reviewed_at = NOW(), updated_at = NOW()
			WHERE id = $1 AND status = 'submitted'`, a.ID, nullable(by))
		return err
	})
	if err != nil {
		return nil, database.MapError(err)
	}
	return s.Get(ctx, orgID, id)
}

// Reject recusa a adesão (pendente ou enviada), com o motivo que a pessoa vê.
func (s *Service) Reject(ctx context.Context, orgID, id, by uuid.UUID, note string) (*Adhesion, error) {
	note = strings.TrimSpace(note)
	if note == "" || utf8.RuneCountInString(note) > MaxNote {
		return nil, invalid("diga o motivo (até %d caracteres): a pessoa vê no link de acompanhamento", MaxNote)
	}
	a, err := s.Get(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	tag, err := s.db.Exec(ctx, `
		UPDATE adhesions SET status = 'rejected', review_note = $2, reviewed_by = $3, reviewed_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND status IN ('pending', 'submitted') AND associate_id IS NULL`, a.ID, note, nullable(by))
	if err != nil {
		return nil, database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return nil, invalid("só dá para recusar a adesão em andamento (antes do cadastro)")
	}
	if a.Inspection != nil && a.Inspection.Status == inspections.StatusPending {
		if _, err := s.inspections.Cancel(ctx, a.Inspection.ID); err != nil {
			s.log.Warn("a vistoria da adesão recusada ficou em aberto", "inspection", a.Inspection.ID, "err", err)
		}
	}
	return s.Get(ctx, orgID, id)
}

func nullable(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}
