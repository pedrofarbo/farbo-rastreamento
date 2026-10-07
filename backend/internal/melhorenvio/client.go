// Package melhorenvio integra a API do Melhor Envios: autorização OAuth do
// aplicativo, cotação de frete, compra e impressão de etiqueta e rastreio.
package melhorenvio

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	SandboxURL    = "https://sandbox.melhorenvio.com.br"
	ProductionURL = "https://melhorenvio.com.br"
)

// Scopes são só as permissões que a integração usa. transactions-read é a
// da carteira (o saldo que paga as etiquetas): a documentação não diz qual
// permissão a consulta do saldo exige, e esta é a que trata da carteira.
var Scopes = []string{
	"cart-read", "cart-write", "orders-read", "shipping-calculate", "shipping-checkout",
	"shipping-generate", "shipping-print", "shipping-tracking", "transactions-read", "users-read",
}

// BaseURLFor escolhe o ambiente; override (testes) tem precedência.
func BaseURLFor(sandbox bool, override string) string {
	switch {
	case override != "":
		return strings.TrimRight(override, "/")
	case sandbox:
		return SandboxURL
	default:
		return ProductionURL
	}
}

// ErrNotConnected: falta autorizar o aplicativo (Conectar no painel).
var ErrNotConnected = errors.New("Melhor Envios não conectado: autorize o aplicativo em Pedidos > Conectar Melhor Envios")

// APIError é uma recusa da API, com a mensagem dela.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string { return "Melhor Envios: " + e.Message }

// Token é o acesso OAuth (30 dias) e o refresh (45 dias).
type Token struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
}

// TokenStore guarda o token entre reinícios.
type TokenStore interface {
	Load(ctx context.Context) (*Token, error) // nil, nil se não houver
	Save(ctx context.Context, t *Token) error
	Delete(ctx context.Context) error
}

type Config struct {
	BaseURL      string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	// ContactEmail vai no User-Agent, exigido pela API.
	ContactEmail string
	// StaticToken é um token pessoal; dispensa o OAuth.
	StaticToken string
	HTTP        *http.Client
}

type Client struct {
	cfg   Config
	store TokenStore
	http  *http.Client
	now   func() time.Time
	mu    sync.Mutex // serializa a renovação do token
}

func New(cfg Config, store TokenStore) *Client {
	httpClient := cfg.HTTP
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	return &Client{cfg: cfg, store: store, http: httpClient, now: time.Now}
}

// CanAuthorize diz se há aplicativo (Client ID + Secret) para o OAuth.
func (c *Client) CanAuthorize() bool { return c.cfg.ClientID != "" && c.cfg.ClientSecret != "" }

// UsesStaticToken: com token pessoal, não há Conectar/Desconectar.
func (c *Client) UsesStaticToken() bool { return c.cfg.StaticToken != "" }

func (c *Client) userAgent() string {
	return fmt.Sprintf("Farbo Rastreadores (%s)", c.cfg.ContactEmail)
}

// AuthorizeURL é para onde o navegador vai autorizar o aplicativo.
func (c *Client) AuthorizeURL(state string) string {
	q := url.Values{}
	q.Set("client_id", c.cfg.ClientID)
	q.Set("redirect_uri", c.cfg.RedirectURL)
	q.Set("response_type", "code")
	q.Set("state", state)
	q.Set("scope", strings.Join(Scopes, " "))
	// A API espera os escopos separados por espaço (%20), não por "+".
	return c.cfg.BaseURL + "/oauth/authorize?" + strings.ReplaceAll(q.Encode(), "+", "%20")
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
}

func (c *Client) requestToken(ctx context.Context, body map[string]string) (*Token, error) {
	body["client_id"] = c.cfg.ClientID
	body["client_secret"] = c.cfg.ClientSecret
	var resp tokenResponse
	if err := c.send(ctx, http.MethodPost, "/oauth/token", "", body, &resp); err != nil {
		return nil, err
	}
	if resp.AccessToken == "" {
		return nil, &APIError{Status: http.StatusBadGateway, Message: "resposta de token sem access_token"}
	}
	expires := time.Duration(resp.ExpiresIn) * time.Second
	if expires <= 0 {
		expires = 30 * 24 * time.Hour
	}
	return &Token{AccessToken: resp.AccessToken, RefreshToken: resp.RefreshToken, ExpiresAt: c.now().Add(expires)}, nil
}

// ExchangeCode troca o code do callback pelo token e o guarda.
func (c *Client) ExchangeCode(ctx context.Context, code string) error {
	token, err := c.requestToken(ctx, map[string]string{
		"grant_type": "authorization_code", "redirect_uri": c.cfg.RedirectURL, "code": code,
	})
	if err != nil {
		return err
	}
	return c.store.Save(ctx, token)
}

// Status diz se há acesso e até quando vale o token atual.
func (c *Client) Status(ctx context.Context) (connected bool, expiresAt *time.Time, err error) {
	if c.UsesStaticToken() {
		return true, nil, nil
	}
	token, err := c.store.Load(ctx)
	if err != nil || token == nil {
		return false, nil, err
	}
	return true, &token.ExpiresAt, nil
}

// Disconnect esquece o token guardado.
func (c *Client) Disconnect(ctx context.Context) error { return c.store.Delete(ctx) }

// accessToken devolve um token válido, renovando quando falta menos de um
// dia (ou quando force, depois de um 401).
func (c *Client) accessToken(ctx context.Context, force bool) (string, error) {
	if c.UsesStaticToken() {
		return c.cfg.StaticToken, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	token, err := c.store.Load(ctx)
	if err != nil {
		return "", err
	}
	if token == nil {
		return "", ErrNotConnected
	}
	if !force && c.now().Before(token.ExpiresAt.Add(-24*time.Hour)) {
		return token.AccessToken, nil
	}
	if token.RefreshToken == "" || !c.CanAuthorize() {
		return "", ErrNotConnected
	}
	renewed, err := c.requestToken(ctx, map[string]string{
		"grant_type": "refresh_token", "refresh_token": token.RefreshToken,
	})
	if err != nil {
		var apiErr *APIError
		// Refresh recusado: a autorização foi revogada ou venceu (45 dias).
		if errors.As(err, &apiErr) && apiErr.Status >= 400 && apiErr.Status < 500 {
			return "", ErrNotConnected
		}
		return "", err
	}
	if renewed.RefreshToken == "" {
		renewed.RefreshToken = token.RefreshToken
	}
	if err := c.store.Save(ctx, renewed); err != nil {
		return "", err
	}
	return renewed.AccessToken, nil
}

// call faz uma chamada autenticada; num 401 renova o token e tenta de novo.
func (c *Client) call(ctx context.Context, method, path string, body, out any) error {
	token, err := c.accessToken(ctx, false)
	if err != nil {
		return err
	}
	err = c.send(ctx, method, path, token, body, out)
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusUnauthorized && !c.UsesStaticToken() {
		if token, err = c.accessToken(ctx, true); err != nil {
			return err
		}
		return c.send(ctx, method, path, token, body, out)
	}
	return err
}

func (c *Client) send(ctx context.Context, method, path, token string, body, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.cfg.BaseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", c.userAgent())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("Melhor Envios indisponível: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		return &APIError{Status: resp.StatusCode, Message: errorMessage(resp.StatusCode, raw)}
	}
	if out == nil || len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		// O começo da resposta ajuda a entender o formato que veio.
		snippet := string(raw)
		if len(snippet) > 300 {
			snippet = snippet[:300] + "…"
		}
		return fmt.Errorf("resposta inesperada do Melhor Envios (%s): %s", path, snippet)
	}
	return nil
}

// errorMessage junta a mensagem e o primeiro erro de campo, que é o que
// explica de fato a recusa ("to.document: CPF inválido").
func errorMessage(status int, raw []byte) string {
	var body struct {
		Message          string              `json:"message"`
		Error            string              `json:"error"`
		ErrorDescription string              `json:"error_description"`
		Errors           map[string][]string `json:"errors"`
	}
	if json.Unmarshal(raw, &body) != nil {
		return fmt.Sprintf("erro %d", status)
	}
	msg := body.Message
	if msg == "" {
		msg = body.ErrorDescription
	}
	if msg == "" {
		msg = body.Error
	}
	if msg == "" {
		msg = fmt.Sprintf("erro %d", status)
	}
	fields := make([]string, 0, len(body.Errors))
	for field := range body.Errors {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	for _, field := range fields {
		if len(body.Errors[field]) > 0 {
			return msg + " (" + field + ": " + body.Errors[field][0] + ")"
		}
	}
	return msg
}

// Money aceita os valores da API, que vêm como número ou como texto ("37.79").
type Money float64

func (m *Money) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*m = 0
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return err
	}
	*m = Money(v)
	return nil
}

func (m Money) Cents() int { return int(math.Round(float64(m) * 100)) }

// ---------------------------------------------------------------------------
// Conta, cotação, etiqueta e rastreio
// ---------------------------------------------------------------------------

type Account struct {
	FirstName string `json:"firstname"`
	LastName  string `json:"lastname"`
	Email     string `json:"email"`
}

func (c *Client) Account(ctx context.Context) (*Account, error) {
	var out Account
	if err := c.call(ctx, http.MethodGet, "/api/v2/me", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// PanelURL é o painel do Melhor Envios (a carteira fica lá).
func (c *Client) PanelURL() string { return c.cfg.BaseURL + "/painel" }

// Wallet é a carteira do Melhor Envios: as etiquetas são pagas com o saldo.
type Wallet struct {
	Balance  Money `json:"balance"`
	Reserved Money `json:"reserved"`
	Debts    Money `json:"debts"`
}

// Balance consulta o saldo da carteira.
func (c *Client) Balance(ctx context.Context) (*Wallet, error) {
	var out Wallet
	if err := c.call(ctx, http.MethodGet, "/api/v2/me/balance", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// TopUpGateway é o meio de pagamento da inserção de saldo (Pix e boleto).
const TopUpGateway = "yapay-transparente"

// Formas de pagar a inserção de saldo.
const (
	TopUpPix    = "pix"
	TopUpBoleto = "boleto"
)

// TopUpRequest é um pedido de saldo. O boleto sai em nome da empresa quando
// vem com a razão social e o CNPJ; sem eles, nos dados da pessoa da conta.
type TopUpRequest struct {
	Method      string
	ValueCents  int
	RedirectURL string
	CompanyName string
	CNPJ        string
}

// TopUp é a cobrança gerada: o link do Pix (QR Code) ou do boleto (PDF).
type TopUp struct {
	ID         string `json:"id"`
	Protocol   string `json:"protocol"`
	Status     string `json:"status"`
	Method     string `json:"method"`
	ValueCents int    `json:"valueCents"`
	Link       string `json:"link"`
	// Digitable é a linha digitável do boleto.
	Digitable string `json:"digitable"`
	// PixCode é o Pix copia-e-cola, quando a resposta traz (o formato dela
	// não é documentado: é procurado em qualquer campo).
	PixCode string `json:"pixCode"`
	// Shape são os campos que vieram, sem os valores: sem o copia-e-cola,
	// vai para o log para entender o formato.
	Shape string `json:"-"`
}

// AddBalance gera a cobrança para inserir saldo na carteira. O saldo só cai
// depois que ela é paga, no Melhor Envios.
func (c *Client) AddBalance(ctx context.Context, r TopUpRequest) (*TopUp, error) {
	body := map[string]string{
		"gateway": TopUpGateway, "slug": r.Method,
		"value": fmt.Sprintf("%d.%02d", r.ValueCents/100, r.ValueCents%100),
	}
	if r.RedirectURL != "" {
		body["redirect_url"] = r.RedirectURL
	}
	if r.Method == TopUpBoleto && r.CompanyName != "" && r.CNPJ != "" {
		body["company_name"], body["cnpj"] = r.CompanyName, digits(r.CNPJ)
	}
	var out struct {
		Payment *struct {
			ID       string          `json:"id"`
			Protocol string          `json:"protocol"`
			Status   string          `json:"status"`
			Link     json.RawMessage `json:"link"`
		} `json:"payment"`
		Redirect  json.RawMessage `json:"redirect"`
		Digitable json.RawMessage `json:"digitable"`
		Message   string          `json:"message"`
	}
	var raw json.RawMessage
	if err := c.call(ctx, http.MethodPost, "/api/v2/me/balance", body, &raw); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, &APIError{Status: http.StatusBadGateway, Message: "resposta inesperada ao gerar o saldo"}
	}
	if out.Payment == nil {
		msg := out.Message
		if msg == "" {
			msg = "cobrança do saldo não gerada"
		}
		return nil, &APIError{Status: http.StatusBadGateway, Message: msg}
	}
	top := &TopUp{
		ID: out.Payment.ID, Protocol: out.Payment.Protocol, Status: out.Payment.Status,
		Method: r.Method, ValueCents: r.ValueCents, Digitable: jsonText(out.Digitable),
	}
	var tree any
	if json.Unmarshal(raw, &tree) == nil {
		if r.Method == TopUpPix {
			top.PixCode = findPixCode(tree)
		}
		top.Shape = shapeOf(tree)
	}
	// O link do pagamento vem no pagamento ou no redirect da resposta (que
	// não é o endereço de volta que mandamos).
	for _, link := range []string{jsonText(out.Payment.Link), jsonText(out.Redirect)} {
		if webLink(link) && link != r.RedirectURL {
			top.Link = link
			break
		}
	}
	return top, nil
}

// findPixCode procura o Pix copia-e-cola (BR Code) em qualquer campo.
func findPixCode(v any) string {
	switch t := v.(type) {
	case string:
		code := strings.TrimSpace(t)
		if strings.HasPrefix(code, "000201") && strings.Contains(strings.ToLower(code), "br.gov.bcb.pix") {
			return code
		}
		// A resposta do meio de pagamento pode vir como JSON dentro de texto.
		if strings.HasPrefix(code, "{") || strings.HasPrefix(code, "[") {
			var inner any
			if json.Unmarshal([]byte(code), &inner) == nil {
				return findPixCode(inner)
			}
		}
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if code := findPixCode(t[k]); code != "" {
				return code
			}
		}
	case []any:
		for _, item := range t {
			if code := findPixCode(item); code != "" {
				return code
			}
		}
	}
	return ""
}

// shapeOf descreve os campos de um JSON sem os valores:
// "payment{id,link,response{qrcode}},redirect".
func shapeOf(v any) string {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, k+shapeOf(t[k]))
		}
		return "{" + strings.Join(parts, ",") + "}"
	case []any:
		if len(t) == 0 {
			return "[]"
		}
		return "[" + shapeOf(t[0]) + "]"
	default:
		return ""
	}
}

// jsonText lê um campo que pode vir como texto ou nulo.
func jsonText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return strings.TrimSpace(s)
}

func webLink(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != ""
}

// Package é um volume (cm e kg) com o valor declarado.
type Package struct {
	HeightCm       int
	WidthCm        int
	LengthCm       int
	WeightKg       float64
	InsuranceCents int
}

// Quote é um serviço de frete cotado.
type Quote struct {
	ServiceID    int    `json:"serviceId"`
	Service      string `json:"service"`
	Company      string `json:"company"`
	PriceCents   int    `json:"priceCents"`
	DeliveryDays int    `json:"deliveryDays"`
	// Error vem preenchido quando o serviço não atende o trecho.
	Error string `json:"error"`
}

// Name é "Correios PAC", para mostrar e gravar.
func (q Quote) Name() string { return strings.TrimSpace(q.Company + " " + q.Service) }

type quoteResponse struct {
	ID                 int    `json:"id"`
	Name               string `json:"name"`
	Price              Money  `json:"price"`
	CustomPrice        Money  `json:"custom_price"`
	DeliveryTime       int    `json:"delivery_time"`
	CustomDeliveryTime int    `json:"custom_delivery_time"`
	Error              string `json:"error"`
	Company            struct {
		Name string `json:"name"`
	} `json:"company"`
}

// Calculate cota o frete de um pacote entre dois CEPs.
func (c *Client) Calculate(ctx context.Context, fromCEP, toCEP string, pkg Package, services string) ([]Quote, error) {
	body := map[string]any{
		"from": map[string]string{"postal_code": digits(fromCEP)},
		"to":   map[string]string{"postal_code": digits(toCEP)},
		"products": []map[string]any{{
			"id": "rastreador", "width": pkg.WidthCm, "height": pkg.HeightCm, "length": pkg.LengthCm,
			"weight": pkg.WeightKg, "insurance_value": float64(pkg.InsuranceCents) / 100, "quantity": 1,
		}},
		"options": map[string]bool{"receipt": false, "own_hand": false},
	}
	if services != "" {
		body["services"] = services
	}
	var raw []quoteResponse
	if err := c.call(ctx, http.MethodPost, "/api/v2/me/shipment/calculate", body, &raw); err != nil {
		return nil, err
	}
	out := make([]Quote, 0, len(raw))
	for _, q := range raw {
		price := q.CustomPrice
		if price == 0 {
			price = q.Price
		}
		days := q.CustomDeliveryTime
		if days == 0 {
			days = q.DeliveryTime
		}
		out = append(out, Quote{
			ServiceID: q.ID, Service: q.Name, Company: q.Company.Name,
			PriceCents: price.Cents(), DeliveryDays: days, Error: q.Error,
		})
	}
	// Os que atendem primeiro, do mais barato ao mais caro.
	sort.SliceStable(out, func(i, j int) bool {
		if (out[i].Error == "") != (out[j].Error == "") {
			return out[i].Error == ""
		}
		return out[i].PriceCents < out[j].PriceCents
	})
	return out, nil
}

// Party é remetente ou destinatário de uma etiqueta.
type Party struct {
	Name            string `json:"name"`
	Phone           string `json:"phone,omitempty"`
	Email           string `json:"email,omitempty"`
	Document        string `json:"document,omitempty"`
	CompanyDocument string `json:"company_document,omitempty"`
	StateRegister   string `json:"state_register,omitempty"`
	Address         string `json:"address"`
	Complement      string `json:"complement,omitempty"`
	Number          string `json:"number"`
	District        string `json:"district"`
	City            string `json:"city"`
	CountryID       string `json:"country_id"`
	PostalCode      string `json:"postal_code"`
	StateAbbr       string `json:"state_abbr"`
}

// CartRequest é a etiqueta a pôr no carrinho.
type CartRequest struct {
	ServiceID     int
	From          Party
	To            Party
	ProductName   string
	Package       Package
	NonCommercial bool
	// Tag identifica o pedido do nosso lado no painel do Melhor Envios.
	Tag string
}

// CartOrder é a etiqueta criada no carrinho.
type CartOrder struct {
	ID       string `json:"id"`
	Protocol string `json:"protocol"`
	Price    Money  `json:"price"`
	Status   string `json:"status"`
}

func (c *Client) AddToCart(ctx context.Context, r CartRequest) (*CartOrder, error) {
	r.From.CountryID, r.To.CountryID = "BR", "BR"
	r.From.PostalCode, r.To.PostalCode = digits(r.From.PostalCode), digits(r.To.PostalCode)
	value := fmt.Sprintf("%.2f", float64(r.Package.InsuranceCents)/100)
	body := map[string]any{
		"service": r.ServiceID,
		"from":    r.From,
		"to":      r.To,
		"products": []map[string]string{{
			"name": r.ProductName, "quantity": "1", "unitary_value": value,
		}},
		"volumes": []map[string]any{{
			"height": r.Package.HeightCm, "width": r.Package.WidthCm,
			"length": r.Package.LengthCm, "weight": r.Package.WeightKg,
		}},
		"options": map[string]any{
			"insurance_value": float64(r.Package.InsuranceCents) / 100,
			"receipt":         false, "own_hand": false, "reverse": false,
			"non_commercial": r.NonCommercial,
			"platform":       "Farbo Rastreadores",
			"tags":           []map[string]string{{"tag": r.Tag}},
		},
	}
	var out CartOrder
	if err := c.call(ctx, http.MethodPost, "/api/v2/me/cart", body, &out); err != nil {
		return nil, err
	}
	if out.ID == "" {
		return nil, &APIError{Status: http.StatusBadGateway, Message: "carrinho não devolveu o id da etiqueta"}
	}
	return &out, nil
}

// RemoveFromCart desfaz uma etiqueta ainda não paga.
func (c *Client) RemoveFromCart(ctx context.Context, orderID string) error {
	return c.call(ctx, http.MethodDelete, "/api/v2/me/cart/"+url.PathEscape(orderID), nil, nil)
}

// Checkout paga a etiqueta com o saldo da carteira do Melhor Envios.
func (c *Client) Checkout(ctx context.Context, orderID string) error {
	var out struct {
		Purchase *struct {
			Status string `json:"status"`
		} `json:"purchase"`
		Message string `json:"message"`
	}
	if err := c.call(ctx, http.MethodPost, "/api/v2/me/shipment/checkout", map[string]any{"orders": []string{orderID}}, &out); err != nil {
		return err
	}
	if out.Purchase == nil {
		msg := out.Message
		if msg == "" {
			msg = "compra não confirmada"
		}
		return &APIError{Status: http.StatusPaymentRequired, Message: msg}
	}
	return nil
}

// Generate gera a etiqueta paga. A resposta traz, por etiqueta, {status,
// message}; na prática ela também vem com mensagens soltas em texto, que são
// o motivo de a etiqueta não ter sido gerada.
func (c *Client) Generate(ctx context.Context, orderID string) error {
	var out map[string]json.RawMessage
	if err := c.call(ctx, http.MethodPost, "/api/v2/me/shipment/generate", map[string]any{"orders": []string{orderID}}, &out); err != nil {
		return err
	}
	if entry, ok := out[orderID]; ok {
		var result struct {
			Status  bool   `json:"status"`
			Message string `json:"message"`
		}
		if json.Unmarshal(entry, &result) == nil {
			if !result.Status {
				return &APIError{Status: http.StatusUnprocessableEntity, Message: result.Message}
			}
			return nil
		}
		var text string
		if json.Unmarshal(entry, &text) == nil {
			return &APIError{Status: http.StatusUnprocessableEntity, Message: text}
		}
	}
	// Sem o resultado da etiqueta: junta as mensagens que vieram.
	var messages []string
	for key, value := range out {
		var text string
		if json.Unmarshal(value, &text) == nil && text != "" {
			messages = append(messages, key+": "+text)
		}
	}
	sort.Strings(messages)
	if len(messages) > 0 {
		return &APIError{Status: http.StatusUnprocessableEntity, Message: strings.Join(messages, "; ")}
	}
	return &APIError{Status: http.StatusBadGateway, Message: "geração da etiqueta sem confirmação"}
}

// Print devolve o link público para imprimir a etiqueta.
func (c *Client) Print(ctx context.Context, orderID string) (string, error) {
	var out struct {
		URL string `json:"url"`
	}
	err := c.call(ctx, http.MethodPost, "/api/v2/me/shipment/print", map[string]any{"mode": "public", "orders": []string{orderID}}, &out)
	return out.URL, err
}

// maxLabelBytes limita o arquivo da etiqueta baixado do Melhor Envios.
const maxLabelBytes = 15 << 20

// FetchLabel baixa o link de impressão da etiqueta (o de Print) e diz o que
// veio: o PDF ou a página para imprimir. Só segue links do Melhor Envios (ou
// do endereço configurado, nos testes): o link vem da API, mas não vira uma
// porta para o servidor buscar qualquer endereço.
func (c *Client) FetchLabel(ctx context.Context, link string) ([]byte, string, error) {
	u, err := url.Parse(link)
	if err != nil || u.Host == "" {
		return nil, "", &APIError{Status: http.StatusBadGateway, Message: "link de impressão inválido"}
	}
	host := strings.ToLower(u.Hostname())
	base, _ := url.Parse(c.cfg.BaseURL)
	official := u.Scheme == "https" && (host == "melhorenvio.com.br" || strings.HasSuffix(host, ".melhorenvio.com.br"))
	configured := base != nil && strings.EqualFold(u.Host, base.Host)
	if !official && !configured {
		return nil, "", &APIError{Status: http.StatusBadGateway, Message: "link de impressão fora do Melhor Envios: " + host}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Accept", "application/pdf, text/html;q=0.9, */*;q=0.8")
	req.Header.Set("User-Agent", c.userAgent())
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, "", &APIError{Status: http.StatusBadGateway, Message: "não deu para baixar a etiqueta agora; tente de novo"}
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, "", &APIError{Status: resp.StatusCode, Message: "o Melhor Envios não entregou a etiqueta"}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxLabelBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("baixando a etiqueta: %w", err)
	}
	if len(body) > maxLabelBytes {
		return nil, "", fmt.Errorf("etiqueta grande demais")
	}
	return body, resp.Header.Get("Content-Type"), nil
}

// TrackingInfo é o ciclo de vida da etiqueta (rastreio e webhooks).
type TrackingInfo struct {
	ID                  string  `json:"id"`
	Protocol            string  `json:"protocol"`
	Status              string  `json:"status"`
	Tracking            *string `json:"tracking"`
	SelfTracking        *string `json:"self_tracking"`
	MelhorEnvioTracking *string `json:"melhorenvio_tracking"`
	PostedAt            *string `json:"posted_at"`
	DeliveredAt         *string `json:"delivered_at"`
}

// Code é o código de rastreio: o da transportadora ou, enquanto ele não
// sai, o do próprio Melhor Envios.
func (t TrackingInfo) Code() string {
	return firstCode(t.Tracking, t.SelfTracking, t.MelhorEnvioTracking)
}

func firstCode(codes ...*string) string {
	for _, c := range codes {
		if c != nil && *c != "" {
			return *c
		}
	}
	return ""
}

// OrderInfo é a etiqueta em detalhe. Na prática o campo status fica em
// "released" mesmo depois de gerada; o que mostra a geração é a data dela e
// a chave de geração concluída.
type OrderInfo struct {
	ID           string  `json:"id"`
	Protocol     string  `json:"protocol"`
	Status       string  `json:"status"`
	Tracking     *string `json:"tracking"`
	SelfTracking *string `json:"self_tracking"`
	GeneratedAt  *string `json:"generated_at"`
	PostedAt     *string `json:"posted_at"`
	DeliveredAt  *string `json:"delivered_at"`
	CanceledAt   *string `json:"canceled_at"`
	ExpiredAt    *string `json:"expired_at"`
	GeneratedKey *struct {
		FinishedAt *string `json:"finished_at"`
		FailedAt   *string `json:"failed_at"`
	} `json:"generated_key"`
}

// Generated diz se a etiqueta já existe (pode imprimir).
func (o OrderInfo) Generated() bool {
	switch strings.ToLower(o.Status) {
	case "generated", "posted", "received", "delivered":
		return true
	}
	if o.GeneratedKey != nil && o.GeneratedKey.FailedAt != nil {
		return false
	}
	return o.GeneratedAt != nil || (o.GeneratedKey != nil && o.GeneratedKey.FinishedAt != nil)
}

// GenerationFailed: o Melhor Envios desistiu de gerar a etiqueta.
func (o OrderInfo) GenerationFailed() bool {
	return o.GeneratedKey != nil && o.GeneratedKey.FailedAt != nil
}

// Cancelled: etiqueta cancelada ou vencida, sem entrega.
func (o OrderInfo) Cancelled() bool {
	switch strings.ToLower(o.Status) {
	case "canceled", "cancelled", "expired":
		return true
	}
	return o.CanceledAt != nil || o.ExpiredAt != nil
}

func (o OrderInfo) Code() string { return firstCode(o.Tracking, o.SelfTracking) }

// AsTracking traduz os detalhes para o formato do rastreio. No sandbox real o
// endpoint de rastreio fica parado em "released" mesmo com a etiqueta
// postada; os detalhes trazem o status e as datas certos.
func (o OrderInfo) AsTracking() TrackingInfo {
	status := o.Status
	if o.Cancelled() {
		status = "canceled"
	}
	return TrackingInfo{ID: o.ID, Protocol: o.Protocol, Status: status, Tracking: o.Tracking,
		SelfTracking: o.SelfTracking, PostedAt: o.PostedAt, DeliveredAt: o.DeliveredAt}
}

// Order consulta uma etiqueta em detalhe.
func (c *Client) Order(ctx context.Context, orderID string) (*OrderInfo, error) {
	var out OrderInfo
	if err := c.call(ctx, http.MethodGet, "/api/v2/me/orders/"+url.PathEscape(orderID), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) Tracking(ctx context.Context, orderIDs []string) (map[string]TrackingInfo, error) {
	out := map[string]TrackingInfo{}
	if len(orderIDs) == 0 {
		return out, nil
	}
	err := c.call(ctx, http.MethodPost, "/api/v2/me/shipment/tracking", map[string]any{"orders": orderIDs}, &out)
	return out, err
}

// VerifySignature confere o X-ME-Signature de um webhook: HMAC-SHA256 do
// corpo com o Secret do aplicativo, em base64.
func VerifySignature(secret string, body []byte, signature string) bool {
	if secret == "" || signature == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(strings.TrimSpace(signature)))
}

func digits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
