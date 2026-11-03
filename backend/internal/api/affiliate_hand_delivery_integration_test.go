package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/contract"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/orders"
)

// A entrega em mãos pelo representante (ele tem os kits): pelo link dele, a
// oferta e a cotação trazem só a entrega em mãos, sem frete; a contratação
// sai com ela (mesmo sem escolher a entrega); o pedido do cliente indicado
// pelo app também; e o pedido mostra quem entrega. Precisa de
// FARBO_TEST_DATABASE_URL.
func TestAffiliateHandDelivery(t *testing.T) {
	env, _ := newLeadsEnv(t, func(d *Deps) {
		d.Config.Catalog.SetupDueDays = 0
		d.Orders = orders.NewService(d.DB, d.Billing, d.Vehicles, d.Config.Catalog, d.Log)
		doc, err := contract.Render(contract.Params{Company: config.Company{Name: "Farbo Rastreadores"}, HistoryOptions: []int{7, 14, 30}})
		if err != nil {
			t.Fatal(err)
		}
		svc := contract.NewService(d.DB, doc, &recordingContractMailer{}, d.Log)
		svc.SetSync()
		d.Contract = svc
	})
	ctx := context.Background()
	admin := env.login(auth.RoleAdmin + "@leads.test")
	env.must(admin, http.MethodPost, "/api/affiliates", map[string]any{
		"name": "Rui Representante", "code": "rui", "active": true, "handDelivery": true,
	}, http.StatusCreated)

	var o struct {
		HandDelivery    string `json:"handDelivery"`
		ContractVersion string `json:"contractVersion"`
	}
	_ = json.Unmarshal(env.must("", http.MethodGet, "/api/public/checkout", nil, http.StatusOK), &o)
	if o.HandDelivery != "" {
		t.Errorf("sem o link = %+v", o)
	}
	_ = json.Unmarshal(env.must("", http.MethodGet, "/api/public/checkout?ref=rui", nil, http.StatusOK), &o)
	if o.HandDelivery != "Rui Representante" {
		t.Fatalf("pelo link = %+v", o)
	}

	address := map[string]any{"zipCode": "01305-000", "street": "Rua Augusta", "number": "500", "district": "Consolação",
		"city": "São Paulo", "state": "SP"}
	type quote struct {
		Enabled bool   `json:"enabled"`
		Arrange bool   `json:"arrange"`
		HandBy  string `json:"handBy"`
		Quotes  []any  `json:"quotes"`
	}
	var q quote
	_ = json.Unmarshal(env.must("", http.MethodPost, "/api/public/checkout/shipping?ref=rui", address, http.StatusOK), &q)
	if !q.Enabled || !q.Arrange || q.HandBy != "Rui Representante" || len(q.Quotes) != 0 {
		t.Errorf("cotação pelo link = %+v", q)
	}

	// A contratação pelo link: em mãos, sem frete, mesmo sem escolher.
	var r struct {
		Subscription billing.Subscription `json:"subscription"`
		Shipping     *struct {
			Name     string `json:"name"`
			Arranged bool   `json:"arranged"`
			HandBy   string `json:"handBy"`
		} `json:"shipping"`
		Tokens *struct {
			AccessToken string `json:"accessToken"`
		} `json:"tokens"`
	}
	_ = json.Unmarshal(env.must("", http.MethodPost, "/api/public/checkout", map[string]any{
		"name": "Ana Souza", "email": "ana@rui.test", "phone": "(11) 95555-0000", "document": "529.982.247-25", "password": "senha-forte-1",
		"vehicle": map[string]any{"kind": "CAR", "brand": "Fiat", "model": "Uno", "plate": ""}, "address": address,
		"contractVersion": o.ContractVersion, "accept": true, "ref": "rui",
	}, http.StatusCreated), &r)
	if r.Shipping == nil || !r.Shipping.Arranged || r.Shipping.HandBy != "Rui Representante" {
		t.Fatalf("entrega da contratação = %+v", r.Shipping)
	}
	handOf := func(subscription uuid.UUID) (string, bool, int) {
		t.Helper()
		var by string
		var arranged bool
		var freight int
		if err := env.db.QueryRow(ctx, `SELECT delivery_hand_by, delivery_arranged, COALESCE(quoted_price_cents, 0)
			FROM fulfillments WHERE subscription_id = $1`, subscription).Scan(&by, &arranged, &freight); err != nil {
			t.Fatal(err)
		}
		return by, arranged, freight
	}
	if by, arranged, freight := handOf(r.Subscription.ID); by != "Rui Representante" || !arranged || freight != 0 {
		t.Errorf("acompanhamento = %q %v %d", by, arranged, freight)
	}

	// No app: a cotação e o pedido do cliente indicado também em mãos.
	token := r.Tokens.AccessToken
	_ = json.Unmarshal(env.must(token, http.MethodGet, "/api/me/shipping-quote", nil, http.StatusOK), &q)
	if q.HandBy != "Rui Representante" || len(q.Quotes) != 0 {
		t.Errorf("cotação no app = %+v", q)
	}
	var placed struct {
		Subscription billing.Subscription `json:"subscription"`
	}
	_ = json.Unmarshal(env.must(token, http.MethodPost, "/api/me/trackers", map[string]any{
		"vehicle": map[string]any{"name": "Moto da Ana", "plate": "ANA1A11"},
	}, http.StatusCreated), &placed)
	if by, _, freight := handOf(placed.Subscription.ID); by != "Rui Representante" || freight != 0 {
		t.Errorf("pedido pelo app = %q %d", by, freight)
	}
}
