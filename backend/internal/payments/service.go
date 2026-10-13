// Package payments cobra as faturas por Pix ou boleto (que traz um Pix junto)
// e dá baixa sozinho quando o pagamento é confirmado pelo provedor (hoje, a
// AbacatePay).
package payments

import (
	"context"
	"crypto/cipher"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/payments/abacatepay"
)

const providerAbacatePay = "abacatepay"

// Os métodos de cobrança.
const (
	MethodPix    = "PIX"
	MethodBoleto = "BOLETO"
	MethodCard   = "CARD"
)

// Como a fatura foi quitada: por Pix, por boleto (pelo código de barras ou
// pelo Pix que vem nele) ou no cartão, confirmados no provedor.
const (
	PaidViaPix    = "PIX"
	PaidViaBoleto = "BOLETO"
	PaidViaCard   = "CARD"
)

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
	// ErrNoInvoices: o Pix de várias faturas precisa de pelo menos uma.
	ErrNoInvoices = errors.New("escolha as faturas para pagar")
	// ErrPayerIncomplete: o boleto exige o nome e o CPF/CNPJ do pagador.
	ErrPayerIncomplete = errors.New("para gerar o boleto, o cadastro precisa do nome e de um CPF ou CNPJ válido")
	// ErrBoletoNoRefund: a AbacatePay não estorna boleto pela API.
	ErrBoletoNoRefund = errors.New("a AbacatePay não estorna boleto: devolva o valor ao cliente por fora (por exemplo, por Pix)")
	// ErrBoletoNoSimulation: o sandbox da AbacatePay só simula o pagamento de Pix.
	ErrBoletoNoSimulation = errors.New("o ambiente de testes da AbacatePay não simula o pagamento de boleto")
	// ErrUnknownMethod: só Pix ou boleto.
	ErrUnknownMethod = errors.New("forma de pagamento desconhecida")
)

// MaxGroupInvoices limita o Pix de várias faturas: folga para a fatura do mês
// das maiores frotas (uma por veículo), com o pedido ainda pequeno.
const MaxGroupInvoices = 5000

const (
	// reuseMargin: um Pix que expira em menos que isso não é reaproveitado —
	// o cliente não teria tempo de pagar. O boleto pede mais folga: vence no
	// fim do dia e o banco precisa estar aberto.
	reuseMargin       = 5 * time.Minute
	boletoReuseMargin = 6 * time.Hour
	// minCheckInterval limita as consultas do mesmo Pix quando a tela fica
	// perguntando o status.
	minCheckInterval = 3 * time.Second
	// syncIdle: a varredura periódica reconsulta um Pix pendente depois disso;
	// boletoSyncIdle, um boleto (vale dias, e o webhook avisa antes).
	syncIdle       = 45 * time.Second
	boletoSyncIdle = 10 * time.Minute
	// webhookRechecks e webhookRecheckDelay: quanto o webhook espera a API
	// confirmar um pagamento anunciado (bem abaixo dos 30 s de limite dela).
	webhookRechecks     = 3
	webhookRecheckDelay = time.Second
	// boletoClearing: o boleto pago no último dia compensa em até 3 dias
	// úteis; até lá, o vencido ainda é consultado.
	boletoClearing = 6 * 24 * time.Hour
)

// Gateway é o que o serviço usa do provedor. *abacatepay.Client implementa.
type Gateway interface {
	CreatePix(ctx context.Context, req abacatepay.PixRequest) (*abacatepay.Pix, error)
	CheckPix(ctx context.Context, id string) (*abacatepay.PixStatus, error)
	SimulatePayment(ctx context.Context, id string) (*abacatepay.Pix, error)
	RefundPix(ctx context.Context, id, reason string) (*abacatepay.Refund, error)
	CreateBoleto(ctx context.Context, req abacatepay.BoletoRequest) (*abacatepay.Boleto, error)
	GetCharge(ctx context.Context, id string) (*abacatepay.PixStatus, error)
	CreateCard(ctx context.Context, req abacatepay.CardRequest) (*abacatepay.Card, error)
	FindCard(ctx context.Context, since time.Time, key, value string) (*abacatepay.Card, error)
}

// Invoices é o que o serviço usa da cobrança. *billing.Service implementa.
type Invoices interface {
	GetInvoice(ctx context.Context, id uuid.UUID) (*billing.Invoice, error)
	GetInvoices(ctx context.Context, ids []uuid.UUID) ([]*billing.Invoice, error)
	SettleInvoice(ctx context.Context, id uuid.UUID, via string, chargeID uuid.UUID) (*billing.Invoice, error)
	SettleInvoices(ctx context.Context, ids []uuid.UUID, via string, chargeID uuid.UUID) ([]uuid.UUID, error)
	ReopenInvoice(ctx context.Context, id uuid.UUID) (*billing.Invoice, error)
	Today() billing.Date
}

// Auditor grava a trilha. *audit.Service implementa.
type Auditor interface {
	Record(ctx context.Context, e *audit.Entry)
}

// Payer são os dados do pagador. No Pix, só vão para o provedor se o CPF/CNPJ
// for válido e houver celular (a AbacatePay exige todos os campos juntos); o
// boleto exige o nome e o CPF/CNPJ.
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
	// O cartão salvo (cifrado) e o débito automático (ver card.go).
	cardKey cipher.AEAD
	debit   DebitConfig
}

func NewService(repo *Repository, invoices Invoices, gateway Gateway, auditor Auditor, cfg config.Payments, log *slog.Logger) *Service {
	return &Service{
		repo: repo, invoices: invoices, gateway: gateway, audit: auditor, cfg: cfg,
		log: log.With("component", "payments"),
	}
}

// Enabled diz se há provedor configurado.
func (s *Service) Enabled() bool { return s != nil && s.gateway != nil && s.cfg.Enabled() }

// PixForInvoice devolve um Pix para pagar a fatura (ChargeForInvoice).
func (s *Service) PixForInvoice(ctx context.Context, invoiceID uuid.UUID, payer *Payer) (*Charge, error) {
	return s.ChargeForInvoice(ctx, invoiceID, MethodPix, payer)
}

// PixForInvoices devolve um Pix só para várias faturas (ChargeForInvoices).
func (s *Service) PixForInvoices(ctx context.Context, customerID uuid.UUID, invoiceIDs []uuid.UUID, payer *Payer) (*Charge, error) {
	return s.ChargeForInvoices(ctx, customerID, invoiceIDs, MethodPix, payer)
}

// ChargeForInvoice devolve o Pix ou o boleto para pagar a fatura: reaproveita
// o pendente que ainda vale ou gera um novo no provedor.
func (s *Service) ChargeForInvoice(ctx context.Context, invoiceID uuid.UUID, method string, payer *Payer) (*Charge, error) {
	if !s.Enabled() {
		return nil, ErrDisabled
	}
	if method != MethodPix && method != MethodBoleto {
		return nil, ErrUnknownMethod
	}
	inv, err := s.invoices.GetInvoice(ctx, invoiceID)
	if err != nil {
		return nil, err
	}
	if inv.Status != billing.InvoiceOpen {
		return nil, ErrInvoiceNotOpen
	}

	if existing, err := s.repo.Reusable(ctx, invoiceID, method, inv.AmountCents, time.Now().Add(reuseMarginFor(method))); err == nil {
		existing.InvoiceStatus = inv.Status
		return existing, nil
	}

	charge, err := s.create(ctx, method, chargeRequest{
		AmountCents: inv.AmountCents,
		Description: "Farbo Rastreadores — " + inv.Description,
		DueDate:     inv.DueDate,
		Payer:       payer,
		Metadata: map[string]string{
			"invoiceId":  inv.ID.String(),
			"customerId": inv.CustomerID.String(),
		},
	})
	if err != nil {
		return nil, err
	}
	charge.InvoiceID, charge.InvoiceStatus = inv.ID, inv.Status
	if err := s.repo.Insert(ctx, charge); err != nil {
		return nil, err
	}
	s.log.Info("cobrança gerada", "method", method, "invoice", inv.ID, "charge", charge.ID,
		"provider_charge", charge.ProviderChargeID, "amount_cents", charge.AmountCents, "dev_mode", charge.DevMode)
	return charge, nil
}

// ChargeForInvoices devolve um Pix ou um boleto só para pagar várias faturas
// do mesmo cliente de uma vez — a fatura do mês de uma frota, uma por
// veículo, ou tudo o que está vencido. Reaproveita o pendente que paga
// exatamente as mesmas pelo total de agora; com uma fatura, é o
// ChargeForInvoice.
func (s *Service) ChargeForInvoices(ctx context.Context, customerID uuid.UUID, invoiceIDs []uuid.UUID, method string, payer *Payer) (*Charge, error) {
	if !s.Enabled() {
		return nil, ErrDisabled
	}
	if method != MethodPix && method != MethodBoleto {
		return nil, ErrUnknownMethod
	}
	invoices, total, err := s.openInvoices(ctx, customerID, invoiceIDs)
	if err != nil {
		return nil, err
	}
	if len(invoices) == 1 {
		return s.ChargeForInvoice(ctx, invoices[0].ID, method, payer)
	}
	ids := make([]uuid.UUID, len(invoices))
	items := make([]ChargeInvoice, len(invoices))
	for i, inv := range invoices {
		ids[i] = inv.ID
		items[i] = ChargeInvoice{InvoiceID: inv.ID, AmountCents: inv.AmountCents}
	}

	if existing, err := s.repo.ReusableGroup(ctx, ids, method, total, time.Now().Add(reuseMarginFor(method))); err == nil {
		existing.InvoiceStatus, existing.InvoiceIDs = billing.InvoiceOpen, ids
		return existing, nil
	}

	description := fmt.Sprintf("Farbo Rastreadores — %d faturas", len(invoices))
	if first, last := invoices[0].DueDate, invoices[len(invoices)-1].DueDate; first.Equal(last.Time) {
		description += ", vencimento " + first.Format("02/01/2006")
	}
	charge, err := s.create(ctx, method, chargeRequest{
		AmountCents: total,
		Description: description,
		DueDate:     invoices[0].DueDate,
		Payer:       payer,
		Metadata: map[string]string{
			"invoiceId":  ids[0].String(),
			"invoices":   fmt.Sprint(len(ids)),
			"customerId": customerID.String(),
		},
	})
	if err != nil {
		return nil, err
	}
	charge.InvoiceID, charge.InvoiceStatus, charge.InvoiceIDs = ids[0], billing.InvoiceOpen, ids
	if err := s.repo.InsertGroup(ctx, charge, items); err != nil {
		return nil, err
	}
	s.log.Info("cobrança de várias faturas gerada", "method", method, "customer", customerID, "invoices", len(ids),
		"charge", charge.ID, "provider_charge", charge.ProviderChargeID, "amount_cents", charge.AmountCents,
		"dev_mode", charge.DevMode)
	return charge, nil
}

// chargeRequest é a cobrança a gerar no provedor. DueDate é o vencimento (da
// primeira fatura a vencer), para o boleto.
type chargeRequest struct {
	AmountCents int
	Description string
	DueDate     billing.Date
	Payer       *Payer
	Metadata    map[string]string
}

// create gera o Pix ou o boleto no provedor. O id da cobrança vai como
// externalId.
func (s *Service) create(ctx context.Context, method string, req chargeRequest) (*Charge, error) {
	id := uuid.New()
	if method == MethodBoleto {
		customer, ok := boletoCustomerFor(req.Payer)
		if !ok {
			return nil, ErrPayerIncomplete
		}
		boleto, err := s.gateway.CreateBoleto(ctx, abacatepay.BoletoRequest{
			AmountCents: req.AmountCents,
			Description: req.Description,
			DueDate:     s.boletoDueDate(req.DueDate),
			ExternalID:  id.String(),
			Customer:    customer,
			Metadata:    req.Metadata,
		})
		if err != nil {
			return nil, fmt.Errorf("gerando boleto: %w", err)
		}
		return &Charge{
			ID: id, Provider: providerAbacatePay, Method: MethodBoleto,
			ProviderChargeID: boleto.ID, AmountCents: boleto.Amount, Status: boleto.Status,
			BrCode: boleto.BrCode, QRCodeImage: boleto.BrCodeBase64, DevMode: boleto.DevMode,
			ExpiresAt: boleto.ExpiresAt, PlatformFeeCents: boleto.PlatformFee,
			BoletoLine: boleto.Line, BoletoBarcode: boleto.BarCode, BoletoURL: boleto.URL,
		}, nil
	}
	pix, err := s.gateway.CreatePix(ctx, abacatepay.PixRequest{
		AmountCents: req.AmountCents,
		Description: req.Description,
		ExpiresIn:   s.cfg.PixExpiresIn,
		ExternalID:  id.String(),
		Customer:    customerFor(req.Payer),
		Metadata:    req.Metadata,
	})
	if err != nil {
		return nil, fmt.Errorf("gerando Pix: %w", err)
	}
	return &Charge{
		ID: id, Provider: providerAbacatePay, Method: MethodPix,
		ProviderChargeID: pix.ID, AmountCents: pix.Amount, Status: pix.Status,
		BrCode: pix.BrCode, QRCodeImage: pix.BrCodeBase64, DevMode: pix.DevMode,
		ExpiresAt: pix.ExpiresAt, PlatformFeeCents: pix.PlatformFee,
	}, nil
}

// boletoDueDate: o vencimento da fatura, se ainda vem pela frente; vencida
// (ou vencendo hoje, quando o boleto já poderia nascer vencido à noite),
// vazio — a AbacatePay dá 3 dias úteis.
func (s *Service) boletoDueDate(due billing.Date) string {
	if due.IsZero() || !s.invoices.Today().Before(due) {
		return ""
	}
	return due.Format(time.DateOnly)
}

func reuseMarginFor(method string) time.Duration {
	if method == MethodBoleto {
		return boletoReuseMargin
	}
	return reuseMargin
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
	// Sem resposta do provedor ainda (o cartão): quem resolve é ReconcileCards.
	if !s.Enabled() || charge.Status == StatusCreating {
		return s.withInvoiceStatus(ctx, charge), nil
	}
	if !force {
		settled := charge.Status != abacatepay.StatusPending && charge.Status != abacatepay.StatusUnderDispute
		recent := charge.CheckedAt != nil && time.Since(*charge.CheckedAt) < minCheckInterval
		if settled || recent {
			return s.withInvoiceStatus(ctx, charge), nil
		}
	}

	// O /transparents/check é só para Pix; o boleto e o cartão se consultam
	// pelo get.
	var status *abacatepay.PixStatus
	if charge.Method != MethodPix {
		status, err = s.gateway.GetCharge(ctx, charge.ProviderChargeID)
	} else {
		status, err = s.gateway.CheckPix(ctx, charge.ProviderChargeID)
	}
	if err != nil {
		return nil, fmt.Errorf("consultando a cobrança: %w", err)
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
	if charge.Method == MethodBoleto {
		return nil, ErrBoletoNoSimulation
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
	if current, err := s.repo.Get(ctx, id); err != nil {
		return nil, err
	} else if current.Method == MethodBoleto {
		return nil, ErrBoletoNoRefund
	}
	// Confere o status atual no provedor antes de decidir.
	charge, err := s.Refresh(ctx, id, true)
	if err != nil {
		return nil, err
	}
	if charge.RefundRequestedAt != nil && charge.Status != abacatepay.StatusRefunded {
		return nil, ErrRefundPending
	}
	if !paidStatus(charge.Method, charge.Status) {
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
	// para refletir); em produção é assíncrono e isto ainda deve voltar pago,
	// com "estorno em andamento" até o webhook ou a consulta periódica.
	return s.awaitChange(ctx, charge.ID, charge.Status)
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
	s.ReconcileCards(ctx)
	pending, err := s.repo.DuePending(ctx, syncIdle, boletoSyncIdle, 100)
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
			failures = append(failures, "cobrança "+charge.ProviderChargeID+" ainda pendente na API")
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

// StatusCreating é a cobrança de cartão gravada antes de ir ao provedor, à
// espera da resposta dele (ver CardForInvoices e ReconcileCards).
const StatusCreating = "CREATING"

// paidStatus diz se o provedor confirmou o pagamento: o Pix e o boleto quando
// pagos; o cartão já na aprovação (o PAID dele é o repasse da adquirente,
// dias depois).
func paidStatus(method, status string) bool {
	return status == abacatepay.StatusPaid || (method == MethodCard && status == abacatepay.StatusApproved)
}

// apply grava o novo status e leva a consequência para a fatura.
func (s *Service) apply(ctx context.Context, charge *Charge, status string) error {
	if status == charge.Status {
		return s.repo.TouchChecked(ctx, charge.ID)
	}
	wasPaid, nowPaid := paidStatus(charge.Method, charge.Status), paidStatus(charge.Method, status)
	if err := s.repo.SetStatus(ctx, charge.ID, status, nowPaid); err != nil {
		return err
	}
	s.log.Info("status da cobrança mudou", "method", charge.Method, "charge", charge.ID, "from", charge.Status, "to", status)
	if wasPaid && nowPaid {
		// O cartão aprovado que a adquirente repassou: a fatura já quitou.
		return nil
	}
	if nowPaid {
		status = abacatepay.StatusPaid
	}

	meta := map[string]any{
		"invoiceId": charge.InvoiceID, "chargeId": charge.ID,
		"providerChargeId": charge.ProviderChargeID, "amountCents": charge.AmountCents,
		"devMode": charge.DevMode, "method": charge.Method,
	}

	group, err := s.repo.ChargeInvoices(ctx, charge.ID)
	if err != nil {
		return err
	}
	if len(group) > 1 {
		meta["invoiceIds"] = group
		return s.applyGroup(ctx, charge, status, group, meta)
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
		inv, err := s.invoices.SettleInvoice(ctx, charge.InvoiceID, paidVia(charge), charge.ID)
		switch {
		case err == nil:
			s.record(ctx, audit.ActionInvoicePaid, "OK", withVia(meta, charge, inv))
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

// applyGroup aplica o status do Pix de várias faturas. Pago, quita todas —
// se nenhuma mudou desde o Pix (todas em aberto, o mesmo total); senão o
// dinheiro entrou, mas nada é quitado sozinho: alguém confere. Estornado,
// reabre as que este Pix quitou.
func (s *Service) applyGroup(ctx context.Context, charge *Charge, status string, ids []uuid.UUID, meta map[string]any) error {
	invoices, err := s.invoices.GetInvoices(ctx, ids)
	if err != nil {
		return err
	}
	switch status {
	case abacatepay.StatusPaid:
		total, open := 0, len(invoices) == len(ids)
		for _, inv := range invoices {
			total += inv.AmountCents
			open = open && inv.Status == billing.InvoiceOpen
		}
		if !open || total != charge.AmountCents {
			s.log.Warn("Pix de várias faturas pago, mas as faturas mudaram", "charge", charge.ID,
				"charge_cents", charge.AmountCents, "invoices_cents", total, "all_open", open)
			meta["invoicesAmountCents"], meta["reason"] = total, "as faturas mudaram depois do Pix"
			s.record(ctx, audit.ActionPaymentUnmatched, "REVIEW", meta)
			return nil
		}
		// Todas num comando só: um Pix de centenas não fica pago pela metade.
		settled, err := s.invoices.SettleInvoices(ctx, ids, paidVia(charge), charge.ID)
		if err != nil {
			return err
		}
		if len(settled) < len(ids) {
			// Quitadas por fora no meio do caminho: as outras entraram, e
			// alguém confere estas.
			paid := map[uuid.UUID]bool{}
			for _, id := range settled {
				paid[id] = true
			}
			var outside []uuid.UUID
			for _, id := range ids {
				if !paid[id] {
					outside = append(outside, id)
				}
			}
			s.log.Warn("faturas do Pix de várias já não estavam em aberto", "invoices", outside, "charge", charge.ID)
			s.record(ctx, audit.ActionPaymentUnmatched, "REVIEW",
				map[string]any{"chargeId": charge.ID, "invoiceIds": outside, "reason": "quitadas por fora"})
		}
		meta["settled"], meta["customerId"] = len(settled), invoices[0].CustomerID
		meta["via"] = paidVia(charge)
		s.record(ctx, audit.ActionInvoicePaid, "OK", meta)

	case abacatepay.StatusRefunded:
		reopened := 0
		for _, inv := range invoices {
			if inv.Status == billing.InvoicePaid && inv.PaidChargeID != nil && *inv.PaidChargeID == charge.ID {
				if _, err := s.invoices.ReopenInvoice(ctx, inv.ID); err != nil && !errors.Is(err, billing.ErrNotOpen) {
					return err
				}
				reopened++
			}
		}
		meta["invoicesReopened"] = reopened
		s.record(ctx, audit.ActionPaymentRefunded, "OK", meta)

	case abacatepay.StatusUnderDispute:
		s.record(ctx, audit.ActionPaymentDisputed, "REVIEW", meta)
	}
	return nil
}

// paidVia é como a cobrança quita a fatura.
func paidVia(charge *Charge) string {
	switch charge.Method {
	case MethodBoleto:
		return PaidViaBoleto
	case MethodCard:
		return PaidViaCard
	}
	return PaidViaPix
}

func withVia(meta map[string]any, charge *Charge, inv *billing.Invoice) map[string]any {
	meta["via"] = paidVia(charge)
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
	group, err := s.repo.ChargeInvoices(ctx, charge.ID)
	if err == nil && len(group) > 1 {
		// Várias: quitado quando todas quitaram.
		charge.InvoiceIDs, charge.InvoiceStatus = group, billing.InvoiceOpen
		if list, err := s.invoices.GetInvoices(ctx, group); err == nil && len(list) == len(group) {
			charge.InvoiceStatus = billing.InvoicePaid
			for _, inv := range list {
				if inv.Status != billing.InvoicePaid {
					charge.InvoiceStatus = billing.InvoiceOpen
					break
				}
			}
		}
		return charge
	}
	if inv, err := s.invoices.GetInvoice(ctx, charge.InvoiceID); err == nil {
		charge.InvoiceStatus = inv.Status
	}
	return charge
}

// boletoCustomerFor é o pagador do boleto: nome e CPF/CNPJ válido
// obrigatórios; o e-mail e o celular vão quando servem.
func boletoCustomerFor(p *Payer) (abacatepay.BoletoCustomer, bool) {
	if p == nil || strings.TrimSpace(p.Name) == "" || !validTaxID(p.Document) {
		return abacatepay.BoletoCustomer{}, false
	}
	c := abacatepay.BoletoCustomer{Name: strings.TrimSpace(p.Name), TaxID: onlyDigits(p.Document)}
	if strings.Contains(p.Email, "@") {
		c.Email = strings.TrimSpace(p.Email)
	}
	if phone := onlyDigits(p.Phone); len(phone) >= 10 && len(phone) <= 13 {
		c.Cellphone = phone
	}
	return c, true
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

// sortByDue ordena as faturas da primeira a vencer à última: a primeira é a
// principal da cobrança de várias (a que a lista de pagamentos mostra).
func sortByDue(invoices []*billing.Invoice) {
	sort.Slice(invoices, func(i, j int) bool {
		if !invoices[i].DueDate.Equal(invoices[j].DueDate.Time) {
			return invoices[i].DueDate.Before(invoices[j].DueDate)
		}
		return invoices[i].CreatedAt.Before(invoices[j].CreatedAt)
	})
}
