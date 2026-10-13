package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/leads"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/orders"
)

// A central corrige o plano de uma assinatura contratada na promoção: a
// mensalidade promocional acompanha o plano (a do Insanos MC é outra), mudar
// só o preço não mexe nela e a assinatura sem promoção continua sem.
// Precisa de FARBO_TEST_DATABASE_URL.
func TestUpdateSubscriptionPromoFollowsPlan(t *testing.T) {
	env, _ := newLeadsEnv(t)
	admin := env.login(auth.RoleAdmin + "@leads.test")
	var rui struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/customers", map[string]any{
		"name": "Rui Insanos", "email": "rui@insanos.test", "phone": "(11) 95555-2222", "document": "", "password": userPassword,
	}, http.StatusCreated), &rui)
	token := env.login("rui@insanos.test")
	env.must(token, http.MethodPut, "/api/me/address", map[string]any{
		"zipCode": "01305-000", "street": "Rua Augusta", "number": "500", "district": "Consolação", "city": "São Paulo", "state": "SP",
	}, http.StatusOK)
	env.must(admin, http.MethodPost, "/api/customers/"+rui.ID+"/launch-promo/grant", nil, http.StatusOK)

	// Contratou antes de a central definir o plano dele: saiu no padrão, com
	// a promoção dos demais.
	order := func(plate string, promo bool) billing.Subscription {
		t.Helper()
		var r struct {
			Subscription billing.Subscription `json:"subscription"`
		}
		_ = json.Unmarshal(env.must(token, http.MethodPost, "/api/me/trackers", map[string]any{
			"vehicle": map[string]any{"name": "Moto " + plate, "plate": plate}, "launchPromo": promo,
		}, http.StatusCreated), &r)
		return r.Subscription
	}
	sub := order("RUI1A11", true)
	if sub.PlanName != "Plano Mensal" || sub.PromoPriceCents == nil || *sub.PromoPriceCents != 3490 {
		t.Fatalf("assinatura contratada = %+v", sub)
	}
	plain := order("RUI2B22", false)

	update := func(id, plan string, cents int) billing.Subscription {
		t.Helper()
		var out billing.Subscription
		_ = json.Unmarshal(env.must(admin, http.MethodPatch, "/api/subscriptions/"+id,
			map[string]any{"planName": plan, "priceCents": cents}, http.StatusOK), &out)
		return out
	}
	promo := func(s billing.Subscription) int {
		if s.PromoPriceCents == nil {
			return -1
		}
		return *s.PromoPriceCents
	}

	// Só o preço: a promoção fica.
	if s := update(sub.ID.String(), "Plano Mensal", 5990); s.PriceCents != 5990 || promo(s) != 3490 {
		t.Errorf("só o preço = %+v (promoção %d)", s, promo(s))
	}
	// O plano dos Insanos: a promoção passa a ser a deles, sem mudar o prazo.
	s := update(sub.ID.String(), "Especial Insanos MC", 3990)
	if s.PlanName != "Especial Insanos MC" || s.PriceCents != 3990 || promo(s) != 2790 {
		t.Errorf("no plano dos Insanos = %+v (promoção %d)", s, promo(s))
	}
	if s.PromoUntil == nil || sub.PromoUntil == nil || s.PromoUntil.String() != sub.PromoUntil.String() {
		t.Errorf("prazo da promoção mudou: %v → %v", sub.PromoUntil, s.PromoUntil)
	}
	// E de volta ao padrão, a dos demais.
	if s := update(sub.ID.String(), "Plano Mensal", 6990); promo(s) != 3490 {
		t.Errorf("de volta ao padrão: promoção %d", promo(s))
	}
	// Sem promoção, trocar o plano não cria uma.
	if s := update(plain.ID.String(), "Especial Insanos MC", 3990); s.PromoPriceCents != nil {
		t.Errorf("assinatura sem promoção ganhou uma: %d", promo(s))
	}
}

// Quem escolheu o preço dos Insanos no pré-cadastro já nasce com o plano
// deles na conta quando a central o cadastra a partir do pré-cliente (o
// pedido feito pelo app sai nele); os outros planos ficam no padrão.
// Precisa de FARBO_TEST_DATABASE_URL.
func TestCreateCustomerFromInsanosLead(t *testing.T) {
	env, _ := newLeadsEnv(t)
	admin := env.login(auth.RoleAdmin + "@leads.test")

	register := func(name, email, plan string) *orders.AccountPlan {
		t.Helper()
		env.must("", http.MethodPost, "/api/public/leads", map[string]any{
			"name": name, "email": email, "phone": "(11) 97777-3333", "city": "Campinas",
			"plan": plan, "vehicleType": "moto", "vehicleCount": 1, "message": "", "consent": true, "website": "",
		}, http.StatusCreated)
		var list []leads.Lead
		_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/leads?status=NEW", nil, http.StatusOK), &list)
		var lead *leads.Lead
		for i := range list {
			if list[i].Email == email {
				lead = &list[i]
			}
		}
		if lead == nil {
			t.Fatalf("pré-cliente %s não apareceu: %+v", email, list)
		}
		var customer struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/customers", map[string]any{
			"name": name, "email": email, "phone": lead.Phone, "document": "", "password": userPassword, "leadId": lead.ID,
		}, http.StatusCreated), &customer)
		var detail struct {
			Plan *orders.AccountPlan `json:"plan"`
		}
		_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/customers/"+customer.ID, nil, http.StatusOK), &detail)
		return detail.Plan
	}

	plan := register("Beto Insanos", "beto@insanos.test", "Preço Especial Insanos MC - R$ 39,90")
	if plan == nil || plan.PlanName != "Especial Insanos MC" || plan.PriceCents != 3990 || plan.DueDay != 10 {
		t.Errorf("plano do Insanos = %+v", plan)
	}
	if plan := register("Caio Geral", "caio@geral.test", "Plano Mensal - R$ 69,90"); plan != nil {
		t.Errorf("plano mensal ganhou plano na conta: %+v", plan)
	}
}
