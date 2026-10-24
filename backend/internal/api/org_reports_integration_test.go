package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/vehicles"
)

// Os relatórios da associação: os veículos dos associados (com o nome de
// cada um), um associado só com ?associado=, e a planilha com a coluna do
// associado. Os de outro cliente ficam de fora. Precisa de
// FARBO_TEST_DATABASE_URL.
func TestOrganizationReports(t *testing.T) {
	env := newCredEnvWith(t, credEnvOptions{fleet: true, orgs: &recordingOrgNotifier{}})
	ctx := context.Background()
	if _, err := env.auth.CreateUser(ctx, "admin@relat.test", "Central", auth.RoleAdmin, userPassword); err != nil {
		t.Fatal(err)
	}
	central := env.tokenFor("admin@relat.test")
	var created struct {
		Organization struct {
			ID uuid.UUID `json:"id"`
		} `json:"organization"`
	}
	_ = json.Unmarshal(env.must(central, http.MethodPost, "/api/organizations", map[string]any{
		"kind": "insurer", "name": "Seguradora Relat", "email": "fin@relat.test", "priceCents": 3000, "dueDay": 5,
		"admin": map[string]string{"name": "Vera", "email": "vera@relat.test"},
	}, http.StatusCreated), &created)
	vera, _ := env.auth.FindByEmail(ctx, "vera@relat.test")
	if _, err := env.auth.SetPassword(ctx, vera.ID, userPassword); err != nil {
		t.Fatal(err)
	}
	org := env.tokenFor("vera@relat.test")

	associate := func(name, email string) uuid.UUID {
		t.Helper()
		var a struct {
			ID uuid.UUID `json:"id"`
		}
		_ = json.Unmarshal(env.must(org, http.MethodPost, "/api/org/associates", map[string]any{"name": name, "email": email},
			http.StatusCreated), &a)
		return a.ID
	}
	bia, caio := associate("Bia", "bia@relat.test"), associate("Caio", "caio@relat.test")
	for _, v := range []struct {
		owner uuid.UUID
		name  string
	}{{bia, "Moto da Bia"}, {caio, "Carro do Caio"}} {
		env.must(org, http.MethodPost, "/api/org/associates/"+v.owner.String()+"/vehicles", map[string]any{"name": v.name}, http.StatusCreated)
	}
	outro, err := env.auth.CreateUser(ctx, "outro@relat.test", "Outro", auth.RoleCustomer, userPassword)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.vehicles.Create(ctx, vehicles.Input{Name: "Carro de fora", OwnerID: &outro.ID}); err != nil {
		t.Fatal(err)
	}

	type summary struct {
		Rows []struct {
			Name      string `json:"name"`
			OwnerName string `json:"ownerName"`
		} `json:"rows"`
	}
	var got summary
	_ = json.Unmarshal(env.must(org, http.MethodGet, "/api/reports/summary", nil, http.StatusOK), &got)
	if len(got.Rows) != 2 || got.Rows[0].OwnerName != "Bia" || got.Rows[1].Name != "Carro do Caio" || got.Rows[1].OwnerName != "Caio" {
		t.Fatalf("resumo da seguradora = %+v", got)
	}
	_ = json.Unmarshal(env.must(org, http.MethodGet, "/api/reports/summary?associado="+caio.String(), nil, http.StatusOK), &got)
	if len(got.Rows) != 1 || got.Rows[0].Name != "Carro do Caio" {
		t.Fatalf("resumo do Caio = %+v", got)
	}
	csv := string(env.must(org, http.MethodGet, "/api/reports/summary?formato=csv", nil, http.StatusOK))
	if !strings.Contains(csv, "Veículo;Placa;Associado;Km") || !strings.Contains(csv, "Moto da Bia;;Bia;") || strings.Contains(csv, "Carro de fora") {
		t.Errorf("planilha:\n%s", csv)
	}
	// A nota de direção e a utilização (os dados de uso): um dia rodado com
	// um excesso; a planilha sai por associado.
	var bikeID uuid.UUID
	if err := env.db.QueryRow(ctx, `SELECT id FROM vehicles WHERE name = 'Moto da Bia'`).Scan(&bikeID); err != nil {
		t.Fatal(err)
	}
	if _, err := env.db.Exec(ctx, `
		INSERT INTO devices (imei) VALUES ('869247061230999');
		`); err != nil {
		t.Fatal(err)
	}
	if _, err := env.db.Exec(ctx, `
		INSERT INTO vehicle_daily_stats (device_id, day, vehicle_id, distance_m, moving_s, overspeeds, night_m, harsh_brakes)
		SELECT id, CURRENT_DATE, $1, 100000, 7200, 1, 50000, 1 FROM devices WHERE imei = '869247061230999'`, bikeID); err != nil {
		t.Fatal(err)
	}
	var driving struct {
		Rows []struct {
			Name         string  `json:"name"`
			Score        *int    `json:"score"`
			NightPercent float64 `json:"nightPercent"`
		} `json:"rows"`
	}
	_ = json.Unmarshal(env.must(org, http.MethodGet, "/api/reports/driving", nil, http.StatusOK), &driving)
	if len(driving.Rows) != 2 || driving.Rows[0].Name != "Moto da Bia" || driving.Rows[0].Score == nil ||
		*driving.Rows[0].Score != 88 || driving.Rows[0].NightPercent != 50 || driving.Rows[1].Score != nil {
		t.Fatalf("direção = %+v", driving)
	}
	csv = string(env.must(org, http.MethodGet, "/api/reports/driving?formato=csv", nil, http.StatusOK))
	if !strings.Contains(csv, "Veículo;Placa;Associado;Nota;Km") || !strings.Contains(csv, "Moto da Bia;;Bia;88;100,0") {
		t.Errorf("planilha da direção:\n%s", csv)
	}
	var usage struct {
		Rows []struct {
			ActiveDays int `json:"activeDays"`
			PeriodDays int `json:"periodDays"`
		} `json:"rows"`
	}
	_ = json.Unmarshal(env.must(org, http.MethodGet, "/api/reports/usage", nil, http.StatusOK), &usage)
	if len(usage.Rows) != 2 || usage.Rows[0].ActiveDays != 1 || usage.Rows[0].PeriodDays < 1 {
		t.Fatalf("utilização = %+v", usage)
	}

	// O ?cliente= da central não vale para a seguradora.
	_ = json.Unmarshal(env.must(org, http.MethodGet, "/api/reports/summary?cliente="+outro.ID.String(), nil, http.StatusOK), &got)
	if len(got.Rows) != 2 {
		t.Errorf("com ?cliente= = %+v", got)
	}
}
