package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/fulfillment"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/melhorenvio"
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
	// EquipmentMaxInstallments: em até quantas vezes sem juros o rastreador
	// pode ser parcelado (1: só à vista).
	EquipmentMaxInstallments int `json:"equipmentMaxInstallments"`
	// LaunchPromo: o cliente pode contratar com a promoção de
	// pré-lançamento (nulo: não pode, ou não é cliente).
	LaunchPromo *orders.PromoOffer `json:"launchPromo"`
	// Para o cliente: o rastreador dele (venda ou comodato) e as taxas, e o
	// vencimento da cobrança de um veículo contratado hoje.
	EquipmentMode     string        `json:"equipmentMode,omitempty"`
	InstallFeeCents   int           `json:"installFeeCents,omitempty"`
	UninstallFeeCents int           `json:"uninstallFeeCents,omitempty"`
	ChargeDate        *billing.Date `json:"chargeDate,omitempty"`
	// PlanTerms: o rastreador vem do plano definido pela central (sem
	// escolha); sem ele, Loan é o comodato que o cliente pode escolher (nulo:
	// só comprado).
	PlanTerms bool              `json:"planTerms,omitempty"`
	Loan      *orders.LoanOffer `json:"loan,omitempty"`
	// Install: a instalação pelos instaladores da Farbo no rastreador
	// comprado (o preço; desligada, o cliente instala por conta própria).
	Install *orders.InstallService `json:"install,omitempty"`
	// PayNow: a pessoa física paga no ato (a 1ª mensalidade, o frete e o
	// rastreador ou a instalação); a empresa, na próxima fatura do mês.
	PayNow bool `json:"payNow,omitempty"`
}

// handleCatalog devolve os preços de um rastreador novo. Para o cliente, o
// plano e o rastreador mostrados são os que ele pagaria (CustomerPrices).
func (s *Server) handleCatalog(w http.ResponseWriter, r *http.Request) {
	c := s.Orders.Catalog()
	view := catalogView{
		PlanName: c.PlanName, PlanPriceCents: c.PlanPriceCents, DefaultDueDay: c.DefaultDueDay,
		EquipmentName: c.EquipmentName, EquipmentPriceCents: c.EquipmentPriceCents, SetupDueDays: c.SetupDueDays,
		EquipmentMaxInstallments: c.EquipmentMaxInstallments,
	}
	opts, err := s.Orders.Options(r.Context())
	if err != nil {
		handleStoreError(w, err, "opções do pedido não encontradas")
		return
	}
	view.Install = &opts.Install
	if customerID, isCustomer := customerOf(r); isCustomer {
		// Os preços dele: a pessoa física paga no app o mesmo que no site.
		if plan, equipment, err := s.Orders.CustomerPrices(r.Context(), customerID); err == nil {
			view.PlanName, view.PlanPriceCents, view.DefaultDueDay = plan.PlanName, plan.PriceCents, plan.DueDay
			view.EquipmentPriceCents = equipment
		}
		if terms, err := s.Orders.CustomerTerms(r.Context(), customerID); err == nil {
			view.EquipmentMode, view.InstallFeeCents, view.UninstallFeeCents = terms.EquipmentMode, terms.InstallFeeCents, terms.UninstallFeeCents
		}
		if account, err := s.Orders.AccountPlan(r.Context(), customerID); err == nil && account != nil {
			view.PlanTerms = true
		} else if opts.Loan.Enabled {
			view.Loan = &opts.Loan
		}
		if !opts.Install.Enabled {
			view.Install = nil
		}
		if upfront, err := s.Orders.PaysUpfront(r.Context(), customerID); err == nil && upfront {
			view.PayNow = true
			due := s.Billing.Today().AddDays(c.SetupDueDays)
			view.ChargeDate = &due
		} else if due, err := s.Billing.ChargeDate(r.Context(), s.DB, customerID, view.DefaultDueDay); err == nil {
			view.ChargeDate = &due
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
	// Valor do equipamento; zero não cobra. A instalação, fora do comodato, é
	// paga direto ao prestador e não entra aqui.
	EquipmentCents int `json:"equipmentCents"`
	// SetupDueDate não vale mais: tudo entra na próxima fatura do mês da
	// conta (aceito para as telas antigas).
	SetupDueDate *billing.Date             `json:"setupDueDate"`
	Plan         billing.SubscriptionInput `json:"plan"`
	// Terms: venda ou comodato e as taxas; vazio, os do plano do cliente.
	Terms *orders.Terms `json:"terms"`
	// LaunchPromo aplica a promoção de pré-lançamento (o cliente precisa ter
	// direito; ver /customers/{id}/launch-promo).
	LaunchPromo bool `json:"launchPromo"`
	// ShippingServiceID: o frete cobrado do cliente (zero: sem frete —
	// entregue em mãos, instalado na base).
	ShippingServiceID int `json:"shippingServiceId"`
	// Installments: o equipamento parcelado sem juros (0 ou 1: à vista).
	Installments int `json:"installments"`
	// FarboInstall: o rastreador comprado instalado pelos instaladores da
	// Farbo, pelo preço das opções do pedido (no comodato, já é da Farbo).
	FarboInstall bool `json:"farboInstall"`
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

	var shipping *fulfillment.Choice
	if req.ShippingServiceID > 0 && req.Vehicle.DeviceID == nil {
		var err error
		if shipping, err = s.chooseShipping(r.Context(), customerID, req.ShippingServiceID); err != nil {
			writeShippingError(w, err)
			return
		}
	}
	terms := req.Terms
	if terms == nil {
		t, err := s.Orders.CustomerTerms(r.Context(), customerID)
		if err != nil {
			handleStoreError(w, err, "cliente não encontrado")
			return
		}
		terms = &t
	}
	order := orders.Order{
		Vehicle: req.Vehicle, EquipmentCents: req.EquipmentCents, Terms: *terms, Plan: req.Plan,
		LaunchPromo: req.LaunchPromo, Shipping: shipping, Installments: req.Installments,
	}
	if req.FarboInstall {
		if err := s.Orders.WithFarboInstall(r.Context(), &order); err != nil {
			writeOrderError(w, r, err, "cliente não encontrado")
			return
		}
	}
	result, err := s.Orders.Place(r.Context(), customerID, order)
	if err != nil {
		writeOrderError(w, r, err, "cliente não encontrado")
		return
	}
	s.orderPlaced(r, customerID.String(), result, "CENTRAL")
	writeJSON(w, http.StatusCreated, result)
}

// adminFleetImportRequest é a frota de uma vez pela central: as linhas da
// planilha e, para todas, o valor do equipamento (numa fatura só) e o plano.
// DryRun só confere.
type adminFleetImportRequest struct {
	Rows           []orders.ImportRow `json:"rows"`
	EquipmentCents int                `json:"equipmentCents"`
	// SetupDueDate não vale mais: tudo entra na próxima fatura do mês da
	// conta (aceito para as telas antigas).
	SetupDueDate *billing.Date             `json:"setupDueDate"`
	Plan         billing.SubscriptionInput `json:"plan"`
	DryRun       bool                      `json:"dryRun"`
}

// codeImportProblems: a planilha foi recusada; problems traz cada linha.
const codeImportProblems = "IMPORT_PROBLEMS"

func (s *Server) handleAdminImportFleet(w http.ResponseWriter, r *http.Request) {
	customerID, ok := s.customerFromURL(w, r)
	if !ok {
		return
	}
	var req adminFleetImportRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido (a planilha passa do tamanho aceito?)")
		return
	}
	terms, err := s.Orders.CustomerTerms(r.Context(), customerID)
	if err != nil {
		handleStoreError(w, err, "cliente não encontrado")
		return
	}
	result, err := s.Orders.ImportFleet(r.Context(), customerID, orders.Import{
		Rows: req.Rows, EquipmentCents: req.EquipmentCents, Terms: terms, Plan: req.Plan, DryRun: req.DryRun,
	})
	var problems orders.ImportProblems
	if errors.As(err, &problems) {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"error": problems.Error(), "code": codeImportProblems, "problems": problems,
		})
		return
	}
	if err != nil {
		writeOrderError(w, r, err, "cliente não encontrado")
		return
	}
	if result.DryRun {
		writeJSON(w, http.StatusOK, result)
		return
	}
	s.vehiclesChanged(r)
	meta := map[string]any{
		"customerId": customerID.String(), "vehicles": result.Vehicles, "installed": result.Installed,
		"newGroups": result.NewGroups, "plan": req.Plan.PlanName, "priceCents": req.Plan.PriceCents,
	}
	if result.SetupInvoice != nil {
		meta["setupInvoiceId"], meta["setupCents"] = result.SetupInvoice.ID, result.SetupInvoice.AmountCents
	}
	s.recordAudit(r, audit.ActionFleetImported, nil, nil, meta)
	writeJSON(w, http.StatusCreated, result)
}

// customerTrackerOrderRequest é o fluxo Novo veículo pelo cliente: ele informa
// só o veículo; equipamento e plano vêm do catálogo e da conta dele.
type customerTrackerOrderRequest struct {
	Vehicle vehicles.Input `json:"vehicle"`
	// LaunchPromo: o painel mostrou os preços da promoção (catálogo) e o
	// cliente confirmou com eles.
	LaunchPromo bool `json:"launchPromo"`
	// ShippingServiceID: a forma de entrega escolhida (obrigatória com o
	// frete ligado); o preço é cotado de novo aqui.
	ShippingServiceID int `json:"shippingServiceId"`
	// ArrangeDelivery: em vez da transportadora, combinar a entrega com a
	// central (só nas cidades de SHIPPING_ARRANGE_CITIES).
	ArrangeDelivery bool `json:"arrangeDelivery"`
	// Installments: o rastreador parcelado sem juros (0 ou 1: à vista); o
	// cliente aceita ficar com a assinatura ativa até a última parcela.
	Installments int `json:"installments"`
	// EquipmentMode: o rastreador comprado (sale, o padrão) ou em comodato
	// (loan, quando a central oferece); com o plano definido pela central,
	// vale o dele.
	EquipmentMode string `json:"equipmentMode"`
	// FarboInstall: o rastreador comprado instalado pelos instaladores da
	// Farbo (a taxa vem junto com a 1ª mensalidade); sem isso, o cliente
	// instala por conta própria. No comodato, a instalação é sempre da Farbo.
	FarboInstall bool `json:"farboInstall"`
}

func (s *Server) handleMyOrderTracker(w http.ResponseWriter, r *http.Request) {
	customerID, _ := customerOf(r)
	var req customerTrackerOrderRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	// O associado de uma associação ou seguradora: quem inclui o veículo é ela.
	if user, err := s.Auth.GetUser(r.Context(), customerID); err != nil {
		handleStoreError(w, err, "cliente não encontrado")
		return
	} else if user.PaidByOrganization {
		writeError(w, http.StatusForbidden, "para incluir um veículo, fale com quem contratou o seu rastreamento")
		return
	}
	// Quem só acompanhava o veículo de outra pessoa aceita o contrato ao
	// pedir o primeiro rastreador.
	if s.Contract != nil {
		accepted, err := s.Contract.HasAccepted(r.Context(), customerID)
		if err != nil {
			handleStoreError(w, err, "")
			return
		}
		if !accepted {
			writeContractRequired(w)
			return
		}
	}

	order, err := s.Orders.CustomerOrder(r.Context(), customerID, req.Vehicle, req.LaunchPromo, req.EquipmentMode, req.FarboInstall)
	if err != nil {
		writeOrderError(w, r, err, "cliente não encontrado")
		return
	}
	order.Installments = req.Installments
	// O frete: com o Melhor Envios ligado e o endereço salvo, o cliente
	// escolhe a entrega e paga junto com o equipamento — ou, perto da base,
	// combina a entrega com a central, sem frete.
	if s.Fulfillment != nil && s.Fulfillment.ShippingEnabled() {
		address, err := s.Addresses.Get(r.Context(), customerID)
		if err != nil {
			handleStoreError(w, err, "")
			return
		}
		if address != nil {
			switch {
			case req.ArrangeDelivery && req.ShippingServiceID > 0:
				writeError(w, http.StatusBadRequest, "escolha só uma forma de entrega")
				return
			case req.ArrangeDelivery:
				order.Shipping, err = s.Fulfillment.Arrange(address.City, address.State)
			case req.ShippingServiceID <= 0:
				writeError(w, http.StatusBadRequest, "escolha a forma de entrega")
				return
			default:
				order.Shipping, err = s.chooseShipping(r.Context(), customerID, req.ShippingServiceID)
			}
			if err != nil {
				writeShippingError(w, err)
				return
			}
		}
	}
	result, err := s.Orders.Place(r.Context(), customerID, order)
	if err != nil {
		writeOrderError(w, r, err, "cliente não encontrado")
		return
	}
	s.orderPlaced(r, customerID.String(), result, "CLIENTE")
	writeJSON(w, http.StatusCreated, result)
}

// shippingQuoteView é a cotação do frete para a tela do pedido.
type shippingQuoteView struct {
	// Enabled: o frete é cotado (Melhor Envios ligado); sem isso, o pedido
	// segue sem frete.
	Enabled bool                `json:"enabled"`
	ZipCode string              `json:"zipCode"`
	Quotes  []melhorenvio.Quote `json:"quotes"`
	// Problem: o que impediu a cotação (sem endereço, Melhor Envios fora).
	Problem string `json:"problem"`
	// Arrange: o endereço fica onde dá para combinar a entrega com a
	// central (vale mesmo com o Melhor Envios fora).
	Arrange bool `json:"arrange"`
}

// shippingQuote cota o frete até o endereço de entrega do cliente.
func (s *Server) shippingQuote(r *http.Request, customerID uuid.UUID) shippingQuoteView {
	out := shippingQuoteView{Quotes: []melhorenvio.Quote{}}
	if s.Fulfillment == nil || !s.Fulfillment.ShippingEnabled() {
		return out
	}
	out.Enabled = true
	address, err := s.Addresses.Get(r.Context(), customerID)
	if err != nil {
		out.Problem = "não deu para ler o endereço de entrega"
		return out
	}
	if address == nil {
		out.Problem = "cadastre o endereço de entrega para calcular o frete"
		return out
	}
	out.ZipCode = address.ZipCode
	out.Arrange = s.Fulfillment.CanArrange(address.City, address.State)
	quotes, err := s.Fulfillment.QuoteTo(r.Context(), address.ZipCode)
	if err != nil {
		s.Log.Warn("falha ao cotar o frete do pedido", "customer", customerID, "err", err)
		out.Problem = shippingProblem(err)
		return out
	}
	if len(quotes) == 0 {
		out.Problem = "nenhuma transportadora atende este CEP"
	}
	out.Quotes = quotes
	return out
}

// shippingProblem traduz a falha da cotação para a tela.
func shippingProblem(err error) string {
	var rule fulfillment.RuleError
	if errors.As(err, &rule) {
		return rule.Message
	}
	return "não deu para calcular o frete agora; tente de novo em instantes"
}

// chooseShipping acha a forma de entrega escolhida, cotada de novo para o
// endereço do cliente (o preço é o do servidor).
func (s *Server) chooseShipping(ctx context.Context, customerID uuid.UUID, serviceID int) (*fulfillment.Choice, error) {
	address, err := s.Addresses.Get(ctx, customerID)
	if err != nil {
		return nil, err
	}
	if address == nil {
		return nil, orders.ErrAddressRequired
	}
	return s.Fulfillment.Choose(ctx, address.ZipCode, serviceID)
}

func writeShippingError(w http.ResponseWriter, err error) {
	var rule fulfillment.RuleError
	switch {
	case errors.Is(err, orders.ErrAddressRequired):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error(), "code": codeAddressRequired})
	case errors.As(err, &rule):
		writeError(w, http.StatusBadRequest, rule.Message)
	case errors.Is(err, fulfillment.ErrShippingDisabled):
		writeError(w, http.StatusBadRequest, "o frete não está configurado")
	default:
		writeError(w, http.StatusServiceUnavailable, "não deu para calcular o frete agora; tente de novo em instantes")
	}
}

// handleMyShippingQuote: as formas de entrega para o pedido do cliente.
func (s *Server) handleMyShippingQuote(w http.ResponseWriter, r *http.Request) {
	customerID, _ := customerOf(r)
	writeJSON(w, http.StatusOK, s.shippingQuote(r, customerID))
}

// handleCustomerShippingQuote: as formas de entrega no pedido feito pela central.
func (s *Server) handleCustomerShippingQuote(w http.ResponseWriter, r *http.Request) {
	customerID, ok := s.customerFromURL(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, s.shippingQuote(r, customerID))
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
	if n := result.Subscription.Installments; n > 0 {
		meta["installments"], meta["commitmentUntil"] = n, result.Subscription.CommitmentUntil
	}
	if result.Shipping != nil {
		meta["shippingService"] = result.Shipping.Name
		meta["shippingCents"] = result.Shipping.PriceCents
		if result.Shipping.Arranged {
			meta["deliveryArranged"] = true
		}
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

// handleSetCustomerPlan define o plano do cliente: todo veículo novo dele
// sai nesse plano (as assinaturas que já existem não mudam).
func (s *Server) handleSetCustomerPlan(w http.ResponseWriter, r *http.Request) {
	customerID, ok := s.customerFromURL(w, r)
	if !ok {
		return
	}
	var in orders.PlanInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	plan, err := s.Orders.SetAccountPlan(r.Context(), customerID, in, actor(r))
	if err != nil {
		writeOrderError(w, r, err, "cliente não encontrado")
		return
	}
	s.recordBillingAudit(r, audit.ActionCustomerUpdated, map[string]any{
		"customerId": customerID, "plan": plan.PlanName, "priceCents": plan.PriceCents, "dueDay": plan.DueDay,
		"equipmentMode": plan.EquipmentMode, "installFeeCents": plan.InstallFeeCents, "uninstallFeeCents": plan.UninstallFeeCents,
	})
	writeJSON(w, http.StatusOK, plan)
}

// handleClearCustomerPlan volta o cliente ao plano padrão.
func (s *Server) handleClearCustomerPlan(w http.ResponseWriter, r *http.Request) {
	customerID, ok := s.customerFromURL(w, r)
	if !ok {
		return
	}
	if err := s.Orders.ClearAccountPlan(r.Context(), customerID); err != nil {
		handleStoreError(w, err, "cliente não encontrado")
		return
	}
	s.recordBillingAudit(r, audit.ActionCustomerUpdated, map[string]any{"customerId": customerID, "plan": "padrão"})
	w.WriteHeader(http.StatusNoContent)
}

// handleGrantLaunchPromo libera a promoção de pré-lançamento para um cliente
// que não se inscreveu na lista (devolve a situação nova).
func (s *Server) handleGrantLaunchPromo(w http.ResponseWriter, r *http.Request) {
	s.changeLaunchPromo(w, r, true)
}

// handleRevokeLaunchPromo retira a liberação (a vaga já usada continua).
func (s *Server) handleRevokeLaunchPromo(w http.ResponseWriter, r *http.Request) {
	s.changeLaunchPromo(w, r, false)
}

func (s *Server) changeLaunchPromo(w http.ResponseWriter, r *http.Request, grant bool) {
	customerID, ok := s.customerFromURL(w, r)
	if !ok {
		return
	}
	var err error
	action := audit.ActionLaunchPromoRevoked
	if grant {
		action = audit.ActionLaunchPromoGranted
		err = s.Orders.GrantPromo(r.Context(), customerID, actor(r))
	} else {
		err = s.Orders.RevokePromo(r.Context(), customerID)
	}
	if err != nil {
		handleStoreError(w, err, "cliente não encontrado")
		return
	}
	s.recordBillingAudit(r, action, map[string]any{"customerId": customerID})
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

// handleOrderOptions: as opções do pedido do cliente (o comodato e a
// instalação pelos instaladores da Farbo).
func (s *Server) handleOrderOptions(w http.ResponseWriter, r *http.Request) {
	opts, err := s.Orders.Options(r.Context())
	if err != nil {
		handleStoreError(w, err, "opções do pedido não encontradas")
		return
	}
	writeJSON(w, http.StatusOK, opts)
}

func (s *Server) handleSetOrderOptions(w http.ResponseWriter, r *http.Request) {
	var in orders.Options
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	opts, err := s.Orders.SetOptions(r.Context(), in, actor(r))
	if err != nil {
		writeOrderError(w, r, err, "opções do pedido não encontradas")
		return
	}
	meta := map[string]any{"loanEnabled": opts.Loan.Enabled, "loanSurchargeCents": opts.Loan.SurchargeCents,
		"loanInstallFeeCents": opts.Loan.InstallFeeCents, "loanUninstallFeeCents": opts.Loan.UninstallFeeCents,
		"installEnabled": opts.Install.Enabled, "installCents": opts.Install.PriceCents,
		"checkoutEnabled": opts.Checkout.Enabled, "checkoutPlanCents": opts.Checkout.PlanCents,
		"checkoutInsanosCents": opts.Checkout.InsanosCents, "checkoutEquipmentCents": opts.Checkout.EquipmentCents}
	s.recordBillingAudit(r, audit.ActionOrderOptionsChanged, meta)
	writeJSON(w, http.StatusOK, opts)
}
