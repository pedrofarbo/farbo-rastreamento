package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/addresses"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/contract"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/fulfillment"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/leads"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/melhorenvio"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/orders"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/payments"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/vehicles"
)

// A contratação rápida pela landing (/api/public/checkout): a pessoa física
// escolhe o rastreador (incluso ou em comodato), informa o veículo, os dados e o endereço,
// aceita o contrato e paga com Pix na hora — a conta, o endereço, o aceite e
// o pedido saem de uma vez, e a página de pagamento do pedido abre em
// seguida, já com a sessão do app.

// checkoutView é o que o funil oferece.
type checkoutView struct {
	Enabled bool `json:"enabled"`
	// PlanCents é a mensalidade do site, com o rastreador incluso.
	PlanName      string `json:"planName"`
	PlanCents     int    `json:"planCents"`
	EquipmentName string `json:"equipmentName"`
	// O preço normal do plano (do catálogo), que as páginas mostram riscado
	// ao lado do do site — e cobrado com a contratação pelo site fechada.
	PlanRegularCents int `json:"planRegularCents"`
	// PlanByKind: pelo link de um afiliado com tabela (?ref=), a mensalidade
	// por tipo de veículo, no lugar de PlanCents (nula: sem tabela).
	PlanByKind *vehicles.KindPrices `json:"planByKind"`
	// Tracker: o rastreador incluso — a parte dele nas primeiras
	// mensalidades, somada à mensalidade pela tabela do afiliado (Added) ou já
	// dentro do preço do site; encerrada antes, o que falta vence de uma vez.
	Tracker orders.Tracker `json:"tracker"`
	DueDay  int            `json:"dueDay"`
	// Loan: o comodato (nulo: só o incluso); Install: a instalação pelos
	// instaladores da Farbo no rastreador incluso (nula: só por conta própria).
	Loan    *orders.LoanOffer      `json:"loan"`
	Install *orders.InstallService `json:"install"`
	// InstallDirect: pelo link de um representante, ele instala, desinstala e
	// recebe direto do cliente — os preços de Install e Loan (as taxas) ficam
	// fora do Pix.
	InstallDirect bool `json:"installDirect"`
	// EquipmentModes: o rastreador que o link oferece — vazio, incluso e
	// comodato; sale, só o incluso (Loan nulo); loan, só comodato.
	EquipmentModes string `json:"equipmentModes"`
	// HandDelivery: o representante do link entrega em mãos, com os kits
	// dele, sem frete (vazio: a entrega de sempre).
	HandDelivery string `json:"handDelivery"`
	// ContractVersion: a versão que o aceite confirma.
	ContractVersion string `json:"contractVersion"`
	ShippingEnabled bool   `json:"shippingEnabled"`
	// Periods: o semestral e o anual (o valor do período, com o rastreador
	// incluso), quando o link do representante — ou o site — os tem ligados;
	// vazio, só o mensal.
	Periods []periodOffer `json:"periods"`
}

// periodOffer é um plano pago por período: os meses, o nome e o valor.
type periodOffer struct {
	Months   int    `json:"months"`
	PlanName string `json:"planName"`
	Cents    int    `json:"cents"`
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
		EquipmentName: c.EquipmentName, PlanRegularCents: c.PlanPriceCents, DueDay: c.DefaultDueDay,
		Tracker:         orders.Tracker{Cents: opts.Tracker.Cents, Months: opts.Tracker.Months},
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
	if affiliate := s.referralFor(r, r.URL.Query().Get("ref")); affiliate != nil {
		if view.PlanByKind, err = s.Orders.AffiliatePrices(r.Context(), *affiliate); err != nil {
			handleStoreError(w, err, "contratação indisponível")
			return
		}
		// Pela tabela do afiliado, a parte do rastreador soma nas primeiras.
		view.Tracker.Added = view.PlanByKind != nil
		install, err := s.Orders.AffiliateInstall(r.Context(), *affiliate)
		if err != nil {
			handleStoreError(w, err, "contratação indisponível")
			return
		}
		if install != nil {
			// O comodato (já em view.Loan, se ligado) e a instalação, no preço dele.
			install.WithInstall(&opts.Install, &opts.Loan)
			view.Install, view.InstallDirect = &opts.Install, install.Mode == orders.InstallDirect
		}
		if view.EquipmentModes, err = s.Orders.AffiliateEquipment(r.Context(), *affiliate); err != nil {
			handleStoreError(w, err, "contratação indisponível")
			return
		}
		view.Loan = orders.WithEquipment(view.EquipmentModes, &opts.Loan)
		if view.HandDelivery, err = s.Orders.AffiliateHandDelivery(r.Context(), *affiliate); err != nil {
			handleStoreError(w, err, "contratação indisponível")
			return
		}
	}
	// O semestral e o anual: os do link do representante, ou os do site (só
	// com o rastreador incluso: o link só de comodato não os tem).
	if view.EquipmentModes != orders.EquipmentLoanOnly {
		recurrence, err := s.Orders.LinkRecurrence(r.Context(), opts, s.referralFor(r, r.URL.Query().Get("ref")))
		if err != nil {
			handleStoreError(w, err, "contratação indisponível")
			return
		}
		for _, months := range []int{billing.PeriodSemiannual, billing.PeriodAnnual} {
			if cents := recurrence.PeriodCents(months); cents > 0 {
				view.Periods = append(view.Periods, periodOffer{Months: months, PlanName: orders.PeriodPlanName(months), Cents: cents})
			}
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, view)
}

// checkoutShipping é a cotação do frete até o endereço informado (pelo link
// do representante que entrega em mãos, handBy: sem frete).
func (s *Server) checkoutShipping(ctx context.Context, address addresses.Address, handBy string) shippingQuoteView {
	if handBy != "" {
		return handQuote(address.ZipCode, handBy)
	}
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
	handBy, err := s.linkHandDelivery(r, r.URL.Query().Get("ref"))
	if err != nil {
		handleStoreError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, s.checkoutShipping(r.Context(), address, handBy))
}

// linkHandDelivery é o representante do link que entrega em mãos (vazio: o
// link não é de um, ou não há link).
func (s *Server) linkHandDelivery(r *http.Request, code string) (string, error) {
	affiliate := s.referralFor(r, code)
	if affiliate == nil {
		return "", nil
	}
	return s.Orders.AffiliateHandDelivery(r.Context(), *affiliate)
}

// checkoutRequest é a contratação: a escolha, o veículo, quem contrata, o
// endereço, a entrega e o aceite do contrato.
type checkoutRequest struct {
	// Insanos: o plano do Insanos MC, das telas antigas. Agora só a central
	// define esse plano (no cliente): marcado, a contratação é recusada.
	Insanos       bool   `json:"insanos"`
	EquipmentMode string `json:"equipmentMode"`
	// PeriodMonths: o semestral (6) ou o anual (12); vazio, o mensal.
	PeriodMonths int `json:"periodMonths"`
	// Installments: o parcelamento do rastreador comprado, das telas antigas.
	// O rastreador vem incluso: mais de uma vez, a contratação é recusada.
	Installments int            `json:"installments"`
	FarboInstall bool           `json:"farboInstall"`
	Vehicle      vehicles.Input `json:"vehicle"`
	Name         string         `json:"name"`
	Email        string         `json:"email"`
	Phone        string         `json:"phone"`
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
	case req.Insanos:
		writeError(w, http.StatusBadRequest, "o plano do Insanos MC é feito só pela central: fale com a gente pelo WhatsApp")
		return
	case req.Installments > 1:
		writeError(w, http.StatusBadRequest, "o rastreador vem incluso na mensalidade: não há o que parcelar. Atualize a página")
		return
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
		EquipmentMode: req.EquipmentMode, FarboInstall: req.FarboInstall, Vehicle: req.Vehicle,
		Affiliate: s.referralFor(r, req.Ref), PeriodMonths: req.PeriodMonths,
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
	// Pelo link do representante que entrega em mãos: sem frete.
	handBy, err := s.linkHandDelivery(r, req.Ref)
	if err != nil {
		handleStoreError(w, err, "contratação indisponível")
		return
	}
	if handBy != "" {
		order.Shipping = fulfillment.HandDelivery(handBy)
	} else if s.Fulfillment != nil {
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

	newUser := auth.NewUser{
		Email: req.Email, Name: req.Name, Role: auth.RoleCustomer, Password: req.Password, Phone: req.Phone, Document: document,
	}
	user, err := s.Auth.Register(ctx, newUser)
	if errors.Is(err, database.ErrConflict) {
		// O e-mail de uma contratação pelo site que não foi paga: vencido o
		// pagamento, o cadastro antigo sai e esta segue; ainda valendo, a
		// pessoa paga o pedido que já fez.
		// A senha da conta diz que é a mesma pessoa recomeçando.
		owner := uuid.Nil
		if u, aerr := s.Auth.Authenticate(ctx, req.Email, req.Password); aerr == nil {
			owner = u.ID
		}
		unpaid, uerr := s.Orders.ReleaseUnpaidCheckout(ctx, req.Email, owner, time.Now())
		switch {
		case uerr != nil:
			s.Log.Error("contratação pelo site: falha ao conferir a contratação sem pagamento do e-mail", "err", uerr)
		case unpaid != nil && unpaid.Released:
			user, err = s.Auth.Register(ctx, newUser)
		case unpaid != nil:
			s.writeUnpaidCheckout(w, unpaid, owner == unpaid.CustomerID)
			return
		}
	}
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
	// A conta só vira cliente quando a contratação for paga: até lá, não
	// aparece como cliente (checkout_pending) e, vencido o pagamento sem
	// pagar, sai (orders.PurgeUnpaidCheckouts).
	if _, err := s.DB.Exec(ctx, `UPDATE users SET checkout_at = NOW() WHERE id = $1`, user.ID); err != nil {
		discard(err)
		handleStoreError(w, err, "")
		return
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

// writeUnpaidCheckout: o e-mail tem uma contratação pelo site esperando o
// pagamento, que ainda vale. Com a senha dela (owner), vai o link para pagar
// o pedido já feito; sem, só o aviso (o link mostraria o pedido de outra
// pessoa a quem só sabe o e-mail).
func (s *Server) writeUnpaidCheckout(w http.ResponseWriter, unpaid *orders.UnpaidCheckout, owner bool) {
	until := unpaid.PayableUntil.In(time.FixedZone("BRT", -3*60*60)).Format("15:04 de 02/01")
	out := map[string]any{"code": "CHECKOUT_UNPAID", "payableUntil": unpaid.PayableUntil}
	if owner && s.Dunning != nil && unpaid.InvoiceID != uuid.Nil {
		out["payUrl"] = s.Dunning.PayURL(unpaid.InvoiceID) + "?todas=1"
		out["error"] = "você já contratou com este e-mail e o pagamento vale até " + until +
			": pague o pedido que já fez ou, depois desse horário, contrate de novo"
	} else {
		out["error"] = "este e-mail tem uma contratação esperando o pagamento até " + until +
			": entre no app com a senha dela para pagar ou, depois desse horário, contrate de novo"
	}
	s.Log.Info("contratação pelo site: o e-mail já tem uma contratação esperando o pagamento", "customer", unpaid.CustomerID)
	writeJSON(w, http.StatusConflict, out)
}

// checkoutLead registra a contratação como pré-cliente já convertido (a
// origem, o plano e o afiliado do link, que fica com o cliente).
func (s *Server) checkoutLead(r *http.Request, user *auth.User, address addresses.Address, req checkoutRequest) {
	if s.Leads == nil {
		return
	}
	ctx := r.Context()
	plan := "Contratação pelo site"
	vehicleType := "carro"
	switch strings.ToUpper(req.Vehicle.Kind) {
	case vehicles.KindMotorcycle:
		vehicleType = "moto"
	case vehicles.KindTruck:
		vehicleType = "caminhao"
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
