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

// A contratação rápida pela landing: a oferta com os preços do funil (um
// plano só, com o rastreador incluso: o do Insanos MC é com a central); a
// pessoa física cria a conta, aceita o contrato e contrata de uma vez,
// pagando no ato — o rastreador incluso, sem fatura de equipamento (com a
// instalação da Farbo, se quiser), ou o comodato com a garantia de troca —; a empresa, o e-mail que já tem conta, o plano do
// Insanos e o funil fechado são recusados; o próximo veículo parte do plano
// sem a garantia. Precisa de FARBO_TEST_DATABASE_URL.
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
		Enabled   bool `json:"enabled"`
		PlanCents int  `json:"planCents"`
		// O preço normal, que as páginas mostram riscado.
		PlanRegularCents int            `json:"planRegularCents"`
		Tracker          orders.Tracker `json:"tracker"`
		ContractVersion  string         `json:"contractVersion"`
		Loan             *struct {
			SurchargeCents, InstallFeeCents, UninstallFeeCents int
		} `json:"loan"`
		Install *struct {
			PriceCents int `json:"priceCents"`
		} `json:"install"`
	}
	raw := env.must("", http.MethodGet, "/api/public/checkout", nil, http.StatusOK)
	_ = json.Unmarshal(raw, &offer)
	if !offer.Enabled || offer.PlanCents != 6990 || offer.Tracker != (orders.Tracker{Cents: 1200, Months: 10}) ||
		offer.ContractVersion != contract.Version || offer.Loan == nil || offer.Loan.SurchargeCents != 300 ||
		offer.Loan.InstallFeeCents != 15000 || offer.Loan.UninstallFeeCents != 8500 || offer.Install == nil ||
		offer.Install.PriceCents != 15000 || offer.PlanRegularCents != catalogPrices.PlanPriceCents || offer.PlanRegularCents == 0 {
		t.Fatalf("oferta = %+v (comodato %+v)", offer, offer.Loan)
	}
	// O rastreador não tem preço: a oferta não fala em vender nem parcelar.
	for _, gone := range []string{"equipmentCents", "maxInstallments", "equipmentRegularCents"} {
		if strings.Contains(string(raw), gone) {
			t.Errorf("oferta com %q: %s", gone, raw)
		}
	}
	// O preço do Insanos MC não sai na oferta pública.
	if strings.Contains(strings.ToLower(string(raw)), "insanos") {
		t.Errorf("oferta com o Insanos: %s", raw)
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

	// O rastreador incluso, com a instalação da Farbo: tudo hoje, no preço do
	// funil, sem fatura de equipamento; a parte do rastreador já está nos
	// R$ 69,90 (vale para o encerramento antes da 10ª).
	var r result
	_ = json.Unmarshal(env.must("", http.MethodPost, "/api/public/checkout",
		body("lia@site.test", "529.982.247-25", map[string]any{"farboInstall": true}), http.StatusCreated), &r)
	if !r.PayNow || r.Subscription.PlanName != "Plano Mensal" || r.Subscription.PriceCents != 6990 || r.SetupInvoice != nil ||
		r.Subscription.EquipmentMode != billing.EquipmentSale || r.Subscription.TrackerCents != 1200 ||
		r.Subscription.TrackerMonths != 10 || r.Subscription.TrackerAdded ||
		r.InstallInvoice == nil || r.InstallInvoice.AmountCents != 15000 ||
		r.FirstInvoice == nil || r.FirstInvoice.AmountCents != 6990 || r.FirstInvoice.DueDate.String() != "2026-10-13" ||
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
	// O mesmo e-mail de novo, sem a senha dela (a contratação acabou de ser
	// feita, ainda sem pagar): o aviso, e a conta dela fica.
	again := body("lia@site.test", "529.982.247-25", nil)
	again["password"] = "outra-senha-9"
	if status, out := env.do("", http.MethodPost, "/api/public/checkout", again); status != http.StatusConflict ||
		!containsCode(out, "CHECKOUT_UNPAID") {
		t.Fatalf("e-mail repetido = %d %s", status, out)
	}
	env.must(lia, http.MethodGet, "/api/me/account", nil, http.StatusOK)

	// O plano do Insanos MC (das telas antigas) é só com a central: recusado,
	// sem criar conta.
	if status, out := env.do("", http.MethodPost, "/api/public/checkout",
		body("rui@site.test", "390.533.447-05", map[string]any{"insanos": true})); status != http.StatusBadRequest ||
		!strings.Contains(string(out), "central") {
		t.Fatalf("Insanos pelo site = %d %s", status, out)
	}
	_ = env.db.QueryRow(ctx, `SELECT count(*) FROM users WHERE email = 'rui@site.test'`).Scan(&users)
	if users != 0 {
		t.Fatalf("conta criada no Insanos pelo site = %d", users)
	}

	// Comodato: sem o rastreador incluso, a instalação e a mensalidade com a
	// garantia de troca (R$ 69,90 + R$ 3,00).
	env.must("", http.MethodPost, "/api/public/checkout",
		body("rui@site.test", "390.533.447-05", map[string]any{"equipmentMode": "loan", "installments": 3}),
		http.StatusBadRequest)
	_ = json.Unmarshal(env.must("", http.MethodPost, "/api/public/checkout",
		body("rui@site.test", "390.533.447-05", map[string]any{"equipmentMode": "loan", "event": "Encontro Insanos MC"}),
		http.StatusCreated), &r)
	if r.SetupInvoice != nil || r.InstallInvoice == nil || r.InstallInvoice.AmountCents != 15000 ||
		r.FirstInvoice == nil || r.FirstInvoice.AmountCents != 6990+300 ||
		r.Subscription.PlanName != "Plano Mensal"+orders.GuaranteeName || r.Subscription.UninstallFeeCents != 8500 ||
		r.Subscription.EquipmentMode != billing.EquipmentLoan || r.Subscription.TrackerCents != 0 {
		t.Fatalf("comodato = %+v", r)
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
	if catalog.PlanName != "Plano Mensal" || catalog.PlanPriceCents != 6990 {
		t.Errorf("plano do próximo veículo = %+v", catalog)
	}
	// A central põe o Rui no plano do Insanos: o próximo veículo dele sai no
	// plano e no preço que ela definiu.
	var rui struct {
		ID string `json:"id"`
	}
	_ = env.db.QueryRow(ctx, `SELECT id FROM users WHERE email = 'rui@site.test'`).Scan(&rui.ID)
	// No Insanos, R$ 27,90 e, nas 10 primeiras, a parte do rastreador
	// somada (R$ 39,90).
	env.must(env.login(auth.RoleAdmin+"@leads.test"), http.MethodPut, "/api/customers/"+rui.ID+"/plan",
		map[string]any{"planName": "Especial Insanos MC", "priceCents": 2790, "dueDay": 10}, http.StatusOK)
	var insanos struct {
		PlanName       string         `json:"planName"`
		PlanPriceCents int            `json:"planPriceCents"`
		Tracker        orders.Tracker `json:"tracker"`
	}
	_ = json.Unmarshal(env.must(r.Tokens.AccessToken, http.MethodGet, "/api/catalog", nil, http.StatusOK), &insanos)
	if insanos.PlanName != "Especial Insanos MC" || insanos.PlanPriceCents != 2790 ||
		insanos.Tracker != (orders.Tracker{Cents: 1200, Months: 10, Added: true}) {
		t.Errorf("plano do próximo veículo, no Insanos pela central = %+v", insanos)
	}

	// No app, a pessoa física paga o mesmo que no site: a central muda o
	// preço do site e o próximo veículo da Lia acompanha.
	type prices struct {
		PlanName       string         `json:"planName"`
		PlanPriceCents int            `json:"planPriceCents"`
		Tracker        orders.Tracker `json:"tracker"`
	}
	pricesOf := func(token string) prices {
		t.Helper()
		var p prices
		_ = json.Unmarshal(env.must(token, http.MethodGet, "/api/catalog", nil, http.StatusOK), &p)
		return p
	}
	if _, err := env.db.Exec(ctx, `UPDATE order_settings SET checkout_plan_cents = 2990`); err != nil {
		t.Fatal(err)
	}
	if p := pricesOf(lia); p.PlanName != "Plano Mensal" || p.PlanPriceCents != 2990 || p.Tracker.Added {
		t.Errorf("app da pessoa física = %+v", p)
	}
	// A empresa (CNPJ) segue no catálogo.
	admin := env.login(auth.RoleAdmin + "@leads.test")
	env.must(admin, http.MethodPost, "/api/customers", map[string]any{
		"name": "Frota Ltda", "email": "frota@site.test", "phone": "(11) 94444-0000", "document": "11.222.333/0001-81",
		"password": userPassword,
	}, http.StatusCreated)
	if p := pricesOf(env.login("frota@site.test")); p.PlanPriceCents != catalogPrices.PlanPriceCents || p.Tracker.Cents != 1200 {
		t.Errorf("app da empresa = %+v", p)
	}

	// Com o funil fechado pela central, não contrata; no app, a pessoa física
	// fica com o plano que já tem.
	if _, err := env.db.Exec(ctx, `UPDATE order_settings SET checkout_enabled = FALSE`); err != nil {
		t.Fatal(err)
	}
	if status, out := env.do("", http.MethodPost, "/api/public/checkout", body("bia@site.test", "529.982.247-25", nil)); status != http.StatusBadRequest ||
		!strings.Contains(string(out), "fechada") {
		t.Fatalf("funil fechado = %d %s", status, out)
	}
	if p := pricesOf(lia); p.PlanPriceCents != 6990 || p.Tracker.Added {
		t.Errorf("app com o funil fechado = %+v", p)
	}
}
