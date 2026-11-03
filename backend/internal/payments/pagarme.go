package payments

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/addresses"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/payments/abacatepay"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/payments/pagarme"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/qrcode"
)

// Os dois gateways: a AbacatePay cobra um valor fixo por Pix e a Pagar.me um
// percentual, então o Pix vai pelo mais barato para o valor (empate fica na
// AbacatePay). O boleto e o cartão vão pela Pagar.me quando ela está ligada
// (o cartão da AbacatePay não chegou a ser liberado). Cada cobrança guarda o
// gateway em que nasceu: a consulta, o estorno e o webhook seguem por ele.

const providerPagarme = "pagarme"

// PagarmeGateway é o que o serviço usa da Pagar.me. *pagarme.Client implementa.
type PagarmeGateway interface {
	CreateOrder(ctx context.Context, req pagarme.OrderRequest) (*pagarme.Order, error)
	GetCharge(ctx context.Context, id string) (*pagarme.Charge, error)
	CancelCharge(ctx context.Context, id, idempotencyKey string) (*pagarme.Charge, error)
	FindOrder(ctx context.Context, code string) (*pagarme.Order, error)
	TestMode() bool
}

var (
	// ErrPixPayerIncomplete: só a Pagar.me está ligada e ela exige o pagador inteiro no Pix.
	ErrPixPayerIncomplete = errors.New("para gerar o Pix, o cadastro precisa do nome, e-mail, CPF ou CNPJ válido e celular")
	// ErrCardAddressMissing: o cartão na Pagar.me exige o endereço de cobrança.
	ErrCardAddressMissing = errors.New("para pagar com cartão, o cadastro precisa do endereço (CEP, rua, número, bairro e cidade)")
	// ErrBoletoAddressMissing: o boleto na Pagar.me exige o endereço do pagador.
	ErrBoletoAddressMissing = errors.New("para gerar o boleto, o cadastro precisa do endereço (CEP, rua, número, bairro e cidade)")
)

// SetPagarme liga a Pagar.me (nil desliga).
func (s *Service) SetPagarme(g PagarmeGateway) { s.pagarme = g }

// abacateReady diz se a AbacatePay está ligada.
func (s *Service) abacateReady() bool { return s.gateway != nil && s.cfg.AbacatePayEnabled() }

// pagarmePixFee é a tarifa da Pagar.me num Pix do valor (arredondada).
func (s *Service) pagarmePixFee(amountCents int) int {
	return (amountCents*s.cfg.PagarmePixFeeBps + 5000) / 10000
}

// route escolhe o gateway da cobrança nova. Com split (a parte do afiliado),
// a Pagar.me sempre que der.
func (s *Service) route(method string, amountCents int, payer *Payer, split bool) (string, error) {
	abacate, pagar := s.abacateReady(), s.pagarme != nil
	switch method {
	case MethodPix:
		usable := pagar && pagarmeCustomer(payer) != nil
		switch {
		case usable && (split || !abacate):
			return providerPagarme, nil
		case usable && s.pagarmePixFee(amountCents) < s.cfg.AbacatePayPixFeeCents:
			return providerPagarme, nil
		case abacate:
			return providerAbacatePay, nil
		case pagar:
			return "", ErrPixPayerIncomplete
		}
	case MethodBoleto:
		customer := pagarmeCustomer(payer)
		switch {
		case pagar && customer != nil && customer.Address != nil:
			return providerPagarme, nil
		case abacate:
			return providerAbacatePay, nil
		case pagar && customer != nil:
			return "", ErrBoletoAddressMissing
		case pagar:
			return "", ErrPayerIncomplete
		}
	case MethodCard:
		switch {
		case pagar && s.cfg.PagarmePublicKey != "":
			return providerPagarme, nil
		case abacate && s.cfg.PublicKey != "":
			return providerAbacatePay, nil
		}
		return "", ErrCardDisabled
	default:
		return "", ErrUnknownMethod
	}
	return "", ErrDisabled
}

// pagarmeCustomer é o pagador no formato da Pagar.me: nome, e-mail, CPF/CNPJ
// válido e celular (obrigatórios); o endereço vai quando o cadastro tem.
func pagarmeCustomer(p *Payer) *pagarme.Customer {
	if p == nil || strings.TrimSpace(p.Name) == "" || !strings.Contains(p.Email, "@") || !validTaxID(p.Document) {
		return nil
	}
	phone := onlyDigits(p.Phone)
	if (len(phone) == 12 || len(phone) == 13) && strings.HasPrefix(phone, "55") {
		phone = phone[2:]
	}
	if len(phone) < 10 || len(phone) > 11 {
		return nil
	}
	doc := onlyDigits(p.Document)
	kind := "individual"
	if len(doc) == 14 {
		kind = "company"
	}
	return &pagarme.Customer{
		Name: strings.TrimSpace(p.Name), Email: strings.TrimSpace(p.Email), Document: doc, Type: kind,
		Phones:  pagarme.Phones{MobilePhone: &pagarme.Phone{CountryCode: "55", AreaCode: phone[:2], Number: phone[2:]}},
		Address: pagarmeAddress(p.Address),
	}
}

// pagarmeAddress: line_1 com "número, rua, bairro" e line_2 com o complemento.
func pagarmeAddress(a *addresses.Address) *pagarme.Address {
	if a == nil || len(a.ZipCode) != 8 || a.Street == "" || a.Number == "" || a.City == "" || len(a.State) != 2 {
		return nil
	}
	line1 := a.Number + ", " + a.Street
	if a.District != "" {
		line1 += ", " + a.District
	}
	return &pagarme.Address{Line1: line1, Line2: a.Complement, ZipCode: a.ZipCode, City: a.City, State: a.State, Country: "BR"}
}

// internalStatus traduz o resultado da Pagar.me para o status da cobrança.
// O estorno em andamento continua pago até concluir.
func internalStatus(outcome string) string {
	switch outcome {
	case pagarme.OutcomePaid, pagarme.OutcomeRefunding:
		return abacatepay.StatusPaid
	case pagarme.OutcomeRefunded:
		return abacatepay.StatusRefunded
	case pagarme.OutcomeExpired:
		return abacatepay.StatusExpired
	case pagarme.OutcomeCanceled:
		return abacatepay.StatusCancelled
	case pagarme.OutcomeFailed:
		return abacatepay.StatusFailed
	}
	return abacatepay.StatusPending
}

// pixQR é a imagem do QR Code do Pix (a Pagar.me manda um link; a imagem sai
// daqui, sem o navegador buscar fora).
func pixQR(code string) string {
	png, err := qrcode.PNG(code, 8)
	if err != nil || code == "" {
		return ""
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
}

// createPagarme gera o Pix ou o boleto na Pagar.me (com o split do afiliado,
// se houver).
func (s *Service) createPagarme(ctx context.Context, method string, id uuid.UUID, req chargeRequest, plan *SplitPlan) (*Charge, error) {
	order := pagarme.OrderRequest{
		Code: id.String(), AmountCents: req.AmountCents, Description: req.Description,
		Customer: pagarmeCustomer(req.Payer), Metadata: req.Metadata, Split: s.splitRules(plan, req.AmountCents),
	}
	if method == MethodBoleto {
		order.Method = pagarme.MethodBoleto
		order.BoletoDueAt = s.pagarmeBoletoDue(req.DueDate)
		order.BoletoInstructions = "Farbo Rastreadores. Pagável em qualquer banco até o vencimento."
	} else {
		order.Method = pagarme.MethodPix
		order.PixExpiresIn = s.cfg.PixExpiresIn
	}
	created, err := s.pagarme.CreateOrder(ctx, order)
	if err != nil {
		return nil, fmt.Errorf("gerando %s na Pagar.me: %w", strings.ToLower(method), err)
	}
	ch := created.FirstCharge()
	if ch == nil {
		return nil, fmt.Errorf("a Pagar.me não devolveu a cobrança do pedido %s", created.ID)
	}
	tx := ch.LastTransaction
	if tx == nil {
		tx = &pagarme.Transaction{}
	}
	status := internalStatus(ch.Outcome())
	if status != abacatepay.StatusPending {
		reason := tx.FailureMessage()
		if reason == "" {
			reason = "a cobrança não foi gerada (" + ch.Status + ")"
		}
		return nil, &pagarme.APIError{HTTPStatus: 422, Message: reason}
	}
	amount := ch.Amount
	if amount <= 0 {
		amount = req.AmountCents
	}
	charge := &Charge{
		ID: id, Provider: providerPagarme, Method: method, ProviderChargeID: ch.ID, AmountCents: amount,
		Status: status, DevMode: s.pagarme.TestMode(),
	}
	if method == MethodBoleto {
		charge.BoletoLine, charge.BoletoURL = tx.Line, tx.PDF
		if charge.BoletoURL == "" {
			charge.BoletoURL = tx.URL
		}
		// O boleto vale até o fim do dia do vencimento.
		if tx.DueAt != nil {
			end := tx.DueAt.Add(24 * time.Hour)
			charge.ExpiresAt = &end
		}
		return charge, nil
	}
	charge.BrCode, charge.QRCodeImage, charge.ExpiresAt = tx.QRCode, pixQR(tx.QRCode), tx.ExpiresAt
	charge.PlatformFeeCents = s.pagarmePixFee(amount)
	return charge, nil
}

// pagarmeBoletoDue: o vencimento da fatura, se ainda vem pela frente; senão,
// daqui a 3 dias.
func (s *Service) pagarmeBoletoDue(due billing.Date) time.Time {
	today := s.invoices.Today()
	if !due.IsZero() && today.Before(due) {
		return due.Time
	}
	return today.AddDays(3).Time
}

// refreshPagarme consulta a cobrança na Pagar.me e aplica o status.
func (s *Service) refreshPagarme(ctx context.Context, charge *Charge) error {
	ch, err := s.pagarme.GetCharge(ctx, charge.ProviderChargeID)
	if err != nil {
		return fmt.Errorf("consultando a cobrança na Pagar.me: %w", err)
	}
	return s.apply(ctx, charge, internalStatus(ch.Outcome()))
}

// refundPagarme pede o estorno (o cancelamento da cobrança paga). Com split,
// a Pagar.me desfaz a parte do afiliado junto.
func (s *Service) refundPagarme(ctx context.Context, charge *Charge) (string, error) {
	ch, err := s.pagarme.CancelCharge(ctx, charge.ProviderChargeID, "refund-"+charge.ID.String())
	if err != nil {
		return "", fmt.Errorf("pedindo estorno na Pagar.me: %w", err)
	}
	return ch.ID, nil
}

// HandlePagarmeWebhook reconsulta na Pagar.me as cobranças citadas. Como no
// da AbacatePay, o corpo só diz o que olhar.
func (s *Service) HandlePagarmeWebhook(ctx context.Context, ev *pagarme.Event) error {
	if s.pagarme == nil {
		return nil
	}
	charges, err := s.repo.ByProviderIDs(ctx, ev.ChargeIDs())
	if err != nil {
		return err
	}
	if len(charges) == 0 {
		s.log.Info("webhook da Pagar.me sem cobrança nossa; ignorado", "event", ev.Type, "id", ev.ID)
		return nil
	}
	expectsPaid := ev.Type == "charge.paid" || ev.Type == "order.paid"
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

// RecordPagarmeWebhook marca o evento como processado.
func (s *Service) RecordPagarmeWebhook(ctx context.Context, ev *pagarme.Event) error {
	return s.repo.RecordWebhook(ctx, ev.ID, providerPagarme, ev.Type)
}

// ---------------------------------------------------------------------------
// O cartão na Pagar.me
// ---------------------------------------------------------------------------

// savedPagarmePrefix marca o cartão salvo na carteira da Pagar.me: o cliente
// dela e o cartão ("pagarme:cus_...:card_..."), cifrados como o da AbacatePay.
const savedPagarmePrefix = "pagarme:"

// cardPagarme cobra no cartão pela Pagar.me, no mesmo molde do da AbacatePay:
// a cobrança é gravada antes (CREATING) com o nosso id como código do pedido e
// chave de idempotência, para a conferência achar o que ficou sem resposta.
func (s *Service) cardPagarme(ctx context.Context, customerID uuid.UUID, invoices []*billing.Invoice, total int,
	in CardInput, payer *Payer) (*Charge, error) {
	req := pagarme.OrderRequest{
		AmountCents: total, Method: pagarme.MethodCreditCard, Description: "Farbo Rastreadores — fatura",
		StatementDescriptor: s.cfg.PagarmeStatementDescriptor,
	}
	switch {
	case in.UseSaved:
		plain, err := s.savedCardToken(ctx, customerID)
		if err != nil {
			return nil, err
		}
		parts := strings.Split(strings.TrimPrefix(plain, savedPagarmePrefix), ":")
		if !strings.HasPrefix(plain, savedPagarmePrefix) || len(parts) != 2 {
			// O salvo é de antes, da AbacatePay: o cliente cadastra de novo.
			return nil, ErrNoSavedCard
		}
		req.CustomerID, req.CardID = parts[0], parts[1]
	case strings.TrimSpace(in.Token) != "":
		customer := pagarmeCustomer(payer)
		if customer == nil {
			return nil, ErrCardPayerIncomplete
		}
		if customer.Address == nil {
			return nil, ErrCardAddressMissing
		}
		req.Customer, req.CardToken, req.BillingAddress = customer, strings.TrimSpace(in.Token), customer.Address
	default:
		return nil, ErrCardTokenMissing
	}

	ids := make([]uuid.UUID, len(invoices))
	items := make([]ChargeInvoice, len(invoices))
	for i, inv := range invoices {
		ids[i] = inv.ID
		items[i] = ChargeInvoice{InvoiceID: inv.ID, AmountCents: inv.AmountCents}
	}
	id := uuid.New()
	req.Code = id.String()
	plan := s.planSplit(ctx, id, customerID, invoices, total)
	req.Split = s.splitRules(plan, total)
	req.Metadata = map[string]string{"chargeId": id.String(), "invoiceId": ids[0].String(), "customerId": customerID.String()}
	if len(ids) > 1 {
		req.Metadata["invoices"] = fmt.Sprint(len(ids))
	}
	for k, v := range in.Metadata {
		req.Metadata[k] = v
	}
	charge := &Charge{
		ID: id, InvoiceID: ids[0], Provider: providerPagarme, Method: MethodCard,
		ProviderChargeID: "creating:" + id.String(), AmountCents: total, Status: StatusCreating,
		DevMode: s.pagarme.TestMode(), split: plan,
	}
	var err error
	if len(ids) > 1 {
		err = s.repo.InsertGroup(ctx, charge, items)
	} else {
		err = s.repo.Insert(ctx, charge)
	}
	if err != nil {
		s.releaseSplit(ctx, id, plan)
		return nil, err
	}
	s.commitSplit(ctx, charge)

	order, err := s.pagarme.CreateOrder(ctx, req)
	var apiErr *pagarme.APIError
	switch {
	case errors.As(err, &apiErr) && apiErr.HTTPStatus < 500:
		// Recusado na entrada (dado inválido): não houve cobrança.
		if serr := s.repo.SetStatus(ctx, id, abacatepay.StatusFailed, false); serr != nil {
			return nil, serr
		}
		s.log.Info("cartão recusado pela Pagar.me", "charge", id, "customer", customerID, "err", apiErr.Message)
		charge.Status = abacatepay.StatusFailed
		return charge, err
	case err != nil:
		s.log.Warn("cartão sem resposta da Pagar.me; a conferência decide", "charge", id, "customer", customerID, "err", err)
		return s.withInvoiceStatus(ctx, charge), ErrCardUnknown
	}
	ch := order.FirstCharge()
	if ch == nil {
		return s.withInvoiceStatus(ctx, charge), ErrCardUnknown
	}
	if err := s.recordPagarmeCard(ctx, charge, ch); err != nil {
		return nil, err
	}
	if charge.Status == abacatepay.StatusFailed {
		reason := ch.LastTransaction.FailureMessage()
		if reason == "" {
			reason = "o banco recusou o cartão"
		}
		s.log.Info("cartão recusado", "charge", id, "customer", customerID, "err", reason)
		return charge, &pagarme.APIError{HTTPStatus: 402, Message: reason}
	}
	if in.Save && !in.UseSaved && ch.Customer != nil && ch.LastTransaction != nil && ch.LastTransaction.Card != nil &&
		ch.LastTransaction.Card.ID != "" {
		// O pagamento passou; se só guardar o cartão falhar, a fatura não volta.
		card := ch.LastTransaction.Card
		if err := s.storeCard(ctx, customerID, savedPagarmePrefix+ch.Customer.ID+":"+card.ID, card.Brand,
			card.LastFourDigits, in.AutoDebit, in.By); err != nil {
			s.log.Error("cartão pago, mas não guardado", "customer", customerID, "err", err)
		}
	}
	s.log.Info("cobrança no cartão (Pagar.me)", "charge", id, "customer", customerID, "invoices", len(ids), "amount_cents", total,
		"status", charge.Status, "saved_card", in.UseSaved, "dev_mode", charge.DevMode)
	return s.Charge(ctx, id)
}

// recordPagarmeCard grava a cobrança que a Pagar.me criou e aplica o status.
func (s *Service) recordPagarmeCard(ctx context.Context, charge *Charge, ch *pagarme.Charge) error {
	amount := ch.Amount
	if amount <= 0 {
		amount = charge.AmountCents
	}
	var brand, last4 string
	if ch.LastTransaction != nil && ch.LastTransaction.Card != nil {
		brand, last4 = ch.LastTransaction.Card.Brand, ch.LastTransaction.Card.LastFourDigits
	}
	if err := s.repo.SetProviderCharge(ctx, charge.ID, ch.ID, amount, brand, last4, charge.DevMode, 0, nil); err != nil {
		return err
	}
	charge.ProviderChargeID, charge.AmountCents, charge.Status = ch.ID, amount, abacatepay.StatusPending
	charge.CardBrand, charge.CardLast4 = brand, last4
	status := internalStatus(ch.Outcome())
	if err := s.apply(ctx, charge, status); err != nil {
		return err
	}
	charge.Status = status
	return nil
}

// reconcilePagarmeCard procura na Pagar.me, pelo código, o pedido do cartão
// que ficou sem resposta.
func (s *Service) reconcilePagarmeCard(ctx context.Context, charge *Charge) {
	order, err := s.pagarme.FindOrder(ctx, charge.ID.String())
	switch {
	case err != nil:
		s.log.Warn("falha ao conferir cobrança de cartão na Pagar.me", "charge", charge.ID, "err", err)
	case order != nil && order.FirstCharge() != nil:
		if err := s.recordPagarmeCard(ctx, charge, order.FirstCharge()); err != nil {
			s.log.Error("falha ao gravar cobrança de cartão conferida", "charge", charge.ID, "err", err)
		} else {
			s.log.Info("cobrança de cartão conferida", "charge", charge.ID, "provider_charge", order.FirstCharge().ID)
		}
	case time.Since(charge.CreatedAt) > cardGiveUp:
		if err := s.repo.SetStatus(ctx, charge.ID, abacatepay.StatusFailed, false); err != nil {
			s.log.Error("falha ao encerrar cobrança de cartão sem resposta", "charge", charge.ID, "err", err)
		} else {
			s.log.Info("cobrança de cartão não chegou à Pagar.me", "charge", charge.ID)
		}
	}
}

// ProviderError é a recusa de um dos gateways: o status HTTP e a mensagem
// que ele devolveu (false se o erro não veio de um gateway).
func ProviderError(err error) (int, string, bool) {
	var a *abacatepay.APIError
	if errors.As(err, &a) {
		return a.HTTPStatus, a.Message, true
	}
	var p *pagarme.APIError
	if errors.As(err, &p) {
		return p.HTTPStatus, p.Message, true
	}
	return 0, "", false
}
