package payments

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// Charge é um Pix gerado para uma fatura.
type Charge struct {
	ID               uuid.UUID  `json:"id"`
	InvoiceID        uuid.UUID  `json:"invoiceId"`
	Provider         string     `json:"provider"`
	ProviderChargeID string     `json:"providerChargeId"`
	AmountCents      int        `json:"amountCents"`
	Status           string     `json:"status"`
	BrCode           string     `json:"brCode"`
	QRCodeImage      string     `json:"qrCodeImage"`
	DevMode          bool       `json:"devMode"`
	ExpiresAt        *time.Time `json:"expiresAt"`
	PaidAt           *time.Time `json:"paidAt"`
	CheckedAt        *time.Time `json:"-"`
	CreatedAt        time.Time  `json:"createdAt"`
	// PlatformFeeCents é a tarifa que a AbacatePay informou ao gerar o Pix
	// (o financeiro a lança como despesa quando o Pix é pago).
	PlatformFeeCents int `json:"-"`

	// Pedido de estorno feito pela central (ver RefundCharge).
	RefundRequestedAt *time.Time `json:"refundRequestedAt"`
	RefundID          string     `json:"refundId"`
	RefundReason      string     `json:"refundReason"`

	// InvoiceStatus acompanha a cobrança para a tela saber, na mesma
	// resposta, se a fatura já quitou.
	InvoiceStatus string `json:"invoiceStatus,omitempty"`
}

const chargeColumns = `id, invoice_id, provider, provider_charge_id, amount_cents, status, br_code,
	qr_code_image, dev_mode, expires_at, paid_at, checked_at, created_at,
	refund_requested_at, refund_id, refund_reason`

type Repository struct{ db *database.DB }

func NewRepository(db *database.DB) *Repository { return &Repository{db: db} }

func scanCharge(row database.Scanner) (*Charge, error) {
	var c Charge
	if err := row.Scan(&c.ID, &c.InvoiceID, &c.Provider, &c.ProviderChargeID, &c.AmountCents,
		&c.Status, &c.BrCode, &c.QRCodeImage, &c.DevMode, &c.ExpiresAt, &c.PaidAt, &c.CheckedAt,
		&c.CreatedAt, &c.RefundRequestedAt, &c.RefundID, &c.RefundReason); err != nil {
		return nil, database.MapError(err)
	}
	return &c, nil
}

func (r *Repository) listCharges(ctx context.Context, query string, args ...any) ([]*Charge, error) {
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	var out []*Charge
	for rows.Next() {
		c, err := scanCharge(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *Repository) Insert(ctx context.Context, c *Charge) error {
	return database.MapError(r.db.QueryRow(ctx, `
		INSERT INTO payment_charges (id, invoice_id, provider, provider_charge_id, amount_cents,
			status, br_code, qr_code_image, dev_mode, expires_at, platform_fee_cents, checked_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NOW())
		RETURNING created_at`,
		c.ID, c.InvoiceID, c.Provider, c.ProviderChargeID, c.AmountCents, c.Status, c.BrCode,
		c.QRCodeImage, c.DevMode, c.ExpiresAt, max(c.PlatformFeeCents, 0),
	).Scan(&c.CreatedAt))
}

func (r *Repository) Get(ctx context.Context, id uuid.UUID) (*Charge, error) {
	return scanCharge(r.db.QueryRow(ctx, `SELECT `+chargeColumns+` FROM payment_charges WHERE id = $1`, id))
}

func (r *Repository) ByProviderIDs(ctx context.Context, ids []string) ([]*Charge, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	return r.listCharges(ctx, `SELECT `+chargeColumns+` FROM payment_charges
		WHERE provider_charge_id = ANY($1)`, ids)
}

// Reusable devolve o Pix pendente mais recente da fatura que ainda vale até
// `validUntil`, para não gerar um novo a cada clique.
func (r *Repository) Reusable(ctx context.Context, invoiceID uuid.UUID, validUntil time.Time) (*Charge, error) {
	return scanCharge(r.db.QueryRow(ctx, `
		SELECT `+chargeColumns+` FROM payment_charges
		WHERE invoice_id = $1 AND status = 'PENDING' AND expires_at > $2
		ORDER BY created_at DESC LIMIT 1`, invoiceID, validUntil))
}

// SetStatus grava o status vindo do provedor; paid_at só é preenchido uma vez.
func (r *Repository) SetStatus(ctx context.Context, id uuid.UUID, status string, paid bool) error {
	_, err := r.db.Exec(ctx, `
		UPDATE payment_charges
		SET status = $2,
			paid_at = CASE WHEN $3 THEN COALESCE(paid_at, NOW()) ELSE paid_at END,
			checked_at = NOW(), updated_at = NOW()
		WHERE id = $1`, id, status, paid)
	return database.MapError(err)
}

func (r *Repository) TouchChecked(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.Exec(ctx, `UPDATE payment_charges SET checked_at = NOW() WHERE id = $1`, id)
	return database.MapError(err)
}

// DuePending lista o que ainda pode mudar no provedor e não é consultado há
// `idle`: Pix pendentes da última semana (depois disso já expiraram) e
// estornos pedidos que ainda não concluíram (em produção são assíncronos).
func (r *Repository) DuePending(ctx context.Context, idle time.Duration, limit int) ([]*Charge, error) {
	return r.listCharges(ctx, `
		SELECT `+chargeColumns+` FROM payment_charges
		WHERE ((status = 'PENDING' AND created_at > NOW() - INTERVAL '7 days')
		    OR (refund_requested_at IS NOT NULL AND status <> 'REFUNDED'
		        AND refund_requested_at > NOW() - INTERVAL '30 days'))
		  AND (checked_at IS NULL OR checked_at < NOW() - make_interval(secs => $1))
		ORDER BY checked_at NULLS FIRST
		LIMIT $2`, idle.Seconds(), limit)
}

// MarkRefundRequested registra o pedido de estorno aceito pelo provedor.
func (r *Repository) MarkRefundRequested(ctx context.Context, id uuid.UUID, refundID, reason string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE payment_charges
		SET refund_requested_at = NOW(), refund_id = $2, refund_reason = $3, updated_at = NOW()
		WHERE id = $1`, id, refundID, reason)
	return database.MapError(err)
}

// Payment é um Pix que chegou a ser pago, com a fatura a que pertence — a
// lista de pagamentos da ficha do cliente.
type Payment struct {
	*Charge
	InvoiceDescription string `json:"invoiceDescription"`
	// SettledInvoice diz se foi este Pix que quitou a fatura (estorná-lo
	// reabre a fatura) ou se é um pagamento a mais.
	SettledInvoice bool `json:"settledInvoice"`
}

// PaymentsForCustomer lista os Pix pagos, estornados ou em disputa de um
// cliente — o que já movimentou dinheiro. Pendentes e expirados ficam fora.
func (r *Repository) PaymentsForCustomer(ctx context.Context, customerID uuid.UUID) ([]*Payment, error) {
	rows, err := r.db.Query(ctx, `
		SELECT `+prefixed("c", chargeColumns)+`, i.description, i.paid_charge_id IS NOT DISTINCT FROM c.id
		FROM payment_charges c
		JOIN invoices i ON i.id = c.invoice_id
		WHERE i.customer_id = $1 AND (c.paid_at IS NOT NULL OR c.status IN ('PAID', 'REFUNDED', 'UNDER_DISPUTE'))
		ORDER BY COALESCE(c.paid_at, c.created_at) DESC
		LIMIT 100`, customerID)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()

	out := []*Payment{}
	for rows.Next() {
		var p Payment
		var c Charge
		if err := rows.Scan(&c.ID, &c.InvoiceID, &c.Provider, &c.ProviderChargeID, &c.AmountCents,
			&c.Status, &c.BrCode, &c.QRCodeImage, &c.DevMode, &c.ExpiresAt, &c.PaidAt, &c.CheckedAt,
			&c.CreatedAt, &c.RefundRequestedAt, &c.RefundID, &c.RefundReason,
			&p.InvoiceDescription, &p.SettledInvoice); err != nil {
			return nil, err
		}
		// A lista não precisa da imagem do QR Code (alguns KB por linha).
		c.QRCodeImage, c.BrCode = "", ""
		p.Charge = &c
		out = append(out, &p)
	}
	return out, rows.Err()
}

// prefixed qualifica cada coluna da lista com o apelido da tabela.
func prefixed(alias, columns string) string {
	parts := strings.Split(columns, ",")
	for i, part := range parts {
		parts[i] = alias + "." + strings.TrimSpace(part)
	}
	return strings.Join(parts, ", ")
}

// WebhookSeen diz se o evento já foi processado.
func (r *Repository) WebhookSeen(ctx context.Context, id string) (bool, error) {
	var seen bool
	err := r.db.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM payment_webhook_events WHERE id = $1)`, id).Scan(&seen)
	return seen, database.MapError(err)
}

// RecordWebhook marca o evento como processado.
func (r *Repository) RecordWebhook(ctx context.Context, id, provider, event string) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO payment_webhook_events (id, provider, event) VALUES ($1, $2, $3)
		ON CONFLICT (id) DO NOTHING`, id, provider, event)
	return database.MapError(err)
}
