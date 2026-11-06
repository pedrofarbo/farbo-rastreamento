package fiscal

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultBaseURL é a API da Notaas.
const DefaultBaseURL = "https://platform.notaas.com.br/api/v1"

// As situações da NFS-e na Notaas.
const (
	notaasQueued     = "queued"
	notaasProcessing = "processing"
	notaasIssued     = "issued"
	notaasError      = "error"
	notaasCancelled  = "cancelled"
)

// Client fala com a API da Notaas (a chave do projeto, no x-api-key).
type Client struct {
	baseURL string
	key     string
	http    *http.Client
}

func NewClient(baseURL, key string) *Client {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		key:     key,
		// O PDF vem num redirecionamento para o CDN: quem segue é o navegador.
		http: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}},
	}
}

// APIError é a recusa da Notaas: o status HTTP, o código (CREDIT_LIMIT,
// CERT_MISSING...) e a mensagem.
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("notaas %d %s: %s", e.Status, e.Code, e.Message)
	}
	return fmt.Sprintf("notaas %d: %s", e.Status, e.Message)
}

// Temporary diz se vale tentar de novo depois (limite de requisições ou
// falha do lado deles).
func (e *APIError) Temporary() bool { return e.Status == http.StatusTooManyRequests || e.Status >= 500 }

// CreditLimit: as notas do plano no mês acabaram.
func (e *APIError) CreditLimit() bool { return e.Code == "CREDIT_LIMIT" }

// Address é o endereço do tomador (a cidade e a UF viram o código do IBGE lá).
type Address struct {
	Street     string `json:"logradouro,omitempty"`
	Number     string `json:"numero,omitempty"`
	Complement string `json:"complemento,omitempty"`
	District   string `json:"bairro,omitempty"`
	City       string `json:"cidade,omitempty"`
	State      string `json:"uf,omitempty"`
	ZipCode    string `json:"cep,omitempty"`
}

// Taker é o tomador do serviço: CPF ou CNPJ (só dígitos), o nome e o e-mail.
type Taker struct {
	CPF     string   `json:"cpf,omitempty"`
	CNPJ    string   `json:"cnpj,omitempty"`
	Name    string   `json:"nome"`
	Email   string   `json:"email,omitempty"`
	Phone   string   `json:"telefone,omitempty"`
	Address *Address `json:"endereco,omitempty"`
}

// EmitRequest é a NFS-e a emitir. Total em reais; ISSRate em % (2 = 2%);
// Competence AAAA-MM; Reference, o id da fatura (volta na consulta).
type EmitRequest struct {
	Reference string
	Taker     Taker
	Code      string
	Text      string
	Total     float64
	ISSRate   float64
	Month     string
}

type emitBody struct {
	Reference string `json:"referencia,omitempty"`
	Taker     Taker  `json:"tomador"`
	Service   struct {
		Code string `json:"codigo,omitempty"`
		Text string `json:"descricao"`
	} `json:"servico"`
	Values struct {
		Total   float64 `json:"total"`
		ISSRate float64 `json:"aliquotaIss"`
	} `json:"valores"`
	Month string `json:"competencia,omitempty"`
}

// Status é a NFS-e na Notaas.
type Status struct {
	ID               string     `json:"invoiceId"`
	Reference        string     `json:"referencia"`
	Status           string     `json:"status"`
	VerificationCode string     `json:"chNFSe"`
	Number           string     `json:"numeroNfe"`
	EmittedAt        *time.Time `json:"emittedAt"`
	PDFURL           string     `json:"pdfUrl"`
	XMLURL           string     `json:"xmlUrl"`
	ErrorCode        string     `json:"errorCode"`
	ErrorMessage     string     `json:"errorMessage"`
	Errors           []struct {
		Code        string `json:"Codigo"`
		Description string `json:"Descricao"`
		Complement  string `json:"Complemento"`
	} `json:"errors"`
}

// Problem é o motivo da rejeição, como a prefeitura (ou a Notaas) disse.
func (s *Status) Problem() string {
	parts := []string{}
	if s.ErrorMessage != "" {
		parts = append(parts, s.ErrorMessage)
	}
	for _, e := range s.Errors {
		text := strings.TrimSpace(strings.Join([]string{e.Code, e.Description, e.Complement}, " "))
		if text != "" {
			parts = append(parts, text)
		}
	}
	if len(parts) == 0 && s.ErrorCode != "" {
		parts = append(parts, s.ErrorCode)
	}
	return strings.Join(parts, "; ")
}

// Emit manda a NFS-e para a fila da Notaas e devolve o id dela lá.
func (c *Client) Emit(ctx context.Context, req EmitRequest) (string, error) {
	var body emitBody
	body.Reference, body.Taker, body.Month = req.Reference, req.Taker, req.Month
	body.Service.Code, body.Service.Text = req.Code, req.Text
	body.Values.Total, body.Values.ISSRate = req.Total, req.ISSRate
	var out struct {
		ID string `json:"invoiceId"`
	}
	if err := c.do(ctx, http.MethodPost, "/emitir", body, &out); err != nil {
		return "", err
	}
	if out.ID == "" {
		return "", errors.New("notaas: emissão sem invoiceId")
	}
	return out.ID, nil
}

// Status consulta a NFS-e.
func (c *Client) Status(ctx context.Context, id string) (*Status, error) {
	var out Status
	if err := c.do(ctx, http.MethodGet, "/invoices/"+url.PathEscape(id)+"/status", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Cancel pede o cancelamento (assíncrono: a consulta diz quando saiu).
// Motivo 2: o serviço não foi prestado (a fatura foi estornada).
func (c *Client) Cancel(ctx context.Context, id, reason string) error {
	body := map[string]any{"invoiceId": id, "codigoMotivo": 2, "motivo": reason}
	return c.do(ctx, http.MethodPost, "/cancelar", body, nil)
}

// PDF é o DANFSE da NFS-e.
type PDF struct {
	Body []byte
}

// PDF busca o DANFSE da NFS-e. Guardado no CDN da Notaas, ela redireciona:
// o arquivo vem de lá (sem a chave, que é só da API).
func (c *Client) PDF(ctx context.Context, id string) (*PDF, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/invoices/"+url.PathEscape(id)+"/pdf", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", c.key)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		if loc, err := resp.Location(); err == nil {
			return c.fetchPDF(ctx, loc.String())
		}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, apiError(resp)
	}
	return readPDF(resp.Body)
}

// fetchPDF baixa o DANFSE do endereço público.
func (c *Client) fetchPDF(ctx context.Context, location string) (*PDF, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, location, nil)
	if err != nil {
		return nil, err
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &APIError{Status: resp.StatusCode, Message: "PDF da nota indisponível no CDN"}
	}
	return readPDF(resp.Body)
}

func readPDF(body io.Reader) (*PDF, error) {
	data, err := io.ReadAll(io.LimitReader(body, 5<<20))
	if err != nil {
		return nil, err
	}
	return &PDF{Body: data}, nil
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("x-api-key", c.key)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return apiError(resp)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out)
}

// apiError lê o corpo de erro da Notaas ({error, message, errorCode}).
func apiError(resp *http.Response) error {
	var body struct {
		Error     string `json:"error"`
		Message   string `json:"message"`
		ErrorCode string `json:"errorCode"`
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	_ = json.Unmarshal(raw, &body)
	message := strings.TrimSpace(strings.Join([]string{body.Error, body.Message}, " "))
	if message == "" {
		message = strings.TrimSpace(string(raw))
	}
	if len(message) > 300 {
		message = message[:300]
	}
	return &APIError{Status: resp.StatusCode, Code: body.ErrorCode, Message: message}
}

// VerifySignature confere o X-Notaas-Signature (sha256=HMAC-SHA256 do corpo,
// em hex).
func VerifySignature(secret string, body []byte, header string) bool {
	if secret == "" || header == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(strings.TrimSpace(header)))
}

// Webhook é o aviso da Notaas (nfse.issued, nfse.error, nfse.cancelled...).
type Webhook struct {
	Event string `json:"event"`
	Data  struct {
		InvoiceID string `json:"invoiceId"`
	} `json:"data"`
}

// ParseWebhook lê o aviso.
func ParseWebhook(body []byte) (*Webhook, error) {
	var w Webhook
	if err := json.Unmarshal(body, &w); err != nil {
		return nil, err
	}
	if w.Event == "" {
		return nil, errors.New("aviso sem evento")
	}
	return &w, nil
}
