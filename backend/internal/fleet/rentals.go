package fleet

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/mail"
)

// As locações: o veículo com quem alugou, de quando a quando, e o km na
// retirada e na devolução (o hodômetro do sistema, se a pessoa não informar).
// Passou da hora sem devolver: a conta é avisada uma vez.

// Rental é uma locação.
type Rental struct {
	ID             uuid.UUID  `json:"id"`
	VehicleID      uuid.UUID  `json:"vehicleId"`
	VehicleName    string     `json:"vehicleName"`
	Plate          string     `json:"plate"`
	RenterName     string     `json:"renterName"`
	RenterPhone    string     `json:"renterPhone"`
	RenterDocument string     `json:"renterDocument"`
	StartsAt       time.Time  `json:"startsAt"`
	EndsAt         time.Time  `json:"endsAt"`
	ReturnedAt     *time.Time `json:"returnedAt"`
	KmStart        *float64   `json:"kmStart"`
	KmEnd          *float64   `json:"kmEnd"`
	Notes          string     `json:"notes"`
	// Overdue: passou da hora e não voltou.
	Overdue   bool      `json:"overdue"`
	CreatedAt time.Time `json:"createdAt"`
}

// RentalInput é a locação lançada.
type RentalInput struct {
	VehicleID      uuid.UUID `json:"vehicleId"`
	RenterName     string    `json:"renterName"`
	RenterPhone    string    `json:"renterPhone"`
	RenterDocument string    `json:"renterDocument"`
	StartsAt       time.Time `json:"startsAt"`
	EndsAt         time.Time `json:"endsAt"`
	KmStart        *float64  `json:"kmStart"`
	Notes          string    `json:"notes"`
}

// Odometer dá o hodômetro de agora do veículo (maintenance.Service), para o
// km da retirada e da devolução.
type Odometer func(ctx context.Context, vehicleID uuid.UUID) (*float64, error)

// Notifier avisa a conta da locação atrasada (mail.FleetMailer).
type Notifier interface {
	RentalOverdue(ctx context.Context, to, name string, n mail.RentalNotice) error
}

const selectRental = `
	SELECT r.id, r.vehicle_id, v.name, COALESCE(v.plate, ''), r.renter_name, r.renter_phone, r.renter_document,
		r.starts_at, r.ends_at, r.returned_at, r.km_start::float8, r.km_end::float8, r.notes, r.created_at
	FROM rentals r JOIN vehicles v ON v.id = r.vehicle_id`

func (s *Service) scanRental(row database.Scanner) (*Rental, error) {
	var r Rental
	if err := row.Scan(&r.ID, &r.VehicleID, &r.VehicleName, &r.Plate, &r.RenterName, &r.RenterPhone, &r.RenterDocument,
		&r.StartsAt, &r.EndsAt, &r.ReturnedAt, &r.KmStart, &r.KmEnd, &r.Notes, &r.CreatedAt); err != nil {
		return nil, database.MapError(err)
	}
	r.Overdue = r.ReturnedAt == nil && s.now().After(r.EndsAt)
	return &r, nil
}

// Rentals são as locações dos veículos: as em aberto primeiro, depois as mais recentes.
func (s *Service) Rentals(ctx context.Context, account uuid.UUID, vehicleIDs []uuid.UUID) ([]*Rental, error) {
	rows, err := s.db.Query(ctx, selectRental+`
		WHERE r.account_id = $1 AND r.vehicle_id = ANY($2)
		ORDER BY r.returned_at IS NULL DESC, r.starts_at DESC LIMIT 1000`, account, vehicleIDs)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	out := []*Rental{}
	for rows.Next() {
		r, err := s.scanRental(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, database.MapError(rows.Err())
}

// CreateRental lança a locação (uma em aberto por veículo). Sem o km da
// retirada, vale o hodômetro de agora.
func (s *Service) CreateRental(ctx context.Context, account, by uuid.UUID, in RentalInput, odometer Odometer) (*Rental, error) {
	in.RenterName, in.RenterPhone, in.RenterDocument = clean(in.RenterName), clean(in.RenterPhone), clean(in.RenterDocument)
	in.Notes = strings.TrimSpace(in.Notes)
	switch {
	case in.RenterName == "" || tooLong(in.RenterName, 120):
		return nil, invalid("informe quem alugou (até 120 caracteres)")
	case tooLong(in.RenterPhone, 30) || tooLong(in.RenterDocument, 30) || tooLong(in.Notes, 500):
		return nil, invalid("telefone e documento vão até 30 caracteres; as observações, até 500")
	case in.StartsAt.IsZero() || in.EndsAt.IsZero() || !in.EndsAt.After(in.StartsAt):
		return nil, invalid("a devolução precisa ser depois da retirada")
	case in.EndsAt.Sub(in.StartsAt) > 366*24*time.Hour:
		return nil, invalid("a locação vai até um ano")
	case in.KmStart != nil && (math.IsNaN(*in.KmStart) || *in.KmStart < 0):
		return nil, invalid("km da retirada inválido")
	}
	if in.KmStart == nil && odometer != nil {
		km, err := odometer(ctx, in.VehicleID)
		if err != nil {
			return nil, err
		}
		in.KmStart = km
	}
	var id uuid.UUID
	err := s.db.QueryRow(ctx, `
		INSERT INTO rentals (account_id, vehicle_id, renter_name, renter_phone, renter_document, starts_at, ends_at, km_start,
			notes, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id`,
		account, in.VehicleID, in.RenterName, in.RenterPhone, in.RenterDocument, in.StartsAt, in.EndsAt, in.KmStart,
		in.Notes, by).Scan(&id)
	if errors.Is(database.MapError(err), database.ErrConflict) {
		return nil, invalid("este veículo já está locado: registre a devolução antes")
	}
	if err != nil {
		return nil, database.MapError(err)
	}
	return s.scanRental(s.db.QueryRow(ctx, selectRental+` WHERE r.id = $1`, id))
}

// ExtendRental muda a data de devolução de uma locação em aberto (e o aviso
// de atraso volta a valer).
func (s *Service) ExtendRental(ctx context.Context, account, id uuid.UUID, endsAt time.Time) (*Rental, error) {
	tag, err := s.db.Exec(ctx, `
		UPDATE rentals SET ends_at = $3, overdue_notified_at = NULL, updated_at = NOW()
		WHERE id = $1 AND account_id = $2 AND returned_at IS NULL AND starts_at < $3`, id, account, endsAt)
	if err != nil {
		return nil, database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return nil, invalid("só dá para mudar a devolução de uma locação em aberto, para depois da retirada")
	}
	return s.scanRental(s.db.QueryRow(ctx, selectRental+` WHERE r.id = $1`, id))
}

// ReturnRental registra a devolução agora (sem o km, o hodômetro de agora).
func (s *Service) ReturnRental(ctx context.Context, account, id uuid.UUID, kmEnd *float64, odometer Odometer) (*Rental, error) {
	r, err := s.scanRental(s.db.QueryRow(ctx, selectRental+` WHERE r.id = $1 AND r.account_id = $2`, id, account))
	if err != nil {
		return nil, err
	}
	if r.ReturnedAt != nil {
		return nil, invalid("a devolução já foi registrada")
	}
	if kmEnd == nil && odometer != nil {
		if kmEnd, err = odometer(ctx, r.VehicleID); err != nil {
			return nil, err
		}
	}
	if kmEnd != nil && r.KmStart != nil && *kmEnd < *r.KmStart {
		return nil, invalid("o km da devolução é menor que o da retirada")
	}
	if _, err := s.db.Exec(ctx, `
		UPDATE rentals SET returned_at = $2, km_end = $3, updated_at = NOW() WHERE id = $1`, id, s.now(), kmEnd); err != nil {
		return nil, database.MapError(err)
	}
	return s.scanRental(s.db.QueryRow(ctx, selectRental+` WHERE r.id = $1`, id))
}

// NotifyOverdue avisa (por e-mail, ao titular e aos Gestores da conta) das
// locações que passaram da hora sem devolver, uma vez cada.
func (s *Service) NotifyOverdue(ctx context.Context) (int, error) {
	if s.notifier == nil {
		return 0, nil
	}
	rows, err := s.db.Query(ctx, selectRental+`
		WHERE r.returned_at IS NULL AND r.ends_at < $1 AND r.overdue_notified_at IS NULL LIMIT 200`, s.now())
	if err != nil {
		return 0, database.MapError(err)
	}
	var list []*Rental
	for rows.Next() {
		r, err := s.scanRental(rows)
		if err != nil {
			rows.Close()
			return 0, err
		}
		list = append(list, r)
	}
	rows.Close()
	sent := 0
	for _, r := range list {
		// Reserva: duas instâncias não avisam a mesma locação.
		var account uuid.UUID
		err := s.db.QueryRow(ctx, `
			UPDATE rentals SET overdue_notified_at = NOW() WHERE id = $1 AND overdue_notified_at IS NULL RETURNING account_id`,
			r.ID).Scan(&account)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return sent, database.MapError(err)
		}
		people, err := s.db.Query(ctx, `
			SELECT u.email, u.name FROM users u WHERE u.id = $1 AND u.active
			UNION ALL
			SELECT u.email, u.name FROM account_members m JOIN users u ON u.id = m.user_id
			WHERE m.account_id = $1 AND m.role = 'manager' AND u.active`, account)
		if err != nil {
			return sent, database.MapError(err)
		}
		type person struct{ email, name string }
		var to []person
		for people.Next() {
			var p person
			if err := people.Scan(&p.email, &p.name); err != nil {
				people.Close()
				return sent, database.MapError(err)
			}
			to = append(to, p)
		}
		people.Close()
		n := mail.RentalNotice{VehicleID: r.VehicleID.String(), VehicleName: r.VehicleName, Plate: r.Plate,
			RenterName: r.RenterName, RenterPhone: r.RenterPhone, EndsAt: r.EndsAt}
		for _, p := range to {
			if err := s.notifier.RentalOverdue(ctx, p.email, p.name, n); err != nil {
				s.log.Warn("aviso de locação atrasada não saiu", "rental", r.ID, "err", err)
				continue
			}
			sent++
		}
	}
	return sent, nil
}

// Run avisa das locações atrasadas a cada 10 minutos.
func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for {
		if sent, err := s.NotifyOverdue(ctx); err != nil && ctx.Err() == nil {
			s.log.Error("falha ao avisar das locações atrasadas", "err", err)
		} else if sent > 0 {
			s.log.Info("avisos de locação atrasada", "emails", sent)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
