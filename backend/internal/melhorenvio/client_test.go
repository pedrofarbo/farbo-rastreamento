package melhorenvio

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

type memStore struct {
	mu    sync.Mutex
	token *Token
	saves int
}

func (m *memStore) Load(context.Context) (*Token, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.token == nil {
		return nil, nil
	}
	copy := *m.token
	return &copy, nil
}
func (m *memStore) Save(_ context.Context, t *Token) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	copy := *t
	m.token, m.saves = &copy, m.saves+1
	return nil
}
func (m *memStore) Delete(context.Context) error { m.token = nil; return nil }

func newClient(t *testing.T, handler http.HandlerFunc, store TokenStore) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return New(Config{
		BaseURL: srv.URL, ClientID: "123", ClientSecret: "segredo", RedirectURL: "http://painel/api/cb",
		ContactEmail: "tec@farbo.test",
	}, store)
}

func TestAuthorizeURL(t *testing.T) {
	c := New(Config{BaseURL: SandboxURL, ClientID: "123", RedirectURL: "http://painel/api/cb"}, &memStore{})
	raw := c.AuthorizeURL("abc")
	if strings.Contains(raw, "+") {
		t.Errorf("escopos devem ir separados por %%20, não +: %s", raw)
	}
	u, _ := url.Parse(raw)
	q := u.Query()
	if u.Path != "/oauth/authorize" || q.Get("client_id") != "123" || q.Get("state") != "abc" ||
		q.Get("response_type") != "code" || q.Get("redirect_uri") != "http://painel/api/cb" {
		t.Fatalf("URL de autorização errada: %s", raw)
	}
	if !strings.Contains(q.Get("scope"), "shipping-checkout") || !strings.Contains(q.Get("scope"), "transactions-read") ||
		!strings.Contains(q.Get("scope"), " ") {
		t.Errorf("escopos: %q", q.Get("scope"))
	}
}

func TestExchangeCodeSavesToken(t *testing.T) {
	store := &memStore{}
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if r.URL.Path != "/oauth/token" || body["grant_type"] != "authorization_code" || body["code"] != "xyz" ||
			body["client_secret"] != "segredo" || body["redirect_uri"] != "http://painel/api/cb" {
			t.Errorf("pedido de token errado: %s %v", r.URL.Path, body)
		}
		if !strings.Contains(r.Header.Get("User-Agent"), "tec@farbo.test") {
			t.Errorf("User-Agent sem e-mail de contato: %q", r.Header.Get("User-Agent"))
		}
		_, _ = w.Write([]byte(`{"token_type":"Bearer","expires_in":2592000,"access_token":"A1","refresh_token":"R1"}`))
	}, store)
	if err := c.ExchangeCode(context.Background(), "xyz"); err != nil {
		t.Fatal(err)
	}
	if store.token == nil || store.token.AccessToken != "A1" || store.token.RefreshToken != "R1" ||
		time.Until(store.token.ExpiresAt) < 29*24*time.Hour {
		t.Fatalf("token não guardado direito: %+v", store.token)
	}
}

func TestRefreshBeforeExpiry(t *testing.T) {
	store := &memStore{token: &Token{AccessToken: "velho", RefreshToken: "R1", ExpiresAt: time.Now().Add(time.Hour)}}
	var calls []string
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path+" "+r.Header.Get("Authorization"))
		if r.URL.Path == "/oauth/token" {
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["grant_type"] != "refresh_token" || body["refresh_token"] != "R1" {
				t.Errorf("refresh errado: %v", body)
			}
			_, _ = w.Write([]byte(`{"expires_in":2592000,"access_token":"novo","refresh_token":"R2"}`))
			return
		}
		_, _ = w.Write([]byte(`{"firstname":"Pedro","email":"p@x"}`))
	}, store)
	acc, err := c.Account(context.Background())
	if err != nil || acc.FirstName != "Pedro" {
		t.Fatalf("conta: %v %v", acc, err)
	}
	if len(calls) != 2 || calls[1] != "/api/v2/me Bearer novo" {
		t.Fatalf("esperava renovar antes (falta < 1 dia) e usar o novo: %v", calls)
	}
	if store.token.RefreshToken != "R2" {
		t.Errorf("refresh token novo não guardado")
	}
}

func TestRetryOnUnauthorized(t *testing.T) {
	store := &memStore{token: &Token{AccessToken: "revogado", RefreshToken: "R1", ExpiresAt: time.Now().Add(20 * 24 * time.Hour)}}
	attempts := 0
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/oauth/token":
			_, _ = w.Write([]byte(`{"expires_in":2592000,"access_token":"bom","refresh_token":"R2"}`))
		case r.Header.Get("Authorization") == "Bearer revogado":
			attempts++
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"Unauthenticated."}`))
		default:
			attempts++
			_, _ = w.Write([]byte(`{"firstname":"Ok"}`))
		}
	}, store)
	if _, err := c.Account(context.Background()); err != nil {
		t.Fatalf("depois do 401 devia renovar e repetir: %v", err)
	}
	if attempts != 2 {
		t.Errorf("tentativas = %d, quer 2", attempts)
	}
}

func TestNotConnected(t *testing.T) {
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("não devia chamar a API") }, &memStore{})
	if _, err := c.Account(context.Background()); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("sem token: quer ErrNotConnected, veio %v", err)
	}
}

func TestRefreshRejectedMeansReconnect(t *testing.T) {
	store := &memStore{token: &Token{AccessToken: "x", RefreshToken: "vencido", ExpiresAt: time.Now()}}
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","message":"The refresh token is invalid."}`))
	}, store)
	if _, err := c.Account(context.Background()); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("refresh recusado: quer ErrNotConnected, veio %v", err)
	}
}

func TestCalculateParsesAndSorts(t *testing.T) {
	store := &memStore{token: &Token{AccessToken: "A", RefreshToken: "R", ExpiresAt: time.Now().Add(20 * 24 * time.Hour)}}
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["from"].(map[string]any)["postal_code"] != "01310100" {
			t.Errorf("CEP de origem não saiu só com dígitos: %v", body["from"])
		}
		_, _ = w.Write([]byte(`[
			{"id":2,"name":"SEDEX","price":"45.10","custom_price":"45.10","delivery_time":2,"custom_delivery_time":3,"company":{"name":"Correios"}},
			{"id":3,"name":".Package","error":"Serviço indisponível para o trecho.","company":{"name":"Jadlog"}},
			{"id":1,"name":"PAC","price":37.79,"delivery_time":8,"company":{"name":"Correios"}}]`))
	}, store)
	quotes, err := c.Calculate(context.Background(), "01310-100", "20040-020",
		Package{HeightCm: 5, WidthCm: 12, LengthCm: 16, WeightKg: 0.3, InsuranceCents: 15000}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(quotes) != 3 || quotes[0].ServiceID != 1 || quotes[0].PriceCents != 3779 || quotes[0].Name() != "Correios PAC" {
		t.Fatalf("cotação: %+v", quotes)
	}
	if quotes[1].PriceCents != 4510 || quotes[1].DeliveryDays != 3 {
		t.Errorf("preço/prazo personalizados: %+v", quotes[1])
	}
	if quotes[2].Error == "" {
		t.Errorf("serviço indisponível devia ir para o fim, com o erro: %+v", quotes[2])
	}
}

func TestErrorMessageShowsFirstFieldError(t *testing.T) {
	msg := errorMessage(422, []byte(`{"message":"The given data was invalid.","errors":{"to.document":["O documento é inválido."]}}`))
	if msg != "The given data was invalid. (to.document: O documento é inválido.)" {
		t.Errorf("mensagem: %q", msg)
	}
}

func TestVerifySignature(t *testing.T) {
	body := []byte(`{"event":"order.posted","data":{"id":"x"}}`)
	mac := hmac.New(sha256.New, []byte("segredo"))
	mac.Write(body)
	good := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	if !VerifySignature("segredo", body, good) {
		t.Error("assinatura válida recusada")
	}
	if VerifySignature("segredo", append(body, ' '), good) || VerifySignature("outro", body, good) || VerifySignature("", body, good) {
		t.Error("assinatura inválida aceita")
	}
}

func TestTrackingCodePrefersCarrier(t *testing.T) {
	carrier, me := "BR123", "ME999"
	if (TrackingInfo{Tracking: &carrier, MelhorEnvioTracking: &me}).Code() != "BR123" {
		t.Error("devia preferir o código da transportadora")
	}
	empty := ""
	if (TrackingInfo{Tracking: &empty, MelhorEnvioTracking: &me}).Code() != "ME999" {
		t.Error("sem o da transportadora, usa o do Melhor Envios")
	}
}

func TestGenerateReadsLooseMessages(t *testing.T) {
	store := &memStore{token: &Token{AccessToken: "A", RefreshToken: "R", ExpiresAt: time.Now().Add(20 * 24 * time.Hour)}}
	reply := `{"abc":{"status":true,"message":"Envio gerado com sucesso"}}`
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(reply)) }, store)
	if err := c.Generate(context.Background(), "abc"); err != nil {
		t.Fatalf("formato da documentação: %v", err)
	}
	reply = `{"abc":{"status":false,"message":"Agência obrigatória"}}`
	if err := c.Generate(context.Background(), "abc"); err == nil || !strings.Contains(err.Error(), "Agência obrigatória") {
		t.Fatalf("status false: %v", err)
	}
	reply = `{"abc":"Etiqueta sem saldo","outro":"x"}`
	if err := c.Generate(context.Background(), "abc"); err == nil || !strings.Contains(err.Error(), "Etiqueta sem saldo") {
		t.Fatalf("mensagem em texto na etiqueta: %v", err)
	}
	reply = `{"error":"Para esta transportadora, informe a agência"}`
	if err := c.Generate(context.Background(), "abc"); err == nil || !strings.Contains(err.Error(), "informe a agência") {
		t.Fatalf("mensagem solta: %v", err)
	}
}

// Formato visto no sandbox real: status continua "released" depois de gerada.
func TestOrderGeneratedFromDetails(t *testing.T) {
	var o OrderInfo
	raw := `{"id":"a2dd","status":"released","tracking":null,"self_tracking":"ME26006ES26BR",
		"generated_at":"2026-09-30 02:52:20","canceled_at":null,
		"generated_key":{"finished_at":"2026-09-30 02:56:09","failed_at":null}}`
	if err := json.Unmarshal([]byte(raw), &o); err != nil {
		t.Fatal(err)
	}
	if !o.Generated() || o.Cancelled() || o.Code() != "ME26006ES26BR" {
		t.Fatalf("gerada=%v cancelada=%v código=%q", o.Generated(), o.Cancelled(), o.Code())
	}
	var pending OrderInfo
	_ = json.Unmarshal([]byte(`{"status":"released","generated_at":null,"generated_key":{"finished_at":null,"failed_at":null}}`), &pending)
	if pending.Generated() {
		t.Error("em geração não é gerada")
	}
	var failed OrderInfo
	_ = json.Unmarshal([]byte(`{"status":"released","generated_key":{"finished_at":"x","failed_at":"y"}}`), &failed)
	if failed.Generated() || !failed.GenerationFailed() {
		t.Error("geração com falha")
	}
}

// Visto no sandbox real: detalhes com "posted" enquanto o rastreio seguia em
// "released".
func TestOrderAsTracking(t *testing.T) {
	var o OrderInfo
	_ = json.Unmarshal([]byte(`{"id":"a2dd","status":"posted","posted_at":"2026-09-30 03:10:09","self_tracking":"ME26006ES26BR"}`), &o)
	tr := o.AsTracking()
	if tr.Status != "posted" || tr.PostedAt == nil || tr.Code() != "ME26006ES26BR" {
		t.Fatalf("rastreio a partir dos detalhes: %+v", tr)
	}
	var c OrderInfo
	_ = json.Unmarshal([]byte(`{"id":"x","status":"released","canceled_at":"2026-09-30 04:00:00"}`), &c)
	if c.AsTracking().Status != "canceled" {
		t.Fatalf("cancelada pelos detalhes: %+v", c.AsTracking())
	}
}

func TestBalanceAcceptsTextAndNumbers(t *testing.T) {
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v2/me/balance" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"balance": "1520.35", "reserved": 22.4, "debts": null}`))
	}, &memStore{token: &Token{AccessToken: "a", ExpiresAt: time.Now().Add(72 * time.Hour)}})
	wallet, err := c.Balance(context.Background())
	if err != nil || wallet.Balance.Cents() != 152035 || wallet.Reserved.Cents() != 2240 || wallet.Debts.Cents() != 0 {
		t.Fatalf("carteira = %+v (%v)", wallet, err)
	}
	if c.PanelURL() != c.cfg.BaseURL+"/painel" {
		t.Errorf("painel = %s", c.PanelURL())
	}
}

func TestAddBalanceFindsThePaymentLink(t *testing.T) {
	var reply string
	var got map[string]string
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(reply))
	}, &memStore{token: &Token{AccessToken: "a", ExpiresAt: time.Now().Add(72 * time.Hour)}})
	ctx := context.Background()
	back := "https://painel.farbo.test/pedidos?saldo=pago"

	// O link no pagamento; o Pix não leva os dados da empresa.
	reply = `{"payment": {"id": "p1", "protocol": "PAY-1", "status": "pending", "link": "https://me.test/pix/1"}, "redirect": "` + back + `", "digitable": null}`
	top, err := c.AddBalance(ctx, TopUpRequest{Method: TopUpPix, ValueCents: 7005, RedirectURL: back, CompanyName: "Farbo", CNPJ: "49.757.084/0001-00"})
	if err != nil || top.Link != "https://me.test/pix/1" || top.Protocol != "PAY-1" || top.ValueCents != 7005 {
		t.Fatalf("Pix = %+v (%v)", top, err)
	}
	if got["value"] != "70.05" || got["gateway"] != TopUpGateway || got["slug"] != "pix" || got["redirect_url"] != back || got["cnpj"] != "" {
		t.Errorf("pedido = %v", got)
	}

	// Sem link no pagamento: vale o redirect da resposta, se não for o de volta.
	reply = `{"payment": {"id": "p2", "protocol": "PAY-2", "status": "pending", "link": null}, "redirect": "https://me.test/checkout/notify/2", "digitable": "3419 0000"}`
	top, err = c.AddBalance(ctx, TopUpRequest{Method: TopUpBoleto, ValueCents: 10000, RedirectURL: back, CompanyName: "Farbo", CNPJ: "49.757.084/0001-00"})
	if err != nil || top.Link != "https://me.test/checkout/notify/2" || top.Digitable != "3419 0000" {
		t.Fatalf("boleto = %+v (%v)", top, err)
	}
	if got["company_name"] != "Farbo" || got["cnpj"] != "49757084000100" {
		t.Errorf("boleto sem a empresa: %v", got)
	}

	// Só o endereço de volta (ou nada que seja link): sem link.
	reply = `{"payment": {"id": "p3", "protocol": "PAY-3", "status": "pending", "link": {"x": 1}}, "redirect": "` + back + `"}`
	if top, err = c.AddBalance(ctx, TopUpRequest{Method: TopUpPix, ValueCents: 100, RedirectURL: back}); err != nil || top.Link != "" {
		t.Errorf("sem link = %+v (%v)", top, err)
	}

	// Sem pagamento na resposta: recusa com a mensagem.
	reply = `{"message": "Valor mínimo de R$ 5,00"}`
	if _, err = c.AddBalance(ctx, TopUpRequest{Method: TopUpPix, ValueCents: 100}); err == nil || !strings.Contains(err.Error(), "Valor mínimo") {
		t.Errorf("sem pagamento = %v", err)
	}
}

func TestAddBalanceFindsThePixCode(t *testing.T) {
	code := "00020126360014br.gov.bcb.pix0114+5511999999999520400005303986540510.005802BR5905YAPAY6009SAO PAULO6304ABCD"
	var reply string
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(reply))
	}, &memStore{token: &Token{AccessToken: "a", ExpiresAt: time.Now().Add(72 * time.Hour)}})
	ctx := context.Background()

	// Dentro da resposta do meio de pagamento, que vem como texto com JSON.
	inner, _ := json.Marshal(map[string]any{"data": map[string]string{"qrcode_original_path": code, "qrcode_path": "https://x/qr.png"}})
	quoted, _ := json.Marshal(string(inner))
	reply = `{"payment": {"id": "p1", "protocol": "PAY-1", "status": "pending", "link": null, "response": ` + string(quoted) + `}, "redirect": null}`
	top, err := c.AddBalance(ctx, TopUpRequest{Method: TopUpPix, ValueCents: 1000})
	if err != nil || top.PixCode != code {
		t.Fatalf("copia-e-cola = %q (%v)", top.PixCode, err)
	}
	if top.Shape != "{payment{id,link,protocol,response,status},redirect}" {
		t.Errorf("campos = %s", top.Shape)
	}

	// Num campo qualquer, com espaço em volta; e o boleto não procura.
	reply = `{"payment": {"id": "p2", "protocol": "PAY-2", "status": "pending"}, "pix": {"emv": " ` + code + ` "}}`
	if top, err = c.AddBalance(ctx, TopUpRequest{Method: TopUpPix, ValueCents: 1000}); err != nil || top.PixCode != code {
		t.Errorf("copia-e-cola solto = %q (%v)", top.PixCode, err)
	}
	if top, err = c.AddBalance(ctx, TopUpRequest{Method: TopUpBoleto, ValueCents: 1000}); err != nil || top.PixCode != "" {
		t.Errorf("boleto com copia-e-cola = %q (%v)", top.PixCode, err)
	}

	// Sem nenhum: vazio, sem inventar.
	reply = `{"payment": {"id": "p3", "protocol": "PAY-3", "status": "pending", "link": "https://me.test/pix/3"}, "qr": "00020101-pela-metade"}`
	if top, err = c.AddBalance(ctx, TopUpRequest{Method: TopUpPix, ValueCents: 1000}); err != nil || top.PixCode != "" {
		t.Errorf("sem copia-e-cola = %q (%v)", top.PixCode, err)
	}
}
