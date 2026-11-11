package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/affiliates"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/contract"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/orders"
)

// O semestral e o anual no comodato e pelo app: o representante só de
// comodato liga os dois; a oferta mostra o valor no comodato (o do período
// mais a garantia de troca de cada mês); a contratação semestral sai assim,
// com a instalação com bloqueio. Pelo app, o cliente indicado por ele pede
// outro veículo no anual (sem a promoção, que é mensal); o cliente sem o
// representante não tem o período; o veículo novo no mensal não herda o valor
// do período. Cancelado com o período pago, o comodato só vira desinstalação
// no fim do período (ou nem isso, dispensada no cancelamento). Precisa de
// FARBO_TEST_DATABASE_URL.
func TestPeriodPlansLoan(t *testing.T) {
	var bs *billing.Service
	env, _ := newLeadsEnv(t, func(d *Deps) {
		bs = d.Billing
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
	clock := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	bs.SetClock(func() time.Time { return clock })
	admin := env.login(auth.RoleAdmin + "@leads.test")

	// Só de comodato, com o semestral e o anual (antes recusado) e os preços
	// próprios cobrados pela Farbo: garantia de R$ 3/mês, desinstalação R$ 85.
	var carla affiliates.Affiliate
	_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/affiliates", map[string]any{
		"name": "Carla Comodato", "code": "carla", "active": true, "equipmentModes": "loan",
		"installPrices": map[string]any{"mode": "custom", "installCents": 10000, "uninstallCents": 8500, "guaranteeCents": 300,
			"blockingCents": 18000},
		"recurrence": map[string]any{"enabled": true, "semiannualCents": 20000, "annualCents": 40000},
	}, http.StatusCreated), &carla)
	if !carla.Recurrence.Enabled {
		t.Fatalf("representante = %+v", carla.Recurrence)
	}
	type periodView struct {
		Months    int `json:"months"`
		Cents     int `json:"cents"`
		LoanCents int `json:"loanCents"`
	}
	var o struct {
		Periods         []periodView `json:"periods"`
		ContractVersion string       `json:"contractVersion"`
	}
	_ = json.Unmarshal(env.must("", http.MethodGet, "/api/public/checkout?ref=carla", nil, http.StatusOK), &o)
	if len(o.Periods) != 2 || o.Periods[0] != (periodView{6, 20000, 20000 + 6*300}) || o.Periods[1] != (periodView{12, 40000, 40000 + 12*300}) {
		t.Fatalf("oferta do link só de comodato = %+v", o.Periods)
	}

	address := map[string]any{"zipCode": "01305-000", "street": "Rua Augusta", "number": "500", "district": "Consolação",
		"city": "São Paulo", "state": "SP"}
	type checkoutResult struct {
		Subscription   billing.Subscription `json:"subscription"`
		FirstInvoice   *billing.Invoice     `json:"firstInvoice"`
		InstallInvoice *billing.Invoice     `json:"installInvoice"`
		Tokens         *struct {
			AccessToken string `json:"accessToken"`
		} `json:"tokens"`
	}
	checkout := func(ref, email, document string, extra map[string]any) checkoutResult {
		t.Helper()
		body := map[string]any{
			"name": "Gil Souza", "email": email, "phone": "(11) 95555-0000", "document": document, "password": "senha-forte-1",
			"vehicle": map[string]any{"kind": "CAR", "brand": "Fiat", "model": "Uno", "plate": ""}, "address": address,
			"contractVersion": o.ContractVersion, "accept": true, "ref": ref,
		}
		for k, v := range extra {
			body[k] = v
		}
		var r checkoutResult
		_ = json.Unmarshal(env.must("", http.MethodPost, "/api/public/checkout", body, http.StatusCreated), &r)
		return r
	}
	paid := func(customer string) {
		t.Helper()
		if _, err := env.db.Exec(ctx, `UPDATE invoices SET status = 'PAID', paid_at = NOW(), paid_via = 'MANUAL'
			WHERE customer_id = $1 AND status = 'OPEN'`, customer); err != nil {
			t.Fatal(err)
		}
	}

	// O semestral no comodato: o valor com a garantia de cada mês, e a
	// instalação com bloqueio na fatura.
	gil := checkout("carla", "gil@carla.test", "529.982.247-25", map[string]any{
		"equipmentMode": "loan", "periodMonths": 6, "installBlocking": true,
	})
	semester := gil.Subscription
	if semester.PeriodMonths != 6 || semester.PriceCents != 20000+6*300 || semester.PlanName != "Plano Semestral"+orders.GuaranteeName ||
		semester.EquipmentMode != billing.EquipmentLoan || semester.UninstallFeeCents != 8500 || semester.TrackerCents != 0 ||
		gil.FirstInvoice == nil || gil.FirstInvoice.AmountCents != 21800 || gil.InstallInvoice == nil || gil.InstallInvoice.AmountCents != 18000 {
		t.Fatalf("semestral no comodato = %+v / %+v / %+v", semester, gil.FirstInvoice, gil.InstallInvoice)
	}
	paid(semester.CustomerID.String())

	// Pelo app: o cliente indicado vê o semestral e o anual (no comodato).
	token := gil.Tokens.AccessToken
	var catalog struct {
		PlanPriceCents int          `json:"planPriceCents"`
		Periods        []periodView `json:"periods"`
	}
	_ = json.Unmarshal(env.must(token, http.MethodGet, "/api/catalog", nil, http.StatusOK), &catalog)
	if len(catalog.Periods) != 2 || catalog.Periods[1] != (periodView{12, 40000, 43600}) {
		t.Fatalf("catálogo do cliente indicado = %+v", catalog.Periods)
	}
	// O veículo novo no mensal não herda o valor do período.
	if catalog.PlanPriceCents >= 20000 {
		t.Errorf("mensalidade do veículo novo = %d", catalog.PlanPriceCents)
	}
	appOrder := func(token string, extra map[string]any, want int) billing.Subscription {
		t.Helper()
		body := map[string]any{"vehicle": map[string]any{"name": "Moto do Gil", "kind": "MOTORCYCLE"}}
		for k, v := range extra {
			body[k] = v
		}
		var r struct {
			Subscription billing.Subscription `json:"subscription"`
		}
		_ = json.Unmarshal(env.must(token, http.MethodPost, "/api/me/trackers", body, want), &r)
		return r.Subscription
	}
	// Com a promoção (que é mensal), não.
	appOrder(token, map[string]any{"equipmentMode": "loan", "periodMonths": 12, "launchPromo": true}, http.StatusBadRequest)
	year := appOrder(token, map[string]any{"equipmentMode": "loan", "periodMonths": 12, "installBlocking": true}, http.StatusCreated)
	if year.PeriodMonths != 12 || year.PriceCents != 43600 || year.PlanName != "Plano Anual"+orders.GuaranteeName ||
		year.EquipmentMode != billing.EquipmentLoan {
		t.Fatalf("anual pelo app = %+v", year)
	}
	var blocking bool
	_ = env.db.QueryRow(ctx, `SELECT install_blocking FROM subscriptions WHERE id = $1`, year.ID).Scan(&blocking)
	if !blocking {
		t.Errorf("anual pelo app sem a instalação com bloqueio")
	}
	paid(semester.CustomerID.String())

	// Sem o representante (o site com o semestral desligado), o app não tem o período.
	site := checkout("", "lia@site.test", "390.533.447-05", nil)
	paid(site.Subscription.CustomerID.String())
	var siteCatalog struct {
		Periods []periodView `json:"periods"`
	}
	_ = json.Unmarshal(env.must(site.Tokens.AccessToken, http.MethodGet, "/api/catalog", nil, http.StatusOK), &siteCatalog)
	if len(siteCatalog.Periods) != 0 {
		t.Fatalf("catálogo sem o representante = %+v", siteCatalog.Periods)
	}
	appOrder(site.Tokens.AccessToken, map[string]any{"periodMonths": 6}, http.StatusBadRequest)

	// Cancelados com o período pago: ficam até o fim, sem a desinstalação
	// agora; a do anual, dispensada no cancelamento.
	var canceled billing.Subscription
	_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/subscriptions/"+semester.ID.String()+"/cancel", nil, http.StatusOK), &canceled)
	if canceled.Status != billing.SubscriptionActive || canceled.EndsOn == nil {
		t.Fatalf("semestral cancelado = %+v", canceled)
	}
	_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/subscriptions/"+year.ID.String()+"/cancel",
		map[string]any{"waiveUninstall": true}, http.StatusOK), &canceled)
	if canceled.Status != billing.SubscriptionActive || canceled.EndsOn == nil || canceled.UninstallFeeCents != 0 {
		t.Fatalf("anual cancelado = %+v", canceled)
	}
	uninstalls := func() (n, cents int) {
		t.Helper()
		_ = env.db.QueryRow(ctx, `SELECT count(*), COALESCE(sum(amount_cents), 0) FROM invoices
			WHERE customer_id = $1 AND description LIKE 'Desinstalação do rastreador (comodato)%'`, semester.CustomerID).Scan(&n, &cents)
		return n, cents
	}
	if n, _ := uninstalls(); n != 0 {
		t.Fatalf("desinstalação antes do fim = %d", n)
	}
	// Depois do fim dos dois: encerrados; a desinstalação só do semestral.
	clock = time.Date(2028, 1, 15, 12, 0, 0, 0, time.UTC)
	bs.GenerateInvoices(ctx)
	for _, id := range []string{semester.ID.String(), year.ID.String()} {
		var status string
		_ = env.db.QueryRow(ctx, `SELECT status FROM subscriptions WHERE id = $1`, id).Scan(&status)
		if status != billing.SubscriptionCanceled {
			t.Errorf("assinatura %s no fim do período = %s", id, status)
		}
	}
	if n, cents := uninstalls(); n != 1 || cents != 8500 {
		t.Errorf("desinstalação no fim = %d (%d)", n, cents)
	}
	// Uma segunda passada não lança de novo.
	bs.GenerateInvoices(ctx)
	if n, _ := uninstalls(); n != 1 {
		t.Errorf("desinstalação de novo = %d", n)
	}
}
