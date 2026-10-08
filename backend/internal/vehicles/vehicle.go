// Package vehicles trata o cadastro dos veículos e o vínculo com o rastreador.
package vehicles

import (
	"context"
	"fmt"
	"strings"
	"time"

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
}

const columns = `id, name, kind, COALESCE(plate, ''), COALESCE(brand, ''), COALESCE(model, ''), year,
	COALESCE(color, ''), speed_limit_kmh, device_id, owner_id, history_retention_days, created_at, updated_at`

type Repository struct{ db *database.DB }

func NewRepository(db *database.DB) *Repository { return &Repository{db: db} }

func scan(row database.Scanner) (*Vehicle, error) {
	var v Vehicle
	err := row.Scan(&v.ID, &v.Name, &v.Kind, &v.Plate, &v.Brand, &v.Model, &v.Year, &v.Color,
		&v.SpeedLimitKmh, &v.DeviceID, &v.OwnerID, &v.HistoryRetentionDays, &v.CreatedAt, &v.UpdatedAt)
	if err != nil {
		return nil, database.MapError(err)
	}
	return &v, nil
}

const insertVehicle = `
	INSERT INTO vehicles (name, plate, brand, model, year, color, speed_limit_kmh, device_id, owner_id, kind)
	VALUES ($1, NULLIF($2, ''), $3, $4, $5, $6, $7, $8, $9, COALESCE(NULLIF($10, ''), 'CAR'))
	RETURNING ` + columns

func insertArgs(in Input) []any {
	return []any{in.Name, strings.ToUpper(strings.TrimSpace(in.Plate)), in.Brand, in.Model,
		in.Year, in.Color, in.SpeedLimitKmh, in.DeviceID, in.OwnerID, normalizeKind(in.Kind)}
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
			kind = COALESCE(NULLIF($10, ''), kind), updated_at = NOW()
		WHERE id = $1
		RETURNING `+columns,
		id, in.Name, strings.ToUpper(strings.TrimSpace(in.Plate)), in.Brand, in.Model,
		in.Year, in.Color, in.SpeedLimitKmh, in.DeviceID, normalizeKind(in.Kind)))
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
	return nil
}
