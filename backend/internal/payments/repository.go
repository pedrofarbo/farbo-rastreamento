package payments

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// Charge é um Pix ou um boleto gerado para uma fatura.
type Charge struct {
	ID        uuid.UUID `json:"id"`
	InvoiceID uuid.UUID `json:"invoiceId"`
	Provider  string    `json:"provider"`
	// Method: PIX ou BOLETO (que traz um Pix junto, em BrCode).
	Method           string `json:"method"`
	ProviderChargeID string `json:"providerChargeId"`
	AmountCents      int    `json:"amountCents"`
	Status           string `json:"status"`
	BrCode           string `json:"brCode"`
	QRCodeImage      string `json:"qrCodeImage"`
	// O boleto: a linha digitável, o código de barras e o link para ver e
	// imprimir.
	BoletoLine    string `json:"boletoLine"`
	BoletoBarcode string `json:"boletoBarcode"`
	BoletoURL     string `json:"boletoUrl"`
	// O cartão: a bandeira e os 4 últimos dígitos.
	CardBrand string     `json:"cardBrand"`
	CardLast4 string     `json:"cardLast4"`
	DevMode   bool       `json:"devMode"`
	ExpiresAt *time.Time `json:"expiresAt"`
	PaidAt    *time.Time `json:"paidAt"`
	CheckedAt *time.Time `json:"-"`
	CreatedAt time.Time  `json:"createdAt"`
	// PlatformFeeCents é a tarifa que a AbacatePay informou ao gerar o Pix
	// (o financeiro a lança como despesa quando o Pix é pago).
	PlatformFeeCents int `json:"-"`

	// Pedido de estorno feito pela central (ver RefundCharge).
	RefundRequestedAt *time.Time `json:"refundRequestedAt"`
	RefundID          string     `json:"refundId"`
	RefundReason      string     `json:"refundReason"`

	// InvoiceStatus acompanha a cobrança para a tela saber, na mesma
	// resposta, se a fatura já quitou (no Pix de várias, se todas quitaram).
	InvoiceStatus string `json:"invoiceStatus,omitempty"`
	// InvoiceIDs: as faturas que o Pix paga de uma vez (a do mês de uma
	// frota); vazio no Pix de uma fatura só (InvoiceID).
	InvoiceIDs []uuid.UUID `json:"invoiceIds,omitempty"`
}

// ChargeInvoice é uma das faturas de um Pix de várias, com o valor dela na
// hora em que o Pix foi gerado.
type ChargeInvoice struct {
	InvoiceID   uuid.UUID
	AmountCents int
}

const chargeColumns = `id, invoice_id, provider, method, provider_charge_id, amount_cents, status, br_code,
	qr_code_image, boleto_line, boleto_barcode, boleto_url, card_brand, card_last4, dev_mode, expires_at, paid_at,
	checked_at, created_at, refund_requested_at, refund_id, refund_reason`

type Repository struct{ db *database.DB }

func NewRepository(db *database.DB) *Repository { return &Repository{db: db} }

// scanCharge lê as colunas de chargeColumns e, depois delas, as extras.
func scanCharge(row database.Scanner, extra ...any) (*Charge, error) {
	var c Charge
	dest := append([]any{&c.ID, &c.InvoiceID, &c.Provider, &c.Method, &c.ProviderChargeID, &c.AmountCents,
		&c.Status, &c.BrCode, &c.QRCodeImage, &c.BoletoLine, &c.BoletoBarcode, &c.BoletoURL, &c.CardBrand, &c.CardLast4,
		&c.DevMode, &c.ExpiresAt, &c.PaidAt, &c.CheckedAt, &c.CreatedAt, &c.RefundRequestedAt, &c.RefundID, &c.RefundReason}, extra...)
	if err := row.Scan(dest...); err != nil {
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

const insertCharge = `
	INSERT INTO payment_charges (id, invoice_id, provider, method, provider_charge_id, amount_cents,
		status, br_code, qr_code_image, boleto_line, boleto_barcode, boleto_url, card_brand, card_last4, dev_mode,
		expires_at, platform_fee_cents, checked_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, NOW())
	RETURNING created_at`

func (c *Charge) insertArgs() []any {
	method := c.Method
	if method == "" {
		method = MethodPix
	}
	return []any{c.ID, c.InvoiceID, c.Provider, method, c.ProviderChargeID, c.AmountCents, c.Status, c.BrCode,
		c.QRCodeImage, c.BoletoLine, c.BoletoBarcode, c.BoletoURL, c.CardBrand, c.CardLast4, c.DevMode, c.ExpiresAt,
		max(c.PlatformFeeCents, 0)}
}

func (r *Repository) Insert(ctx context.Context, c *Charge) error {
	return database.MapError(r.db.QueryRow(ctx, insertCharge, c.insertArgs()...).Scan(&c.CreatedAt))
}

// InsertGroup grava a cobrança de várias faturas e as faturas dela.
func (r *Repository) InsertGroup(ctx context.Context, c *Charge, items []ChargeInvoice) error {
	return database.MapError(pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, insertCharge, c.insertArgs()...).Scan(&c.CreatedAt); err != nil {
			return err
		}
		// As faturas num comando só (numa frota, centenas).
		ids := make([]uuid.UUID, len(items))
		cents := make([]int32, len(items))
		for i, it := range items {
			ids[i], cents[i] = it.InvoiceID, int32(it.AmountCents)
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO payment_charge_invoices (charge_id, invoice_id, amount_cents)
			SELECT $1, i.id, i.cents FROM unnest($2::uuid[], $3::int[]) AS i(id, cents)`, c.ID, ids, cents)
		return err
	}))
}

// ChargeInvoices são as faturas de um Pix de várias (vazio: o Pix é de uma
// fatura só, a InvoiceID).
func (r *Repository) ChargeInvoices(ctx context.Context, chargeID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.db.Query(ctx, `
		SELECT pci.invoice_id FROM payment_charge_invoices pci JOIN invoices i ON i.id = pci.invoice_id
		WHERE pci.charge_id = $1 ORDER BY i.due_date, i.created_at, i.id`, chargeID)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ReusableGroup é a cobrança de várias (do método pedido), pendente e ainda
// valendo, que paga exatamente estas faturas pelo total de agora.
func (r *Repository) ReusableGroup(ctx context.Context, invoiceIDs []uuid.UUID, method string, totalCents int, validUntil time.Time) (*Charge, error) {
	return scanCharge(r.db.QueryRow(ctx, `
		SELECT `+chargeColumns+` FROM payment_charges c
		WHERE c.status = 'PENDING' AND c.method = $4 AND c.expires_at > $2 AND c.amount_cents = $3 AND c.invoice_id = ANY($1)
			AND (SELECT array_agg(pci.invoice_id ORDER BY pci.invoice_id) FROM payment_charge_invoices pci
				WHERE pci.charge_id = c.id) = (SELECT array_agg(x ORDER BY x) FROM unnest($1::uuid[]) AS x)
		ORDER BY c.created_at DESC LIMIT 1`, invoiceIDs, validUntil, totalCents, method))
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

// Reusable é a cobrança pendente mais recente da fatura (do método pedido)
// que ainda vale até `validUntil` e tem o valor dela, para não gerar uma
// nova a cada clique. Se a central mudou o valor da fatura, a antiga não
// serve mais. A cobrança de várias faturas não entra (ReusableGroup).
func (r *Repository) Reusable(ctx context.Context, invoiceID uuid.UUID, method string, amountCents int, validUntil time.Time) (*Charge, error) {
	return scanCharge(r.db.QueryRow(ctx, `
		SELECT `+chargeColumns+` FROM payment_charges c
		WHERE invoice_id = $1 AND method = $4 AND status = 'PENDING' AND expires_at > $2 AND amount_cents = $3
			AND NOT EXISTS (SELECT 1 FROM payment_charge_invoices pci WHERE pci.charge_id = c.id)
		ORDER BY created_at DESC LIMIT 1`, invoiceID, validUntil, amountCents, method))
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

// DuePending lista o que ainda pode mudar no provedor: Pix (e cartão à espera
// do banco) pendentes da última semana (depois disso já expiraram) e estornos
// pedidos que ainda não
// concluíram (em produção são assíncronos), não consultados há `idle`; e
// boletos pendentes ou vencidos há até boletoClearing (o pago no último dia
// ainda compensa), não consultados há `boletoIdle` — o boleto vale dias, e o
// webhook avisa antes.
func (r *Repository) DuePending(ctx context.Context, idle, boletoIdle time.Duration, limit int) ([]*Charge, error) {
	return r.listCharges(ctx, `
		SELECT `+chargeColumns+` FROM payment_charges
		WHERE (method IN ('PIX', 'CARD')
			AND ((status = 'PENDING' AND created_at > NOW() - INTERVAL '7 days')
			    OR (refund_requested_at IS NOT NULL AND status <> 'REFUNDED'
			        AND refund_requested_at > NOW() - INTERVAL '30 days'))
			AND (checked_at IS NULL OR checked_at < NOW() - make_interval(secs => $1)))
		   OR (method = 'BOLETO' AND status IN ('PENDING', 'EXPIRED')
			AND expires_at > NOW() - make_interval(secs => $4)
			AND (checked_at IS NULL OR checked_at < NOW() - make_interval(secs => $2)))
		ORDER BY checked_at NULLS FIRST
		LIMIT $3`, idle.Seconds(), boletoIdle.Seconds(), limit, boletoClearing.Seconds())
}

// MarkRefundRequested registra o pedido de estorno aceito pelo provedor.
func (r *Repository) MarkRefundRequested(ctx context.Context, id uuid.UUID, refundID, reason string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE payment_charges
		SET refund_requested_at = NOW(), refund_id = $2, refund_reason = $3, updated_at = NOW()
		WHERE id = $1`, id, refundID, reason)
	return database.MapError(err)
}

// Payment é um Pix ou boleto que chegou a ser pago, com a fatura a que
// pertence — a lista de pagamentos da ficha do cliente.
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
		SELECT `+prefixed("c", chargeColumns)+`, i.description, i.paid_charge_id IS NOT DISTINCT FROM c.id,
			(SELECT count(*) FROM payment_charge_invoices pci WHERE pci.charge_id = c.id)
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
		var grouped int
		c, err := scanCharge(rows, &p.InvoiceDescription, &p.SettledInvoice, &grouped)
		if err != nil {
			return nil, err
		}
		if grouped > 1 {
			p.InvoiceDescription = fmt.Sprintf("%d faturas pagas juntas (%s e mais)", grouped, p.InvoiceDescription)
		}
		// A lista não precisa da imagem do QR Code (alguns KB por linha).
		c.QRCodeImage, c.BrCode = "", ""
		p.Charge = c
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

// SetProviderCharge grava a cobrança de cartão que o provedor criou (até ali
// ela era CREATING, sem id dele).
func (r *Repository) SetProviderCharge(ctx context.Context, id uuid.UUID, providerID string, amountCents int, brand, last4 string,
	devMode bool, feeCents int, expiresAt *time.Time) error {
	_, err := r.db.Exec(ctx, `
		UPDATE payment_charges SET provider_charge_id = $2, amount_cents = $3, card_brand = $4, card_last4 = $5,
			dev_mode = $6, platform_fee_cents = $7, expires_at = $8, status = 'PENDING', checked_at = NOW(), updated_at = NOW()
		WHERE id = $1`, id, providerID, amountCents, brand, last4, devMode, max(feeCents, 0), expiresAt)
	return database.MapError(err)
}

// Creating são as cobranças de cartão que ficaram sem resposta do provedor
// (CREATING) há mais de `idle`: a conferência procura cada uma lá.
func (r *Repository) Creating(ctx context.Context, idle time.Duration, limit int) ([]*Charge, error) {
	return r.listCharges(ctx, `
		SELECT `+chargeColumns+` FROM payment_charges
		WHERE method = 'CARD' AND status = 'CREATING' AND created_at < NOW() - make_interval(secs => $1)
		ORDER BY created_at LIMIT $2`, idle.Seconds(), limit)
}
