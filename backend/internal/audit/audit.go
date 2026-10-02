// Package audit registra quem fez o quê — em especial toda ação que chega ao
// veículo (§27).
package audit

import (
	"context"
	"log/slog"
	"net"
	"net/netip"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// Ações auditadas.
const (
	ActionLogin       = "AUTH_LOGIN"
	ActionLoginFailed = "AUTH_LOGIN_FAILED"
	ActionLogout      = "AUTH_LOGOUT"

	ActionPasswordResetRequested = "AUTH_PASSWORD_RESET_REQUESTED"
	ActionPasswordReset          = "AUTH_PASSWORD_RESET"
	ActionPasswordResetFailed    = "AUTH_PASSWORD_RESET_FAILED"

	// Confirmação extra (biometria ou senha) de ações sensíveis.
	ActionStepUp           = "AUTH_STEP_UP"
	ActionStepUpFailed     = "AUTH_STEP_UP_FAILED"
	ActionBiometricAdded   = "AUTH_BIOMETRIC_ADDED"
	ActionBiometricRemoved = "AUTH_BIOMETRIC_REMOVED"

	ActionCommandRequested = "COMMAND_REQUESTED"
	ActionCommandSent      = "COMMAND_SENT"
	ActionCommandRejected  = "COMMAND_REJECTED"
	ActionCommandResult    = "COMMAND_RESULT"
	ActionDeviceCreated    = "DEVICE_CREATED"
	ActionDeviceUpdated    = "DEVICE_UPDATED"
	ActionDeviceDeleted    = "DEVICE_DELETED"
	ActionVehicleCreated   = "VEHICLE_CREATED"
	ActionVehicleUpdated   = "VEHICLE_UPDATED"
	ActionVehicleDeleted   = "VEHICLE_DELETED"
	ActionGeofenceChanged  = "GEOFENCE_CHANGED"

	ActionCustomerCreated      = "CUSTOMER_CREATED"
	ActionCustomerUpdated      = "CUSTOMER_UPDATED"
	ActionCustomerInvited      = "CUSTOMER_INVITED"
	ActionSubscriptionCreated  = "SUBSCRIPTION_CREATED"
	ActionSubscriptionUpdated  = "SUBSCRIPTION_UPDATED"
	ActionSubscriptionCanceled = "SUBSCRIPTION_CANCELED"
	ActionInvoiceCreated       = "INVOICE_CREATED"
	ActionInvoiceUpdated       = "INVOICE_UPDATED"
	ActionInvoicePaid          = "INVOICE_PAID"
	ActionInvoiceCanceled      = "INVOICE_CANCELED"
	ActionTrackerOrdered       = "TRACKER_ORDERED"
	ActionInstallerChanged     = "INSTALLER_CHANGED"
	ActionDeliveryAddressSaved = "DELIVERY_ADDRESS_SAVED"
	ActionFulfillmentChanged   = "FULFILLMENT_CHANGED"
	ActionShippingLabelBought  = "SHIPPING_LABEL_BOUGHT"
	ActionRetentionChanged     = "HISTORY_RETENTION_CHANGED"
	ActionAlertSettingsChanged = "ALERT_SETTINGS_CHANGED"
	ActionPaymentPixCreated    = "PAYMENT_PIX_CREATED"
	ActionPaymentRefundAsked   = "PAYMENT_REFUND_REQUESTED"
	ActionPaymentRefunded      = "PAYMENT_REFUNDED"
	ActionPaymentDisputed      = "PAYMENT_DISPUTED"
	// ActionPaymentUnmatched: Pix pago para fatura que não estava mais em
	// aberto — precisa de revisão (devolver ou aproveitar o valor).
	ActionPaymentUnmatched = "PAYMENT_UNMATCHED"
)

type Entry struct {
	ID     int64      `json:"id"`
	UserID *uuid.UUID `json:"userId"`
	Action string     `json:"action"`

	VehicleID *uuid.UUID `json:"vehicleId"`
	DeviceID  *uuid.UUID `json:"deviceId"`

	Result    string `json:"result"`
	IPAddress string `json:"ipAddress"`

	Metadata map[string]any `json:"metadata"`

	CreatedAt time.Time `json:"createdAt"`
}

const columns = `id, user_id, action, vehicle_id, device_id, COALESCE(result, ''),
	COALESCE(host(ip_address), ''), metadata, created_at`

type Repository struct{ db *database.DB }

func NewRepository(db *database.DB) *Repository { return &Repository{db: db} }

func (r *Repository) Create(ctx context.Context, e *Entry) error {
	if e.Metadata == nil {
		e.Metadata = map[string]any{}
	}
	var ip *string
	if addr := normalizeIP(e.IPAddress); addr != "" {
		ip = &addr
	}
	return database.MapError(r.db.QueryRow(ctx, `
		INSERT INTO audit_logs (user_id, action, vehicle_id, device_id, result, ip_address, metadata)
		VALUES ($1, $2, $3, $4, $5, $6::inet, $7)
		RETURNING id, created_at`,
		e.UserID, e.Action, e.VehicleID, e.DeviceID, e.Result, ip, e.Metadata,
	).Scan(&e.ID, &e.CreatedAt))
}

type Query struct {
	DeviceID  *uuid.UUID
	VehicleID *uuid.UUID
	Limit     int
}

func (r *Repository) List(ctx context.Context, q Query) ([]*Entry, error) {
	if q.Limit <= 0 || q.Limit > 500 {
		q.Limit = 100
	}
	rows, err := r.db.Query(ctx, `
		SELECT `+columns+` FROM audit_logs
		WHERE ($1::uuid IS NULL OR device_id = $1)
		  AND ($2::uuid IS NULL OR vehicle_id = $2)
		ORDER BY created_at DESC LIMIT $3`, q.DeviceID, q.VehicleID, q.Limit)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()

	out := []*Entry{}
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.UserID, &e.Action, &e.VehicleID, &e.DeviceID,
			&e.Result, &e.IPAddress, &e.Metadata, &e.CreatedAt); err != nil {
			return nil, err
		}
		if e.Metadata == nil {
			e.Metadata = map[string]any{}
		}
		out = append(out, &e)
	}
	return out, rows.Err()
}

type Service struct {
	repo *Repository
	log  *slog.Logger
}

func NewService(repo *Repository, log *slog.Logger) *Service {
	return &Service{repo: repo, log: log.With("component", "audit")}
}

// Record grava a trilha. Uma falha aqui é erro de log, não de operação: o
// comando já aconteceu e a informação não pode ser perdida em silêncio.
func (s *Service) Record(ctx context.Context, e *Entry) {
	if err := s.repo.Create(ctx, e); err != nil {
		s.log.Error("FALHA AO GRAVAR AUDITORIA", "action", e.Action,
			"device", e.DeviceID, "user", e.UserID, "err", err)
	}
}

func (s *Service) List(ctx context.Context, q Query) ([]*Entry, error) {
	return s.repo.List(ctx, q)
}

// normalizeIP extrai o endereço de um "host:porta" e valida o formato antes de
// mandar para uma coluna inet.
func normalizeIP(raw string) string {
	if raw == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(raw); err == nil {
		raw = host
	}
	addr, err := netip.ParseAddr(raw)
	if err != nil {
		return ""
	}
	return addr.String()
}
