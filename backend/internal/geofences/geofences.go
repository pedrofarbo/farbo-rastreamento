// Package geofences trata as cercas (círculo ou polígono) e a avaliação de
// entrada/saída.
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

	// Shape: "circle" (centro e raio) ou "polygon" (a área desenhada). No
	// polígono, o centro e o raio são os que cobrem a área.
	Shape        string       `json:"shape"`
	Latitude     float64      `json:"latitude"`
	Longitude    float64      `json:"longitude"`
	RadiusMeters float64      `json:"radiusMeters"`
	Polygon      [][2]float64 `json:"polygon"`

	Active bool `json:"active"`

	// O horário dos avisos (ver NotifiesAt): dias da semana (0 = domingo) e
	// das/até em minutos do dia. Vazio/nulo: sempre.
	ScheduleDays  []int `json:"scheduleDays"`
	ScheduleStart *int  `json:"scheduleStart"`
	ScheduleEnd   *int  `json:"scheduleEnd"`

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
	Name          string       `json:"name"`
	Shape         string       `json:"shape"`
	Latitude      float64      `json:"latitude"`
	Longitude     float64      `json:"longitude"`
	RadiusMeters  float64      `json:"radiusMeters"`
	Polygon       [][2]float64 `json:"polygon"`
	Active        *bool        `json:"active"`
	VehicleIDs    []uuid.UUID  `json:"vehicleIds"`
	NotifyEnter   *bool        `json:"notifyEnter"`
	NotifyExit    *bool        `json:"notifyExit"`
	ScheduleDays  []int        `json:"scheduleDays"`
	ScheduleStart *int         `json:"scheduleStart"`
	ScheduleEnd   *int         `json:"scheduleEnd"`
}

// Limites das cercas do cliente. Quantas: cresce com a frota (garagens,
// clientes e filiais), FencesPerVehicle por veículo ativo, de MinPerCustomer
// a MaxPerCustomer. O raio mínimo fica acima do erro do GPS: menor que isso,
// o veículo parado "entraria e sairia" sozinho.
const (
	MinPerCustomer     = 20
	FencesPerVehicle   = 2
	MaxPerCustomer     = 500
	CustomerMinRadiusM = 50
	CustomerMaxRadiusM = 50000

	centralMinRadiusM = 20
	centralMaxRadiusM = 200000
	maxNameLength     = 100
	// MaxPolygonVertices: os pontos de uma área desenhada.
	MaxPolygonVertices = 100

	ShapeCircle  = "circle"
	ShapePolygon = "polygon"
)

const (
	columns = `g.id, g.name, g.shape, g.latitude, g.longitude, g.radius_meters, g.polygon, g.active, g.owner_id,
		g.notify_enter, g.notify_exit, g.schedule_days, g.schedule_start, g.schedule_end,
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
	var days []int16
	var start, end *int16
	err := row.Scan(&g.ID, &g.Name, &g.Shape, &g.Latitude, &g.Longitude, &g.RadiusMeters, &g.Polygon,
		&g.Active, &g.OwnerID, &g.NotifyEnter, &g.NotifyExit, &days, &start, &end, &g.VehicleIDs, &g.CreatedAt, &g.UpdatedAt)
	if err != nil {
		return nil, database.MapError(err)
	}
	if g.VehicleIDs == nil {
		g.VehicleIDs = []uuid.UUID{}
	}
	g.ScheduleDays = make([]int, len(days))
	for i, d := range days {
		g.ScheduleDays[i] = int(d)
	}
	if start != nil && end != nil {
		s, e := int(*start), int(*end)
		g.ScheduleStart, g.ScheduleEnd = &s, &e
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
			INSERT INTO geofences (name, shape, latitude, longitude, radius_meters, polygon, active, owner_id,
				notify_enter, notify_exit, schedule_days, schedule_start, schedule_end)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13) RETURNING id`,
			in.Name, in.Shape, in.Latitude, in.Longitude, in.RadiusMeters, polygonValue(in), flag(in.Active), owner,
			flag(in.NotifyEnter), flag(in.NotifyExit), in.ScheduleDays, in.ScheduleStart, in.ScheduleEnd,
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
			UPDATE geofences g SET name = $3, shape = $4, latitude = $5, longitude = $6, radius_meters = $7,
				polygon = $8, active = $9, notify_enter = $10, notify_exit = $11,
				schedule_days = $12, schedule_start = $13, schedule_end = $14, updated_at = NOW()
			WHERE g.id = $1 AND `+ownerMatches+`$2`,
			id, owner, in.Name, in.Shape, in.Latitude, in.Longitude, in.RadiusMeters, polygonValue(in), flag(in.Active),
			flag(in.NotifyEnter), flag(in.NotifyExit), in.ScheduleDays, in.ScheduleStart, in.ScheduleEnd)
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

// ActiveVehicles conta os veículos ativos do cliente (as assinaturas ativas).
func (r *Repository) ActiveVehicles(ctx context.Context, owner uuid.UUID) (int, error) {
	var n int
	err := r.db.QueryRow(ctx, `SELECT count(*) FROM subscriptions WHERE customer_id = $1 AND status = 'ACTIVE'`, owner).Scan(&n)
	return n, database.MapError(err)
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

// Contains diz se o ponto está dentro da cerca: o círculo (no polígono, o
// que cobre a área, que corta a conta para os longe) e, no polígono, a área.
func (g *Geofence) Contains(lat, lon float64) bool {
	if DistanceMeters(lat, lon, g.Latitude, g.Longitude) > g.RadiusMeters {
		return false
	}
	return g.Shape != ShapePolygon || insidePolygon(g.Polygon, lat, lon)
}

// insidePolygon: o ponto dentro da área (o raio cruza as bordas um número
// ímpar de vezes). Na escala de uma cerca, latitude e longitude servem de
// plano.
func insidePolygon(polygon [][2]float64, lat, lon float64) bool {
	inside := false
	for i, j := 0, len(polygon)-1; i < len(polygon); j, i = i, i+1 {
		yi, xi := polygon[i][0], polygon[i][1]
		yj, xj := polygon[j][0], polygon[j][1]
		if (yi > lat) != (yj > lat) && lon < (xj-xi)*(lat-yi)/(yj-yi)+xi {
			inside = !inside
		}
	}
	return inside
}

// NotifiesAt diz se a cerca avisa no instante t (no fuso loc): sem horário,
// sempre; com dias, só neles; com das/até, só no intervalo. Um intervalo que
// atravessa a meia-noite (das 22:00 às 06:00) pertence ao dia em que começa:
// a madrugada de sábado é da sexta.
func (g *Geofence) NotifiesAt(t time.Time, loc *time.Location) bool {
	local := t.In(loc)
	day, minute := int(local.Weekday()), local.Hour()*60+local.Minute()
	onDay := func(d int) bool { return len(g.ScheduleDays) == 0 || slices.Contains(g.ScheduleDays, d) }
	if g.ScheduleStart == nil || g.ScheduleEnd == nil {
		return onDay(day)
	}
	start, end := *g.ScheduleStart, *g.ScheduleEnd
	if start < end {
		return onDay(day) && minute >= start && minute < end
	}
	switch {
	case minute >= start:
		return onDay(day)
	case minute < end:
		return onDay((day + 6) % 7)
	}
	return false
}

// Service mantém as cercas ativas em memória: a avaliação roda a cada posição
// recebida e não pode depender de uma consulta ao banco.
type Service struct {
	repo *Repository
	// loc é o fuso do horário dos avisos.
	loc *time.Location

	// Baseline é chamado ao salvar uma cerca, antes de ela valer para a
	// ingestão: marca os veículos que já estão dentro, para que criar a cerca
	// "Casa" com o carro na garagem não avise "entrou em Casa" (nem mudar o
	// raio avise "saiu"). Opcional.
	Baseline func(ctx context.Context, g *Geofence)

	mu     sync.RWMutex
	cached map[uuid.UUID]*Geofence
}

func NewService(repo *Repository) *Service {
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		loc = time.FixedZone("BRT", -3*60*60)
	}
	return &Service{repo: repo, loc: loc}
}

// SetLocation troca o fuso do horário dos avisos (o da central).
func (s *Service) SetLocation(loc *time.Location) {
	if loc != nil {
		s.loc = loc
	}
}

// Notifies diz se a cerca avisa no instante t, no fuso da central.
func (s *Service) Notifies(g *Geofence, t time.Time) bool {
	if s == nil {
		return true
	}
	return g.NotifiesAt(t, s.loc)
}

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

// LimitFor é quantas cercas cabem numa conta com tantos veículos ativos.
func LimitFor(activeVehicles int) int {
	return min(MaxPerCustomer, max(MinPerCustomer, FencesPerVehicle*activeVehicles))
}

// Limit é quantas cercas o cliente pode ter, e quantas já tem.
func (s *Service) Limit(ctx context.Context, owner uuid.UUID) (limit, used int, err error) {
	vehicles, err := s.repo.ActiveVehicles(ctx, owner)
	if err != nil {
		return 0, 0, err
	}
	used, err = s.repo.CountByOwner(ctx, owner)
	return LimitFor(vehicles), used, err
}

// Create cria a cerca da central (owner nulo) ou de um cliente.
func (s *Service) Create(ctx context.Context, owner *uuid.UUID, in Input) (*Geofence, error) {
	in, err := normalize(in, owner)
	if err != nil {
		return nil, err
	}
	if owner != nil {
		limit, used, err := s.Limit(ctx, *owner)
		if err != nil {
			return nil, err
		}
		if used >= limit {
			return nil, ValidationError{Message: fmt.Sprintf(
				"limite de %d cercas nesta conta (%d por veículo, no mínimo %d): apague uma para criar outra",
				limit, FencesPerVehicle, MinPerCustomer)}
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
	if in, err := normalizeSchedule(in); err != nil {
		return in, err
	}
	in.ScheduleDays = uniqueDays(in.ScheduleDays)
	switch in.Shape {
	case "", ShapeCircle:
		in.Shape, in.Polygon = ShapeCircle, nil
	case ShapePolygon:
		var err error
		if in, err = normalizePolygon(in); err != nil {
			return in, err
		}
	default:
		return in, ValidationError{Message: "formato de cerca inválido (círculo ou área desenhada)"}
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
		if in.Shape == ShapePolygon {
			return in, ValidationError{Message: "a área desenhada precisa ter de 50 m a 50 km do centro até a borda"}
		}
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

// normalizePolygon confere os vértices (de 3 a MaxPolygonVertices, sem
// repetir o primeiro no fim) e calcula o centro e o raio que cobrem a área.
func normalizePolygon(in Input) (Input, error) {
	points := in.Polygon
	if n := len(points); n > 1 && points[0] == points[n-1] {
		points = points[:n-1]
	}
	if len(points) < 3 {
		return in, ValidationError{Message: "desenhe a área com pelo menos 3 pontos"}
	}
	if len(points) > MaxPolygonVertices {
		return in, ValidationError{Message: fmt.Sprintf("a área pode ter até %d pontos", MaxPolygonVertices)}
	}
	var lat, lon float64
	for _, p := range points {
		if math.IsNaN(p[0]) || math.IsNaN(p[1]) || p[0] < -90 || p[0] > 90 || p[1] < -180 || p[1] > 180 {
			return in, ValidationError{Message: "ponto da área fora do mapa"}
		}
		lat += p[0]
		lon += p[1]
	}
	lat /= float64(len(points))
	lon /= float64(len(points))
	radius := 0.0
	for _, p := range points {
		radius = math.Max(radius, DistanceMeters(lat, lon, p[0], p[1]))
	}
	in.Polygon, in.Latitude, in.Longitude, in.RadiusMeters = points, lat, lon, math.Ceil(radius)
	return in, nil
}

// normalizeSchedule confere o horário: os dias de 0 a 6 e das/até juntos.
func normalizeSchedule(in Input) (Input, error) {
	for _, d := range in.ScheduleDays {
		if d < 0 || d > 6 {
			return in, ValidationError{Message: "dia da semana inválido"}
		}
	}
	if (in.ScheduleStart == nil) != (in.ScheduleEnd == nil) {
		return in, ValidationError{Message: "informe o início e o fim do horário"}
	}
	if in.ScheduleStart != nil {
		s, e := *in.ScheduleStart, *in.ScheduleEnd
		if s < 0 || s > 1439 || e < 0 || e > 1439 {
			return in, ValidationError{Message: "horário inválido"}
		}
		if s == e {
			return in, ValidationError{Message: "o horário precisa de início e fim diferentes (para sempre, tire o horário)"}
		}
	}
	return in, nil
}

// uniqueDays: os dias sem repetir, em ordem; todos os 7 é o mesmo que nenhum.
func uniqueDays(days []int) []int {
	out := []int{}
	for d := 0; d <= 6; d++ {
		if slices.Contains(days, d) {
			out = append(out, d)
		}
	}
	if len(out) == 7 {
		return []int{}
	}
	return out
}

// polygonValue: o polígono para o banco (nulo no círculo).
func polygonValue(in Input) any {
	if in.Shape != ShapePolygon {
		return nil
	}
	return in.Polygon
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
