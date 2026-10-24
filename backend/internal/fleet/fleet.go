// Package fleet é a gestão da frota das empresas: os motoristas e quem
// dirige cada veículo (a jornada e as multas acham o motorista por aqui), os
// custos (combustível, pedágio, manutenção...), as multas e as locações. Tudo
// é da conta do cliente (account_id: o titular); quem chama já conferiu que
// os veículos são da conta e que a pessoa os vê.
package fleet

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// InputError é um pedido recusado; a mensagem vai para a tela.
type InputError struct{ Message string }

func (e *InputError) Error() string { return e.Message }

func invalid(format string, args ...any) error { return &InputError{fmt.Sprintf(format, args...)} }

func mapErr(err error) error {
	var input *InputError
	if errors.As(err, &input) {
		return input
	}
	return database.MapError(err)
}

func clean(s string) string { return strings.Join(strings.Fields(s), " ") }

func tooLong(s string, max int) bool { return utf8.RuneCountInString(s) > max }

type Service struct {
	db       *database.DB
	loc      *time.Location
	now      func() time.Time
	notifier Notifier
	log      *slog.Logger
}

func NewService(db *database.DB, timezone string, notifier Notifier, log *slog.Logger) *Service {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		loc = time.FixedZone("BRT", -3*60*60)
	}
	return &Service{db: db, loc: loc, now: time.Now, notifier: notifier, log: log.With("component", "fleet")}
}

// SetClock troca o relógio (testes).
func (s *Service) SetClock(now func() time.Time) { s.now = now }

// ---------------------------------------------------------------------------
// Motoristas
// ---------------------------------------------------------------------------

// Driver é um motorista da conta, com o veículo que dirige agora.
type Driver struct {
	ID              uuid.UUID  `json:"id"`
	Name            string     `json:"name"`
	Phone           string     `json:"phone"`
	Document        string     `json:"document"`
	LicenseNumber   string     `json:"licenseNumber"`
	LicenseCategory string     `json:"licenseCategory"`
	LicenseExpires  *string    `json:"licenseExpires"`
	Active          bool       `json:"active"`
	Notes           string     `json:"notes"`
	VehicleID       *uuid.UUID `json:"vehicleId"`
	VehicleName     string     `json:"vehicleName"`
	Since           *time.Time `json:"since"`
	CreatedAt       time.Time  `json:"createdAt"`
}

// DriverInput é o cadastro do motorista.
type DriverInput struct {
	Name            string `json:"name"`
	Phone           string `json:"phone"`
	Document        string `json:"document"`
	LicenseNumber   string `json:"licenseNumber"`
	LicenseCategory string `json:"licenseCategory"`
	LicenseExpires  string `json:"licenseExpires"`
	Active          *bool  `json:"active"`
	Notes           string `json:"notes"`
}

const selectDriver = `
	SELECT d.id, d.name, d.phone, d.document, d.license_number, d.license_category, d.license_expires, d.active, d.notes,
		a.vehicle_id, COALESCE(v.name, ''), a.starts_at, d.created_at
	FROM drivers d
	LEFT JOIN driver_assignments a ON a.driver_id = d.id AND a.ends_at IS NULL
	LEFT JOIN vehicles v ON v.id = a.vehicle_id`

func scanDriver(row database.Scanner) (*Driver, error) {
	var d Driver
	var expires *time.Time
	if err := row.Scan(&d.ID, &d.Name, &d.Phone, &d.Document, &d.LicenseNumber, &d.LicenseCategory, &expires, &d.Active,
		&d.Notes, &d.VehicleID, &d.VehicleName, &d.Since, &d.CreatedAt); err != nil {
		return nil, database.MapError(err)
	}
	if expires != nil {
		day := expires.Format(time.DateOnly)
		d.LicenseExpires = &day
	}
	return &d, nil
}

// Drivers são os motoristas da conta, por nome.
func (s *Service) Drivers(ctx context.Context, account uuid.UUID) ([]*Driver, error) {
	rows, err := s.db.Query(ctx, selectDriver+` WHERE d.account_id = $1 ORDER BY d.active DESC, d.name`, account)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	out := []*Driver{}
	for rows.Next() {
		d, err := scanDriver(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, database.MapError(rows.Err())
}

// Driver é um motorista da conta (de outra: não encontrado).
func (s *Service) Driver(ctx context.Context, account, id uuid.UUID) (*Driver, error) {
	return scanDriver(s.db.QueryRow(ctx, selectDriver+` WHERE d.id = $1 AND d.account_id = $2`, id, account))
}

func checkDriver(in *DriverInput) (*time.Time, error) {
	in.Name, in.Phone, in.Document = clean(in.Name), clean(in.Phone), clean(in.Document)
	in.LicenseNumber, in.LicenseCategory = clean(in.LicenseNumber), strings.ToUpper(clean(in.LicenseCategory))
	in.Notes = strings.TrimSpace(in.Notes)
	switch {
	case in.Name == "" || tooLong(in.Name, 120):
		return nil, invalid("informe o nome do motorista (até 120 caracteres)")
	case tooLong(in.Phone, 30) || tooLong(in.Document, 30) || tooLong(in.LicenseNumber, 30):
		return nil, invalid("telefone, CPF e CNH vão até 30 caracteres")
	case tooLong(in.LicenseCategory, 5):
		return nil, invalid("a categoria da CNH vai até 5 letras (AB, D, E...)")
	case tooLong(in.Notes, 500):
		return nil, invalid("as observações vão até 500 caracteres")
	}
	if in.LicenseExpires == "" {
		return nil, nil
	}
	day, err := time.Parse(time.DateOnly, in.LicenseExpires)
	if err != nil {
		return nil, invalid("validade da CNH inválida (AAAA-MM-DD)")
	}
	return &day, nil
}

// CreateDriver cadastra o motorista.
func (s *Service) CreateDriver(ctx context.Context, account uuid.UUID, in DriverInput) (*Driver, error) {
	expires, err := checkDriver(&in)
	if err != nil {
		return nil, err
	}
	var id uuid.UUID
	if err := s.db.QueryRow(ctx, `
		INSERT INTO drivers (account_id, name, phone, document, license_number, license_category, license_expires, notes)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`,
		account, in.Name, in.Phone, in.Document, in.LicenseNumber, in.LicenseCategory, expires, in.Notes).Scan(&id); err != nil {
		return nil, database.MapError(err)
	}
	return s.Driver(ctx, account, id)
}

// UpdateDriver altera o cadastro; desativado, deixa de dirigir o veículo atual.
func (s *Service) UpdateDriver(ctx context.Context, account, id uuid.UUID, in DriverInput) (*Driver, error) {
	expires, err := checkDriver(&in)
	if err != nil {
		return nil, err
	}
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE drivers SET name = $3, phone = $4, document = $5, license_number = $6, license_category = $7,
				license_expires = $8, notes = $9, active = COALESCE($10, active), updated_at = NOW()
			WHERE id = $1 AND account_id = $2`,
			id, account, in.Name, in.Phone, in.Document, in.LicenseNumber, in.LicenseCategory, expires, in.Notes, in.Active)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return database.ErrNotFound
		}
		if in.Active != nil && !*in.Active {
			_, err = tx.Exec(ctx, `UPDATE driver_assignments SET ends_at = $2 WHERE driver_id = $1 AND ends_at IS NULL`, id, s.now())
		}
		return err
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return s.Driver(ctx, account, id)
}

// Assign põe o motorista no veículo a partir de agora (o anterior do
// veículo, e o veículo anterior do motorista, terminam agora). driverID
// nulo tira o motorista do veículo.
func (s *Service) Assign(ctx context.Context, account, vehicleID uuid.UUID, driverID *uuid.UUID, by uuid.UUID) error {
	now := s.now()
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if driverID != nil {
			var active bool
			if err := tx.QueryRow(ctx, `SELECT active FROM drivers WHERE id = $1 AND account_id = $2 FOR UPDATE`,
				*driverID, account).Scan(&active); err != nil {
				return err
			}
			if !active {
				return invalid("o motorista está desativado")
			}
			if _, err := tx.Exec(ctx, `UPDATE driver_assignments SET ends_at = $2 WHERE driver_id = $1 AND ends_at IS NULL`,
				*driverID, now); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE driver_assignments SET ends_at = $2 WHERE vehicle_id = $1 AND ends_at IS NULL`,
			vehicleID, now); err != nil {
			return err
		}
		if driverID == nil {
			return nil
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO driver_assignments (vehicle_id, driver_id, starts_at, created_by) VALUES ($1, $2, $3, $4)`,
			vehicleID, *driverID, now, by)
		return err
	})
	return mapErr(err)
}

// DriverAt é o motorista que dirigia o veículo no instante (nil se ninguém).
func (s *Service) DriverAt(ctx context.Context, vehicleID uuid.UUID, at time.Time) (*uuid.UUID, string, error) {
	var id uuid.UUID
	var name string
	err := s.db.QueryRow(ctx, `
		SELECT d.id, d.name FROM driver_assignments a JOIN drivers d ON d.id = a.driver_id
		WHERE a.vehicle_id = $1 AND a.starts_at <= $2 AND (a.ends_at IS NULL OR a.ends_at > $2)
		ORDER BY a.starts_at DESC LIMIT 1`, vehicleID, at).Scan(&id, &name)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", database.MapError(err)
	}
	return &id, name, nil
}

// Assignment é um trecho de quem dirigiu o veículo.
type Assignment struct {
	VehicleID  uuid.UUID  `json:"vehicleId"`
	DriverID   uuid.UUID  `json:"driverId"`
	DriverName string     `json:"driverName"`
	StartsAt   time.Time  `json:"startsAt"`
	EndsAt     *time.Time `json:"endsAt"`
}

// Assignments são os trechos dos veículos que cruzam [from, to).
func (s *Service) Assignments(ctx context.Context, vehicleIDs []uuid.UUID, from, to time.Time) ([]Assignment, error) {
	rows, err := s.db.Query(ctx, `
		SELECT a.vehicle_id, a.driver_id, d.name, a.starts_at, a.ends_at FROM driver_assignments a
		JOIN drivers d ON d.id = a.driver_id
		WHERE a.vehicle_id = ANY($1) AND a.starts_at < $3 AND (a.ends_at IS NULL OR a.ends_at > $2)
		ORDER BY a.vehicle_id, a.starts_at`, vehicleIDs, from, to)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	out := []Assignment{}
	for rows.Next() {
		var a Assignment
		if err := rows.Scan(&a.VehicleID, &a.DriverID, &a.DriverName, &a.StartsAt, &a.EndsAt); err != nil {
			return nil, database.MapError(err)
		}
		out = append(out, a)
	}
	return out, database.MapError(rows.Err())
}
