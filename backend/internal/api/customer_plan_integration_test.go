package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/orders"
)

// O plano do cliente, definido pela central antes de ele cadastrar os
// veículos: o app mostra e usa só esse plano (o cliente não escolhe), vale
// para todo veículo novo — com a mensalidade da promoção do plano dele —, a
// troca não mexe nas assinaturas que já existem e o padrão volta quando a
// central retira. Precisa de FARBO_TEST_DATABASE_URL.
func TestCustomerAccountPlan(t *testing.T) {
	env, _ := newLeadsEnv(t)
	admin := env.login(auth.RoleAdmin + "@leads.test")
	var edu struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/customers", map[string]any{
		"name": "Edu Insanos", "email": "edu@insanos.test", "phone": "(11) 96666-1111", "document": "", "password": userPassword,
	}, http.StatusCreated), &edu)
	token := env.login("edu@insanos.test")
	env.must(token, http.MethodPut, "/api/me/address", map[string]any{
		"zipCode": "01305-000", "street": "Rua Augusta", "number": "500", "district": "Consolação", "city": "São Paulo", "state": "SP",
	}, http.StatusOK)
	path := "/api/customers/" + edu.ID

	type catalogView struct {
		PlanName       string             `json:"planName"`
		PlanPriceCents int                `json:"planPriceCents"`
		DefaultDueDay  int                `json:"defaultDueDay"`
		LaunchPromo    *orders.PromoOffer `json:"launchPromo"`
	}
	catalog := func() catalogView {
		t.Helper()
		var c catalogView
		_ = json.Unmarshal(env.must(token, http.MethodGet, "/api/catalog", nil, http.StatusOK), &c)
		return c
	}
	detail := func() *orders.AccountPlan {
		t.Helper()
		var d struct {
			Plan *orders.AccountPlan `json:"plan"`
		}
		_ = json.Unmarshal(env.must(admin, http.MethodGet, path, nil, http.StatusOK), &d)
		return d.Plan
	}

	// Sem plano definido: o padrão.
	if p := detail(); p != nil {
		t.Fatalf("plano antes = %+v", p)
	}
	if c := catalog(); c.PlanName != "Plano Mensal" || c.PlanPriceCents != 6990 {
		t.Fatalf("catálogo antes = %+v", c)
	}

	// A central define o do Insanos MC (só o admin; dia de vencimento válido).
	insanos := map[string]any{"planName": "Especial Insanos MC", "priceCents": 3990, "dueDay": 15}
	env.must(env.login(auth.RoleOperator+"@leads.test"), http.MethodPut, path+"/plan", insanos, http.StatusForbidden)
	env.must(admin, http.MethodPut, path+"/plan", map[string]any{"planName": "Especial Insanos MC", "priceCents": 3990, "dueDay": 31}, http.StatusBadRequest)
	var plan orders.AccountPlan
	_ = json.Unmarshal(env.must(admin, http.MethodPut, path+"/plan", insanos, http.StatusOK), &plan)
	if plan.PlanName != "Especial Insanos MC" || plan.PriceCents != 3990 || plan.DueDay != 15 {
		t.Fatalf("plano definido = %+v", plan)
	}
	if p := detail(); p == nil || p.PlanName != "Especial Insanos MC" {
		t.Fatalf("na ficha = %+v", p)
	}

	// O app mostra o plano dele; com a promoção liberada, a mensalidade
	// promocional é a do Insanos.
	env.must(admin, http.MethodPost, path+"/launch-promo/grant", nil, http.StatusOK)
	c := catalog()
	if c.PlanName != "Especial Insanos MC" || c.PlanPriceCents != 3990 || c.DefaultDueDay != 15 || c.LaunchPromo == nil ||
		c.LaunchPromo.MonthlyCents != 2790 {
		t.Fatalf("catálogo com o plano = %+v", c)
	}

	// O cliente contrata sozinho (sem escolher plano): os dois veículos saem
	// no plano dele; o primeiro, com a promoção.
	type result struct {
		Subscription billing.Subscription `json:"subscription"`
	}
	order := func(plate string, promo bool) billing.Subscription {
		t.Helper()
		var r result
		_ = json.Unmarshal(env.must(token, http.MethodPost, "/api/me/trackers", map[string]any{
			"vehicle": map[string]any{"name": "Moto " + plate, "plate": plate}, "launchPromo": promo,
		}, http.StatusCreated), &r)
		return r.Subscription
	}
	first := order("INS1A11", true)
	second := order("INS2B22", false)
	for _, s := range []billing.Subscription{first, second} {
		if s.PlanName != "Especial Insanos MC" || s.PriceCents != 3990 || s.DueDay != 15 {
			t.Errorf("assinatura = %+v", s)
		}
	}
	if first.PromoPriceCents == nil || *first.PromoPriceCents != 2790 || second.PromoPriceCents != nil {
		t.Errorf("promoção: primeira %v, segunda %v", first.PromoPriceCents, second.PromoPriceCents)
	}
	// O cliente não manda plano (o pedido dele não tem esse campo).
	env.must(token, http.MethodPost, "/api/me/trackers", map[string]any{
		"vehicle": map[string]any{"name": "Carro", "plate": "INS3C33"}, "plan": map[string]any{"planName": "Barato", "priceCents": 100, "dueDay": 1},
	}, http.StatusBadRequest)

	// Trocar o plano vale para os novos; as assinaturas de antes ficam.
	env.must(admin, http.MethodPut, path+"/plan", map[string]any{"planName": "Plano Frota", "priceCents": 5990, "dueDay": 5}, http.StatusOK)
	third := order("FRO1A11", false)
	if third.PlanName != "Plano Frota" || third.PriceCents != 5990 || third.DueDay != 5 {
		t.Errorf("depois da troca = %+v", third)
	}
	var subs []billing.Subscription
	_ = json.Unmarshal(env.must(token, http.MethodGet, "/api/me/subscriptions", nil, http.StatusOK), &subs)
	insanosSubs := 0
	for _, s := range subs {
		if s.PlanName == "Especial Insanos MC" && s.PriceCents == 3990 {
			insanosSubs++
		}
	}
	if insanosSubs != 2 {
		t.Errorf("as assinaturas de antes mudaram: %+v", subs)
	}

	// Voltar ao padrão: o veículo novo segue a assinatura ativa.
	env.must(admin, http.MethodDelete, path+"/plan", nil, http.StatusNoContent)
	if p := detail(); p != nil {
		t.Errorf("depois de voltar ao padrão = %+v", p)
	}
	if c := catalog(); c.PlanName != "Especial Insanos MC" {
		t.Errorf("catálogo no padrão (a assinatura ativa) = %+v", c)
	}
}
