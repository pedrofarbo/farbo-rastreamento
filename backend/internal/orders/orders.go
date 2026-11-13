// Package orders cuida do fluxo único de contratação: 1. veículo, 2. rastreador,
// 3. assinatura. Numa transação só, cadastra o veículo, cria a assinatura dele
// (com a cópia do endereço de entrega e o rastreador incluso ou em comodato)
// e lança o frete e as taxas. A Farbo não vende o rastreador: fora do
// comodato, ele vai incluso e fica com o cliente, e a parte dele está nas
// primeiras mensalidades.
package orders

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/addresses"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/fulfillment"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/vehicles"
)

// maxPending limita quantos rastreadores o cliente pode contratar sozinho
// enquanto os anteriores não foram instalados.
const maxPending = 3

var (
	// ErrHasOverdue: o cliente tem fatura vencida e precisa regularizar antes.
	ErrHasOverdue = errors.New("há fatura vencida em aberto; pague-a antes de contratar outro rastreador")
	// ErrTooManyPending: muitos rastreadores contratados e ainda não instalados
	// (instalado = o aparelho já deu sinal).
	ErrTooManyPending = fmt.Errorf("você já tem %d rastreadores aguardando instalação; "+
		"agende a instalação antes de contratar outro", maxPending)
	// ErrAddressRequired: sem endereço de entrega, não há para onde mandar o
	// aparelho.
	ErrAddressRequired = errors.New("cadastre o endereço de entrega antes de contratar um rastreador")
	// ErrSubscriptionTaken: a assinatura já tem veículo ou foi encerrada.
	ErrSubscriptionTaken = errors.New("esta assinatura já tem veículo ou foi encerrada")
	// ErrVehicleHasSubscription: o veículo já tem assinatura ativa.
	ErrVehicleHasSubscription = errors.New("este veículo já tem assinatura ativa")
)

// ValidationError descreve um pedido recusado; a mensagem vai para a tela.
type ValidationError struct{ Message string }

func (e ValidationError) Error() string { return e.Message }

// Order é o fluxo completo: veículo, rastreador (equipamento e entrega) e
// assinatura. Nada é cobrado na hora: tudo entra na próxima fatura do mês da
// conta (ver billing.ChargeDate), com o proporcional dos dias até ela.
type Order struct {
	Vehicle vehicles.Input
	// Terms: o rastreador incluso ou em comodato e as taxas (o comodato
	// cobra a instalação na fatura e a desinstalação no encerramento).
	Terms Terms
	// Tracker: o rastreador incluso (fora do comodato) — a parte dele nas
	// primeiras mensalidades, somada ao plano ou já dentro do preço. Zero: o
	// aparelho do cliente.
	Tracker Tracker
	// RequireAddress recusa o pedido se o cliente não tiver endereço de
	// entrega. Vale para o pedido do cliente; a central pode lançar sem ele
	// (aparelho instalado na hora, entregue em mãos).
	RequireAddress bool
	Plan           billing.SubscriptionInput
	// LaunchPromo pede a promoção de pré-lançamento: a mensalidade, pelo
	// dela nos primeiros meses. Sem direito, o pedido é recusado com
	// PromoUnavailable.
	LaunchPromo bool
	// Shipping é o frete escolhido (cotado de novo no servidor): vira a
	// fatura do frete e fica no acompanhamento. Nulo: sem frete; na entrega
	// combinada, só fica no acompanhamento.
	Shipping *fulfillment.Choice
	// FarboInstall: o rastreador incluso instalado pelos instaladores da
	// Farbo, por InstallCents (cobrada junto com a 1ª mensalidade); sem isso,
	// o cliente instala por conta própria. No comodato, a instalação é sempre
	// da Farbo, com a taxa dos Terms.
	FarboInstall bool
	InstallCents int
	// InstallBlocking: a instalação com o relé do bloqueio do motor (pelo
	// link do representante que oferece a escolha), no preço dela; fica na
	// assinatura para a agenda e o instalador.
	InstallBlocking bool
	// Coupon: o código do cupom de desconto (a contratação direta pelo
	// site): a mensalidade com o desconto nas primeiras cobranças. Não vai
	// junto com a promoção de pré-lançamento.
	Coupon string
	// Link: o código do link de pagamento de onde veio o pedido (montado
	// pela oferta dele em linkOrder); conferido de novo, travado, na
	// gravação, e o uso fica com a assinatura.
	Link string
}

// Result é o que a contratação criou. Tudo vence em ChargeDate: no ato, para
// a pessoa física; na fatura do mês da conta, para a empresa.
type Result struct {
	Vehicle      *vehicles.Vehicle     `json:"vehicle"`
	Subscription *billing.Subscription `json:"subscription"`
	// ChargeDate é o vencimento da cobrança da contratação.
	ChargeDate billing.Date `json:"chargeDate"`
	// PayNow: a pessoa física paga no ato (a tela abre o pagamento).
	PayNow bool `json:"payNow"`
	// SetupInvoice é a linha do frete; nula sem frete.
	SetupInvoice *billing.Invoice `json:"setupInvoice"`
	// InstallInvoice é a linha da instalação no comodato (nula sem taxa).
	InstallInvoice *billing.Invoice `json:"installInvoice"`
	// ProrataInvoice é a linha dos dias de hoje até o vencimento (nula sem dias).
	ProrataInvoice *billing.Invoice `json:"prorataInvoice"`
	// FirstInvoice é a linha da 1ª mensalidade; nula se a geração falhar (a
	// rotina de faturas a cria depois).
	FirstInvoice *billing.Invoice `json:"firstInvoice"`
	// Shipping é a entrega escolhida (nula: sem frete).
	Shipping *fulfillment.Choice `json:"shipping"`
	// CheckoutLinkID é o link de pagamento de onde veio o pedido (nulo: nenhum).
	CheckoutLinkID *uuid.UUID `json:"checkoutLinkId,omitempty"`
}

type Service struct {
	db       *database.DB
	billing  *billing.Service
	vehicles *vehicles.Service
	catalog  config.Catalog
	log      *slog.Logger
}

func NewService(db *database.DB, billingSvc *billing.Service, vehicleSvc *vehicles.Service, catalog config.Catalog, log *slog.Logger) *Service {
	return &Service{db: db, billing: billingSvc, vehicles: vehicleSvc, catalog: catalog, log: log.With("component", "orders")}
}

// Catalog devolve a tabela de preços.
func (s *Service) Catalog() config.Catalog { return s.catalog }

// lockCustomer trava a linha do cliente: pedidos simultâneos do mesmo
// cliente entram em fila.
func lockCustomer(ctx context.Context, tx pgx.Tx, customerID uuid.UUID) error {
	_, err := tx.Exec(ctx, `SELECT 1 FROM users WHERE id = $1 FOR UPDATE`, customerID)
	return err
}

// Place grava o pedido numa transação: ou sai tudo (veículo, assinatura e
// fatura), ou nada — uma placa repetida, por exemplo, não deixa uma
// assinatura órfã para trás.
func (s *Service) Place(ctx context.Context, customerID uuid.UUID, o Order) (*Result, error) {
	if err := vehicles.Validate(o.Vehicle); err != nil {
		return nil, err
	}
	o.Vehicle.OwnerID = &customerID
	sub, err := s.billing.NewSubscription(customerID, o.Plan)
	if err != nil {
		return nil, err
	}
	if err := o.Terms.check(); err != nil {
		return nil, err
	}
	if err := o.Tracker.check(); err != nil {
		return nil, err
	}
	if o.Coupon != "" && o.LaunchPromo {
		return nil, ValidationError{"o cupom não vale junto com a promoção de pré-lançamento"}
	}
	if o.Link != "" && (o.Coupon != "" || o.LaunchPromo) {
		return nil, ValidationError{"o link de pagamento já tem o valor dele: não vale junto com cupom ou promoção"}
	}
	loan := o.Terms.EquipmentMode == billing.EquipmentLoan
	if loan {
		// O rastreador emprestado volta à Farbo: não há parte dele a pagar.
		o.Tracker = Tracker{}
		sub.EquipmentMode, sub.UninstallFeeCents = billing.EquipmentLoan, o.Terms.UninstallFeeCents
	}

	var setup *billing.Invoice
	// A pessoa física paga no ato; a empresa, na próxima fatura do mês.
	upfront, err := s.PaysUpfront(ctx, customerID)
	if err != nil {
		return nil, err
	}
	// O vencimento sai na transação (o dia da conta e a fatura do mês).
	today := s.billing.Today()
	due := today

	result := &Result{}
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if err := lockCustomer(ctx, tx, customerID); err != nil {
			return err
		}
		// A conta vence num dia só: o veículo novo entra no dia dela.
		if err := s.billing.AlignDueDay(ctx, tx, sub, o.Plan.FirstDueDate != nil); err != nil {
			return err
		}
		// A pessoa física paga no ato: a 1ª mensalidade, o frete e a
		// instalação, quando houver, vencem no dia da contratação (mais o
		// prazo do catálogo), e a 2ª pelo menos um mês depois, no dia da conta. A empresa: nada é cobrado na hora; tudo
		// entra na próxima fatura do mês da conta (a que ainda não foi
		// paga), com o proporcional. Um 1º vencimento combinado prevalece.
		switch {
		case o.Plan.FirstDueDate != nil:
			due = sub.NextDueDate
		case upfront:
			due = today.AddDays(s.catalog.SetupDueDays)
			sub.NextDueDate = due
		default:
			var err error
			if due, err = s.billing.ChargeDate(ctx, tx, customerID, sub.DueDay); err != nil {
				return err
			}
			sub.NextDueDate = due
		}
		result.ChargeDate = due
		if o.LaunchPromo {
			if err := s.applyPromo(ctx, tx, customerID, sub); err != nil {
				return err
			}
		}
		var link *CheckoutLink
		if o.Link != "" {
			var err error
			if link, err = s.useCheckoutLink(ctx, tx, o.Link); err != nil {
				return err
			}
		}
		var coupon *Coupon
		var couponDiscount int
		if o.Coupon != "" {
			var err error
			if coupon, couponDiscount, err = s.applyCoupon(ctx, tx, o.Coupon, sub); err != nil {
				return err
			}
		}
		// O rastreador incluso: a parte dele nas primeiras mensalidades, a
		// partir da 1ª.
		sub.IncludeTracker(o.Tracker.Cents, o.Tracker.Months, o.Tracker.Added)
		// O frete é a única cobrança do rastreador, na fatura do pedido.
		if o.Shipping != nil && o.Vehicle.DeviceID == nil {
			if o.Shipping.PriceCents > 0 {
				var err error
				if setup, err = s.billing.NewInvoice(customerID, billing.InvoiceInput{
					Description: "Frete do rastreador: " + o.Shipping.Name, AmountCents: o.Shipping.PriceCents, DueDate: due,
				}); err != nil {
					return err
				}
			}
			result.Shipping = o.Shipping
		}
		address, err := addresses.GetWith(ctx, tx, customerID)
		if err != nil {
			return err
		}
		if address == nil && o.RequireAddress {
			return ErrAddressRequired
		}
		if result.Vehicle, err = vehicles.InsertWith(ctx, tx, o.Vehicle); err != nil {
			return err
		}
		sub.VehicleID = &result.Vehicle.ID
		// Aparelho já instalado (vinculado no pedido) não é enviado: sem
		// cópia do endereço de entrega.
		if o.Vehicle.DeviceID == nil {
			sub.DeliveryAddress = address
		}
		if result.Subscription, err = billing.InsertSubscription(ctx, tx, sub); err != nil {
			return err
		}
		if coupon != nil {
			if _, err := tx.Exec(ctx, `INSERT INTO coupon_redemptions (subscription_id, coupon_id, customer_id, discount_cents)
				VALUES ($1, $2, $3, $4)`, result.Subscription.ID, coupon.ID, customerID, couponDiscount); err != nil {
				return err
			}
		}
		if link != nil {
			if _, err := tx.Exec(ctx, `INSERT INTO checkout_link_uses (subscription_id, link_id, customer_id) VALUES ($1, $2, $3)`,
				result.Subscription.ID, link.ID, customerID); err != nil {
				return err
			}
			result.CheckoutLinkID = &link.ID
		}
		if o.LaunchPromo {
			// A vaga fica com o cliente; a linha, com esta assinatura (a
			// promoção vale para todos os veículos dele).
			if _, err := tx.Exec(ctx, `INSERT INTO launch_promo_claims (customer_id, subscription_id) VALUES ($1, $2)`,
				customerID, result.Subscription.ID); err != nil {
				return err
			}
		}
		// Sem aparelho instalado na hora, o rastreador (e o chip) passam a ser
		// acompanhados até a casa do cliente.
		if result.Vehicle.DeviceID == nil {
			id, err := fulfillment.Create(ctx, tx, customerID, result.Vehicle.ID, &result.Subscription.ID)
			if err != nil {
				return err
			}
			if result.Shipping != nil {
				if err := fulfillment.RecordChoice(ctx, tx, id, *result.Shipping); err != nil {
					return err
				}
			}
		}
		if setup != nil {
			// A linha do frete é avulsa (sem assinatura): se tivesse a
			// assinatura, a chave única (assinatura, vencimento) impediria a
			// mensalidade do mesmo dia.
			if result.SetupInvoice, err = billing.InsertInvoice(ctx, tx, setup); err != nil {
				return err
			}
		}
		vehicleLabel := result.Vehicle.Name
		if result.Vehicle.Plate != "" {
			vehicleLabel += " · " + result.Vehicle.Plate
		}
		// O comodato: a instalação, na mesma fatura.
		if loan && o.Terms.InstallFeeCents > 0 {
			if result.InstallInvoice, err = billing.InsertInvoice(ctx, tx, &billing.Invoice{
				CustomerID: customerID, Description: "Instalação do rastreador (comodato): " + vehicleLabel,
				AmountCents: o.Terms.InstallFeeCents, DueDate: due,
			}); err != nil {
				return err
			}
		}
		// O rastreador incluso com a instalação da Farbo: a taxa, junto.
		if !loan && o.FarboInstall && o.InstallCents > 0 {
			if result.InstallInvoice, err = billing.InsertInvoice(ctx, tx, &billing.Invoice{
				CustomerID: customerID, Description: "Instalação do rastreador (instaladores Farbo): " + vehicleLabel,
				AmountCents: o.InstallCents, DueDate: due,
			}); err != nil {
				return err
			}
		}
		// Quem instala é a Farbo (o comodato e a instalação contratada): a
		// agenda da central mostra. E a garantia de troca que a mensalidade
		// inclui (o próximo veículo parte do plano sem ela).
		if loan || o.FarboInstall || o.Terms.GuaranteeCents > 0 {
			if _, err := tx.Exec(ctx, `UPDATE subscriptions SET farbo_install = $2, guarantee_cents = $3, install_blocking = $4
				WHERE id = $1`, result.Subscription.ID, loan || o.FarboInstall, o.Terms.GuaranteeCents,
				o.InstallBlocking && (loan || o.FarboInstall)); err != nil {
				return err
			}
		}
		// Na próxima fatura, os dias de hoje até o vencimento, no preço do 1º
		// mês (no ato, a 1ª mensalidade começa hoje).
		if cents, days := billing.Prorata(result.Subscription.PriceOn(due), today, due); cents > 0 && !upfront {
			if result.ProrataInvoice, err = billing.InsertInvoice(ctx, tx, &billing.Invoice{
				CustomerID:  customerID,
				Description: billing.ProrataDescription(result.Subscription.PlanName, today, due, days) + ": " + vehicleLabel,
				AmountCents: cents, DueDate: due,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, database.MapError(err)
	}
	for _, inv := range []*billing.Invoice{result.SetupInvoice, result.InstallInvoice, result.ProrataInvoice} {
		if inv != nil {
			s.billing.Decorate(inv)
		}
	}

	// A linha da 1ª mensalidade sai na hora (vence no dia da fatura), para o
	// cliente já ver a fatura do mês com tudo.
	s.billing.GenerateInvoices(ctx)
	first, err := s.billing.FirstInvoice(ctx, result.Subscription.ID)
	switch {
	case err == nil:
		result.FirstInvoice = first
	case !errors.Is(err, database.ErrNotFound):
		s.log.Warn("pedido feito, mas a 1ª mensalidade não foi lida", "subscription", result.Subscription.ID, "err", err)
	}
	// O pagamento no ato abre na tela quando há o que pagar.
	if upfront {
		for _, inv := range []*billing.Invoice{result.SetupInvoice, result.InstallInvoice, result.FirstInvoice} {
			if inv != nil && inv.Status == billing.InvoiceOpen && inv.AmountCents > 0 {
				result.PayNow = true
			}
		}
	}
	freightCents := 0
	if result.Shipping != nil {
		freightCents = result.Shipping.PriceCents
	}
	s.log.Info("rastreador contratado", "customer", customerID, "vehicle", result.Vehicle.ID,
		"subscription", result.Subscription.ID, "promo", o.LaunchPromo, "coupon", o.Coupon, "freight_cents", freightCents,
		"equipment_mode", o.Terms.EquipmentMode, "tracker_cents", o.Tracker.Cents, "tracker_months", o.Tracker.Months,
		"tracker_added", o.Tracker.Added, "charge_date", result.ChargeDate.String(), "pay_now", result.PayNow)
	return result, nil
}

// applyPromo reserva a vaga da promoção (na transação do pedido) e dá à
// assinatura a mensalidade promocional pelos primeiros meses (a do plano do
// Insanos MC, se for o plano deles).
func (s *Service) applyPromo(ctx context.Context, tx pgx.Tx, customerID uuid.UUID, sub *billing.Subscription) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, promoLockKey); err != nil {
		return err
	}
	p := s.catalog.LaunchPromo
	reason, err := promoCheck(ctx, tx, customerID, p)
	if err != nil {
		return err
	}
	if reason != "" {
		return PromoUnavailable{reason}
	}
	monthly := p.MonthlyFor(sub.PlanName)
	until := billing.Date{Time: sub.NextDueDate.AddDate(0, p.Months, 0)}
	sub.PromoPriceCents, sub.PromoUntil = &monthly, &until
	return nil
}

// OrderTerms é o rastreador do pedido do cliente: o do plano definido pela
// central para ele (sem escolha); sem plano, o que ele escolheu — incluso,
// ou em comodato quando a central oferece (para a pessoa física, a garantia
// de troca do rastreador soma na mensalidade do plano).
func (s *Service) OrderTerms(ctx context.Context, customerID uuid.UUID, equipmentMode string,
	plan *billing.SubscriptionInput, individual bool) (Terms, error) {
	account, err := s.AccountPlan(ctx, customerID)
	if err != nil {
		return Terms{}, err
	}
	if account != nil {
		if equipmentMode != "" && equipmentMode != account.EquipmentMode {
			return Terms{}, ValidationError{"o rastreador já está definido no plano da sua conta: fale com a Farbo para mudar"}
		}
		return account.Terms, nil
	}
	// O rastreador que o afiliado que indicou o cliente oferece: incluso,
	// comodato ou os dois.
	modes, err := s.CustomerEquipment(ctx, customerID)
	if err != nil {
		return Terms{}, err
	}
	if equipmentMode, err = linkEquipment(modes, equipmentMode); err != nil {
		return Terms{}, err
	}
	switch equipmentMode {
	case billing.EquipmentSale:
		return Terms{EquipmentMode: billing.EquipmentSale}, nil
	case billing.EquipmentLoan:
		offer, err := s.Loan(ctx)
		if err != nil {
			return Terms{}, err
		}
		if !offer.Enabled && modes != EquipmentLoanOnly {
			return Terms{}, ValidationError{"o comodato não está disponível agora: escolha o rastreador incluso"}
		}
		terms := Terms{EquipmentMode: billing.EquipmentLoan, InstallFeeCents: offer.InstallFeeCents,
			UninstallFeeCents: offer.UninstallFeeCents}
		// A garantia de troca: a do afiliado que indicou o cliente, se ele
		// tiver uma (as taxas dele vêm depois, no pedido).
		install, err := s.CustomerInstall(ctx, customerID)
		if err != nil {
			return Terms{}, err
		}
		if guarantee := install.Guarantee(offer.SurchargeCents); individual && guarantee > 0 && plan != nil {
			withGuarantee(plan, &terms, guarantee)
		}
		return terms, nil
	}
	return Terms{}, ValidationError{"escolha o rastreador: incluso ou em comodato"}
}

// withGuarantee soma a garantia de troca do rastreador na mensalidade (e no
// nome) do plano.
func withGuarantee(plan *billing.SubscriptionInput, terms *Terms, cents int) {
	plan.PriceCents += cents
	if !strings.HasSuffix(plan.PlanName, GuaranteeName) {
		plan.PlanName += GuaranteeName
	}
	terms.GuaranteeCents = cents
}

// CustomerChoice é o que o cliente escolhe no pedido de um veículo novo pelo
// app: a promoção (quando o painel mostrou), o rastreador, a instalação e,
// pelo representante que o indicou com o semestral e o anual ligados, o
// período.
type CustomerChoice struct {
	LaunchPromo   bool
	EquipmentMode string
	FarboInstall  bool
	// InstallBlocking: a instalação com o relé do bloqueio, quando o
	// representante que o indicou oferece a escolha.
	InstallBlocking bool
	// PeriodMonths: o semestral (6) ou o anual (12); 0 ou 1, o mensal.
	PeriodMonths int
}

// CustomerOrder monta o pedido feito pelo próprio cliente, com os preços dele
// (CustomerPrices) — ele não escolhe valor.
func (s *Service) CustomerOrder(ctx context.Context, customerID uuid.UUID, vehicle vehicles.Input, choice CustomerChoice) (Order, error) {
	summary, err := s.billing.GetCustomer(ctx, customerID)
	if err != nil {
		return Order{}, err
	}
	if summary.OverdueInvoices > 0 {
		return Order{}, ErrHasOverdue
	}
	awaiting, err := s.vehicles.CountAwaitingInstall(ctx, customerID)
	if err != nil {
		return Order{}, err
	}
	if awaiting >= maxPending {
		return Order{}, ErrTooManyPending
	}
	// O cliente não vincula aparelho nem escolhe dono.
	vehicle.DeviceID, vehicle.OwnerID = nil, nil
	return s.conditionsOrder(ctx, customerID, vehicle, choice, false)
}

// ReferralOrder monta o veículo que a central lança para o cliente que veio
// por um representante, nas condições dele, como o cliente pediria pelo app
// (a tabela pelo tipo, o rastreador que ele oferece, a instalação e o
// bloqueio no preço dele, o semestral e o anual): a central não escolhe
// valor. Ela pode vincular o aparelho já instalado, lançar sem o endereço e
// contratar a instalação da Farbo mesmo desligada nas opções do pedido.
func (s *Service) ReferralOrder(ctx context.Context, customerID uuid.UUID, vehicle vehicles.Input, choice CustomerChoice) (Order, error) {
	vehicle.OwnerID = nil
	return s.conditionsOrder(ctx, customerID, vehicle, choice, true)
}

// conditionsOrder é o pedido nos preços e nas condições do cliente
// (CustomerPrices e as do representante que o indicou); central: o pedido
// feito pela central.
func (s *Service) conditionsOrder(ctx context.Context, customerID uuid.UUID, vehicle vehicles.Input, choice CustomerChoice,
	central bool) (Order, error) {
	launchPromo, equipmentMode, farboInstall := choice.LaunchPromo, choice.EquipmentMode, choice.FarboInstall
	plan, tracker, err := s.CustomerPrices(ctx, customerID, vehicle.Kind)
	if err != nil {
		return Order{}, err
	}
	individual, err := s.PaysUpfront(ctx, customerID)
	if err != nil {
		return Order{}, err
	}
	terms, err := s.OrderTerms(ctx, customerID, equipmentMode, &plan, individual)
	if err != nil {
		return Order{}, err
	}
	if terms.EquipmentMode == billing.EquipmentLoan && launchPromo {
		return Order{}, ValidationError{"a promoção de pré-lançamento vale para o rastreador incluso"}
	}

	order := Order{
		Vehicle: vehicle, Tracker: tracker, Terms: terms, RequireAddress: !central, Plan: plan,
		LaunchPromo: launchPromo,
	}
	// A instalação do afiliado que indicou o cliente (sem plano da central):
	// o preço dele, ou fora da plataforma quando o representante recebe.
	install, err := s.CustomerInstall(ctx, customerID)
	if err != nil {
		return Order{}, err
	}
	// O rastreador incluso: instalar por conta própria ou com os
	// instaladores da Farbo (pelo preço das opções do pedido).
	if farboInstall && terms.EquipmentMode == billing.EquipmentSale {
		if err := s.withFarboInstall(ctx, &order, install == nil && !central); err != nil {
			return Order{}, err
		}
	}
	if err := withBlocking(install, choice.InstallBlocking, &order); err != nil {
		return Order{}, err
	}
	// O semestral ou o anual do representante que o indicou: pelo app, só a
	// pessoa física (paga no ato, como no site); pela central, também a
	// empresa. Sem a promoção (que é mensal).
	if choice.PeriodMonths > 1 {
		if err := s.customerPeriod(ctx, customerID, individual || central, choice.PeriodMonths, &order); err != nil {
			return Order{}, err
		}
	}
	install.applyInstall(&order)
	return order, nil
}

// customerPeriod põe no pedido do cliente o semestral ou o anual do
// representante que o indicou.
func (s *Service) customerPeriod(ctx context.Context, customerID uuid.UUID, individual bool, months int, order *Order) error {
	switch {
	case !billing.ValidPeriod(months):
		return ValidationError{"a recorrência é mensal, semestral ou anual"}
	case !individual:
		return ValidationError{"o semestral e o anual pelo app são para pessoa física: fale com a Farbo"}
	case order.LaunchPromo:
		return ValidationError{"a promoção de pré-lançamento é no plano mensal: escolha o mensal ou contrate sem a promoção"}
	}
	recurrence, err := s.CustomerRecurrence(ctx, customerID)
	if err != nil {
		return err
	}
	return applyPeriod(recurrence, months, order)
}

// withFarboInstall põe no pedido a instalação pelos instaladores da Farbo,
// no preço das opções do pedido; offered exige a opção ligada (o pedido do
// cliente; a central contrata mesmo desligada).
func (s *Service) withFarboInstall(ctx context.Context, o *Order, offered bool) error {
	opts, err := s.Options(ctx)
	if err != nil {
		return err
	}
	if offered && !opts.Install.Enabled {
		return ValidationError{"a instalação pelos instaladores da Farbo não está disponível agora: instale por conta própria"}
	}
	o.FarboInstall, o.InstallCents = true, opts.Install.PriceCents
	return nil
}

// WithFarboInstall: a central contrata a instalação da Farbo no pedido dela
// (no rastreador incluso; no comodato, ela já é da Farbo).
func (s *Service) WithFarboInstall(ctx context.Context, o *Order) error {
	if o.Terms.EquipmentMode == billing.EquipmentLoan {
		return nil
	}
	return s.withFarboInstall(ctx, o, false)
}

// CustomerPrices são o plano e o rastreador incluso de um novo veículo do
// cliente, do tipo informado. O cliente indicado por um afiliado com a tabela
// por tipo de veículo paga a mensalidade dela (CustomerKindPrices), mais a
// parte do rastreador nas primeiras. A pessoa física sem plano definido pela
// central paga no app o mesmo que no site: o plano normal (sem a tabela do
// afiliado) e o do Insanos MC no preço da contratação pelo site (com ela
// fechada, os do catálogo). A empresa, e quem tem plano da central ou um
// plano próprio na assinatura, segue com o plano dele. Abaixo do preço do
// site, a parte do rastreador é somada nas primeiras mensalidades (TrackerFor).
func (s *Service) CustomerPrices(ctx context.Context, customerID uuid.UUID, kind string) (billing.SubscriptionInput, Tracker, error) {
	plan, err := s.CustomerPlan(ctx, customerID)
	if err != nil {
		return plan, Tracker{}, err
	}
	opts, err := s.Options(ctx)
	if err != nil {
		return plan, Tracker{}, err
	}
	account, err := s.AccountPlan(ctx, customerID)
	if err != nil || account != nil {
		return plan, s.TrackerFor(opts, plan, false), err
	}
	table, err := s.CustomerKindPrices(ctx, customerID)
	if err != nil {
		return plan, Tracker{}, err
	}
	if table != nil {
		plan.PriceCents = table.For(kind)
	}
	individual, err := s.PaysUpfront(ctx, customerID)
	if err != nil || !individual || !opts.Checkout.Enabled {
		return plan, s.TrackerFor(opts, plan, table != nil), err
	}
	switch insanos := s.catalog.LaunchPromo.InsanosPlanName; {
	case insanos != "" && strings.EqualFold(plan.PlanName, insanos):
		plan.PriceCents = opts.Checkout.InsanosCents
	case plan.PlanName == s.catalog.PlanName:
		if table == nil {
			plan.PriceCents = opts.Checkout.PlanCents
		}
	}
	// Um plano próprio na assinatura (combinado com a central) fica como está.
	return plan, s.TrackerFor(opts, plan, table != nil), nil
}

// CustomerPlan é o plano de um novo veículo do cliente: o que a central
// definiu para ele; sem isso, o da assinatura ativa que ele já tem ou, sem
// nenhuma, o padrão do catálogo. O vencimento é sempre o da conta (o das
// assinaturas ativas), quando ela já tem um.
func (s *Service) CustomerPlan(ctx context.Context, customerID uuid.UUID) (billing.SubscriptionInput, error) {
	plan, err := s.customerPlan(ctx, customerID)
	if err != nil {
		return plan, err
	}
	day, err := s.billing.AccountDueDay(ctx, customerID)
	if err != nil {
		return plan, err
	}
	if day > 0 {
		plan.DueDay = day
	}
	return plan, nil
}

func (s *Service) customerPlan(ctx context.Context, customerID uuid.UUID) (billing.SubscriptionInput, error) {
	account, err := s.AccountPlan(ctx, customerID)
	if err != nil {
		return billing.SubscriptionInput{}, err
	}
	if account != nil {
		return billing.SubscriptionInput{PlanName: account.PlanName, PriceCents: account.PriceCents, DueDay: account.DueDay}, nil
	}
	subscriptions, err := s.billing.ListSubscriptions(ctx, customerID)
	if err != nil {
		return billing.SubscriptionInput{}, err
	}
	for _, sub := range subscriptions {
		// O semestral e o anual são só na contratação: o veículo novo é mensal
		// (o valor do período não é mensalidade).
		if sub.Status == billing.SubscriptionActive && sub.PeriodMonths <= 1 {
			// Sem a garantia de troca do comodato: ela é do rastreador, não do
			// plano. E sem o desconto do cupom para sempre (que já está no
			// valor): ele é desta assinatura, não do veículo novo. A do link
			// de pagamento não conta: o valor dela é só daquela contratação.
			var guarantee, forever int
			var fromLink bool
			if err := s.db.QueryRow(ctx, `
				SELECT s.guarantee_cents, COALESCE((SELECT r.discount_cents FROM coupon_redemptions r
					WHERE r.subscription_id = s.id AND s.coupon_code IS NOT NULL AND s.promo_price_cents IS NULL), 0),
					EXISTS (SELECT 1 FROM checkout_link_uses u WHERE u.subscription_id = s.id)
				FROM subscriptions s WHERE s.id = $1`, sub.ID).Scan(&guarantee, &forever, &fromLink); err != nil {
				return billing.SubscriptionInput{}, database.MapError(err)
			}
			if fromLink {
				continue
			}
			return billing.SubscriptionInput{PlanName: strings.TrimSuffix(sub.PlanName, GuaranteeName),
				PriceCents: sub.PriceCents + forever - guarantee, DueDay: sub.DueDay}, nil
		}
	}
	return billing.SubscriptionInput{
		PlanName: s.catalog.PlanName, PriceCents: s.catalog.PlanPriceCents, DueDay: s.catalog.DefaultDueDay,
	}, nil
}

// AttachVehicle cadastra o veículo de uma assinatura antiga que ficou sem
// nenhum (dados anteriores ao fluxo único). Não gera cobrança: a assinatura e
// o equipamento já existiam.
func (s *Service) AttachVehicle(ctx context.Context, customerID, subscriptionID uuid.UUID, in vehicles.Input) (*vehicles.Vehicle, error) {
	if err := vehicles.Validate(in); err != nil {
		return nil, err
	}
	in.OwnerID = &customerID

	var vehicle *vehicles.Vehicle
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if err := lockCustomer(ctx, tx, customerID); err != nil {
			return err
		}
		// Assinatura de outro cliente é "não encontrada" (não revela que
		// existe); a do próprio cliente, já com veículo ou encerrada, é conflito.
		var status string
		var current *uuid.UUID
		if err := tx.QueryRow(ctx, `
			SELECT status, vehicle_id FROM subscriptions WHERE id = $1 AND customer_id = $2`,
			subscriptionID, customerID).Scan(&status, &current); err != nil {
			return err
		}
		if status != billing.SubscriptionActive || current != nil {
			return ErrSubscriptionTaken
		}
		var err error
		if vehicle, err = vehicles.InsertWith(ctx, tx, in); err != nil {
			return err
		}
		if _, err = billing.LinkVehicle(ctx, tx, customerID, subscriptionID, vehicle.ID); err != nil {
			return err
		}
		if vehicle.DeviceID == nil {
			_, err = fulfillment.Create(ctx, tx, customerID, vehicle.ID, &subscriptionID)
		}
		return err
	})
	if err != nil {
		return nil, database.MapError(err)
	}
	return vehicle, nil
}

// Reactivate abre uma nova assinatura para um veículo do cliente que ficou
// sem nenhuma ativa (encerrada, ou de antes do fluxo único). Sem fatura de
// equipamento: o aparelho já está com o cliente.
func (s *Service) Reactivate(ctx context.Context, customerID, vehicleID uuid.UUID, plan billing.SubscriptionInput) (*billing.Subscription, error) {
	sub, err := s.billing.NewSubscription(customerID, plan)
	if err != nil {
		return nil, err
	}
	sub.VehicleID = &vehicleID

	var created *billing.Subscription
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if err := lockCustomer(ctx, tx, customerID); err != nil {
			return err
		}
		if err := s.billing.AlignDueDay(ctx, tx, sub, plan.FirstDueDate != nil); err != nil {
			return err
		}
		var owner *uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT owner_id FROM vehicles WHERE id = $1`, vehicleID).Scan(&owner); err != nil {
			return err
		}
		if owner == nil || *owner != customerID {
			return database.ErrNotFound
		}
		var err error
		created, err = billing.InsertSubscription(ctx, tx, sub)
		return err
	})
	if err != nil {
		err = database.MapError(err)
		// O índice único (um veículo, uma assinatura ativa) barra a segunda.
		if errors.Is(err, database.ErrConflict) {
			return nil, ErrVehicleHasSubscription
		}
		return nil, err
	}

	s.billing.GenerateInvoices(ctx)
	return s.billing.GetSubscription(ctx, created.ID)
}

// money: 2240 → "R$ 22,40".
func money(cents int) string {
	return fmt.Sprintf("R$ %d,%02d", cents/100, cents%100)
}
