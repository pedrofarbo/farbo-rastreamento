package orders

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

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
