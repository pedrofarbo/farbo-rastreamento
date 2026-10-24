package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/devices"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/tracking"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/vehicles"
)

// Os itinerários de ponta a ponta: a linha com os pontos, as viagens do
// veículo (repetidas nos dias seguintes), o progresso pelas posições, o
// link do passageiro (só com a linha pública, sem placa) e ninguém de fora
// vê. Precisa de FARBO_TEST_DATABASE_URL.
func TestBusRoutes(t *testing.T) {
	env := newCredEnvWith(t, credEnvOptions{fleet: true, fleetOps: &recordingFleetNotifier{}})
	ctx := context.Background()
	owner, err := env.auth.CreateUser(ctx, "linhas@onibus.test", "Viação Linha", auth.RoleCustomer, userPassword)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.auth.CreateUser(ctx, "outro@onibus.test", "Outro", auth.RoleCustomer, userPassword); err != nil {
		t.Fatal(err)
	}
	dev, err := env.devices.Create(ctx, devices.Input{IMEIConfirmed: true, IMEI: "869247061230555", Protocol: "gt06"})
	if err != nil {
		t.Fatal(err)
	}
	bus, err := env.vehicles.Create(ctx, vehicles.Input{Name: "Ônibus 30", Plate: "LIN3A30", DeviceID: &dev.ID, OwnerID: &owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := env.owners.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	tok, outro := env.tokenFor("linhas@onibus.test"), env.tokenFor("outro@onibus.test")

	stops := []map[string]any{
		{"name": "Terminal", "latitude": -23.60, "longitude": -46.60, "offsetMin": 0},
		{"name": "Praça", "latitude": -23.59, "longitude": -46.60, "offsetMin": 10},
		{"name": "Hospital", "latitude": -23.58, "longitude": -46.60, "offsetMin": 20},
	}
	env.must(tok, http.MethodPost, "/api/me/routes", map[string]any{"name": "Linha 1", "stops": stops[:1]}, http.StatusBadRequest)
	var route struct {
		ID        uuid.UUID `json:"id"`
		PublicURL string    `json:"publicUrl"`
		Stops     []struct {
			Seq     int    `json:"seq"`
			Name    string `json:"name"`
			RadiusM int    `json:"radiusM"`
		} `json:"stops"`
	}
	_ = json.Unmarshal(env.must(tok, http.MethodPost, "/api/me/routes", map[string]any{"name": "Linha 1 - Centro", "stops": stops},
		http.StatusCreated), &route)
	if len(route.Stops) != 3 || route.Stops[2].Name != "Hospital" || route.Stops[0].RadiusM != 80 || route.PublicURL != "" {
		t.Fatalf("linha = %+v", route)
	}
	path := "/api/me/routes/" + route.ID.String()
	env.must(outro, http.MethodGet, path, nil, http.StatusNotFound)

	// As viagens: hoje, saiu há 15 min, e amanhã no mesmo horário.
	departs := time.Now().Add(-15 * time.Minute).Truncate(time.Minute)
	var trips []struct {
		ID uuid.UUID `json:"id"`
	}
	_ = json.Unmarshal(env.must(tok, http.MethodPost, path+"/trips", map[string]any{"vehicleId": bus.ID, "departsAt": departs, "days": 2},
		http.StatusCreated), &trips)
	if len(trips) != 2 {
		t.Fatalf("viagens = %+v", trips)
	}
	for _, p := range []struct {
		min      int
		lat, lon float64
	}{{1, -23.6, -46.6}, {12, -23.59, -46.6}} {
		at := departs.Add(time.Duration(p.min) * time.Minute)
		valid := true
		if err := env.positions.Insert(ctx, &tracking.Position{DeviceID: dev.ID, GPSTimestamp: at, ReceivedAt: at, Latitude: p.lat,
			Longitude: p.lon, GPSValid: &valid}); err != nil {
			t.Fatal(err)
		}
	}
	type progress struct {
		Status   string `json:"status"`
		NextSeq  *int   `json:"nextSeq"`
		DelayMin *int   `json:"delayMin"`
		Stops    []struct {
			ArrivedAt *time.Time `json:"arrivedAt"`
		} `json:"stops"`
	}
	var day struct {
		Trips []progress `json:"trips"`
	}
	_ = json.Unmarshal(env.must(tok, http.MethodGet, path+"/day", nil, http.StatusOK), &day)
	if len(day.Trips) != 1 || day.Trips[0].Status != "running" || *day.Trips[0].NextSeq != 3 || *day.Trips[0].DelayMin != 2 ||
		day.Trips[0].Stops[1].ArrivedAt == nil {
		t.Fatalf("dia = %+v", day)
	}

	// O link do passageiro: só com a linha pública.
	_ = json.Unmarshal(env.must(tok, http.MethodPut, path, map[string]any{"name": "Linha 1 - Centro", "stops": stops, "public": true},
		http.StatusOK), &route)
	if !strings.HasPrefix(route.PublicURL, "https://painel.farbo.test/linha/") {
		t.Fatalf("link = %q", route.PublicURL)
	}
	public := "/api/public/routes/" + strings.TrimPrefix(route.PublicURL, "https://painel.farbo.test/linha/")
	body := string(env.must("", http.MethodGet, public, nil, http.StatusOK))
	if !strings.Contains(body, `"name":"Linha 1 - Centro"`) || !strings.Contains(body, `"status":"running"`) || strings.Contains(body, "LIN3A30") {
		t.Fatalf("público = %s", body)
	}
	env.must(tok, http.MethodPut, path, map[string]any{"name": "Linha 1 - Centro", "stops": stops, "public": false}, http.StatusOK)
	env.must("", http.MethodGet, public, nil, http.StatusGone)

	// Cancelada, a viagem fica cancelada no dia.
	env.must(outro, http.MethodPost, "/api/me/trips/"+trips[0].ID.String()+"/cancel", nil, http.StatusNotFound)
	env.must(tok, http.MethodPost, "/api/me/trips/"+trips[0].ID.String()+"/cancel", nil, http.StatusNoContent)
	_ = json.Unmarshal(env.must(tok, http.MethodGet, path+"/day", nil, http.StatusOK), &day)
	if day.Trips[0].Status != "canceled" {
		t.Fatalf("cancelada = %+v", day)
	}
}
