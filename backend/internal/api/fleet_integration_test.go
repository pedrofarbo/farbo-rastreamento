package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/vehicles"
)

// A frota organizada: grupos e campos personalizados da conta, o veículo
// criado com eles, a edição que não manda os campos não apaga, o grupo de
// outra conta não entra, apagar o grupo deixa os veículos sem grupo, e o
// relatório filtra pelo grupo (com o grupo e os campos na planilha).
// Precisa de FARBO_TEST_DATABASE_URL.
func TestFleetGroupsAndCustomFields(t *testing.T) {
	env, _ := newLeadsEnv(t)
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	authSvc := auth.NewService(auth.NewRepository(env.db), config.Auth{BcryptCost: bcrypt.MinCost}, nil, log)
	frota, err := authSvc.CreateUser(ctx, "frota@grupos.test", "Frota Ltda", auth.RoleCustomer, userPassword)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authSvc.CreateUser(ctx, "outra@grupos.test", "Outra", auth.RoleCustomer, userPassword); err != nil {
		t.Fatal(err)
	}
	customer, other, admin := env.login("frota@grupos.test"), env.login("outra@grupos.test"), env.login(auth.RoleAdmin+"@leads.test")

	group := func(token, name string, want int) vehicles.Group {
		t.Helper()
		var g vehicles.Group
		_ = json.Unmarshal(env.must(token, http.MethodPost, "/api/me/fleet/groups", map[string]any{"name": name}, want), &g)
		return g
	}
	zonaSul := group(customer, "Entregas Zona Sul", http.StatusCreated)
	diretoria := group(customer, "Diretoria", http.StatusCreated)
	group(customer, "  entregas zona sul ", http.StatusBadRequest) // nome repetido
	alheio := group(other, "Grupo da outra", http.StatusCreated)
	env.must(customer, http.MethodPut, "/api/me/fleet/labels", map[string]any{"label1": "Nº do contrato", "label2": "Centro de custo"}, http.StatusOK)

	// O veículo nasce no grupo, com os dois campos.
	create := func(plate string, extra map[string]any) vehicles.Vehicle {
		t.Helper()
		v := map[string]any{"name": "Van " + plate, "plate": plate}
		for k, val := range extra {
			v[k] = val
		}
		var out struct {
			Vehicle vehicles.Vehicle `json:"vehicle"`
		}
		_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/customers/"+frota.ID.String()+"/trackers", map[string]any{
			"vehicle": v, "equipmentCents": 0, "plan": map[string]any{"planName": "Plano Frota", "priceCents": 4990, "dueDay": 10},
		}, http.StatusCreated), &out)
		return out.Vehicle
	}
	van := create("GRP1A11", map[string]any{"groupId": zonaSul.ID.String(), "custom1": " PMSP-2026/0042 ", "custom2": "CC-11"})
	if van.GroupID == nil || *van.GroupID != zonaSul.ID || van.Custom1 != "PMSP-2026/0042" || van.Custom2 != "CC-11" {
		t.Fatalf("van = %+v", van)
	}
	// O grupo de outra conta não entra.
	if moto := create("GRP2B22", map[string]any{"groupId": alheio.ID.String()}); moto.GroupID != nil {
		t.Errorf("entrou no grupo de outra conta: %+v", moto)
	}

	// Editar sem os campos (tela antiga) mantém; com eles, troca.
	path := "/api/vehicles/" + van.ID.String()
	var edited vehicles.Vehicle
	_ = json.Unmarshal(env.must(customer, http.MethodPatch, path, map[string]any{"name": "Van 01", "plate": "GRP1A11"}, http.StatusOK), &edited)
	if edited.GroupID == nil || *edited.GroupID != zonaSul.ID || edited.Custom1 != "PMSP-2026/0042" {
		t.Errorf("a edição sem os campos apagou: %+v", edited)
	}
	_ = json.Unmarshal(env.must(customer, http.MethodPatch, path, map[string]any{"name": "Van 01", "plate": "GRP1A11",
		"groupId": diretoria.ID.String(), "custom1": "", "custom2": "CC-12"}, http.StatusOK), &edited)
	if edited.GroupID == nil || *edited.GroupID != diretoria.ID || edited.Custom1 != "" || edited.Custom2 != "CC-12" {
		t.Errorf("a edição com os campos = %+v", edited)
	}
	env.must(customer, http.MethodPatch, path, map[string]any{"name": "Van 01", "groupId": "não-é-id"}, http.StatusBadRequest)
	env.must(customer, http.MethodPatch, path, map[string]any{"name": "Van 01", "custom1": strings.Repeat("x", 101)}, http.StatusBadRequest)

	// A conta vê os grupos (com quantos veículos) e os nomes dos campos.
	var fleet struct {
		Groups []vehicles.Group     `json:"groups"`
		Labels vehicles.FieldLabels `json:"labels"`
	}
	_ = json.Unmarshal(env.must(customer, http.MethodGet, "/api/me/fleet", nil, http.StatusOK), &fleet)
	if len(fleet.Groups) != 2 || fleet.Groups[0].Name != "Diretoria" || fleet.Groups[0].Vehicles != 1 || fleet.Labels.Label1 != "Nº do contrato" {
		t.Errorf("a frota = %+v", fleet)
	}

	// O relatório filtra pelo grupo; a planilha leva o grupo e os campos.
	var summary struct {
		Rows []struct {
			Name    string     `json:"name"`
			GroupID *uuid.UUID `json:"groupId"`
		} `json:"rows"`
	}
	_ = json.Unmarshal(env.must(customer, http.MethodGet, "/api/reports/summary?grupo="+diretoria.ID.String(), nil, http.StatusOK), &summary)
	if len(summary.Rows) != 1 || summary.Rows[0].Name != "Van 01" {
		t.Errorf("relatório da Diretoria = %+v", summary.Rows)
	}
	_, raw := env.do(customer, http.MethodGet, "/api/reports/summary?formato=csv", nil)
	if !strings.Contains(string(raw), "Veículo;Placa;Grupo;Nº do contrato;Centro de custo;Km") ||
		!strings.Contains(string(raw), "Van 01;GRP1A11;Diretoria;;CC-12;") {
		t.Errorf("planilha:\n%s", raw)
	}

	// Renomear e apagar: os veículos do grupo apagado ficam sem grupo.
	env.must(customer, http.MethodPatch, "/api/me/fleet/groups/"+diretoria.ID.String(), map[string]any{"name": "Diretoria SP"}, http.StatusNoContent)
	env.must(other, http.MethodDelete, "/api/me/fleet/groups/"+diretoria.ID.String(), nil, http.StatusNotFound)
	env.must(customer, http.MethodDelete, "/api/me/fleet/groups/"+diretoria.ID.String(), nil, http.StatusNoContent)
	var after vehicles.Vehicle
	_ = json.Unmarshal(env.must(customer, http.MethodGet, path, nil, http.StatusOK), &after)
	if after.GroupID != nil || after.Custom2 != "CC-12" {
		t.Errorf("depois de apagar o grupo = %+v", after)
	}
}
