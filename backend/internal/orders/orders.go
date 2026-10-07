// Package orders cuida do fluxo único de contratação: 1. veículo, 2. rastreador,
// 3. assinatura. Numa transação só, cadastra o veículo, cria a assinatura dele
// (com a cópia do endereço de entrega) e lança a fatura do equipamento. A
// instalação não é cobrada pela plataforma: o cliente paga direto ao
// prestador (ver o pacote installers).
package orders

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

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
// assinatura.
type Order struct {
	Vehicle vehicles.Input
	// EquipmentCents é o valor do aparelho; zero não gera fatura (aparelho do
	// cliente, cortesia).
	EquipmentCents int
	// SetupDueDate é o vencimento dessa fatura; nulo usa o prazo do catálogo.
	SetupDueDate *billing.Date
	// RequireAddress recusa o pedido se o cliente não tiver endereço de
	// entrega. Vale para o pedido do cliente; a central pode lançar sem ele
	// (aparelho instalado na hora, entregue em mãos).
	RequireAddress bool
	Plan           billing.SubscriptionInput
	// LaunchPromo pede a promoção de pré-lançamento: o equipamento sai pelo
	// valor dela (se for cobrado) e a mensalidade, pelo dela nos primeiros
	// meses. Sem direito, o pedido é recusado com PromoUnavailable.
	LaunchPromo bool
	// Shipping é o frete escolhido (cotado de novo no servidor): entra na
	// fatura do equipamento e fica no acompanhamento. Nulo: sem frete; na
	// entrega combinada, só fica no acompanhamento.
	Shipping *fulfillment.Choice
	// Installments parcela o equipamento sem juros (0 ou 1: à vista; até o
	// EquipmentMaxInstallments do catálogo): a 1ª parcela vai na fatura do
	// pedido, com o frete, e as demais, somadas às mensalidades. A assinatura
	// fica ativa até a última.
	Installments int
}

// Result é o que a contratação criou.
type Result struct {
	Vehicle      *vehicles.Vehicle     `json:"vehicle"`
	Subscription *billing.Subscription `json:"subscription"`
	// SetupInvoice é a fatura do equipamento (com o frete); nula quando nada
	// foi cobrado (aparelho do cliente, cortesia, sem frete...).
	SetupInvoice *billing.Invoice `json:"setupInvoice"`
	// Shipping é a entrega escolhida (nula: sem frete).
	Shipping *fulfillment.Choice `json:"shipping"`
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

	var setup *billing.Invoice
	if o.EquipmentCents < 0 {
		return nil, ValidationError{"o valor do equipamento não pode ser negativo"}
	}
	if o.Installments == 1 {
		o.Installments = 0
	}
	if max := s.catalog.EquipmentMaxInstallments; o.Installments < 0 || o.Installments > max ||
		o.Installments > billing.MaxInstallments {
		if max <= 1 {
			return nil, ValidationError{"o rastreador só pode ser pago à vista"}
		}
		return nil, ValidationError{fmt.Sprintf("o rastreador pode ser parcelado em até %d vezes", max)}
	}
	due := s.billing.Today().AddDays(s.catalog.SetupDueDays)
	if o.SetupDueDate != nil {
		due = *o.SetupDueDate
	}
	if o.EquipmentCents > 0 {
		setup, err = s.billing.NewInvoice(customerID, billing.InvoiceInput{
			Description: s.catalog.EquipmentName,
			AmountCents: o.EquipmentCents,
			DueDate:     due,
		})
		if err != nil {
			return nil, err
		}
	}

	result := &Result{}
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if err := lockCustomer(ctx, tx, customerID); err != nil {
			return err
		}
		if o.LaunchPromo {
			var err error
			if setup, err = s.applyPromo(ctx, tx, customerID, sub, setup); err != nil {
				return err
			}
		}
		// Parcelado: a fatura do pedido leva só a 1ª parcela.
		var installments []int
		if o.Installments > 0 {
			if setup == nil {
				return ValidationError{"não há valor de equipamento para parcelar"}
			}
			total := setup.AmountCents
			installments = billing.SplitInstallments(total, o.Installments)
			setup.AmountCents = installments[0]
			setup.Description += fmt.Sprintf(" · parcela 1/%d (%s em %dx sem juros)", o.Installments, money(total), o.Installments)
			sub.PlanInstallments(o.Installments)
		}
		// O frete entra na fatura do equipamento (ou é a fatura, se o
		// equipamento não for cobrado).
		if o.Shipping != nil && o.Vehicle.DeviceID == nil {
			if o.Shipping.PriceCents > 0 {
				freight := fmt.Sprintf("frete %s (%s)", o.Shipping.Name, money(o.Shipping.PriceCents))
				if setup == nil {
					var err error
					if setup, err = s.billing.NewInvoice(customerID, billing.InvoiceInput{
						Description: "Frete do rastreador: " + o.Shipping.Name, AmountCents: o.Shipping.PriceCents, DueDate: due,
					}); err != nil {
						return err
					}
				} else {
					setup.AmountCents += o.Shipping.PriceCents
					setup.Description += " + " + freight
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
		if o.LaunchPromo {
			// A vaga fica com o cliente (e com esta assinatura).
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
			// A fatura do equipamento é avulsa (sem assinatura): se tivesse a
			// assinatura e caísse no mesmo dia da primeira mensalidade, a
			// chave única (assinatura, vencimento) impediria a mensalidade.
			if result.SetupInvoice, err = billing.InsertInvoice(ctx, tx, setup); err != nil {
				return err
			}
		}
		if installments != nil {
			return billing.InsertInstallments(ctx, tx, result.Subscription.ID, installments, result.SetupInvoice.ID)
		}
		return nil
	})
	if err != nil {
		return nil, database.MapError(err)
	}
	if result.SetupInvoice != nil {
		s.billing.Decorate(result.SetupInvoice)
	}

	// Primeira mensalidade, se já estiver dentro da antecedência.
	s.billing.GenerateInvoices(ctx)
	freightCents := 0
	if result.Shipping != nil {
		freightCents = result.Shipping.PriceCents
	}
	s.log.Info("rastreador contratado", "customer", customerID, "vehicle", result.Vehicle.ID,
		"subscription", result.Subscription.ID, "equipment_cents", o.EquipmentCents, "promo", o.LaunchPromo,
		"freight_cents", freightCents, "installments", o.Installments)
	return result, nil
}

// applyPromo reserva a vaga da promoção (na transação do pedido) e troca os
// valores: o equipamento cobrado sai pelo da promoção e a assinatura ganha a
// mensalidade promocional pelos primeiros meses (a do plano do Insanos MC,
// se for o plano deles).
func (s *Service) applyPromo(ctx context.Context, tx pgx.Tx, customerID uuid.UUID, sub *billing.Subscription,
	setup *billing.Invoice) (*billing.Invoice, error) {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, promoLockKey); err != nil {
		return nil, err
	}
	p := s.catalog.LaunchPromo
	reason, err := promoCheck(ctx, tx, customerID, p)
	if err != nil {
		return nil, err
	}
	if reason != "" {
		return nil, PromoUnavailable{reason}
	}
	monthly := p.MonthlyFor(sub.PlanName)
	until := billing.Date{Time: sub.NextDueDate.AddDate(0, p.Months, 0)}
	sub.PromoPriceCents, sub.PromoUntil = &monthly, &until
	// Equipamento já com o cliente (sem cobrança) continua sem cobrança.
	if setup != nil {
		if p.EquipmentCents == 0 {
			return nil, nil
		}
		setup.AmountCents = p.EquipmentCents
		setup.Description += " · promoção de pré-lançamento"
	}
	return setup, nil
}

// CustomerOrder monta o pedido feito pelo próprio cliente, com os preços do
// catálogo — ele não escolhe valor. O plano é o que ele já tem (quem paga o
// preço especial continua nele); sem assinatura ativa, vale o plano padrão.
func (s *Service) CustomerOrder(ctx context.Context, customerID uuid.UUID, vehicle vehicles.Input, launchPromo bool) (Order, error) {
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

	plan, err := s.CustomerPlan(ctx, customerID)
	if err != nil {
		return Order{}, err
	}

	// O cliente não vincula aparelho nem escolhe dono.
	vehicle.DeviceID, vehicle.OwnerID = nil, nil

	return Order{
		Vehicle: vehicle, EquipmentCents: s.catalog.EquipmentPriceCents, RequireAddress: true, Plan: plan,
		LaunchPromo: launchPromo,
	}, nil
}

// CustomerPlan é o plano de um novo veículo do cliente: o da assinatura
// ativa que ele já tem ou, sem nenhuma, o padrão do catálogo.
func (s *Service) CustomerPlan(ctx context.Context, customerID uuid.UUID) (billing.SubscriptionInput, error) {
	subscriptions, err := s.billing.ListSubscriptions(ctx, customerID)
	if err != nil {
		return billing.SubscriptionInput{}, err
	}
	for _, sub := range subscriptions {
		if sub.Status == billing.SubscriptionActive {
			return billing.SubscriptionInput{PlanName: sub.PlanName, PriceCents: sub.PriceCents, DueDay: sub.DueDay}, nil
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
