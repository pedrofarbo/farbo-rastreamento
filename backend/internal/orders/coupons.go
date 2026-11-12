package orders

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// Cupons de desconto da contratação direta pelo site (sem o link de um
// representante): a central cria; cada um abate a mensalidade — em
// porcentagem ou num valor fixo — nas primeiras cobranças da assinatura (no
// semestral e no anual, cada cobrança é um período) ou para sempre. Nas
// primeiras, o desconto vira o preço promocional da assinatura, e o valor
// normal volta sozinho depois; para sempre, vira o valor da assinatura (o
// reajuste anual segue valendo sobre ele).

// ErrCouponUsed: o cupom já foi usado e não pode ser excluído (desative).
var ErrCouponUsed = errors.New("o cupom já foi usado: desative em vez de excluir")

// couponCode é o formato do código: maiúsculas, números, - e _.
var couponCode = regexp.MustCompile(`^[A-Z0-9_-]{3,30}$`)

// Coupon é um cupom de desconto, com quantas contratações já o usaram.
type Coupon struct {
	ID          uuid.UUID `json:"id"`
	Code        string    `json:"code"`
	Description string    `json:"description"`
	// Um dos dois: Percent (1 a 100) ou AmountCents (o valor fixo abatido em
	// cada cobrança).
	Percent     *int `json:"percent"`
	AmountCents *int `json:"amountCents"`
	// Charges: em quantas das primeiras cobranças o desconto vale; nulo,
	// em todas (para sempre).
	Charges *int `json:"charges"`
	// ValidUntil: até que dia (inclusive) dá para usar; nulo, sem validade.
	ValidUntil *billing.Date `json:"validUntil"`
	// MaxUses: quantas contratações podem usar; nulo, sem limite.
	MaxUses   *int      `json:"maxUses"`
	Active    bool      `json:"active"`
	Uses      int       `json:"uses"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// CouponInput é o que a central informa (o código, só na criação).
type CouponInput struct {
	Code        string        `json:"code"`
	Description string        `json:"description"`
	Percent     *int          `json:"percent"`
	AmountCents *int          `json:"amountCents"`
	Charges     *int          `json:"charges"`
	ValidUntil  *billing.Date `json:"validUntil"`
	MaxUses     *int          `json:"maxUses"`
	Active      bool          `json:"active"`
}

// CouponOffer é o que o site mostra de um cupom válido: o desconto e em
// quantas cobranças.
type CouponOffer struct {
	Code        string `json:"code"`
	Description string `json:"description"`
	Percent     *int   `json:"percent,omitempty"`
	AmountCents *int   `json:"amountCents,omitempty"`
	// Charges nulo: para sempre.
	Charges *int `json:"charges"`
}

// Offer é o cupom como o site mostra.
func (c *Coupon) Offer() CouponOffer {
	return CouponOffer{Code: c.Code, Description: c.Description, Percent: c.Percent, AmountCents: c.AmountCents, Charges: c.Charges}
}

// Discounted é o valor da cobrança com o desconto (nunca abaixo de zero): a
// porcentagem arredondada ao centavo, ou o valor fixo.
func (c *Coupon) Discounted(cents int) int {
	switch {
	case c.Percent != nil:
		return cents - (cents**c.Percent+50)/100
	case c.AmountCents != nil:
		return max(cents-*c.AmountCents, 0)
	}
	return cents
}

// NormalizeCouponCode deixa o código como fica guardado: sem espaços, em
// maiúsculas.
func NormalizeCouponCode(code string) string {
	return strings.ToUpper(strings.Join(strings.Fields(code), ""))
}

func (in *CouponInput) check() error {
	in.Code = NormalizeCouponCode(in.Code)
	in.Description = strings.TrimSpace(in.Description)
	switch {
	case !couponCode.MatchString(in.Code):
		return ValidationError{"o código do cupom tem de 3 a 30 letras, números, - ou _ (sem espaços nem acentos)"}
	case len([]rune(in.Description)) > 200:
		return ValidationError{"a descrição do cupom vai até 200 caracteres"}
	case (in.Percent == nil) == (in.AmountCents == nil):
		return ValidationError{"o cupom dá a porcentagem ou o valor fixo de desconto (um dos dois)"}
	case in.Percent != nil && (*in.Percent < 1 || *in.Percent > 100):
		return ValidationError{"a porcentagem do cupom vai de 1 a 100"}
	case in.AmountCents != nil && (*in.AmountCents < 1 || *in.AmountCents > 100_000_00):
		return ValidationError{"o valor do cupom vai de R$ 0,01 a R$ 100.000,00"}
	case in.Charges != nil && (*in.Charges < 1 || *in.Charges > 60):
		return ValidationError{"o desconto vale de 1 a 60 cobranças (vazio: para sempre)"}
	case in.MaxUses != nil && (*in.MaxUses < 1 || *in.MaxUses > 1_000_000):
		return ValidationError{"o limite de usos vai de 1 a 1.000.000 (vazio: sem limite)"}
	}
	return nil
}

const couponColumns = `c.id, c.code, c.description, c.percent, c.amount_cents, c.charges, c.valid_until, c.max_uses,
	c.active, (SELECT count(*) FROM coupon_redemptions r WHERE r.coupon_id = c.id), c.created_at, c.updated_at`

func scanCoupon(row database.Scanner) (*Coupon, error) {
	var c Coupon
	var until *time.Time
	if err := row.Scan(&c.ID, &c.Code, &c.Description, &c.Percent, &c.AmountCents, &c.Charges, &until, &c.MaxUses,
		&c.Active, &c.Uses, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, database.MapError(err)
	}
	if until != nil {
		c.ValidUntil = &billing.Date{Time: *until}
	}
	return &c, nil
}

// Coupons são todos os cupons, os ativos primeiro e os mais novos antes.
func (s *Service) Coupons(ctx context.Context) ([]Coupon, error) {
	rows, err := s.db.Query(ctx, `SELECT `+couponColumns+` FROM coupons c ORDER BY c.active DESC, c.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Coupon{}
	for rows.Next() {
		c, err := scanCoupon(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

// CreateCoupon cria o cupom; um código repetido é database.ErrConflict.
func (s *Service) CreateCoupon(ctx context.Context, in CouponInput, by *uuid.UUID) (*Coupon, error) {
	if err := in.check(); err != nil {
		return nil, err
	}
	var id uuid.UUID
	if err := s.db.QueryRow(ctx, `
		INSERT INTO coupons (code, description, percent, amount_cents, charges, valid_until, max_uses, active, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`,
		in.Code, in.Description, in.Percent, in.AmountCents, in.Charges, dateOrNil(in.ValidUntil), in.MaxUses, in.Active, by,
	).Scan(&id); err != nil {
		return nil, database.MapError(err)
	}
	return s.coupon(ctx, s.db, id)
}

// UpdateCoupon muda o cupom (o código fica). Vale para as contratações
// daqui em diante: quem já usou segue com o desconto que ganhou.
func (s *Service) UpdateCoupon(ctx context.Context, id uuid.UUID, in CouponInput) (*Coupon, error) {
	current, err := s.coupon(ctx, s.db, id)
	if err != nil {
		return nil, err
	}
	in.Code = current.Code
	if err := in.check(); err != nil {
		return nil, err
	}
	tag, err := s.db.Exec(ctx, `
		UPDATE coupons SET description = $2, percent = $3, amount_cents = $4, charges = $5, valid_until = $6,
			max_uses = $7, active = $8, updated_at = NOW()
		WHERE id = $1`, id, in.Description, in.Percent, in.AmountCents, in.Charges, dateOrNil(in.ValidUntil), in.MaxUses, in.Active)
	if err != nil {
		return nil, database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return nil, database.ErrNotFound
	}
	return s.coupon(ctx, s.db, id)
}

// DeleteCoupon exclui o cupom que ninguém usou (ErrCouponUsed se já usaram).
func (s *Service) DeleteCoupon(ctx context.Context, id uuid.UUID) error {
	tag, err := s.db.Exec(ctx, `DELETE FROM coupons c WHERE c.id = $1
		AND NOT EXISTS (SELECT 1 FROM coupon_redemptions r WHERE r.coupon_id = c.id)`, id)
	if err != nil {
		if errors.Is(database.MapError(err), database.ErrForeignKey) {
			return ErrCouponUsed
		}
		return err
	}
	if tag.RowsAffected() == 0 {
		if _, err := s.coupon(ctx, s.db, id); err != nil {
			return err
		}
		return ErrCouponUsed
	}
	return nil
}

func (s *Service) coupon(ctx context.Context, q database.Querier, id uuid.UUID) (*Coupon, error) {
	return scanCoupon(q.QueryRow(ctx, `SELECT `+couponColumns+` FROM coupons c WHERE c.id = $1`, id))
}

// usable diz por que o cupom não vale hoje (vazio: vale).
func (c *Coupon) usable(today billing.Date) string {
	switch {
	case !c.Active:
		return "este cupom não está mais valendo"
	case c.ValidUntil != nil && c.ValidUntil.Before(today):
		return "este cupom venceu em " + c.ValidUntil.Format("02/01/2006")
	case c.MaxUses != nil && c.Uses >= *c.MaxUses:
		return "este cupom já foi usado o máximo de vezes"
	}
	return ""
}

// CheckoutCoupon é o cupom do código, se ele vale hoje na contratação pelo
// site; senão, ValidationError com o motivo.
func (s *Service) CheckoutCoupon(ctx context.Context, code string) (*Coupon, error) {
	return s.checkoutCoupon(ctx, s.db, code, false)
}

func (s *Service) checkoutCoupon(ctx context.Context, q database.Querier, code string, lock bool) (*Coupon, error) {
	code = NormalizeCouponCode(code)
	if code == "" {
		return nil, ValidationError{"informe o cupom"}
	}
	if lock {
		// A trava antes, num comando à parte: a contagem dos usos, no seguinte,
		// já vê o uso de quem tinha a trava antes.
		if _, err := q.Exec(ctx, `SELECT 1 FROM coupons WHERE code = $1 FOR UPDATE`, code); err != nil {
			return nil, err
		}
	}
	c, err := scanCoupon(q.QueryRow(ctx, `SELECT `+couponColumns+` FROM coupons c WHERE c.code = $1`, code))
	if errors.Is(err, database.ErrNotFound) {
		return nil, ValidationError{"cupom não encontrado: confira o código"}
	}
	if err != nil {
		return nil, err
	}
	if reason := c.usable(s.billing.Today()); reason != "" {
		return nil, ValidationError{reason}
	}
	return c, nil
}

// HasCheckoutCoupons diz se há algum cupom valendo hoje (sem nenhum, o site
// nem mostra o campo).
func (s *Service) HasCheckoutCoupons(ctx context.Context) (bool, error) {
	var ok bool
	err := s.db.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM coupons c WHERE c.active AND (c.valid_until IS NULL OR c.valid_until >= $1)
			AND (c.max_uses IS NULL OR c.max_uses > (SELECT count(*) FROM coupon_redemptions r WHERE r.coupon_id = c.id)))`,
		s.billing.Today().Time).Scan(&ok)
	return ok, err
}

// applyCoupon confere o cupom de novo (travado, na transação do pedido: duas
// contratações ao mesmo tempo não passam do limite) e dá à assinatura o
// desconto: nas primeiras cobranças, o preço promocional; para sempre, o
// valor da assinatura com o desconto. Devolve o cupom e o desconto de cada
// cobrança, para o uso ser gravado com a assinatura.
func (s *Service) applyCoupon(ctx context.Context, tx pgx.Tx, code string, sub *billing.Subscription) (*Coupon, int, error) {
	c, err := s.checkoutCoupon(ctx, tx, code, true)
	if err != nil {
		return nil, 0, err
	}
	discounted := c.Discounted(sub.PriceCents)
	discount := sub.PriceCents - discounted
	sub.CouponCode = c.Code
	if c.Charges == nil {
		sub.PriceCents = discounted
		return c, discount, nil
	}
	until := billing.Date{Time: sub.NextDueDate.AddDate(0, *c.Charges*max(sub.PeriodMonths, 1), 0)}
	sub.PromoPriceCents, sub.PromoUntil = &discounted, &until
	return c, discount, nil
}

func dateOrNil(d *billing.Date) *time.Time {
	if d == nil {
		return nil
	}
	return &d.Time
}
