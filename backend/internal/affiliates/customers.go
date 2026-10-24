package affiliates

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// Indique e ganhe: o cliente tem um link de indicação próprio (um afiliado
// ligado a ele, sem comissão em dinheiro). Quem chega pelo link e vira
// cliente fica indicado por ele, como nos afiliados; quando o indicado paga
// a primeira mensalidade, quem indicou ganha um crédito (o valor da
// configuração), que abate das próximas faturas de mensalidade dele.

// ErrNotEligible: o associado de uma organização não participa (quem paga é
// a organização).
var ErrNotEligible = errors.New("o indique e ganhe é para quem paga o próprio rastreamento")

// Credit é um crédito ganho por indicação.
type Credit struct {
	ReferredName   string    `json:"referredName"`
	AmountCents    int       `json:"amountCents"`
	RemainingCents int       `json:"remainingCents"`
	CreatedAt      time.Time `json:"createdAt"`
}

// Program é o indique e ganhe do cliente: o link, os números e os créditos.
type Program struct {
	Code        string   `json:"code"`
	RewardCents int      `json:"rewardCents"`
	Signups     int      `json:"signups"`
	Customers   int      `json:"customers"`
	EarnedCents int64    `json:"earnedCents"`
	OpenCents   int64    `json:"openCents"`
	Credits     []Credit `json:"credits"`
}

// SaveCustomerReward muda o crédito por indicação (zero desliga; os créditos
// já dados ficam como estão).
func (s *Service) SaveCustomerReward(ctx context.Context, cents int) error {
	if cents < 0 || cents > MaxCommissionCents {
		return invalid("Valor do crédito por indicação inválido.")
	}
	_, err := s.db.Exec(ctx, `UPDATE affiliate_settings SET customer_reward_cents = $1, updated_at = NOW()`, cents)
	return database.MapError(err)
}

// customerCode acha (ou cria) o link do cliente.
func (s *Service) customerCode(ctx context.Context, customerID uuid.UUID) (string, error) {
	var code string
	err := s.db.QueryRow(ctx, `SELECT code FROM affiliates WHERE customer_id = $1`, customerID).Scan(&code)
	if err == nil {
		return code, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", database.MapError(err)
	}
	var name, email, role string
	var org *uuid.UUID
	if err := s.db.QueryRow(ctx, `SELECT name, email, role, organization_id FROM users WHERE id = $1`, customerID).
		Scan(&name, &email, &role, &org); err != nil {
		return "", database.MapError(err)
	}
	if role != "customer" || org != nil {
		return "", ErrNotEligible
	}
	first := strings.Fields(name)
	display := "Cliente Farbo"
	if len(first) > 0 {
		display = first[0]
	}
	base := Code(display)
	if len(base) > 20 {
		base = base[:20]
	}
	if base == "" {
		base = "cliente"
	}
	token, err := newToken()
	if err != nil {
		return "", err
	}
	// O código: o primeiro nome e um sufixo curto ("ana-7k2p"); colidiu, outro.
	for range 5 {
		candidate := base + "-" + shortSuffix()
		err := s.db.QueryRow(ctx, `
			INSERT INTO affiliates (name, code, report_token, commission_cents, email, customer_id)
			VALUES ($1, $2, $3, 0, $4, $5)
			ON CONFLICT (customer_id) DO UPDATE SET updated_at = affiliates.updated_at
			RETURNING code`, display, candidate, token, email, customerID).Scan(&code)
		if err == nil {
			return code, nil
		}
		if !errors.Is(database.MapError(err), database.ErrConflict) {
			return "", database.MapError(err)
		}
	}
	return "", fmt.Errorf("não foi possível gerar o código de indicação")
}

// shortSuffix: 4 letras e números, sem os que se confundem.
func shortSuffix() string {
	const alphabet = "abcdefghjkmnpqrstuvwxyz23456789"
	raw := make([]byte, 4)
	_, _ = rand.Read(raw)
	for i := range raw {
		raw[i] = alphabet[int(raw[i])%len(alphabet)]
	}
	return string(raw)
}

// CustomerProgram é o indique e ganhe do cliente (cria o link na primeira vez).
func (s *Service) CustomerProgram(ctx context.Context, customerID uuid.UUID) (*Program, error) {
	code, err := s.customerCode(ctx, customerID)
	if err != nil {
		return nil, err
	}
	out := &Program{Code: code, Credits: []Credit{}}
	if err := s.db.QueryRow(ctx, `
		SELECT st.customer_reward_cents,
			(SELECT count(DISTINCT lower(x.email)) FROM (
				SELECT email FROM launch_waitlist WHERE affiliate_id = a.id
				UNION ALL SELECT email FROM leads WHERE affiliate_id = a.id) x),
			(SELECT count(*) FROM affiliate_referrals r WHERE r.affiliate_id = a.id)
		FROM affiliates a, affiliate_settings st WHERE a.customer_id = $1`, customerID).
		Scan(&out.RewardCents, &out.Signups, &out.Customers); err != nil {
		return nil, database.MapError(err)
	}
	rows, err := s.db.Query(ctx, `
		SELECT split_part(u.name, ' ', 1), c.amount_cents, c.remaining_cents, c.created_at
		FROM referral_credits c JOIN users u ON u.id = c.referred_id
		WHERE c.customer_id = $1 ORDER BY c.created_at DESC`, customerID)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var c Credit
		if err := rows.Scan(&c.ReferredName, &c.AmountCents, &c.RemainingCents, &c.CreatedAt); err != nil {
			return nil, database.MapError(err)
		}
		out.Credits = append(out.Credits, c)
		out.EarnedCents += int64(c.AmountCents)
		out.OpenCents += int64(c.RemainingCents)
	}
	return out, database.MapError(rows.Err())
}

// RewardReferrals dá o crédito a quem indicou, quando o indicado pagou a
// primeira mensalidade (um crédito por indicado; o valor é o da
// configuração na hora).
func (s *Service) RewardReferrals(ctx context.Context) (int64, error) {
	tag, err := s.db.Exec(ctx, `
		INSERT INTO referral_credits (customer_id, referred_id, amount_cents, remaining_cents)
		SELECT a.customer_id, r.customer_id, st.customer_reward_cents, st.customer_reward_cents
		FROM affiliate_referrals r
		JOIN affiliates a ON a.id = r.affiliate_id AND a.customer_id IS NOT NULL AND a.customer_id <> r.customer_id
		JOIN users ref ON ref.id = a.customer_id AND ref.active AND ref.organization_id IS NULL
		CROSS JOIN affiliate_settings st
		WHERE st.customer_reward_cents > 0 AND EXISTS (
			SELECT 1 FROM invoices i WHERE i.customer_id = r.customer_id AND i.status = 'PAID'
				AND i.subscription_id IS NOT NULL AND i.amount_cents > 0)
		ON CONFLICT (referred_id) DO NOTHING`)
	if err != nil {
		return 0, database.MapError(err)
	}
	return tag.RowsAffected(), nil
}

// ApplyCredits abate os créditos nas faturas de mensalidade em aberto de quem
// indicou (as que vencem de hoje em diante e ainda sem Pix ou boleto
// gerado, para o valor cobrado não mudar debaixo do cliente). A fatura que
// fica zerada é dada como paga com o crédito.
func (s *Service) ApplyCredits(ctx context.Context) (int, error) {
	today := s.now().In(s.loc).Format(time.DateOnly)
	rows, err := s.db.Query(ctx, `SELECT DISTINCT customer_id FROM referral_credits WHERE remaining_cents > 0`)
	if err != nil {
		return 0, database.MapError(err)
	}
	var customers []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, database.MapError(err)
		}
		customers = append(customers, id)
	}
	rows.Close()
	applied := 0
	for _, customer := range customers {
		n, err := s.applyCustomerCredits(ctx, customer, today)
		if err != nil {
			return applied, err
		}
		applied += n
	}
	return applied, nil
}

func (s *Service) applyCustomerCredits(ctx context.Context, customerID uuid.UUID, today string) (int, error) {
	applied := 0
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		for {
			var creditID uuid.UUID
			var remaining int
			err := tx.QueryRow(ctx, `
				SELECT id, remaining_cents FROM referral_credits WHERE customer_id = $1 AND remaining_cents > 0
				ORDER BY created_at LIMIT 1 FOR UPDATE`, customerID).Scan(&creditID, &remaining)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			if err != nil {
				return err
			}
			var invoiceID uuid.UUID
			var amount int
			err = tx.QueryRow(ctx, `
				SELECT i.id, i.amount_cents FROM invoices i
				WHERE i.customer_id = $1 AND i.status = 'OPEN' AND i.subscription_id IS NOT NULL
					AND i.amount_cents > 0 AND i.due_date >= $2::date
					AND NOT EXISTS (SELECT 1 FROM payment_charges c WHERE c.invoice_id = i.id AND c.status = 'PENDING')
					AND NOT EXISTS (SELECT 1 FROM payment_charge_invoices ci JOIN payment_charges c ON c.id = ci.charge_id
						WHERE ci.invoice_id = i.id AND c.status = 'PENDING')
				ORDER BY i.due_date, i.created_at LIMIT 1 FOR UPDATE OF i`, customerID, today).Scan(&invoiceID, &amount)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil // sem fatura para abater agora: fica para a próxima
			}
			if err != nil {
				return err
			}
			use := min(remaining, amount)
			if _, err := tx.Exec(ctx, `
				UPDATE invoices SET amount_cents = amount_cents - $2,
					description = CASE WHEN description LIKE '%(desconto de indicação)' THEN description
						ELSE description || ' (desconto de indicação)' END,
					status = CASE WHEN amount_cents - $2 = 0 THEN 'PAID' ELSE status END,
					paid_at = CASE WHEN amount_cents - $2 = 0 THEN NOW() ELSE paid_at END,
					paid_via = CASE WHEN amount_cents - $2 = 0 THEN 'CREDIT' ELSE paid_via END,
					updated_at = NOW()
				WHERE id = $1`, invoiceID, use); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO referral_credit_uses (credit_id, invoice_id, amount_cents) VALUES ($1, $2, $3)`,
				creditID, invoiceID, use); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				UPDATE referral_credits SET remaining_cents = remaining_cents - $2 WHERE id = $1`, creditID, use); err != nil {
				return err
			}
			applied++
		}
	})
	return applied, database.MapError(err)
}
