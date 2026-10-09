package orders

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// AccountPlan é o plano que a central definiu para o cliente (ex.: o
// Especial Insanos MC): todo veículo novo dele sai nesse plano, pelo app ou
// pela central. O cliente não escolhe plano.
type AccountPlan struct {
	PlanName   string    `json:"planName"`
	PriceCents int       `json:"priceCents"`
	DueDay     int       `json:"dueDay"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// AccountPlan é o plano definido para o cliente (nil: nenhum).
func (s *Service) AccountPlan(ctx context.Context, customerID uuid.UUID) (*AccountPlan, error) {
	var p AccountPlan
	err := s.db.QueryRow(ctx, `
		SELECT plan_name, price_cents, due_day, updated_at FROM customer_plans WHERE customer_id = $1`, customerID).
		Scan(&p.PlanName, &p.PriceCents, &p.DueDay, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, database.MapError(err)
	}
	return &p, nil
}

// SetAccountPlan define (ou troca) o plano do cliente. Vale para os
// veículos novos; as assinaturas que já existem não mudam.
func (s *Service) SetAccountPlan(ctx context.Context, customerID uuid.UUID, in billing.SubscriptionInput, by *uuid.UUID) (*AccountPlan, error) {
	in.FirstDueDate = nil
	if err := s.billing.Validate(in); err != nil {
		return nil, err
	}
	var p AccountPlan
	err := s.db.QueryRow(ctx, `
		INSERT INTO customer_plans (customer_id, plan_name, price_cents, due_day, updated_by)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (customer_id) DO UPDATE SET plan_name = EXCLUDED.plan_name, price_cents = EXCLUDED.price_cents,
			due_day = EXCLUDED.due_day, updated_by = EXCLUDED.updated_by, updated_at = NOW()
		RETURNING plan_name, price_cents, due_day, updated_at`,
		customerID, strings.TrimSpace(in.PlanName), in.PriceCents, in.DueDay, by).
		Scan(&p.PlanName, &p.PriceCents, &p.DueDay, &p.UpdatedAt)
	if err != nil {
		return nil, database.MapError(err)
	}
	return &p, nil
}

// ClearAccountPlan volta o cliente ao padrão (o da assinatura ativa, ou o
// do catálogo).
func (s *Service) ClearAccountPlan(ctx context.Context, customerID uuid.UUID) error {
	_, err := s.db.Exec(ctx, `DELETE FROM customer_plans WHERE customer_id = $1`, customerID)
	return database.MapError(err)
}
