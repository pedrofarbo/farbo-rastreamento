package billing

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"
	// Garante o banco de fusos mesmo em imagem sem tzdata: o "hoje" dos
	// vencimentos depende dele.
	_ "time/tzdata"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
)

// ValidationError descreve um pedido recusado; a mensagem vai para a tela.
type ValidationError struct{ Message string }

func (e ValidationError) Error() string { return e.Message }

type Service struct {
	repo *Repository
	cfg  config.Billing
	loc  *time.Location
	log  *slog.Logger
	now  func() time.Time
}

func NewService(repo *Repository, cfg config.Billing, log *slog.Logger) (*Service, error) {
	loc, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		return nil, fmt.Errorf("fuso de cobrança: %w", err)
	}
	return &Service{repo: repo, cfg: cfg, loc: loc, log: log.With("component", "billing"), now: time.Now}, nil
}

// SetClock troca o relógio (testes).
func (s *Service) SetClock(now func() time.Time) { s.now = now }

// Today é a data de hoje no fuso de cobrança.
func (s *Service) Today() Date { return DateIn(s.now(), s.loc) }

// SuspendAfterDays é o atraso que suspende o acesso (0 = nunca).
func (s *Service) SuspendAfterDays() int { return s.cfg.SuspendAfterDays }

// suspensionCutoff: fatura em aberto com vencimento antes desta data
// suspende o cliente. Com 10 dias, a fatura do dia 1 suspende a partir do 12.
func (s *Service) suspensionCutoff() Date { return s.Today().AddDays(-s.cfg.SuspendAfterDays) }

// IsSuspended diz se o cliente perdeu o acesso por atraso.
func (s *Service) IsSuspended(ctx context.Context, customerID uuid.UUID) (bool, error) {
	if s.cfg.SuspendAfterDays <= 0 {
		return false, nil
	}
	return s.repo.HasOverdue(ctx, customerID, s.suspensionCutoff())
}

// GenerateInvoices cria as faturas que vencem nos próximos InvoiceLeadDays.
func (s *Service) GenerateInvoices(ctx context.Context) {
	created, err := s.repo.GenerateDue(ctx, s.Today().AddDays(s.cfg.InvoiceLeadDays))
	if err != nil {
		s.log.Error("falha ao gerar faturas", "err", err)
		return
	}
	if created > 0 {
		s.log.Info("faturas geradas", "count", created)
	}
}

func (s *Service) ListCustomers(ctx context.Context) ([]*CustomerSummary, error) {
	return s.repo.ListCustomers(ctx, s.Today(), s.suspensionCutoff(), s.cfg.SuspendAfterDays > 0)
}

func (s *Service) GetCustomer(ctx context.Context, id uuid.UUID) (*CustomerSummary, error) {
	return s.repo.GetCustomer(ctx, id, s.Today(), s.suspensionCutoff(), s.cfg.SuspendAfterDays > 0)
}

// ---------------------------------------------------------------------------
// Assinaturas
// ---------------------------------------------------------------------------

// SubscriptionInput é o cadastro de uma assinatura.
type SubscriptionInput struct {
	PlanName   string `json:"planName"`
	PriceCents int    `json:"priceCents"`
	DueDay     int    `json:"dueDay"`
	// FirstDueDate é opcional; sem ele, o primeiro vencimento é o próximo
	// dia DueDay a partir de hoje.
	FirstDueDate *Date `json:"firstDueDate"`
}

// Validate confere a assinatura antes de qualquer gravação.
func (s *Service) Validate(in SubscriptionInput) error {
	if err := validatePlan(in.PlanName, in.PriceCents); err != nil {
		return err
	}
	if in.DueDay < 1 || in.DueDay > 28 {
		return ValidationError{"o dia de vencimento vai de 1 a 28"}
	}
	if in.FirstDueDate != nil {
		if in.FirstDueDate.Day() != in.DueDay {
			return ValidationError{"o primeiro vencimento precisa cair no dia de vencimento escolhido"}
		}
		if in.FirstDueDate.Before(s.Today()) {
			return ValidationError{"o primeiro vencimento não pode estar no passado"}
		}
	}
	return nil
}

func validatePlan(planName string, priceCents int) error {
	name := strings.TrimSpace(planName)
	if name == "" || len(name) > 100 {
		return ValidationError{"informe o nome do plano (até 100 caracteres)"}
	}
	if priceCents < 0 || priceCents > 100_000_00 {
		return ValidationError{"valor mensal fora da faixa (R$ 0 a R$ 100.000)"}
	}
	return nil
}

// NewSubscription valida e monta a assinatura (com o primeiro vencimento),
// sem gravar. Quem grava junto com outras coisas usa InsertSubscription.
func (s *Service) NewSubscription(customerID uuid.UUID, in SubscriptionInput) (*Subscription, error) {
	if err := s.Validate(in); err != nil {
		return nil, err
	}
	first := firstDueDate(s.Today(), in.DueDay)
	if in.FirstDueDate != nil {
		first = *in.FirstDueDate
	}
	return &Subscription{
		CustomerID:  customerID,
		PlanName:    strings.TrimSpace(in.PlanName),
		PriceCents:  in.PriceCents,
		DueDay:      in.DueDay,
		NextDueDate: first,
	}, nil
}

// HasActiveForVehicle diz se o veículo tem assinatura ativa (e não pode ser
// excluído antes de encerrá-la).
func (s *Service) HasActiveForVehicle(ctx context.Context, vehicleID uuid.UUID) (bool, error) {
	return s.repo.HasActiveForVehicle(ctx, vehicleID)
}

func (s *Service) GetSubscription(ctx context.Context, id uuid.UUID) (*Subscription, error) {
	return s.repo.GetSubscription(ctx, id)
}

func (s *Service) UpdateSubscription(ctx context.Context, id uuid.UUID, planName string, priceCents int) (*Subscription, error) {
	if err := validatePlan(planName, priceCents); err != nil {
		return nil, err
	}
	return s.repo.UpdateSubscription(ctx, id, strings.TrimSpace(planName), priceCents)
}

func (s *Service) CancelSubscription(ctx context.Context, id uuid.UUID) (*Subscription, error) {
	return s.repo.CancelSubscription(ctx, id, s.Today())
}

func (s *Service) ListSubscriptions(ctx context.Context, customerID uuid.UUID) ([]*Subscription, error) {
	return s.repo.ListSubscriptions(ctx, customerID)
}

// ---------------------------------------------------------------------------
// Faturas
// ---------------------------------------------------------------------------

// InvoiceInput é a fatura avulsa lançada pela central (instalação, por exemplo).
type InvoiceInput struct {
	Description string `json:"description"`
	AmountCents int    `json:"amountCents"`
	DueDate     Date   `json:"dueDate"`
	PaymentURL  string `json:"paymentUrl"`
	PixCode     string `json:"pixCode"`
}

func (s *Service) CreateInvoice(ctx context.Context, customerID uuid.UUID, in InvoiceInput) (*Invoice, error) {
	prepared, err := s.NewInvoice(customerID, in)
	if err != nil {
		return nil, err
	}
	inv, err := s.repo.CreateInvoice(ctx, prepared)
	if err != nil {
		return nil, err
	}
	return s.decorate(inv), nil
}

// Decorate calcula se a fatura está vencida (para quem gravou por fora).
func (s *Service) Decorate(inv *Invoice) *Invoice { return s.decorate(inv) }

// NewInvoice valida e monta a fatura avulsa, sem gravar. Quem grava junto
// com outras coisas usa InsertInvoice.
func (s *Service) NewInvoice(customerID uuid.UUID, in InvoiceInput) (*Invoice, error) {
	description := strings.TrimSpace(in.Description)
	if description == "" || len(description) > 200 {
		return nil, ValidationError{"informe a descrição da fatura (até 200 caracteres)"}
	}
	if in.AmountCents <= 0 || in.AmountCents > 100_000_00 {
		return nil, ValidationError{"valor fora da faixa (R$ 0,01 a R$ 100.000)"}
	}
	if in.DueDate.IsZero() {
		return nil, ValidationError{"informe o vencimento"}
	}
	paymentURL, pixCode, err := validatePayment(in.PaymentURL, in.PixCode)
	if err != nil {
		return nil, err
	}
	return &Invoice{
		CustomerID: customerID, Description: description, AmountCents: in.AmountCents,
		DueDate: in.DueDate, PaymentURL: paymentURL, PixCode: pixCode,
	}, nil
}

// validatePayment aceita só link http(s): o endereço vira um botão na tela do
// cliente, e "javascript:" ali seria uma porta para injeção de script.
func validatePayment(paymentURL, pixCode string) (string, string, error) {
	paymentURL, pixCode = strings.TrimSpace(paymentURL), strings.TrimSpace(pixCode)
	if paymentURL != "" {
		parsed, err := url.Parse(paymentURL)
		if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
			return "", "", ValidationError{"o link de pagamento precisa começar com https://"}
		}
		if len(paymentURL) > 1000 {
			return "", "", ValidationError{"link de pagamento longo demais"}
		}
	}
	if len(pixCode) > 1000 {
		return "", "", ValidationError{"código Pix longo demais"}
	}
	return paymentURL, pixCode, nil
}

func (s *Service) GetInvoice(ctx context.Context, id uuid.UUID) (*Invoice, error) {
	inv, err := s.repo.GetInvoice(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.decorate(inv), nil
}

func (s *Service) ListInvoices(ctx context.Context, customerID uuid.UUID) ([]*Invoice, error) {
	list, err := s.repo.ListInvoices(ctx, customerID)
	if err != nil {
		return nil, err
	}
	for _, inv := range list {
		s.decorate(inv)
	}
	return list, nil
}

// PaidManually marca a baixa feita pela central.
const PaidManually = "MANUAL"

func (s *Service) MarkInvoicePaid(ctx context.Context, id uuid.UUID) (*Invoice, error) {
	return s.decorated(s.repo.MarkInvoicePaid(ctx, id, PaidManually, nil))
}

// SettleInvoice quita a fatura por outro meio (Pix confirmado no provedor),
// guardando qual cobrança pagou.
func (s *Service) SettleInvoice(ctx context.Context, id uuid.UUID, via string, chargeID uuid.UUID) (*Invoice, error) {
	return s.decorated(s.repo.MarkInvoicePaid(ctx, id, via, &chargeID))
}

// ReopenInvoice reabre a fatura cujo pagamento foi estornado.
func (s *Service) ReopenInvoice(ctx context.Context, id uuid.UUID) (*Invoice, error) {
	return s.decorated(s.repo.ReopenInvoice(ctx, id))
}

func (s *Service) CancelInvoice(ctx context.Context, id uuid.UUID) (*Invoice, error) {
	return s.decorated(s.repo.CancelInvoice(ctx, id))
}

func (s *Service) UpdateInvoicePayment(ctx context.Context, id uuid.UUID, paymentURL, pixCode string) (*Invoice, error) {
	paymentURL, pixCode, err := validatePayment(paymentURL, pixCode)
	if err != nil {
		return nil, err
	}
	return s.decorated(s.repo.UpdateInvoicePayment(ctx, id, paymentURL, pixCode))
}

func (s *Service) decorated(inv *Invoice, err error) (*Invoice, error) {
	if err != nil {
		return nil, err
	}
	return s.decorate(inv), nil
}

// decorate calcula se a fatura está vencida, e há quantos dias.
func (s *Service) decorate(inv *Invoice) *Invoice {
	today := s.Today()
	if inv.Status == InvoiceOpen && inv.DueDate.Before(today) {
		inv.Overdue = true
		inv.DaysOverdue = int(today.Sub(inv.DueDate.Time).Hours() / 24)
	}
	return inv
}

// ---------------------------------------------------------------------------
// Visão do cliente
// ---------------------------------------------------------------------------

// Account é o resumo que o cliente vê: cota de veículos, suspensão e a
// próxima fatura a pagar.
type Account struct {
	ActiveSubscriptions int      `json:"activeSubscriptions"`
	Vehicles            int      `json:"vehicles"`
	OpenInvoices        int      `json:"openInvoices"`
	OverdueInvoices     int      `json:"overdueInvoices"`
	OpenAmountCents     int      `json:"openAmountCents"`
	Suspended           bool     `json:"suspended"`
	SuspendAfterDays    int      `json:"suspendAfterDays"`
	NextInvoice         *Invoice `json:"nextInvoice"`
}

func (s *Service) Account(ctx context.Context, customerID uuid.UUID) (*Account, error) {
	summary, err := s.GetCustomer(ctx, customerID)
	if err != nil {
		return nil, err
	}
	account := &Account{
		ActiveSubscriptions: summary.ActiveSubscriptions,
		Vehicles:            summary.VehicleCount,
		OpenInvoices:        summary.OpenInvoices,
		OverdueInvoices:     summary.OverdueInvoices,
		OpenAmountCents:     summary.OpenAmountCents,
		Suspended:           summary.Suspended,
		SuspendAfterDays:    s.cfg.SuspendAfterDays,
	}

	invoices, err := s.ListInvoices(ctx, customerID)
	if err != nil {
		return nil, err
	}
	// A lista vem do vencimento mais novo para o mais antigo; a próxima a
	// pagar é a em aberto mais antiga.
	for _, inv := range invoices {
		if inv.Status == InvoiceOpen {
			account.NextInvoice = inv
		}
	}
	return account, nil
}
