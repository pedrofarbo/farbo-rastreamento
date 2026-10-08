// Package twilio manda SMS pela API de mensagens do Twilio (2010-04-01) e
// confere a assinatura dos webhooks (status de entrega e SMS recebidos).
package twilio

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
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

// DefaultBaseURL é a API do Twilio.
const DefaultBaseURL = "https://api.twilio.com/2010-04-01"

// SignatureHeader carrega a assinatura de cada webhook.
const SignatureHeader = "X-Twilio-Signature"

// Status de uma mensagem que não mudam mais.
const (
	StatusDelivered   = "delivered"
	StatusUndelivered = "undelivered"
	StatusFailed      = "failed"
	StatusCanceled    = "canceled"
	StatusReceived    = "received"
)

// Final diz se o status da mensagem enviada não muda mais.
func Final(status string) bool {
	switch status {
	case StatusDelivered, StatusUndelivered, StatusFailed, StatusCanceled:
		return true
	}
	return false
}

// Failed diz se a mensagem não chegou.
func Failed(status string) bool {
	return status == StatusUndelivered || status == StatusFailed || status == StatusCanceled
}

// Config é a conta e o remetente: um número (From, E.164) ou um Messaging
// Service. Com a API Key (SK... e o segredo), as chamadas à API usam ela — dá
// para revogá-la sem trocar o token da conta; o Auth Token fica para conferir
// a assinatura dos webhooks (o Twilio assina com ele).
type Config struct {
	BaseURL             string
	AccountSID          string
	AuthToken           string
	APIKeySID           string
	APIKeySecret        string
	From                string
	MessagingServiceSID string
}

// usesAPIKey diz se as chamadas vão com a API Key.
func (c Config) usesAPIKey() bool { return c.APIKeySID != "" && c.APIKeySecret != "" }

// Enabled diz se dá para mandar SMS: a conta, uma credencial (API Key ou Auth
// Token) e o remetente.
func (c Config) Enabled() bool {
	return c.AccountSID != "" && (c.AuthToken != "" || c.usesAPIKey()) && (c.From != "" || c.MessagingServiceSID != "")
}

type Client struct {
	cfg  Config
	http *http.Client
}

func NewClient(cfg Config) *Client {
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	return &Client{cfg: cfg, http: &http.Client{Timeout: 20 * time.Second}}
}

// Sender é quem envia (para a tela): o número ou o Messaging Service.
func (c *Client) Sender() string {
	if c.cfg.From != "" {
		return c.cfg.From
	}
	return c.cfg.MessagingServiceSID
}

// Message é a mensagem no Twilio.
type Message struct {
	SID          string `json:"sid"`
	Status       string `json:"status"`
	To           string `json:"to"`
	From         string `json:"from"`
	ErrorCode    *int   `json:"error_code"`
	ErrorMessage string `json:"error_message"`
	NumSegments  string `json:"num_segments"`
}

// APIError é a recusa do Twilio.
type APIError struct {
	HTTPStatus int
	Code       int
	Message    string
}

func (e *APIError) Error() string {
	if e.Code != 0 {
		return fmt.Sprintf("Twilio respondeu %d (erro %d): %s", e.HTTPStatus, e.Code, e.Message)
	}
	return fmt.Sprintf("Twilio respondeu %d: %s", e.HTTPStatus, e.Message)
}

// IsDefinitive diz se o erro é uma recusa (a mensagem certamente não saiu),
// e não uma falha de rede ou do servidor do Twilio.
func IsDefinitive(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.HTTPStatus >= 400 && apiErr.HTTPStatus < 500 && apiErr.HTTPStatus != http.StatusTooManyRequests
}

// Send manda o SMS. statusCallback (opcional) recebe as mudanças de status.
func (c *Client) Send(ctx context.Context, to, body, statusCallback string) (*Message, error) {
	form := url.Values{"To": {to}, "Body": {body}}
	if c.cfg.MessagingServiceSID != "" {
		form.Set("MessagingServiceSid", c.cfg.MessagingServiceSID)
	} else {
		form.Set("From", c.cfg.From)
	}
	if statusCallback != "" {
		form.Set("StatusCallback", statusCallback)
	}
	var msg Message
	if err := c.do(ctx, http.MethodPost, "/Messages.json", form, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

// Fetch consulta a mensagem (para quando o aviso de status não chega).
func (c *Client) Fetch(ctx context.Context, sid string) (*Message, error) {
	var msg Message
	if err := c.do(ctx, http.MethodGet, "/Messages/"+url.PathEscape(sid)+".json", nil, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

func (c *Client) do(ctx context.Context, method, path string, form url.Values, out any) error {
	endpoint := c.cfg.BaseURL + "/Accounts/" + url.PathEscape(c.cfg.AccountSID) + path
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return err
	}
	if c.cfg.usesAPIKey() {
		req.SetBasicAuth(c.cfg.APIKeySID, c.cfg.APIKeySecret)
	} else {
		req.SetBasicAuth(c.cfg.AccountSID, c.cfg.AuthToken)
	}
	req.Header.Set("Accept", "application/json")
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("Twilio indisponível: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("lendo resposta do Twilio: %w", err)
	}
	if resp.StatusCode >= 300 {
		var e struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal(raw, &e)
		if e.Message == "" {
			e.Message = http.StatusText(resp.StatusCode)
		}
		return &APIError{HTTPStatus: resp.StatusCode, Code: e.Code, Message: e.Message}
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decodificando resposta do Twilio: %w", err)
	}
	return nil
}

// Sign calcula a assinatura de um webhook: HMAC-SHA1 (chave: o Auth Token)
// da URL completa chamada pelo Twilio seguida dos parâmetros do corpo em
// ordem alfabética, cada um como nome+valor; em base64.
func Sign(authToken, fullURL string, params url.Values) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(fullURL)
	for _, k := range keys {
		values := append([]string(nil), params[k]...)
		sort.Strings(values)
		for _, v := range values {
			b.WriteString(k)
			b.WriteString(v)
		}
	}
	mac := hmac.New(sha1.New, []byte(authToken))
	mac.Write([]byte(b.String()))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// ValidSignature confere a assinatura do webhook, em tempo constante.
func ValidSignature(authToken, fullURL string, params url.Values, signature string) bool {
	signature = strings.TrimSpace(signature)
	if authToken == "" || signature == "" {
		return false
	}
	return hmac.Equal([]byte(Sign(authToken, fullURL, params)), []byte(signature))
}
