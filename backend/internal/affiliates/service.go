package affiliates

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

type Service struct {
	db  *database.DB
	loc *time.Location
	now func() time.Time
	log *slog.Logger
}

func NewService(db *database.DB, log *slog.Logger) *Service {
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		loc = time.FixedZone("BRT", -3*60*60)
	}
	return &Service{db: db, loc: loc, now: time.Now, log: log.With("component", "affiliates")}
}

// SetClock troca o relógio (testes).
func (s *Service) SetClock(now func() time.Time) { s.now = now }

func mapErr(err error) error {
	var v ValidationError
	if errors.As(err, &v) {
		return v
	}
	return database.MapError(err)
}

// ---------------------------------------------------------------------------
// Configuração
// ---------------------------------------------------------------------------

// Settings: o valor dos afiliados novos e a categoria das contas do fechamento.
type Settings struct {
	DefaultCommissionCents int        `json:"defaultCommissionCents"`
	CategoryID             *uuid.UUID `json:"categoryId"`
}

func (s *Service) Settings(ctx context.Context) (*Settings, error) {
	var out Settings
	err := s.db.QueryRow(ctx, `SELECT default_commission_cents, category_id FROM affiliate_settings`).
		Scan(&out.DefaultCommissionCents, &out.CategoryID)
	return &out, database.MapError(err)
}

// SaveSettings muda o valor padrão; com applyToAll, vale também para todos
// os afiliados (as comissões já geradas ficam como estão).
func (s *Service) SaveSettings(ctx context.Context, defaultCents int, applyToAll bool) (*Settings, error) {
	if defaultCents < 0 || defaultCents > MaxCommissionCents {
		return nil, invalid("Valor da comissão inválido.")
	}
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE affiliate_settings SET default_commission_cents = $1, updated_at = NOW()`, defaultCents); err != nil {
			return err
		}
		if applyToAll {
			_, err := tx.Exec(ctx, `UPDATE affiliates SET commission_cents = $1, updated_at = NOW()`, defaultCents)
			return err
		}
		return nil
	})
	if err != nil {
		return nil, database.MapError(err)
	}
	return s.Settings(ctx)
}

// ---------------------------------------------------------------------------
// Afiliados
// ---------------------------------------------------------------------------

const columns = `a.id, a.name, a.handle, a.code, a.report_token, a.commission_cents, a.email, a.phone, a.pix_key,
	a.notes, a.supplier_id, a.active, a.created_at,
	(SELECT count(DISTINCT lower(x.email)) FROM (
		SELECT email FROM launch_waitlist WHERE affiliate_id = a.id
		UNION ALL SELECT email FROM leads WHERE affiliate_id = a.id) x),
	(SELECT count(*) FROM affiliate_referrals r WHERE r.affiliate_id = a.id),
	(SELECT count(*) FROM affiliate_referrals r WHERE r.affiliate_id = a.id AND EXISTS (
		SELECT 1 FROM subscriptions sb WHERE sb.customer_id = r.customer_id AND sb.status = 'ACTIVE')),
	(SELECT count(*) FROM affiliate_referrals r JOIN subscriptions sb ON sb.customer_id = r.customer_id
		WHERE r.affiliate_id = a.id AND sb.status = 'ACTIVE'),
	COALESCE((SELECT sum(c.amount_cents) FROM affiliate_commissions c
		WHERE c.affiliate_id = a.id AND c.payout_id IS NULL), 0)::bigint,
	COALESCE((SELECT sum(p.amount_cents) FROM affiliate_payouts p LEFT JOIN finance_entries e ON e.id = p.finance_entry_id
		WHERE p.affiliate_id = a.id AND e.status IS DISTINCT FROM 'PAID'), 0)::bigint,
	COALESCE((SELECT sum(p.amount_cents) FROM affiliate_payouts p JOIN finance_entries e ON e.id = p.finance_entry_id
		WHERE p.affiliate_id = a.id AND e.status = 'PAID'), 0)::bigint`

func scan(row database.Scanner) (*Affiliate, error) {
	var a Affiliate
	err := row.Scan(&a.ID, &a.Name, &a.Handle, &a.Code, &a.ReportToken, &a.CommissionCents, &a.Email, &a.Phone,
		&a.PixKey, &a.Notes, &a.SupplierID, &a.Active, &a.CreatedAt,
		&a.Signups, &a.Customers, &a.ActiveCustomers, &a.ActiveVehicles, &a.PendingCents, &a.OpenCents, &a.PaidCents)
	if err != nil {
		return nil, database.MapError(err)
	}
	return &a, nil
}

// List traz os afiliados com os números (acertando as comissões antes).
func (s *Service) List(ctx context.Context) ([]*Affiliate, error) {
	if _, _, err := s.Reconcile(ctx); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `SELECT `+columns+` FROM affiliates a ORDER BY a.active DESC, a.created_at DESC`)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	out := []*Affiliate{}
	for rows.Next() {
		a, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (*Affiliate, error) {
	return scan(s.db.QueryRow(ctx, `SELECT `+columns+` FROM affiliates a WHERE a.id = $1`, id))
}

func conflictMessage(err error) error {
	if errors.Is(database.MapError(err), database.ErrConflict) {
		return invalid("Esse código de link já é de outro afiliado: escolha outro.")
	}
	return mapErr(err)
}

// Create cadastra o afiliado, com o link secreto da página dele.
func (s *Service) Create(ctx context.Context, in Input) (*Affiliate, error) {
	in, err := in.Normalize()
	if err != nil {
		return nil, err
	}
	if in.CommissionCents == nil {
		settings, err := s.Settings(ctx)
		if err != nil {
			return nil, err
		}
		in.CommissionCents = &settings.DefaultCommissionCents
	}
	token, err := newToken()
	if err != nil {
		return nil, err
	}
	var id uuid.UUID
	err = s.db.QueryRow(ctx, `
		INSERT INTO affiliates (name, handle, code, report_token, commission_cents, email, phone, pix_key, notes, active)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id`,
		in.Name, in.Handle, in.Code, token, *in.CommissionCents, in.Email, in.Phone, in.PixKey, in.Notes, in.Active).Scan(&id)
	if err != nil {
		return nil, conflictMessage(err)
	}
	return s.Get(ctx, id)
}

// Update muda o cadastro. O valor novo vale para as comissões que ainda vão
// nascer; as geradas ficam com o valor de quando nasceram.
func (s *Service) Update(ctx context.Context, id uuid.UUID, in Input) (*Affiliate, error) {
	in, err := in.Normalize()
	if err != nil {
		return nil, err
	}
	tag, err := s.db.Exec(ctx, `
		UPDATE affiliates SET name = $2, handle = $3, code = $4, commission_cents = COALESCE($5, commission_cents),
			email = $6, phone = $7, pix_key = $8, notes = $9, active = $10, updated_at = NOW()
		WHERE id = $1`,
		id, in.Name, in.Handle, in.Code, in.CommissionCents, in.Email, in.Phone, in.PixKey, in.Notes, in.Active)
	if err != nil {
		return nil, conflictMessage(err)
	}
	if tag.RowsAffected() == 0 {
		return nil, database.ErrNotFound
	}
	return s.Get(ctx, id)
}

// RegenerateToken troca o link secreto da página do afiliado (o antigo para
// de abrir).
func (s *Service) RegenerateToken(ctx context.Context, id uuid.UUID) (*Affiliate, error) {
	token, err := newToken()
	if err != nil {
		return nil, err
	}
	tag, err := s.db.Exec(ctx, `UPDATE affiliates SET report_token = $2, updated_at = NOW() WHERE id = $1`, id, token)
	if err != nil {
		return nil, database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return nil, database.ErrNotFound
	}
	return s.Get(ctx, id)
}

// Public é o que a tela de cadastro mostra ("Indicado por @fulano").
type Public struct {
	Name   string `json:"name"`
	Handle string `json:"handle"`
	Code   string `json:"code"`
}

// Lookup acha o afiliado ativo do código do link (nil se não há).
func (s *Service) Lookup(ctx context.Context, code string) (*Public, *uuid.UUID, error) {
	code = Code(code)
	if code == "" {
		return nil, nil, nil
	}
	var p Public
	var id uuid.UUID
	err := s.db.QueryRow(ctx, `SELECT id, name, handle, code FROM affiliates WHERE code = $1 AND active`, code).
		Scan(&id, &p.Name, &p.Handle, &p.Code)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, database.MapError(err)
	}
	return &p, &id, nil
}

// ---------------------------------------------------------------------------
// Clientes indicados
// ---------------------------------------------------------------------------

// Referral é quem indicou o cliente.
type Referral struct {
	AffiliateID uuid.UUID `json:"affiliateId"`
	Name        string    `json:"name"`
	Handle      string    `json:"handle"`
	// Source: lead (pré-cadastro), waitlist (lista de lançamento) ou admin.
	Source string    `json:"source"`
	Since  time.Time `json:"since"`
}

// CustomerReferral: o afiliado do cliente (nil se não foi indicado).
func (s *Service) CustomerReferral(ctx context.Context, customerID uuid.UUID) (*Referral, error) {
	var r Referral
	err := s.db.QueryRow(ctx, `
		SELECT a.id, a.name, a.handle, r.source, r.created_at
		FROM affiliate_referrals r JOIN affiliates a ON a.id = r.affiliate_id
		WHERE r.customer_id = $1`, customerID).Scan(&r.AffiliateID, &r.Name, &r.Handle, &r.Source, &r.Since)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, database.MapError(err)
	}
	return &r, nil
}

// AttachCustomer liga o cliente recém-cadastrado ao afiliado que o trouxe:
// o do pré-cadastro de onde ele veio, ou o do e-mail dele na lista de
// lançamento (ou num pré-cadastro). Sem afiliado ativo, nada muda.
func (s *Service) AttachCustomer(ctx context.Context, customerID uuid.UUID, email string, leadID *uuid.UUID) (bool, error) {
	tag, err := s.db.Exec(ctx, `
		INSERT INTO affiliate_referrals (customer_id, affiliate_id, source)
		SELECT $1, x.affiliate_id, x.source FROM (
			SELECT affiliate_id, 'lead' AS source, 1 AS rank FROM leads WHERE id = $2::uuid AND affiliate_id IS NOT NULL
			UNION ALL
			SELECT affiliate_id, 'waitlist', 2 FROM launch_waitlist WHERE lower(email) = lower($3) AND affiliate_id IS NOT NULL
			UNION ALL
			SELECT affiliate_id, 'lead', 3 FROM leads WHERE lower(email) = lower($3) AND affiliate_id IS NOT NULL
		) x JOIN affiliates a ON a.id = x.affiliate_id AND a.active
		ORDER BY x.rank LIMIT 1
		ON CONFLICT (customer_id) DO NOTHING`, customerID, leadID, email)
	if err != nil {
		return false, database.MapError(err)
	}
	return tag.RowsAffected() > 0, nil
}

// SetCustomer é o admin dizendo quem indicou o cliente (nil tira a
// indicação). A indicação nova vale a partir de agora; as comissões ainda não
// fechadas do afiliado anterior saem.
func (s *Service) SetCustomer(ctx context.Context, customerID uuid.UUID, affiliateID *uuid.UUID) (*Referral, error) {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var role string
		if err := tx.QueryRow(ctx, `SELECT role FROM users WHERE id = $1`, customerID).Scan(&role); err != nil {
			return err
		}
		if role != "customer" {
			return invalid("Só clientes podem ter afiliado.")
		}
		if _, err := tx.Exec(ctx, `
			DELETE FROM affiliate_commissions WHERE customer_id = $1 AND payout_id IS NULL
				AND affiliate_id IS DISTINCT FROM $2::uuid`, customerID, affiliateID); err != nil {
			return err
		}
		if affiliateID == nil {
			_, err := tx.Exec(ctx, `DELETE FROM affiliate_referrals WHERE customer_id = $1`, customerID)
			return err
		}
		var ok bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM affiliates WHERE id = $1)`, *affiliateID).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return invalid("Afiliado não encontrado.")
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO affiliate_referrals (customer_id, affiliate_id, source) VALUES ($1, $2, 'admin')
			ON CONFLICT (customer_id) DO UPDATE SET
				created_at = CASE WHEN affiliate_referrals.affiliate_id = EXCLUDED.affiliate_id
					THEN affiliate_referrals.created_at ELSE NOW() END,
				source = CASE WHEN affiliate_referrals.affiliate_id = EXCLUDED.affiliate_id
					THEN affiliate_referrals.source ELSE 'admin' END,
				affiliate_id = EXCLUDED.affiliate_id`, customerID, *affiliateID)
		return err
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return s.CustomerReferral(ctx, customerID)
}

// ---------------------------------------------------------------------------
// Comissões
// ---------------------------------------------------------------------------

// Reconcile acerta as comissões com as mensalidades: cada mês pago de cada
// veículo (assinatura) de um cliente indicado, a partir do mês da indicação,
// rende uma comissão ao afiliado ativo, pelo valor dele — o cliente com dois
// veículos rende duas. A de uma mensalidade estornada — ou de um cliente que
// mudou de afiliado — sai, se ainda não foi fechada. Idempotente: roda de
// hora em hora e antes dos relatórios.
func (s *Service) Reconcile(ctx context.Context) (created, removed int64, err error) {
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			INSERT INTO affiliate_commissions (affiliate_id, customer_id, subscription_id, month, amount_cents)
			SELECT r.affiliate_id, i.customer_id, i.subscription_id, date_trunc('month', i.due_date)::date, a.commission_cents
			FROM invoices i
			JOIN affiliate_referrals r ON r.customer_id = i.customer_id
			JOIN affiliates a ON a.id = r.affiliate_id
			WHERE i.status = 'PAID' AND i.subscription_id IS NOT NULL AND i.amount_cents > 0
				AND a.active AND a.commission_cents > 0
				AND i.due_date >= date_trunc('month', r.created_at AT TIME ZONE $1)::date
			GROUP BY 1, 2, 3, 4, 5
			ON CONFLICT (affiliate_id, subscription_id, month) DO NOTHING`, s.loc.String())
		if err != nil {
			return err
		}
		created = tag.RowsAffected()
		tag, err = tx.Exec(ctx, `
			DELETE FROM affiliate_commissions c
			WHERE c.payout_id IS NULL AND (
				NOT EXISTS (SELECT 1 FROM invoices i
					WHERE i.subscription_id = c.subscription_id AND i.customer_id = c.customer_id AND i.status = 'PAID'
						AND date_trunc('month', i.due_date)::date = c.month)
				OR NOT EXISTS (SELECT 1 FROM affiliate_referrals r
					WHERE r.customer_id = c.customer_id AND r.affiliate_id = c.affiliate_id))`)
		if err != nil {
			return err
		}
		removed = tag.RowsAffected()
		return nil
	})
	return created, removed, database.MapError(err)
}

// Work é o que o worker chama de hora em hora.
func (s *Service) Work(ctx context.Context) {
	created, removed, err := s.Reconcile(ctx)
	if err != nil {
		s.log.Error("falha ao acertar as comissões dos afiliados", "err", err)
		return
	}
	if created > 0 || removed > 0 {
		s.log.Info("comissões dos afiliados acertadas", "novas", created, "removidas", removed)
	}
}

// ---------------------------------------------------------------------------
// Fechamento
// ---------------------------------------------------------------------------

// ClosingLine é o que um afiliado tem a receber no fechamento: as comissões
// ainda abertas até o mês fechado (as atrasadas de meses anteriores entram).
type ClosingLine struct {
	AffiliateID uuid.UUID `json:"affiliateId"`
	Name        string    `json:"name"`
	Handle      string    `json:"handle"`
	PixKey      string    `json:"pixKey"`
	Commissions int       `json:"commissions"`
	AmountCents int64     `json:"amountCents"`
}

// monthStart: "2026-10" → 2026-10-01.
func monthStart(month string) (time.Time, error) {
	t, err := time.Parse("2006-01", month)
	if err != nil {
		return t, invalid("Mês inválido: use AAAA-MM.")
	}
	return t, nil
}

// ClosingPreview mostra o fechamento antes de fazer.
func (s *Service) ClosingPreview(ctx context.Context, month string) ([]ClosingLine, error) {
	m, err := monthStart(month)
	if err != nil {
		return nil, err
	}
	if _, _, err := s.Reconcile(ctx); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `
		SELECT a.id, a.name, a.handle, a.pix_key, count(*), sum(c.amount_cents)::bigint
		FROM affiliate_commissions c JOIN affiliates a ON a.id = c.affiliate_id
		WHERE c.payout_id IS NULL AND c.month <= $1
		GROUP BY a.id ORDER BY 6 DESC, a.name`, m)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	out := []ClosingLine{}
	for rows.Next() {
		var l ClosingLine
		if err := rows.Scan(&l.AffiliateID, &l.Name, &l.Handle, &l.PixKey, &l.Commissions, &l.AmountCents); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// Close fecha o mês: para cada afiliado com comissões abertas até ele, uma
// conta a pagar na Empresa (com o afiliado de fornecedor) e o registro do
// fechamento. Pagar a conta paga as comissões.
func (s *Service) Close(ctx context.Context, month string, due billing.Date, by *uuid.UUID) ([]*Payout, error) {
	m, err := monthStart(month)
	if err != nil {
		return nil, err
	}
	if due.IsZero() {
		return nil, invalid("Informe o vencimento das contas.")
	}
	if _, _, err := s.Reconcile(ctx); err != nil {
		return nil, err
	}
	var ids []uuid.UUID
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var category *uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT category_id FROM affiliate_settings`).Scan(&category); err != nil {
			return err
		}
		if category == nil {
			return invalid("A categoria das comissões não existe mais: crie uma em Empresa → Cadastros.")
		}
		// Trava as comissões (não o afiliado): a soma é exatamente o que o
		// fechamento marca, e dois fechamentos ao mesmo tempo não pegam a
		// mesma comissão.
		type line struct {
			id, supplier            *uuid.UUID
			name, email, phone, pix string
			commissions             []uuid.UUID
			cents                   int64
		}
		rows, err := tx.Query(ctx, `
			SELECT c.id, c.amount_cents, a.id, a.supplier_id, a.name, a.email, a.phone, a.pix_key
			FROM affiliate_commissions c JOIN affiliates a ON a.id = c.affiliate_id
			WHERE c.payout_id IS NULL AND c.month <= $1
			ORDER BY a.name, a.id
			FOR UPDATE OF c`, m)
		if err != nil {
			return err
		}
		var lines []*line
		byAffiliate := map[uuid.UUID]*line{}
		for rows.Next() {
			var commission, affiliate uuid.UUID
			var cents int64
			var l line
			if err := rows.Scan(&commission, &cents, &affiliate, &l.supplier, &l.name, &l.email, &l.phone, &l.pix); err != nil {
				rows.Close()
				return err
			}
			cur := byAffiliate[affiliate]
			if cur == nil {
				l.id = &affiliate
				cur = &l
				byAffiliate[affiliate] = cur
				lines = append(lines, cur)
			}
			cur.commissions = append(cur.commissions, commission)
			cur.cents += cents
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if len(lines) == 0 {
			return invalid("Nenhuma comissão aberta até %s.", monthLabel(m))
		}
		for _, l := range lines {
			if l.supplier == nil {
				var supplier uuid.UUID
				if err := tx.QueryRow(ctx, `
					INSERT INTO suppliers (name, email, phone, pix_key, notes)
					VALUES ($1, $2, $3, $4, 'Afiliado (criado no fechamento das comissões)') RETURNING id`,
					l.name, l.email, l.phone, l.pix).Scan(&supplier); err != nil {
					return err
				}
				if _, err := tx.Exec(ctx, `UPDATE affiliates SET supplier_id = $2 WHERE id = $1`, *l.id, supplier); err != nil {
					return err
				}
				l.supplier = &supplier
			}
			var entry uuid.UUID
			description := fmt.Sprintf("Comissão de afiliado: %s (até %s)", l.name, monthLabel(m))
			notes := fmt.Sprintf("%d comissão(ões) por veículo de clientes indicados.", len(l.commissions))
			if l.pix != "" {
				notes += " Pix: " + l.pix
			}
			if err := tx.QueryRow(ctx, `
				INSERT INTO finance_entries (kind, description, category_id, supplier_id, amount_cents, due_date, notes, created_by)
				VALUES ('PAYABLE', $1, $2, $3, $4, $5, $6, $7) RETURNING id`,
				description, *category, *l.supplier, l.cents, due.Time, notes, by).Scan(&entry); err != nil {
				return err
			}
			var payout uuid.UUID
			if err := tx.QueryRow(ctx, `
				INSERT INTO affiliate_payouts (affiliate_id, month, amount_cents, commissions, finance_entry_id, created_by)
				VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
				*l.id, m, l.cents, len(l.commissions), entry, by).Scan(&payout); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE affiliate_commissions SET payout_id = $2 WHERE id = ANY($1)`,
				l.commissions, payout); err != nil {
				return err
			}
			ids = append(ids, payout)
		}
		return nil
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return s.payouts(ctx, `WHERE p.id = ANY($1)`, ids)
}

// Payout é um fechamento de um afiliado e a conta a pagar dele.
type Payout struct {
	ID            uuid.UUID  `json:"id"`
	AffiliateID   uuid.UUID  `json:"affiliateId"`
	AffiliateName string     `json:"affiliateName"`
	Month         string     `json:"month"`
	AmountCents   int64      `json:"amountCents"`
	Commissions   int        `json:"commissions"`
	EntryID       *uuid.UUID `json:"entryId"`
	// Status: OPEN (a pagar), PAID (paga) ou MISSING (a conta foi excluída
	// ou cancelada na Empresa).
	Status    string        `json:"status"`
	DueDate   *billing.Date `json:"dueDate"`
	PaidOn    *billing.Date `json:"paidOn"`
	CreatedAt time.Time     `json:"createdAt"`
}

func (s *Service) payouts(ctx context.Context, where string, args ...any) ([]*Payout, error) {
	rows, err := s.db.Query(ctx, `
		SELECT p.id, p.affiliate_id, a.name, to_char(p.month, 'YYYY-MM'), p.amount_cents, p.commissions,
			p.finance_entry_id, COALESCE(e.status, ''), e.due_date, e.paid_on, p.created_at
		FROM affiliate_payouts p
		JOIN affiliates a ON a.id = p.affiliate_id
		LEFT JOIN finance_entries e ON e.id = p.finance_entry_id `+where+`
		ORDER BY p.created_at DESC, a.name LIMIT 200`, args...)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	out := []*Payout{}
	for rows.Next() {
		var p Payout
		var status string
		var due, paid *time.Time
		if err := rows.Scan(&p.ID, &p.AffiliateID, &p.AffiliateName, &p.Month, &p.AmountCents, &p.Commissions,
			&p.EntryID, &status, &due, &paid, &p.CreatedAt); err != nil {
			return nil, err
		}
		switch status {
		case "PAID":
			p.Status = "PAID"
		case "OPEN":
			p.Status = "OPEN"
		default:
			p.Status = "MISSING"
		}
		if due != nil {
			p.DueDate = &billing.Date{Time: *due}
		}
		if paid != nil {
			p.PaidOn = &billing.Date{Time: *paid}
		}
		out = append(out, &p)
	}
	return out, rows.Err()
}

// Payouts lista os fechamentos, os mais recentes primeiro.
func (s *Service) Payouts(ctx context.Context) ([]*Payout, error) {
	return s.payouts(ctx, ``)
}

// UndoPayout desfaz um fechamento ainda não pago: a conta a pagar sai da
// Empresa e as comissões voltam a ficar abertas.
func (s *Service) UndoPayout(ctx context.Context, id uuid.UUID) error {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var entry *uuid.UUID
		var status *string
		if err := tx.QueryRow(ctx, `
			SELECT p.finance_entry_id, e.status FROM affiliate_payouts p
			LEFT JOIN finance_entries e ON e.id = p.finance_entry_id
			WHERE p.id = $1 FOR UPDATE OF p`, id).Scan(&entry, &status); err != nil {
			return err
		}
		if status != nil && *status == "PAID" {
			return invalid("A conta deste fechamento já foi paga: reabra a conta na Empresa antes de desfazer.")
		}
		if entry != nil {
			if _, err := tx.Exec(ctx, `DELETE FROM finance_entries WHERE id = $1`, *entry); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `DELETE FROM affiliate_payouts WHERE id = $1`, id)
		return err
	})
	return mapErr(err)
}

// ---------------------------------------------------------------------------
// A página do afiliado (link secreto)
// ---------------------------------------------------------------------------

// ReportMonth é um mês na página do afiliado.
type ReportMonth struct {
	Month string `json:"month"`
	// Vehicles: os veículos com a mensalidade paga no mês (uma comissão cada).
	Vehicles    int   `json:"vehicles"`
	AmountCents int64 `json:"amountCents"`
	// Status: pending (o mês ainda não foi fechado), closed (fechado, a
	// pagar) ou paid (pago).
	Status string `json:"status"`
}

// Report é o que o afiliado vê: os números, sem dado pessoal de ninguém.
type Report struct {
	Name            string        `json:"name"`
	Handle          string        `json:"handle"`
	Code            string        `json:"code"`
	Active          bool          `json:"active"`
	CommissionCents int           `json:"commissionCents"`
	Signups         int           `json:"signups"`
	Customers       int           `json:"customers"`
	ActiveCustomers int           `json:"activeCustomers"`
	ActiveVehicles  int           `json:"activeVehicles"`
	ToReceiveCents  int64         `json:"toReceiveCents"`
	PaidCents       int64         `json:"paidCents"`
	Months          []ReportMonth `json:"months"`
}

// ReferralCode é o código do link de indicação do afiliado ativo, pelo link
// secreto da página dele ("" se o link não vale ou o afiliado está pausado).
func (s *Service) ReferralCode(ctx context.Context, token string) (string, error) {
	if len(token) < 20 || len(token) > 64 {
		return "", nil
	}
	var code string
	err := s.db.QueryRow(ctx, `SELECT code FROM affiliates WHERE report_token = $1 AND active`, token).Scan(&code)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return code, database.MapError(err)
}

// Report é a página do afiliado pelo link secreto (nil se o link não vale).
func (s *Service) Report(ctx context.Context, token string) (*Report, error) {
	if len(token) < 20 || len(token) > 64 {
		return nil, nil
	}
	a, err := scan(s.db.QueryRow(ctx, `SELECT `+columns+` FROM affiliates a WHERE a.report_token = $1`, token))
	if errors.Is(err, database.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := &Report{
		Name: a.Name, Handle: a.Handle, Code: a.Code, Active: a.Active, CommissionCents: a.CommissionCents,
		Signups: a.Signups, Customers: a.Customers, ActiveCustomers: a.ActiveCustomers, ActiveVehicles: a.ActiveVehicles,
		ToReceiveCents: a.PendingCents + a.OpenCents, PaidCents: a.PaidCents, Months: []ReportMonth{},
	}
	from := time.Date(s.now().In(s.loc).Year(), s.now().In(s.loc).Month()-11, 1, 0, 0, 0, 0, time.UTC)
	rows, err := s.db.Query(ctx, `
		SELECT to_char(c.month, 'YYYY-MM'), count(*), sum(c.amount_cents)::bigint,
			count(*) FILTER (WHERE c.payout_id IS NULL), count(*) FILTER (WHERE e.status = 'PAID')
		FROM affiliate_commissions c
		LEFT JOIN affiliate_payouts p ON p.id = c.payout_id
		LEFT JOIN finance_entries e ON e.id = p.finance_entry_id
		WHERE c.affiliate_id = $1 AND c.month >= $2
		GROUP BY c.month ORDER BY c.month DESC`, a.ID, from)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var m ReportMonth
		var open, paid int
		if err := rows.Scan(&m.Month, &m.Vehicles, &m.AmountCents, &open, &paid); err != nil {
			return nil, err
		}
		switch {
		case paid == m.Vehicles:
			m.Status = "paid"
		case open > 0:
			m.Status = "pending"
		default:
			m.Status = "closed"
		}
		out.Months = append(out.Months, m)
	}
	return out, rows.Err()
}
