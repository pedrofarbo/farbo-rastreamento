// Package kits são os kits de instalação dos representantes: a central manda
// rastreadores (já com o chip) para o representante que entrega em mãos e
// instala, e acompanha cada remessa — preparando, enviada (a transportadora e
// o código de rastreio), entregue ou cancelada. O representante pede kits e
// acompanha as remessas pela página dele; o estoque dele são os rastreadores
// das remessas ainda sem veículo (instalar baixa sozinho).
package kits

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// As situações de uma remessa.
const (
	StatusRequested = "requested" // pedida pelo representante, esperando a central
	StatusPreparing = "preparing" // a central separou os rastreadores
	StatusShipped   = "shipped"   // enviada
	StatusDelivered = "delivered" // o representante recebeu
	StatusCanceled  = "canceled"
)

// maxRequest: quantos kits o representante pede de uma vez.
const maxRequest = 100

// ValidationError é um pedido recusado; a mensagem vai para a tela.
type ValidationError struct{ Message string }

func (e ValidationError) Error() string { return e.Message }

func invalid(format string, args ...any) error { return ValidationError{fmt.Sprintf(format, args...)} }

// ErrNotFound: a remessa não existe (ou não é do representante).
var ErrNotFound = errors.New("remessa não encontrada")

// Device é um rastreador de kit: o IMEI, o chip e, instalado, o veículo (e o
// cliente, para a central e para a página com senha).
type Device struct {
	ID           uuid.UUID `json:"id"`
	IMEI         string    `json:"imei"`
	ICCID        string    `json:"iccid"`
	Installed    bool      `json:"installed"`
	VehicleName  string    `json:"vehicleName,omitempty"`
	Plate        string    `json:"plate,omitempty"`
	CustomerName string    `json:"customerName,omitempty"`
}

// Shipment é uma remessa de kits para um representante.
type Shipment struct {
	ID            uuid.UUID `json:"id"`
	AffiliateID   uuid.UUID `json:"affiliateId"`
	AffiliateName string    `json:"affiliateName"`
	Status        string    `json:"status"`
	// O pedido do representante (nulo: a remessa nasceu na central).
	RequestedQuantity *int       `json:"requestedQuantity"`
	RequestNote       string     `json:"requestNote"`
	Carrier           string     `json:"carrier"`
	TrackingCode      string     `json:"trackingCode"`
	Notes             string     `json:"notes"`
	ShippedAt         *time.Time `json:"shippedAt"`
	DeliveredAt       *time.Time `json:"deliveredAt"`
	CanceledAt        *time.Time `json:"canceledAt"`
	CreatedAt         time.Time  `json:"createdAt"`
	Devices           []Device   `json:"devices"`
}

type Service struct {
	db *database.DB
}

func NewService(db *database.DB) *Service { return &Service{db: db} }

const selectShipment = `
	SELECT k.id, k.affiliate_id, a.name, k.status, k.requested_quantity, k.request_note, k.carrier, k.tracking_code,
		k.notes, k.shipped_at, k.delivered_at, k.canceled_at, k.created_at
	FROM kit_shipments k JOIN affiliates a ON a.id = k.affiliate_id`

// list são as remessas (com os rastreadores); withCustomers põe o nome do
// cliente nos instalados.
func (s *Service) list(ctx context.Context, withCustomers bool, where string, args ...any) ([]Shipment, error) {
	rows, err := s.db.Query(ctx, selectShipment+` WHERE `+where+` ORDER BY k.created_at DESC LIMIT 500`, args...)
	if err != nil {
		return nil, database.MapError(err)
	}
	out := []Shipment{}
	index := map[uuid.UUID]int{}
	for rows.Next() {
		var k Shipment
		if err := rows.Scan(&k.ID, &k.AffiliateID, &k.AffiliateName, &k.Status, &k.RequestedQuantity, &k.RequestNote, &k.Carrier,
			&k.TrackingCode, &k.Notes, &k.ShippedAt, &k.DeliveredAt, &k.CanceledAt, &k.CreatedAt); err != nil {
			rows.Close()
			return nil, database.MapError(err)
		}
		k.Devices = []Device{}
		index[k.ID] = len(out)
		out = append(out, k)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, database.MapError(err)
	}
	if len(out) == 0 {
		return out, nil
	}
	ids := make([]uuid.UUID, 0, len(out))
	for _, k := range out {
		ids = append(ids, k.ID)
	}
	drows, err := s.db.Query(ctx, `
		SELECT kd.shipment_id, d.id, d.imei, COALESCE(d.iccid, ''), v.id IS NOT NULL, COALESCE(v.name, ''), COALESCE(v.plate, ''),
			COALESCE(u.name, '')
		FROM kit_shipment_devices kd JOIN devices d ON d.id = kd.device_id
		LEFT JOIN vehicles v ON v.device_id = d.id LEFT JOIN users u ON u.id = v.owner_id
		WHERE kd.shipment_id = ANY($1) ORDER BY d.imei`, ids)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer drows.Close()
	for drows.Next() {
		var shipment uuid.UUID
		var d Device
		if err := drows.Scan(&shipment, &d.ID, &d.IMEI, &d.ICCID, &d.Installed, &d.VehicleName, &d.Plate, &d.CustomerName); err != nil {
			return nil, database.MapError(err)
		}
		if !withCustomers {
			d.CustomerName = ""
		}
		out[index[shipment]].Devices = append(out[index[shipment]].Devices, d)
	}
	return out, database.MapError(drows.Err())
}

func (s *Service) get(ctx context.Context, id uuid.UUID) (*Shipment, error) {
	list, err := s.list(ctx, true, `k.id = $1`, id)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, ErrNotFound
	}
	return &list[0], nil
}

// List são as remessas para a central (status vazio: todas).
func (s *Service) List(ctx context.Context, status string) ([]Shipment, error) {
	if status == "" {
		return s.list(ctx, true, `TRUE`)
	}
	return s.list(ctx, true, `k.status = $1`, status)
}

// Available são os rastreadores que podem ir num kit: sem veículo e fora de
// qualquer remessa.
func (s *Service) Available(ctx context.Context) ([]Device, error) {
	rows, err := s.db.Query(ctx, `
		SELECT d.id, d.imei, COALESCE(d.iccid, '') FROM devices d
		WHERE NOT EXISTS (SELECT 1 FROM vehicles v WHERE v.device_id = d.id)
			AND NOT EXISTS (SELECT 1 FROM kit_shipment_devices kd WHERE kd.device_id = d.id)
		ORDER BY d.imei LIMIT 2000`)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	out := []Device{}
	for rows.Next() {
		var d Device
		if err := rows.Scan(&d.ID, &d.IMEI, &d.ICCID); err != nil {
			return nil, database.MapError(err)
		}
		out = append(out, d)
	}
	return out, database.MapError(rows.Err())
}

// putDevices põe os rastreadores na remessa, conferindo que cada um está
// livre (sem veículo e fora de outra remessa).
func putDevices(ctx context.Context, tx pgx.Tx, shipment uuid.UUID, deviceIDs []uuid.UUID) error {
	if len(deviceIDs) == 0 {
		return invalid("escolha os rastreadores do kit")
	}
	seen := map[uuid.UUID]bool{}
	for _, id := range deviceIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		var imei string
		var busy bool
		err := tx.QueryRow(ctx, `
			SELECT d.imei, EXISTS (SELECT 1 FROM vehicles v WHERE v.device_id = d.id)
				OR EXISTS (SELECT 1 FROM kit_shipment_devices kd WHERE kd.device_id = d.id)
			FROM devices d WHERE d.id = $1 FOR UPDATE`, id).Scan(&imei, &busy)
		if errors.Is(err, pgx.ErrNoRows) {
			return invalid("rastreador não encontrado")
		}
		if err != nil {
			return database.MapError(err)
		}
		if busy {
			return invalid("o rastreador %s já está num veículo ou em outra remessa", imei)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO kit_shipment_devices (shipment_id, device_id) VALUES ($1, $2)`, shipment, id); err != nil {
			return database.MapError(err)
		}
	}
	return nil
}

func cleanText(text string, max int, what string) (string, error) {
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) > max {
		return "", invalid("%s longo demais (até %d caracteres)", what, max)
	}
	return text, nil
}

// Create monta uma remessa na central: o representante e os rastreadores.
func (s *Service) Create(ctx context.Context, by uuid.UUID, affiliateID uuid.UUID, deviceIDs []uuid.UUID, notes string) (*Shipment, error) {
	notes, err := cleanText(notes, 1000, "a observação")
	if err != nil {
		return nil, err
	}
	var id uuid.UUID
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var active bool
		if err := tx.QueryRow(ctx, `SELECT active FROM affiliates WHERE id = $1 AND customer_id IS NULL`, affiliateID).Scan(&active); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return invalid("escolha o representante")
			}
			return database.MapError(err)
		}
		if !active {
			return invalid("o representante está inativo")
		}
		if err := tx.QueryRow(ctx, `INSERT INTO kit_shipments (affiliate_id, status, notes, created_by)
			VALUES ($1, 'preparing', $2, $3) RETURNING id`, affiliateID, notes, by).Scan(&id); err != nil {
			return database.MapError(err)
		}
		return putDevices(ctx, tx, id, deviceIDs)
	})
	if err != nil {
		return nil, err
	}
	return s.get(ctx, id)
}

// transition muda a situação da remessa, se ela estiver numa das de origem.
func (s *Service) transition(ctx context.Context, id uuid.UUID, from []string, set string, args ...any) error {
	query := `UPDATE kit_shipments SET ` + set + `, updated_at = NOW() WHERE id = $1 AND status = ANY($2)`
	tag, err := s.db.Exec(ctx, query, append([]any{id, from}, args...)...)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		if _, err := s.get(ctx, id); err != nil {
			return err
		}
		return invalid("a remessa não está numa situação que permita isso")
	}
	return nil
}

// Prepare atende o pedido do representante: os rastreadores da remessa.
func (s *Service) Prepare(ctx context.Context, id uuid.UUID, deviceIDs []uuid.UUID, notes string) (*Shipment, error) {
	notes, err := cleanText(notes, 1000, "a observação")
	if err != nil {
		return nil, err
	}
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE kit_shipments SET status = 'preparing', notes = $2, updated_at = NOW()
			WHERE id = $1 AND status = 'requested'`, id, notes)
		if err != nil {
			return database.MapError(err)
		}
		if tag.RowsAffected() == 0 {
			return invalid("só dá para preparar um pedido que ainda espera a central")
		}
		return putDevices(ctx, tx, id, deviceIDs)
	})
	if err != nil {
		return nil, err
	}
	return s.get(ctx, id)
}

// Ship marca a remessa como enviada, com a transportadora e o código de
// rastreio (ou "em mãos", sem código).
func (s *Service) Ship(ctx context.Context, id uuid.UUID, carrier, trackingCode string) (*Shipment, error) {
	carrier, err := cleanText(carrier, 80, "a transportadora")
	if err != nil {
		return nil, err
	}
	trackingCode, err = cleanText(strings.ToUpper(trackingCode), 60, "o código de rastreio")
	if err != nil {
		return nil, err
	}
	if carrier == "" {
		return nil, invalid("informe a transportadora (ou \"em mãos\")")
	}
	if err := s.transition(ctx, id, []string{StatusPreparing}, `status = 'shipped', shipped_at = NOW(), carrier = $3, tracking_code = $4`,
		carrier, trackingCode); err != nil {
		return nil, err
	}
	return s.get(ctx, id)
}

// Deliver marca a remessa como entregue (a central, ou o representante pela página).
func (s *Service) Deliver(ctx context.Context, id uuid.UUID) (*Shipment, error) {
	if err := s.transition(ctx, id, []string{StatusShipped}, `status = 'delivered', delivered_at = NOW()`); err != nil {
		return nil, err
	}
	return s.get(ctx, id)
}

// Cancel cancela a remessa ainda não entregue: os rastreadores voltam a ficar
// livres.
func (s *Service) Cancel(ctx context.Context, id uuid.UUID) (*Shipment, error) {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE kit_shipments SET status = 'canceled', canceled_at = NOW(), updated_at = NOW()
			WHERE id = $1 AND status IN ('requested', 'preparing', 'shipped')`, id)
		if err != nil {
			return database.MapError(err)
		}
		if tag.RowsAffected() == 0 {
			return invalid("a remessa entregue (ou já cancelada) não pode ser cancelada")
		}
		_, err = tx.Exec(ctx, `DELETE FROM kit_shipment_devices WHERE shipment_id = $1`, id)
		return database.MapError(err)
	})
	if err != nil {
		return nil, err
	}
	return s.get(ctx, id)
}

// PartnerKits é o que o representante vê: o estoque (os rastreadores das
// remessas ainda sem veículo), quantos já instalou e as remessas.
type PartnerKits struct {
	Stock     []Device   `json:"stock"`
	Installed int        `json:"installed"`
	Shipments []Shipment `json:"shipments"`
}

// ForPartner são os kits do representante; withCustomers (a página com
// senha) põe o nome do cliente nos instalados.
func (s *Service) ForPartner(ctx context.Context, affiliateID uuid.UUID, withCustomers bool) (*PartnerKits, error) {
	shipments, err := s.list(ctx, withCustomers, `k.affiliate_id = $1`, affiliateID)
	if err != nil {
		return nil, err
	}
	out := &PartnerKits{Stock: []Device{}, Shipments: shipments}
	for _, k := range shipments {
		// O estoque é o que já chegou (ou está a caminho e ele já recebeu).
		if k.Status != StatusDelivered && k.Status != StatusShipped {
			continue
		}
		for _, d := range k.Devices {
			if d.Installed {
				out.Installed++
			} else if k.Status == StatusDelivered {
				out.Stock = append(out.Stock, d)
			}
		}
	}
	return out, nil
}

// Request é o pedido de kits do representante, pela página: um por vez (ele
// cancela o que está esperando para pedir outro).
func (s *Service) Request(ctx context.Context, affiliateID uuid.UUID, quantity int, note string) (*Shipment, error) {
	if quantity < 1 || quantity > maxRequest {
		return nil, invalid("peça de 1 a %d kits", maxRequest)
	}
	note, err := cleanText(note, 500, "a observação")
	if err != nil {
		return nil, err
	}
	var id uuid.UUID
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		// A trava no afiliado: dois pedidos ao mesmo tempo não passam juntos.
		if _, err := tx.Exec(ctx, `SELECT 1 FROM affiliates WHERE id = $1 FOR UPDATE`, affiliateID); err != nil {
			return database.MapError(err)
		}
		var open bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM kit_shipments WHERE affiliate_id = $1 AND status = 'requested')`,
			affiliateID).Scan(&open); err != nil {
			return database.MapError(err)
		}
		if open {
			return invalid("você já tem um pedido esperando a Farbo: cancele-o para pedir outro")
		}
		return database.MapError(tx.QueryRow(ctx, `INSERT INTO kit_shipments (affiliate_id, status, requested_quantity, request_note)
			VALUES ($1, 'requested', $2, $3) RETURNING id`, affiliateID, quantity, note).Scan(&id))
	})
	if err != nil {
		return nil, err
	}
	return s.get(ctx, id)
}

// owned confere que a remessa é do representante.
func (s *Service) owned(ctx context.Context, affiliateID, id uuid.UUID) error {
	var owner uuid.UUID
	err := s.db.QueryRow(ctx, `SELECT affiliate_id FROM kit_shipments WHERE id = $1`, id).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && owner != affiliateID) {
		return ErrNotFound
	}
	return database.MapError(err)
}

// forPartner tira o nome dos clientes da remessa (a página sem senha).
func forPartner(k *Shipment, withCustomers bool) *Shipment {
	if !withCustomers {
		for i := range k.Devices {
			k.Devices[i].CustomerName = ""
		}
	}
	return k
}

// Received: o representante confirma, pela página, que a remessa chegou.
func (s *Service) Received(ctx context.Context, affiliateID, id uuid.UUID, withCustomers bool) (*Shipment, error) {
	if err := s.owned(ctx, affiliateID, id); err != nil {
		return nil, err
	}
	k, err := s.Deliver(ctx, id)
	if err != nil {
		return nil, err
	}
	return forPartner(k, withCustomers), nil
}

// CancelRequest: o representante desiste do pedido que ainda espera a central.
func (s *Service) CancelRequest(ctx context.Context, affiliateID, id uuid.UUID) (*Shipment, error) {
	if err := s.owned(ctx, affiliateID, id); err != nil {
		return nil, err
	}
	if err := s.transition(ctx, id, []string{StatusRequested}, `status = 'canceled', canceled_at = NOW()`); err != nil {
		return nil, err
	}
	k, err := s.get(ctx, id)
	if err != nil {
		return nil, err
	}
	return forPartner(k, false), nil
}
