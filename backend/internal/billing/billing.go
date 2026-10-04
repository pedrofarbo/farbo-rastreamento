// Package billing cuida das assinaturas e faturas dos clientes: cada
// assinatura ativa dá direito a um veículo e gera uma fatura por mês.
package billing

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/addresses"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

const (
	SubscriptionActive   = "ACTIVE"
	SubscriptionCanceled = "CANCELED"

	InvoiceOpen     = "OPEN"
	InvoicePaid     = "PAID"
	InvoiceCanceled = "CANCELED"
)

var (
	// ErrNotOpen: só fatura em aberto pode ser paga, cancelada ou editada.
	ErrNotOpen = errors.New("a fatura não está em aberto")
	// ErrAlreadyCanceled: a assinatura já foi cancelada.
	ErrAlreadyCanceled = errors.New("a assinatura já está cancelada")
)

// maxCatchUp limita quantas faturas uma assinatura gera numa rodada. Só
// passa de uma se o servidor ficou meses parado; o restante sai na próxima.
const maxCatchUp = 12

type Subscription struct {
	ID          uuid.UUID  `json:"id"`
	CustomerID  uuid.UUID  `json:"customerId"`
	PlanName    string     `json:"planName"`
	PriceCents  int        `json:"priceCents"`
	DueDay      int        `json:"dueDay"`
	NextDueDate Date       `json:"nextDueDate"`
	Status      string     `json:"status"`
	CanceledAt  *time.Time `json:"canceledAt"`
	// VehicleID é o veículo que a assinatura cobre. Nulo só em dados
	// anteriores ao fluxo único (a central informa o veículo depois).
	VehicleID *uuid.UUID `json:"vehicleId"`
	// DeliveryAddress é a cópia do endereço de entrega no momento da
	// contratação do rastreador; nula quando não houve envio.
	DeliveryAddress *addresses.Address `json:"deliveryAddress"`
	// PromoPriceCents vale para as faturas que vencem antes de PromoUntil
	// (promoção de pré-lançamento); depois, PriceCents. Os dois nulos: sem
	// promoção.
	PromoPriceCents *int      `json:"promoPriceCents"`
	PromoUntil      *Date     `json:"promoUntil"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

// PriceOn é o valor da fatura que vence em due.
func (s *Subscription) PriceOn(due Date) int {
	return priceOn(s.PriceCents, s.PromoPriceCents, s.PromoUntil, due)
}

func priceOn(regular int, promo *int, until *Date, due Date) int {
	if promo != nil && until != nil && due.Before(*until) {
		return *promo
	}
	return regular
}

type Invoice struct {
	ID             uuid.UUID  `json:"id"`
	CustomerID     uuid.UUID  `json:"customerId"`
	SubscriptionID *uuid.UUID `json:"subscriptionId"`
	Description    string     `json:"description"`
	AmountCents    int        `json:"amountCents"`
	DueDate        Date       `json:"dueDate"`
	Status         string     `json:"status"`
	// Overdue e DaysOverdue são calculados na leitura, com o "hoje" do fuso
	// de cobrança; não existem no banco.
	Overdue     bool       `json:"overdue"`
	DaysOverdue int        `json:"daysOverdue"`
	PaidAt      *time.Time `json:"paidAt"`
	// PaidVia: MANUAL (baixa da central) ou PIX (confirmado pelo provedor).
	PaidVia string `json:"paidVia"`
	// PaidChargeID é o Pix que quitou a fatura (só com PaidVia = PIX).
	PaidChargeID *uuid.UUID `json:"paidChargeId"`
	PaymentURL   string     `json:"paymentUrl"`
	PixCode      string     `json:"pixCode"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
}

// CustomerSummary é a linha da lista de clientes da central.
type CustomerSummary struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Phone     string    `json:"phone"`
	Document  string    `json:"document"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"createdAt"`

	ActiveSubscriptions int `json:"activeSubscriptions"`
	VehicleCount        int `json:"vehicleCount"`
	// SharedVehicles: veículos de outros clientes que este acompanha (acesso
	// de terceiro). Quem só tem isso não é cliente pagante.
	SharedVehicles  int  `json:"sharedVehicles"`
	OpenInvoices    int  `json:"openInvoices"`
	OverdueInvoices int  `json:"overdueInvoices"`
	OpenAmountCents int  `json:"openAmountCents"`
	Suspended       bool `json:"suspended"`
}

type Repository struct{ db *database.DB }

func NewRepository(db *database.DB) *Repository { return &Repository{db: db} }

// ---------------------------------------------------------------------------
// Assinaturas
// ---------------------------------------------------------------------------

const subscriptionColumns = `id, customer_id, plan_name, price_cents, due_day, next_due_date,
	status, canceled_at, vehicle_id, delivery_address, promo_price_cents, promo_until, created_at, updated_at`

func scanSubscription(row database.Scanner) (*Subscription, error) {
	var s Subscription
	var next time.Time
	var promoUntil *time.Time
	if err := row.Scan(&s.ID, &s.CustomerID, &s.PlanName, &s.PriceCents, &s.DueDay, &next,
		&s.Status, &s.CanceledAt, &s.VehicleID, &s.DeliveryAddress, &s.PromoPriceCents, &promoUntil,
		&s.CreatedAt, &s.UpdatedAt); err != nil {
		return nil, database.MapError(err)
	}
	s.NextDueDate = Date{next}
	if promoUntil != nil {
		s.PromoUntil = &Date{*promoUntil}
	}
	return &s, nil
}

// InsertSubscription grava a assinatura pelo Querier informado (pool ou
// transação).
func InsertSubscription(ctx context.Context, q database.Querier, s *Subscription) (*Subscription, error) {
	return scanSubscription(q.QueryRow(ctx, `
		INSERT INTO subscriptions (customer_id, plan_name, price_cents, due_day, next_due_date,
			vehicle_id, delivery_address, promo_price_cents, promo_until)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING `+subscriptionColumns,
		s.CustomerID, s.PlanName, s.PriceCents, s.DueDay, s.NextDueDate.Time, s.VehicleID, s.DeliveryAddress,
		s.PromoPriceCents, dateOrNil(s.PromoUntil)))
}

func dateOrNil(d *Date) *time.Time {
	if d == nil {
		return nil
	}
	return &d.Time
}

// LinkVehicle liga um veículo a uma assinatura ativa que ainda não tem
// nenhum (dados anteriores ao fluxo único). ErrNotFound se a assinatura não
// for do cliente, estiver encerrada ou já tiver veículo.
func LinkVehicle(ctx context.Context, q database.Querier, customerID, subscriptionID, vehicleID uuid.UUID) (*Subscription, error) {
	return scanSubscription(q.QueryRow(ctx, `
		UPDATE subscriptions SET vehicle_id = $3, updated_at = NOW()
		WHERE id = $1 AND customer_id = $2 AND status = 'ACTIVE' AND vehicle_id IS NULL
		RETURNING `+subscriptionColumns, subscriptionID, customerID, vehicleID))
}

// HasActiveForVehicle diz se o veículo tem assinatura ativa.
func (r *Repository) HasActiveForVehicle(ctx context.Context, vehicleID uuid.UUID) (bool, error) {
	var exists bool
	err := r.db.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM subscriptions WHERE vehicle_id = $1 AND status = 'ACTIVE')`,
		vehicleID).Scan(&exists)
	return exists, database.MapError(err)
}

func (r *Repository) GetSubscription(ctx context.Context, id uuid.UUID) (*Subscription, error) {
	return scanSubscription(r.db.QueryRow(ctx,
		`SELECT `+subscriptionColumns+` FROM subscriptions WHERE id = $1`, id))
}

// UpdateSubscription troca plano e valor; vale para as faturas geradas
// daqui em diante.
func (r *Repository) UpdateSubscription(ctx context.Context, id uuid.UUID, planName string, priceCents int) (*Subscription, error) {
	return scanSubscription(r.db.QueryRow(ctx, `
		UPDATE subscriptions SET plan_name = $2, price_cents = $3, updated_at = NOW()
		WHERE id = $1
		RETURNING `+subscriptionColumns, id, planName, priceCents))
}

// CancelSubscription encerra a assinatura e cancela as faturas dela que ainda
// não venceram. As já vencidas continuam em aberto: são dívida.
func (r *Repository) CancelSubscription(ctx context.Context, id uuid.UUID, today Date) (*Subscription, error) {
	var sub *Subscription
	err := pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		var err error
		sub, err = scanSubscription(tx.QueryRow(ctx, `
			UPDATE subscriptions SET status = 'CANCELED', canceled_at = NOW(), updated_at = NOW()
			WHERE id = $1 AND status = 'ACTIVE'
			RETURNING `+subscriptionColumns, id))
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			UPDATE invoices SET status = 'CANCELED', updated_at = NOW()
			WHERE subscription_id = $1 AND status = 'OPEN' AND due_date >= $2`, id, today.Time)
		return err
	})
	if errors.Is(err, database.ErrNotFound) {
		if _, getErr := r.GetSubscription(ctx, id); getErr == nil {
			return nil, ErrAlreadyCanceled
		}
	}
	if err != nil {
		return nil, database.MapError(err)
	}
	return sub, nil
}

func (r *Repository) ListSubscriptions(ctx context.Context, customerID uuid.UUID) ([]*Subscription, error) {
	rows, err := r.db.Query(ctx, `
		SELECT `+subscriptionColumns+` FROM subscriptions
		WHERE customer_id = $1
		ORDER BY status, created_at`, customerID)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()

	out := []*Subscription{}
	for rows.Next() {
		s, err := scanSubscription(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Faturas
// ---------------------------------------------------------------------------

const invoiceColumns = `id, customer_id, subscription_id, description, amount_cents, due_date,
	status, paid_at, paid_via, paid_charge_id, payment_url, pix_code, created_at, updated_at`

func scanInvoice(row database.Scanner) (*Invoice, error) {
	var inv Invoice
	var due time.Time
	if err := row.Scan(&inv.ID, &inv.CustomerID, &inv.SubscriptionID, &inv.Description,
		&inv.AmountCents, &due, &inv.Status, &inv.PaidAt, &inv.PaidVia, &inv.PaidChargeID, &inv.PaymentURL, &inv.PixCode,
		&inv.CreatedAt, &inv.UpdatedAt); err != nil {
		return nil, database.MapError(err)
	}
	inv.DueDate = Date{due}
	return &inv, nil
}

func (r *Repository) CreateInvoice(ctx context.Context, inv *Invoice) (*Invoice, error) {
	return InsertInvoice(ctx, r.db, inv)
}

// InsertInvoice grava a fatura pelo Querier informado (pool ou transação).
func InsertInvoice(ctx context.Context, q database.Querier, inv *Invoice) (*Invoice, error) {
	return scanInvoice(q.QueryRow(ctx, `
		INSERT INTO invoices (customer_id, subscription_id, description, amount_cents, due_date,
			payment_url, pix_code)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING `+invoiceColumns,
		inv.CustomerID, inv.SubscriptionID, inv.Description, inv.AmountCents, inv.DueDate.Time,
		inv.PaymentURL, inv.PixCode))
}

func (r *Repository) GetInvoice(ctx context.Context, id uuid.UUID) (*Invoice, error) {
	return scanInvoice(r.db.QueryRow(ctx, `SELECT `+invoiceColumns+` FROM invoices WHERE id = $1`, id))
}

// ListInvoices devolve as faturas do cliente, as mais recentes primeiro.
func (r *Repository) ListInvoices(ctx context.Context, customerID uuid.UUID) ([]*Invoice, error) {
	rows, err := r.db.Query(ctx, `
		SELECT `+invoiceColumns+` FROM invoices
		WHERE customer_id = $1
		ORDER BY due_date DESC, created_at DESC
		LIMIT 120`, customerID)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()

	out := []*Invoice{}
	for rows.Next() {
		inv, err := scanInvoice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

// transitionInvoice aplica uma mudança só a fatura em aberto; fatura paga ou
// cancelada devolve ErrNotOpen.
func (r *Repository) transitionInvoice(ctx context.Context, id uuid.UUID, set string, args ...any) (*Invoice, error) {
	inv, err := scanInvoice(r.db.QueryRow(ctx, `
		UPDATE invoices SET `+set+`, updated_at = NOW()
		WHERE id = $1 AND status = 'OPEN'
		RETURNING `+invoiceColumns, append([]any{id}, args...)...))
	if errors.Is(err, database.ErrNotFound) {
		if _, getErr := r.GetInvoice(ctx, id); getErr == nil {
			return nil, ErrNotOpen
		}
	}
	return inv, err
}

// MarkInvoicePaid quita a fatura em aberto; via diz como (MANUAL ou PIX) e
// chargeID, quando houver, qual Pix pagou.
func (r *Repository) MarkInvoicePaid(ctx context.Context, id uuid.UUID, via string, chargeID *uuid.UUID) (*Invoice, error) {
	return r.transitionInvoice(ctx, id,
		`status = 'PAID', paid_at = NOW(), paid_via = $2, paid_charge_id = $3`, via, chargeID)
}

// ReopenInvoice devolve a fatura paga para "em aberto" — quando o pagamento
// foi estornado. Fatura que não estava paga devolve ErrNotOpen.
func (r *Repository) ReopenInvoice(ctx context.Context, id uuid.UUID) (*Invoice, error) {
	inv, err := scanInvoice(r.db.QueryRow(ctx, `
		UPDATE invoices SET status = 'OPEN', paid_at = NULL, paid_via = '', paid_charge_id = NULL,
			updated_at = NOW()
		WHERE id = $1 AND status = 'PAID'
		RETURNING `+invoiceColumns, id))
	if errors.Is(err, database.ErrNotFound) {
		if _, getErr := r.GetInvoice(ctx, id); getErr == nil {
			return nil, ErrNotOpen
		}
	}
	return inv, err
}

func (r *Repository) CancelInvoice(ctx context.Context, id uuid.UUID) (*Invoice, error) {
	return r.transitionInvoice(ctx, id, `status = 'CANCELED'`)
}

func (r *Repository) UpdateInvoicePayment(ctx context.Context, id uuid.UUID, paymentURL, pixCode string) (*Invoice, error) {
	return r.transitionInvoice(ctx, id, `payment_url = $2, pix_code = $3`, paymentURL, pixCode)
}

// GenerateDue cria as faturas de todas as assinaturas ativas com vencimento
// até `horizon` e avança o próximo vencimento de cada uma. Várias instâncias
// podem rodar ao mesmo tempo: SKIP LOCKED separa as assinaturas entre elas, e
// a chave única (assinatura, vencimento) impede fatura em dobro.
func (r *Repository) GenerateDue(ctx context.Context, horizon Date) (int64, error) {
	type due struct {
		id, customerID uuid.UUID
		planName       string
		priceCents     int
		dueDay         int
		next           Date
		promoCents     *int
		promoUntil     *Date
	}

	var created int64
	err := pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id, customer_id, plan_name, price_cents, due_day, next_due_date, promo_price_cents, promo_until
			FROM subscriptions
			WHERE status = 'ACTIVE' AND next_due_date <= $1
			ORDER BY next_due_date
			FOR UPDATE SKIP LOCKED`, horizon.Time)
		if err != nil {
			return err
		}
		var pending []due
		for rows.Next() {
			var d due
			var next time.Time
			var promoUntil *time.Time
			if err := rows.Scan(&d.id, &d.customerID, &d.planName, &d.priceCents, &d.dueDay, &next,
				&d.promoCents, &promoUntil); err != nil {
				rows.Close()
				return err
			}
			d.next = Date{next}
			if promoUntil != nil {
				d.promoUntil = &Date{*promoUntil}
			}
			pending = append(pending, d)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}

		for _, d := range pending {
			next := d.next
			for i := 0; i < maxCatchUp && !horizon.Before(next); i++ {
				amount := priceOn(d.priceCents, d.promoCents, d.promoUntil, next)
				description := invoiceDescription(d.planName, next)
				if amount != d.priceCents {
					description += " · promoção de pré-lançamento"
				}
				tag, err := tx.Exec(ctx, `
					INSERT INTO invoices (customer_id, subscription_id, description, amount_cents, due_date)
					VALUES ($1, $2, $3, $4, $5)
					ON CONFLICT (subscription_id, due_date) DO NOTHING`,
					d.customerID, d.id, description, amount, next.Time)
				if err != nil {
					return err
				}
				created += tag.RowsAffected()
				next = nextMonthly(next, d.dueDay)
			}
			if _, err := tx.Exec(ctx,
				`UPDATE subscriptions SET next_due_date = $2, updated_at = NOW() WHERE id = $1`,
				d.id, next.Time); err != nil {
				return err
			}
		}
		return nil
	})
	return created, database.MapError(err)
}

// HasOverdue diz se o cliente tem fatura em aberto vencida antes de `cutoff`.
func (r *Repository) HasOverdue(ctx context.Context, customerID uuid.UUID, cutoff Date) (bool, error) {
	var overdue bool
	err := r.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM invoices
			WHERE customer_id = $1 AND status = 'OPEN' AND due_date < $2
		)`, customerID, cutoff.Time).Scan(&overdue)
	return overdue, database.MapError(err)
}

// ---------------------------------------------------------------------------
// Clientes
// ---------------------------------------------------------------------------

// summaryQuery monta o resumo de cada cliente. $1 = hoje (vencida é o que
// vence antes dele), $2 = data de corte da suspensão, $3 = suspensão ligada.
const summaryQuery = `
	SELECT u.id, u.name, u.email, u.phone, u.document, u.active, u.created_at,
		(SELECT count(*) FROM subscriptions s WHERE s.customer_id = u.id AND s.status = 'ACTIVE'),
		(SELECT count(*) FROM vehicles v WHERE v.owner_id = u.id),
		(SELECT count(*) FROM vehicle_shares sh JOIN vehicles v ON v.id = sh.vehicle_id AND v.owner_id = sh.owner_id
			WHERE sh.guest_id = u.id),
		(SELECT count(*) FROM invoices i WHERE i.customer_id = u.id AND i.status = 'OPEN'),
		(SELECT count(*) FROM invoices i
			WHERE i.customer_id = u.id AND i.status = 'OPEN' AND i.due_date < $1),
		(SELECT COALESCE(sum(i.amount_cents), 0) FROM invoices i
			WHERE i.customer_id = u.id AND i.status = 'OPEN'),
		$3::boolean AND EXISTS (SELECT 1 FROM invoices i
			WHERE i.customer_id = u.id AND i.status = 'OPEN' AND i.due_date < $2)
	FROM users u
	WHERE u.role = 'customer'`

func scanSummary(row database.Scanner) (*CustomerSummary, error) {
	var c CustomerSummary
	if err := row.Scan(&c.ID, &c.Name, &c.Email, &c.Phone, &c.Document, &c.Active, &c.CreatedAt,
		&c.ActiveSubscriptions, &c.VehicleCount, &c.SharedVehicles, &c.OpenInvoices, &c.OverdueInvoices,
		&c.OpenAmountCents, &c.Suspended); err != nil {
		return nil, database.MapError(err)
	}
	return &c, nil
}

func (r *Repository) ListCustomers(ctx context.Context, today, cutoff Date, suspension bool) ([]*CustomerSummary, error) {
	rows, err := r.db.Query(ctx, summaryQuery+` ORDER BY lower(u.name), u.email`,
		today.Time, cutoff.Time, suspension)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()

	out := []*CustomerSummary{}
	for rows.Next() {
		c, err := scanSummary(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *Repository) GetCustomer(ctx context.Context, id uuid.UUID, today, cutoff Date, suspension bool) (*CustomerSummary, error) {
	return scanSummary(r.db.QueryRow(ctx, summaryQuery+` AND u.id = $4`,
		today.Time, cutoff.Time, suspension, id))
}
