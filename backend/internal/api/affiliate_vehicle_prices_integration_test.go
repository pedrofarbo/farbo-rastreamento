package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/affiliates"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/contract"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/orders"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/vehicles"
)

// A mensalidade pelo link do afiliado, por tipo de veículo: a oferta pelo
// link traz a tabela dele; quem contrata pelo link paga a do tipo do veículo
// (o caminhão também); o cliente indicado segue nela nos veículos que pede
// pelo app; o plano que a central define vale acima dela; e o afiliado sem
// tabela volta ao preço normal. Precisa de FARBO_TEST_DATABASE_URL.
func TestAffiliateVehiclePrices(t *testing.T) {
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
	table := vehicles.KindPrices{MotorcycleCents: 3990, CarCents: 4990, TruckCents: 5990}

	// A tabela leva os três preços.
	env.must(admin, http.MethodPost, "/api/affiliates", map[string]any{
		"name": "Errado", "active": true, "planPrices": map[string]any{"motorcycleCents": 3990, "carCents": 0, "truckCents": 5990},
	}, http.StatusBadRequest)
	var lia affiliates.Affiliate
	_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/affiliates", map[string]any{
		"name": "Lia Tabela", "code": "lia-tabela", "active": true, "planPrices": table,
	}, http.StatusCreated), &lia)
	if lia.PlanPrices == nil || *lia.PlanPrices != table {
		t.Fatalf("afiliada = %+v", lia)
	}

	type offerView struct {
		PlanCents       int                  `json:"planCents"`
		PlanByKind      *vehicles.KindPrices `json:"planByKind"`
		ContractVersion string               `json:"contractVersion"`
	}
	offer := func(query string) offerView {
		t.Helper()
		var o offerView
		_ = json.Unmarshal(env.must("", http.MethodGet, "/api/public/checkout"+query, nil, http.StatusOK), &o)
		return o
	}
	if o := offer(""); o.PlanByKind != nil || o.PlanCents != 3490 {
		t.Errorf("oferta sem o link = %+v", o)
	}
	o := offer("?ref=lia-tabela")
	if o.PlanByKind == nil || *o.PlanByKind != table {
		t.Fatalf("oferta pelo link = %+v", o)
	}

	address := map[string]any{"zipCode": "01305-000", "street": "Rua Augusta", "number": "500", "district": "Consolação",
		"city": "São Paulo", "state": "SP"}
	type result struct {
		Subscription billing.Subscription `json:"subscription"`
		FirstInvoice *billing.Invoice     `json:"firstInvoice"`
		Tokens       *struct {
			AccessToken string `json:"accessToken"`
		} `json:"tokens"`
	}
	contractWith := func(email, document, kind string) result {
		t.Helper()
		var r result
		_ = json.Unmarshal(env.must("", http.MethodPost, "/api/public/checkout", map[string]any{
			"name": "Gil Souza", "email": email, "phone": "(11) 95555-0000", "document": document, "password": "senha-forte-1",
			"vehicle": map[string]any{"kind": kind, "brand": "Volvo", "model": "FH", "plate": ""}, "address": address,
			"contractVersion": o.ContractVersion, "accept": true, "ref": "lia-tabela",
		}, http.StatusCreated), &r)
		return r
	}

	// O caminhão pelo link: a mensalidade dele na tabela.
	gil := contractWith("gil@tabela.test", "529.982.247-25", vehicles.KindTruck)
	if gil.Subscription.PriceCents != 5990 || gil.FirstInvoice == nil || gil.FirstInvoice.AmountCents != 5990 || gil.Tokens == nil {
		t.Fatalf("caminhão pelo link = %+v", gil)
	}
	var kind, leadType string
	_ = env.db.QueryRow(ctx, `SELECT v.kind FROM subscriptions s JOIN vehicles v ON v.id = s.vehicle_id WHERE s.id = $1`,
		gil.Subscription.ID).Scan(&kind)
	_ = env.db.QueryRow(ctx, `SELECT vehicle_type FROM leads WHERE lower(email) = 'gil@tabela.test'`).Scan(&leadType)
	if kind != vehicles.KindTruck || leadType != "caminhao" {
		t.Errorf("veículo = %q, pré-cliente = %q", kind, leadType)
	}
	// A moto pelo link: a dela.
	if r := contractWith("ana@tabela.test", "390.533.447-05", vehicles.KindMotorcycle); r.Subscription.PriceCents != 3990 {
		t.Errorf("moto pelo link = %+v", r.Subscription)
	}

	// No app, o Gil segue na tabela: o catálogo traz os preços por tipo, e o
	// carro que ele pede sai no preço do carro.
	type catalogView struct {
		PlanName       string               `json:"planName"`
		PlanPriceCents int                  `json:"planPriceCents"`
		PlanByKind     *vehicles.KindPrices `json:"planByKind"`
	}
	catalogOf := func(token string) catalogView {
		t.Helper()
		var c catalogView
		_ = json.Unmarshal(env.must(token, http.MethodGet, "/api/catalog", nil, http.StatusOK), &c)
		return c
	}
	token := gil.Tokens.AccessToken
	if c := catalogOf(token); c.PlanByKind == nil || *c.PlanByKind != table || c.PlanPriceCents != 4990 {
		t.Errorf("catálogo do Gil = %+v", c)
	}
	var placed struct {
		Subscription billing.Subscription `json:"subscription"`
	}
	_ = json.Unmarshal(env.must(token, http.MethodPost, "/api/me/trackers", map[string]any{
		"vehicle": map[string]any{"name": "Carro do Gil", "kind": vehicles.KindCar, "plate": "GIL1A11"},
	}, http.StatusCreated), &placed)
	if placed.Subscription.PriceCents != 4990 || placed.Subscription.PlanName != "Plano Mensal" {
		t.Errorf("carro do Gil pelo app = %+v", placed.Subscription)
	}

	// O plano da central vale acima da tabela.
	var gilID string
	_ = env.db.QueryRow(ctx, `SELECT id FROM users WHERE email = 'gil@tabela.test'`).Scan(&gilID)
	env.must(admin, http.MethodPut, "/api/customers/"+gilID+"/plan",
		map[string]any{"planName": "Plano Frota", "priceCents": 4500, "dueDay": 10}, http.StatusOK)
	if c := catalogOf(token); c.PlanByKind != nil || c.PlanName != "Plano Frota" || c.PlanPriceCents != 4500 {
		t.Errorf("catálogo com o plano da central = %+v", c)
	}

	// Sem a tabela, o link volta ao preço normal.
	env.must(admin, http.MethodPatch, "/api/affiliates/"+lia.ID.String(), map[string]any{
		"name": "Lia Tabela", "code": "lia-tabela", "active": true,
	}, http.StatusOK)
	if o := offer("?ref=lia-tabela"); o.PlanByKind != nil {
		t.Errorf("oferta sem a tabela = %+v", o)
	}
}
