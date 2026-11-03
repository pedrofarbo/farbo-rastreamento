// Package pagarme fala com a API v5 (core) da Pagar.me: pedidos com Pix,
// boleto e cartão de crédito (com o split para os recebedores), consulta e
// cancelamento (estorno) das cobranças, os recebedores com a prova de vida e
// os webhooks.
package pagarme

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// DefaultBaseURL é a API v5; a mesma URL atende as chaves de teste
// (sk_test_...), que operam no ambiente de testes.
const DefaultBaseURL = "https://api.pagar.me/core/v5"

// APIError é a recusa da Pagar.me, com a mensagem que ela devolveu.
type APIError struct {
	HTTPStatus int
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("Pagar.me respondeu %d: %s", e.HTTPStatus, e.Message)
}

// IsNotFound diz se a Pagar.me não achou o recurso.
func IsNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.HTTPStatus == http.StatusNotFound
}

// IsTestKey diz se a chave secreta é do ambiente de testes.
func IsTestKey(secretKey string) bool { return strings.HasPrefix(secretKey, "sk_test_") }

type Client struct {
	baseURL   string
	secretKey string
	http      *http.Client
}

func NewClient(baseURL, secretKey string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		baseURL:   strings.TrimRight(baseURL, "/"),
		secretKey: secretKey,
		http:      &http.Client{Timeout: 30 * time.Second},
	}
}

// TestMode diz se o cliente usa a chave de testes.
func (c *Client) TestMode() bool { return IsTestKey(c.secretKey) }

// do chama a API: autenticação básica com a chave secreta (sem senha) e, nas
// criações, a chave de idempotência (repetir o pedido devolve o mesmo).
func (c *Client) do(ctx context.Context, method, path string, query url.Values, idempotencyKey string, body, out any) error {
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
	req.SetBasicAuth(c.secretKey, "")
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		return &APIError{HTTPStatus: resp.StatusCode, Message: errorMessage(raw)}
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("resposta da Pagar.me ilegível: %w", err)
	}
	return nil
}

// errorMessage tira a mensagem da recusa: {"message": "...", "errors":
// {"campo": ["motivo"]}}.
func errorMessage(raw []byte) string {
	var body struct {
		Message string              `json:"message"`
		Errors  map[string][]string `json:"errors"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return truncate(strings.TrimSpace(string(raw)), 300)
	}
	parts := []string{}
	if body.Message != "" {
		parts = append(parts, body.Message)
	}
	fields := make([]string, 0, len(body.Errors))
	for field := range body.Errors {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	for _, field := range fields {
		parts = append(parts, field+": "+strings.Join(body.Errors[field], ", "))
	}
	if len(parts) == 0 {
		return truncate(strings.TrimSpace(string(raw)), 300)
	}
	return truncate(strings.Join(parts, "; "), 500)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
