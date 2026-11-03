package pagarme

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// As formas de pagamento do pedido.
const (
	MethodPix        = "pix"
	MethodBoleto     = "boleto"
	MethodCreditCard = "credit_card"
)

// Phone é o telefone no formato da Pagar.me (o DDI 55 sem o +).
type Phone struct {
	CountryCode string `json:"country_code"`
	AreaCode    string `json:"area_code"`
	Number      string `json:"number"`
}

// Phones: a Pagar.me pede o celular no Pix.
type Phones struct {
	MobilePhone *Phone `json:"mobile_phone,omitempty"`
}

// Address é o endereço: line_1 com "número, rua, bairro" e line_2 com o
// complemento, como a Pagar.me recomenda.
type Address struct {
	Line1   string `json:"line_1"`
	Line2   string `json:"line_2,omitempty"`
	ZipCode string `json:"zip_code"`
	City    string `json:"city"`
	State   string `json:"state"`
	Country string `json:"country"`
}

// Customer é o pagador. Type: individual (CPF) ou company (CNPJ).
type Customer struct {
	Name     string   `json:"name"`
	Email    string   `json:"email"`
	Document string   `json:"document"`
	Type     string   `json:"type"`
	Phones   Phones   `json:"phones"`
	Address  *Address `json:"address,omitempty"`
}

// SplitRule é a parte de um recebedor no pagamento. Type flat: Amount em
// centavos; percentage: Amount em pontos percentuais.
type SplitRule struct {
	Amount      int          `json:"amount"`
	RecipientID string       `json:"recipient_id"`
	Type        string       `json:"type"`
	Options     SplitOptions `json:"options"`
}

// SplitOptions: quem responde pelo chargeback (Liable), quem paga a tarifa
// (ChargeProcessingFee) e quem fica com a sobra do arredondamento
// (ChargeRemainderFee). Ao menos um recebedor tem cada uma.
type SplitOptions struct {
	Liable              bool `json:"liable"`
	ChargeProcessingFee bool `json:"charge_processing_fee"`
	ChargeRemainderFee  bool `json:"charge_remainder_fee"`
}

// OrderRequest é o pedido a criar: uma cobrança, numa forma de pagamento.
type OrderRequest struct {
	// Code é o nosso id da cobrança (também a chave de idempotência).
	Code        string
	AmountCents int
	Description string
	Customer    *Customer
	// CustomerID: o cliente da Pagar.me (o do cartão salvo), no lugar de Customer.
	CustomerID string
	Method     string
	// Pix: a validade.
	PixExpiresIn time.Duration
	// Boleto: o vencimento e as instruções.
	BoletoDueAt        time.Time
	BoletoInstructions string
	// Cartão: o token do formulário (token_...) ou o cartão salvo (card_...),
	// com o endereço de cobrança.
	CardToken           string
	CardID              string
	BillingAddress      *Address
	StatementDescriptor string
	Split               []SplitRule
	Metadata            map[string]string
}

type orderItem struct {
	Amount      int    `json:"amount"`
	Description string `json:"description"`
	Quantity    int    `json:"quantity"`
	Code        string `json:"code"`
}

type cardPayment struct {
	Installments        int              `json:"installments"`
	StatementDescriptor string           `json:"statement_descriptor,omitempty"`
	OperationType       string           `json:"operation_type"`
	CardToken           string           `json:"card_token,omitempty"`
	CardID              string           `json:"card_id,omitempty"`
	Card                *cardBillingOnly `json:"card,omitempty"`
}

type cardBillingOnly struct {
	BillingAddress *Address `json:"billing_address,omitempty"`
}

type payment struct {
	PaymentMethod string         `json:"payment_method"`
	Pix           map[string]any `json:"pix,omitempty"`
	Boleto        map[string]any `json:"boleto,omitempty"`
	CreditCard    *cardPayment   `json:"credit_card,omitempty"`
	Split         []SplitRule    `json:"split,omitempty"`
}

// Order é o pedido criado, com a cobrança.
type Order struct {
	ID      string    `json:"id"`
	Code    string    `json:"code"`
	Status  string    `json:"status"`
	Charges []*Charge `json:"charges"`
}

// Charge é a cobrança do pedido.
type Charge struct {
	ID              string            `json:"id"`
	Code            string            `json:"code"`
	Amount          int               `json:"amount"`
	Status          string            `json:"status"`
	PaymentMethod   string            `json:"payment_method"`
	PaidAt          *time.Time        `json:"paid_at"`
	PaidAmount      int               `json:"paid_amount"`
	CanceledAt      *time.Time        `json:"canceled_at"`
	CanceledAmount  int               `json:"canceled_amount"`
	Customer        *CustomerRef      `json:"customer"`
	LastTransaction *Transaction      `json:"last_transaction"`
	Metadata        map[string]string `json:"metadata"`
}

// CustomerRef é o cliente que a Pagar.me criou (ou usou) para o pedido.
type CustomerRef struct {
	ID string `json:"id"`
}

// Transaction é a tentativa de pagamento da cobrança: o QR Code do Pix, o
// boleto ou o cartão.
type Transaction struct {
	ID              string     `json:"id"`
	TransactionType string     `json:"transaction_type"`
	Status          string     `json:"status"`
	Success         bool       `json:"success"`
	QRCode          string     `json:"qr_code"`
	QRCodeURL       string     `json:"qr_code_url"`
	ExpiresAt       *time.Time `json:"expires_at"`
	Line            string     `json:"line"`
	Barcode         string     `json:"barcode"`
	PDF             string     `json:"pdf"`
	URL             string     `json:"url"`
	DueAt           *time.Time `json:"due_at"`
	Card            *Card      `json:"card"`
	AcquirerMessage string     `json:"acquirer_message"`
	GatewayResponse *struct {
		Code   string `json:"code"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	} `json:"gateway_response"`
}

// Card é o cartão da transação (ou o salvo na carteira do cliente).
type Card struct {
	ID             string `json:"id"`
	LastFourDigits string `json:"last_four_digits"`
	Brand          string `json:"brand"`
}

// FailureMessage é o motivo da recusa, como a Pagar.me (ou o banco) disse.
func (t *Transaction) FailureMessage() string {
	if t == nil {
		return ""
	}
	if t.AcquirerMessage != "" {
		return t.AcquirerMessage
	}
	if t.GatewayResponse != nil {
		msgs := make([]string, 0, len(t.GatewayResponse.Errors))
		for _, e := range t.GatewayResponse.Errors {
			if e.Message != "" {
				msgs = append(msgs, e.Message)
			}
		}
		return strings.Join(msgs, "; ")
	}
	return ""
}

// CreateOrder cria o pedido com a cobrança. O Code vai também como chave de
// idempotência: repetir o mesmo pedido (uma rede que caiu no meio) devolve o
// pedido que já existe, sem cobrar duas vezes.
func (c *Client) CreateOrder(ctx context.Context, req OrderRequest) (*Order, error) {
	pay := payment{PaymentMethod: req.Method, Split: req.Split}
	switch req.Method {
	case MethodPix:
		pay.Pix = map[string]any{"expires_in": int(req.PixExpiresIn.Seconds())}
	case MethodBoleto:
		boleto := map[string]any{"instructions": req.BoletoInstructions}
		if !req.BoletoDueAt.IsZero() {
			boleto["due_at"] = req.BoletoDueAt.UTC().Format(time.RFC3339)
		}
		pay.Boleto = boleto
	case MethodCreditCard:
		card := &cardPayment{
			Installments: 1, StatementDescriptor: req.StatementDescriptor, OperationType: "auth_and_capture",
			CardToken: req.CardToken, CardID: req.CardID,
		}
		if req.BillingAddress != nil && req.CardToken != "" {
			card.Card = &cardBillingOnly{BillingAddress: req.BillingAddress}
		}
		pay.CreditCard = card
	}
	body := map[string]any{
		"code":     req.Code,
		"items":    []orderItem{{Amount: req.AmountCents, Description: truncate(req.Description, 256), Quantity: 1, Code: "fatura"}},
		"payments": []payment{pay},
		"closed":   true,
	}
	if req.CustomerID != "" {
		body["customer_id"] = req.CustomerID
	} else if req.Customer != nil {
		body["customer"] = req.Customer
	}
	if len(req.Metadata) > 0 {
		body["metadata"] = req.Metadata
	}
	var order Order
	if err := c.do(ctx, http.MethodPost, "/orders", nil, req.Code, body, &order); err != nil {
		return nil, err
	}
	return &order, nil
}

// FirstCharge é a cobrança do pedido (nil se não veio nenhuma).
func (o *Order) FirstCharge() *Charge {
	if o == nil || len(o.Charges) == 0 {
		return nil
	}
	return o.Charges[0]
}

// GetCharge consulta a cobrança.
func (c *Client) GetCharge(ctx context.Context, id string) (*Charge, error) {
	var charge Charge
	if err := c.do(ctx, http.MethodGet, "/charges/"+url.PathEscape(id), nil, "", nil, &charge); err != nil {
		return nil, err
	}
	return &charge, nil
}

// CancelCharge cancela a cobrança pendente ou estorna a paga (inteira). Com
// split, a Pagar.me desfaz a parte de cada recebedor junto.
func (c *Client) CancelCharge(ctx context.Context, id, idempotencyKey string) (*Charge, error) {
	var charge Charge
	if err := c.do(ctx, http.MethodDelete, "/charges/"+url.PathEscape(id), nil, idempotencyKey, nil, &charge); err != nil {
		return nil, err
	}
	return &charge, nil
}

// FindOrder procura o pedido pelo nosso código (nil se não existe): é como a
// conferência acha o cartão que ficou sem resposta.
func (c *Client) FindOrder(ctx context.Context, code string) (*Order, error) {
	var page struct {
		Data []*Order `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, "/orders", url.Values{"code": {code}, "size": {"5"}}, "", nil, &page); err != nil {
		return nil, err
	}
	for _, o := range page.Data {
		if o.Code == code {
			return o, nil
		}
	}
	return nil, nil
}

// Os resultados de uma cobrança, do jeito que a cobrança da Farbo entende.
const (
	OutcomePending  = "pending"
	OutcomePaid     = "paid"
	OutcomeRefunded = "refunded"
	OutcomeExpired  = "expired"
	OutcomeCanceled = "canceled"
	OutcomeFailed   = "failed"
	// OutcomeRefunding: paga, com o estorno pedido e ainda em andamento.
	OutcomeRefunding = "refunding"
)

// Outcome traduz o status da cobrança. Cancelada depois de paga é estorno;
// sem ter sido paga, o Pix vencido é expirado e o resto, cancelado. O
// chargeback devolve o dinheiro ao pagador: conta como estorno.
func (ch *Charge) Outcome() string {
	paidOnce := ch.PaidAt != nil || ch.PaidAmount > 0
	tx := ""
	if ch.LastTransaction != nil {
		tx = ch.LastTransaction.Status
	}
	switch ch.Status {
	case "paid", "overpaid":
		if tx == "pending_refund" || tx == "waiting_cancellation" {
			return OutcomeRefunding
		}
		return OutcomePaid
	case "canceled", "chargedback":
		if paidOnce || ch.Status == "chargedback" {
			return OutcomeRefunded
		}
		if ch.PaymentMethod == MethodPix {
			return OutcomeExpired
		}
		return OutcomeCanceled
	case "failed":
		if ch.PaymentMethod == MethodPix {
			return OutcomeExpired
		}
		return OutcomeFailed
	}
	// pending, processing, underpaid (boleto pago a menos: não quita).
	return OutcomePending
}
