package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
)

// O corretor da seguradora: vê e cadastra só os associados dele (o que ele
// cadastra fica com ele), os veículos e os relatórios deles; não desliga nem
// passa associado para outro, não bloqueia e não vê o resto do painel. O
// Administrador põe o corretor no associado. Precisa de
// FARBO_TEST_DATABASE_URL.
func TestOrganizationBrokers(t *testing.T) {
	notifier := &recordingOrgNotifier{}
	env := newCredEnvWith(t, credEnvOptions{orgs: notifier, fleet: true})
	ctx := context.Background()
	if _, err := env.auth.CreateUser(ctx, "admin@corretor.test", "Central", auth.RoleAdmin, userPassword); err != nil {
		t.Fatal(err)
	}
	central := env.tokenFor("admin@corretor.test")
	var created struct {
		Organization struct {
			ID uuid.UUID `json:"id"`
		} `json:"organization"`
	}
	_ = json.Unmarshal(env.must(central, http.MethodPost, "/api/organizations", map[string]any{
		"kind": "insurer", "name": "Seguros Unidos", "document": "11.222.333/0001-81", "email": "contato@unidos.test",
		"priceCents": 4000, "dueDay": 10, "admin": map[string]string{"name": "Ana Admin", "email": "ana@unidos.test"},
	}, http.StatusCreated), &created)
	for _, email := range []string{"ana@unidos.test"} {
		env.must("", http.MethodPost, "/api/auth/reset-password", map[string]string{"token": notifier.token(email), "password": userPassword}, http.StatusNoContent)
	}
	ana := env.tokenFor("ana@unidos.test")
	var broker struct {
		UserID uuid.UUID `json:"userId"`
		Role   string    `json:"role"`
	}
	_ = json.Unmarshal(env.must(ana, http.MethodPost, "/api/org/team",
		map[string]string{"name": "Beto Corretor", "email": "beto@corretora.test", "role": "broker"}, http.StatusCreated), &broker)
	if broker.Role != "broker" {
		t.Fatalf("corretor = %+v", broker)
	}
	env.must("", http.MethodPost, "/api/auth/reset-password", map[string]string{"token": notifier.token("beto@corretora.test"), "password": userPassword},
		http.StatusNoContent)
	beto := env.tokenFor("beto@corretora.test")

	associate := func(token, name, email string, extra map[string]any) uuid.UUID {
		t.Helper()
		body := map[string]any{"name": name, "email": email}
		for k, v := range extra {
			body[k] = v
		}
		var a struct {
			ID uuid.UUID `json:"id"`
		}
		_ = json.Unmarshal(env.must(token, http.MethodPost, "/api/org/associates", body, http.StatusCreated), &a)
		return a.ID
	}
	aldo := associate(ana, "Aldo Sem Corretor", "aldo@seg.test", nil)
	bela := associate(ana, "Bela do Beto", "bela@seg.test", map[string]any{"brokerId": broker.UserID.String()})
	caio := associate(beto, "Caio do Beto", "caio@seg.test", nil)
	env.must(ana, http.MethodPost, "/api/org/associates", map[string]any{"name": "X", "email": "x@seg.test", "brokerId": aldo.String()},
		http.StatusBadRequest)
	for _, a := range []struct {
		id   uuid.UUID
		name string
	}{{aldo, "Carro do Aldo"}, {bela, "Carro da Bela"}, {caio, "Moto do Caio"}} {
		env.must(ana, http.MethodPost, "/api/org/associates/"+a.id.String()+"/vehicles", map[string]any{"name": a.name}, http.StatusCreated)
	}
	if err := env.owners.Refresh(ctx); err != nil {
		t.Fatal(err)
	}

	var list []struct {
		ID         uuid.UUID `json:"id"`
		BrokerName string    `json:"brokerName"`
	}
	_ = json.Unmarshal(env.must(beto, http.MethodGet, "/api/org/associates", nil, http.StatusOK), &list)
	if len(list) != 2 || list[0].ID != bela || list[1].ID != caio || list[1].BrokerName != "Beto Corretor" {
		t.Fatalf("associados do corretor = %+v", list)
	}
	_ = json.Unmarshal(env.must(ana, http.MethodGet, "/api/org/associates", nil, http.StatusOK), &list)
	if len(list) != 3 {
		t.Fatalf("associados da admin = %+v", list)
	}
	env.must(beto, http.MethodGet, "/api/org/associates/"+aldo.String(), nil, http.StatusNotFound)
	env.must(beto, http.MethodPost, "/api/org/associates/"+aldo.String()+"/vehicles", map[string]any{"name": "Outro"}, http.StatusNotFound)

	var vehicles []struct {
		ID   uuid.UUID `json:"id"`
		Name string    `json:"name"`
	}
	_ = json.Unmarshal(env.must(beto, http.MethodGet, "/api/vehicles", nil, http.StatusOK), &vehicles)
	if len(vehicles) != 2 {
		t.Fatalf("veículos do corretor = %+v", vehicles)
	}
	var all []struct {
		ID   uuid.UUID `json:"id"`
		Name string    `json:"name"`
	}
	_ = json.Unmarshal(env.must(ana, http.MethodGet, "/api/vehicles", nil, http.StatusOK), &all)
	var aldoCar uuid.UUID
	for _, v := range all {
		if v.Name == "Carro do Aldo" {
			aldoCar = v.ID
		}
	}
	env.must(beto, http.MethodGet, "/api/vehicles/"+aldoCar.String(), nil, http.StatusNotFound)
	env.must(beto, http.MethodPost, "/api/vehicles/"+vehicles[0].ID.String()+"/commands/request-position", nil, http.StatusForbidden)

	var summary struct {
		Rows []struct{ Name string } `json:"rows"`
	}
	_ = json.Unmarshal(env.must(beto, http.MethodGet, "/api/reports/summary", nil, http.StatusOK), &summary)
	if len(summary.Rows) != 2 {
		t.Fatalf("relatório do corretor = %+v", summary)
	}

	// Não desliga nem passa para outro; o resto do painel, não.
	var updated struct {
		Active   bool       `json:"active"`
		BrokerID *uuid.UUID `json:"brokerId"`
	}
	_ = json.Unmarshal(env.must(beto, http.MethodPatch, "/api/org/associates/"+caio.String(),
		map[string]any{"name": "Caio Lima", "active": false, "brokerId": ""}, http.StatusOK), &updated)
	if !updated.Active || updated.BrokerID == nil || *updated.BrokerID != broker.UserID {
		t.Fatalf("o corretor desligou ou tirou o corretor: %+v", updated)
	}
	for _, path := range []string{"/api/org/recoveries", "/api/org/announcements", "/api/org/team", "/api/org/invoices"} {
		env.must(beto, http.MethodGet, path, nil, http.StatusForbidden)
	}
	var overview struct {
		Organization struct{ Associates, Vehicles int }
	}
	_ = json.Unmarshal(env.must(beto, http.MethodGet, "/api/org/", nil, http.StatusOK), &overview)
	if overview.Organization.Associates != 2 || overview.Organization.Vehicles != 2 {
		t.Fatalf("números do corretor = %+v", overview)
	}

	// A admin passa a Bela para ninguém: some do corretor.
	env.must(ana, http.MethodPatch, "/api/org/associates/"+bela.String(), map[string]any{"name": "Bela do Beto", "brokerId": ""}, http.StatusOK)
	_ = json.Unmarshal(env.must(beto, http.MethodGet, "/api/org/associates", nil, http.StatusOK), &list)
	if len(list) != 1 || list[0].ID != caio {
		t.Fatalf("depois de tirar a Bela = %+v", list)
	}
}
