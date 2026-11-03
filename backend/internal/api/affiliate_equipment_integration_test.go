package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/contract"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/orders"
)

// O rastreador pelo link do afiliado: só comprado (sem o comodato na oferta,
// e o comodato recusado), só comodato (o comodato sem escolher, e o comprado
// recusado) — também nos pedidos do cliente indicado pelo app. Precisa de
// FARBO_TEST_DATABASE_URL.
func TestAffiliateEquipmentModes(t *testing.T) {
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
	admin := env.login(auth.RoleAdmin + "@leads.test")
	env.must(admin, http.MethodPost, "/api/affiliates", map[string]any{"name": "Errado", "active": true, "equipmentModes": "aluguel"},
		http.StatusBadRequest)
	for code, modes := range map[string]string{"so-comprado": orders.EquipmentSaleOnly, "so-comodato": orders.EquipmentLoanOnly} {
		env.must(admin, http.MethodPost, "/api/affiliates", map[string]any{"name": code, "code": code, "active": true, "equipmentModes": modes},
			http.StatusCreated)
	}

	type offerView struct {
		Loan            *orders.LoanOffer `json:"loan"`
		EquipmentModes  string            `json:"equipmentModes"`
		ContractVersion string            `json:"contractVersion"`
	}
	offer := func(query string) offerView {
		t.Helper()
		var o offerView
		_ = json.Unmarshal(env.must("", http.MethodGet, "/api/public/checkout"+query, nil, http.StatusOK), &o)
		return o
	}
	if o := offer("?ref=so-comprado"); o.Loan != nil || o.EquipmentModes != orders.EquipmentSaleOnly {
		t.Errorf("oferta só comprado = %+v", o)
	}
	o := offer("?ref=so-comodato")
	if o.Loan == nil || o.EquipmentModes != orders.EquipmentLoanOnly {
		t.Errorf("oferta só comodato = %+v", o)
	}

	address := map[string]any{"zipCode": "01305-000", "street": "Rua Augusta", "number": "500", "district": "Consolação",
		"city": "São Paulo", "state": "SP"}
	type result struct {
		Subscription billing.Subscription `json:"subscription"`
		Tokens       *struct {
			AccessToken string `json:"accessToken"`
		} `json:"tokens"`
	}
	body := func(ref, email, document string, extra map[string]any) map[string]any {
		b := map[string]any{
			"name": "Gil Souza", "email": email, "phone": "(11) 95555-0000", "document": document, "password": "senha-forte-1",
			"vehicle": map[string]any{"kind": "CAR", "brand": "Fiat", "model": "Uno", "plate": ""}, "address": address,
			"contractVersion": o.ContractVersion, "accept": true, "ref": ref,
		}
		for k, v := range extra {
			b[k] = v
		}
		return b
	}
	env.must("", http.MethodPost, "/api/public/checkout", body("so-comprado", "a@link.test", "529.982.247-25",
		map[string]any{"equipmentMode": "loan"}), http.StatusBadRequest)
	env.must("", http.MethodPost, "/api/public/checkout", body("so-comodato", "a@link.test", "529.982.247-25",
		map[string]any{"equipmentMode": "sale"}), http.StatusBadRequest)
	// Só comodato, sem escolher: o comodato.
	var r result
	_ = json.Unmarshal(env.must("", http.MethodPost, "/api/public/checkout", body("so-comodato", "a@link.test", "529.982.247-25", nil),
		http.StatusCreated), &r)
	if r.Subscription.EquipmentMode != billing.EquipmentLoan {
		t.Fatalf("só comodato pelo link = %+v", r.Subscription)
	}

	// No app, o cliente do link só comodato segue nele.
	token := r.Tokens.AccessToken
	var catalog struct {
		Loan           *orders.LoanOffer `json:"loan"`
		EquipmentModes string            `json:"equipmentModes"`
	}
	_ = json.Unmarshal(env.must(token, http.MethodGet, "/api/catalog", nil, http.StatusOK), &catalog)
	if catalog.Loan == nil || catalog.EquipmentModes != orders.EquipmentLoanOnly {
		t.Errorf("catálogo do cliente só comodato = %+v", catalog)
	}
	env.must(token, http.MethodPost, "/api/me/trackers", map[string]any{
		"vehicle": map[string]any{"name": "Moto", "plate": "GIL1A11"}, "equipmentMode": "sale",
	}, http.StatusBadRequest)
	var placed struct {
		Subscription billing.Subscription `json:"subscription"`
	}
	_ = json.Unmarshal(env.must(token, http.MethodPost, "/api/me/trackers", map[string]any{
		"vehicle": map[string]any{"name": "Moto", "plate": "GIL1A11"},
	}, http.StatusCreated), &placed)
	if placed.Subscription.EquipmentMode != billing.EquipmentLoan {
		t.Errorf("pedido pelo app do cliente só comodato = %+v", placed.Subscription)
	}
}
