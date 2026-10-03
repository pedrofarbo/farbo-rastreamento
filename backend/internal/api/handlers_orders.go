package api

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/orders"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/vehicles"
)

// codeAddressRequired acompanha o 409 da contratação sem endereço de entrega.
const codeAddressRequired = "ADDRESS_REQUIRED"

// codePromoUnavailable: pediu a promoção de pré-lançamento sem ter direito
// (o painel volta a mostrar os preços normais).
const codePromoUnavailable = "PROMO_UNAVAILABLE"

// catalogView é a tabela de preços em JSON.
type catalogView struct {
	PlanName            string `json:"planName"`
	PlanPriceCents      int    `json:"planPriceCents"`
	DefaultDueDay       int    `json:"defaultDueDay"`
	EquipmentName       string `json:"equipmentName"`
	EquipmentPriceCents int    `json:"equipmentPriceCents"`
	SetupDueDays        int    `json:"setupDueDays"`
	// LaunchPromo: o cliente pode contratar com a promoção de
	// pré-lançamento (nulo: não pode, ou não é cliente).
	LaunchPromo *orders.PromoOffer `json:"launchPromo"`
}

// handleCatalog devolve os preços de um rastreador novo. Para o cliente, o
// plano mostrado é o que ele pagaria (o que já tem, ou o padrão).
func (s *Server) handleCatalog(w http.ResponseWriter, r *http.Request) {
	c := s.Orders.Catalog()
	view := catalogView{
		PlanName: c.PlanName, PlanPriceCents: c.PlanPriceCents, DefaultDueDay: c.DefaultDueDay,
		EquipmentName: c.EquipmentName, EquipmentPriceCents: c.EquipmentPriceCents, SetupDueDays: c.SetupDueDays,
	}
	if customerID, isCustomer := customerOf(r); isCustomer {
		if subs, err := s.Billing.ListSubscriptions(r.Context(), customerID); err == nil {
			for _, sub := range subs {
				if sub.Status == billing.SubscriptionActive {
					view.PlanName, view.PlanPriceCents, view.DefaultDueDay = sub.PlanName, sub.PriceCents, sub.DueDay
					break
				}
			}
		}
		if promo, err := s.Orders.PromoFor(r.Context(), customerID); err == nil && promo.Eligible {
			// A mensalidade da promoção no plano dele (o do Insanos MC tem a sua).
			promo.Offer.MonthlyCents = c.LaunchPromo.MonthlyFor(view.PlanName)
			view.LaunchPromo = &promo.Offer
		}
	}
	writeJSON(w, http.StatusOK, view)
}

// writeOrderError traduz os erros do fluxo Novo veículo.
func writeOrderError(w http.ResponseWriter, r *http.Request, err error, notFound string) {
	var orderValidation orders.ValidationError
	var vehicleValidation vehicles.ValidationError
	var billingValidation billing.ValidationError
	switch {
	case errors.As(err, &orderValidation):
		writeError(w, http.StatusBadRequest, orderValidation.Message)
	case errors.As(err, &vehicleValidation):
		writeError(w, http.StatusBadRequest, vehicleValidation.Message)
	case errors.As(err, &billingValidation):
		writeError(w, http.StatusBadRequest, billingValidation.Message)
	case errors.Is(err, orders.ErrHasOverdue), errors.Is(err, orders.ErrTooManyPending),
		errors.Is(err, orders.ErrSubscriptionTaken), errors.Is(err, orders.ErrVehicleHasSubscription):
		writeError(w, http.StatusConflict, err.Error())
	case errors.As(err, new(orders.PromoUnavailable)):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error(), "code": codePromoUnavailable})
	case errors.Is(err, orders.ErrAddressRequired):
		// O código leva o painel direto ao cadastro do endereço.
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error(), "code": codeAddressRequired})
	case errors.Is(err, database.ErrConflict):
		writeVehicleConflict(w, r)
	default:
		handleStoreError(w, err, notFound)
	}
}

// adminTrackerOrderRequest é o fluxo Novo veículo pela central: 1. veículo
// (pode já vir com o aparelho, se instalado na hora), 2. rastreador (valor do
// equipamento e vencimento; a entrega vai para o endereço do cliente) e
// 3. assinatura.
type adminTrackerOrderRequest struct {
	Vehicle vehicles.Input `json:"vehicle"`
	// Valor do equipamento; zero não gera fatura. A instalação é paga direto
	// ao prestador e não entra aqui.
	EquipmentCents int                       `json:"equipmentCents"`
	SetupDueDate   *billing.Date             `json:"setupDueDate"`
	Plan           billing.SubscriptionInput `json:"plan"`
	// LaunchPromo aplica a promoção de pré-lançamento (o cliente precisa ter
	// direito; ver /customers/{id}/launch-promo).
	LaunchPromo bool `json:"launchPromo"`
}

func (s *Server) handleAdminOrderTracker(w http.ResponseWriter, r *http.Request) {
	customerID, ok := s.customerFromURL(w, r)
	if !ok {
		return
	}
	var req adminTrackerOrderRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}

	result, err := s.Orders.Place(r.Context(), customerID, orders.Order{
		Vehicle: req.Vehicle, EquipmentCents: req.EquipmentCents, SetupDueDate: req.SetupDueDate, Plan: req.Plan,
		LaunchPromo: req.LaunchPromo,
	})
	if err != nil {
		writeOrderError(w, r, err, "cliente não encontrado")
		return
	}
	s.orderPlaced(r, customerID.String(), result, "CENTRAL")
	writeJSON(w, http.StatusCreated, result)
}

// customerTrackerOrderRequest é o fluxo Novo veículo pelo cliente: ele informa
// só o veículo; equipamento e plano vêm do catálogo e da conta dele.
type customerTrackerOrderRequest struct {
	Vehicle vehicles.Input `json:"vehicle"`
	// LaunchPromo: o painel mostrou os preços da promoção (catálogo) e o
	// cliente confirmou com eles.
	LaunchPromo bool `json:"launchPromo"`
}

func (s *Server) handleMyOrderTracker(w http.ResponseWriter, r *http.Request) {
	customerID, _ := customerOf(r)
	var req customerTrackerOrderRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}

	order, err := s.Orders.CustomerOrder(r.Context(), customerID, req.Vehicle, req.LaunchPromo)
	if err != nil {
		writeOrderError(w, r, err, "cliente não encontrado")
		return
	}
	result, err := s.Orders.Place(r.Context(), customerID, order)
	if err != nil {
		writeOrderError(w, r, err, "cliente não encontrado")
		return
	}
	s.orderPlaced(r, customerID.String(), result, "CLIENTE")
	writeJSON(w, http.StatusCreated, result)
}

// orderPlaced atualiza os caches e audita o pedido.
func (s *Server) orderPlaced(r *http.Request, customerID string, result *orders.Result, origin string) {
	s.vehiclesChanged(r)
	meta := map[string]any{
		"customerId": customerID, "origin": origin,
		"subscriptionId": result.Subscription.ID, "plan": result.Subscription.PlanName,
		"priceCents": result.Subscription.PriceCents, "plate": result.Vehicle.Plate,
	}
	if result.SetupInvoice != nil {
		meta["setupInvoiceId"] = result.SetupInvoice.ID
		meta["setupCents"] = result.SetupInvoice.AmountCents
	}
	if result.Subscription.PromoPriceCents != nil {
		meta["launchPromo"] = true
	}
	s.recordAudit(r, audit.ActionTrackerOrdered, &result.Vehicle.ID, result.Vehicle.DeviceID, meta)
}

// ---------------------------------------------------------------------------
// Acertos de dados anteriores ao fluxo único e reativação
// ---------------------------------------------------------------------------

// handleMyAttachVehicle: assinatura antiga sem veículo — o cliente informa
// qual veículo ela cobre.
func (s *Server) handleMyAttachVehicle(w http.ResponseWriter, r *http.Request) {
	customerID, _ := customerOf(r)
	s.attachVehicle(w, r, customerID, true)
}

// handleAdminAttachVehicle: o mesmo pela central, podendo já vincular o
// aparelho instalado.
func (s *Server) handleAdminAttachVehicle(w http.ResponseWriter, r *http.Request) {
	customerID, ok := s.customerFromURL(w, r)
	if !ok {
		return
	}
	s.attachVehicle(w, r, customerID, false)
}

func (s *Server) attachVehicle(w http.ResponseWriter, r *http.Request, customerID uuid.UUID, byCustomer bool) {
	subscriptionID, err := urlUUID(r, "subscriptionId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}
	var in vehicles.Input
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	if byCustomer {
		// Quem vincula o rastreador é a central, depois da instalação.
		in.DeviceID = nil
	}

	vehicle, err := s.Orders.AttachVehicle(r.Context(), customerID, subscriptionID, in)
	if err != nil {
		writeOrderError(w, r, err, "assinatura não encontrada")
		return
	}
	s.vehiclesChanged(r)
	s.recordAudit(r, audit.ActionVehicleCreated, &vehicle.ID, vehicle.DeviceID, map[string]any{
		"name": vehicle.Name, "plate": vehicle.Plate, "customerId": customerID, "subscriptionId": subscriptionID,
	})
	writeJSON(w, http.StatusCreated, vehicle)
}

// handleReactivateSubscription: a central abre uma nova assinatura para um
// veículo do cliente que ficou sem nenhuma ativa.
func (s *Server) handleReactivateSubscription(w http.ResponseWriter, r *http.Request) {
	customerID, ok := s.customerFromURL(w, r)
	if !ok {
		return
	}
	vehicleID, err := urlUUID(r, "vehicleId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}
	var plan billing.SubscriptionInput
	if err := decodeJSON(w, r, &plan); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}

	sub, err := s.Orders.Reactivate(r.Context(), customerID, vehicleID, plan)
	if err != nil {
		writeOrderError(w, r, err, "veículo não encontrado")
		return
	}
	s.recordBillingAudit(r, audit.ActionSubscriptionCreated, map[string]any{
		"customerId": customerID, "subscriptionId": sub.ID, "vehicleId": vehicleID,
		"plan": sub.PlanName, "priceCents": sub.PriceCents,
	})
	writeJSON(w, http.StatusCreated, sub)
}

// handleCustomerLaunchPromo: a central vê se o cliente pode contratar com a
// promoção de pré-lançamento (e por que não).
func (s *Server) handleCustomerLaunchPromo(w http.ResponseWriter, r *http.Request) {
	customerID, ok := s.customerFromURL(w, r)
	if !ok {
		return
	}
	status, err := s.Orders.PromoFor(r.Context(), customerID)
	if err != nil {
		handleStoreError(w, err, "cliente não encontrado")
		return
	}
	writeJSON(w, http.StatusOK, status)
}

// handleLaunchPromoUsage: vagas da promoção usadas e o total.
func (s *Server) handleLaunchPromoUsage(w http.ResponseWriter, r *http.Request) {
	usage, err := s.Orders.PromoUsage(r.Context())
	if err != nil {
		handleStoreError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, usage)
}
