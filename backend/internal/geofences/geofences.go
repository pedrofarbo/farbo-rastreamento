// Package geofences trata as cercas circulares e a avaliação de entrada/saída.
//
// Há dois tipos de cerca. A da central (sem dono) vale para todos os veículos.
// A do cliente vale só para os veículos dele que ele escolheu, e só ele a vê e
// recebe os avisos de entrada e saída.
package geofences

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

type Geofence struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`

	Latitude     float64 `json:"latitude"`
	Longitude    float64 `json:"longitude"`
	RadiusMeters float64 `json:"radiusMeters"`

	Active bool `json:"active"`

	// OwnerID é o cliente dono; nulo é cerca da central (todos os veículos).
	OwnerID *uuid.UUID `json:"ownerId"`
	// VehicleIDs são os veículos do cliente vigiados pela cerca (vazio nas
	// cercas da central).
	VehicleIDs []uuid.UUID `json:"vehicleIds"`
	// NotifyEnter/NotifyExit: o cliente recebe aviso na entrada/saída.
	NotifyEnter bool `json:"notifyEnter"`
	NotifyExit  bool `json:"notifyExit"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type Input struct {
	Name         string      `json:"name"`
	Latitude     float64     `json:"latitude"`
	Longitude    float64     `json:"longitude"`
	RadiusMeters float64     `json:"radiusMeters"`
	Active       *bool       `json:"active"`
	VehicleIDs   []uuid.UUID `json:"vehicleIds"`
	NotifyEnter  *bool       `json:"notifyEnter"`
	NotifyExit   *bool       `json:"notifyExit"`
}

// Limites das cercas do cliente. O raio mínimo fica acima do erro do GPS:
// menor que isso, o veículo parado "entraria e sairia" sozinho.
const (
	MaxPerCustomer     = 20
	CustomerMinRadiusM = 50
	CustomerMaxRadiusM = 50000

	centralMinRadiusM = 20
	centralMaxRadiusM = 200000
	maxNameLength     = 100
)

const (
	columns = `g.id, g.name, g.latitude, g.longitude, g.radius_meters, g.active, g.owner_id,
		g.notify_enter, g.notify_exit,
		COALESCE((SELECT array_agg(gv.vehicle_id ORDER BY gv.vehicle_id)
		          FROM geofence_vehicles gv WHERE gv.geofence_id = g.id), '{}'),
		g.created_at, g.updated_at`
	// ownerMatches compara o dono com um parâmetro que pode ser nulo (central).
	ownerMatches = `g.owner_id IS NOT DISTINCT FROM `
)

type Repository struct{ db *database.DB }

func NewRepository(db *database.DB) *Repository { return &Repository{db: db} }

func scan(row database.Scanner) (*Geofence, error) {
	var g Geofence
	err := row.Scan(&g.ID, &g.Name, &g.Latitude, &g.Longitude, &g.RadiusMeters,
		&g.Active, &g.OwnerID, &g.NotifyEnter, &g.NotifyExit, &g.VehicleIDs, &g.CreatedAt, &g.UpdatedAt)
	if err != nil {
		return nil, database.MapError(err)
	}
	if g.VehicleIDs == nil {
		g.VehicleIDs = []uuid.UUID{}
	}
	return &g, nil
}

func flag(v *bool) bool { return v == nil || *v }

// Create grava a cerca e os veículos dela numa transação. owner nulo é cerca
// da central.
func (r *Repository) Create(ctx context.Context, owner *uuid.UUID, in Input) (*Geofence, error) {
	var out *Geofence
	err := pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO geofences (name, latitude, longitude, radius_meters, active, owner_id, notify_enter, notify_exit)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`,
			in.Name, in.Latitude, in.Longitude, in.RadiusMeters, flag(in.Active), owner,
			flag(in.NotifyEnter), flag(in.NotifyExit),
		).Scan(&id); err != nil {
			return database.MapError(err)
		}
		if err := setVehicles(ctx, tx, id, owner, in.VehicleIDs); err != nil {
			return err
		}
		var err error
		out, err = scan(tx.QueryRow(ctx, `SELECT `+columns+` FROM geofences g WHERE g.id = $1`, id))
		return err
	})
	return out, err
}

// Update troca tudo da cerca. Só mexe na cerca do mesmo dono (owner nulo =
// central): a de outro cliente é "não encontrada".
func (r *Repository) Update(ctx context.Context, id uuid.UUID, owner *uuid.UUID, in Input) (*Geofence, error) {
	var out *Geofence
	err := pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE geofences g SET name = $3, latitude = $4, longitude = $5, radius_meters = $6,
				active = $7, notify_enter = $8, notify_exit = $9, updated_at = NOW()
			WHERE g.id = $1 AND `+ownerMatches+`$2`,
			id, owner, in.Name, in.Latitude, in.Longitude, in.RadiusMeters, flag(in.Active),
			flag(in.NotifyEnter), flag(in.NotifyExit))
		if err != nil {
			return database.MapError(err)
		}
		if tag.RowsAffected() == 0 {
			return database.ErrNotFound
		}
		if _, err := tx.Exec(ctx, `DELETE FROM geofence_vehicles WHERE geofence_id = $1`, id); err != nil {
			return database.MapError(err)
		}
		if err := setVehicles(ctx, tx, id, owner, in.VehicleIDs); err != nil {
			return err
		}
		out, err = scan(tx.QueryRow(ctx, `SELECT `+columns+` FROM geofences g WHERE g.id = $1`, id))
		return err
	})
	return out, err
}

// setVehicles liga a cerca aos veículos, que precisam ser do dono dela.
func setVehicles(ctx context.Context, tx pgx.Tx, id uuid.UUID, owner *uuid.UUID, vehicleIDs []uuid.UUID) error {
	if owner == nil || len(vehicleIDs) == 0 {
		return nil
	}
	tag, err := tx.Exec(ctx, `
		INSERT INTO geofence_vehicles (geofence_id, vehicle_id)
		SELECT $1, v.id FROM vehicles v WHERE v.id = ANY($2) AND v.owner_id = $3`,
		id, vehicleIDs, *owner)
	if err != nil {
		return database.MapError(err)
	}
	if int(tag.RowsAffected()) != len(vehicleIDs) {
		return ValidationError{Message: "veículo não encontrado entre os seus"}
	}
	return nil
}

func (r *Repository) Delete(ctx context.Context, id uuid.UUID, owner *uuid.UUID) error {
	tag, err := r.db.Exec(ctx, `DELETE FROM geofences g WHERE g.id = $1 AND `+ownerMatches+`$2`, id, owner)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return database.ErrNotFound
	}
	return nil
}

// List devolve todas as cercas, da central e dos clientes (para a avaliação).
func (r *Repository) List(ctx context.Context) ([]*Geofence, error) {
	return r.list(ctx, `SELECT `+columns+` FROM geofences g ORDER BY g.name`)
}

// ListByOwner devolve as cercas de um cliente (owner nulo = as da central).
func (r *Repository) ListByOwner(ctx context.Context, owner *uuid.UUID) ([]*Geofence, error) {
	return r.list(ctx, `SELECT `+columns+` FROM geofences g WHERE `+ownerMatches+`$1 ORDER BY g.name`, owner)
}

func (r *Repository) CountByOwner(ctx context.Context, owner uuid.UUID) (int, error) {
	var n int
	err := r.db.QueryRow(ctx, `SELECT count(*) FROM geofences WHERE owner_id = $1`, owner).Scan(&n)
	return n, database.MapError(err)
}

func (r *Repository) list(ctx context.Context, query string, args ...any) ([]*Geofence, error) {
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()

	out := []*Geofence{}
	for rows.Next() {
		g, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// Subject é o veículo avaliado: o id e o dono atual (nulos para rastreador
// sem veículo ou veículo da central).
type Subject struct {
	VehicleID *uuid.UUID
	OwnerID   *uuid.UUID
}

// AppliesTo diz se a cerca vigia o veículo: a da central vigia todos; a do
// cliente, só os veículos escolhidos que continuam sendo dele.
func (g *Geofence) AppliesTo(v Subject) bool {
	if g.OwnerID == nil {
		return true
	}
	if v.VehicleID == nil || v.OwnerID == nil || *v.OwnerID != *g.OwnerID {
		return false
	}
	return slices.Contains(g.VehicleIDs, *v.VehicleID)
}

// Contains diz se o ponto está dentro do círculo.
func (g *Geofence) Contains(lat, lon float64) bool {
	return DistanceMeters(lat, lon, g.Latitude, g.Longitude) <= g.RadiusMeters
}

// Service mantém as cercas ativas em memória: a avaliação roda a cada posição
// recebida e não pode depender de uma consulta ao banco.
type Service struct {
	repo *Repository

	// Baseline é chamado ao salvar uma cerca, antes de ela valer para a
	// ingestão: marca os veículos que já estão dentro, para que criar a cerca
	// "Casa" com o carro na garagem não avise "entrou em Casa" (nem mudar o
	// raio avise "saiu"). Opcional.
	Baseline func(ctx context.Context, g *Geofence)

	mu     sync.RWMutex
	cached map[uuid.UUID]*Geofence
}

func NewService(repo *Repository) *Service { return &Service{repo: repo} }

// Refresh recarrega o cache a partir do banco.
func (s *Service) Refresh(ctx context.Context) error {
	all, err := s.repo.List(ctx)
	if err != nil {
		return err
	}
	active := make(map[uuid.UUID]*Geofence, len(all))
	for _, g := range all {
		if g.Active {
			active[g.ID] = g
		}
	}
	s.mu.Lock()
	s.cached = active
	s.mu.Unlock()
	return nil
}

// ListByOwner devolve as cercas de um cliente; owner nulo, as da central.
func (s *Service) ListByOwner(ctx context.Context, owner *uuid.UUID) ([]*Geofence, error) {
	return s.repo.ListByOwner(ctx, owner)
}

// Create cria a cerca da central (owner nulo) ou de um cliente.
func (s *Service) Create(ctx context.Context, owner *uuid.UUID, in Input) (*Geofence, error) {
	in, err := normalize(in, owner)
	if err != nil {
		return nil, err
	}
	if owner != nil {
		count, err := s.repo.CountByOwner(ctx, *owner)
		if err != nil {
			return nil, err
		}
		if count >= MaxPerCustomer {
			return nil, ValidationError{Message: fmt.Sprintf("limite de %d cercas por conta: apague uma para criar outra", MaxPerCustomer)}
		}
	}
	g, err := s.repo.Create(ctx, owner, in)
	if err != nil {
		return nil, err
	}
	s.saved(ctx, g)
	return g, nil
}

func (s *Service) Update(ctx context.Context, id uuid.UUID, owner *uuid.UUID, in Input) (*Geofence, error) {
	in, err := normalize(in, owner)
	if err != nil {
		return nil, err
	}
	g, err := s.repo.Update(ctx, id, owner, in)
	if err != nil {
		return nil, err
	}
	s.saved(ctx, g)
	return g, nil
}

func (s *Service) saved(ctx context.Context, g *Geofence) {
	if s.Baseline != nil {
		s.Baseline(ctx, g)
	}
	_ = s.Refresh(ctx)
}

func (s *Service) Delete(ctx context.Context, id uuid.UUID, owner *uuid.UUID) error {
	if err := s.repo.Delete(ctx, id, owner); err != nil {
		return err
	}
	return s.Refresh(ctx)
}

// Inside devolve os IDs das cercas que vigiam o veículo e contêm o ponto.
func (s *Service) Inside(v Subject, lat, lon float64) []uuid.UUID {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	var out []uuid.UUID
	for _, g := range s.cached {
		if g.AppliesTo(v) && g.Contains(lat, lon) {
			out = append(out, g.ID)
		}
	}
	slices.SortFunc(out, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	return out
}

// Lookup devolve a cerca ativa (cópia), se existir.
func (s *Service) Lookup(id uuid.UUID) (*Geofence, bool) {
	if s == nil {
		return nil, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	g, ok := s.cached[id]
	if !ok {
		return nil, false
	}
	copied := *g
	return &copied, true
}

// Applies diz se a cerca continua ativa e vigiando o veículo. Saída de cerca
// apagada, desligada ou da qual o veículo foi tirado não é saída de verdade.
func (s *Service) Applies(id uuid.UUID, v Subject) bool {
	g, ok := s.Lookup(id)
	return ok && g.AppliesTo(v)
}

// ValidationError descreve um payload recusado.
type ValidationError struct{ Message string }

func (e ValidationError) Error() string { return e.Message }

// normalize valida e arruma o que veio da tela.
func normalize(in Input, owner *uuid.UUID) (Input, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return in, ValidationError{Message: "o nome da cerca é obrigatório"}
	}
	if utf8.RuneCountInString(in.Name) > maxNameLength {
		return in, ValidationError{Message: fmt.Sprintf("o nome da cerca pode ter até %d caracteres", maxNameLength)}
	}
	if math.IsNaN(in.Latitude) || math.IsNaN(in.Longitude) ||
		in.Latitude < -90 || in.Latitude > 90 || in.Longitude < -180 || in.Longitude > 180 {
		return in, ValidationError{Message: "coordenadas da cerca fora de faixa"}
	}
	if owner == nil {
		if in.RadiusMeters < centralMinRadiusM || in.RadiusMeters > centralMaxRadiusM {
			return in, ValidationError{Message: "raio fora da faixa 20..200000 metros"}
		}
		if len(in.VehicleIDs) > 0 {
			return in, ValidationError{Message: "a cerca da central vale para todos os veículos: não escolha veículos"}
		}
		return in, nil
	}
	if in.RadiusMeters < CustomerMinRadiusM || in.RadiusMeters > CustomerMaxRadiusM {
		return in, ValidationError{Message: "o raio precisa ficar entre 50 m e 50 km"}
	}
	seen := map[uuid.UUID]bool{}
	unique := make([]uuid.UUID, 0, len(in.VehicleIDs))
	for _, id := range in.VehicleIDs {
		if !seen[id] {
			seen[id] = true
			unique = append(unique, id)
		}
	}
	if len(unique) == 0 {
		return in, ValidationError{Message: "escolha ao menos um veículo para a cerca"}
	}
	in.VehicleIDs = unique
	return in, nil
}

const earthRadiusMeters = 6371000.0

// DistanceMeters devolve a distância entre dois pontos pela fórmula de haversine.
func DistanceMeters(lat1, lon1, lat2, lon2 float64) float64 {
	phi1 := lat1 * math.Pi / 180
	phi2 := lat2 * math.Pi / 180
	dPhi := (lat2 - lat1) * math.Pi / 180
	dLambda := (lon2 - lon1) * math.Pi / 180

	a := math.Sin(dPhi/2)*math.Sin(dPhi/2) +
		math.Cos(phi1)*math.Cos(phi2)*math.Sin(dLambda/2)*math.Sin(dLambda/2)
	return 2 * earthRadiusMeters * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}
