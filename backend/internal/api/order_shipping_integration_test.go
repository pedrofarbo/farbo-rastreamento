package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/fulfillment"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/melhorenvio"
)

// fakeMelhorEnvio cota o frete como o Melhor Envios: dois serviços que
// atendem e um que não; ou falha, se o teste mandar.
type fakeMelhorEnvio struct {
	mu    sync.Mutex
	calls int
	to    string
	fail  bool
}

func (f *fakeMelhorEnvio) server(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.URL.Path != "/api/v2/me/shipment/calculate" {
			http.NotFound(w, r)
			return
		}
		f.calls++
		var body struct {
			To struct {
				PostalCode string `json:"postal_code"`
			} `json:"to"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.to = body.To.PostalCode
		if f.fail {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = io.WriteString(w, `{"message":"indisponível"}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[
			{"id": 2, "name": "SEDEX", "price": "39.90", "delivery_time": 2, "company": {"name": "Correios"}},
			{"id": 3, "name": ".Package", "error": "Serviço indisponível para o trecho", "company": {"name": "Jadlog"}},
			{"id": 1, "name": "PAC", "price": "22.40", "custom_price": "22.40", "delivery_time": 7, "company": {"name": "Correios"}}
		]`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (f *fakeMelhorEnvio) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// O frete no pedido de um rastreador, com o Melhor Envios de mentira: o
// cliente vê as formas de entrega do CEP dele, escolhe uma e paga junto com
// o equipamento (o preço é o do servidor, não o da tela); a central pode
// cobrar ou não; e sem cotação, o pedido do cliente não sai sem frete. Em
// São Paulo, o cliente pode combinar a entrega com a central (sem frete,
// mesmo com o Melhor Envios fora).
// Precisa de FARBO_TEST_DATABASE_URL.
func TestOrderShippingEndToEnd(t *testing.T) {
	fulfillment.ForgetQuotes()
	t.Cleanup(fulfillment.ForgetQuotes)
	fake := &fakeMelhorEnvio{}
	me := fake.server(t)
	env, _ := newLeadsEnv(t, func(d *Deps) {
		shipping := config.Shipping{
			From: config.ShippingAddress{
				Name: "Farbo", Phone: "11999990000", Document: "52998224725", PostalCode: "01310100", Address: "Av. Paulista",
				Number: "1000", District: "Bela Vista", City: "São Paulo", State: "SP",
			},
			WeightKg: 0.3, HeightCm: 4, WidthCm: 12, LengthCm: 16, InsuranceCents: 15000,
			ArrangeCities: []string{"São Paulo/SP"},
		}
		d.Config.Shipping = shipping
		carrier := melhorenvio.New(melhorenvio.Config{BaseURL: me.URL, StaticToken: "token", ContactEmail: "t@farbo.test"}, nil)
		d.Fulfillment = fulfillment.NewService(d.DB, fulfillment.NewRepository(d.DB), carrier, nil, shipping, "Rastreador",
			slog.New(slog.NewTextHandler(io.Discard, nil)))
	})
	ctx := context.Background()
	authSvc := auth.NewService(auth.NewRepository(env.db), config.Auth{BcryptCost: bcrypt.MinCost}, nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	customer, err := authSvc.CreateUser(ctx, "frete@cliente.test", "Cliente Frete", auth.RoleCustomer, userPassword)
	if err != nil {
		t.Fatal(err)
	}
	token := env.login("frete@cliente.test")
	admin := env.login(auth.RoleAdmin + "@leads.test")
	type quoteView struct {
		Enabled bool                `json:"enabled"`
		ZipCode string              `json:"zipCode"`
		Quotes  []melhorenvio.Quote `json:"quotes"`
		Problem string              `json:"problem"`
		Arrange bool                `json:"arrange"`
	}
	quote := func(tok, path string) quoteView {
		t.Helper()
		var q quoteView
		if err := json.Unmarshal(env.must(tok, http.MethodGet, path, nil, http.StatusOK), &q); err != nil {
			t.Fatal(err)
		}
		return q
	}

	// Sem endereço, não há para onde cotar.
	if q := quote(token, "/api/me/shipping-quote"); !q.Enabled || len(q.Quotes) != 0 || !strings.Contains(q.Problem, "endereço") {
		t.Fatalf("sem endereço = %+v", q)
	}
	env.must(token, http.MethodPut, "/api/me/address", map[string]any{
		"zipCode": "20040-020", "street": "Rua da Assembleia", "number": "10", "district": "Centro", "city": "Rio de Janeiro", "state": "RJ",
	}, http.StatusOK)

	// As formas de entrega do CEP: só as que atendem, da mais barata.
	q := quote(token, "/api/me/shipping-quote")
	if q.ZipCode != "20040020" || len(q.Quotes) != 2 || q.Quotes[0].ServiceID != 1 || q.Quotes[0].PriceCents != 2240 ||
		q.Quotes[0].Name() != "Correios PAC" || q.Quotes[0].DeliveryDays != 7 || q.Quotes[1].PriceCents != 3990 || q.Problem != "" ||
		q.Arrange {
		t.Fatalf("cotação = %+v", q)
	}
	if fake.to != "20040020" {
		t.Errorf("cotou para %q", fake.to)
	}

	order := func(tok, path string, body map[string]any, want int) map[string]any {
		t.Helper()
		var out map[string]any
		_ = json.Unmarshal(env.must(tok, http.MethodPost, path, body, want), &out)
		return out
	}
	vehicle := func(plate string) map[string]any { return map[string]any{"name": "Moto " + plate, "plate": plate} }

	// O cliente precisa escolher; serviço que não atende é recusado.
	order(token, "/api/me/trackers", map[string]any{"vehicle": vehicle("FRT1A11")}, http.StatusBadRequest)
	order(token, "/api/me/trackers", map[string]any{"vehicle": vehicle("FRT1A11"), "shippingServiceId": 3}, http.StatusBadRequest)
	// Combinar a entrega é só para São Paulo.
	if out := order(token, "/api/me/trackers", map[string]any{"vehicle": vehicle("FRT1A11"), "arrangeDelivery": true},
		http.StatusBadRequest); !strings.Contains(out["error"].(string), "só vale para São Paulo/SP") {
		t.Errorf("combinar fora de SP = %+v", out)
	}
	// Escolheu o PAC: o frete entra na fatura do equipamento (com o preço do servidor).
	// (A tela não manda preço: um campo a mais é recusado.)
	order(token, "/api/me/trackers", map[string]any{"vehicle": vehicle("FRT1A11"), "shippingServiceId": 1, "shippingCents": 1},
		http.StatusBadRequest)
	res := order(token, "/api/me/trackers", map[string]any{"vehicle": vehicle("FRT1A11"), "shippingServiceId": 1}, http.StatusCreated)
	setup, _ := res["setupInvoice"].(map[string]any)
	shipping, _ := res["shipping"].(map[string]any)
	if setup["amountCents"] != float64(15000+2240) || !strings.Contains(setup["description"].(string), "frete Correios PAC (R$ 22,40)") ||
		shipping["name"] != "Correios PAC" || shipping["priceCents"] != float64(2240) {
		t.Fatalf("pedido = %+v", res)
	}
	if fake.count() != 1 {
		t.Errorf("cotações no Melhor Envios = %d (a do pedido vem da guardada)", fake.count())
	}
	// O acompanhamento guarda a entrega escolhida (o cliente vê, a etiqueta sai por ela).
	var mine []map[string]any
	_ = json.Unmarshal(env.must(token, http.MethodGet, "/api/me/fulfillments", nil, http.StatusOK), &mine)
	if len(mine) != 1 || mine[0]["deliveryService"] != "Correios PAC" || mine[0]["deliveryDays"] != float64(7) {
		t.Fatalf("acompanhamento do cliente = %+v", mine)
	}
	var all []map[string]any
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/fulfillments", nil, http.StatusOK), &all)
	if len(all) != 1 || all[0]["quotedServiceId"] != float64(1) || all[0]["quotedPriceCents"] != float64(2240) {
		t.Fatalf("acompanhamento na central = %+v", all)
	}

	// O Melhor Envios fora: a tela avisa e o pedido do cliente não sai sem frete.
	fulfillment.ForgetQuotes()
	fake.mu.Lock()
	fake.fail = true
	fake.mu.Unlock()
	if q := quote(token, "/api/me/shipping-quote"); len(q.Quotes) != 0 || !strings.Contains(q.Problem, "tente de novo") {
		t.Errorf("fora do ar = %+v", q)
	}
	order(token, "/api/me/trackers", map[string]any{"vehicle": vehicle("FRT5E55"), "shippingServiceId": 1}, http.StatusServiceUnavailable)

	// Em São Paulo (acento e maiúsculas não importam), dá para combinar a
	// entrega: sem frete na fatura e sem o Melhor Envios.
	if _, err := authSvc.CreateUser(ctx, "sp@cliente.test", "Cliente SP", auth.RoleCustomer, userPassword); err != nil {
		t.Fatal(err)
	}
	sp := env.login("sp@cliente.test")
	env.must(sp, http.MethodPut, "/api/me/address", map[string]any{
		"zipCode": "01305-000", "street": "Rua Augusta", "number": "500", "district": "Consolação", "city": "sao paulo", "state": "sp",
	}, http.StatusOK)
	if q := quote(sp, "/api/me/shipping-quote"); !q.Arrange || !strings.Contains(q.Problem, "tente de novo") {
		t.Errorf("SP com o Melhor Envios fora = %+v", q)
	}
	order(sp, "/api/me/trackers", map[string]any{"vehicle": vehicle("FRT6F66"), "arrangeDelivery": true, "shippingServiceId": 1},
		http.StatusBadRequest)
	calls := fake.count()
	res = order(sp, "/api/me/trackers", map[string]any{"vehicle": vehicle("FRT6F66"), "arrangeDelivery": true}, http.StatusCreated)
	setup, _ = res["setupInvoice"].(map[string]any)
	shipping, _ = res["shipping"].(map[string]any)
	if setup["amountCents"] != float64(15000) || strings.Contains(setup["description"].(string), "frete") ||
		shipping["arranged"] != true || shipping["name"] != fulfillment.ArrangedService || shipping["priceCents"] != float64(0) {
		t.Fatalf("pedido combinado = %+v", res)
	}
	if fake.count() != calls {
		t.Errorf("a entrega combinada cotou no Melhor Envios")
	}
	_ = json.Unmarshal(env.must(sp, http.MethodGet, "/api/me/fulfillments", nil, http.StatusOK), &mine)
	if len(mine) != 1 || mine[0]["deliveryArranged"] != true || mine[0]["deliveryService"] != fulfillment.ArrangedService ||
		mine[0]["deliveryDays"] != nil {
		t.Fatalf("acompanhamento combinado = %+v", mine)
	}
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/fulfillments", nil, http.StatusOK), &all)
	for _, f := range all {
		if f["customerName"] == "Cliente SP" && (f["deliveryArranged"] != true || f["quotedServiceId"] != nil || f["quotedPriceCents"] != float64(0)) {
			t.Errorf("combinado na central = %+v", f)
		}
	}
	// De volta.
	fulfillment.ForgetQuotes()
	fake.mu.Lock()
	fake.fail = false
	fake.mu.Unlock()

	// A central: cota pela ficha e escolhe o SEDEX — ou não cobra frete.
	if q := quote(admin, "/api/customers/"+customer.ID.String()+"/shipping-quote"); len(q.Quotes) != 2 {
		t.Fatalf("cotação na central = %+v", q)
	}
	plan := map[string]any{"planName": "Plano Mensal", "priceCents": 6990, "dueDay": 10}
	res = order(admin, "/api/customers/"+customer.ID.String()+"/trackers",
		map[string]any{"vehicle": vehicle("FRT2B22"), "equipmentCents": 15000, "plan": plan, "shippingServiceId": 2}, http.StatusCreated)
	if setup, _ := res["setupInvoice"].(map[string]any); setup["amountCents"] != float64(15000+3990) {
		t.Errorf("central com SEDEX = %+v", res)
	}
	res = order(admin, "/api/customers/"+customer.ID.String()+"/trackers",
		map[string]any{"vehicle": vehicle("FRT3C33"), "equipmentCents": 15000, "plan": plan}, http.StatusCreated)
	if setup, _ := res["setupInvoice"].(map[string]any); setup["amountCents"] != float64(15000) || res["shipping"] != nil {
		t.Errorf("central sem frete = %+v", res)
	}
	// Equipamento sem cobrança, com frete: a fatura é só do frete.
	res = order(admin, "/api/customers/"+customer.ID.String()+"/trackers",
		map[string]any{"vehicle": vehicle("FRT4D44"), "equipmentCents": 0, "plan": plan, "shippingServiceId": 1}, http.StatusCreated)
	if setup, _ := res["setupInvoice"].(map[string]any); setup == nil || setup["amountCents"] != float64(2240) ||
		!strings.HasPrefix(setup["description"].(string), "Frete do rastreador") {
		t.Errorf("só o frete = %+v", res)
	}

}
