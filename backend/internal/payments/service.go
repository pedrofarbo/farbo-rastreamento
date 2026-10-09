// Package payments cobra as faturas por Pix e dá baixa sozinho quando o
// pagamento é confirmado pelo provedor (hoje, a AbacatePay).
package payments

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/payments/abacatepay"
)

const providerAbacatePay = "abacatepay"

// PaidViaPix marca a fatura quitada por Pix confirmado no provedor.
const PaidViaPix = "PIX"

var (
	// ErrDisabled: nenhuma chave de provedor configurada.
	ErrDisabled = errors.New("pagamento online não configurado")
	// ErrInvoiceNotOpen: só fatura em aberto recebe Pix.
	ErrInvoiceNotOpen = errors.New("esta fatura não está em aberto")
	// ErrNotDevMode: simular pagamento só vale com chave de testes.
	ErrNotDevMode = errors.New("simulação de pagamento só existe no ambiente de testes")
	// ErrNotRefundable: só Pix pago (e fora de disputa) pode ser estornado.
	ErrNotRefundable = errors.New("só um pagamento confirmado, e fora de disputa, pode ser estornado")
	// ErrRefundPending: o estorno já foi pedido e ainda está em andamento.
	ErrRefundPending = errors.New("o estorno deste pagamento já foi pedido e está em andamento")
)

const (
	// reuseMargin: um Pix que expira em menos que isso não é reaproveitado —
	// o cliente não teria tempo de pagar.
	reuseMargin = 5 * time.Minute
	// minCheckInterval limita as consultas do mesmo Pix quando a tela fica
	// perguntando o status.
	minCheckInterval = 3 * time.Second
	// syncIdle: a varredura periódica reconsulta um Pix pendente depois disso.
	syncIdle = 45 * time.Second
	// webhookRechecks e webhookRecheckDelay: quanto o webhook espera a API
	// confirmar um pagamento anunciado (bem abaixo dos 30 s de limite dela).
	webhookRechecks     = 3
	webhookRecheckDelay = time.Second
)

// Gateway é o que o serviço usa do provedor. *abacatepay.Client implementa.
type Gateway interface {
	CreatePix(ctx context.Context, req abacatepay.PixRequest) (*abacatepay.Pix, error)
	CheckPix(ctx context.Context, id string) (*abacatepay.PixStatus, error)
	SimulatePayment(ctx context.Context, id string) (*abacatepay.Pix, error)
	RefundPix(ctx context.Context, id, reason string) (*abacatepay.Refund, error)
}

// Invoices é o que o serviço usa da cobrança. *billing.Service implementa.
type Invoices interface {
	GetInvoice(ctx context.Context, id uuid.UUID) (*billing.Invoice, error)
	SettleInvoice(ctx context.Context, id uuid.UUID, via string, chargeID uuid.UUID) (*billing.Invoice, error)
	ReopenInvoice(ctx context.Context, id uuid.UUID) (*billing.Invoice, error)
}

// Auditor grava a trilha. *audit.Service implementa.
type Auditor interface {
	Record(ctx context.Context, e *audit.Entry)
}

// Payer são os dados do pagador. Só vão para o provedor se o CPF/CNPJ for
// válido e houver celular: a AbacatePay exige todos os campos juntos.
type Payer struct {
	Name     string
	Email    string
	Document string
	Phone    string
}

type Service struct {
	repo     *Repository
	invoices Invoices
	gateway  Gateway
	audit    Auditor
	cfg      config.Payments
	log      *slog.Logger
}

func NewService(repo *Repository, invoices Invoices, gateway Gateway, auditor Auditor, cfg config.Payments, log *slog.Logger) *Service {
	return &Service{
		repo: repo, invoices: invoices, gateway: gateway, audit: auditor, cfg: cfg,
		log: log.With("component", "payments"),
	}
}

// Enabled diz se há provedor configurado.
func (s *Service) Enabled() bool { return s != nil && s.gateway != nil && s.cfg.Enabled() }

// PixForInvoice devolve um Pix para pagar a fatura: reaproveita o pendente
// que ainda vale ou gera um novo no provedor.
func (s *Service) PixForInvoice(ctx context.Context, invoiceID uuid.UUID, payer *Payer) (*Charge, error) {
	if !s.Enabled() {
		return nil, ErrDisabled
	}
	inv, err := s.invoices.GetInvoice(ctx, invoiceID)
	if err != nil {
		return nil, err
	}
	if inv.Status != billing.InvoiceOpen {
		return nil, ErrInvoiceNotOpen
	}

	if existing, err := s.repo.Reusable(ctx, invoiceID, inv.AmountCents, time.Now().Add(reuseMargin)); err == nil {
		existing.InvoiceStatus = inv.Status
		return existing, nil
	}

	chargeID := uuid.New()
	pix, err := s.gateway.CreatePix(ctx, abacatepay.PixRequest{
		AmountCents: inv.AmountCents,
		Description: "Farbo Rastreadores — " + inv.Description,
		ExpiresIn:   s.cfg.PixExpiresIn,
		ExternalID:  chargeID.String(),
		Customer:    customerFor(payer),
		Metadata: map[string]string{
			"invoiceId":  inv.ID.String(),
			"customerId": inv.CustomerID.String(),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("gerando Pix: %w", err)
	}

	charge := &Charge{
		ID: chargeID, InvoiceID: inv.ID, Provider: providerAbacatePay,
		ProviderChargeID: pix.ID, AmountCents: pix.Amount, Status: pix.Status,
		BrCode: pix.BrCode, QRCodeImage: pix.BrCodeBase64, DevMode: pix.DevMode,
		ExpiresAt: pix.ExpiresAt, InvoiceStatus: inv.Status, PlatformFeeCents: pix.PlatformFee,
	}
	if err := s.repo.Insert(ctx, charge); err != nil {
		return nil, err
	}
	s.log.Info("Pix gerado", "invoice", inv.ID, "charge", charge.ID, "provider_charge", pix.ID,
		"amount_cents", pix.Amount, "dev_mode", pix.DevMode)
	return charge, nil
}

// Charge devolve a cobrança com o status atual da fatura.
func (s *Service) Charge(ctx context.Context, id uuid.UUID) (*Charge, error) {
	charge, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.withInvoiceStatus(ctx, charge), nil
}

// Refresh reconsulta o Pix no provedor e aplica o que mudou. Sem `force`,
// respeita um intervalo mínimo entre consultas e não reconsulta Pix que já
// chegou ao fim (pago, expirado, cancelado).
func (s *Service) Refresh(ctx context.Context, id uuid.UUID, force bool) (*Charge, error) {
	charge, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if !s.Enabled() {
		return s.withInvoiceStatus(ctx, charge), nil
	}
	if !force {
		settled := charge.Status != abacatepay.StatusPending && charge.Status != abacatepay.StatusUnderDispute
		recent := charge.CheckedAt != nil && time.Since(*charge.CheckedAt) < minCheckInterval
		if settled || recent {
			return s.withInvoiceStatus(ctx, charge), nil
		}
	}

	status, err := s.gateway.CheckPix(ctx, charge.ProviderChargeID)
	if err != nil {
		return nil, fmt.Errorf("consultando Pix: %w", err)
	}
	if err := s.apply(ctx, charge, status.Status); err != nil {
		return nil, err
	}
	return s.Charge(ctx, id)
}

// Simulate paga o Pix de mentira. Só existe com chave de testes, para
// exercitar o fluxo inteiro sem dinheiro de verdade.
func (s *Service) Simulate(ctx context.Context, id uuid.UUID) (*Charge, error) {
	charge, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if !charge.DevMode {
		return nil, ErrNotDevMode
	}
	if _, err := s.gateway.SimulatePayment(ctx, charge.ProviderChargeID); err != nil {
		return nil, fmt.Errorf("simulando pagamento: %w", err)
	}
	return s.awaitChange(ctx, id, abacatepay.StatusPending)
}

// RefundCharge devolve ao pagador o valor de um Pix pago. A AbacatePay só
// faz estorno integral. Se foi este Pix que quitou a fatura, ela volta a
// ficar em aberto quando o estorno conclui (na hora em testes; em produção,
// com o webhook ou a consulta periódica).
func (s *Service) RefundCharge(ctx context.Context, id uuid.UUID, reason string) (*Charge, error) {
	if !s.Enabled() {
		return nil, ErrDisabled
	}
	// Confere o status atual no provedor antes de decidir.
	charge, err := s.Refresh(ctx, id, true)
	if err != nil {
		return nil, err
	}
	if charge.RefundRequestedAt != nil && charge.Status != abacatepay.StatusRefunded {
		return nil, ErrRefundPending
	}
	if charge.Status != abacatepay.StatusPaid {
		return nil, ErrNotRefundable
	}

	reason = strings.TrimSpace(reason)
	refund, err := s.gateway.RefundPix(ctx, charge.ProviderChargeID, reason)
	if err != nil {
		return nil, fmt.Errorf("pedindo estorno: %w", err)
	}
	if err := s.repo.MarkRefundRequested(ctx, charge.ID, refund.RefundID(), reason); err != nil {
		// O estorno já foi aceito lá; só o registro falhou. A consulta
		// periódica ainda vai ver o REFUNDED e aplicar.
		s.log.Error("estorno aceito, mas não registrado", "charge", charge.ID, "refund", refund.RefundID(), "err", err)
	}
	s.log.Info("estorno pedido", "charge", charge.ID, "refund", refund.RefundID(), "status", refund.Status)

	// Em testes o estorno conclui na hora (a consulta pode levar um instante
	// para refletir); em produção é assíncrono e isto ainda deve voltar PAID,
	// com "estorno em andamento" até o webhook ou a consulta periódica.
	return s.awaitChange(ctx, charge.ID, abacatepay.StatusPaid)
}

// awaitChange reconsulta o Pix até o status sair de `from`, por poucos
// segundos. A API da AbacatePay pode levar um instante para refletir uma
// operação que ela mesma acabou de confirmar (pagamento, estorno).
func (s *Service) awaitChange(ctx context.Context, id uuid.UUID, from string) (*Charge, error) {
	charge, err := s.Refresh(ctx, id, true)
	for attempt := 0; err == nil && charge.Status == from && attempt < webhookRechecks; attempt++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(webhookRecheckDelay):
		}
		charge, err = s.Refresh(ctx, id, true)
	}
	return charge, err
}

// Payments lista os Pix que movimentaram dinheiro para um cliente.
func (s *Service) Payments(ctx context.Context, customerID uuid.UUID) ([]*Payment, error) {
	return s.repo.PaymentsForCustomer(ctx, customerID)
}

// SyncPending reconsulta os Pix pendentes. É o que dá baixa quando o webhook
// não chega — em desenvolvimento, por exemplo, sem endereço público.
func (s *Service) SyncPending(ctx context.Context) {
	if !s.Enabled() {
		return
	}
	pending, err := s.repo.DuePending(ctx, syncIdle, 100)
	if err != nil {
		s.log.Warn("falha ao listar Pix pendentes", "err", err)
		return
	}
	for _, charge := range pending {
		if _, err := s.Refresh(ctx, charge.ID, true); err != nil {
			s.log.Warn("falha ao reconsultar Pix", "charge", charge.ID, "err", err)
		}
	}
}

// HandleWebhook reconsulta no provedor as cobranças citadas no evento. O
// conteúdo do webhook só indica o que olhar; quem decide é a API.
func (s *Service) HandleWebhook(ctx context.Context, ev *abacatepay.Event) error {
	charges, err := s.repo.ByProviderIDs(ctx, ev.ChargeIDs())
	if err != nil {
		return err
	}
	if len(charges) == 0 {
		s.log.Info("webhook sem cobrança nossa; ignorado", "event", ev.Event, "id", ev.ID)
		return nil
	}
	// A API da AbacatePay pode levar um instante para refletir o pagamento
	// que o próprio webhook anuncia. Para "completed", insiste um pouco; se
	// continuar pendente, devolve erro para ela reenviar o evento mais tarde
	// (a consulta periódica também pega, mas bem depois).
	expectsPaid := strings.HasSuffix(ev.Event, ".completed")
	var failures []string
	for _, charge := range charges {
		var updated *Charge
		var err error
		if expectsPaid {
			updated, err = s.awaitChange(ctx, charge.ID, abacatepay.StatusPending)
		} else {
			updated, err = s.Refresh(ctx, charge.ID, true)
		}
		switch {
		case err != nil:
			failures = append(failures, err.Error())
		case expectsPaid && updated.Status == abacatepay.StatusPending:
			failures = append(failures, "Pix "+charge.ProviderChargeID+" ainda pendente na API")
		}
	}
	if len(failures) > 0 {
		return errors.New(strings.Join(failures, "; "))
	}
	return nil
}

// WebhookSeen e RecordWebhook garantem processar cada evento uma vez.
func (s *Service) WebhookSeen(ctx context.Context, id string) (bool, error) {
	return s.repo.WebhookSeen(ctx, id)
}

func (s *Service) RecordWebhook(ctx context.Context, ev *abacatepay.Event) error {
	return s.repo.RecordWebhook(ctx, ev.ID, providerAbacatePay, ev.Event)
}

// apply grava o novo status e leva a consequência para a fatura.
func (s *Service) apply(ctx context.Context, charge *Charge, status string) error {
	if status == charge.Status {
		return s.repo.TouchChecked(ctx, charge.ID)
	}
	if err := s.repo.SetStatus(ctx, charge.ID, status, status == abacatepay.StatusPaid); err != nil {
		return err
	}
	s.log.Info("status do Pix mudou", "charge", charge.ID, "from", charge.Status, "to", status)

	meta := map[string]any{
		"invoiceId": charge.InvoiceID, "chargeId": charge.ID,
		"providerChargeId": charge.ProviderChargeID, "amountCents": charge.AmountCents,
		"devMode": charge.DevMode,
	}

	switch status {
	case abacatepay.StatusPaid:
		// Um Pix antigo, de antes da central mudar o valor da fatura: o
		// dinheiro entrou, mas não quita sozinho — alguém confere.
		if current, err := s.invoices.GetInvoice(ctx, charge.InvoiceID); err != nil {
			return err
		} else if current.Status == billing.InvoiceOpen && current.AmountCents != charge.AmountCents {
			s.log.Warn("Pix pago com valor diferente do da fatura", "invoice", charge.InvoiceID, "charge", charge.ID,
				"charge_cents", charge.AmountCents, "invoice_cents", current.AmountCents)
			meta["invoiceAmountCents"], meta["reason"] = current.AmountCents, "valor diferente do da fatura"
			s.record(ctx, audit.ActionPaymentUnmatched, "REVIEW", meta)
			return nil
		}
		inv, err := s.invoices.SettleInvoice(ctx, charge.InvoiceID, PaidViaPix, charge.ID)
		switch {
		case err == nil:
			s.record(ctx, audit.ActionInvoicePaid, "OK", withVia(meta, inv))
		case errors.Is(err, billing.ErrNotOpen):
			// Pagou uma fatura que a central já tinha quitado ou cancelado:
			// o dinheiro entrou, e alguém precisa decidir o que fazer com ele.
			s.log.Warn("Pix pago para fatura que não estava em aberto", "invoice", charge.InvoiceID, "charge", charge.ID)
			s.record(ctx, audit.ActionPaymentUnmatched, "REVIEW", meta)
		default:
			return err
		}

	case abacatepay.StatusRefunded:
		inv, err := s.invoices.GetInvoice(ctx, charge.InvoiceID)
		if err != nil {
			return err
		}
		// Só reabre se foi este Pix que quitou a fatura: estornar um
		// pagamento em duplicidade não pode desfazer a quitação do outro.
		reopened := false
		if inv.Status == billing.InvoicePaid && inv.PaidChargeID != nil && *inv.PaidChargeID == charge.ID {
			if _, err := s.invoices.ReopenInvoice(ctx, charge.InvoiceID); err != nil && !errors.Is(err, billing.ErrNotOpen) {
				return err
			}
			reopened = true
		}
		meta["invoiceReopened"] = reopened
		s.record(ctx, audit.ActionPaymentRefunded, "OK", meta)

	case abacatepay.StatusUnderDispute:
		s.record(ctx, audit.ActionPaymentDisputed, "REVIEW", meta)
	}
	return nil
}

func withVia(meta map[string]any, inv *billing.Invoice) map[string]any {
	meta["via"] = PaidViaPix
	if inv != nil {
		meta["customerId"] = inv.CustomerID
	}
	return meta
}

func (s *Service) record(ctx context.Context, action, result string, meta map[string]any) {
	if s.audit == nil {
		return
	}
	s.audit.Record(ctx, &audit.Entry{Action: action, Result: result, Metadata: meta})
}

func (s *Service) withInvoiceStatus(ctx context.Context, charge *Charge) *Charge {
	if inv, err := s.invoices.GetInvoice(ctx, charge.InvoiceID); err == nil {
		charge.InvoiceStatus = inv.Status
	}
	return charge
}

// customerFor manda o pagador ao provedor só quando todos os dados que ele
// exige estão presentes e válidos; senão o Pix sai sem identificação.
func customerFor(p *Payer) *abacatepay.Customer {
	if p == nil || strings.TrimSpace(p.Name) == "" || !strings.Contains(p.Email, "@") {
		return nil
	}
	phone := onlyDigits(p.Phone)
	if !validTaxID(p.Document) || len(phone) < 10 || len(phone) > 13 {
		return nil
	}
	return &abacatepay.Customer{
		Name: strings.TrimSpace(p.Name), Email: strings.TrimSpace(p.Email),
		TaxID: onlyDigits(p.Document), Cellphone: phone,
	}
}
