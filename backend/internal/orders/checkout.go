package orders

import (
	"context"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/vehicles"
)

// CheckoutChoice é o que a pessoa escolhe na contratação rápida pela
// landing.
type CheckoutChoice struct {
	// EquipmentMode: o rastreador comprado (sale, o padrão) ou em comodato.
	EquipmentMode string
	// Installments: o rastreador comprado parcelado sem juros (0 ou 1: à vista).
	Installments int
	// FarboInstall: o rastreador comprado instalado pelos instaladores da
	// Farbo (no comodato, a instalação já é da Farbo).
	FarboInstall bool
	Vehicle      vehicles.Input
	// Affiliate: o afiliado do link (a mensalidade sai da tabela dele por
	// tipo de veículo, se ele tiver uma).
	Affiliate *uuid.UUID
}

// CheckoutOrder monta o pedido da contratação rápida pela landing (a pessoa
// física acabou de criar a conta e paga com Pix na hora): o plano do funil
// no preço dele — ou, pelo link de um afiliado com tabela, no preço do tipo do
// veículo —, sem prazo (o do Insanos MC, só a central define); o rastreador
// comprado pelo preço do funil (à vista ou parcelado, com a instalação da
// Farbo se ela quiser) ou em comodato, com as taxas e a garantia de troca. Pelo
// link de um afiliado com a instalação dele, a instalação e a desinstalação
// saem no preço dele (ou fora da plataforma, quando o representante recebe).
func (s *Service) CheckoutOrder(ctx context.Context, in CheckoutChoice) (Order, error) {
	opts, err := s.Options(ctx)
	if err != nil {
		return Order{}, err
	}
	if !opts.Checkout.Enabled {
		return Order{}, ValidationError{"a contratação pelo site está fechada agora: deixe o seu contato que a gente fala com você"}
	}
	plan := billing.SubscriptionInput{PlanName: s.catalog.PlanName, PriceCents: opts.Checkout.PlanCents, DueDay: s.catalog.DefaultDueDay}
	var install *InstallPrices
	modes := ""
	if in.Affiliate != nil {
		table, err := s.AffiliatePrices(ctx, *in.Affiliate)
		if err != nil {
			return Order{}, err
		}
		if table != nil {
			plan.PriceCents = table.For(in.Vehicle.Kind)
		}
		if install, err = s.AffiliateInstall(ctx, *in.Affiliate); err != nil {
			return Order{}, err
		}
		if modes, err = s.AffiliateEquipment(ctx, *in.Affiliate); err != nil {
			return Order{}, err
		}
	}
	// O rastreador que o link oferece: comprado, comodato ou os dois.
	mode, err := linkEquipment(modes, in.EquipmentMode)
	if err != nil {
		return Order{}, err
	}
	vehicle := in.Vehicle
	vehicle.DeviceID, vehicle.OwnerID = nil, nil
	order := Order{Vehicle: vehicle, Plan: plan, RequireAddress: true, Installments: in.Installments}
	switch mode {
	case billing.EquipmentSale:
		order.Terms = Terms{EquipmentMode: billing.EquipmentSale}
		order.EquipmentCents = opts.Checkout.EquipmentCents
		if in.FarboInstall {
			if !opts.Install.Enabled && install == nil {
				return Order{}, ValidationError{"a instalação pelos instaladores da Farbo não está disponível agora: instale por conta própria"}
			}
			order.FarboInstall, order.InstallCents = true, opts.Install.PriceCents
		}
	case billing.EquipmentLoan:
		if !opts.Loan.Enabled && modes != EquipmentLoanOnly {
			return Order{}, ValidationError{"o comodato não está disponível agora: escolha comprar o rastreador"}
		}
		if in.Installments > 1 {
			return Order{}, ValidationError{"no comodato o rastreador não é vendido: não há o que parcelar"}
		}
		terms := Terms{EquipmentMode: billing.EquipmentLoan, InstallFeeCents: opts.Loan.InstallFeeCents,
			UninstallFeeCents: opts.Loan.UninstallFeeCents}
		// A garantia de troca: a do afiliado do link, se ele tiver uma.
		if guarantee := install.Guarantee(opts.Loan.SurchargeCents); guarantee > 0 {
			withGuarantee(&order.Plan, &terms, guarantee)
		}
		order.Terms = terms
	default:
		return Order{}, ValidationError{"escolha o rastreador: comprado ou em comodato"}
	}
	install.applyInstall(&order)
	return order, nil
}
