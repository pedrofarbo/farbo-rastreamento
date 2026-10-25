package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/orders"
)

// O pedido da pessoa física é pago no ato: a 1ª mensalidade, o rastreador à
// vista (parcelado, a 1ª parcela vem na 1ª mensalidade) ou, no comodato, a
// instalação — tudo no dia da contratação, sem proporcional — e a 2ª
// mensalidade pelo menos um mês depois, no dia da conta. O comodato é a
// escolha do cliente quando a central oferece (com a mensalidade e as taxas
// dela); com o plano definido pela central, vale o do plano. Precisa de
// FARBO_TEST_DATABASE_URL.
func TestUpfrontIndividualOrder(t *testing.T) {
	var bs *billing.Service
	env, _ := newLeadsEnv(t, func(d *Deps) {
		bs = d.Billing
		d.Config.Catalog.SetupDueDays = 0
		d.Orders = orders.NewService(d.DB, d.Billing, d.Vehicles, d.Config.Catalog, d.Log)
	})
	ctx := context.Background()
	bs.SetClock(func() time.Time { return time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC) })

	admin := env.login(auth.RoleAdmin + "@leads.test")
	var lia struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/customers", map[string]any{
		"name": "Lia Souza", "email": "lia@noato.test", "phone": "(11) 95555-0000", "document": "529.982.247-25",
		"password": userPassword,
	}, http.StatusCreated), &lia)
	token := env.login("lia@noato.test")
	env.must(token, http.MethodPut, "/api/me/address", map[string]any{
		"zipCode": "01305-000", "street": "Rua Augusta", "number": "500", "district": "Consolação", "city": "São Paulo", "state": "SP",
	}, http.StatusOK)

	type result struct {
		Subscription   billing.Subscription `json:"subscription"`
		ChargeDate     string               `json:"chargeDate"`
		PayNow         bool                 `json:"payNow"`
		SetupInvoice   *billing.Invoice     `json:"setupInvoice"`
		InstallInvoice *billing.Invoice     `json:"installInvoice"`
		ProrataInvoice *billing.Invoice     `json:"prorataInvoice"`
		FirstInvoice   *billing.Invoice     `json:"firstInvoice"`
	}
	order := func(plate string, body map[string]any, want int) result {
		t.Helper()
		body["vehicle"] = map[string]any{"name": "Moto " + plate, "plate": plate}
		var r result
		_ = json.Unmarshal(env.must(token, http.MethodPost, "/api/me/trackers", body, want), &r)
		return r
	}
	nextDue := func(id string) string {
		t.Helper()
		var next time.Time
		_ = env.db.QueryRow(ctx, `SELECT next_due_date FROM subscriptions WHERE id = $1`, id).Scan(&next)
		return next.Format(time.DateOnly)
	}
	type catalog struct {
		PayNow     bool   `json:"payNow"`
		ChargeDate string `json:"chargeDate"`
		PlanTerms  bool   `json:"planTerms"`
		Install    *struct {
			PriceCents int `json:"priceCents"`
		} `json:"install"`
		Loan *struct {
			SurchargeCents  int `json:"surchargeCents"`
			InstallFeeCents int `json:"installFeeCents"`
		} `json:"loan"`
	}
	readCatalog := func() catalog {
		t.Helper()
		var c catalog
		_ = json.Unmarshal(env.must(token, http.MethodGet, "/api/catalog", nil, http.StatusOK), &c)
		return c
	}
	// O comodato começa desligado neste teste (a central liga mais abaixo).
	install := map[string]any{"enabled": true, "priceCents": 15000}
	env.must(admin, http.MethodPut, "/api/order-options", map[string]any{"loan": map[string]any{"enabled": false}, "install": install,
		"checkout": map[string]any{"enabled": true, "planCents": 3490, "insanosCents": 2790, "equipmentCents": 12000}}, http.StatusOK)
	if c := readCatalog(); !c.PayNow || c.ChargeDate != "2026-10-13" || c.Loan != nil || c.PlanTerms ||
		c.Install == nil || c.Install.PriceCents != 15000 {
		t.Fatalf("catálogo sem comodato = %+v", c)
	}

	// À vista: o rastreador e a 1ª mensalidade hoje, sem proporcional; a 2ª
	// em 10/12 (pelo menos um mês depois de 13/10, no dia da conta).
	r := order("NOA1A11", map[string]any{}, http.StatusCreated)
	if !r.PayNow || r.ChargeDate != "2026-10-13" || r.ProrataInvoice != nil ||
		r.SetupInvoice == nil || r.SetupInvoice.DueDate.String() != "2026-10-13" || r.SetupInvoice.AmountCents != 15000 ||
		r.FirstInvoice == nil || r.FirstInvoice.DueDate.String() != "2026-10-13" || r.FirstInvoice.AmountCents != 6990 {
		t.Fatalf("à vista = %+v", r)
	}
	if got := nextDue(r.Subscription.ID.String()); got != "2026-12-10" {
		t.Errorf("2ª mensalidade em %s, quer 2026-12-10", got)
	}
	if r.InstallInvoice != nil {
		t.Errorf("por conta própria, sem a instalação: %+v", r.InstallInvoice)
	}
	// Parcelado, com a instalação pelos instaladores da Farbo (R$ 150,00):
	// nada do equipamento à parte; a 1ª parcela na 1ª mensalidade e a
	// instalação, hoje.
	r = order("NOA2B22", map[string]any{"installments": 10, "farboInstall": true}, http.StatusCreated)
	if !r.PayNow || r.SetupInvoice != nil || r.FirstInvoice == nil || r.FirstInvoice.DueDate.String() != "2026-10-13" ||
		r.FirstInvoice.AmountCents != 6990+1500 || r.InstallInvoice == nil || r.InstallInvoice.AmountCents != 15000 ||
		r.InstallInvoice.DueDate.String() != "2026-10-13" {
		t.Fatalf("parcelado com a instalação = %+v", r)
	}
	farbo := func(id string) bool {
		t.Helper()
		var v bool
		_ = env.db.QueryRow(ctx, `SELECT farbo_install FROM subscriptions WHERE id = $1`, id).Scan(&v)
		return v
	}
	if !farbo(r.Subscription.ID.String()) {
		t.Error("a instalação da Farbo não ficou marcada na assinatura")
	}

	// O comodato: desligado, não dá; ligado pela central, a mensalidade e as
	// taxas dele (a promoção de pré-lançamento não vale).
	order("NOA3C33", map[string]any{"equipmentMode": "loan"}, http.StatusBadRequest)
	// A mensalidade do comodato da pessoa física: a do plano mais a garantia de troca.
	monthly := 6990 + 300
	env.must(token, http.MethodPut, "/api/order-options", map[string]any{"loan": map[string]any{"enabled": true}, "install": install},
		http.StatusForbidden)
	env.must(admin, http.MethodPut, "/api/order-options", map[string]any{
		"loan": map[string]any{"enabled": true, "surchargeCents": -1}, "install": install,
	}, http.StatusBadRequest)
	env.must(admin, http.MethodPut, "/api/order-options", map[string]any{
		"loan":    map[string]any{"enabled": true, "surchargeCents": 300, "installFeeCents": 9900, "uninstallFeeCents": 4900},
		"install": install,
	}, http.StatusOK)
	if c := readCatalog(); c.Loan == nil || c.Loan.SurchargeCents != 300 || c.Loan.InstallFeeCents != 9900 {
		t.Fatalf("catálogo com comodato = %+v", c)
	}
	order("NOA3C33", map[string]any{"equipmentMode": "loan", "launchPromo": true}, http.StatusBadRequest)
	order("NOA3C33", map[string]any{"equipmentMode": "aluguel"}, http.StatusBadRequest)
	// No comodato, a instalação é sempre da Farbo, com a taxa do comodato
	// (o "farboInstall" não troca a taxa).
	r = order("NOA3C33", map[string]any{"equipmentMode": "loan", "farboInstall": true}, http.StatusCreated)
	if !r.PayNow || r.SetupInvoice != nil || r.InstallInvoice == nil || r.InstallInvoice.DueDate.String() != "2026-10-13" ||
		r.InstallInvoice.AmountCents != 9900 || r.FirstInvoice == nil || r.FirstInvoice.AmountCents != monthly ||
		r.Subscription.EquipmentMode != billing.EquipmentLoan || r.Subscription.UninstallFeeCents != 4900 ||
		r.Subscription.PriceCents != monthly || r.Subscription.PlanName != "Plano Mensal"+orders.GuaranteeName ||
		!farbo(r.Subscription.ID.String()) {
		t.Fatalf("comodato = %+v", r)
	}

	// Com o plano definido pela central, vale o rastreador do plano (sem escolha).
	env.must(admin, http.MethodPut, "/api/customers/"+lia.ID+"/plan", map[string]any{
		"planName": "Plano Mensal", "priceCents": 6990, "dueDay": 10, "equipmentMode": "sale",
	}, http.StatusOK)
	if c := readCatalog(); !c.PlanTerms || c.Loan != nil {
		t.Fatalf("catálogo com plano = %+v", c)
	}
}
