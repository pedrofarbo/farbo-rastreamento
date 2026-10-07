// Package abacatepay fala com a API v2 da AbacatePay: Pix por checkout
// transparente (criar, consultar, simular em testes) e verificação dos
// webhooks.
package abacatepay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
)

// DefaultBaseURL é a API v2 de produção; a mesma URL atende as chaves de
// teste (abc_dev_...), que operam em modo de desenvolvimento.
const DefaultBaseURL = "https://api.abacatepay.com/v2"

// Status de um Pix na AbacatePay.
const (
	StatusPending      = "PENDING"
	StatusPaid         = "PAID"
	StatusExpired      = "EXPIRED"
	StatusCancelled    = "CANCELLED"
	StatusRefunded     = "REFUNDED"
	StatusUnderDispute = "UNDER_DISPUTE"
)

// PixReceiveFeeCents é a tarifa da AbacatePay por Pix recebido (R$ 0,80 por
// transação), descontada do valor pago: vale quando a resposta não informa.
const PixReceiveFeeCents = 80

// APIError é a recusa da AbacatePay, com a mensagem que ela devolveu.
type APIError struct {
	HTTPStatus int
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("AbacatePay respondeu %d: %s", e.HTTPStatus, e.Message)
}

// IsDevKey diz se a chave é de testes (sandbox).
func IsDevKey(apiKey string) bool { return strings.HasPrefix(apiKey, "abc_dev_") }

type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

func NewClient(baseURL, apiKey string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		http:    &http.Client{Timeout: 20 * time.Second},
	}
}

// Customer identifica o pagador. Na AbacatePay, se o objeto vai, todos os
// campos são obrigatórios e o CPF/CNPJ precisa ser válido.
type Customer struct {
	Name      string `json:"name"`
	Email     string `json:"email"`
	TaxID     string `json:"taxId"`
	Cellphone string `json:"cellphone"`
}

// PixRequest é o Pix a criar.
type PixRequest struct {
	AmountCents int
	Description string
	ExpiresIn   time.Duration
	// ExternalID é o identificador da cobrança no nosso sistema.
	ExternalID string
	Customer   *Customer
	Metadata   map[string]string
}

// Pix é a cobrança criada.
type Pix struct {
	ID           string            `json:"id"`
	Amount       int               `json:"amount"`
	Status       string            `json:"status"`
	DevMode      bool              `json:"devMode"`
	BrCode       string            `json:"brCode"`
	BrCodeBase64 string            `json:"brCodeBase64"`
	PlatformFee  int               `json:"platformFee"`
	ExpiresAt    *time.Time        `json:"expiresAt"`
	CreatedAt    time.Time         `json:"createdAt"`
	Metadata     map[string]string `json:"metadata"`
}

// PixStatus é a resposta da consulta de status.
type PixStatus struct {
	ID        string     `json:"id"`
	Status    string     `json:"status"`
	ExpiresAt *time.Time `json:"expiresAt"`
}

// CreatePix gera o Pix (checkout transparente): devolve o copia-e-cola e a
// imagem do QR Code, sem redirecionar o pagador.
func (c *Client) CreatePix(ctx context.Context, req PixRequest) (*Pix, error) {
	data := map[string]any{"amount": req.AmountCents}
	if description := sanitizeDescription(req.Description); description != "" {
		data["description"] = truncate(description, 500)
	}
	if req.ExpiresIn > 0 {
		data["expiresIn"] = int(req.ExpiresIn.Seconds())
	}
	if req.ExternalID != "" {
		data["externalId"] = req.ExternalID
	}
	if req.Customer != nil {
		data["customer"] = req.Customer
	}
	if len(req.Metadata) > 0 {
		data["metadata"] = req.Metadata
	}

	var pix Pix
	err := c.do(ctx, http.MethodPost, "/transparents/create", nil,
		map[string]any{"method": "PIX", "data": data}, &pix)
	if err != nil {
		return nil, err
	}
	return &pix, nil
}

// CheckPix consulta o status atual de um Pix.
func (c *Client) CheckPix(ctx context.Context, id string) (*PixStatus, error) {
	var status PixStatus
	if err := c.do(ctx, http.MethodGet, "/transparents/check", url.Values{"id": {id}}, nil, &status); err != nil {
		return nil, err
	}
	return &status, nil
}

// SimulatePayment marca o Pix como pago. Só funciona com chave de testes; em
// produção a AbacatePay recusa.
func (c *Client) SimulatePayment(ctx context.Context, id string) (*Pix, error) {
	var pix Pix
	err := c.do(ctx, http.MethodPost, "/transparents/simulate-payment", url.Values{"id": {id}},
		map[string]any{"metadata": map[string]any{}}, &pix)
	if err != nil {
		return nil, err
	}
	return &pix, nil
}

// Refund é o estorno criado. A documentação mostra refundPublicId; a API
// devolve id (tran_...), status e originalId. Aceitamos os dois formatos.
type Refund struct {
	ID             string `json:"id"`
	RefundPublicID string `json:"refundPublicId"`
	Status         string `json:"status"`
	Amount         int    `json:"amount"`
	OriginalID     string `json:"originalId"`
}

// RefundID devolve o identificador do estorno, em qualquer dos formatos.
func (r *Refund) RefundID() string {
	if r.RefundPublicID != "" {
		return r.RefundPublicID
	}
	return r.ID
}

// RefundPix devolve ao pagador o valor integral de um Pix pago (a
// AbacatePay não faz estorno parcial). Em testes conclui na hora; em
// produção é assíncrono e termina com o webhook transparent.refunded.
func (c *Client) RefundPix(ctx context.Context, id, reason string) (*Refund, error) {
	body := map[string]any{"id": id}
	if reason = strings.TrimSpace(reason); reason != "" {
		body["reason"] = truncate(reason, 500)
	}
	var refund Refund
	if err := c.do(ctx, http.MethodPost, "/transparents/refund", nil, body, &refund); err != nil {
		return nil, err
	}
	return &refund, nil
}

// envelope é o formato de toda resposta: { data, success, error }.
type envelope struct {
	Data    json.RawMessage `json:"data"`
	Success bool            `json:"success"`
	Error   json.RawMessage `json:"error"`
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	endpoint := c.baseURL + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}

	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")
	// O firewall da AbacatePay (Cloudflare) barra alguns agentes genéricos;
	// um nome próprio também ajuda o suporte deles a achar nossas chamadas.
	req.Header.Set("User-Agent", "FarboRastreadores/1.0")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("AbacatePay indisponível: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return fmt.Errorf("lendo resposta da AbacatePay: %w", err)
	}

	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return &APIError{HTTPStatus: resp.StatusCode, Message: "resposta fora do formato esperado"}
	}
	if resp.StatusCode >= 300 || !env.Success {
		return &APIError{HTTPStatus: resp.StatusCode, Message: errorMessage(env.Error)}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		return fmt.Errorf("decodificando resposta da AbacatePay: %w", err)
	}
	return nil
}

// errorMessage aceita o campo error como texto ou objeto.
func errorMessage(raw json.RawMessage) string {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil && text != "" {
		return text
	}
	var obj struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil && obj.Message != "" {
		return obj.Message
	}
	if len(raw) > 0 && string(raw) != "null" {
		return string(raw)
	}
	return "erro sem descrição"
}

// sanitizeDescription adapta o texto ao que a AbacatePay aceita: ela recusa o
// Pix inteiro por um travessão ("—", "–") ou um emoji, mas aceita acentos e a
// pontuação comum. Travessões e aspas curvas viram os equivalentes simples;
// emojis e outros símbolos gráficos saem.
func sanitizeDescription(s string) string {
	replacer := strings.NewReplacer(
		"\u2014", "-", "\u2013", "-", "\u2012", "-", "\u2212", "-", "\u2010", "-", "\u2011", "-",
		"\u201C", `"`, "\u201D", `"`, "\u2018", "'", "\u2019", "'", "\u2026", "...",
	)
	s = replacer.Replace(s)

	var b strings.Builder
	for _, r := range s {
		switch {
		// Quebra de linha e tabula\u00E7\u00E3o tamb\u00E9m s\u00E3o caracteres de controle: o
		// espa\u00E7o precisa ser tratado antes, sen\u00E3o as palavras grudam.
		case unicode.IsSpace(r):
			b.WriteRune(' ')
		case r > 0xFFFF, unicode.Is(unicode.So, r), unicode.Is(unicode.Sk, r),
			r == '\u200D', r == '\uFE0F', unicode.IsControl(r):
			continue
		default:
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func truncate(s string, max int) string {
	if len([]rune(s)) <= max {
		return s
	}
	return string([]rune(s)[:max])
}

// IsNotFound diz se a AbacatePay não reconheceu o id consultado.
func IsNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && strings.Contains(strings.ToLower(apiErr.Message), "not found")
}
