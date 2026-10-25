package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/addresses"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/contract"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/leads"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/melhorenvio"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/orders"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/payments"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/vehicles"
)

// A contratação rápida pela landing (/api/public/checkout): a pessoa física
// escolhe o plano e o rastreador, informa o veículo, os dados e o endereço,
// aceita o contrato e paga com Pix na hora — a conta, o endereço, o aceite e
// o pedido saem de uma vez, e a página de pagamento do pedido abre em
// seguida, já com a sessão do app.

// checkoutView é o que o funil oferece.
type checkoutView struct {
	Enabled         bool   `json:"enabled"`
	PlanName        string `json:"planName"`
	PlanCents       int    `json:"planCents"`
	InsanosPlanName string `json:"insanosPlanName"`
	InsanosCents    int    `json:"insanosCents"`
	EquipmentName   string `json:"equipmentName"`
	EquipmentCents  int    `json:"equipmentCents"`
	// Os preços normais (do catálogo), que as páginas mostram riscados ao
	// lado dos do site — e cobrados com a contratação pelo site fechada.
	PlanRegularCents      int `json:"planRegularCents"`
	InsanosRegularCents   int `json:"insanosRegularCents"`
	EquipmentRegularCents int `json:"equipmentRegularCents"`
	// MaxInstallments: em até quantas vezes sem juros o rastreador comprado
	// pode ser parcelado (1: só à vista).
	MaxInstallments int `json:"maxInstallments"`
	DueDay          int `json:"dueDay"`
	// Loan: o comodato (nulo: só comprado); Install: a instalação pelos
	// instaladores da Farbo no rastreador comprado (nula: só por conta própria).
	Loan    *orders.LoanOffer      `json:"loan"`
	Install *orders.InstallService `json:"install"`
	// ContractVersion: a versão que o aceite confirma.
	ContractVersion string `json:"contractVersion"`
	ShippingEnabled bool   `json:"shippingEnabled"`
}

func (s *Server) handleCheckoutOffer(w http.ResponseWriter, r *http.Request) {
	opts, err := s.Orders.Options(r.Context())
	if err != nil {
		handleStoreError(w, err, "contratação indisponível")
		return
	}
	c := s.Orders.Catalog()
	view := checkoutView{
		Enabled: opts.Checkout.Enabled, PlanName: c.PlanName, PlanCents: opts.Checkout.PlanCents,
		InsanosPlanName: c.LaunchPromo.InsanosPlanName, InsanosCents: opts.Checkout.InsanosCents,
		EquipmentName: c.EquipmentName, EquipmentCents: opts.Checkout.EquipmentCents,
		PlanRegularCents: c.PlanPriceCents, InsanosRegularCents: c.LaunchPromo.InsanosPlanCents,
		EquipmentRegularCents: c.EquipmentPriceCents,
		MaxInstallments:       c.EquipmentMaxInstallments, DueDay: c.DefaultDueDay,
		ShippingEnabled: s.Fulfillment != nil && s.Fulfillment.ShippingEnabled(),
	}
	if opts.Loan.Enabled {
		view.Loan = &opts.Loan
	}
	if opts.Install.Enabled {
		view.Install = &opts.Install
	}
	if s.Contract != nil {
		view.ContractVersion = s.Contract.Current().Version
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, view)
}

// checkoutShipping é a cotação do frete até o endereço informado.
func (s *Server) checkoutShipping(ctx context.Context, address addresses.Address) shippingQuoteView {
	out := shippingQuoteView{Quotes: []melhorenvio.Quote{}, ZipCode: address.ZipCode}
	if s.Fulfillment == nil {
		return out
	}
	out.Arrange = s.Fulfillment.CanArrange(address.City, address.State)
	if !s.Fulfillment.ShippingEnabled() {
		return out
	}
	out.Enabled = true
	quotes, err := s.Fulfillment.QuoteTo(ctx, address.ZipCode)
	if err != nil {
		s.Log.Warn("falha ao cotar o frete da contratação pelo site", "zip", address.ZipCode, "err", err)
		out.Problem = shippingProblem(err)
		return out
	}
	if len(quotes) == 0 {
		out.Problem = "nenhuma transportadora atende este CEP"
	}
	out.Quotes = quotes
	return out
}

func (s *Server) handleCheckoutShipping(w http.ResponseWriter, r *http.Request) {
	var address addresses.Address
	if err := decodeJSON(w, r, &address); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	address, err := addresses.Normalize(address)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.checkoutShipping(r.Context(), address))
}

// checkoutRequest é a contratação: a escolha, o veículo, quem contrata, o
// endereço, a entrega e o aceite do contrato.
type checkoutRequest struct {
	Insanos       bool           `json:"insanos"`
	EquipmentMode string         `json:"equipmentMode"`
	Installments  int            `json:"installments"`
	FarboInstall  bool           `json:"farboInstall"`
	Vehicle       vehicles.Input `json:"vehicle"`
	Name          string         `json:"name"`
	Email         string         `json:"email"`
	Phone         string         `json:"phone"`
	// Document: o CPF (a contratação pelo site é de pessoa física).
	Document string            `json:"document"`
	Password string            `json:"password"`
	Address  addresses.Address `json:"address"`
	// ShippingServiceID: a entrega escolhida (com o frete ligado);
	// ArrangeDelivery: combinar a entrega com a central (perto da base).
	ShippingServiceID int  `json:"shippingServiceId"`
	ArrangeDelivery   bool `json:"arrangeDelivery"`
	// ContractVersion e Accept: o aceite do contrato em vigor.
	ContractVersion string `json:"contractVersion"`
	Accept          bool   `json:"accept"`
	// Ref: o código do afiliado do link; Event: o evento do QR Code de onde a
	// pessoa veio; Website: a isca dos robôs.
	Ref     string `json:"ref"`
	Event   string `json:"event"`
	Website string `json:"website"`
}

// checkoutResult é o pedido feito, o link do pagamento e a sessão do app.
type checkoutResult struct {
	*orders.Result
	// PayURL abre o Pix de todo o pedido (sem login).
	PayURL string       `json:"payUrl"`
	Tokens *auth.Tokens `json:"tokens"`
}

func (s *Server) handleCheckout(w http.ResponseWriter, r *http.Request) {
	var req checkoutRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	if req.Website != "" {
		s.Log.Info("contratação pelo site descartada (isca preenchida)", "ip", clientIP(r))
		writeError(w, http.StatusBadRequest, "não deu para concluir agora")
		return
	}
	ctx := r.Context()
	document := payments.Digits(req.Document)
	switch {
	case !req.Accept:
		writeError(w, http.StatusBadRequest, "leia e aceite o contrato para contratar")
		return
	case len(document) == 14:
		writeError(w, http.StatusBadRequest, "a contratação pelo site é para pessoa física: para empresa (CNPJ), fale com a gente")
		return
	case len(document) != 11 || !payments.ValidTaxID(document):
		writeError(w, http.StatusBadRequest, "CPF inválido: confira os números")
		return
	case strings.TrimSpace(req.Name) == "":
		writeError(w, http.StatusBadRequest, "informe o seu nome")
		return
	case len(req.Password) < 8:
		writeError(w, http.StatusBadRequest, "a senha precisa de pelo menos 8 caracteres")
		return
	}
	address, err := addresses.Normalize(req.Address)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// O pedido e a entrega conferidos antes de criar a conta: um erro aqui
	// não deixa conta pela metade.
	order, err := s.Orders.CheckoutOrder(ctx, orders.CheckoutChoice{
		Insanos: req.Insanos, EquipmentMode: req.EquipmentMode, Installments: req.Installments,
		FarboInstall: req.FarboInstall, Vehicle: req.Vehicle,
	})
	if err != nil {
		writeOrderError(w, r, err, "contratação indisponível")
		return
	}
	if strings.TrimSpace(order.Vehicle.Name) == "" {
		order.Vehicle.Name = strings.TrimSpace(strings.Join([]string{order.Vehicle.Brand, order.Vehicle.Model}, " "))
	}
	if strings.TrimSpace(order.Vehicle.Name) == "" {
		writeError(w, http.StatusBadRequest, "informe o veículo")
		return
	}
	if s.Fulfillment != nil {
		switch {
		case req.ArrangeDelivery && req.ShippingServiceID > 0:
			writeError(w, http.StatusBadRequest, "escolha só uma forma de entrega")
			return
		case req.ArrangeDelivery:
			order.Shipping, err = s.Fulfillment.Arrange(address.City, address.State)
		case req.ShippingServiceID > 0:
			order.Shipping, err = s.Fulfillment.Choose(ctx, address.ZipCode, req.ShippingServiceID)
		case s.Fulfillment.ShippingEnabled():
			writeError(w, http.StatusBadRequest, "escolha a forma de entrega")
			return
		}
		if err != nil {
			writeShippingError(w, err)
			return
		}
	}
	if s.Contract != nil && req.ContractVersion != s.Contract.Current().Version {
		writeError(w, http.StatusBadRequest, "o contrato foi atualizado: recarregue a página para ler a versão nova")
		return
	}

	user, err := s.Auth.Register(ctx, auth.NewUser{
		Email: req.Email, Name: req.Name, Role: auth.RoleCustomer, Password: req.Password, Phone: req.Phone, Document: document,
	})
	if errors.Is(err, database.ErrConflict) {
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": "este e-mail já tem conta: entre no app e use Novo veículo (ou Esqueci a senha)", "code": "EMAIL_TAKEN",
		})
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Daqui em diante, uma falha desfaz a conta (o pedido tem a transação dele).
	discard := func(cause error) {
		if _, err := s.DB.Exec(context.WithoutCancel(ctx), `DELETE FROM users WHERE id = $1`, user.ID); err != nil {
			s.Log.Error("contratação pelo site falhou e a conta não foi desfeita", "user", user.ID, "cause", cause, "err", err)
		}
	}
	if _, err := s.Addresses.Save(ctx, user.ID, address); err != nil {
		discard(err)
		handleStoreError(w, err, "")
		return
	}
	if s.Contract != nil {
		if _, err := s.Contract.Accept(ctx, user.ID, req.ContractVersion, document, clientIP(r), r.UserAgent()); err != nil {
			discard(err)
			var rule contract.RuleError
			if errors.As(err, &rule) {
				writeError(w, http.StatusBadRequest, rule.Message)
				return
			}
			handleStoreError(w, err, "")
			return
		}
	}
	result, err := s.Orders.Place(ctx, user.ID, order)
	if err != nil {
		discard(err)
		writeOrderError(w, r, err, "contratação indisponível")
		return
	}
	s.checkoutLead(r, user, address, req)
	s.orderPlaced(r, user.ID.String(), result, "SITE")

	out := checkoutResult{Result: result}
	if s.Dunning != nil {
		for _, inv := range []*billing.Invoice{result.FirstInvoice, result.SetupInvoice, result.InstallInvoice} {
			if inv != nil && inv.Status == billing.InvoiceOpen {
				out.PayURL = s.Dunning.PayURL(inv.ID) + "?todas=1"
				break
			}
		}
	}
	if out.Tokens, err = s.Auth.LoginVerified(ctx, user.ID, r.UserAgent()); err != nil {
		s.Log.Warn("contratação pelo site feita, mas a sessão do app não abriu", "user", user.ID, "err", err)
	}
	writeJSON(w, http.StatusCreated, out)
}

// checkoutLead registra a contratação como pré-cliente já convertido (a
// origem, o plano e o afiliado do link, que fica com o cliente).
func (s *Server) checkoutLead(r *http.Request, user *auth.User, address addresses.Address, req checkoutRequest) {
	if s.Leads == nil {
		return
	}
	ctx := r.Context()
	plan := "Contratação pelo site"
	if req.Insanos {
		plan += " · Insanos MC"
	}
	vehicleType := "carro"
	if req.Vehicle.Kind == vehicles.KindMotorcycle {
		vehicleType = "moto"
	}
	message := "Contratou pelo site, com Pix."
	if event := leads.EventSlug(req.Event); event != "" {
		message = "Contratou pelo site, com Pix, vindo do evento " + event + "."
	}
	lead, err := s.Leads.Submit(ctx, leads.Input{
		Name: user.Name, Email: user.Email, Phone: user.Phone, City: address.City, Plan: plan, VehicleType: vehicleType,
		VehicleCount: 1, Message: message, Consent: true, AffiliateID: s.referralFor(r, req.Ref),
	})
	if err != nil {
		s.Log.Warn("contratação pelo site sem o pré-cliente", "user", user.ID, "err", err)
		return
	}
	var leadID *uuid.UUID
	if lead != nil {
		leadID = &lead.ID
		if err := s.Leads.Repo().MarkConverted(ctx, lead.ID, user.ID); err != nil {
			s.Log.Warn("pré-cliente da contratação pelo site não marcado como convertido", "lead", lead.ID, "err", err)
		}
	}
	if s.Affiliates != nil {
		if _, err := s.Affiliates.AttachCustomer(ctx, user.ID, user.Email, leadID); err != nil {
			s.Log.Error("contratação pelo site sem o afiliado que indicou", "customer", user.ID, "err", err)
		}
	}
}
