package appointments

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// A gestão da instalação pelo representante: o afiliado com a gestão ligada
// cuida da instalação e da desinstalação dos clientes que indicou, pela página
// dele. Aqui, as filas e os agendamentos desses clientes; o agendamento é o
// mesmo da central (a agenda mostra), e o link do instalador conclui.

// ErrNotReferred: o veículo ou o agendamento não é de um cliente do representante.
var ErrNotReferred = errors.New("este veículo não é de um cliente seu")

// referredBy: o cliente (u) foi indicado pelo afiliado ($1).
const referredBy = `u.id IN (SELECT customer_id FROM affiliate_referrals WHERE affiliate_id = $1)`

// PartnerVehicle é um veículo de cliente indicado que espera a instalação ou
// a retirada do rastreador.
type PartnerVehicle struct {
	VehicleID      uuid.UUID `json:"vehicleId"`
	VehicleName    string    `json:"vehicleName"`
	Plate          string    `json:"plate"`
	VehicleDetails string    `json:"vehicleDetails"`
	CustomerName   string    `json:"customerName"`
	CustomerPhone  string    `json:"customerPhone"`
	Address        string    `json:"address"`
	// IMEI: o rastreador a retirar (na desinstalação).
	IMEI string `json:"imei"`
	// Since: desde quando espera (o pedido ou o encerramento).
	Since time.Time `json:"since"`
}

// PartnerQueue é a página das instalações do representante: os veículos
// esperando a instalação e a retirada, e os agendamentos (em aberto e os dos
// últimos 30 dias), com o link do instalador.
type PartnerQueue struct {
	AwaitingInstall   []PartnerVehicle `json:"awaitingInstall"`
	AwaitingUninstall []PartnerVehicle `json:"awaitingUninstall"`
	Appointments      []*Appointment   `json:"appointments"`
}

const partnerVehicle = `
	SELECT v.id, v.name, COALESCE(v.plate, ''),
		concat_ws(' · ', NULLIF(v.brand, ''), NULLIF(v.model, ''), v.year::text, NULLIF(v.color, '')),
		u.name, u.phone,
		COALESCE((SELECT concat_ws(', ', ad.street || ' ' || ad.number, NULLIF(ad.complement, ''), ad.district,
			ad.city || '/' || ad.state) FROM customer_addresses ad WHERE ad.customer_id = u.id), ''),
		COALESCE(d.imei, ''), %s
	FROM vehicles v JOIN users u ON u.id = v.owner_id LEFT JOIN devices d ON d.id = v.device_id
	WHERE ` + referredBy + `
		AND NOT EXISTS (SELECT 1 FROM installation_appointments x WHERE x.vehicle_id = v.id AND x.status IN ('scheduled', 'arrived'))`

// PartnerQueue é o que o representante vê dos clientes que indicou.
func (s *Service) PartnerQueue(ctx context.Context, affiliateID uuid.UUID) (*PartnerQueue, error) {
	out := &PartnerQueue{}
	var err error
	// A instalação que é da Farbo (agora dele): o comodato e a contratada no
	// pedido. Quem instala por conta própria fica fora.
	if out.AwaitingInstall, err = s.partnerVehicles(ctx, `v.created_at`, `
		AND v.device_id IS NULL AND u.active AND u.role = 'customer'
		AND EXISTS (SELECT 1 FROM subscriptions sb WHERE sb.vehicle_id = v.id AND sb.status = 'ACTIVE' AND sb.farbo_install)
		AND COALESCE((SELECT x.kind FROM installation_appointments x WHERE x.vehicle_id = v.id AND x.status = 'done'
			ORDER BY x.completed_at DESC LIMIT 1), 'install') <> 'uninstall'`, affiliateID); err != nil {
		return nil, err
	}
	// A retirada: o rastreador do comodato que ficou sem assinatura ativa.
	if out.AwaitingUninstall, err = s.partnerVehicles(ctx, `COALESCE((SELECT max(sb.updated_at) FROM subscriptions sb
		WHERE sb.vehicle_id = v.id), v.created_at)`, `
		AND v.device_id IS NOT NULL
		AND EXISTS (SELECT 1 FROM subscriptions sb WHERE sb.vehicle_id = v.id AND sb.equipment_mode = 'loan')
		AND NOT EXISTS (SELECT 1 FROM subscriptions sb WHERE sb.vehicle_id = v.id AND sb.status = 'ACTIVE')`, affiliateID); err != nil {
		return nil, err
	}
	if out.Appointments, err = s.list(ctx, referredBy+` AND (a.status IN ('scheduled', 'arrived')
		OR a.scheduled_for > NOW() - INTERVAL '30 days')`, affiliateID); err != nil {
		return nil, err
	}
	for _, a := range out.Appointments {
		a.URL = s.PublicURL(a.ID)
	}
	return out, nil
}

func (s *Service) partnerVehicles(ctx context.Context, since, where string, affiliateID uuid.UUID) ([]PartnerVehicle, error) {
	rows, err := s.db.Query(ctx, fmt.Sprintf(partnerVehicle, since)+where+` ORDER BY 9 LIMIT 500`, affiliateID)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	out := []PartnerVehicle{}
	for rows.Next() {
		var v PartnerVehicle
		if err := rows.Scan(&v.VehicleID, &v.VehicleName, &v.Plate, &v.VehicleDetails, &v.CustomerName, &v.CustomerPhone,
			&v.Address, &v.IMEI, &v.Since); err != nil {
			return nil, database.MapError(err)
		}
		out = append(out, v)
	}
	return out, database.MapError(rows.Err())
}

// PartnerInput é o agendamento pelo representante: o veículo (na criação), o
// tipo, o dia e a hora e a observação. O endereço é o do cliente.
type PartnerInput struct {
	Kind         string    `json:"kind"`
	VehicleID    uuid.UUID `json:"vehicleId"`
	ScheduledFor time.Time `json:"scheduledFor"`
	Notes        string    `json:"notes"`
}

// referred confere que o veículo é de um cliente indicado pelo afiliado e
// devolve o endereço do cliente.
func (s *Service) referred(ctx context.Context, affiliateID, vehicleID uuid.UUID) (string, error) {
	var address string
	err := s.db.QueryRow(ctx, `
		SELECT COALESCE((SELECT concat_ws(', ', ad.street || ' ' || ad.number, NULLIF(ad.complement, ''), ad.district,
			ad.city || '/' || ad.state) FROM customer_addresses ad WHERE ad.customer_id = u.id), '')
		FROM vehicles v JOIN users u ON u.id = v.owner_id WHERE v.id = $2 AND `+referredBy, affiliateID, vehicleID).Scan(&address)
	if errors.Is(database.MapError(err), database.ErrNotFound) {
		return "", ErrNotReferred
	}
	return address, database.MapError(err)
}

// PartnerSchedule agenda a instalação ou a retirada pelo representante (ele é
// o instalador, com o nome dele) no endereço do cliente.
func (s *Service) PartnerSchedule(ctx context.Context, affiliateID uuid.UUID, installer string, in PartnerInput) (*Appointment, error) {
	address, err := s.referred(ctx, affiliateID, in.VehicleID)
	if err != nil {
		return nil, err
	}
	if address == "" {
		return nil, invalid("o cliente ainda não tem endereço: combine com ele e peça à Farbo para cadastrar")
	}
	return s.create(ctx, nil, Input{Kind: in.Kind, VehicleID: in.VehicleID, InstallerName: installer, ScheduledFor: in.ScheduledFor,
		Address: address, Notes: in.Notes})
}

// partnerAppointment é o agendamento de um cliente do representante.
func (s *Service) partnerAppointment(ctx context.Context, affiliateID, id uuid.UUID) (*Appointment, error) {
	a, err := s.Get(ctx, id)
	if errors.Is(err, database.ErrNotFound) {
		return nil, ErrNotReferred
	}
	if err != nil {
		return nil, err
	}
	if _, err := s.referred(ctx, affiliateID, a.VehicleID); err != nil {
		return nil, err
	}
	return a, nil
}

// PartnerReschedule remarca (o dia e a hora e a observação) pelo representante.
func (s *Service) PartnerReschedule(ctx context.Context, affiliateID, id uuid.UUID, in PartnerInput) (*Appointment, error) {
	a, err := s.partnerAppointment(ctx, affiliateID, id)
	if err != nil {
		return nil, err
	}
	return s.Reschedule(ctx, id, Input{Kind: a.Kind, VehicleID: a.VehicleID, InstallerID: a.InstallerID,
		InstallerName: a.InstallerName, ScheduledFor: in.ScheduledFor, DurationMin: a.DurationMin, Address: a.Address, Notes: in.Notes})
}

// PartnerCancel cancela pelo representante (o link do instalador para).
func (s *Service) PartnerCancel(ctx context.Context, affiliateID, id uuid.UUID) (*Appointment, error) {
	if _, err := s.partnerAppointment(ctx, affiliateID, id); err != nil {
		return nil, err
	}
	return s.Close(ctx, id, StatusCanceled)
}
