package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/contract"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/orders"
)

// A contratação rápida pela landing: a oferta com os preços do funil; a
// pessoa física cria a conta, aceita o contrato e contrata de uma vez,
// pagando no ato — o rastreador comprado (com a instalação da Farbo, se
// quiser) ou o comodato com a garantia de troca —; a empresa, o e-mail que já
// tem conta e o funil fechado são recusados; o próximo veículo parte do
// plano sem a garantia. Precisa de FARBO_TEST_DATABASE_URL.
func TestCheckoutFunnel(t *testing.T) {
	var bs *billing.Service
	var catalogPrices config.Catalog
	env, _ := newLeadsEnv(t, func(d *Deps) {
		bs = d.Billing
		d.Config.Catalog.SetupDueDays = 0
		d.Config.Catalog.LaunchPromo.InsanosPlanCents = 3990
		catalogPrices = d.Config.Catalog
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
	bs.SetClock(func() time.Time { return time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC) })

	var offer struct {
		Enabled         bool   `json:"enabled"`
		PlanCents       int    `json:"planCents"`
		InsanosPlanName string `json:"insanosPlanName"`
		InsanosCents    int    `json:"insanosCents"`
		EquipmentCents  int    `json:"equipmentCents"`
		// Os preços normais, que as páginas mostram riscados.
		PlanRegularCents      int    `json:"planRegularCents"`
		InsanosRegularCents   int    `json:"insanosRegularCents"`
		EquipmentRegularCents int    `json:"equipmentRegularCents"`
		ContractVersion       string `json:"contractVersion"`
		Loan                  *struct {
			SurchargeCents, InstallFeeCents, UninstallFeeCents int
		} `json:"loan"`
		Install *struct {
			PriceCents int `json:"priceCents"`
		} `json:"install"`
	}
	_ = json.Unmarshal(env.must("", http.MethodGet, "/api/public/checkout", nil, http.StatusOK), &offer)
	if !offer.Enabled || offer.PlanCents != 3490 || offer.InsanosCents != 2790 || offer.EquipmentCents != 12000 ||
		offer.ContractVersion != contract.Version || offer.Loan == nil || offer.Loan.SurchargeCents != 300 ||
		offer.Loan.InstallFeeCents != 15000 || offer.Loan.UninstallFeeCents != 8500 || offer.Install == nil ||
		offer.Install.PriceCents != 15000 || offer.InsanosPlanName != "Especial Insanos MC" ||
		offer.PlanRegularCents != catalogPrices.PlanPriceCents || offer.PlanRegularCents == 0 || offer.InsanosRegularCents != 3990 ||
		offer.EquipmentRegularCents != catalogPrices.EquipmentPriceCents || offer.EquipmentRegularCents == 0 {
		t.Fatalf("oferta = %+v (comodato %+v)", offer, offer.Loan)
	}

	address := map[string]any{"zipCode": "01305-000", "street": "Rua Augusta", "number": "500", "district": "Consolação",
		"city": "São Paulo", "state": "SP"}
	body := func(email, document string, extra map[string]any) map[string]any {
		b := map[string]any{
			"name": "Lia Souza", "email": email, "phone": "(11) 95555-0000", "document": document, "password": "senha-forte-1",
			"vehicle": map[string]any{"kind": "CAR", "brand": "Fiat", "model": "Uno", "plate": ""},
			"address": address, "contractVersion": offer.ContractVersion, "accept": true,
		}
		for k, v := range extra {
			b[k] = v
		}
		return b
	}
	type result struct {
		Subscription   billing.Subscription `json:"subscription"`
		PayNow         bool                 `json:"payNow"`
		SetupInvoice   *billing.Invoice     `json:"setupInvoice"`
		InstallInvoice *billing.Invoice     `json:"installInvoice"`
		FirstInvoice   *billing.Invoice     `json:"firstInvoice"`
		Tokens         *struct {
			AccessToken string `json:"accessToken"`
		} `json:"tokens"`
	}
	// Empresa, sem o aceite e CPF errado: recusados sem criar conta.
	env.must("", http.MethodPost, "/api/public/checkout", body("lia@site.test", "11.222.333/0001-81", nil), http.StatusBadRequest)
	env.must("", http.MethodPost, "/api/public/checkout", body("lia@site.test", "529.982.247-25", map[string]any{"accept": false}),
		http.StatusBadRequest)
	env.must("", http.MethodPost, "/api/public/checkout", body("lia@site.test", "111.111.111-11", nil), http.StatusBadRequest)
	var users int
	_ = env.db.QueryRow(ctx, `SELECT count(*) FROM users WHERE email = 'lia@site.test'`).Scan(&users)
	if users != 0 {
		t.Fatalf("contas criadas nas recusas = %d", users)
	}

	// Comprado à vista, com a instalação da Farbo: tudo hoje, no preço do funil.
	var r result
	_ = json.Unmarshal(env.must("", http.MethodPost, "/api/public/checkout",
		body("lia@site.test", "529.982.247-25", map[string]any{"farboInstall": true}), http.StatusCreated), &r)
	if !r.PayNow || r.Subscription.PlanName != "Plano Mensal" || r.Subscription.PriceCents != 3490 ||
		r.SetupInvoice == nil || r.SetupInvoice.AmountCents != 12000 || r.SetupInvoice.DueDate.String() != "2026-10-13" ||
		r.InstallInvoice == nil || r.InstallInvoice.AmountCents != 15000 ||
		r.FirstInvoice == nil || r.FirstInvoice.AmountCents != 3490 || r.FirstInvoice.DueDate.String() != "2026-10-13" ||
		r.Tokens == nil || r.Tokens.AccessToken == "" {
		t.Fatalf("contratação = %+v", r)
	}
	var accepted, converted int
	_ = env.db.QueryRow(ctx, `SELECT count(*) FROM contract_acceptances a JOIN users u ON u.id = a.user_id
		WHERE u.email = 'lia@site.test' AND a.version = $1`, contract.Version).Scan(&accepted)
	_ = env.db.QueryRow(ctx, `SELECT count(*) FROM leads WHERE lower(email) = 'lia@site.test' AND customer_id IS NOT NULL`).Scan(&converted)
	if accepted != 1 || converted != 1 {
		t.Errorf("aceite = %d, pré-cliente convertido = %d", accepted, converted)
	}
	// O app já abre com a sessão da contratação.
	env.must(r.Tokens.AccessToken, http.MethodGet, "/api/me/account", nil, http.StatusOK)
	lia := r.Tokens.AccessToken
	// O mesmo e-mail de novo: entra no app.
	if status, out := env.do("", http.MethodPost, "/api/public/checkout", body("lia@site.test", "529.982.247-25", nil)); status != http.StatusConflict ||
		!containsCode(out, "EMAIL_TAKEN") {
		t.Fatalf("e-mail repetido = %d %s", status, out)
	}

	// Insanos MC em comodato: sem o rastreador, a instalação e a mensalidade
	// do Insanos com a garantia de troca (R$ 27,90 + R$ 3,00).
	env.must("", http.MethodPost, "/api/public/checkout",
		body("rui@site.test", "390.533.447-05", map[string]any{"insanos": true, "equipmentMode": "loan", "installments": 3}),
		http.StatusBadRequest)
	_ = json.Unmarshal(env.must("", http.MethodPost, "/api/public/checkout",
		body("rui@site.test", "390.533.447-05", map[string]any{"insanos": true, "equipmentMode": "loan", "event": "Encontro Insanos MC"}),
		http.StatusCreated), &r)
	if r.SetupInvoice != nil || r.InstallInvoice == nil || r.InstallInvoice.AmountCents != 15000 ||
		r.FirstInvoice == nil || r.FirstInvoice.AmountCents != 2790+300 ||
		r.Subscription.PlanName != "Especial Insanos MC"+orders.GuaranteeName || r.Subscription.UninstallFeeCents != 8500 ||
		r.Subscription.EquipmentMode != billing.EquipmentLoan {
		t.Fatalf("comodato do Insanos = %+v", r)
	}
	var guarantee int
	_ = env.db.QueryRow(ctx, `SELECT guarantee_cents FROM subscriptions WHERE id = $1`, r.Subscription.ID).Scan(&guarantee)
	if guarantee != 300 {
		t.Errorf("garantia na assinatura = %d", guarantee)
	}
	// Veio do QR Code de um evento: o pré-cliente diz qual.
	var message string
	_ = env.db.QueryRow(ctx, `SELECT message FROM leads WHERE lower(email) = 'rui@site.test'`).Scan(&message)
	if !strings.Contains(message, "evento encontro-insanos-mc") {
		t.Errorf("mensagem do pré-cliente = %q", message)
	}
	// O próximo veículo dele parte do plano, sem a garantia (ela é do comodato).
	var catalog struct {
		PlanName       string `json:"planName"`
		PlanPriceCents int    `json:"planPriceCents"`
	}
	_ = json.Unmarshal(env.must(r.Tokens.AccessToken, http.MethodGet, "/api/catalog", nil, http.StatusOK), &catalog)
	if catalog.PlanName != "Especial Insanos MC" || catalog.PlanPriceCents != 2790 {
		t.Errorf("plano do próximo veículo = %+v", catalog)
	}

	// No app, a pessoa física paga o mesmo que no site: a central muda o
	// preço do site e o próximo veículo da Lia acompanha (o rastreador também).
	type prices struct {
		PlanName            string `json:"planName"`
		PlanPriceCents      int    `json:"planPriceCents"`
		EquipmentPriceCents int    `json:"equipmentPriceCents"`
	}
	pricesOf := func(token string) prices {
		t.Helper()
		var p prices
		_ = json.Unmarshal(env.must(token, http.MethodGet, "/api/catalog", nil, http.StatusOK), &p)
		return p
	}
	if _, err := env.db.Exec(ctx, `UPDATE order_settings SET checkout_plan_cents = 2990, checkout_equipment_cents = 9900`); err != nil {
		t.Fatal(err)
	}
	if p := pricesOf(lia); p.PlanName != "Plano Mensal" || p.PlanPriceCents != 2990 || p.EquipmentPriceCents != 9900 {
		t.Errorf("app da pessoa física = %+v", p)
	}
	// A empresa (CNPJ) segue no catálogo.
	admin := env.login(auth.RoleAdmin + "@leads.test")
	env.must(admin, http.MethodPost, "/api/customers", map[string]any{
		"name": "Frota Ltda", "email": "frota@site.test", "phone": "(11) 94444-0000", "document": "11.222.333/0001-81",
		"password": userPassword,
	}, http.StatusCreated)
	if p := pricesOf(env.login("frota@site.test")); p.PlanPriceCents != catalogPrices.PlanPriceCents ||
		p.EquipmentPriceCents != catalogPrices.EquipmentPriceCents {
		t.Errorf("app da empresa = %+v", p)
	}

	// Com o funil fechado pela central, não contrata; no app, a pessoa física
	// fica com o plano que já tem e o rastreador do catálogo.
	if _, err := env.db.Exec(ctx, `UPDATE order_settings SET checkout_enabled = FALSE`); err != nil {
		t.Fatal(err)
	}
	if status, out := env.do("", http.MethodPost, "/api/public/checkout", body("bia@site.test", "529.982.247-25", nil)); status != http.StatusBadRequest ||
		!strings.Contains(string(out), "fechada") {
		t.Fatalf("funil fechado = %d %s", status, out)
	}
	if p := pricesOf(lia); p.PlanPriceCents != 3490 || p.EquipmentPriceCents != catalogPrices.EquipmentPriceCents {
		t.Errorf("app com o funil fechado = %+v", p)
	}
}
