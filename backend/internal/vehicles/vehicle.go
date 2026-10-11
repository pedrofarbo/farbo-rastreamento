// Package vehicles trata o cadastro dos veículos e o vínculo com o rastreador.
package vehicles

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// Os tipos de veículo: o mapa desenha um carro ou uma moto.
const (
	KindCar        = "CAR"
	KindMotorcycle = "MOTORCYCLE"
)

type Vehicle struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	// Kind é o tipo: CAR ou MOTORCYCLE.
	Kind  string `json:"kind"`
	Plate string `json:"plate"`
	Brand string `json:"brand"`
	Model string `json:"model"`
	Year  *int   `json:"year"`
	Color string `json:"color"`

	// SpeedLimitKmh nulo faz o serviço usar o limite global (§21).
	SpeedLimitKmh *float64 `json:"speedLimitKmh"`

	DeviceID *uuid.UUID `json:"deviceId"`

	// OwnerID é o cliente dono do veículo; nulo é veículo da central.
	OwnerID *uuid.UUID `json:"ownerId"`

	// HistoryRetentionDays é a exceção do veículo (7, 14 ou 30 dias); nulo
	// segue o cliente (ver o pacote retention).
	HistoryRetentionDays *int `json:"historyRetentionDays"`

	// GroupID é o grupo do veículo na conta (nulo: sem grupo); Custom1 e
	// Custom2, os dois campos personalizados que a conta nomeia.
	GroupID *uuid.UUID `json:"groupId"`
	Custom1 string     `json:"custom1"`
	Custom2 string     `json:"custom2"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type Input struct {
	Name string `json:"name"`
	// Kind: CAR ou MOTORCYCLE. Vazio é carro na criação e, na edição, mantém o
	// tipo atual (tela que não mostra o campo não muda o tipo).
	Kind          string     `json:"kind"`
	Plate         string     `json:"plate"`
	Brand         string     `json:"brand"`
	Model         string     `json:"model"`
	Year          *int       `json:"year"`
	Color         string     `json:"color"`
	SpeedLimitKmh *float64   `json:"speedLimitKmh"`
	DeviceID      *uuid.UUID `json:"deviceId"`
	// OwnerID só é lido na criação feita pela central; a edição não troca o
	// dono de um veículo.
	OwnerID *uuid.UUID `json:"ownerId"`
	// GroupID: o grupo da conta do dono ("" tira do grupo). Custom1/Custom2:
	// os campos personalizados. Ausentes (nulos), a edição mantém o que já
	// tinha — a tela que não mostra os campos não os apaga.
	GroupID *string `json:"groupId"`
	Custom1 *string `json:"custom1"`
	Custom2 *string `json:"custom2"`
}

const columns = `id, name, kind, COALESCE(plate, ''), COALESCE(brand, ''), COALESCE(model, ''), year,
	COALESCE(color, ''), speed_limit_kmh, device_id, owner_id, history_retention_days, group_id, custom1, custom2,
	created_at, updated_at`

type Repository struct{ db *database.DB }

func NewRepository(db *database.DB) *Repository { return &Repository{db: db} }

func scan(row database.Scanner) (*Vehicle, error) {
	var v Vehicle
	err := row.Scan(&v.ID, &v.Name, &v.Kind, &v.Plate, &v.Brand, &v.Model, &v.Year, &v.Color,
		&v.SpeedLimitKmh, &v.DeviceID, &v.OwnerID, &v.HistoryRetentionDays, &v.GroupID, &v.Custom1, &v.Custom2,
		&v.CreatedAt, &v.UpdatedAt)
	if err != nil {
		return nil, database.MapError(err)
	}
	return &v, nil
}

// O grupo só entra se for da conta do dono do veículo: o de outra conta vira
// "sem grupo".
const insertVehicle = `
	INSERT INTO vehicles (name, plate, brand, model, year, color, speed_limit_kmh, device_id, owner_id, kind,
		group_id, custom1, custom2)
	VALUES ($1, NULLIF($2, ''), $3, $4, $5, $6, $7, $8, $9, COALESCE(NULLIF($10, ''), 'CAR'),
		(SELECT g.id FROM vehicle_groups g WHERE g.id = NULLIF($11, '')::uuid AND g.owner_id = $9),
		COALESCE($12, ''), COALESCE($13, ''))
	RETURNING ` + columns

func insertArgs(in Input) []any {
	return []any{in.Name, strings.ToUpper(strings.TrimSpace(in.Plate)), in.Brand, in.Model,
		in.Year, in.Color, in.SpeedLimitKmh, in.DeviceID, in.OwnerID, normalizeKind(in.Kind),
		in.GroupID, trimmed(in.Custom1), trimmed(in.Custom2)}
}

func trimmed(s *string) *string {
	if s == nil {
		return nil
	}
	t := strings.TrimSpace(*s)
	return &t
}

// normalizeKind aceita o tipo em qualquer caixa; vazio continua vazio.
func normalizeKind(kind string) string {
	return strings.ToUpper(strings.TrimSpace(kind))
}

func (r *Repository) Create(ctx context.Context, in Input) (*Vehicle, error) {
	return InsertWith(ctx, r.db, in)
}

// InsertWith grava o veículo pelo Querier informado (pool ou transação). Não
// valida: quem chama usa Validate antes.
func InsertWith(ctx context.Context, q database.Querier, in Input) (*Vehicle, error) {
	return scan(q.QueryRow(ctx, insertVehicle, insertArgs(in)...))
}

// CountAwaitingInstall conta os veículos do cliente ainda não instalados: sem
// aparelho, ou com um aparelho que nunca deu sinal.
func (r *Repository) CountAwaitingInstall(ctx context.Context, ownerID uuid.UUID) (int, error) {
	var n int
	err := r.db.QueryRow(ctx,
		`SELECT count(*) FROM vehicles v
		 LEFT JOIN devices d ON d.id = v.device_id
		 WHERE v.owner_id = $1 AND d.last_seen_at IS NULL`, ownerID).Scan(&n)
	return n, database.MapError(err)
}

func (r *Repository) Update(ctx context.Context, id uuid.UUID, in Input) (*Vehicle, error) {
	return scan(r.db.QueryRow(ctx, `
		UPDATE vehicles SET name = $2, plate = NULLIF($3, ''), brand = $4, model = $5,
			year = $6, color = $7, speed_limit_kmh = $8, device_id = $9,
			kind = COALESCE(NULLIF($10, ''), kind),
			group_id = CASE WHEN $11::text IS NULL THEN group_id ELSE (
				SELECT g.id FROM vehicle_groups g WHERE g.id = NULLIF($11, '')::uuid AND g.owner_id = vehicles.owner_id) END,
			custom1 = COALESCE($12, custom1), custom2 = COALESCE($13, custom2), updated_at = NOW()
		WHERE id = $1
		RETURNING `+columns,
		id, in.Name, strings.ToUpper(strings.TrimSpace(in.Plate)), in.Brand, in.Model,
		in.Year, in.Color, in.SpeedLimitKmh, in.DeviceID, normalizeKind(in.Kind),
		in.GroupID, trimmed(in.Custom1), trimmed(in.Custom2)))
}

func (r *Repository) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.db.Exec(ctx, `DELETE FROM vehicles WHERE id = $1`, id)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return database.ErrNotFound
	}
	return nil
}

func (r *Repository) GetByID(ctx context.Context, id uuid.UUID) (*Vehicle, error) {
	return scan(r.db.QueryRow(ctx, `SELECT `+columns+` FROM vehicles WHERE id = $1`, id))
}

func (r *Repository) GetByDeviceID(ctx context.Context, deviceID uuid.UUID) (*Vehicle, error) {
	return scan(r.db.QueryRow(ctx, `SELECT `+columns+` FROM vehicles WHERE device_id = $1`, deviceID))
}

func (r *Repository) List(ctx context.Context) ([]*Vehicle, error) {
	return r.list(ctx, `SELECT `+columns+` FROM vehicles ORDER BY name`)
}

// ListByOwner devolve só os veículos de um cliente.
func (r *Repository) ListByOwner(ctx context.Context, ownerID uuid.UUID) ([]*Vehicle, error) {
	return r.list(ctx, `SELECT `+columns+` FROM vehicles WHERE owner_id = $1 ORDER BY name`, ownerID)
}

func (r *Repository) list(ctx context.Context, query string, args ...any) ([]*Vehicle, error) {
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()

	out := []*Vehicle{}
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

type Service struct{ repo *Repository }

func NewService(repo *Repository) *Service { return &Service{repo: repo} }

func (s *Service) List(ctx context.Context) ([]*Vehicle, error) { return s.repo.List(ctx) }

func (s *Service) ListByOwner(ctx context.Context, ownerID uuid.UUID) ([]*Vehicle, error) {
	return s.repo.ListByOwner(ctx, ownerID)
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (*Vehicle, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *Service) GetByDeviceID(ctx context.Context, deviceID uuid.UUID) (*Vehicle, error) {
	return s.repo.GetByDeviceID(ctx, deviceID)
}

func (s *Service) Create(ctx context.Context, in Input) (*Vehicle, error) {
	if err := Validate(in); err != nil {
		return nil, err
	}
	return s.repo.Create(ctx, in)
}

func (s *Service) Update(ctx context.Context, id uuid.UUID, in Input) (*Vehicle, error) {
	if err := Validate(in); err != nil {
		return nil, err
	}
	return s.repo.Update(ctx, id, in)
}

func (s *Service) Delete(ctx context.Context, id uuid.UUID) error { return s.repo.Delete(ctx, id) }

// ValidationError descreve um payload recusado.
type ValidationError struct{ Message string }

func (e ValidationError) Error() string { return e.Message }

// CountAwaitingInstall conta os veículos do cliente ainda não instalados: sem
// aparelho, ou com um aparelho que nunca deu sinal.
func (s *Service) CountAwaitingInstall(ctx context.Context, ownerID uuid.UUID) (int, error) {
	return s.repo.CountAwaitingInstall(ctx, ownerID)
}

// Validate confere os dados do veículo antes de gravar.
func Validate(in Input) error {
	if strings.TrimSpace(in.Name) == "" {
		return ValidationError{Message: "o nome do veículo é obrigatório"}
	}
	if len(in.Name) > 100 {
		return ValidationError{Message: "nome do veículo longo demais"}
	}
	if k := normalizeKind(in.Kind); k != "" && k != KindCar && k != KindMotorcycle {
		return ValidationError{Message: "tipo do veículo: carro (CAR) ou moto (MOTORCYCLE)"}
	}
	if in.Year != nil && (*in.Year < 1900 || *in.Year > time.Now().Year()+1) {
		return ValidationError{Message: fmt.Sprintf("ano fora da faixa 1900..%d", time.Now().Year()+1)}
	}
	if in.SpeedLimitKmh != nil && (*in.SpeedLimitKmh < 0 || *in.SpeedLimitKmh > 300) {
		return ValidationError{Message: "limite de velocidade fora da faixa 0..300 km/h"}
	}
	if in.GroupID != nil && *in.GroupID != "" {
		if _, err := uuid.Parse(*in.GroupID); err != nil {
			return ValidationError{Message: "grupo inválido"}
		}
	}
	for _, c := range []*string{in.Custom1, in.Custom2} {
		if c != nil && utf8.RuneCountInString(strings.TrimSpace(*c)) > MaxCustomLength {
			return ValidationError{Message: fmt.Sprintf("os campos personalizados vão até %d caracteres", MaxCustomLength)}
		}
	}
	return nil
}
