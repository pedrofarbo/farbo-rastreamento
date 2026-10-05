package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/addresses"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/affiliates"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/devices"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/fulfillment"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/payments"
)

// customerDetail é a ficha do cliente na administração.
type customerDetail struct {
	*billing.CustomerSummary
	Subscriptions []*billing.Subscription `json:"subscriptions"`
	Invoices      []*billing.Invoice      `json:"invoices"`
	Vehicles      []vehicleView           `json:"vehicles"`
	OnlinePayment bool                    `json:"onlinePayment"`
	// Payments são os Pix que movimentaram dinheiro (pagos, estornados, em
	// disputa) — onde a central estorna.
	Payments []*payments.Payment `json:"payments"`
	// DeliveryAddress é para onde vão os rastreadores; nulo se não cadastrado.
	DeliveryAddress *addresses.Address `json:"deliveryAddress"`
	// Fulfillments é o acompanhamento de cada veículo (chip e rastreador).
	Fulfillments []*fulfillment.Fulfillment `json:"fulfillments"`
	// HistoryRetentionDays é o prazo do cliente (nulo = padrão da central,
	// em DefaultHistoryDays).
	HistoryRetentionDays *int `json:"historyRetentionDays"`
	DefaultHistoryDays   int  `json:"defaultHistoryDays"`
	// Affiliate: quem indicou o cliente (nulo se ninguém).
	Affiliate *affiliates.Referral `json:"affiliate"`
}

func (s *Server) customerDetail(ctx context.Context, id uuid.UUID) (*customerDetail, error) {
	summary, err := s.Billing.GetCustomer(ctx, id)
	if err != nil {
		return nil, err
	}
	subscriptions, err := s.Billing.ListSubscriptions(ctx, id)
	if err != nil {
		return nil, err
	}
	invoices, err := s.Billing.ListInvoices(ctx, id)
	if err != nil {
		return nil, err
	}
	owned, err := s.Vehicles.ListByOwner(ctx, id)
	if err != nil {
		return nil, err
	}
	// A ficha do cliente é só do admin (ver as rotas).
	views, err := s.vehicleViews(ctx, owned, devices.AudienceAdmin)
	if err != nil {
		return nil, err
	}
	paid, err := s.Payments.Payments(ctx, id)
	if err != nil {
		return nil, err
	}
	address, err := s.Addresses.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	tracking, err := s.Fulfillment.Repo().ListByCustomer(ctx, id)
	if err != nil {
		return nil, err
	}
	retentionDays, err := s.Retention.CustomerDays(ctx, id)
	if err != nil {
		return nil, err
	}
	var referral *affiliates.Referral
	if s.Affiliates != nil {
		if referral, err = s.Affiliates.CustomerReferral(ctx, id); err != nil {
			return nil, err
		}
	}
	return &customerDetail{
		CustomerSummary: summary, Subscriptions: subscriptions, Invoices: invoices, Vehicles: views,
		OnlinePayment: s.Payments.Enabled(), Payments: paid, DeliveryAddress: address, Fulfillments: tracking,
		HistoryRetentionDays: retentionDays, DefaultHistoryDays: s.Retention.Default(), Affiliate: referral,
	}, nil
}

// writeBillingError traduz os erros de cobrança em respostas HTTP.
func writeBillingError(w http.ResponseWriter, err error, notFound string) {
	var validation billing.ValidationError
	switch {
	case errors.As(err, &validation):
		writeError(w, http.StatusBadRequest, validation.Message)
	case errors.Is(err, billing.ErrNotOpen):
		writeError(w, http.StatusConflict, "esta fatura não está mais em aberto")
	case errors.Is(err, billing.ErrAlreadyCanceled):
		writeError(w, http.StatusConflict, "esta assinatura já foi cancelada")
	default:
		handleStoreError(w, err, notFound)
	}
}

// customerFromURL confere que o id da rota é de um cliente (e não de alguém
// da equipe) antes de qualquer alteração.
func (s *Server) customerFromURL(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return uuid.Nil, false
	}
	if _, err := s.Billing.GetCustomer(r.Context(), id); err != nil {
		handleStoreError(w, err, "cliente não encontrado")
		return uuid.Nil, false
	}
	return id, true
}

func (s *Server) recordBillingAudit(r *http.Request, action string, metadata map[string]any) {
	s.recordAudit(r, action, nil, nil, metadata)
}

// ---------------------------------------------------------------------------
// Clientes
// ---------------------------------------------------------------------------

func (s *Server) handleListCustomers(w http.ResponseWriter, r *http.Request) {
	list, err := s.Billing.ListCustomers(r.Context())
	if err != nil {
		handleStoreError(w, err, "clientes não encontrados")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleGetCustomer(w http.ResponseWriter, r *http.Request) {
	id, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}
	detail, err := s.customerDetail(r.Context(), id)
	if err != nil {
		handleStoreError(w, err, "cliente não encontrado")
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

type createCustomerRequest struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Phone    string `json:"phone"`
	Document string `json:"document"`
	// Password vazio manda um convite por e-mail para o cliente criar a
	// própria senha — o caminho recomendado. Veículos e assinaturas entram
	// depois, pelo fluxo Novo veículo.
	Password string `json:"password"`
	// LeadID: o pré-cliente de onde veio o cadastro, que passa a convertido.
	LeadID *uuid.UUID `json:"leadId"`
}

func (s *Server) handleCreateCustomer(w http.ResponseWriter, r *http.Request) {
	var req createCustomerRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "o nome do cliente é obrigatório")
		return
	}
	password := req.Password
	invite := strings.TrimSpace(password) == ""
	if invite {
		random, err := auth.RandomPassword()
		if err != nil {
			handleStoreError(w, err, "")
			return
		}
		password = random
	}

	user, err := s.Auth.Register(r.Context(), auth.NewUser{
		Email: req.Email, Name: req.Name, Role: auth.RoleCustomer, Password: password,
		Phone: req.Phone, Document: req.Document,
	})
	if err != nil {
		if errors.Is(err, database.ErrConflict) {
			writeError(w, http.StatusConflict, "já existe um usuário com esse e-mail")
			return
		}
		// O que sobra são erros de validação, que o cliente consegue corrigir.
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if invite {
		if err := s.Auth.InviteUser(r.Context(), user); err != nil {
			s.Log.Error("falha ao gerar o convite do cliente", "user", user.ID, "err", err)
		}
	}

	s.recordBillingAudit(r, audit.ActionCustomerCreated,
		map[string]any{"customerId": user.ID, "email": user.Email, "invite": invite, "leadId": req.LeadID})
	if req.LeadID != nil && s.Leads != nil {
		if err := s.Leads.Repo().MarkConverted(r.Context(), *req.LeadID, user.ID); err != nil {
			s.Log.Error("cliente cadastrado, mas o pré-cliente não foi marcado como convertido",
				"lead", *req.LeadID, "customer", user.ID, "err", err)
		}
	}
	// Veio pelo link de um afiliado (o pré-cadastro, ou o e-mail na lista de
	// lançamento): o cliente fica indicado por ele.
	if s.Affiliates != nil {
		if _, err := s.Affiliates.AttachCustomer(r.Context(), user.ID, user.Email, req.LeadID); err != nil {
			s.Log.Error("cliente cadastrado, mas sem o afiliado que o indicou", "customer", user.ID, "err", err)
		}
	}

	detail, err := s.customerDetail(r.Context(), user.ID)
	if err != nil {
		handleStoreError(w, err, "cliente não encontrado")
		return
	}
	writeJSON(w, http.StatusCreated, detail)
}

type updateCustomerRequest struct {
	Name     string `json:"name"`
	Phone    string `json:"phone"`
	Document string `json:"document"`
	Active   bool   `json:"active"`
}

func (s *Server) handleUpdateCustomer(w http.ResponseWriter, r *http.Request) {
	id, ok := s.customerFromURL(w, r)
	if !ok {
		return
	}
	var req updateCustomerRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}

	if _, err := s.Auth.UpdateProfile(r.Context(), id, auth.Profile{
		Name: req.Name, Phone: req.Phone, Document: req.Document, Active: req.Active,
	}); err != nil {
		if errors.Is(err, database.ErrNotFound) {
			writeError(w, http.StatusNotFound, "cliente não encontrado")
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.recordBillingAudit(r, audit.ActionCustomerUpdated,
		map[string]any{"customerId": id, "active": req.Active})
	detail, err := s.customerDetail(r.Context(), id)
	if err != nil {
		handleStoreError(w, err, "cliente não encontrado")
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

// handleInviteCustomer reenvia o e-mail de boas-vindas (link para criar a
// senha). O link anterior deixa de valer.
func (s *Server) handleInviteCustomer(w http.ResponseWriter, r *http.Request) {
	id, ok := s.customerFromURL(w, r)
	if !ok {
		return
	}
	user, err := s.Auth.GetUser(r.Context(), id)
	if err != nil {
		handleStoreError(w, err, "cliente não encontrado")
		return
	}
	if err := s.Auth.InviteUser(r.Context(), user); err != nil {
		if errors.Is(err, auth.ErrInactiveUser) {
			writeError(w, http.StatusConflict, "o cliente está desativado; reative antes de convidar")
			return
		}
		handleStoreError(w, err, "cliente não encontrado")
		return
	}
	s.recordBillingAudit(r, audit.ActionCustomerInvited, map[string]any{"customerId": id})
	writeJSON(w, http.StatusAccepted, map[string]string{"message": "convite enviado para " + user.Email})
}

// ---------------------------------------------------------------------------
// Assinaturas
// ---------------------------------------------------------------------------

type updateSubscriptionRequest struct {
	PlanName   string `json:"planName"`
	PriceCents int    `json:"priceCents"`
}

func (s *Server) handleUpdateSubscription(w http.ResponseWriter, r *http.Request) {
	id, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}
	var req updateSubscriptionRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}

	sub, err := s.Billing.UpdateSubscription(r.Context(), id, req.PlanName, req.PriceCents)
	if err != nil {
		writeBillingError(w, err, "assinatura não encontrada")
		return
	}
	s.recordBillingAudit(r, audit.ActionSubscriptionUpdated, map[string]any{
		"customerId": sub.CustomerID, "subscriptionId": sub.ID,
		"plan": sub.PlanName, "priceCents": sub.PriceCents,
	})
	writeJSON(w, http.StatusOK, sub)
}

func (s *Server) handleCancelSubscription(w http.ResponseWriter, r *http.Request) {
	id, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}
	sub, err := s.Billing.CancelSubscription(r.Context(), id)
	if err != nil {
		writeBillingError(w, err, "assinatura não encontrada")
		return
	}
	s.recordBillingAudit(r, audit.ActionSubscriptionCanceled,
		map[string]any{"customerId": sub.CustomerID, "subscriptionId": sub.ID})
	writeJSON(w, http.StatusOK, sub)
}

// ---------------------------------------------------------------------------
// Faturas
// ---------------------------------------------------------------------------

func (s *Server) handleCreateInvoice(w http.ResponseWriter, r *http.Request) {
	customerID, ok := s.customerFromURL(w, r)
	if !ok {
		return
	}
	var in billing.InvoiceInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido: confira valor e vencimento (AAAA-MM-DD)")
		return
	}

	inv, err := s.Billing.CreateInvoice(r.Context(), customerID, in)
	if err != nil {
		writeBillingError(w, err, "cliente não encontrado")
		return
	}
	s.recordBillingAudit(r, audit.ActionInvoiceCreated, map[string]any{
		"customerId": customerID, "invoiceId": inv.ID, "amountCents": inv.AmountCents,
	})
	writeJSON(w, http.StatusCreated, inv)
}

type updateInvoiceRequest struct {
	PaymentURL string `json:"paymentUrl"`
	PixCode    string `json:"pixCode"`
}

// handleUpdateInvoice grava o link de pagamento e o Pix copia-e-cola que o
// cliente vai ver na fatura.
func (s *Server) handleUpdateInvoice(w http.ResponseWriter, r *http.Request) {
	id, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}
	var req updateInvoiceRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}

	inv, err := s.Billing.UpdateInvoicePayment(r.Context(), id, req.PaymentURL, req.PixCode)
	if err != nil {
		writeBillingError(w, err, "fatura não encontrada")
		return
	}
	s.recordBillingAudit(r, audit.ActionInvoiceUpdated,
		map[string]any{"customerId": inv.CustomerID, "invoiceId": inv.ID})
	writeJSON(w, http.StatusOK, inv)
}

// handlePayInvoice dá baixa manual: a central confirmou o recebimento. Se era
// a fatura que suspendia o cliente, o acesso volta na hora.
func (s *Server) handlePayInvoice(w http.ResponseWriter, r *http.Request) {
	s.transitionInvoice(w, r, s.Billing.MarkInvoicePaid, audit.ActionInvoicePaid)
}

func (s *Server) handleCancelInvoice(w http.ResponseWriter, r *http.Request) {
	s.transitionInvoice(w, r, s.Billing.CancelInvoice, audit.ActionInvoiceCanceled)
}

func (s *Server) transitionInvoice(
	w http.ResponseWriter, r *http.Request,
	apply func(context.Context, uuid.UUID) (*billing.Invoice, error), action string,
) {
	id, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}
	inv, err := apply(r.Context(), id)
	if err != nil {
		writeBillingError(w, err, "fatura não encontrada")
		return
	}
	s.recordBillingAudit(r, action, map[string]any{
		"customerId": inv.CustomerID, "invoiceId": inv.ID, "amountCents": inv.AmountCents,
	})
	writeJSON(w, http.StatusOK, inv)
}
