package abacatepay

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// O cartão de crédito (checkout transparente, método CARD). Na primeira vez
// o navegador gera um token de uso único (ctok_..., 60 s) com o
// abacatepay.js, que monta os campos do cartão num iframe: o número nunca
// passa por aqui. A cobrança aprovada devolve o cardToken, permanente, com
// que o backend cobra o mesmo cartão de novo sem o cliente presente (o
// débito automático). O cardToken nunca vai para o navegador.
//
// A cobrança de cartão não aceita description nem externalId: o vínculo com
// a nossa cobrança vai em metadata.

// StatusApproved: o cartão foi aprovado (pode liberar); PAID vem depois,
// quando a adquirente repassa o valor.
const StatusApproved = "APPROVED"

// StatusFailed: a cobrança do cartão não passou.
const StatusFailed = "FAILED"

// CardRequest é a cobrança no cartão: Token (do formulário) ou CardToken
// (o cartão salvo), nunca os dois.
type CardRequest struct {
	AmountCents int
	Token       string
	CardToken   string
	// Customer: os quatro campos são obrigatórios no cartão.
	Customer Customer
	Metadata map[string]string
}

// Card é a cobrança no cartão.
type Card struct {
	ID          string            `json:"id"`
	Amount      int               `json:"amount"`
	Status      string            `json:"status"`
	DevMode     bool              `json:"devMode"`
	CardToken   string            `json:"cardToken"`
	Brand       string            `json:"brand"`
	LastFour    string            `json:"lastFour"`
	PlatformFee int               `json:"platformFee"`
	ReceiptURL  string            `json:"receiptUrl"`
	ExpiresAt   *time.Time        `json:"expiresAt"`
	CreatedAt   time.Time         `json:"createdAt"`
	Metadata    map[string]string `json:"metadata"`
}

// CreateCard cobra o cartão. A recusa volta como APIError (a mensagem da
// AbacatePay é genérica de propósito). Erro de rede ou tempo esgotado não é
// recusa: a cobrança pode ter passado — confira com FindCard antes de tentar
// de novo.
func (c *Client) CreateCard(ctx context.Context, req CardRequest) (*Card, error) {
	data := map[string]any{"amount": req.AmountCents, "customer": req.Customer}
	if req.Token != "" {
		data["token"] = req.Token
	} else {
		data["cardToken"] = req.CardToken
	}
	if len(req.Metadata) > 0 {
		data["metadata"] = req.Metadata
	}
	var card Card
	if err := c.do(ctx, http.MethodPost, "/transparents/create", nil,
		map[string]any{"method": "CARD", "data": data}, &card); err != nil {
		return nil, err
	}
	return &card, nil
}

// FindCard procura, entre as cobranças de cartão criadas desde `since`, a que
// leva metadata[key] = value — a conferência depois de um erro de rede.
func (c *Client) FindCard(ctx context.Context, since time.Time, key, value string) (*Card, error) {
	query := url.Values{"method": {"CARD"}, "startDate": {since.UTC().Format(time.DateOnly)}, "limit": {"100"}}
	for page := 0; page < 20; page++ {
		var list []Card
		var env struct {
			Pagination struct {
				HasMore bool   `json:"hasMore"`
				Next    string `json:"next"`
			} `json:"pagination"`
		}
		if err := c.doPaged(ctx, "/transparents/list", query, &list, &env); err != nil {
			return nil, err
		}
		for i := range list {
			if list[i].Metadata[key] == value {
				return &list[i], nil
			}
		}
		if !env.Pagination.HasMore || env.Pagination.Next == "" {
			return nil, nil
		}
		query.Set("after", env.Pagination.Next)
	}
	return nil, nil
}
