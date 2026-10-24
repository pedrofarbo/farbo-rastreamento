package fleet

import (
	"context"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// Os tipos de custo.
var ExpenseKinds = []string{"fuel", "maintenance", "toll", "parking", "insurance", "tax", "wash", "fine", "other"}

// Expense é um custo de um veículo.
type Expense struct {
	ID          uuid.UUID `json:"id"`
	VehicleID   uuid.UUID `json:"vehicleId"`
	VehicleName string    `json:"vehicleName"`
	Plate       string    `json:"plate"`
	Kind        string    `json:"kind"`
	SpentOn     string    `json:"spentOn"`
	AmountCents int       `json:"amountCents"`
	Liters      *float64  `json:"liters"`
	OdometerKm  *float64  `json:"odometerKm"`
	FullTank    bool      `json:"fullTank"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"createdAt"`
}

// ExpenseInput é o custo lançado.
type ExpenseInput struct {
	VehicleID   uuid.UUID `json:"vehicleId"`
	Kind        string    `json:"kind"`
	SpentOn     string    `json:"spentOn"`
	AmountCents int       `json:"amountCents"`
	Liters      *float64  `json:"liters"`
	OdometerKm  *float64  `json:"odometerKm"`
	FullTank    bool      `json:"fullTank"`
	Description string    `json:"description"`
}

// MaxExpenseCents: R$ 1 milhão num lançamento é erro de digitação.
const MaxExpenseCents = 100_000_000

const selectExpense = `
	SELECT e.id, e.vehicle_id, v.name, COALESCE(v.plate, ''), e.kind, e.spent_on, e.amount_cents, e.liters::float8,
		e.odometer_km::float8, e.full_tank, e.description, e.created_at
	FROM fleet_expenses e JOIN vehicles v ON v.id = e.vehicle_id`

func scanExpense(row database.Scanner) (*Expense, error) {
	var e Expense
	var day time.Time
	if err := row.Scan(&e.ID, &e.VehicleID, &e.VehicleName, &e.Plate, &e.Kind, &day, &e.AmountCents, &e.Liters,
		&e.OdometerKm, &e.FullTank, &e.Description, &e.CreatedAt); err != nil {
		return nil, database.MapError(err)
	}
	e.SpentOn = day.Format(time.DateOnly)
	return &e, nil
}

// Expenses são os custos dos veículos no período [from, to] (dias), dos mais recentes.
func (s *Service) Expenses(ctx context.Context, account uuid.UUID, vehicleIDs []uuid.UUID, from, to string) ([]*Expense, error) {
	rows, err := s.db.Query(ctx, selectExpense+`
		WHERE e.account_id = $1 AND e.vehicle_id = ANY($2) AND e.spent_on BETWEEN $3 AND $4
		ORDER BY e.spent_on DESC, e.created_at DESC LIMIT 2000`, account, vehicleIDs, from, to)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	out := []*Expense{}
	for rows.Next() {
		e, err := scanExpense(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, database.MapError(rows.Err())
}

func (s *Service) checkExpense(in *ExpenseInput) error {
	in.Description = clean(in.Description)
	switch {
	case !slices.Contains(ExpenseKinds, in.Kind):
		return invalid("escolha o tipo do custo")
	case in.AmountCents <= 0 || in.AmountCents > MaxExpenseCents:
		return invalid("informe o valor")
	case tooLong(in.Description, 200):
		return invalid("a descrição vai até 200 caracteres")
	case in.Liters != nil && (math.IsNaN(*in.Liters) || *in.Liters <= 0 || *in.Liters > 2000):
		return invalid("litros fora da faixa")
	case in.OdometerKm != nil && (math.IsNaN(*in.OdometerKm) || *in.OdometerKm < 0 || *in.OdometerKm > 5_000_000):
		return invalid("hodômetro fora da faixa")
	}
	day, err := time.Parse(time.DateOnly, in.SpentOn)
	if err != nil {
		return invalid("data inválida")
	}
	if day.After(s.now().In(s.loc).AddDate(0, 0, 1)) {
		return invalid("a data não pode ser no futuro")
	}
	if in.Kind != "fuel" {
		in.Liters, in.FullTank = nil, false
	}
	return nil
}

// CreateExpense lança o custo.
func (s *Service) CreateExpense(ctx context.Context, account, by uuid.UUID, in ExpenseInput) (*Expense, error) {
	if err := s.checkExpense(&in); err != nil {
		return nil, err
	}
	var id uuid.UUID
	if err := s.db.QueryRow(ctx, `
		INSERT INTO fleet_expenses (account_id, vehicle_id, kind, spent_on, amount_cents, liters, odometer_km, full_tank,
			description, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id`,
		account, in.VehicleID, in.Kind, in.SpentOn, in.AmountCents, in.Liters, in.OdometerKm, in.FullTank,
		in.Description, by).Scan(&id); err != nil {
		return nil, database.MapError(err)
	}
	return scanExpense(s.db.QueryRow(ctx, selectExpense+` WHERE e.id = $1`, id))
}

// DeleteExpense apaga o custo da conta.
func (s *Service) DeleteExpense(ctx context.Context, account, id uuid.UUID) (*Expense, error) {
	e, err := scanExpense(s.db.QueryRow(ctx, selectExpense+` WHERE e.id = $1 AND e.account_id = $2`, id, account))
	if err != nil {
		return nil, err
	}
	if _, err := s.db.Exec(ctx, `DELETE FROM fleet_expenses WHERE id = $1`, id); err != nil {
		return nil, database.MapError(err)
	}
	return e, nil
}

// CostRow é o custo de um veículo no período.
type CostRow struct {
	VehicleID   uuid.UUID        `json:"vehicleId"`
	TotalCents  int64            `json:"totalCents"`
	ByKind      map[string]int64 `json:"byKind"`
	DistanceKm  float64          `json:"distanceKm"`
	CentsPerKm  *float64         `json:"centsPerKm"`
	Liters      float64          `json:"liters"`
	KmPerLiter  *float64         `json:"kmPerLiter"`
	FuelEntries int              `json:"fuelEntries"`
}

// Costs soma os custos de cada veículo no período e cruza com os km do GPS:
// o custo por km e o consumo (km/l). O consumo vem de um tanque cheio ao
// outro pelo hodômetro informado; sem isso, dos km do GPS sobre os litros do
// período.
func (s *Service) Costs(ctx context.Context, account uuid.UUID, vehicleIDs []uuid.UUID, from, to string) ([]CostRow, error) {
	byVehicle := map[uuid.UUID]*CostRow{}
	row := func(id uuid.UUID) *CostRow {
		if r, ok := byVehicle[id]; ok {
			return r
		}
		r := &CostRow{VehicleID: id, ByKind: map[string]int64{}}
		byVehicle[id] = r
		return r
	}
	for _, id := range vehicleIDs {
		row(id)
	}
	expenses, err := s.Expenses(ctx, account, vehicleIDs, from, to)
	if err != nil {
		return nil, err
	}
	fuel := map[uuid.UUID][]*Expense{}
	for _, e := range expenses {
		r := row(e.VehicleID)
		r.TotalCents += int64(e.AmountCents)
		r.ByKind[e.Kind] += int64(e.AmountCents)
		if e.Kind == "fuel" && e.Liters != nil {
			r.Liters += *e.Liters
			r.FuelEntries++
			fuel[e.VehicleID] = append(fuel[e.VehicleID], e)
		}
	}
	rows, err := s.db.Query(ctx, `
		SELECT s.vehicle_id, sum(s.distance_m) FROM vehicle_daily_stats s
		WHERE s.vehicle_id = ANY($1) AND s.day BETWEEN $2 AND $3 GROUP BY s.vehicle_id`, vehicleIDs, from, to)
	if err != nil {
		return nil, database.MapError(err)
	}
	for rows.Next() {
		var id uuid.UUID
		var meters float64
		if err := rows.Scan(&id, &meters); err != nil {
			rows.Close()
			return nil, database.MapError(err)
		}
		row(id).DistanceKm = math.Round(meters/100) / 10
	}
	rows.Close()
	out := make([]CostRow, 0, len(byVehicle))
	for _, id := range vehicleIDs {
		r := byVehicle[id]
		if r.DistanceKm > 0 && r.TotalCents > 0 {
			per := math.Round(float64(r.TotalCents)/r.DistanceKm*10) / 10
			r.CentsPerKm = &per
		}
		r.Liters = math.Round(r.Liters*100) / 100
		if kml := consumption(fuel[id]); kml != nil {
			r.KmPerLiter = kml
		} else if r.Liters > 0 && r.DistanceKm > 0 {
			kml := math.Round(r.DistanceKm/r.Liters*10) / 10
			r.KmPerLiter = &kml
		}
		out = append(out, *r)
	}
	return out, nil
}

// consumption é o km/l de um tanque cheio ao outro pelo hodômetro: os km
// entre o primeiro e o último tanque cheio sobre os litros abastecidos
// depois do primeiro. Nil sem dois tanques cheios com hodômetro.
func consumption(entries []*Expense) *float64 {
	list := slices.Clone(entries)
	slices.SortFunc(list, func(a, b *Expense) int {
		if c := strings.Compare(a.SpentOn, b.SpentOn); c != 0 {
			return c
		}
		return a.CreatedAt.Compare(b.CreatedAt)
	})
	started := false
	// pending: os litros desde o último tanque cheio (só contam quando outro
	// tanque cheio fecha o trecho).
	var liters, km, pending, lastFull float64
	for _, e := range list {
		if !started {
			if e.FullTank && e.OdometerKm != nil {
				started, lastFull = true, *e.OdometerKm
			}
			continue
		}
		if e.Liters != nil {
			pending += *e.Liters
		}
		if e.FullTank && e.OdometerKm != nil && *e.OdometerKm > lastFull {
			km += *e.OdometerKm - lastFull
			liters += pending
			lastFull, pending = *e.OdometerKm, 0
		}
	}
	if km <= 0 || liters <= 0 {
		return nil
	}
	v := math.Round(km/liters*10) / 10
	return &v
}
