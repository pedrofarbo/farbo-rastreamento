// Package smsmarket manda SMS pela API REST da SMS Market
// (https://dashboard.smsmarket.com.br/api_smsmarket_documentacao.php),
// consulta a situação de entrega de cada um e lê as respostas recebidas
// (MO). Os SMS saem como "interativos" (type 2): é o que deixa o chip do
// rastreador responder. Os avisos (callbacks) não são usados: o worker da
// configuração por SMS consulta a cada 10 s.
package smsmarket

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

// DefaultBaseURL é a API REST da SMS Market.
const DefaultBaseURL = "https://api.smsmarket.com.br/webservice-rest"

// PanelURL é o painel da conta, onde se compra SMS e se libera o IP da API.
const PanelURL = "https://dashboard.smsmarket.com.br"

// Status de um SMS, no vocabulário que a configuração por SMS grava (o da
// SMS Market, numérico, é traduzido por MapStatus).
const (
	StatusQueued    = "queued"
	StatusSent      = "sent"
	StatusDelivered = "delivered"
	StatusFailed    = "failed"
	StatusCanceled  = "canceled"
	StatusReceived  = "received"
)

// typeInteractive é o SMS interativo: o destinatário pode responder.
const typeInteractive = "2"

// brt é o fuso das datas que a API recebe e devolve.
var brt = time.FixedZone("BRT", -3*3600)

const (
	brtOffset  = "-03:00"
	dateLayout = "2006-01-02 15:04:05"
)

// Failed diz se a mensagem não chegou (e não vai chegar).
func Failed(status string) bool {
	return status == StatusFailed || status == StatusCanceled || status == "undelivered"
}

// MapStatus traduz o status de envio da SMS Market: -1, 3 e 6 esperando
// (fila, preparando, pausada); 0 enviado; 1 entregue (2, lido); -6
// cancelada; os demais negativos e 7, 8 e 9, falha. Vazio para o que não
// conhece (4 é mensagem recebida, não status de envio).
func MapStatus(code string) string {
	switch strings.TrimSpace(code) {
	case "-1", "3", "6":
		return StatusQueued
	case "0":
		return StatusSent
	case "1", "2":
		return StatusDelivered
	case "-6":
		return StatusCanceled
	case "-2", "-3", "-4", "-5", "-7", "-8", "-9", "-10", "-11", "7", "8", "9":
		return StatusFailed
	}
	return ""
}

// statusMessages explica as falhas de entrega para a tela.
var statusMessages = map[string]string{
	"-2":  "erro na rede ou falha de entrega",
	"-3":  "número inválido",
	"-4":  "número fixo (não recebe SMS)",
	"-5":  "número na lista de bloqueio da SMS Market",
	"-6":  "a mensagem foi cancelada na SMS Market",
	"-7":  "número indisponível",
	"-8":  "conteúdo não permitido pela SMS Market",
	"-9":  "sem cobertura para o número",
	"-10": "pagamento restrito na SMS Market",
	"-11": "limite de envio atingido na SMS Market",
	"7":   "expirou sem ser entregue",
	"8":   "rejeitada pela operadora",
	"9":   "não chegou no aparelho",
}

// responseMessages explica as recusas da API (responseCode) para a tela.
var responseMessages = map[string]string{
	"010": "usuário ou senha da SMS Market inválidos",
	"020": "tipo de mensagem inválido",
	"030": "mensagem vazia",
	"050": "número inválido",
	"060": "envio internacional não permitido",
	"070": "mensagem recusada pela SMS Market",
	"080": "saldo de SMS insuficiente ou vencido na SMS Market",
	"090": "conta bloqueada na SMS Market",
	"091": "o IP do servidor não está liberado na SMS Market (Perfil → Webhook / API → Controle de acesso)",
	"100": "a SMS Market está em manutenção",
	"110": "a SMS Market não conseguiu processar agora",
}

// transientCodes são as recusas que passam com o tempo (manutenção, falha
// momentânea): vale tentar de novo.
var transientCodes = map[string]bool{"100": true, "110": true}

// Config é o usuário e a senha da conta na SMS Market (os mesmos do painel).
type Config struct {
	BaseURL  string
	User     string
	Password string
}

// Enabled diz se dá para mandar SMS.
func (c Config) Enabled() bool {
	return strings.TrimSpace(c.User) != "" && c.Password != ""
}

type Client struct {
	cfg  Config
	http *http.Client
	now  func() time.Time
}

func NewClient(cfg Config) *Client {
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	cfg.User = strings.TrimSpace(cfg.User)
	return &Client{cfg: cfg, http: &http.Client{Timeout: 20 * time.Second}, now: time.Now}
}

// Sender é quem envia (para a tela).
func (c *Client) Sender() string { return "SMS Market" }

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
	// ID é o do SMS recebido; SentID, o identificador (campaign_id) do
	// enviado que ele responde.
	ID     string
	SentID string
	From   string
	Body   string
}

// APIError é a recusa da SMS Market (responseCode de erro) ou uma resposta
// HTTP de erro.
type APIError struct {
	HTTPStatus int
	Code       string
	Message    string
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("SMS Market recusou (código %s): %s", e.Code, e.Message)
	}
	return fmt.Sprintf("SMS Market respondeu %d: %s", e.HTTPStatus, e.Message)
}

// IsDefinitive diz se o erro é uma recusa (a mensagem certamente não saiu:
// número inválido, sem saldo, senha errada, IP não liberado), e não uma
// falha de rede, uma manutenção ou um erro do servidor da SMS Market.
func IsDefinitive(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	if transientCodes[apiErr.Code] {
		return false
	}
	if apiErr.Code != "" {
		return true
	}
	return apiErr.HTTPStatus >= 400 && apiErr.HTTPStatus < 500 && apiErr.HTTPStatus != http.StatusTooManyRequests
}

// text aceita o campo como texto ou número.
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

// response é o envelope de toda resposta da API.
type response struct {
	Success             bool   `json:"success"`
	ResponseCode        text   `json:"responseCode"`
	ResponseDescription string `json:"responseDescription"`
	ID                  text   `json:"id"`
	SMS                 text   `json:"sms"`
	// Balance1 e Balance2 são os saldos de SMS como a conta responde de
	// verdade (3 e 4 são os de WhatsApp); a documentação fala só em sms.
	Balance1 text   `json:"balance_1"`
	Balance2 text   `json:"balance_2"`
	Messages []item `json:"messages"`
}

// item é uma mensagem nas consultas (enviada ou recebida).
type item struct {
	ID          text `json:"id"`
	Number      text `json:"number"`
	Content     text `json:"content"`
	CampaignID  text `json:"campaign_id"`
	Status      text `json:"status"`
	CarrierName text `json:"carrier_name"`
}

// refusal transforma a recusa da API em erro, com a explicação para a tela.
func refusal(httpStatus int, r *response) *APIError {
	code := string(r.ResponseCode)
	msg := responseMessages[code]
	if msg == "" {
		msg = strings.TrimSpace(r.ResponseDescription)
	}
	if msg == "" {
		msg = http.StatusText(httpStatus)
	}
	return &APIError{HTTPStatus: httpStatus, Code: code, Message: msg}
}

// Send manda o SMS interativo (o chip do rastreador responde). O número vai
// sem o DDI (country_code 55), só com os dígitos. Cada envio leva um
// identificador próprio (campaign_id), que volta nas respostas.
func (c *Client) Send(ctx context.Context, to, body string) (*Message, error) {
	number, err := nationalNumber(to)
	if err != nil {
		return nil, err
	}
	form := url.Values{
		"type": {typeInteractive}, "country_code": {"55"}, "number": {number}, "content": {body},
		"campaign_id": {campaignID()},
	}
	// Fora da tabela básica (acentos, colchetes, chaves...), vai em UTF-8; o
	// texto longo vai em partes.
	basic := isBasic(body)
	if !basic {
		form.Set("utf8", "1")
	}
	if n := utf8.RuneCountInString(body); n > 160 || (!basic && n > 70) {
		form.Set("concatenation", "1")
	}
	r, err := c.call(ctx, http.MethodPost, "send-single", form)
	if err != nil {
		return nil, err
	}
	if r.ID == "" {
		return nil, &APIError{HTTPStatus: http.StatusOK, Message: "a SMS Market não devolveu o id da mensagem"}
	}
	return &Message{ID: string(r.ID), Status: StatusQueued}, nil
}

// Fetch consulta a situação de entrega da mensagem. Sem notícia dela ainda,
// volta sem status.
func (c *Client) Fetch(ctx context.Context, id string) (*Message, error) {
	r, err := c.call(ctx, http.MethodGet, "mt_id", url.Values{"id": {id}, "timezone": {brtOffset}})
	if err != nil {
		return nil, err
	}
	msg := &Message{ID: id}
	for _, m := range r.Messages {
		if string(m.ID) != id {
			continue
		}
		code := strings.TrimSpace(string(m.Status))
		msg.Status, msg.Carrier = MapStatus(code), string(m.CarrierName)
		if Failed(msg.Status) {
			msg.ErrorCode, msg.ErrorMessage = code, statusMessages[code]
		}
	}
	return msg, nil
}

// Inbox lê as respostas recebidas de since até agora (horário de Brasília):
// todas, não só as novas — quem chama ignora as que já gravou. A janela da
// SMS Market é de 32 dias.
func (c *Client) Inbox(ctx context.Context, since time.Time) ([]Reply, error) {
	r, err := c.call(ctx, http.MethodGet, "mo", url.Values{
		"start_date": {since.In(brt).Format(dateLayout)},
		"end_date":   {c.now().In(brt).Format(dateLayout)},
		"timezone":   {brtOffset},
	})
	if err != nil {
		return nil, err
	}
	out := []Reply{}
	for _, m := range r.Messages {
		if m.ID == "" {
			continue
		}
		out = append(out, Reply{ID: string(m.ID), SentID: string(m.CampaignID), From: string(m.Number), Body: string(m.Content)})
	}
	return out, nil
}

// Balance é o saldo da conta, em SMS: o campo sms da documentação ou, como a
// conta responde de verdade, o menor dos dois saldos de SMS (balance_1 e
// balance_2; os outros são de WhatsApp) — o contador nunca promete a mais.
func (c *Client) Balance(ctx context.Context) (int, error) {
	r, err := c.call(ctx, http.MethodGet, "balance", url.Values{})
	if err != nil {
		return 0, err
	}
	fields := []text{r.SMS}
	if r.SMS == "" {
		fields = []text{r.Balance1, r.Balance2}
	}
	n, found := 0, false
	for _, f := range fields {
		var v int
		if f == "" {
			continue
		}
		if _, err := fmt.Sscan(strings.TrimSpace(string(f)), &v); err != nil {
			return 0, fmt.Errorf("saldo ilegível na SMS Market: %q", f)
		}
		if !found || v < n {
			n, found = v, true
		}
	}
	if !found {
		return 0, errors.New("a SMS Market não informou o saldo de SMS")
	}
	return n, nil
}

// call faz a chamada com o usuário e a senha no Authorization (nunca na URL)
// e devolve a resposta; a recusa (success false) vira APIError.
func (c *Client) call(ctx context.Context, method, path string, params url.Values) (*response, error) {
	endpoint := c.cfg.BaseURL + "/" + path
	var body io.Reader
	if method == http.MethodGet {
		if len(params) > 0 {
			endpoint += "?" + params.Encode()
		}
	} else {
		body = strings.NewReader(params.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.cfg.User, c.cfg.Password)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("SMS Market indisponível: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("lendo resposta da SMS Market: %w", err)
	}
	var r response
	if jsonErr := json.Unmarshal(bytes.TrimSpace(raw), &r); jsonErr != nil {
		if resp.StatusCode >= 300 {
			msg := strings.TrimSpace(string(raw))
			if msg == "" || len(msg) > 200 {
				msg = http.StatusText(resp.StatusCode)
			}
			return nil, &APIError{HTTPStatus: resp.StatusCode, Message: msg}
		}
		return nil, fmt.Errorf("resposta da SMS Market ilegível: %w", jsonErr)
	}
	if !r.Success || resp.StatusCode >= 300 {
		return nil, refusal(resp.StatusCode, &r)
	}
	return &r, nil
}

// nationalNumber deixa o número como a SMS Market quer: só os dígitos, sem o
// 55 (DDD + número, 10 ou 11 dígitos).
func nationalNumber(phone string) (string, error) {
	d := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, phone)
	if strings.HasPrefix(d, "55") && (len(d) == 12 || len(d) == 13) {
		d = d[2:]
	}
	if len(d) != 10 && len(d) != 11 {
		return "", &APIError{HTTPStatus: http.StatusOK, Code: "050", Message: "número inválido: use DDD e número de um chip do Brasil"}
	}
	return d, nil
}

// isBasic diz se o texto só tem caracteres da tabela básica do SMS (os que
// não precisam de UTF-8).
func isBasic(s string) bool {
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case strings.ContainsRune(" \n\r!\"#$%&'()*+,-./:;<=>?@_`“”‘’", r):
		default:
			return false
		}
	}
	return true
}

// campaignID é o identificador do envio no nosso lado (volta nas respostas).
func campaignID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "farbo-" + hex.EncodeToString(b)
}
