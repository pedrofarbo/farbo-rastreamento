package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
)

// De quem é o veículo no mapa da central: a equipe (admin e operador) recebe o
// nome do dono na lista; o cliente, não (os veículos são dele). Precisa de
// FARBO_TEST_DATABASE_URL.
func TestVehicleOwnerNameForStaff(t *testing.T) {
	env, _ := newLeadsEnv(t)
	admin := env.login(auth.RoleAdmin + "@leads.test")
	env.must(admin, http.MethodPost, "/api/customers", map[string]any{
		"name": "Transportes Frota Ltda", "email": "frota@dono.test", "phone": "(11) 92222-0000", "document": "", "password": userPassword,
	}, http.StatusCreated)
	token := env.login("frota@dono.test")
	env.must(token, http.MethodPut, "/api/me/address", map[string]any{
		"zipCode": "01305-000", "street": "Rua Augusta", "number": "500", "district": "Consolação", "city": "São Paulo", "state": "SP",
	}, http.StatusOK)
	var placed struct {
		Vehicle struct {
			ID string `json:"id"`
		} `json:"vehicle"`
	}
	_ = json.Unmarshal(env.must(token, http.MethodPost, "/api/me/trackers", map[string]any{
		"vehicle": map[string]any{"name": "Van 01", "plate": "DON1A11"},
	}, http.StatusCreated), &placed)

	ownerOf := func(as string) string {
		t.Helper()
		var list []struct {
			ID        string `json:"id"`
			OwnerName string `json:"ownerName"`
		}
		_ = json.Unmarshal(env.must(as, http.MethodGet, "/api/vehicles", nil, http.StatusOK), &list)
		for _, v := range list {
			if v.ID == placed.Vehicle.ID {
				return v.OwnerName
			}
		}
		t.Fatalf("o veículo %s não está na lista", placed.Vehicle.ID)
		return ""
	}
	if got := ownerOf(admin); got != "Transportes Frota Ltda" {
		t.Errorf("admin vê o dono %q", got)
	}
	if got := ownerOf(env.login(auth.RoleOperator + "@leads.test")); got != "Transportes Frota Ltda" {
		t.Errorf("operador vê o dono %q", got)
	}
	if raw := env.must(token, http.MethodGet, "/api/vehicles", nil, http.StatusOK); strings.Contains(string(raw), "ownerName") {
		t.Errorf("o cliente recebeu o dono: %s", raw)
	}
}
