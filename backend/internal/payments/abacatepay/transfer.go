package abacatepay

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Envio de Pix a terceiros (pagar fornecedores) e o saldo da conta. Sai do
// saldo da AbacatePay; a chave precisa da permissão WITHDRAW:CREATE (e
// STORE:READ para o saldo).

// Tipos de chave Pix de destino.
const (
	KeyCPF    = "CPF"
	KeyCNPJ   = "CNPJ"
	KeyPhone  = "PHONE"
	KeyEmail  = "EMAIL"
	KeyRandom = "RANDOM"
	// KeyBRCode é o Pix copia-e-cola (QR Code) do recebedor.
	KeyBRCode = "BR_CODE"
)

// Status de um envio: COMPLETE é criado e enviado; FAILED, o envio falhou e
// o valor voltou ao saldo (pode chegar depois de um COMPLETE).
const (
	TransferComplete = "COMPLETE"
	TransferFailed   = "FAILED"
)

// MinTransferCents é o mínimo de um envio (R$ 1,00).
const MinTransferCents = 100

// TransferRequest é o Pix a enviar.
type TransferRequest struct {
	AmountCents int
	// ExternalID é o identificador do envio no nosso sistema.
	ExternalID  string
	Description string
	Key         string
	KeyType     string
}

// Transfer é o envio na AbacatePay.
type Transfer struct {
	ID          string    `json:"id"`
	Status      string    `json:"status"`
	DevMode     bool      `json:"devMode"`
	ReceiptURL  string    `json:"receiptUrl"`
	Amount      int       `json:"amount"`
	PlatformFee int       `json:"platformFee"`
	ExternalID  string    `json:"externalId"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// SendPix envia o Pix. A resposta já diz se saiu (COMPLETE) ou não (FAILED).
func (c *Client) SendPix(ctx context.Context, req TransferRequest) (*Transfer, error) {
	body := map[string]any{
		"amount":     req.AmountCents,
		"externalId": req.ExternalID,
		"pix":        map[string]any{"key": req.Key, "type": req.KeyType},
	}
	if description := sanitizeDescription(req.Description); description != "" {
		body["description"] = truncate(description, 140)
	}
	var t Transfer
	if err := c.do(ctx, http.MethodPost, "/pix/send", nil, body, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

// GetTransfer consulta um envio pelo id da AbacatePay (tran_...).
func (c *Client) GetTransfer(ctx context.Context, id string) (*Transfer, error) {
	var t Transfer
	if err := c.do(ctx, http.MethodGet, "/pix/get", url.Values{"id": {id}}, nil, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

// Balance é o saldo da conta, em centavos: Available é o que pode sair.
type Balance struct {
	Available int `json:"available"`
	Pending   int `json:"pending"`
	Blocked   int `json:"blocked"`
}

// Balance consulta o saldo da loja.
func (c *Client) Balance(ctx context.Context) (*Balance, error) {
	var store struct {
		Balance Balance `json:"balance"`
	}
	if err := c.do(ctx, http.MethodGet, "/stores/get", nil, nil, &store); err != nil {
		return nil, err
	}
	return &store.Balance, nil
}

// IsDefinitive diz se o erro é uma recusa da AbacatePay (o Pix certamente
// não saiu) — e não uma falha de rede ou do servidor dela, em que o envio
// pode ter saído sem a resposta chegar.
func IsDefinitive(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.HTTPStatus >= 400 && apiErr.HTTPStatus < 500 && apiErr.HTTPStatus != http.StatusRequestTimeout
}

// TransferIDs devolve os ids de envio (tran_...) citados no evento.
func (e *Event) TransferIDs() []string { return e.idsWithPrefix("tran_") }

// IsTransferEvent diz se o evento é de um envio (transfer.completed,
// transfer.failed) ou de um saque.
func (e *Event) IsTransferEvent() bool {
	return strings.HasPrefix(e.Event, "transfer.") || strings.HasPrefix(e.Event, "payout.")
}
