package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/leads"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/orders"
)

// O plano dos Insanos é só com a central: o "Sou integrante do Insanos MC"
// das telas antigas do pré-cadastro e o plano deles escolhido no formulário
// da landing não chegam à conta — a central cria o cliente (a partir do
// pré-cliente ou não) e ele nasce sem plano. Quando a central define o plano
// dos Insanos, o carro pedido pelo app sai nele.
// Precisa de FARBO_TEST_DATABASE_URL.
func TestInsanosPlanOnlyByCentral(t *testing.T) {
	env, _ := newLeadsEnv(t)
	admin := env.login(auth.RoleAdmin + "@leads.test")

	env.must("", http.MethodPost, "/api/public/launch", map[string]any{
		"name": "Edu Insano", "email": "edu@evento.test", "phone": "(11) 97777-2222", "consent": true, "event": "encontro-insanos",
		"city": "São Paulo", "insanos": true, "website": "",
	}, http.StatusCreated)
	env.must("", http.MethodPost, "/api/public/leads", map[string]any{
		"name": "Beto Insanos", "email": "beto@insanos.test", "phone": "(11) 97777-3333", "city": "Campinas",
		"plan": "Preço Especial Insanos MC - R$ 39,90", "vehicleType": "moto", "vehicleCount": 1, "message": "", "consent": true,
		"website": "",
	}, http.StatusCreated)

	var list []leads.Lead
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/leads", nil, http.StatusOK), &list)
	byEmail := map[string]leads.Lead{}
	for _, l := range list {
		byEmail[l.Email] = l
	}
	if edu, ok := byEmail["edu@evento.test"]; !ok || edu.Plan != "" {
		t.Fatalf("pré-cliente do evento = %+v", edu)
	}
	beto, ok := byEmail["beto@insanos.test"]
	if !ok {
		t.Fatalf("pré-cliente da landing não apareceu: %+v", list)
	}

	type customer struct {
		ID   string              `json:"id"`
		Plan *orders.AccountPlan `json:"plan"`
	}
	create := func(body map[string]any) customer {
		t.Helper()
		body["phone"], body["document"], body["password"] = "(11) 97777-2222", "", userPassword
		var c customer
		_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/customers", body, http.StatusCreated), &c)
		_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/customers/"+c.ID, nil, http.StatusOK), &c)
		return c
	}
	edu := create(map[string]any{"name": "Edu Insano", "email": "edu@evento.test"})
	if edu.Plan != nil {
		t.Errorf("conta do Edu nasceu com plano: %+v", edu.Plan)
	}
	if c := create(map[string]any{"name": "Beto Insanos", "email": "beto@insanos.test", "leadId": beto.ID}); c.Plan != nil {
		t.Errorf("conta do Beto nasceu com plano: %+v", c.Plan)
	}

	// A central põe o Edu no plano dos Insanos: o carro que ele pede pelo
	// app sai nele.
	env.must(admin, http.MethodPut, "/api/customers/"+edu.ID+"/plan",
		map[string]any{"planName": "Especial Insanos MC", "priceCents": 3990, "dueDay": 10}, http.StatusOK)
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
