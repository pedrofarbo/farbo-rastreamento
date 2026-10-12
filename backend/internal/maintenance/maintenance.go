// Package maintenance cuida do hodômetro, do horímetro e da manutenção dos
// veículos.
//
// O hodômetro é a leitura que o cliente informa mais os km medidos pelo GPS
// desde então (os do resumo diário dos relatórios). O horímetro (horas de
// motor: andando ou parado com a ignição ligada) é igual, mas sem a leitura
// valem as horas medidas pelo rastreador. Cada manutenção vence a cada
// tantos km, horas de motor e/ou meses, contando da última vez que foi
// feita — o que vier primeiro —, e fica em dia, perto ou vencida. Uma vez por dia, quem
// cuida da conta recebe por e-mail as que ficaram perto ou venceram.
package maintenance

import (
	"context"
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// Situações de uma manutenção.
const (
	StatusOK = "ok"
	// StatusSoon: faltam até SoonKm ou SoonHours (ou 10% do intervalo, o
	// que for maior) ou SoonDays.
	StatusSoon = "soon"
	StatusDue  = "due"
	// StatusUnknown: vence só por km e o veículo ainda não tem hodômetro.
	StatusUnknown = "unknown"
)

const (
	SoonKm             = 500
	SoonHours          = 25
	SoonDays           = 15
	MaxNameLength      = 60
	MaxNotesLength     = 300
	MaxItemsPerVehicle = 20
	// MaxBulk é quantos veículos (ou manutenções) entram numa operação.
	MaxBulk = 1000
	// MaxOdometerKm: acima disso é erro de digitação.
	MaxOdometerKm = 5_000_000
	// MaxEngineHours: acima disso é erro de digitação.
	MaxEngineHours = 1_000_000
)

// InputError é um pedido recusado; a mensagem vai para a tela.
type InputError struct{ Message string }

func (e *InputError) Error() string { return e.Message }

// Odometer é o hodômetro e o horímetro de um veículo.
type Odometer struct {
	VehicleID uuid.UUID `json:"vehicleId"`
	// TrackedKm são os km medidos pelo GPS desde a instalação.
	TrackedKm float64 `json:"trackedKm"`
	// Km é o hodômetro; nulo enquanto o cliente não informa a leitura.
	Km    *float64   `json:"km"`
	SetAt *time.Time `json:"setAt"`
	// TrackedHours são as horas de motor medidas desde a instalação.
	TrackedHours float64 `json:"trackedHours"`
	// Hours é o horímetro: a leitura informada mais as horas medidas
	// depois; sem a leitura, as medidas.
	Hours      float64    `json:"hours"`
	HoursSetAt *time.Time `json:"hoursSetAt"`
}

// Item é uma manutenção de um veículo, com quanto falta.
type Item struct {
	ID          uuid.UUID  `json:"id"`
	VehicleID   uuid.UUID  `json:"vehicleId"`
	VehicleName string     `json:"vehicleName"`
	Plate       string     `json:"plate"`
	GroupID     *uuid.UUID `json:"groupId"`
	Name        string     `json:"name"`
	EveryKm     *int       `json:"everyKm"`
	EveryMonths *int       `json:"everyMonths"`
	EveryHours  *int       `json:"everyHours"`
	LastKm      *float64   `json:"lastKm"`
	LastHours   *float64   `json:"lastHours"`
	LastDate    string     `json:"lastDate"`
	// Calculados: o próximo km, as próximas horas e a próxima data, quanto
	// falta (negativo: passou) e a situação.
	NextKm         *float64 `json:"nextKm"`
	NextHours      *float64 `json:"nextHours"`
	NextDate       *string  `json:"nextDate"`
	RemainingKm    *float64 `json:"remainingKm"`
	RemainingHours *float64 `json:"remainingHours"`
	RemainingDays  *int     `json:"remainingDays"`
	Status         string   `json:"status"`

	accountID uuid.UUID
	notified  *string
}

// Record é uma manutenção feita (o histórico).
type Record struct {
	ID        uuid.UUID  `json:"id"`
	VehicleID uuid.UUID  `json:"vehicleId"`
	ItemID    *uuid.UUID `json:"itemId"`
	Name      string     `json:"name"`
	DoneDate  string     `json:"doneDate"`
	Km        *float64   `json:"km"`
	Hours     *float64   `json:"hours"`
	Notes     string     `json:"notes"`
	CreatedAt time.Time  `json:"createdAt"`
}

// ItemInput é o pedido de uma manutenção. LastDate vazio é hoje; LastKm e
// LastHours nulos, o hodômetro e o horímetro de agora (de cada veículo).
type ItemInput struct {
	Name        string   `json:"name"`
	EveryKm     *int     `json:"everyKm"`
	EveryMonths *int     `json:"everyMonths"`
	EveryHours  *int     `json:"everyHours"`
	LastDate    string   `json:"lastDate"`
	LastKm      *float64 `json:"lastKm"`
	LastHours   *float64 `json:"lastHours"`
}

type Service struct {
	db  *database.DB
	loc *time.Location
	now func() time.Time
}

func NewService(db *database.DB, timezone string) *Service {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		loc = time.FixedZone("BRT", -3*60*60)
	}
	return &Service{db: db, loc: loc, now: time.Now}
}

// SetClock troca o relógio (testes).
func (s *Service) SetClock(now func() time.Time) { s.now = now }

func (s *Service) today() time.Time {
	t := s.now().In(s.loc)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// ---------------------------------------------------------------------------
// Hodômetro
// ---------------------------------------------------------------------------

// Os metros e os segundos de motor medidos do veículo v.
const (
	trackedMetersSQL  = `COALESCE((SELECT sum(st.distance_m) FROM vehicle_daily_stats st WHERE st.vehicle_id = v.id), 0)::float8`
	trackedSecondsSQL = `COALESCE((SELECT sum(st.moving_s + st.idle_s) FROM vehicle_daily_stats st WHERE st.vehicle_id = v.id), 0)::float8`
)

// Odometers são os hodômetros e horímetros dos veículos pedidos.
func (s *Service) Odometers(ctx context.Context, vehicleIDs []uuid.UUID) (map[uuid.UUID]Odometer, error) {
	out := map[uuid.UUID]Odometer{}
	if len(vehicleIDs) == 0 {
		return out, nil
	}
	rows, err := s.db.Query(ctx, `
		SELECT v.id, v.odometer_offset_m, v.odometer_set_at, v.engine_offset_s, v.engine_set_at,
			`+trackedMetersSQL+`, `+trackedSecondsSQL+`
		FROM vehicles v WHERE v.id = ANY($1)`, vehicleIDs)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var o Odometer
		var offset, engineOffset *float64
		var tracked, engine float64
		if err := rows.Scan(&o.VehicleID, &offset, &o.SetAt, &engineOffset, &o.HoursSetAt, &tracked, &engine); err != nil {
			return nil, database.MapError(err)
		}
		o.TrackedKm = tracked / 1000
		if offset != nil {
			km := math.Max(0, (*offset+tracked)/1000)
			o.Km = &km
		}
		o.TrackedHours = engine / 3600
		o.Hours = o.TrackedHours
		if engineOffset != nil {
			o.Hours = math.Max(0, (*engineOffset+engine)/3600)
		}
		out[o.VehicleID] = o
	}
	return out, database.MapError(rows.Err())
}

// SetOdometer grava a leitura do hodômetro do veículo agora.
func (s *Service) SetOdometer(ctx context.Context, vehicleID uuid.UUID, km float64) (*Odometer, error) {
	if math.IsNaN(km) || km < 0 || km > MaxOdometerKm {
		return nil, &InputError{fmt.Sprintf("informe o hodômetro em km (de 0 a %d)", MaxOdometerKm)}
	}
	tag, err := s.db.Exec(ctx, `
		UPDATE vehicles SET odometer_offset_m = $2 * 1000 -
			COALESCE((SELECT sum(st.distance_m) FROM vehicle_daily_stats st WHERE st.vehicle_id = $1), 0),
			odometer_set_at = NOW(), updated_at = NOW()
		WHERE id = $1`, vehicleID, km)
	if err != nil {
		return nil, database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return nil, database.ErrNotFound
	}
	odometers, err := s.Odometers(ctx, []uuid.UUID{vehicleID})
	if err != nil {
		return nil, err
	}
	o := odometers[vehicleID]
	return &o, nil
}

// SetEngineHours grava a leitura do horímetro do veículo agora. Na primeira
// leitura, as horas guardadas até ali (a última vez de cada manutenção e o
// histórico), que eram as medidas pelo rastreador, passam para a escala do
// horímetro; nas seguintes, ficam como estão (são leituras de verdade).
func (s *Service) SetEngineHours(ctx context.Context, vehicleID uuid.UUID, hours float64) (*Odometer, error) {
	if math.IsNaN(hours) || hours < 0 || hours > MaxEngineHours {
		return nil, &InputError{fmt.Sprintf("informe o horímetro em horas (de 0 a %s)", thousands(MaxEngineHours))}
	}
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var previous *float64
		var engine float64
		if err := tx.QueryRow(ctx, `
			SELECT v.engine_offset_s, `+trackedSecondsSQL+`
			FROM vehicles v WHERE v.id = $1 FOR UPDATE`, vehicleID).Scan(&previous, &engine); err != nil {
			return err
		}
		offset := hours*3600 - engine
		if _, err := tx.Exec(ctx, `
			UPDATE vehicles SET engine_offset_s = $2, engine_set_at = NOW(), updated_at = NOW() WHERE id = $1`,
			vehicleID, offset); err != nil {
			return err
		}
		if previous != nil {
			return nil
		}
		shift := offset / 3600
		if _, err := tx.Exec(ctx, `
			UPDATE maintenance_items SET last_hours = last_hours + $2 WHERE vehicle_id = $1 AND last_hours IS NOT NULL`,
			vehicleID, shift); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			UPDATE maintenance_records SET hours = hours + $2 WHERE vehicle_id = $1 AND hours IS NOT NULL`, vehicleID, shift)
		return err
	})
	if err != nil {
		return nil, database.MapError(err)
	}
	odometers, err := s.Odometers(ctx, []uuid.UUID{vehicleID})
	if err != nil {
		return nil, err
	}
	o := odometers[vehicleID]
	return &o, nil
}

// ---------------------------------------------------------------------------
// Manutenções
// ---------------------------------------------------------------------------

const itemColumns = `i.id, i.vehicle_id, v.name, COALESCE(v.plate, ''), v.group_id, v.owner_id, i.name, i.every_km,
	i.every_months, i.every_hours, i.last_km, i.last_hours, i.last_date, i.notified`

func (s *Service) items(ctx context.Context, where string, args ...any) ([]*Item, error) {
	rows, err := s.db.Query(ctx, `
		SELECT `+itemColumns+` FROM maintenance_items i JOIN vehicles v ON v.id = i.vehicle_id
		WHERE `+where+` ORDER BY v.name, i.name`, args...)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	out := []*Item{}
	vehicleIDs := []uuid.UUID{}
	for rows.Next() {
		var it Item
		var owner *uuid.UUID
		var last time.Time
		if err := rows.Scan(&it.ID, &it.VehicleID, &it.VehicleName, &it.Plate, &it.GroupID, &owner, &it.Name,
			&it.EveryKm, &it.EveryMonths, &it.EveryHours, &it.LastKm, &it.LastHours, &last, &it.notified); err != nil {
			return nil, database.MapError(err)
		}
		if owner != nil {
			it.accountID = *owner
		}
		it.LastDate = last.Format(time.DateOnly)
		out = append(out, &it)
		if !slices.Contains(vehicleIDs, it.VehicleID) {
			vehicleIDs = append(vehicleIDs, it.VehicleID)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, database.MapError(err)
	}
	rows.Close()
	odometers, err := s.Odometers(ctx, vehicleIDs)
	if err != nil {
		return nil, err
	}
	today := s.today()
	for _, it := range out {
		it.evaluate(odometers[it.VehicleID], today)
	}
	return out, nil
}

// Items são as manutenções dos veículos pedidos, das mais urgentes às em dia.
func (s *Service) Items(ctx context.Context, vehicleIDs []uuid.UUID) ([]*Item, error) {
	list, err := s.items(ctx, `i.vehicle_id = ANY($1)`, vehicleIDs)
	if err != nil {
		return nil, err
	}
	SortByUrgency(list)
	return list, nil
}

// ItemVehicles são os veículos das manutenções pedidas (para conferir o acesso).
func (s *Service) ItemVehicles(ctx context.Context, itemIDs []uuid.UUID) (map[uuid.UUID]uuid.UUID, error) {
	rows, err := s.db.Query(ctx, `SELECT id, vehicle_id FROM maintenance_items WHERE id = ANY($1)`, itemIDs)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	out := map[uuid.UUID]uuid.UUID{}
	for rows.Next() {
		var id, vehicle uuid.UUID
		if err := rows.Scan(&id, &vehicle); err != nil {
			return nil, database.MapError(err)
		}
		out[id] = vehicle
	}
	return out, database.MapError(rows.Err())
}

// evaluate calcula o próximo km, as próximas horas e a próxima data, quanto
// falta e a situação.
func (it *Item) evaluate(o Odometer, today time.Time) {
	it.Status = StatusOK
	worst := func(status string) {
		if rank(status) > rank(it.Status) {
			it.Status = status
		}
	}
	// Sem nenhum intervalo que dê para medir (só km, sem hodômetro), a
	// situação é desconhecida.
	measured := false
	if it.EveryMonths != nil {
		last, _ := time.Parse(time.DateOnly, it.LastDate)
		next := last.AddDate(0, *it.EveryMonths, 0)
		nextDay := next.Format(time.DateOnly)
		days := int(next.Sub(today).Hours() / 24)
		it.NextDate, it.RemainingDays = &nextDay, &days
		measured = true
		switch {
		case days <= 0:
			worst(StatusDue)
		case days <= SoonDays:
			worst(StatusSoon)
		}
	}
	// Sem hodômetro, o km não diz nada.
	if it.EveryKm != nil && it.LastKm != nil && o.Km != nil {
		next := *it.LastKm + float64(*it.EveryKm)
		remaining := next - *o.Km
		it.NextKm, it.RemainingKm = &next, &remaining
		measured = true
		switch {
		case remaining <= 0:
			worst(StatusDue)
		case remaining <= math.Max(SoonKm, float64(*it.EveryKm)/10):
			worst(StatusSoon)
		}
	}
	if it.EveryHours != nil && it.LastHours != nil {
		next := *it.LastHours + float64(*it.EveryHours)
		remaining := next - o.Hours
		it.NextHours, it.RemainingHours = &next, &remaining
		measured = true
		switch {
		case remaining <= 0:
			worst(StatusDue)
		case remaining <= math.Max(SoonHours, float64(*it.EveryHours)/10):
			worst(StatusSoon)
		}
	}
	if !measured {
		it.Status = StatusUnknown
	}
}

// rank ordena as situações da mais tranquila à mais urgente.
func rank(status string) int {
	switch status {
	case StatusDue:
		return 3
	case StatusSoon:
		return 2
	case StatusUnknown:
		return 1
	}
	return 0
}

// SortByUrgency põe as vencidas primeiro, depois as perto, as sem hodômetro e
// as em dia; dentro de cada uma, as que vencem antes.
func SortByUrgency(list []*Item) {
	urgency := func(it *Item) float64 {
		// Dias até vencer; o km vira dias à razão de 100 km/dia e as horas, de
		// 8 h/dia (só para ordenar).
		u := math.Inf(1)
		if it.RemainingDays != nil {
			u = float64(*it.RemainingDays)
		}
		if it.RemainingKm != nil {
			u = math.Min(u, *it.RemainingKm/100)
		}
		if it.RemainingHours != nil {
			u = math.Min(u, *it.RemainingHours/8)
		}
		return u
	}
	sort.SliceStable(list, func(i, j int) bool {
		if ri, rj := rank(list[i].Status), rank(list[j].Status); ri != rj {
			return ri > rj
		}
		return urgency(list[i]) < urgency(list[j])
	})
}

func (s *Service) checkInput(in *ItemInput) error {
	in.Name = strings.Join(strings.Fields(in.Name), " ")
	switch {
	case in.Name == "" || utf8.RuneCountInString(in.Name) > MaxNameLength:
		return &InputError{fmt.Sprintf("dê um nome à manutenção (até %d caracteres)", MaxNameLength)}
	case in.EveryKm == nil && in.EveryMonths == nil && in.EveryHours == nil:
		return &InputError{"informe a cada quantos km, horas de motor ou meses"}
	case in.EveryKm != nil && (*in.EveryKm < 100 || *in.EveryKm > 1_000_000):
		return &InputError{"a cada quantos km: de 100 a 1.000.000"}
	case in.EveryHours != nil && (*in.EveryHours < 10 || *in.EveryHours > 100_000):
		return &InputError{"a cada quantas horas de motor: de 10 a 100.000"}
	case in.EveryMonths != nil && (*in.EveryMonths < 1 || *in.EveryMonths > 120):
		return &InputError{"a cada quantos meses: de 1 a 120"}
	case in.LastKm != nil && (math.IsNaN(*in.LastKm) || *in.LastKm < 0 || *in.LastKm > MaxOdometerKm):
		return &InputError{"o km da última vez está fora da faixa"}
	case in.LastHours != nil && (math.IsNaN(*in.LastHours) || *in.LastHours < 0 || *in.LastHours > MaxEngineHours):
		return &InputError{"as horas da última vez estão fora da faixa"}
	}
	if in.LastDate == "" {
		in.LastDate = s.today().Format(time.DateOnly)
	}
	last, err := time.Parse(time.DateOnly, in.LastDate)
	if err != nil || last.After(s.today()) {
		return &InputError{"a data da última vez precisa ser hoje ou antes"}
	}
	return nil
}

// Create põe a manutenção em cada veículo pedido. Sem o km da última vez,
// vale o hodômetro de agora de cada um (sem hodômetro, fica em branco até o
// cliente informar); sem as horas, o horímetro de agora.
func (s *Service) Create(ctx context.Context, vehicleIDs []uuid.UUID, in ItemInput) (int, error) {
	if err := s.checkInput(&in); err != nil {
		return 0, err
	}
	if len(vehicleIDs) == 0 || len(vehicleIDs) > MaxBulk {
		return 0, &InputError{fmt.Sprintf("escolha de 1 a %d veículos", MaxBulk)}
	}
	if (in.LastKm != nil || in.LastHours != nil) && len(vehicleIDs) > 1 {
		return 0, &InputError{"o km e as horas da última vez só valem para um veículo; em vários, cada um usa o hodômetro e o horímetro dele"}
	}
	var full int
	if err := s.db.QueryRow(ctx, `
		SELECT count(*) FROM (SELECT vehicle_id FROM maintenance_items WHERE vehicle_id = ANY($1)
			GROUP BY vehicle_id HAVING count(*) >= $2) f`, vehicleIDs, MaxItemsPerVehicle).Scan(&full); err != nil {
		return 0, database.MapError(err)
	}
	if full > 0 {
		return 0, &InputError{fmt.Sprintf("até %d manutenções por veículo: %d dos escolhidos já chegaram lá", MaxItemsPerVehicle, full)}
	}
	odometers, err := s.Odometers(ctx, vehicleIDs)
	if err != nil {
		return 0, err
	}
	lastKm := make([]*float64, len(vehicleIDs))
	lastHours := make([]float64, len(vehicleIDs))
	for i, id := range vehicleIDs {
		lastKm[i], lastHours[i] = odometers[id].Km, odometers[id].Hours
		if in.LastKm != nil {
			lastKm[i] = in.LastKm
		}
		if in.LastHours != nil {
			lastHours[i] = *in.LastHours
		}
	}
	tag, err := s.db.Exec(ctx, `
		INSERT INTO maintenance_items (vehicle_id, name, every_km, every_months, every_hours, last_km, last_hours, last_date)
		SELECT v, $2, $3, $4, $5, k, h, $6 FROM unnest($1::uuid[], $7::float8[], $8::float8[]) AS t(v, k, h)`,
		vehicleIDs, in.Name, in.EveryKm, in.EveryMonths, in.EveryHours, in.LastDate, lastKm, lastHours)
	if err != nil {
		return 0, database.MapError(err)
	}
	return int(tag.RowsAffected()), nil
}

// Update troca o nome, os intervalos e a última vez (para corrigir).
func (s *Service) Update(ctx context.Context, id uuid.UUID, in ItemInput) error {
	if err := s.checkInput(&in); err != nil {
		return err
	}
	tag, err := s.db.Exec(ctx, `
		UPDATE maintenance_items SET name = $2, every_km = $3, every_months = $4, every_hours = $5, last_date = $6,
			last_km = COALESCE($7, last_km), last_hours = COALESCE($8, last_hours), notified = NULL, updated_at = NOW()
		WHERE id = $1`, id, in.Name, in.EveryKm, in.EveryMonths, in.EveryHours, in.LastDate, in.LastKm, in.LastHours)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return database.ErrNotFound
	}
	return nil
}

// Delete apaga a manutenção (o histórico fica).
func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := s.db.Exec(ctx, `DELETE FROM maintenance_items WHERE id = $1`, id)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return database.ErrNotFound
	}
	return nil
}

// DoneInput é o registro de manutenções feitas: a data (vazia: hoje), o km
// e as horas de motor (só com uma manutenção; nas várias, o hodômetro e o
// horímetro de cada veículo) e uma nota.
type DoneInput struct {
	ItemIDs []uuid.UUID `json:"itemIds"`
	Date    string      `json:"date"`
	Km      *float64    `json:"km"`
	Hours   *float64    `json:"hours"`
	Notes   string      `json:"notes"`
}

// Done registra as manutenções feitas: vai para o histórico e a contagem
// recomeça daí.
func (s *Service) Done(ctx context.Context, in DoneInput, by uuid.UUID) (int, error) {
	if len(in.ItemIDs) == 0 || len(in.ItemIDs) > MaxBulk {
		return 0, &InputError{fmt.Sprintf("escolha de 1 a %d manutenções", MaxBulk)}
	}
	if (in.Km != nil || in.Hours != nil) && len(in.ItemIDs) > 1 {
		return 0, &InputError{"o km e as horas só valem para uma manutenção; em várias, cada uma usa o hodômetro e o horímetro do veículo"}
	}
	if in.Km != nil && (math.IsNaN(*in.Km) || *in.Km < 0 || *in.Km > MaxOdometerKm) {
		return 0, &InputError{"o km está fora da faixa"}
	}
	if in.Hours != nil && (math.IsNaN(*in.Hours) || *in.Hours < 0 || *in.Hours > MaxEngineHours) {
		return 0, &InputError{"as horas de motor estão fora da faixa"}
	}
	in.Notes = strings.TrimSpace(in.Notes)
	if utf8.RuneCountInString(in.Notes) > MaxNotesLength {
		return 0, &InputError{fmt.Sprintf("a observação vai até %d caracteres", MaxNotesLength)}
	}
	if in.Date == "" {
		in.Date = s.today().Format(time.DateOnly)
	}
	if day, err := time.Parse(time.DateOnly, in.Date); err != nil || day.After(s.today()) {
		return 0, &InputError{"a data precisa ser hoje ou antes"}
	}
	items, err := s.items(ctx, `i.id = ANY($1)`, in.ItemIDs)
	if err != nil {
		return 0, err
	}
	vehicleIDs := []uuid.UUID{}
	for _, it := range items {
		vehicleIDs = append(vehicleIDs, it.VehicleID)
	}
	odometers, err := s.Odometers(ctx, vehicleIDs)
	if err != nil {
		return 0, err
	}
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		for _, it := range items {
			km, hours := odometers[it.VehicleID].Km, odometers[it.VehicleID].Hours
			if in.Km != nil {
				km = in.Km
			}
			if in.Hours != nil {
				hours = *in.Hours
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO maintenance_records (vehicle_id, item_id, name, done_date, km, hours, notes, created_by)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, it.VehicleID, it.ID, it.Name, in.Date, km, hours, in.Notes, by); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				UPDATE maintenance_items SET last_date = $2, last_km = COALESCE($3, last_km), last_hours = $4,
					notified = NULL, updated_at = NOW()
				WHERE id = $1`, it.ID, in.Date, km, hours); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, database.MapError(err)
	}
	return len(items), nil
}

// Records é o histórico do veículo, do mais recente.
func (s *Service) Records(ctx context.Context, vehicleID uuid.UUID, limit int) ([]Record, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, vehicle_id, item_id, name, done_date, km, hours, notes, created_at FROM maintenance_records
		WHERE vehicle_id = $1 ORDER BY done_date DESC, created_at DESC LIMIT $2`, vehicleID, limit)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	out := []Record{}
	for rows.Next() {
		var r Record
		var day time.Time
		if err := rows.Scan(&r.ID, &r.VehicleID, &r.ItemID, &r.Name, &day, &r.Km, &r.Hours, &r.Notes, &r.CreatedAt); err != nil {
			return nil, database.MapError(err)
		}
		r.DoneDate = day.Format(time.DateOnly)
		out = append(out, r)
	}
	return out, database.MapError(rows.Err())
}
