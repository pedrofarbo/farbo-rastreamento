// Package fulfillment acompanha o pedido de um rastreador até a casa do
// cliente, em duas linhas do tempo: o chip M2M, que vai dentro do rastreador,
// e o próprio rastreador, que sai pelo Melhor Envios.
package fulfillment

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// Linhas do tempo.
const (
	TrackChip    = "CHIP"
	TrackTracker = "TRACKER"
)

// Chip M2M.
const (
	ChipRequested = "REQUESTED" // solicitado no fornecedor
	ChipShipped   = "SHIPPED"   // enviado pelo fornecedor
	ChipAtBase    = "AT_BASE"   // chegou em nossa base
	ChipSeparated = "SEPARATED" // separado para configuração
)

// Rastreador.
const (
	TrackerAwaitingSupplier = "AWAITING_SUPPLIER" // aguardando o fornecedor
	TrackerAtBase           = "AT_BASE"           // chegou em nossa base
	TrackerAwaitingChip     = "AWAITING_CHIP"     // aguardando o chip M2M
	TrackerConfiguring      = "CONFIGURING"       // em configuração
	TrackerConfigured       = "CONFIGURED"        // configurado
	TrackerShipped          = "SHIPPED"           // enviado (etiqueta do Melhor Envios)
	TrackerInTransit        = "IN_TRANSIT"        // em trânsito
	TrackerDelivered        = "DELIVERED"         // chegou
)

// ChipSteps e TrackerSteps são as linhas do tempo, na ordem.
var (
	ChipSteps    = []string{ChipRequested, ChipShipped, ChipAtBase, ChipSeparated}
	TrackerSteps = []string{
		TrackerAwaitingSupplier, TrackerAtBase, TrackerAwaitingChip, TrackerConfiguring,
		TrackerConfigured, TrackerShipped, TrackerInTransit, TrackerDelivered,
	}
)

// Steps devolve a linha do tempo de um track.
func Steps(track string) []string {
	if track == TrackChip {
		return ChipSteps
	}
	return TrackerSteps
}

// Position é a posição do status na linha do tempo; -1 se não existir.
func Position(track, status string) int {
	for i, s := range Steps(track) {
		if s == status {
			return i
		}
	}
	return -1
}

// ErrNotFound: acompanhamento inexistente (ou de outro cliente).
var ErrNotFound = database.ErrNotFound

// RuleError é uma mudança recusada pelas regras; a mensagem vai para a tela.
type RuleError struct{ Message string }

func (e RuleError) Error() string { return e.Message }

type Event struct {
	ID     int64  `json:"id"`
	Track  string `json:"track"`
	Status string `json:"status"`
	Note   string `json:"note"`
	// Automatic: mudou sozinho (rastreio do Melhor Envios), sem alguém da
	// central.
	Automatic bool      `json:"automatic"`
	CreatedAt time.Time `json:"createdAt"`
}

// Fulfillment é o acompanhamento de um veículo.
type Fulfillment struct {
	ID             uuid.UUID  `json:"id"`
	CustomerID     uuid.UUID  `json:"customerId"`
	CustomerName   string     `json:"customerName"`
	VehicleID      uuid.UUID  `json:"vehicleId"`
	VehicleName    string     `json:"vehicleName"`
	VehiclePlate   string     `json:"vehiclePlate"`
	SubscriptionID *uuid.UUID `json:"subscriptionId"`

	ChipStatus    string `json:"chipStatus"`
	TrackerStatus string `json:"trackerStatus"`

	ShippingOrderID    *string `json:"shippingOrderId"`
	ShippingProtocol   string  `json:"shippingProtocol"`
	ShippingService    string  `json:"shippingService"`
	ShippingPriceCents *int    `json:"shippingPriceCents"`
	// ShippingStatus é o status bruto do Melhor Envios (pending, released,
	// generated, posted, delivered, undelivered, paused...).
	ShippingStatus string `json:"shippingStatus"`
	TrackingCode   string `json:"trackingCode"`
	LabelURL       string `json:"labelUrl"`
	// O frete escolhido no pedido (e cobrado do cliente): a etiqueta sai,
	// por padrão, por esse serviço. Nulo nos pedidos sem frete.
	QuotedServiceID  *int   `json:"quotedServiceId"`
	QuotedService    string `json:"quotedService"`
	QuotedPriceCents *int   `json:"quotedPriceCents"`
	QuotedDays       *int   `json:"quotedDays"`
	// DeliveryArranged: o cliente combina a entrega com a central (sem frete
	// nem etiqueta); o rastreador é entregue em mãos.
	DeliveryArranged bool `json:"deliveryArranged"`

	Events    []Event   `json:"events"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`

	// Contato do cliente, para os e-mails; não sai em JSON.
	CustomerEmail string `json:"-"`
}

// CustomerView é o que o cliente vê: sem o custo do frete, o protocolo e a
// etiqueta, que são da operação.
type CustomerView struct {
	ID            uuid.UUID `json:"id"`
	VehicleID     uuid.UUID `json:"vehicleId"`
	VehicleName   string    `json:"vehicleName"`
	ChipStatus    string    `json:"chipStatus"`
	TrackerStatus string    `json:"trackerStatus"`
	Carrier       string    `json:"carrier"`
	TrackingCode  string    `json:"trackingCode"`
	// A entrega escolhida no pedido e o prazo dela (dias úteis).
	DeliveryService string `json:"deliveryService"`
	DeliveryDays    *int   `json:"deliveryDays"`
	// DeliveryArranged: a entrega é combinada com a central.
	DeliveryArranged bool      `json:"deliveryArranged"`
	Events           []Event   `json:"events"`
	CreatedAt        time.Time `json:"createdAt"`
}

func (f *Fulfillment) CustomerView() CustomerView {
	return CustomerView{
		ID: f.ID, VehicleID: f.VehicleID, VehicleName: f.VehicleName,
		ChipStatus: f.ChipStatus, TrackerStatus: f.TrackerStatus,
		Carrier: f.ShippingService, TrackingCode: f.TrackingCode, Events: f.Events, CreatedAt: f.CreatedAt,
		DeliveryService: f.QuotedService, DeliveryDays: f.QuotedDays, DeliveryArranged: f.DeliveryArranged,
	}
}

// ---------------------------------------------------------------------------
// Repositório
// ---------------------------------------------------------------------------

type Repository struct{ db *database.DB }

func NewRepository(db *database.DB) *Repository { return &Repository{db: db} }

// Create abre o acompanhamento de um veículo (dentro da transação do pedido),
// no começo das duas linhas do tempo.
func Create(ctx context.Context, q database.Querier, customerID, vehicleID uuid.UUID, subscriptionID *uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	if err := q.QueryRow(ctx, `
		INSERT INTO fulfillments (customer_id, vehicle_id, subscription_id)
		VALUES ($1, $2, $3) RETURNING id`, customerID, vehicleID, subscriptionID).Scan(&id); err != nil {
		return uuid.Nil, database.MapError(err)
	}
	_, err := q.Exec(ctx, `
		INSERT INTO fulfillment_events (fulfillment_id, track, status, note)
		VALUES ($1, 'CHIP', 'REQUESTED', 'Pedido recebido'), ($1, 'TRACKER', 'AWAITING_SUPPLIER', 'Pedido recebido')`, id)
	return id, database.MapError(err)
}

const selectFulfillment = `
	SELECT f.id, f.customer_id, u.name, u.email, f.vehicle_id, v.name, COALESCE(v.plate, ''), f.subscription_id,
		f.chip_status, f.tracker_status, f.shipping_order_id, f.shipping_protocol, f.shipping_service,
		f.shipping_price_cents, f.shipping_status, f.tracking_code, f.label_url, f.created_at, f.updated_at,
		f.quoted_service_id, f.quoted_service, f.quoted_price_cents, f.quoted_days,
		f.delivery_arranged
	FROM fulfillments f
	JOIN users u ON u.id = f.customer_id
	JOIN vehicles v ON v.id = f.vehicle_id`

func scan(row database.Scanner) (*Fulfillment, error) {
	var f Fulfillment
	if err := row.Scan(&f.ID, &f.CustomerID, &f.CustomerName, &f.CustomerEmail, &f.VehicleID, &f.VehicleName,
		&f.VehiclePlate, &f.SubscriptionID, &f.ChipStatus, &f.TrackerStatus, &f.ShippingOrderID,
		&f.ShippingProtocol, &f.ShippingService, &f.ShippingPriceCents, &f.ShippingStatus, &f.TrackingCode,
		&f.LabelURL, &f.CreatedAt, &f.UpdatedAt, &f.QuotedServiceID, &f.QuotedService, &f.QuotedPriceCents,
		&f.QuotedDays, &f.DeliveryArranged); err != nil {
		return nil, database.MapError(err)
	}
	f.Events = []Event{}
	return &f, nil
}

func (r *Repository) list(ctx context.Context, where string, args ...any) ([]*Fulfillment, error) {
	rows, err := r.db.Query(ctx, selectFulfillment+" "+where+" ORDER BY f.created_at DESC", args...)
	if err != nil {
		return nil, database.MapError(err)
	}
	out := []*Fulfillment{}
	for rows.Next() {
		f, err := scan(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, database.MapError(err)
	}
	return out, r.loadEvents(ctx, out)
}

// loadEvents traz o histórico de todos de uma vez.
func (r *Repository) loadEvents(ctx context.Context, list []*Fulfillment) error {
	if len(list) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(list))
	byID := make(map[uuid.UUID]*Fulfillment, len(list))
	for i, f := range list {
		ids[i] = f.ID
		byID[f.ID] = f
	}
	rows, err := r.db.Query(ctx, `
		SELECT fulfillment_id, id, track, status, note, automatic, created_at
		FROM fulfillment_events WHERE fulfillment_id = ANY($1) ORDER BY id`, ids)
	if err != nil {
		return database.MapError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var owner uuid.UUID
		var e Event
		if err := rows.Scan(&owner, &e.ID, &e.Track, &e.Status, &e.Note, &e.Automatic, &e.CreatedAt); err != nil {
			return database.MapError(err)
		}
		byID[owner].Events = append(byID[owner].Events, e)
	}
	return rows.Err()
}

func (r *Repository) Get(ctx context.Context, id uuid.UUID) (*Fulfillment, error) {
	list, err := r.list(ctx, "WHERE f.id = $1", id)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, ErrNotFound
	}
	return list[0], nil
}

// GetByShippingOrder acha o acompanhamento pela etiqueta do Melhor Envios.
func (r *Repository) GetByShippingOrder(ctx context.Context, orderID string) (*Fulfillment, error) {
	list, err := r.list(ctx, "WHERE f.shipping_order_id = $1", orderID)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, ErrNotFound
	}
	return list[0], nil
}

func (r *Repository) ListByCustomer(ctx context.Context, customerID uuid.UUID) ([]*Fulfillment, error) {
	return r.list(ctx, "WHERE f.customer_id = $1", customerID)
}

// List é a fila da central: em andamento (nada entregue ainda) ou todos.
func (r *Repository) List(ctx context.Context, onlyOpen bool) ([]*Fulfillment, error) {
	if onlyOpen {
		return r.list(ctx, "WHERE f.tracker_status <> 'DELIVERED'")
	}
	return r.list(ctx, "")
}

// InTransit são os que a sincronização com o Melhor Envios acompanha.
func (r *Repository) InTransit(ctx context.Context) ([]*Fulfillment, error) {
	return r.list(ctx, "WHERE f.tracker_status IN ('SHIPPED', 'IN_TRANSIT') AND f.shipping_order_id IS NOT NULL")
}

// PendingLabels são as etiquetas pagas que o Melhor Envios ainda estava
// gerando (rastreador ainda "Configurado").
func (r *Repository) PendingLabels(ctx context.Context) ([]*Fulfillment, error) {
	return r.list(ctx, "WHERE f.tracker_status = 'CONFIGURED' AND f.shipping_order_id IS NOT NULL")
}

// lock trava o acompanhamento para uma mudança.
func lock(ctx context.Context, tx pgx.Tx, id uuid.UUID) (chip, tracker string, vehicleID uuid.UUID, err error) {
	err = tx.QueryRow(ctx, `SELECT chip_status, tracker_status, vehicle_id FROM fulfillments WHERE id = $1 FOR UPDATE`, id).
		Scan(&chip, &tracker, &vehicleID)
	return chip, tracker, vehicleID, database.MapError(err)
}

// setStatus grava a mudança e o evento do histórico. Sem actor, a mudança é
// automática (rastreio do Melhor Envios).
func setStatus(ctx context.Context, tx pgx.Tx, id uuid.UUID, track, status, note string, actor *uuid.UUID) error {
	column := "tracker_status"
	if track == TrackChip {
		column = "chip_status"
	}
	if _, err := tx.Exec(ctx, `UPDATE fulfillments SET `+column+` = $2, updated_at = NOW() WHERE id = $1`, id, status); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO fulfillment_events (fulfillment_id, track, status, note, actor_id, automatic)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		id, track, status, note, actor, actor == nil)
	return err
}

// ShippingUpdate são os dados do envio a gravar; nil mantém o que está.
type ShippingUpdate struct {
	OrderID    *string
	ClearOrder bool
	Protocol   *string
	Service    *string
	PriceCents *int
	Status     *string
	Tracking   *string
	LabelURL   *string
}

func (r *Repository) updateShipping(ctx context.Context, q database.Querier, id uuid.UUID, u ShippingUpdate) error {
	_, err := q.Exec(ctx, `
		UPDATE fulfillments SET
			shipping_order_id    = CASE WHEN $2 THEN NULL ELSE COALESCE($3, shipping_order_id) END,
			shipping_protocol    = COALESCE($4, shipping_protocol),
			shipping_service     = COALESCE($5, shipping_service),
			shipping_price_cents = CASE WHEN $2 THEN NULL ELSE COALESCE($6, shipping_price_cents) END,
			shipping_status      = COALESCE($7, shipping_status),
			tracking_code        = COALESCE($8, tracking_code),
			label_url            = COALESCE($9, label_url),
			updated_at = NOW()
		WHERE id = $1`,
		id, u.ClearOrder, u.OrderID, u.Protocol, u.Service, u.PriceCents, u.Status, u.Tracking, u.LabelURL)
	return database.MapError(err)
}
