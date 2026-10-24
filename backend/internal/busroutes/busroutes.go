// Package busroutes são os itinerários das empresas de ônibus (e de
// fretamento, transporte escolar): a linha com os pontos em ordem e o minuto
// previsto desde a saída; as viagens de cada veículo; e o progresso ao vivo
// — por qual ponto já passou, quanto está atrasado, quando chega no próximo
// e se saiu do caminho. A linha pode ter um link público (o id assinado) para
// o passageiro ver onde o ônibus está.
package busroutes

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

const (
	MaxStops  = 80
	MaxRoutes = 200
	// MaxTripsAtOnce: quantas viagens um pedido cria (repetindo nos dias seguintes).
	MaxTripsAtOnce = 31
	// earlyMargin: a posição até este tanto antes da saída já conta.
	earlyMargin = 10 * time.Minute
	// tripTail: depois do último ponto previsto, a viagem ainda corre por
	// este tempo (o atraso).
	tripTail = 3 * time.Hour
)

var (
	// ErrLinkEnded: o link público não vale (linha desligada ou não pública).
	ErrLinkEnded = errors.New("este link de linha não está ativo")
	colorRe      = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	encoding     = base64.RawURLEncoding
)

// InputError é um pedido recusado; a mensagem vai para a tela.
type InputError struct{ Message string }

func (e *InputError) Error() string { return e.Message }

func invalid(format string, args ...any) error { return &InputError{fmt.Sprintf(format, args...)} }

// Stop é um ponto da linha.
type Stop struct {
	ID        uuid.UUID `json:"id"`
	Seq       int       `json:"seq"`
	Name      string    `json:"name"`
	Latitude  float64   `json:"latitude"`
	Longitude float64   `json:"longitude"`
	RadiusM   int       `json:"radiusM"`
	OffsetMin int       `json:"offsetMin"`
}

// Route é uma linha.
type Route struct {
	ID         uuid.UUID `json:"id"`
	Name       string    `json:"name"`
	Color      string    `json:"color"`
	DeviationM int       `json:"deviationM"`
	Public     bool      `json:"public"`
	Active     bool      `json:"active"`
	Stops      []Stop    `json:"stops"`
	CreatedAt  time.Time `json:"createdAt"`
	// PublicURL: o link do passageiro (só com Public).
	PublicURL string `json:"publicUrl,omitempty"`
}

// StopInput é um ponto pedido.
type StopInput struct {
	Name      string  `json:"name"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	RadiusM   int     `json:"radiusM"`
	OffsetMin int     `json:"offsetMin"`
}

// Input é a linha pedida (os pontos em ordem substituem os de antes).
type Input struct {
	Name       string      `json:"name"`
	Color      string      `json:"color"`
	DeviationM int         `json:"deviationM"`
	Public     bool        `json:"public"`
	Active     *bool       `json:"active"`
	Stops      []StopInput `json:"stops"`
}

// Trip é uma viagem de um veículo na linha.
type Trip struct {
	ID          uuid.UUID  `json:"id"`
	RouteID     uuid.UUID  `json:"routeId"`
	VehicleID   uuid.UUID  `json:"vehicleId"`
	VehicleName string     `json:"vehicleName"`
	DeviceID    *uuid.UUID `json:"-"`
	DepartsAt   time.Time  `json:"departsAt"`
	Canceled    bool       `json:"canceled"`
}

type Service struct {
	db     *database.DB
	appURL string
	key    []byte
	now    func() time.Time
}

func NewService(db *database.DB, appURL string, secret []byte) *Service {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte("farbo-bus-route-v1"))
	return &Service{db: db, appURL: strings.TrimRight(appURL, "/"), key: mac.Sum(nil), now: time.Now}
}

// SetClock troca o relógio (testes).
func (s *Service) SetClock(now func() time.Time) { s.now = now }

// Token é a parte secreta do link público da linha.
func (s *Service) Token(id uuid.UUID) string {
	mac := hmac.New(sha256.New, s.key)
	mac.Write(id[:])
	return encoding.EncodeToString(append(id[:], mac.Sum(nil)[:16]...))
}

func (s *Service) withURL(r *Route) *Route {
	if r.Public {
		r.PublicURL = s.appURL + "/linha/" + s.Token(r.ID)
	}
	return r
}

func (s *Service) stops(ctx context.Context, routes []*Route) error {
	if len(routes) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(routes))
	byID := map[uuid.UUID]*Route{}
	for i, r := range routes {
		ids[i], byID[r.ID] = r.ID, r
		r.Stops = []Stop{}
	}
	rows, err := s.db.Query(ctx, `
		SELECT route_id, id, seq, name, latitude, longitude, radius_m, offset_min FROM bus_route_stops
		WHERE route_id = ANY($1) ORDER BY route_id, seq`, ids)
	if err != nil {
		return database.MapError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var routeID uuid.UUID
		var st Stop
		if err := rows.Scan(&routeID, &st.ID, &st.Seq, &st.Name, &st.Latitude, &st.Longitude, &st.RadiusM, &st.OffsetMin); err != nil {
			return database.MapError(err)
		}
		byID[routeID].Stops = append(byID[routeID].Stops, st)
	}
	return database.MapError(rows.Err())
}

const selectRoute = `SELECT id, name, color, deviation_m, public, active, created_at FROM bus_routes`

func scanRoute(row database.Scanner) (*Route, error) {
	var r Route
	if err := row.Scan(&r.ID, &r.Name, &r.Color, &r.DeviationM, &r.Public, &r.Active, &r.CreatedAt); err != nil {
		return nil, database.MapError(err)
	}
	return &r, nil
}

// Routes são as linhas da conta, com os pontos.
func (s *Service) Routes(ctx context.Context, account uuid.UUID) ([]*Route, error) {
	rows, err := s.db.Query(ctx, selectRoute+` WHERE account_id = $1 ORDER BY active DESC, name`, account)
	if err != nil {
		return nil, database.MapError(err)
	}
	out := []*Route{}
	for rows.Next() {
		r, err := scanRoute(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, s.withURL(r))
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, database.MapError(err)
	}
	return out, s.stops(ctx, out)
}

// Route é uma linha da conta.
func (s *Service) Route(ctx context.Context, account, id uuid.UUID) (*Route, error) {
	r, err := scanRoute(s.db.QueryRow(ctx, selectRoute+` WHERE id = $1 AND account_id = $2`, id, account))
	if err != nil {
		return nil, err
	}
	return s.withURL(r), s.stops(ctx, []*Route{r})
}

func checkRoute(in *Input) error {
	in.Name = strings.Join(strings.Fields(in.Name), " ")
	if in.Color == "" {
		in.Color = "#22c55e"
	}
	if in.DeviationM == 0 {
		in.DeviationM = 500
	}
	switch {
	case in.Name == "" || utf8.RuneCountInString(in.Name) > 80:
		return invalid("dê um nome à linha (até 80 caracteres)")
	case !colorRe.MatchString(in.Color):
		return invalid("cor inválida")
	case in.DeviationM < 100 || in.DeviationM > 5000:
		return invalid("o desvio vai de 100 a 5.000 metros")
	case len(in.Stops) < 2 || len(in.Stops) > MaxStops:
		return invalid("a linha tem de 2 a %d pontos", MaxStops)
	}
	last := -1
	for i := range in.Stops {
		st := &in.Stops[i]
		st.Name = strings.Join(strings.Fields(st.Name), " ")
		if st.Name == "" {
			st.Name = fmt.Sprintf("Ponto %d", i+1)
		}
		if st.RadiusM == 0 {
			st.RadiusM = 80
		}
		switch {
		case utf8.RuneCountInString(st.Name) > 80:
			return invalid("o nome do ponto %d vai até 80 caracteres", i+1)
		case math.Abs(st.Latitude) > 90 || math.Abs(st.Longitude) > 180 || (st.Latitude == 0 && st.Longitude == 0):
			return invalid("o ponto %d está sem lugar no mapa", i+1)
		case st.RadiusM < 20 || st.RadiusM > 1000:
			return invalid("o raio do ponto %d vai de 20 a 1.000 metros", i+1)
		case st.OffsetMin < 0 || st.OffsetMin > 1440:
			return invalid("o horário do ponto %d vai de 0 a 1.440 minutos depois da saída", i+1)
		case st.OffsetMin < last:
			return invalid("o ponto %d está previsto antes do anterior", i+1)
		}
		last = st.OffsetMin
	}
	return nil
}

// Save cria (id nulo) ou altera a linha; os pontos substituem os de antes.
func (s *Service) Save(ctx context.Context, account uuid.UUID, id *uuid.UUID, in Input) (*Route, error) {
	if err := checkRoute(&in); err != nil {
		return nil, err
	}
	var routeID uuid.UUID
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if id == nil {
			var count int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM bus_routes WHERE account_id = $1`, account).Scan(&count); err != nil {
				return err
			}
			if count >= MaxRoutes {
				return invalid("são até %d linhas por conta", MaxRoutes)
			}
			if err := tx.QueryRow(ctx, `
				INSERT INTO bus_routes (account_id, name, color, deviation_m, public) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
				account, in.Name, in.Color, in.DeviationM, in.Public).Scan(&routeID); err != nil {
				return err
			}
		} else {
			tag, err := tx.Exec(ctx, `
				UPDATE bus_routes SET name = $3, color = $4, deviation_m = $5, public = $6, active = COALESCE($7, active),
					updated_at = NOW()
				WHERE id = $1 AND account_id = $2`, *id, account, in.Name, in.Color, in.DeviationM, in.Public, in.Active)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				return database.ErrNotFound
			}
			routeID = *id
			if _, err := tx.Exec(ctx, `DELETE FROM bus_route_stops WHERE route_id = $1`, routeID); err != nil {
				return err
			}
		}
		for i, st := range in.Stops {
			if _, err := tx.Exec(ctx, `
				INSERT INTO bus_route_stops (route_id, seq, name, latitude, longitude, radius_m, offset_min)
				VALUES ($1, $2, $3, $4, $5, $6, $7)`, routeID, i+1, st.Name, st.Latitude, st.Longitude, st.RadiusM, st.OffsetMin); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		var input *InputError
		if errors.As(err, &input) {
			return nil, input
		}
		return nil, database.MapError(err)
	}
	return s.Route(ctx, account, routeID)
}

// Delete apaga a linha (e as viagens).
func (s *Service) Delete(ctx context.Context, account, id uuid.UUID) error {
	tag, err := s.db.Exec(ctx, `DELETE FROM bus_routes WHERE id = $1 AND account_id = $2`, id, account)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return database.ErrNotFound
	}
	return nil
}

const selectTrip = `
	SELECT t.id, t.route_id, t.vehicle_id, v.name, v.device_id, t.departs_at, t.canceled
	FROM bus_trips t JOIN vehicles v ON v.id = t.vehicle_id`

func scanTrips(rows pgx.Rows) ([]*Trip, error) {
	defer rows.Close()
	out := []*Trip{}
	for rows.Next() {
		var t Trip
		if err := rows.Scan(&t.ID, &t.RouteID, &t.VehicleID, &t.VehicleName, &t.DeviceID, &t.DepartsAt, &t.Canceled); err != nil {
			return nil, database.MapError(err)
		}
		out = append(out, &t)
	}
	return out, database.MapError(rows.Err())
}

// Trips são as viagens da linha que saem em [from, to).
func (s *Service) Trips(ctx context.Context, routeID uuid.UUID, from, to time.Time) ([]*Trip, error) {
	rows, err := s.db.Query(ctx, selectTrip+` WHERE t.route_id = $1 AND t.departs_at >= $2 AND t.departs_at < $3
		ORDER BY t.departs_at`, routeID, from, to)
	if err != nil {
		return nil, database.MapError(err)
	}
	return scanTrips(rows)
}

// CreateTrips põe o veículo na linha: uma viagem na hora pedida e, com days,
// a mesma hora nos dias seguintes.
func (s *Service) CreateTrips(ctx context.Context, routeID, vehicleID, by uuid.UUID, departsAt time.Time, days int) ([]*Trip, error) {
	if days < 1 {
		days = 1
	}
	if days > MaxTripsAtOnce {
		return nil, invalid("são até %d dias de uma vez", MaxTripsAtOnce)
	}
	if departsAt.IsZero() || departsAt.Before(s.now().Add(-24*time.Hour)) {
		return nil, invalid("escolha o horário de saída")
	}
	ids := []uuid.UUID{}
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		for d := range days {
			var id uuid.UUID
			if err := tx.QueryRow(ctx, `
				INSERT INTO bus_trips (route_id, vehicle_id, departs_at, created_by) VALUES ($1, $2, $3, $4) RETURNING id`,
				routeID, vehicleID, departsAt.AddDate(0, 0, d), by).Scan(&id); err != nil {
				return err
			}
			ids = append(ids, id)
		}
		return nil
	})
	if err != nil {
		return nil, database.MapError(err)
	}
	rows, err := s.db.Query(ctx, selectTrip+` WHERE t.id = ANY($1) ORDER BY t.departs_at`, ids)
	if err != nil {
		return nil, database.MapError(err)
	}
	return scanTrips(rows)
}

// CancelTrip cancela a viagem da linha da conta.
func (s *Service) CancelTrip(ctx context.Context, account, tripID uuid.UUID) error {
	tag, err := s.db.Exec(ctx, `
		UPDATE bus_trips t SET canceled = TRUE FROM bus_routes r
		WHERE t.id = $1 AND r.id = t.route_id AND r.account_id = $2`, tripID, account)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return database.ErrNotFound
	}
	return nil
}

// ByToken é a linha do link público (pública e ativa), com os pontos.
func (s *Service) ByToken(ctx context.Context, token string) (*Route, error) {
	token = strings.TrimSpace(token)
	raw, err := encoding.DecodeString(token)
	if err != nil || len(raw) != 32 {
		return nil, ErrLinkEnded
	}
	id, err := uuid.FromBytes(raw[:16])
	if err != nil || !hmac.Equal([]byte(s.Token(id)), []byte(token)) {
		return nil, ErrLinkEnded
	}
	r, err := scanRoute(s.db.QueryRow(ctx, selectRoute+` WHERE id = $1`, id))
	if errors.Is(err, database.ErrNotFound) {
		return nil, ErrLinkEnded
	}
	if err != nil {
		return nil, err
	}
	if !r.Public || !r.Active {
		return nil, ErrLinkEnded
	}
	return r, s.stops(ctx, []*Route{r})
}

// Window é o período em que as posições contam para a viagem.
func Window(r *Route, t *Trip) (time.Time, time.Time) {
	last := 0
	if len(r.Stops) > 0 {
		last = r.Stops[len(r.Stops)-1].OffsetMin
	}
	return t.DepartsAt.Add(-earlyMargin), t.DepartsAt.Add(time.Duration(last)*time.Minute + tripTail)
}
