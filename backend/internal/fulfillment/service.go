package fulfillment

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/addresses"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/melhorenvio"
)

// ErrShippingDisabled: Melhor Envios sem credencial configurada.
var ErrShippingDisabled = errors.New("envio pelo Melhor Envios não configurado (MELHORENVIO_CLIENT_ID e MELHORENVIO_CLIENT_SECRET)")

// Notifier avisa o cliente nos marcos: rastreador enviado e rastreador chegou.
type Notifier interface {
	Shipped(ctx context.Context, f *Fulfillment) error
	Delivered(ctx context.Context, f *Fulfillment) error
}

type Service struct {
	db       *database.DB
	repo     *Repository
	carrier  *melhorenvio.Client // nil: envio não configurado
	notifier Notifier
	shipping config.Shipping
	product  string
	log      *slog.Logger
	// generateWait é o intervalo entre as consultas da etiqueta em geração.
	generateWait time.Duration
}

func NewService(db *database.DB, repo *Repository, carrier *melhorenvio.Client, notifier Notifier,
	shipping config.Shipping, productName string, log *slog.Logger) *Service {
	return &Service{db: db, repo: repo, carrier: carrier, notifier: notifier, shipping: shipping,
		product: productName, log: log.With("component", "fulfillment"), generateWait: 2 * time.Second}
}

func (s *Service) Repo() *Repository { return s.repo }

// ---------------------------------------------------------------------------
// Regras das duas linhas do tempo
// ---------------------------------------------------------------------------

// plan decide o que uma mudança pedida pela central grava: os status, em
// ordem, no track pedido — ou o motivo da recusa. hasDevice diz se o veículo
// já tem (ou está recebendo agora) o aparelho configurado; labeled, se há
// etiqueta do Melhor Envios (sem ela, a entrega é em mãos ou por fora).
func plan(track, target, chip, tracker string, hasDevice, labeled bool) ([]string, error) {
	if track != TrackChip && track != TrackTracker {
		return nil, RuleError{"linha do tempo inválida"}
	}
	if Position(track, target) < 0 {
		return nil, RuleError{"status inválido"}
	}

	if track == TrackChip {
		if target == chip {
			return nil, RuleError{"o chip já está nesse status"}
		}
		if Position(TrackTracker, tracker) >= Position(TrackTracker, TrackerConfiguring) &&
			Position(TrackChip, target) < Position(TrackChip, ChipSeparated) {
			return nil, RuleError{"o rastreador já foi configurado com este chip; não dá para voltar o chip"}
		}
		return []string{target}, nil
	}

	if target == tracker {
		return nil, RuleError{"o rastreador já está nesse status"}
	}
	current, next := Position(TrackTracker, tracker), Position(TrackTracker, target)
	shipped := Position(TrackTracker, TrackerShipped)
	configured := Position(TrackTracker, TrackerConfigured)
	switch {
	case target == TrackerShipped:
		return nil, RuleError{"o envio sai pela compra da etiqueta no Melhor Envios"}
	// Entregue em mãos (sem etiqueta) pode ser desfeito; com etiqueta, só
	// o cancelamento dela no Melhor Envios volta o rastreador.
	case current >= shipped && next < shipped && labeled:
		return nil, RuleError{"o rastreador já foi enviado; se a etiqueta for cancelada no Melhor Envios, ele volta sozinho para Configurado"}
	case target == TrackerInTransit && (current < shipped || !labeled):
		return nil, RuleError{"em trânsito só depois do envio (compra da etiqueta)"}
	case target == TrackerDelivered && current < configured:
		return nil, RuleError{"o rastreador é entregue depois de configurado"}
	case target == TrackerDelivered && current == configured && labeled:
		return nil, RuleError{"há uma etiqueta paga no Melhor Envios; conclua o envio ou cancele a etiqueta antes de marcar a entrega em mãos"}
	case target == TrackerDelivered && current == configured && !hasDevice:
		return nil, RuleError{"vincule o aparelho (IMEI) ao veículo antes de entregar"}
	case target == TrackerAwaitingChip && Position(TrackChip, chip) >= Position(TrackChip, ChipAtBase):
		return nil, RuleError{"o chip já chegou à base; siga para a configuração"}
	}

	if next > current {
		configuring := Position(TrackTracker, TrackerConfiguring)
		if next >= configuring && next <= Position(TrackTracker, TrackerConfigured) && chip != ChipSeparated {
			return nil, RuleError{"separe o chip M2M para configuração antes de configurar o rastreador"}
		}
		if target == TrackerConfigured && !hasDevice {
			return nil, RuleError{"escolha o aparelho (IMEI) configurado para este veículo"}
		}
		// Rastreador na base e chip ainda não: fica esperando o chip.
		if target == TrackerAtBase && Position(TrackChip, chip) < Position(TrackChip, ChipAtBase) {
			return []string{TrackerAtBase, TrackerAwaitingChip}, nil
		}
	}
	return []string{target}, nil
}

// ChangeRequest é uma mudança feita pela central.
type ChangeRequest struct {
	Track  string
	Status string
	Note   string
	// DeviceID vincula o aparelho ao veículo ao marcar "Configurado".
	DeviceID *uuid.UUID
}

// Change aplica a mudança da central (com as regras) e avisa o cliente se o
// rastreador chegou.
func (s *Service) Change(ctx context.Context, id uuid.UUID, req ChangeRequest, actor uuid.UUID) (*Fulfillment, error) {
	req.Note = strings.TrimSpace(req.Note)
	if len([]rune(req.Note)) > 300 {
		return nil, RuleError{"observação: até 300 caracteres"}
	}
	var statuses []string
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		chip, tracker, vehicleID, err := lock(ctx, tx, id)
		if err != nil {
			return err
		}
		var device *uuid.UUID
		var labeled bool
		if err := tx.QueryRow(ctx, `
			SELECT v.device_id, f.shipping_order_id IS NOT NULL
			FROM fulfillments f JOIN vehicles v ON v.id = f.vehicle_id
			WHERE f.id = $1`, id).Scan(&device, &labeled); err != nil {
			return err
		}
		linking := req.Track == TrackTracker && req.Status == TrackerConfigured && req.DeviceID != nil
		if statuses, err = plan(req.Track, req.Status, chip, tracker, device != nil || linking, labeled); err != nil {
			return err
		}
		if linking {
			if _, err := tx.Exec(ctx, `UPDATE vehicles SET device_id = $2, updated_at = NOW() WHERE id = $1`,
				vehicleID, *req.DeviceID); err != nil {
				if errors.Is(database.MapError(err), database.ErrConflict) {
					return RuleError{"este aparelho já está vinculado a outro veículo"}
				}
				if errors.Is(database.MapError(err), database.ErrForeignKey) {
					return RuleError{"aparelho não encontrado"}
				}
				return err
			}
		}
		if req.Note == "" && req.Track == TrackTracker && req.Status == TrackerDelivered && !labeled {
			req.Note = "Entregue em mãos, sem etiqueta do Melhor Envios"
		}
		for i, status := range statuses {
			note := req.Note
			if i > 0 {
				note = "O chip M2M ainda não chegou à base"
			}
			if err := setStatus(ctx, tx, id, req.Track, status, note, &actor); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, database.MapError(err)
	}
	f, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if req.Track == TrackTracker && req.Status == TrackerDelivered {
		s.notify(ctx, f, true)
	}
	return f, nil
}

// notify manda o e-mail do marco: enviado ou chegou.
func (s *Service) notify(ctx context.Context, f *Fulfillment, delivered bool) {
	if s.notifier == nil {
		return
	}
	send := s.notifier.Shipped
	if delivered {
		send = s.notifier.Delivered
	}
	if err := send(ctx, f); err != nil {
		s.log.Error("falha ao avisar o cliente do envio", "fulfillment", f.ID, "err", err)
	}
}

// ---------------------------------------------------------------------------
// Envio pelo Melhor Envios
// ---------------------------------------------------------------------------

// destination é para quem vai a etiqueta: a cópia do endereço guardada no
// pedido ou, sem ela, o endereço atual do cliente.
type destination struct {
	name, email, phone, document string
	address                      addresses.Address
}

func (s *Service) destination(ctx context.Context, f *Fulfillment) (*destination, error) {
	d := &destination{}
	var snapshot []byte
	err := s.db.QueryRow(ctx, `
		SELECT u.name, u.email, u.phone, u.document, s.delivery_address
		FROM users u LEFT JOIN subscriptions s ON s.id = $2
		WHERE u.id = $1`, f.CustomerID, f.SubscriptionID).Scan(&d.name, &d.email, &d.phone, &d.document, &snapshot)
	if err != nil {
		return nil, database.MapError(err)
	}
	if len(snapshot) > 0 && string(snapshot) != "null" {
		if err := json.Unmarshal(snapshot, &d.address); err != nil {
			return nil, err
		}
		return d, nil
	}
	current, err := addresses.GetWith(ctx, s.db, f.CustomerID)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, RuleError{"o cliente não tem endereço de entrega; cadastre na ficha dele"}
	}
	d.address = *current
	return d, nil
}

func (s *Service) pkg() melhorenvio.Package {
	return melhorenvio.Package{
		HeightCm: s.shipping.HeightCm, WidthCm: s.shipping.WidthCm, LengthCm: s.shipping.LengthCm,
		WeightKg: s.shipping.WeightKg, InsuranceCents: s.shipping.InsuranceCents,
	}
}

func (s *Service) requireCarrier() error {
	if s.carrier == nil {
		return ErrShippingDisabled
	}
	return nil
}

// Quote cota o frete do rastreador configurado até o cliente.
func (s *Service) Quote(ctx context.Context, id uuid.UUID) ([]melhorenvio.Quote, error) {
	if err := s.requireCarrier(); err != nil {
		return nil, err
	}
	if missing := s.shipping.MissingOrigin(); len(missing) > 0 {
		return nil, RuleError{"falta configurar o remetente: " + strings.Join(missing, ", ")}
	}
	f, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if f.TrackerStatus != TrackerConfigured {
		return nil, RuleError{"o frete é cotado com o rastreador configurado"}
	}
	d, err := s.destination(ctx, f)
	if err != nil {
		return nil, err
	}
	return s.carrier.Calculate(ctx, s.shipping.From.PostalCode, d.address.ZipCode, s.pkg(), s.shipping.Services)
}

// BuyLabel compra a etiqueta do serviço escolhido e marca o rastreador como
// enviado. É retomável: cada etapa (carrinho, pagamento, geração, impressão)
// fica gravada antes da próxima, e repetir continua de onde parou — sem
// comprar duas vezes.
func (s *Service) BuyLabel(ctx context.Context, id uuid.UUID, serviceID int, actor uuid.UUID) (*Fulfillment, error) {
	if err := s.requireCarrier(); err != nil {
		return nil, err
	}
	f, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if f.TrackerStatus != TrackerConfigured {
		return nil, RuleError{"a etiqueta é comprada com o rastreador configurado"}
	}

	orderID := ""
	if f.ShippingOrderID != nil {
		orderID = *f.ShippingOrderID
	}
	status, serviceName := f.ShippingStatus, f.ShippingService

	// 1. Carrinho.
	if orderID == "" && serviceID <= 0 {
		return nil, RuleError{"escolha o serviço de frete"}
	}
	if orderID == "" {
		if missing := s.shipping.MissingOrigin(); len(missing) > 0 {
			return nil, RuleError{"falta configurar o remetente: " + strings.Join(missing, ", ")}
		}
		d, err := s.destination(ctx, f)
		if err != nil {
			return nil, err
		}
		to, err := recipient(d)
		if err != nil {
			return nil, err
		}
		quotes, err := s.carrier.Calculate(ctx, s.shipping.From.PostalCode, d.address.ZipCode, s.pkg(), s.shipping.Services)
		if err != nil {
			return nil, err
		}
		var chosen *melhorenvio.Quote
		for i := range quotes {
			if quotes[i].ServiceID == serviceID && quotes[i].Error == "" {
				chosen = &quotes[i]
			}
		}
		if chosen == nil {
			return nil, RuleError{"serviço de frete indisponível para este endereço; cote de novo"}
		}
		order, err := s.carrier.AddToCart(ctx, melhorenvio.CartRequest{
			ServiceID: serviceID, From: s.sender(), To: to, ProductName: s.product,
			Package: s.pkg(), NonCommercial: s.shipping.NonCommercial, Tag: "Pedido " + f.ID.String()[:8],
		})
		if err != nil {
			return nil, err
		}
		orderID, status = order.ID, "pending"
		price := order.Price.Cents()
		if price == 0 {
			price = chosen.PriceCents
		}
		serviceName = chosen.Name()
		if err := s.repo.updateShipping(ctx, s.db, id, ShippingUpdate{
			OrderID: &orderID, Protocol: &order.Protocol, Service: &serviceName, PriceCents: &price, Status: &status,
		}); err != nil {
			return nil, err
		}
	}

	// 2. Pagamento (saldo da carteira). Recusado, a etiqueta sai do carrinho
	// para uma nova tentativa começar limpa.
	if status == "pending" {
		if err := s.carrier.Checkout(ctx, orderID); err != nil {
			if rmErr := s.carrier.RemoveFromCart(ctx, orderID); rmErr != nil {
				s.log.Warn("etiqueta não removida do carrinho", "order", orderID, "err", rmErr)
			}
			empty := ""
			if uErr := s.repo.updateShipping(ctx, s.db, id, ShippingUpdate{
				ClearOrder: true, Protocol: &empty, Service: &empty, Status: &empty,
			}); uErr != nil {
				s.log.Error("falha ao limpar a etiqueta recusada", "fulfillment", id, "err", uErr)
			}
			return nil, err
		}
		status = "released"
		if err := s.repo.updateShipping(ctx, s.db, id, ShippingUpdate{Status: &status}); err != nil {
			return nil, err
		}
	}

	// 3. Geração. No Melhor Envios ela é assíncrona: o pedido é aceito ("o
	// envio já está sendo processado") e a etiqueta fica gerada instantes
	// depois. Espera um pouco; se não sair, o pedido fica "etiqueta paga, em
	// geração" e a sincronização conclui o envio sozinha.
	if status == "released" {
		genErr := s.carrier.Generate(ctx, orderID)
		if !s.waitGenerated(ctx, orderID) {
			if genErr != nil {
				s.log.Info("etiqueta ainda em geração", "fulfillment", id, "detail", genErr)
			}
			return s.repo.Get(ctx, id)
		}
		status = "generated"
		if err := s.repo.updateShipping(ctx, s.db, id, ShippingUpdate{Status: &status}); err != nil {
			return nil, err
		}
	}

	// 4. Impressão, código de rastreio e "Enviado".
	return s.finishShipment(ctx, id, orderID, serviceName, &actor)
}

// waitGenerated consulta a etiqueta por alguns segundos até ela ficar
// gerada (pelos detalhes dela: o status do rastreio demora a mudar).
func (s *Service) waitGenerated(ctx context.Context, orderID string) bool {
	for attempt := 0; attempt < 4; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return false
			case <-time.After(s.generateWait):
			}
		}
		order, err := s.carrier.Order(ctx, orderID)
		if err == nil && order.Generated() {
			return true
		}
	}
	return false
}

// finishShipment imprime a etiqueta gerada, grava o código de rastreio e
// marca o rastreador como enviado (avisando o cliente). Sem actor, quem
// concluiu foi a sincronização.
func (s *Service) finishShipment(ctx context.Context, id uuid.UUID, orderID, serviceName string, actor *uuid.UUID) (*Fulfillment, error) {
	label, err := s.carrier.Print(ctx, orderID)
	if err != nil {
		return nil, err
	}
	status := "generated"
	update := ShippingUpdate{LabelURL: &label, Status: &status}
	if order, err := s.carrier.Order(ctx, orderID); err == nil {
		if code := order.Code(); code != "" {
			update.Tracking = &code
		}
	} else {
		s.log.Warn("código de rastreio ainda indisponível", "order", orderID, "err", err)
	}
	shipped := false
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		_, tracker, _, err := lock(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := s.repo.updateShipping(ctx, tx, id, update); err != nil {
			return err
		}
		// Outra conclusão (botão ou sincronização) já marcou.
		if tracker != TrackerConfigured {
			return nil
		}
		shipped = true
		return setStatus(ctx, tx, id, TrackTracker, TrackerShipped, "Enviado por "+serviceName, actor)
	})
	if err != nil {
		return nil, database.MapError(err)
	}
	f, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if shipped {
		s.notify(ctx, f, false)
	}
	return f, nil
}

func (s *Service) sender() melhorenvio.Party {
	from := s.shipping.From
	return melhorenvio.Party{
		Name: from.Name, Phone: onlyDigits(from.Phone), Email: from.Email, Document: onlyDigits(from.Document),
		CompanyDocument: onlyDigits(from.CompanyDocument), StateRegister: from.StateRegister,
		Address: from.Address, Complement: from.Complement, Number: from.Number, District: from.District,
		City: from.City, PostalCode: from.PostalCode, StateAbbr: from.State,
	}
}

// recipient monta o destinatário; a transportadora exige CPF ou CNPJ.
func recipient(d *destination) (melhorenvio.Party, error) {
	doc := onlyDigits(d.document)
	p := melhorenvio.Party{
		Name: d.name, Phone: onlyDigits(d.phone), Email: d.email,
		Address: d.address.Street, Complement: d.address.Complement, Number: d.address.Number,
		District: d.address.District, City: d.address.City, PostalCode: d.address.ZipCode, StateAbbr: d.address.State,
	}
	switch len(doc) {
	case 11:
		p.Document = doc
	case 14:
		p.CompanyDocument = doc
	default:
		return p, RuleError{"cadastre o CPF ou CNPJ do cliente na ficha dele: a transportadora exige"}
	}
	return p, nil
}

func onlyDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// Rastreio: sincronização e webhook
// ---------------------------------------------------------------------------

// Sync consulta o rastreio de tudo o que está a caminho. Devolve quantos
// mudaram.
func (s *Service) Sync(ctx context.Context) (int, error) {
	if s.carrier == nil {
		return 0, nil
	}
	finished := s.finishPending(ctx)
	list, err := s.repo.InTransit(ctx)
	if err != nil || len(list) == 0 {
		return finished, err
	}
	// Um a um pelos detalhes da etiqueta (o rastreio em lote fica
	// desatualizado), com poucas consultas em paralelo.
	var changed atomic.Int64
	var firstErr error
	var mu sync.Mutex
	work := make(chan *Fulfillment)
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for f := range work {
				order, err := s.carrier.Order(ctx, *f.ShippingOrderID)
				if err != nil {
					mu.Lock()
					if firstErr == nil {
						firstErr = err
					}
					mu.Unlock()
					continue
				}
				moved, err := s.Apply(ctx, f, order.AsTracking())
				if err != nil {
					s.log.Error("falha ao aplicar o rastreio", "fulfillment", f.ID, "err", err)
					continue
				}
				if moved {
					changed.Add(1)
				}
			}
		}()
	}
	for _, f := range list {
		work <- f
	}
	close(work)
	wg.Wait()
	return finished + int(changed.Load()), firstErr
}

// finishPending conclui as etiquetas pagas que o Melhor Envios terminou de
// gerar. Devolve quantas viraram "Enviado".
func (s *Service) finishPending(ctx context.Context) int {
	pending, err := s.repo.PendingLabels(ctx)
	if err != nil || len(pending) == 0 {
		return 0
	}
	done := 0
	for _, f := range pending {
		order, err := s.carrier.Order(ctx, *f.ShippingOrderID)
		if err != nil {
			s.log.Warn("falha ao consultar etiqueta em geração", "fulfillment", f.ID, "err", err)
			continue
		}
		if order.Cancelled() {
			// Cancelada antes de sair: o pedido fica livre para outra etiqueta.
			empty, status := "", "canceled"
			if err := s.repo.updateShipping(ctx, s.db, f.ID, ShippingUpdate{
				ClearOrder: true, Status: &status, Tracking: &empty, LabelURL: &empty,
			}); err != nil {
				s.log.Error("falha ao liberar a etiqueta cancelada", "fulfillment", f.ID, "err", err)
			}
			continue
		}
		if !order.Generated() {
			continue
		}
		if _, err := s.finishShipment(ctx, f.ID, *f.ShippingOrderID, f.ShippingService, nil); err != nil {
			s.log.Error("falha ao concluir o envio", "fulfillment", f.ID, "err", err)
			continue
		}
		done++
	}
	return done
}

// SyncOne atualiza um acompanhamento agora (botão da central).
func (s *Service) SyncOne(ctx context.Context, id uuid.UUID) (*Fulfillment, error) {
	if err := s.requireCarrier(); err != nil {
		return nil, err
	}
	f, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if f.ShippingOrderID == nil {
		return nil, RuleError{"este rastreador ainda não tem etiqueta"}
	}
	order, err := s.carrier.Order(ctx, *f.ShippingOrderID)
	if err != nil {
		return nil, err
	}
	// Etiqueta paga que estava em geração: se já saiu, conclui o envio.
	if f.TrackerStatus == TrackerConfigured {
		if order.Generated() {
			return s.finishShipment(ctx, f.ID, *f.ShippingOrderID, f.ShippingService, nil)
		}
		return f, nil
	}
	if _, err := s.Apply(ctx, f, order.AsTracking()); err != nil {
		return nil, err
	}
	return s.repo.Get(ctx, id)
}

// ApplyWebhook aplica um evento de etiqueta recebido do Melhor Envios.
func (s *Service) ApplyWebhook(ctx context.Context, info melhorenvio.TrackingInfo) error {
	f, err := s.repo.GetByShippingOrder(ctx, info.ID)
	if err != nil {
		return err
	}
	_, err = s.Apply(ctx, f, info)
	return err
}

// Apply leva o status do Melhor Envios para a linha do tempo: postado vira
// "Em trânsito", entregue vira "Chegou" (e avisa o cliente), cancelado volta
// para "Configurado" para comprar outra etiqueta. Devolve se o rastreador
// mudou de status.
func (s *Service) Apply(ctx context.Context, f *Fulfillment, info melhorenvio.TrackingInfo) (bool, error) {
	status := strings.ToLower(info.Status)
	// O status às vezes demora a acompanhar as datas: elas valem mais.
	if info.DeliveredAt != nil && *info.DeliveredAt != "" {
		status = "delivered"
	} else if info.PostedAt != nil && *info.PostedAt != "" && status != "delivered" {
		status = "posted"
	}
	code := info.Code()
	var steps []string
	var note string
	cancel := false
	switch status {
	case "posted", "received":
		if f.TrackerStatus == TrackerShipped {
			steps, note = []string{TrackerInTransit}, "Postado na transportadora"
		}
	case "delivered":
		switch f.TrackerStatus {
		case TrackerShipped:
			steps = []string{TrackerInTransit, TrackerDelivered}
		case TrackerInTransit:
			steps = []string{TrackerDelivered}
		}
		note = "Entregue pela transportadora"
	case "canceled", "cancelled", "expired":
		if f.TrackerStatus == TrackerShipped || f.TrackerStatus == TrackerInTransit {
			steps, note, cancel = []string{TrackerConfigured}, "Etiqueta cancelada no Melhor Envios; compre outra", true
		}
	}
	if len(steps) == 0 && status == f.ShippingStatus && (code == "" || code == f.TrackingCode) {
		return false, nil
	}

	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		_, tracker, _, err := lock(ctx, tx, f.ID)
		if err != nil {
			return err
		}
		// Outra sincronização já aplicou: nada a fazer.
		if tracker != f.TrackerStatus {
			steps = nil
		}
		update := ShippingUpdate{Status: &status}
		if code != "" {
			update.Tracking = &code
		}
		if cancel && steps != nil {
			empty := ""
			update = ShippingUpdate{ClearOrder: true, Status: &status, Tracking: &empty, LabelURL: &empty}
		}
		if err := s.repo.updateShipping(ctx, tx, f.ID, update); err != nil {
			return err
		}
		for i, step := range steps {
			stepNote := note
			if len(steps) > 1 && i == 0 {
				stepNote = "Postado na transportadora"
			}
			if err := setStatus(ctx, tx, f.ID, TrackTracker, step, stepNote, nil); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return false, database.MapError(err)
	}
	if len(steps) > 0 && steps[len(steps)-1] == TrackerDelivered {
		if fresh, err := s.repo.Get(ctx, f.ID); err == nil {
			s.notify(ctx, fresh, true)
		}
	}
	if len(steps) > 0 {
		s.log.Info("rastreio aplicado", "fulfillment", f.ID, "status", status, "tracker", steps[len(steps)-1])
	}
	return len(steps) > 0, nil
}
