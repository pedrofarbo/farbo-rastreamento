package payments

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/payments/pagarme"
)

// O split da Pagar.me: quando o cliente indicado paga, a parte do afiliado
// (a comissão das faturas pagas e as acumuladas) sai no próprio pagamento,
// direto para o recebedor dele — não passa pela conta da Farbo. A cobrança
// com split vai sempre pela Pagar.me. A Farbo fica com o resto e responde
// pela tarifa, pelo chargeback e pela sobra do arredondamento.

// splitMaxShare: a parte do afiliado não passa de 80% da cobrança (a tarifa
// e o resto ficam com a Farbo); o que não coube fica para o próximo.
const splitMaxShare = 80

// SplitPlan é a parte do afiliado numa cobrança.
type SplitPlan struct {
	AffiliateID uuid.UUID
	RecipientID string
	AmountCents int
	// Items: as comissões das faturas desta cobrança (nascem quando ela é
	// paga, já pagas pelo split).
	Items []SplitItem
	// Reserved: as comissões acumuladas que ela leva (reservadas para ela).
	Reserved []uuid.UUID
}

// SplitItem é a comissão de um veículo no mês, numa cobrança com split.
type SplitItem struct {
	SubscriptionID uuid.UUID
	Month          time.Time
	AmountCents    int
}

// Splitter é o programa de afiliados visto daqui. *affiliates.Service implementa.
type Splitter interface {
	// PlanSplit monta a parte do afiliado do cliente (nil: sem split) e
	// reserva as acumuladas para a cobrança.
	PlanSplit(ctx context.Context, chargeID, customerID uuid.UUID, invoices []*billing.Invoice, maxCents int) (*SplitPlan, error)
	// CommitSplit grava a parte na cobrança já gravada.
	CommitSplit(ctx context.Context, chargeID uuid.UUID, plan *SplitPlan) error
	// ReleaseSplit solta o que a cobrança que não nasceu tinha reservado.
	ReleaseSplit(ctx context.Context, chargeID uuid.UUID) error
	// SplitPaid dá as comissões da cobrança paga como pagas pelo split.
	SplitPaid(ctx context.Context, chargeID uuid.UUID) error
}

// SetSplit liga o split: o programa de afiliados e o recebedor da Farbo na
// Pagar.me (rp_...), que fica com o resto de cada cobrança.
func (s *Service) SetSplit(splitter Splitter, farboRecipientID string) {
	s.splitter, s.farboRecipient = splitter, farboRecipientID
}

// SplitEnabled diz se o split está ligado (precisa da Pagar.me).
func (s *Service) SplitEnabled() bool {
	return s.splitter != nil && s.farboRecipient != "" && s.pagarme != nil
}

// planSplit monta a parte do afiliado da cobrança (nil: sem split). Falha
// aqui não impede a cobrança: a comissão acumula para o próximo split.
func (s *Service) planSplit(ctx context.Context, chargeID, customerID uuid.UUID, invoices []*billing.Invoice, total int) *SplitPlan {
	if !s.SplitEnabled() || customerID == uuid.Nil || len(invoices) == 0 {
		return nil
	}
	plan, err := s.splitter.PlanSplit(ctx, chargeID, customerID, invoices, total*splitMaxShare/100)
	if err != nil {
		s.log.Error("falha ao montar o split do afiliado; a comissão acumula", "charge", chargeID, "customer", customerID, "err", err)
		return nil
	}
	if plan == nil || plan.AmountCents <= 0 {
		return nil
	}
	return plan
}

// releaseSplit solta a reserva da cobrança que não nasceu.
func (s *Service) releaseSplit(ctx context.Context, chargeID uuid.UUID, plan *SplitPlan) {
	if plan == nil {
		return
	}
	if err := s.splitter.ReleaseSplit(ctx, chargeID); err != nil {
		// A conferência solta depois (a reserva sem cobrança vence).
		s.log.Warn("falha ao soltar a reserva do split", "charge", chargeID, "err", err)
	}
}

// commitSplit grava a parte do afiliado na cobrança recém-gravada.
func (s *Service) commitSplit(ctx context.Context, charge *Charge) {
	if charge.split == nil {
		return
	}
	if err := s.splitter.CommitSplit(ctx, charge.ID, charge.split); err != nil {
		s.log.Error("cobrança com split gravada sem a parte do afiliado", "charge", charge.ID, "err", err)
	}
}

// splitRules são as regras do pedido: o afiliado com a parte dele; a Farbo
// com o resto, a tarifa, o chargeback e a sobra.
func (s *Service) splitRules(plan *SplitPlan, total int) []pagarme.SplitRule {
	if plan == nil {
		return nil
	}
	return []pagarme.SplitRule{
		{Amount: plan.AmountCents, RecipientID: plan.RecipientID, Type: "flat"},
		{Amount: total - plan.AmountCents, RecipientID: s.farboRecipient, Type: "flat",
			Options: pagarme.SplitOptions{Liable: true, ChargeProcessingFee: true, ChargeRemainderFee: true}},
	}
}

// splitPaid dá a parte do afiliado como paga. A comissão já saiu na
// Pagar.me: falhar aqui só atrasa o registro (a conferência refaz).
func (s *Service) splitPaid(ctx context.Context, charge *Charge) {
	if s.splitter == nil || charge.Provider != providerPagarme {
		return
	}
	if err := s.splitter.SplitPaid(ctx, charge.ID); err != nil {
		s.log.Error("falha ao registrar as comissões pagas pelo split", "charge", charge.ID, "err", err)
	}
}
