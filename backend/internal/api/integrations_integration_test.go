package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/devices"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/events"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/tracking"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/vehicles"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/webhooks"
)

// As integrações de ponta a ponta: o titular gera a chave (que aparece uma
// vez) e o sistema dele lê os veículos, as posições e os eventos pela API;
// revogada, para na hora; a chave de uma conta não vê o veículo de outra. O
// webhook recebe o evento assinado; o teste manda o PING. Precisa de
// FARBO_TEST_DATABASE_URL.
func TestIntegrations(t *testing.T) {
	env := newCredEnvWith(t, credEnvOptions{integrations: true})
	ctx := context.Background()
	owner, err := env.auth.CreateUser(ctx, "ti@frota.test", "Frota TI", auth.RoleCustomer, userPassword)
	if err != nil {
		t.Fatal(err)
	}
	other, err := env.auth.CreateUser(ctx, "outra@frota.test", "Outra", auth.RoleCustomer, userPassword)
	if err != nil {
		t.Fatal(err)
	}
	dev, err := env.devices.Create(ctx, devices.Input{IMEIConfirmed: true, IMEI: "869247061230777", Protocol: "gt06"})
	if err != nil {
		t.Fatal(err)
	}
	truck, err := env.vehicles.Create(ctx, vehicles.Input{Name: "Caminhão 7", Plate: "CAM7A77", DeviceID: &dev.ID, OwnerID: &owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	otherCar, err := env.vehicles.Create(ctx, vehicles.Input{Name: "Carro de fora", OwnerID: &other.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := env.positions.Insert(ctx, &tracking.Position{DeviceID: dev.ID, GPSTimestamp: time.Now().Add(-time.Minute),
		ReceivedAt: time.Now(), Latitude: -23.5, Longitude: -46.6, SpeedKmh: 55}); err != nil {
		t.Fatal(err)
	}
	tok := env.tokenFor("ti@frota.test")

	var created struct {
		Key struct {
			ID     uuid.UUID `json:"id"`
			Prefix string    `json:"prefix"`
		} `json:"key"`
		Secret string `json:"secret"`
	}
	_ = json.Unmarshal(env.must(tok, http.MethodPost, "/api/me/api-keys", map[string]string{"name": "ERP da frota"}, http.StatusCreated), &created)
	if !strings.HasPrefix(created.Secret, "fk_"+created.Key.Prefix+"_") {
		t.Fatalf("chave = %+v", created)
	}
	api := func(key, path string, want int) []byte {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, env.srv.URL+path, nil)
		req.Header.Set("Authorization", "Bearer "+key)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		if res.StatusCode != want {
			t.Fatalf("GET %s = %d %s (quer %d)", path, res.StatusCode, body, want)
		}
		return body
	}
	var listed struct {
		Vehicles []struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			Position *struct {
				SpeedKmh float64 `json:"speedKmh"`
			} `json:"position"`
		} `json:"vehicles"`
	}
	_ = json.Unmarshal(api(created.Secret, "/api/v1/vehicles", http.StatusOK), &listed)
	if len(listed.Vehicles) != 1 || listed.Vehicles[0].Name != "Caminhão 7" || listed.Vehicles[0].Position == nil ||
		listed.Vehicles[0].Position.SpeedKmh != 55 {
		t.Fatalf("veículos = %s", api(created.Secret, "/api/v1/vehicles", http.StatusOK))
	}
	api(created.Secret, "/api/v1/vehicles/"+truck.ID.String()+"/positions", http.StatusOK)
	api(created.Secret, "/api/v1/vehicles/"+otherCar.ID.String()+"/positions", http.StatusNotFound)
	api(created.Secret[:len(created.Secret)-2]+"xx", "/api/v1/vehicles", http.StatusUnauthorized)
	api("", "/api/v1/vehicles", http.StatusUnauthorized)

	// Os eventos, pelo cursor.
	env.must(tok, http.MethodGet, "/api/me/webhooks", nil, http.StatusOK)
	ev := &events.Event{DeviceID: dev.ID, Type: events.IgnitionOn, Timestamp: time.Now()}
	if _, err := env.db.Exec(ctx, `INSERT INTO vehicle_events (device_id, type, timestamp) VALUES ($1, 'IGNITION_ON', NOW())`, dev.ID); err != nil {
		t.Fatal(err)
	}
	var evs struct {
		Events []struct {
			ID        int64  `json:"id"`
			Type      string `json:"type"`
			VehicleID string `json:"vehicleId"`
		} `json:"events"`
		Next int64 `json:"next"`
	}
	_ = json.Unmarshal(api(created.Secret, "/api/v1/events", http.StatusOK), &evs)
	if len(evs.Events) != 1 || evs.Events[0].VehicleID != truck.ID.String() || evs.Next != evs.Events[0].ID {
		t.Fatalf("eventos = %+v", evs)
	}
	_ = json.Unmarshal(api(created.Secret, "/api/v1/events?after="+strings.TrimSpace(jsonNumber(evs.Next)), http.StatusOK), &evs)
	if len(evs.Events) != 0 {
		t.Fatalf("depois do cursor = %+v", evs)
	}

	// O webhook: assinado, com o evento do veículo; o PING do teste.
	var mu sync.Mutex
	var got []string
	var signatures []string
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		got = append(got, string(body))
		signatures = append(signatures, r.Header.Get("X-Farbo-Signature")+"|"+r.Header.Get("X-Farbo-Timestamp"))
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer sink.Close()
	var hook struct {
		ID     uuid.UUID `json:"id"`
		Secret string    `json:"secret"`
	}
	_ = json.Unmarshal(env.must(tok, http.MethodPost, "/api/me/webhooks", map[string]any{"url": sink.URL + "/farbo", "events": []string{"ignition_on"}},
		http.StatusCreated), &hook)
	if !strings.HasPrefix(hook.Secret, "whsec_") {
		t.Fatalf("webhook = %+v", hook)
	}
	env.webhooks.Dispatch(ctx, ev)
	env.webhooks.Dispatch(ctx, &events.Event{DeviceID: dev.ID, Type: events.IgnitionOff, Timestamp: time.Now()}) // fora da lista
	mu.Lock()
	if len(got) != 1 || !strings.Contains(got[0], `"type":"IGNITION_ON"`) || !strings.Contains(got[0], `"plate":"CAM7A77"`) {
		t.Fatalf("recebido = %v", got)
	}
	parts := strings.SplitN(signatures[0], "|", 2)
	if parts[0] != webhooks.Sign(hook.Secret, parts[1], []byte(got[0])) {
		t.Fatalf("assinatura não confere: %s", signatures[0])
	}
	mu.Unlock()
	var tested struct {
		LastStatus *int `json:"lastStatus"`
	}
	_ = json.Unmarshal(env.must(tok, http.MethodPost, "/api/me/webhooks/"+hook.ID.String()+"/test", nil, http.StatusOK), &tested)
	if tested.LastStatus == nil || *tested.LastStatus != 204 {
		t.Fatalf("teste = %+v", tested)
	}

	// Revogada, para na hora.
	env.must(tok, http.MethodDelete, "/api/me/api-keys/"+created.Key.ID.String(), nil, http.StatusNoContent)
	api(created.Secret, "/api/v1/vehicles", http.StatusUnauthorized)
}

func jsonNumber(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
