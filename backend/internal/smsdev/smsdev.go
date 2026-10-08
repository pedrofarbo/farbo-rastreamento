// Package smsdev manda SMS pela API do SMSDev (https://www.smsdev.com.br),
// consulta a situação de entrega de cada um (DLR) e lê as respostas
// recebidas (MO). Os avisos (callbacks) do SMSDev não são usados: não vêm
// assinados, e o worker da configuração por SMS consulta a cada 10 s.
package smsdev

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
)

// DefaultBaseURL é a API do SMSDev.
const DefaultBaseURL = "https://api.smsdev.com.br/v1"

// Status de um SMS, no vocabulário que a configuração por SMS grava (o do
// SMSDev é traduzido por MapStatus).
const (
	StatusQueued    = "queued"
	StatusSent      = "sent"
	StatusDelivered = "delivered"
	StatusFailed    = "failed"
	StatusCanceled  = "canceled"
	StatusReceived  = "received"
)

// Failed diz se a mensagem não chegou (e não vai chegar).
func Failed(status string) bool {
	return status == StatusFailed || status == StatusCanceled || status == "undelivered"
}

// MapStatus traduz a situação do SMSDev: RECEBIDA (entregue no aparelho),
// ENVIADA (à operadora), FILA/APROVACAO (esperando), ERRO, CANCELADA e BLACK
// LIST (o número bloqueado). Vazio para o que não conhece.
func MapStatus(situacao string) string {
	s := strings.ToUpper(strings.TrimSpace(situacao))
	switch {
	case s == "RECEBIDA":
		return StatusDelivered
	case s == "ENVIADA":
		return StatusSent
	case s == "FILA" || s == "APROVACAO" || s == "APROVAÇÃO" || strings.Contains(s, "FILA"):
		return StatusQueued
	case s == "ERRO" || strings.ReplaceAll(s, " ", "") == "BLACKLIST":
		return StatusFailed
	case s == "CANCELADA":
		return StatusCanceled
	}
	return ""
}

// statusMessage explica as situações de falha para a tela.
func statusMessage(situacao string) string {
	s := strings.ToUpper(strings.TrimSpace(situacao))
	switch {
	case strings.ReplaceAll(s, " ", "") == "BLACKLIST":
		return "o número está na lista de bloqueio do SMSDev"
	case s == "ERRO":
		return "o SMSDev recusou a mensagem"
	case s == "CANCELADA":
		return "a mensagem foi cancelada no SMSDev"
	}
	return ""
}

// Config é a chave da conta (no painel do SMSDev, em Configurações → Conta).
type Config struct {
	BaseURL string
	APIKey  string
}

// Enabled diz se dá para mandar SMS.
func (c Config) Enabled() bool { return strings.TrimSpace(c.APIKey) != "" }

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

// Sender é quem envia (para a tela).
func (c *Client) Sender() string { return "SMSDev" }

// Message é um SMS enviado.
type Message struct {
	ID           string
	Status       string
	ErrorCode    string
	ErrorMessage string
	// Carrier é a operadora do número (da consulta de entrega).
	Carrier string
}

// Reply é um SMS recebido (a resposta ao que foi enviado).
type Reply struct {
	// ID é o do SMS recebido; SentID, o do enviado que ele responde.
	ID     string
	SentID string
	From   string
	Body   string
}

// APIError é a recusa do SMSDev (situacao ERRO) ou uma resposta HTTP de erro.
type APIError struct {
	HTTPStatus int
	Code       string
	Message    string
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("SMSDev recusou (código %s): %s", e.Code, e.Message)
	}
	return fmt.Sprintf("SMSDev respondeu %d: %s", e.HTTPStatus, e.Message)
}

// IsDefinitive diz se o erro é uma recusa (a mensagem certamente não saiu:
// número inválido, sem saldo, chave errada), e não uma falha de rede ou do
// servidor do SMSDev.
func IsDefinitive(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	if apiErr.HTTPStatus == http.StatusOK {
		return true
	}
	return apiErr.HTTPStatus >= 400 && apiErr.HTTPStatus < 500 && apiErr.HTTPStatus != http.StatusTooManyRequests
}

// text aceita o campo como texto ou número (o SMSDev manda os dois).
type text string

func (t *text) UnmarshalJSON(raw []byte) error {
	raw = bytes.TrimSpace(raw)
	if bytes.Equal(raw, []byte("null")) {
		*t = ""
		return nil
	}
	if len(raw) > 0 && raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return err
		}
		*t = text(s)
		return nil
	}
	*t = text(raw)
	return nil
}

// result é uma linha de resposta do SMSDev (envio, consulta ou resposta recebida).
type result struct {
	Situacao  text `json:"situacao"`
	Codigo    text `json:"codigo"`
	ID        text `json:"id"`
	Descricao text `json:"descricao"`
	Operadora text `json:"operadora"`
	Telefone  text `json:"telefone"`
	IDSMSRead text `json:"id_sms_read"`
	SaldoSMS  text `json:"saldo_sms"`
}

func (r result) failed() bool { return strings.EqualFold(string(r.Situacao), "ERRO") }

// Send manda o SMS (type 9: SMS comum). O número vai só com os dígitos,
// com o 55 (ex.: 5511988887777).
func (c *Client) Send(ctx context.Context, to, body string) (*Message, error) {
	rows, err := c.call(ctx, "send", url.Values{"type": {"9"}, "number": {digits(to)}, "msg": {body}})
	if err != nil {
		return nil, err
	}
	r := rows[0]
	if r.failed() {
		return nil, &APIError{HTTPStatus: http.StatusOK, Code: string(r.Codigo), Message: string(r.Descricao)}
	}
	if r.ID == "" {
		return nil, &APIError{HTTPStatus: http.StatusOK, Message: "o SMSDev não devolveu o id da mensagem"}
	}
	return &Message{ID: string(r.ID), Status: StatusQueued}, nil
}

// Fetch consulta a situação de entrega (DLR) da mensagem.
func (c *Client) Fetch(ctx context.Context, id string) (*Message, error) {
	rows, err := c.call(ctx, "dlr", url.Values{"id": {id}})
	if err != nil {
		return nil, err
	}
	r := rows[0]
	if r.failed() {
		return nil, &APIError{HTTPStatus: http.StatusOK, Code: string(r.Codigo), Message: string(r.Descricao)}
	}
	status := MapStatus(string(r.Descricao))
	msg := &Message{ID: id, Status: status, Carrier: string(r.Operadora)}
	if Failed(status) {
		msg.ErrorMessage = statusMessage(string(r.Descricao))
	}
	return msg, nil
}

// Inbox lê as respostas recebidas de since até hoje (horário de Brasília):
// todas, não só as novas — quem chama ignora as que já gravou.
func (c *Client) Inbox(ctx context.Context, since time.Time) ([]Reply, error) {
	brt := time.FixedZone("BRT", -3*3600)
	rows, err := c.call(ctx, "inbox", url.Values{
		"status":    {"1"},
		"date_from": {since.In(brt).Format("02/01/2006")},
		"date_to":   {time.Now().In(brt).Format("02/01/2006")},
	})
	if err != nil {
		return nil, err
	}
	out := []Reply{}
	for _, r := range rows {
		if r.failed() {
			return nil, &APIError{HTTPStatus: http.StatusOK, Code: string(r.Codigo), Message: string(r.Descricao)}
		}
		if r.IDSMSRead == "" {
			continue // "nenhuma mensagem"
		}
		out = append(out, Reply{ID: string(r.IDSMSRead), SentID: string(r.ID), From: string(r.Telefone), Body: string(r.Descricao)})
	}
	return out, nil
}

// Balance é o saldo da conta, em SMS.
func (c *Client) Balance(ctx context.Context) (int, error) {
	rows, err := c.call(ctx, "balance", url.Values{})
	if err != nil {
		return 0, err
	}
	r := rows[0]
	if r.failed() {
		return 0, &APIError{HTTPStatus: http.StatusOK, Code: string(r.Codigo), Message: string(r.Descricao)}
	}
	var n int
	if _, err := fmt.Sscan(strings.TrimSpace(string(r.SaldoSMS)), &n); err != nil {
		return 0, fmt.Errorf("saldo ilegível no SMSDev: %q", r.SaldoSMS)
	}
	return n, nil
}

// call faz o POST (a chave vai no corpo, não na URL) e devolve as linhas da
// resposta, que vem como objeto ou como lista.
func (c *Client) call(ctx context.Context, method string, form url.Values) ([]result, error) {
	form.Set("key", c.cfg.APIKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+"/"+method, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("SMSDev indisponível: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("lendo resposta do SMSDev: %w", err)
	}
	if resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(raw))
		if msg == "" || len(msg) > 200 {
			msg = http.StatusText(resp.StatusCode)
		}
		return nil, &APIError{HTTPStatus: resp.StatusCode, Message: msg}
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		// A consulta de um id que o SMSDev ainda não conhece volta vazia:
		// sem situação, por enquanto.
		return []result{{Situacao: "OK"}}, nil
	}
	var rows []result
	if raw[0] == '[' {
		err = json.Unmarshal(raw, &rows)
	} else {
		var one result
		err = json.Unmarshal(raw, &one)
		rows = []result{one}
	}
	if err != nil {
		return nil, fmt.Errorf("resposta do SMSDev ilegível: %w", err)
	}
	if len(rows) == 0 {
		rows = []result{{Situacao: "OK"}}
	}
	return rows, nil
}

// digits deixa só os números (o SMSDev não quer o +).
func digits(phone string) string {
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, phone)
}
