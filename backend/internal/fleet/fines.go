package fleet

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// As situações de uma multa.
var FineStatuses = []string{"pending", "paid", "appealed", "canceled"}

// Fine é uma multa de um veículo, com o motorista da hora.
type Fine struct {
	ID              uuid.UUID  `json:"id"`
	VehicleID       uuid.UUID  `json:"vehicleId"`
	VehicleName     string     `json:"vehicleName"`
	Plate           string     `json:"plate"`
	DriverID        *uuid.UUID `json:"driverId"`
	DriverName      string     `json:"driverName"`
	OccurredAt      time.Time  `json:"occurredAt"`
	Place           string     `json:"place"`
	Description     string     `json:"description"`
	Code            string     `json:"code"`
	AmountCents     int        `json:"amountCents"`
	Points          int        `json:"points"`
	DueOn           *string    `json:"dueOn"`
	Status          string     `json:"status"`
	DriverIndicated bool       `json:"driverIndicated"`
	Notes           string     `json:"notes"`
	CreatedAt       time.Time  `json:"createdAt"`
}

// FineInput é a multa lançada. Sem DriverID, vale quem dirigia na hora.
type FineInput struct {
	VehicleID       uuid.UUID  `json:"vehicleId"`
	DriverID        *uuid.UUID `json:"driverId"`
	OccurredAt      time.Time  `json:"occurredAt"`
	Place           string     `json:"place"`
	Description     string     `json:"description"`
	Code            string     `json:"code"`
	AmountCents     int        `json:"amountCents"`
	Points          int        `json:"points"`
	DueOn           string     `json:"dueOn"`
	Status          string     `json:"status"`
	DriverIndicated bool       `json:"driverIndicated"`
	Notes           string     `json:"notes"`
}

const selectFine = `
	SELECT f.id, f.vehicle_id, v.name, COALESCE(v.plate, ''), f.driver_id, COALESCE(d.name, ''), f.occurred_at, f.place,
		f.description, f.code, f.amount_cents, f.points, f.due_on, f.status, f.driver_indicated, f.notes, f.created_at
	FROM traffic_fines f JOIN vehicles v ON v.id = f.vehicle_id LEFT JOIN drivers d ON d.id = f.driver_id`

func scanFine(row database.Scanner) (*Fine, error) {
	var f Fine
	var due *time.Time
	if err := row.Scan(&f.ID, &f.VehicleID, &f.VehicleName, &f.Plate, &f.DriverID, &f.DriverName, &f.OccurredAt, &f.Place,
		&f.Description, &f.Code, &f.AmountCents, &f.Points, &due, &f.Status, &f.DriverIndicated, &f.Notes, &f.CreatedAt); err != nil {
		return nil, database.MapError(err)
	}
	if due != nil {
		day := due.Format(time.DateOnly)
		f.DueOn = &day
	}
	return &f, nil
}

// Fines são as multas dos veículos, das mais recentes.
func (s *Service) Fines(ctx context.Context, account uuid.UUID, vehicleIDs []uuid.UUID) ([]*Fine, error) {
	rows, err := s.db.Query(ctx, selectFine+`
		WHERE f.account_id = $1 AND f.vehicle_id = ANY($2) ORDER BY f.occurred_at DESC LIMIT 1000`, account, vehicleIDs)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	out := []*Fine{}
	for rows.Next() {
		f, err := scanFine(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, database.MapError(rows.Err())
}

func (s *Service) checkFine(ctx context.Context, account uuid.UUID, in *FineInput) (*time.Time, error) {
	in.Place, in.Description, in.Code = clean(in.Place), clean(in.Description), clean(in.Code)
	in.Notes = strings.TrimSpace(in.Notes)
	if in.Status == "" {
		in.Status = "pending"
	}
	switch {
	case in.OccurredAt.IsZero() || in.OccurredAt.After(s.now().Add(time.Hour)):
		return nil, invalid("informe quando foi a infração")
	case in.Description == "" || tooLong(in.Description, 200):
		return nil, invalid("descreva a infração (até 200 caracteres)")
	case tooLong(in.Place, 200) || tooLong(in.Code, 20) || tooLong(in.Notes, 500):
		return nil, invalid("o local vai até 200 caracteres, o código até 20 e as observações até 500")
	case in.AmountCents < 0 || in.AmountCents > MaxExpenseCents:
		return nil, invalid("valor da multa inválido")
	case in.Points < 0 || in.Points > 7:
		return nil, invalid("os pontos vão de 0 a 7")
	case !slices.Contains(FineStatuses, in.Status):
		return nil, invalid("situação inválida")
	}
	if in.DriverID != nil {
		if _, err := s.Driver(ctx, account, *in.DriverID); err != nil {
			return nil, invalid("motorista não encontrado")
		}
	} else {
		driver, _, err := s.DriverAt(ctx, in.VehicleID, in.OccurredAt)
		if err != nil {
			return nil, err
		}
		in.DriverID = driver
	}
	if in.DueOn == "" {
		return nil, nil
	}
	day, err := time.Parse(time.DateOnly, in.DueOn)
	if err != nil {
		return nil, invalid("vencimento inválido")
	}
	return &day, nil
}

// CreateFine lança a multa (com o motorista de quem dirigia na hora, se
// não vier um).
func (s *Service) CreateFine(ctx context.Context, account, by uuid.UUID, in FineInput) (*Fine, error) {
	due, err := s.checkFine(ctx, account, &in)
	if err != nil {
		return nil, err
	}
	var id uuid.UUID
	if err := s.db.QueryRow(ctx, `
		INSERT INTO traffic_fines (account_id, vehicle_id, driver_id, occurred_at, place, description, code, amount_cents,
			points, due_on, status, driver_indicated, notes, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14) RETURNING id`,
		account, in.VehicleID, in.DriverID, in.OccurredAt, in.Place, in.Description, in.Code, in.AmountCents, in.Points,
		due, in.Status, in.DriverIndicated, in.Notes, by).Scan(&id); err != nil {
		return nil, database.MapError(err)
	}
	return scanFine(s.db.QueryRow(ctx, selectFine+` WHERE f.id = $1`, id))
}

// UpdateFine altera a multa da conta (o veículo fica o mesmo).
func (s *Service) UpdateFine(ctx context.Context, account, id uuid.UUID, in FineInput) (*Fine, error) {
	current, err := scanFine(s.db.QueryRow(ctx, selectFine+` WHERE f.id = $1 AND f.account_id = $2`, id, account))
	if err != nil {
		return nil, err
	}
	in.VehicleID = current.VehicleID
	due, err := s.checkFine(ctx, account, &in)
	if err != nil {
		return nil, err
	}
	if _, err := s.db.Exec(ctx, `
		UPDATE traffic_fines SET driver_id = $2, occurred_at = $3, place = $4, description = $5, code = $6,
			amount_cents = $7, points = $8, due_on = $9, status = $10, driver_indicated = $11, notes = $12, updated_at = NOW()
		WHERE id = $1`, id, in.DriverID, in.OccurredAt, in.Place, in.Description, in.Code, in.AmountCents, in.Points, due,
		in.Status, in.DriverIndicated, in.Notes); err != nil {
		return nil, database.MapError(err)
	}
	return scanFine(s.db.QueryRow(ctx, selectFine+` WHERE f.id = $1`, id))
}

// DeleteFine apaga a multa da conta.
func (s *Service) DeleteFine(ctx context.Context, account, id uuid.UUID) error {
	tag, err := s.db.Exec(ctx, `DELETE FROM traffic_fines WHERE id = $1 AND account_id = $2`, id, account)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return database.ErrNotFound
	}
	return nil
}
