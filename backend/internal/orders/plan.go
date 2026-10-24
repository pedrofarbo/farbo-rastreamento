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
	PlanName   string `json:"planName"`
	PriceCents int    `json:"priceCents"`
	DueDay     int    `json:"dueDay"`
	Terms
	UpdatedAt time.Time `json:"updatedAt"`
}

// Terms é o rastreador do cliente e as taxas, definidos pela central no
// plano dele: vendido (o padrão) ou em comodato, com a instalação cobrada na
// fatura da contratação e a desinstalação no encerramento.
type Terms struct {
	EquipmentMode     string `json:"equipmentMode"`
	InstallFeeCents   int    `json:"installFeeCents"`
	UninstallFeeCents int    `json:"uninstallFeeCents"`
}

// check confere os termos (vazio é venda).
func (t *Terms) check() error {
	if t.EquipmentMode == "" {
		t.EquipmentMode = billing.EquipmentSale
	}
	switch {
	case t.EquipmentMode != billing.EquipmentSale && t.EquipmentMode != billing.EquipmentLoan:
		return ValidationError{"escolha o rastreador: venda ou comodato"}
	case t.InstallFeeCents < 0 || t.InstallFeeCents > 100_000_00 || t.UninstallFeeCents < 0 || t.UninstallFeeCents > 100_000_00:
		return ValidationError{"as taxas de instalação e de desinstalação vão de R$ 0 a R$ 100.000"}
	}
	return nil
}

// PlanInput é o plano do cliente com o rastreador e as taxas.
type PlanInput struct {
	billing.SubscriptionInput
	Terms
}

// CustomerTerms é o rastreador e as taxas do cliente: os do plano dele; sem
// plano, vendido e sem taxas.
func (s *Service) CustomerTerms(ctx context.Context, customerID uuid.UUID) (Terms, error) {
	plan, err := s.AccountPlan(ctx, customerID)
	if err != nil || plan == nil {
		return Terms{EquipmentMode: billing.EquipmentSale}, err
	}
	return plan.Terms, nil
}

// AccountPlan é o plano definido para o cliente (nil: nenhum).
func (s *Service) AccountPlan(ctx context.Context, customerID uuid.UUID) (*AccountPlan, error) {
	var p AccountPlan
	err := s.db.QueryRow(ctx, `
		SELECT plan_name, price_cents, due_day, equipment_mode, install_fee_cents, uninstall_fee_cents, updated_at
		FROM customer_plans WHERE customer_id = $1`, customerID).
		Scan(&p.PlanName, &p.PriceCents, &p.DueDay, &p.EquipmentMode, &p.InstallFeeCents, &p.UninstallFeeCents, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, database.MapError(err)
	}
	return &p, nil
}

// SetAccountPlan define (ou troca) o plano do cliente. O nome e o valor
// valem para os veículos novos (as assinaturas que já existem não mudam); o
// vencimento é o da conta: num dia diferente, todas as assinaturas ativas
// vão para ele.
func (s *Service) SetAccountPlan(ctx context.Context, customerID uuid.UUID, input PlanInput, by *uuid.UUID) (*AccountPlan, error) {
	in, terms := input.SubscriptionInput, input.Terms
	in.FirstDueDate = nil
	if err := s.billing.Validate(in); err != nil {
		return nil, err
	}
	if err := terms.check(); err != nil {
		return nil, err
	}
	day, err := s.billing.AccountDueDay(ctx, customerID)
	if err != nil {
		return nil, err
	}
	if day > 0 && day != in.DueDay {
		if _, err := s.billing.ChangeAccountDueDay(ctx, customerID, in.DueDay); err != nil {
			return nil, err
		}
	}
	var p AccountPlan
	err = s.db.QueryRow(ctx, `
		INSERT INTO customer_plans (customer_id, plan_name, price_cents, due_day, updated_by,
			equipment_mode, install_fee_cents, uninstall_fee_cents)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (customer_id) DO UPDATE SET plan_name = EXCLUDED.plan_name, price_cents = EXCLUDED.price_cents,
			due_day = EXCLUDED.due_day, updated_by = EXCLUDED.updated_by, equipment_mode = EXCLUDED.equipment_mode,
			install_fee_cents = EXCLUDED.install_fee_cents, uninstall_fee_cents = EXCLUDED.uninstall_fee_cents,
			updated_at = NOW()
		RETURNING plan_name, price_cents, due_day, equipment_mode, install_fee_cents, uninstall_fee_cents, updated_at`,
		customerID, strings.TrimSpace(in.PlanName), in.PriceCents, in.DueDay, by,
		terms.EquipmentMode, terms.InstallFeeCents, terms.UninstallFeeCents).
		Scan(&p.PlanName, &p.PriceCents, &p.DueDay, &p.EquipmentMode, &p.InstallFeeCents, &p.UninstallFeeCents, &p.UpdatedAt)
	if err != nil {
		return nil, database.MapError(err)
	}
	return &p, nil
}

// SyncPlanDueDay leva o vencimento novo da conta ao plano dela, se houver.
func (s *Service) SyncPlanDueDay(ctx context.Context, customerID uuid.UUID, day int) error {
	_, err := s.db.Exec(ctx, `UPDATE customer_plans SET due_day = $2, updated_at = NOW() WHERE customer_id = $1`, customerID, day)
	return database.MapError(err)
}

// ClearAccountPlan volta o cliente ao padrão (o da assinatura ativa, ou o
// do catálogo).
func (s *Service) ClearAccountPlan(ctx context.Context, customerID uuid.UUID) error {
	_, err := s.db.Exec(ctx, `DELETE FROM customer_plans WHERE customer_id = $1`, customerID)
	return database.MapError(err)
}
