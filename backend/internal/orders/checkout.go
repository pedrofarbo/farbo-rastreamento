package orders

import (
	"context"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/vehicles"
)

// CheckoutChoice é o que a pessoa escolhe na contratação rápida pela
// landing.
type CheckoutChoice struct {
	// Insanos: integrante do Insanos MC (autodeclarado), com o plano dele.
	Insanos bool
	// EquipmentMode: o rastreador comprado (sale, o padrão) ou em comodato.
	EquipmentMode string
	// Installments: o rastreador comprado parcelado sem juros (0 ou 1: à vista).
	Installments int
	// FarboInstall: o rastreador comprado instalado pelos instaladores da
	// Farbo (no comodato, a instalação já é da Farbo).
	FarboInstall bool
	Vehicle      vehicles.Input
}

// CheckoutOrder monta o pedido da contratação rápida pela landing (a pessoa
// física acabou de criar a conta e paga com Pix na hora): o plano do funil —
// o normal ou o do Insanos MC — no preço dele, sem prazo; o rastreador
// comprado pelo preço do funil (à vista ou parcelado, com a instalação da
// Farbo se ela quiser) ou em comodato, com as taxas e a garantia de troca.
func (s *Service) CheckoutOrder(ctx context.Context, in CheckoutChoice) (Order, error) {
	opts, err := s.Options(ctx)
	if err != nil {
		return Order{}, err
	}
	if !opts.Checkout.Enabled {
		return Order{}, ValidationError{"a contratação pelo site está fechada agora: deixe o seu contato que a gente fala com você"}
	}
	plan := billing.SubscriptionInput{PlanName: s.catalog.PlanName, PriceCents: opts.Checkout.PlanCents, DueDay: s.catalog.DefaultDueDay}
	if in.Insanos && s.catalog.LaunchPromo.InsanosPlanName != "" {
		plan.PlanName, plan.PriceCents = s.catalog.LaunchPromo.InsanosPlanName, opts.Checkout.InsanosCents
	}
	vehicle := in.Vehicle
	vehicle.DeviceID, vehicle.OwnerID = nil, nil
	order := Order{Vehicle: vehicle, Plan: plan, RequireAddress: true, Installments: in.Installments}
	switch in.EquipmentMode {
	case "", billing.EquipmentSale:
		order.Terms = Terms{EquipmentMode: billing.EquipmentSale}
		order.EquipmentCents = opts.Checkout.EquipmentCents
		if in.FarboInstall {
			if !opts.Install.Enabled {
				return Order{}, ValidationError{"a instalação pelos instaladores da Farbo não está disponível agora: instale por conta própria"}
			}
			order.FarboInstall, order.InstallCents = true, opts.Install.PriceCents
		}
	case billing.EquipmentLoan:
		if !opts.Loan.Enabled {
			return Order{}, ValidationError{"o comodato não está disponível agora: escolha comprar o rastreador"}
		}
		if in.Installments > 1 {
			return Order{}, ValidationError{"no comodato o rastreador não é vendido: não há o que parcelar"}
		}
		terms := Terms{EquipmentMode: billing.EquipmentLoan, InstallFeeCents: opts.Loan.InstallFeeCents,
			UninstallFeeCents: opts.Loan.UninstallFeeCents}
		if opts.Loan.SurchargeCents > 0 {
			withGuarantee(&order.Plan, &terms, opts.Loan.SurchargeCents)
		}
		order.Terms = terms
	default:
		return Order{}, ValidationError{"escolha o rastreador: comprado ou em comodato"}
	}
	return order, nil
}
