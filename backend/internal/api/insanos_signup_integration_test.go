package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/leads"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/orders"
)

// O plano dos Insanos escolhido nas páginas públicas chega à conta: na
// página de pré-cadastro (QR Code do evento, link de indicação), quem marca
// "Sou integrante do Insanos MC" vira pré-cliente com o plano deles; a conta
// criada pela central — a partir do pré-cliente ou não — já nasce no plano,
// e o carro pedido pelo app sai nele. Quem não marca fica no padrão.
// Precisa de FARBO_TEST_DATABASE_URL.
func TestInsanosChoiceBecomesAccountPlan(t *testing.T) {
	env, _ := newLeadsEnv(t)
	admin := env.login(auth.RoleAdmin + "@leads.test")

	join := func(name, email string, insanos bool) {
		t.Helper()
		env.must("", http.MethodPost, "/api/public/launch", map[string]any{
			"name": name, "email": email, "phone": "(11) 97777-2222", "consent": true, "event": "encontro-insanos",
			"city": "São Paulo", "insanos": insanos, "website": "",
		}, http.StatusCreated)
	}
	join("Edu Insano", "edu@evento.test", true)
	join("Gil Geral", "gil@evento.test", false)

	var list []leads.Lead
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/leads", nil, http.StatusOK), &list)
	plans := map[string]string{}
	for _, l := range list {
		plans[l.Email] = l.Plan
	}
	if plans["edu@evento.test"] != "Preço Especial Insanos MC - R$ 39,90" || plans["gil@evento.test"] != "" {
		t.Fatalf("planos dos pré-clientes = %v", plans)
	}

	// A central cria as contas sem partir do pré-cliente (pelo e-mail).
	accountPlan := func(name, email string) *orders.AccountPlan {
		t.Helper()
		var c struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/customers", map[string]any{
			"name": name, "email": email, "phone": "(11) 97777-2222", "document": "", "password": userPassword,
		}, http.StatusCreated), &c)
		var d struct {
			Plan *orders.AccountPlan `json:"plan"`
		}
		_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/customers/"+c.ID, nil, http.StatusOK), &d)
		return d.Plan
	}
	if p := accountPlan("Edu Insano", "edu@evento.test"); p == nil || p.PlanName != "Especial Insanos MC" || p.PriceCents != 3990 {
		t.Errorf("conta do Edu = %+v", p)
	}
	if p := accountPlan("Gil Geral", "gil@evento.test"); p != nil {
		t.Errorf("conta do Gil (sem marcar) = %+v", p)
	}

	// O carro que o Edu pede pelo app sai no plano dos Insanos.
	token := env.login("edu@evento.test")
	env.must(token, http.MethodPut, "/api/me/address", map[string]any{
		"zipCode": "01305-000", "street": "Rua Augusta", "number": "500", "district": "Consolação", "city": "São Paulo", "state": "SP",
	}, http.StatusOK)
	var placed struct {
		Subscription struct {
			PlanName   string `json:"planName"`
			PriceCents int    `json:"priceCents"`
		} `json:"subscription"`
	}
	_ = json.Unmarshal(env.must(token, http.MethodPost, "/api/me/trackers", map[string]any{
		"vehicle": map[string]any{"name": "Moto do Edu", "plate": "EDU1A11"},
	}, http.StatusCreated), &placed)
	if placed.Subscription.PlanName != "Especial Insanos MC" || placed.Subscription.PriceCents != 3990 {
		t.Errorf("carro do Edu = %+v", placed.Subscription)
	}
}
