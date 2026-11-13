package orders

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/vehicles"
)

// CheckoutChoice é o que a pessoa escolhe na contratação rápida pela
// landing.
type CheckoutChoice struct {
	// EquipmentMode: o rastreador incluso (sale, o padrão) ou em comodato.
	EquipmentMode string
	// FarboInstall: o rastreador incluso instalado pelos instaladores da
	// Farbo (no comodato, a instalação já é da Farbo).
	FarboInstall bool
	Vehicle      vehicles.Input
	// Affiliate: o afiliado do link (a mensalidade sai da tabela dele por
	// tipo de veículo, se ele tiver uma).
	Affiliate *uuid.UUID
	// PeriodMonths: o semestral (6) ou o anual (12), quando o link (ou o
	// site) os tem ligados; 0 ou 1, o mensal.
	PeriodMonths int
	// InstallBlocking: a instalação com o relé do bloqueio do motor, quando
	// o link oferece a escolha (no comodato ou com a instalação contratada).
	InstallBlocking bool
	// Coupon: o cupom de desconto, só na contratação direta (sem o link de
	// um representante).
	Coupon string
	// Link: o código do link de pagamento: o pedido sai com a oferta dele,
	// e as escolhas acima (menos o veículo) não contam.
	Link string
}

// CheckoutOrder monta o pedido da contratação rápida pela landing (a pessoa
// física acabou de criar a conta e paga com Pix na hora): o plano do funil
// no preço dele, já com o rastreador incluso — ou, pelo link de um afiliado
// com tabela, no preço do tipo do veículo mais a parte do rastreador nas
// primeiras mensalidades —, sem prazo (o do Insanos MC, só a central define);
// o rastreador incluso (com a instalação da Farbo se ela quiser) ou em
// comodato, com as taxas e a garantia de troca. Pelo link de um afiliado com
// a instalação dele, a instalação e a desinstalação saem no preço dele (ou
// fora da plataforma, quando o representante recebe).
func (s *Service) CheckoutOrder(ctx context.Context, in CheckoutChoice) (Order, error) {
	opts, err := s.Options(ctx)
	if err != nil {
		return Order{}, err
	}
	// Pelo link de pagamento: a oferta dele, também com a contratação pelo
	// site fechada. Conferido aqui (a pessoa vê o motivo antes de criar a
	// conta) e de novo, travado, na gravação do pedido.
	if strings.TrimSpace(in.Link) != "" {
		switch {
		case in.Affiliate != nil:
			return Order{}, ValidationError{"o link de pagamento não vale junto com o link de um representante"}
		case NormalizeCouponCode(in.Coupon) != "":
			return Order{}, ValidationError{"o link de pagamento já tem o valor dele: não vale junto com cupom"}
		}
		link, err := s.CheckoutLinkFor(ctx, in.Link)
		if err != nil {
			return Order{}, err
		}
		vehicle := in.Vehicle
		vehicle.DeviceID, vehicle.OwnerID = nil, nil
		return s.linkOrder(opts, link, vehicle), nil
	}
	if !opts.Checkout.Enabled {
		return Order{}, ValidationError{"a contratação pelo site está fechada agora: deixe o seu contato que a gente fala com você"}
	}
	plan := billing.SubscriptionInput{PlanName: s.catalog.PlanName, PriceCents: opts.Checkout.PlanCents, DueDay: s.catalog.DefaultDueDay}
	var install *InstallPrices
	modes := ""
	table := false
	if in.Affiliate != nil {
		prices, err := s.AffiliatePrices(ctx, *in.Affiliate)
		if err != nil {
			return Order{}, err
		}
		if prices != nil {
			plan.PriceCents, table = prices.For(in.Vehicle.Kind), true
		}
		if install, err = s.AffiliateInstall(ctx, *in.Affiliate); err != nil {
			return Order{}, err
		}
		if modes, err = s.AffiliateEquipment(ctx, *in.Affiliate); err != nil {
			return Order{}, err
		}
	}
	// O rastreador que o link oferece: incluso, comodato ou os dois.
	mode, err := linkEquipment(modes, in.EquipmentMode)
	if err != nil {
		return Order{}, err
	}
	vehicle := in.Vehicle
	vehicle.DeviceID, vehicle.OwnerID = nil, nil
	order := Order{Vehicle: vehicle, Plan: plan, RequireAddress: true}
	switch mode {
	case billing.EquipmentSale:
		// O rastreador incluso: no plano do site, a parte dele já está no
		// preço; pela tabela do afiliado, ela é somada nas primeiras.
		order.Terms = Terms{EquipmentMode: billing.EquipmentSale}
		order.Tracker = s.TrackerFor(opts, plan, table)
		if in.FarboInstall {
			if !opts.Install.Enabled && install == nil {
				return Order{}, ValidationError{"a instalação pelos instaladores da Farbo não está disponível agora: instale por conta própria"}
			}
			order.FarboInstall, order.InstallCents = true, opts.Install.PriceCents
		}
	case billing.EquipmentLoan:
		if !opts.Loan.Enabled && modes != EquipmentLoanOnly {
			return Order{}, ValidationError{"o comodato não está disponível agora: escolha o rastreador incluso"}
		}
		terms := Terms{EquipmentMode: billing.EquipmentLoan, InstallFeeCents: opts.Loan.InstallFeeCents,
			UninstallFeeCents: opts.Loan.UninstallFeeCents}
		// A garantia de troca: a do afiliado do link, se ele tiver uma.
		if guarantee := install.Guarantee(opts.Loan.SurchargeCents); guarantee > 0 {
			withGuarantee(&order.Plan, &terms, guarantee)
		}
		order.Terms = terms
	default:
		return Order{}, ValidationError{"escolha o rastreador: incluso ou em comodato"}
	}
	// O semestral ou o anual: o valor do período (com o rastreador incluso
	// nele, nada somado; no comodato, com a garantia de troca de cada mês).
	if in.PeriodMonths > 1 {
		if err := s.periodPlan(ctx, opts, in, &order); err != nil {
			return Order{}, err
		}
	}
	if err := withBlocking(install, in.InstallBlocking, &order); err != nil {
		return Order{}, err
	}
	install.applyInstall(&order)
	// O cupom: conferido aqui (a pessoa vê o motivo antes de criar a conta)
	// e de novo, travado, na gravação do pedido.
	if code := NormalizeCouponCode(in.Coupon); code != "" {
		if in.Affiliate != nil {
			return Order{}, ValidationError{"o cupom vale só na contratação direta pelo site, sem o link de um representante"}
		}
		if _, err := s.CheckoutCoupon(ctx, code); err != nil {
			return Order{}, err
		}
		order.Coupon = code
	}
	return order, nil
}

// withBlocking põe no pedido a instalação com o relé do bloqueio, quando o
// cliente a escolheu: só pelo link que oferece a escolha e com quem instala
// sendo a Farbo ou o representante (no rastreador incluso por conta própria,
// a escolha não vale).
func withBlocking(install *InstallPrices, blocking bool, order *Order) error {
	if !blocking || (order.Terms.EquipmentMode != billing.EquipmentLoan && !order.FarboInstall) {
		return nil
	}
	if install == nil || install.BlockingCents == nil {
		return ValidationError{"a instalação com bloqueio não está disponível por este link: escolha a instalação sem bloqueio"}
	}
	order.InstallBlocking = true
	return nil
}

// periodPlan põe no pedido o semestral ou o anual: o valor do período (o do
// link do representante, ou o do site). Com o rastreador incluso, ele já está
// no valor; no comodato, soma-se a garantia de troca de cada mês do período.
func (s *Service) periodPlan(ctx context.Context, opts *Options, in CheckoutChoice, order *Order) error {
	if !billing.ValidPeriod(in.PeriodMonths) {
		return ValidationError{"a recorrência é mensal, semestral ou anual"}
	}
	recurrence, err := s.LinkRecurrence(ctx, opts, in.Affiliate)
	if err != nil {
		return err
	}
	return applyPeriod(recurrence, in.PeriodMonths, order)
}

// applyPeriod troca o plano do pedido pelo semestral ou pelo anual: o valor
// do período (com o rastreador incluso nele; no comodato, mais a garantia de
// troca de cada mês), sem a parte do rastreador nas mensalidades.
func applyPeriod(recurrence *Recurrence, months int, order *Order) error {
	cents := recurrence.PeriodCents(months)
	if cents == 0 {
		return ValidationError{"o plano semestral e o anual não estão disponíveis agora: escolha o mensal"}
	}
	name := PeriodPlanName(months)
	if guarantee := order.Terms.GuaranteeCents; guarantee > 0 {
		cents += guarantee * months
		name += GuaranteeName
	}
	order.Plan.PlanName, order.Plan.PriceCents, order.Plan.PeriodMonths = name, cents, months
	order.Tracker = Tracker{}
	return nil
}
