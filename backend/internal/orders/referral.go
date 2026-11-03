package orders

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/vehicles"
)

// A mensalidade pelo link do afiliado: a tabela por tipo de veículo (moto,
// carro e caminhão) que a central dá a cada afiliado. Quem contrata pelo link
// paga por ela, e o cliente indicado segue nela em todos os veículos (também
// nos que pedir depois pelo app). O plano que a central define para o cliente
// vale acima dela.

// AffiliatePrices é a tabela do link do afiliado ativo (nil: sem tabela ou
// afiliado pausado — o preço normal).
func (s *Service) AffiliatePrices(ctx context.Context, affiliateID uuid.UUID) (*vehicles.KindPrices, error) {
	return s.kindPrices(ctx, `SELECT plan_motorcycle_cents, plan_car_cents, plan_truck_cents
		FROM affiliates WHERE id = $1 AND active`, affiliateID)
}

// ReferralPrices é a tabela do afiliado que indicou o cliente (nil: sem
// afiliado ou sem tabela). Pausar o afiliado não tira a tabela de quem ele já
// indicou.
func (s *Service) ReferralPrices(ctx context.Context, customerID uuid.UUID) (*vehicles.KindPrices, error) {
	return s.kindPrices(ctx, `SELECT a.plan_motorcycle_cents, a.plan_car_cents, a.plan_truck_cents
		FROM affiliate_referrals r JOIN affiliates a ON a.id = r.affiliate_id WHERE r.customer_id = $1`, customerID)
}

func (s *Service) kindPrices(ctx context.Context, query string, id uuid.UUID) (*vehicles.KindPrices, error) {
	var motorcycle, car, truck *int
	err := s.db.QueryRow(ctx, query, id).Scan(&motorcycle, &car, &truck)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, database.MapError(err)
	}
	if motorcycle == nil || car == nil || truck == nil {
		return nil, nil
	}
	return &vehicles.KindPrices{MotorcycleCents: *motorcycle, CarCents: *car, TruckCents: *truck}, nil
}

// Os modos da instalação pelo link do afiliado (sem modo: as da Farbo).
const (
	// InstallCustom: preços próprios, cobrados pela Farbo.
	InstallCustom = "custom"
	// InstallDirect: o representante instala, desinstala e recebe direto do
	// cliente; a Farbo não cobra nada disso.
	InstallDirect = "direct"
)

// InstallPrices é a instalação (a do rastreador comprado e a do comodato) e
// a desinstalação do comodato pelo link de um afiliado, com a garantia de
// troca do comodato dele (nil: a da Farbo).
type InstallPrices struct {
	Mode           string `json:"mode"`
	InstallCents   int    `json:"installCents"`
	UninstallCents int    `json:"uninstallCents"`
	// GuaranteeCents: a garantia de troca do rastreador no comodato, por mês
	// (a Farbo cobra na mensalidade, também no modo direto); nil, a da Farbo.
	GuaranteeCents *int `json:"guaranteeCents"`
}

// Valid: um dos modos, com os valores na faixa.
func (p InstallPrices) Valid() bool {
	in := func(cents int) bool { return cents >= 0 && cents <= vehicles.MaxPlanCents }
	return (p.Mode == InstallCustom || p.Mode == InstallDirect) && in(p.InstallCents) && in(p.UninstallCents) &&
		(p.GuaranteeCents == nil || in(*p.GuaranteeCents))
}

// Guarantee é a garantia de troca do comodato: a do afiliado ou, sem ela, a
// da Farbo (farbo).
func (p *InstallPrices) Guarantee(farbo int) int {
	if p == nil || p.GuaranteeCents == nil {
		return farbo
	}
	return *p.GuaranteeCents
}

// Charged é o que a Farbo cobra da instalação e da desinstalação (no modo
// direto, nada: o representante recebe do cliente).
func (p InstallPrices) Charged() (install, uninstall int) {
	if p.Mode == InstallDirect {
		return 0, 0
	}
	return p.InstallCents, p.UninstallCents
}

// AffiliateInstall é a instalação do link do afiliado ativo (nil: a da Farbo).
func (s *Service) AffiliateInstall(ctx context.Context, affiliateID uuid.UUID) (*InstallPrices, error) {
	return s.installPrices(ctx, `SELECT install_mode, install_cents, uninstall_cents, loan_surcharge_cents
		FROM affiliates WHERE id = $1 AND active`, affiliateID)
}

// CustomerInstall é a instalação do afiliado que indicou o cliente, quando a
// central não definiu um plano para ele (nil: a da Farbo).
func (s *Service) CustomerInstall(ctx context.Context, customerID uuid.UUID) (*InstallPrices, error) {
	account, err := s.AccountPlan(ctx, customerID)
	if err != nil || account != nil {
		return nil, err
	}
	return s.installPrices(ctx, `SELECT a.install_mode, a.install_cents, a.uninstall_cents, a.loan_surcharge_cents
		FROM affiliate_referrals r JOIN affiliates a ON a.id = r.affiliate_id WHERE r.customer_id = $1`, customerID)
}

func (s *Service) installPrices(ctx context.Context, query string, id uuid.UUID) (*InstallPrices, error) {
	var p InstallPrices
	var install, uninstall *int
	err := s.db.QueryRow(ctx, query, id).Scan(&p.Mode, &install, &uninstall, &p.GuaranteeCents)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, database.MapError(err)
	}
	if p.Mode == "" || install == nil || uninstall == nil {
		return nil, nil
	}
	p.InstallCents, p.UninstallCents = *install, *uninstall
	return &p, nil
}

// WithInstall aplica a instalação do afiliado às opções do pedido mostradas
// (a do rastreador comprado, as taxas e a garantia de troca do comodato): o
// preço dele, também com a instalação da Farbo desligada.
func (p *InstallPrices) WithInstall(install *InstallService, loan *LoanOffer) {
	if p == nil {
		return
	}
	install.Enabled, install.PriceCents = true, p.InstallCents
	loan.InstallFeeCents, loan.UninstallFeeCents = p.InstallCents, p.UninstallCents
	loan.SurchargeCents = p.Guarantee(loan.SurchargeCents)
}

// applyInstall põe no pedido o que a Farbo cobra da instalação do afiliado:
// a do rastreador comprado (com a instalação contratada) e as taxas do
// comodato.
func (p *InstallPrices) applyInstall(o *Order) {
	if p == nil {
		return
	}
	install, uninstall := p.Charged()
	if o.Terms.EquipmentMode == billing.EquipmentLoan {
		o.Terms.InstallFeeCents, o.Terms.UninstallFeeCents = install, uninstall
		return
	}
	if o.FarboInstall {
		o.InstallCents = install
	}
}

// CustomerKindPrices é a tabela que vale para os veículos novos do cliente:
// a do afiliado que o indicou, quando a central não definiu um plano para ele
// e ele está no plano padrão (nil: o preço de sempre).
func (s *Service) CustomerKindPrices(ctx context.Context, customerID uuid.UUID) (*vehicles.KindPrices, error) {
	account, err := s.AccountPlan(ctx, customerID)
	if err != nil || account != nil {
		return nil, err
	}
	plan, err := s.customerPlan(ctx, customerID)
	if err != nil || plan.PlanName != s.catalog.PlanName {
		return nil, err
	}
	return s.ReferralPrices(ctx, customerID)
}
