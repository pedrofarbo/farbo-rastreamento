package orders

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// LoanOffer é o comodato que o próprio cliente escolhe no pedido, quando a
// central não definiu um plano para ele: as taxas e, para a pessoa física, a
// "garantia de troca do rastreador" somada à mensalidade do plano.
// Desligado, o pedido só tem o rastreador incluso.
type LoanOffer struct {
	Enabled           bool `json:"enabled"`
	SurchargeCents    int  `json:"surchargeCents"`
	InstallFeeCents   int  `json:"installFeeCents"`
	UninstallFeeCents int  `json:"uninstallFeeCents"`
}

// Checkout são os preços da contratação rápida pela landing (pessoa física,
// paga com Pix na hora): o plano normal, já com o rastreador incluso, e o dos
// integrantes do Insanos MC (nas primeiras mensalidades, mais a parte do
// rastreador: TrackerRule). O rastreador não tem preço: a Farbo não o vende.
type Checkout struct {
	Enabled      bool `json:"enabled"`
	PlanCents    int  `json:"planCents"`
	InsanosCents int  `json:"insanosCents"`
}

// TrackerRule é o rastreador incluso dos pedidos novos: a parte dele em cada
// uma das Months primeiras mensalidades. Os planos abaixo do preço do site (a
// tabela do afiliado, que soma sempre, o dos Insanos MC, os combinados com a
// central) somam essa parte ao plano nessas mensalidades; o do site já a
// inclui. Encerrada a assinatura antes, o que falta vence numa fatura só.
type TrackerRule struct {
	Cents  int `json:"cents"`
	Months int `json:"months"`
}

// GuaranteeName é o que a mensalidade com a garantia de troca leva no nome
// do plano ("Plano Mensal + garantia de troca").
const GuaranteeName = " + garantia de troca"

// InstallService é a instalação pelos instaladores da Farbo no rastreador
// incluso, pelo preço daqui (desligada, o cliente instala por conta
// própria). No comodato, a instalação é sempre da Farbo, com a taxa dele.
type InstallService struct {
	Enabled    bool `json:"enabled"`
	PriceCents int  `json:"priceCents"`
}

// Options são as opções do pedido do cliente.
type Options struct {
	Loan      LoanOffer      `json:"loan"`
	Install   InstallService `json:"install"`
	Checkout  Checkout       `json:"checkout"`
	Tracker   TrackerRule    `json:"tracker"`
	UpdatedAt time.Time      `json:"updatedAt"`
}

// Options são as opções em vigor.
func (s *Service) Options(ctx context.Context) (*Options, error) {
	var o Options
	err := s.db.QueryRow(ctx, `
		SELECT loan_enabled, loan_surcharge_cents, loan_install_fee_cents, loan_uninstall_fee_cents,
			install_enabled, install_cents,
			checkout_enabled, checkout_plan_cents, checkout_insanos_cents, tracker_cents, tracker_months, updated_at
		FROM order_settings`).
		Scan(&o.Loan.Enabled, &o.Loan.SurchargeCents, &o.Loan.InstallFeeCents, &o.Loan.UninstallFeeCents,
			&o.Install.Enabled, &o.Install.PriceCents,
			&o.Checkout.Enabled, &o.Checkout.PlanCents, &o.Checkout.InsanosCents, &o.Tracker.Cents, &o.Tracker.Months, &o.UpdatedAt)
	return &o, database.MapError(err)
}

// Loan é o comodato oferecido no pedido.
func (s *Service) Loan(ctx context.Context) (*LoanOffer, error) {
	o, err := s.Options(ctx)
	if err != nil {
		return nil, err
	}
	return &o.Loan, nil
}

// SetOptions muda as opções (valem para os pedidos novos).
func (s *Service) SetOptions(ctx context.Context, in Options, by *uuid.UUID) (*Options, error) {
	const max = 100_000_00
	// Sem a regra do rastreador (telas antigas), fica a que está.
	if in.Tracker == (TrackerRule{}) {
		current, err := s.Options(ctx)
		if err != nil {
			return nil, err
		}
		in.Tracker = current.Tracker
	}
	l, c, t := in.Loan, in.Checkout, in.Tracker
	out := func(v int) bool { return v < 0 || v > max }
	switch {
	case out(l.SurchargeCents):
		return nil, ValidationError{"a garantia de troca vai de R$ 0 a R$ 100.000"}
	case out(l.InstallFeeCents) || out(l.UninstallFeeCents):
		return nil, ValidationError{"as taxas de instalação e de desinstalação vão de R$ 0 a R$ 100.000"}
	case out(in.Install.PriceCents):
		return nil, ValidationError{"a instalação vai de R$ 0 a R$ 100.000"}
	case out(c.PlanCents) || out(c.InsanosCents):
		return nil, ValidationError{"os preços da contratação pela landing vão de R$ 0 a R$ 100.000"}
	case out(t.Cents):
		return nil, ValidationError{"a parte do rastreador incluso vai de R$ 0 a R$ 100.000"}
	case t.Months < 1 || t.Months > 60:
		return nil, ValidationError{"a parte do rastreador incluso vai de 1 a 60 mensalidades"}
	}
	if _, err := s.db.Exec(ctx, `
		UPDATE order_settings SET loan_enabled = $1, loan_surcharge_cents = $2, loan_install_fee_cents = $3,
			loan_uninstall_fee_cents = $4, install_enabled = $5, install_cents = $6,
			checkout_enabled = $7, checkout_plan_cents = $8, checkout_insanos_cents = $9, tracker_cents = $10,
			tracker_months = $11, updated_by = $12, updated_at = NOW()`,
		l.Enabled, l.SurchargeCents, l.InstallFeeCents, l.UninstallFeeCents, in.Install.Enabled, in.Install.PriceCents,
		c.Enabled, c.PlanCents, c.InsanosCents, t.Cents, t.Months, by); err != nil {
		return nil, database.MapError(err)
	}
	return s.Options(ctx)
}

// PaysUpfront diz se o pedido do cliente é pago no ato: a pessoa física
// (CPF, ou ainda sem documento) paga a 1ª mensalidade, o frete e a
// instalação, quando houver, na contratação; a empresa (CNPJ) tem tudo na
// próxima fatura do mês da conta, como as associações.
func (s *Service) PaysUpfront(ctx context.Context, customerID uuid.UUID) (bool, error) {
	var document string
	if err := s.db.QueryRow(ctx, `SELECT document FROM users WHERE id = $1`, customerID).Scan(&document); err != nil {
		return false, database.MapError(err)
	}
	return !isCompany(document), nil
}

// isCompany: o documento é um CNPJ (14 números).
func isCompany(document string) bool {
	digits := 0
	for _, r := range document {
		if r >= '0' && r <= '9' {
			digits++
		}
	}
	return digits == 14
}

// Tracker é o rastreador incluso de um pedido: a parte dele em cada uma das
// Months primeiras mensalidades, somada ao plano (Added) ou já dentro do
// preço. Zero: sem rastreador incluso (o comodato, o aparelho do cliente).
type Tracker struct {
	Cents  int  `json:"cents"`
	Months int  `json:"months"`
	Added  bool `json:"added"`
}

func (t Tracker) check() error {
	switch {
	case t.Cents < 0 || t.Cents > 100_000_00:
		return ValidationError{"a parte do rastreador incluso vai de R$ 0 a R$ 100.000"}
	case t.Months < 0 || t.Months > 60 || (t.Cents > 0 && t.Months == 0):
		return ValidationError{"a parte do rastreador incluso vai de 1 a 60 mensalidades"}
	}
	return nil
}

// SitePlanCents é o preço do plano do site, que já inclui o rastreador: o da
// contratação pelo site ou, sem ele, o do catálogo.
func (s *Service) SitePlanCents(o *Options) int {
	if o != nil && o.Checkout.PlanCents > 0 {
		return o.Checkout.PlanCents
	}
	return s.catalog.PlanPriceCents
}

// TrackerFor é o rastreador incluso de um plano novo: abaixo do preço do
// site (ou pela tabela do afiliado, sempre), a parte dele é somada nas
// primeiras mensalidades; no preço do site ou acima, ela já está dentro.
func (s *Service) TrackerFor(o *Options, plan billing.SubscriptionInput, table bool) Tracker {
	return Tracker{Cents: o.Tracker.Cents, Months: o.Tracker.Months,
		Added: table || plan.PriceCents < s.SitePlanCents(o)}
}
