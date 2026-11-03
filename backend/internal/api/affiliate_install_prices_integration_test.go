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
)

// A instalação e a desinstalação pelo link do afiliado: com preços próprios,
// a Farbo cobra os dele; com o representante, a oferta mostra os preços dele
// e nada disso entra nas faturas (ele recebe direto do cliente), nem no
// comodato, nem na instalação do rastreador comprado, nem no pedido que o
// cliente faz depois pelo app. Precisa de FARBO_TEST_DATABASE_URL.
func TestAffiliateInstallPrices(t *testing.T) {
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
		"name": "Errado", "active": true, "installPrices": map[string]any{"mode": "meio", "installCents": 100, "uninstallCents": 100},
	}, http.StatusBadRequest)
	create := func(name, code string, install orders.InstallPrices) affiliates.Affiliate {
		t.Helper()
		var a affiliates.Affiliate
		_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/affiliates", map[string]any{
			"name": name, "code": code, "active": true, "installPrices": install,
		}, http.StatusCreated), &a)
		if a.InstallPrices == nil || *a.InstallPrices != install {
			t.Fatalf("afiliado %s = %+v", code, a)
		}
		return a
	}
	create("Rui Representante", "rui-rep", orders.InstallPrices{Mode: orders.InstallDirect, InstallCents: 12000, UninstallCents: 8000})
	create("Loja Parceira", "loja", orders.InstallPrices{Mode: orders.InstallCustom, InstallCents: 9000, UninstallCents: 5000})

	type offerView struct {
		Install *struct {
			Enabled    bool `json:"enabled"`
			PriceCents int  `json:"priceCents"`
		} `json:"install"`
		Loan *struct {
			InstallFeeCents   int `json:"installFeeCents"`
			UninstallFeeCents int `json:"uninstallFeeCents"`
		} `json:"loan"`
		InstallDirect   bool   `json:"installDirect"`
		ContractVersion string `json:"contractVersion"`
	}
	offer := func(query string) offerView {
		t.Helper()
		var o offerView
		_ = json.Unmarshal(env.must("", http.MethodGet, "/api/public/checkout"+query, nil, http.StatusOK), &o)
		return o
	}
	if o := offer(""); o.InstallDirect || o.Install == nil || o.Install.PriceCents != 15000 || o.Loan == nil || o.Loan.UninstallFeeCents != 8500 {
		t.Fatalf("oferta sem link = %+v / %+v / %+v", o, o.Install, o.Loan)
	}
	o := offer("?ref=rui-rep")
	if !o.InstallDirect || o.Install == nil || o.Install.PriceCents != 12000 || o.Loan == nil || o.Loan.InstallFeeCents != 12000 ||
		o.Loan.UninstallFeeCents != 8000 {
		t.Fatalf("oferta do representante = %+v / %+v / %+v", o, o.Install, o.Loan)
	}

	address := map[string]any{"zipCode": "01305-000", "street": "Rua Augusta", "number": "500", "district": "Consolação",
		"city": "São Paulo", "state": "SP"}
	type result struct {
		Subscription   billing.Subscription `json:"subscription"`
		InstallInvoice *billing.Invoice     `json:"installInvoice"`
		Tokens         *struct {
			AccessToken string `json:"accessToken"`
		} `json:"tokens"`
	}
	contractWith := func(ref, email, document string, extra map[string]any) result {
		t.Helper()
		body := map[string]any{
			"name": "Gil Souza", "email": email, "phone": "(11) 95555-0000", "document": document, "password": "senha-forte-1",
			"vehicle": map[string]any{"kind": "CAR", "brand": "Fiat", "model": "Uno", "plate": ""}, "address": address,
			"contractVersion": o.ContractVersion, "accept": true, "ref": ref,
		}
		for k, v := range extra {
			body[k] = v
		}
		var r result
		_ = json.Unmarshal(env.must("", http.MethodPost, "/api/public/checkout", body, http.StatusCreated), &r)
		return r
	}
	farboInstall := func(subscription string) bool {
		t.Helper()
		var on bool
		_ = env.db.QueryRow(ctx, `SELECT farbo_install FROM subscriptions WHERE id = $1`, subscription).Scan(&on)
		return on
	}

	// Comodato pelo representante: sem a instalação nem a desinstalação na
	// plataforma, e a instalação na agenda da central.
	loan := contractWith("rui-rep", "gil@rep.test", "529.982.247-25", map[string]any{"equipmentMode": "loan"})
	if loan.InstallInvoice != nil || loan.Subscription.UninstallFeeCents != 0 || !farboInstall(loan.Subscription.ID.String()) {
		t.Errorf("comodato pelo representante = %+v (instalação %+v)", loan.Subscription, loan.InstallInvoice)
	}
	// O rastreador comprado com a instalação dele: sem a fatura da instalação.
	sale := contractWith("rui-rep", "ana@rep.test", "390.533.447-05", map[string]any{"farboInstall": true})
	if sale.InstallInvoice != nil || !farboInstall(sale.Subscription.ID.String()) {
		t.Errorf("comprado pelo representante = %+v", sale.InstallInvoice)
	}
	// Com preços próprios, a Farbo cobra os dele.
	custom := contractWith("loja", "bia@loja.test", "111.444.777-35", map[string]any{"equipmentMode": "loan"})
	if custom.InstallInvoice == nil || custom.InstallInvoice.AmountCents != 9000 || custom.Subscription.UninstallFeeCents != 5000 {
		t.Errorf("comodato pela loja = %+v (instalação %+v)", custom.Subscription, custom.InstallInvoice)
	}

	// No app, a cliente do representante segue com ele: o catálogo mostra
	// os preços dele, e o rastreador comprado com a instalação sai sem ela.
	var catalog struct {
		Install *struct {
			PriceCents int `json:"priceCents"`
		} `json:"install"`
		InstallDirect bool `json:"installDirect"`
	}
	token := sale.Tokens.AccessToken
	_ = json.Unmarshal(env.must(token, http.MethodGet, "/api/catalog", nil, http.StatusOK), &catalog)
	if !catalog.InstallDirect || catalog.Install == nil || catalog.Install.PriceCents != 12000 {
		t.Errorf("catálogo da cliente do representante = %+v / %+v", catalog, catalog.Install)
	}
	var placed struct {
		Subscription   billing.Subscription `json:"subscription"`
		InstallInvoice *billing.Invoice     `json:"installInvoice"`
	}
	_ = json.Unmarshal(env.must(token, http.MethodPost, "/api/me/trackers", map[string]any{
		"vehicle": map[string]any{"name": "Carro da Ana", "plate": "ANA1A11"}, "farboInstall": true,
	}, http.StatusCreated), &placed)
	if placed.InstallInvoice != nil || !farboInstall(placed.Subscription.ID.String()) {
		t.Errorf("pedido pelo app = %+v (instalação %+v)", placed.Subscription, placed.InstallInvoice)
	}
}
