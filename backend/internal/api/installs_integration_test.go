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

// A agenda de instalação de ponta a ponta: a central vê o veículo sem
// rastreador e agenda (um em aberto por veículo); o instalador abre o link
// sem login, marca que chegou, testa o IMEI (sem cadastro, de outro veículo,
// sem sinal) e, com o sinal e a foto, conclui — o rastreador fica ligado ao
// veículo e o link vira só leitura. Precisa de FARBO_TEST_DATABASE_URL.
func TestInstallationAgenda(t *testing.T) {
	env := newCredEnvWith(t, credEnvOptions{installs: true})
	pusher := &recordingPusher{}
	env.installs.SetPusher(pusher)
	ctx := context.Background()
	if _, err := env.auth.CreateUser(ctx, "op@agenda.test", "Operadora", auth.RoleOperator, userPassword); err != nil {
		t.Fatal(err)
	}
	customer, err := env.auth.CreateUser(ctx, "dona@agenda.test", "Dona Carla", auth.RoleCustomer, userPassword)
	if err != nil {
		t.Fatal(err)
	}
	car, err := env.vehicles.Create(ctx, vehicles.Input{Name: "HB20 da Carla", Plate: "AGD1A23", OwnerID: &customer.ID})
	if err != nil {
		t.Fatal(err)
	}
	busyDev, err := env.devices.Create(ctx, devices.Input{IMEIConfirmed: true, IMEI: "869247061230111", Protocol: "gt06"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.vehicles.Create(ctx, vehicles.Input{Name: "Outro", DeviceID: &busyDev.ID, OwnerID: &customer.ID}); err != nil {
		t.Fatal(err)
	}
	newDev, err := env.devices.Create(ctx, devices.Input{IMEIConfirmed: true, IMEI: "869247061230222", Protocol: "gt06"})
	if err != nil {
		t.Fatal(err)
	}
	op := env.tokenFor("op@agenda.test")
	tok := env.tokenFor("dona@agenda.test")

	var awaiting []struct {
		VehicleID uuid.UUID `json:"vehicleId"`
	}
	_ = json.Unmarshal(env.must(op, http.MethodGet, "/api/installations/awaiting", nil, http.StatusOK), &awaiting)
	if len(awaiting) != 1 || awaiting[0].VehicleID != car.ID {
		t.Fatalf("aguardando = %+v", awaiting)
	}
	env.must(tok, http.MethodGet, "/api/installations/awaiting", nil, http.StatusForbidden)

	when := time.Now().Add(2 * time.Hour)
	var appt struct {
		ID     uuid.UUID `json:"id"`
		URL    string    `json:"url"`
		Status string    `json:"status"`
	}
	_ = json.Unmarshal(env.must(op, http.MethodPost, "/api/installations", map[string]any{
		"vehicleId": car.ID, "installerName": "Zé Instalador", "scheduledFor": when, "address": "Rua das Flores, 10 - Centro",
	}, http.StatusCreated), &appt)
	if !strings.HasPrefix(appt.URL, "https://painel.farbo.test/instalador/") || appt.Status != "scheduled" {
		t.Fatalf("agendada = %+v", appt)
	}
	env.must(op, http.MethodPost, "/api/installations", map[string]any{
		"vehicleId": car.ID, "installerName": "Outro", "scheduledFor": when, "address": "Rua X",
	}, http.StatusBadRequest)
	if got := pusher.take(); len(got[customer.ID]) != 1 || !strings.HasPrefix(got[customer.ID][0].Title, "Instalação agendada") {
		t.Fatalf("aviso ao cliente = %+v", got)
	}
	var mine []struct{ ID uuid.UUID }
	_ = json.Unmarshal(env.must(tok, http.MethodGet, "/api/me/installations", nil, http.StatusOK), &mine)
	if len(mine) != 1 {
		t.Fatalf("instalações do cliente = %+v", mine)
	}

	// O instalador, pelo link.
	token := strings.TrimPrefix(appt.URL, "https://painel.farbo.test/instalador/")
	base := "/api/public/install/" + token
	var view struct {
		VehicleName  string `json:"vehicleName"`
		CustomerName string `json:"customerName"`
		Status       string `json:"status"`
	}
	_ = json.Unmarshal(env.must("", http.MethodGet, base, nil, http.StatusOK), &view)
	if view.VehicleName != "HB20 da Carla" || view.CustomerName != "Dona" {
		t.Fatalf("link = %+v", view)
	}
	env.must("", http.MethodGet, "/api/public/install/"+token[:len(token)-2]+"xx", nil, http.StatusGone)
	_ = json.Unmarshal(env.must("", http.MethodPost, base+"/arrive", nil, http.StatusOK), &view)
	if view.Status != "arrived" {
		t.Fatalf("chegou = %+v", view)
	}
	type check struct {
		Found, OtherVehicle, Ready bool
	}
	var c check
	_ = json.Unmarshal(env.must("", http.MethodPost, base+"/check", map[string]string{"imei": "000000000000000"}, http.StatusOK), &c)
	if c.Found {
		t.Fatalf("IMEI inexistente = %+v", c)
	}
	_ = json.Unmarshal(env.must("", http.MethodPost, base+"/check", map[string]string{"imei": busyDev.IMEI}, http.StatusOK), &c)
	if !c.OtherVehicle {
		t.Fatalf("IMEI de outro veículo = %+v", c)
	}
	_ = json.Unmarshal(env.must("", http.MethodPost, base+"/check", map[string]string{"imei": "8692 4706 1230 222"}, http.StatusOK), &c)
	if !c.Found || c.Ready {
		t.Fatalf("sem sinal = %+v", c)
	}
	env.must("", http.MethodPost, base+"/complete", map[string]string{"imei": newDev.IMEI}, http.StatusConflict)
	if err := env.positions.Insert(ctx, &tracking.Position{DeviceID: newDev.ID, GPSTimestamp: time.Now(), ReceivedAt: time.Now(),
		Latitude: -23.5, Longitude: -46.6}); err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(env.must("", http.MethodPost, base+"/check", map[string]string{"imei": newDev.IMEI}, http.StatusOK), &c)
	if !c.Ready {
		t.Fatalf("com sinal = %+v", c)
	}
	// Sem a foto do rastreador, não conclui.
	env.must("", http.MethodPost, base+"/complete", map[string]string{"imei": newDev.IMEI}, http.StatusBadRequest)
	if status, out := env.upload("", http.MethodPut, base+"/photos/tracker", jpegBytes, nil); status != http.StatusNoContent {
		t.Fatalf("foto: %d %s", status, out)
	}
	_ = json.Unmarshal(env.must("", http.MethodPost, base+"/complete", map[string]string{"imei": newDev.IMEI, "note": "Atrás do painel"},
		http.StatusOK), &view)
	if view.Status != "done" {
		t.Fatalf("concluída = %+v", view)
	}
	got, err := env.vehicles.Get(ctx, car.ID)
	if err != nil || got.DeviceID == nil || *got.DeviceID != newDev.ID {
		t.Fatalf("o veículo não ficou com o rastreador: %+v %v", got, err)
	}
	// Concluída: o link ainda abre para ver, mas não muda mais.
	env.must("", http.MethodGet, base, nil, http.StatusOK)
	env.must("", http.MethodPost, base+"/complete", map[string]string{"imei": newDev.IMEI}, http.StatusBadRequest)
	if status, _ := env.upload("", http.MethodPut, base+"/photos/vehicle", jpegBytes, nil); status != http.StatusConflict {
		t.Fatalf("foto depois de concluir: %d", status)
	}
	env.must(op, http.MethodGet, "/api/installations/"+appt.ID.String()+"/photos/tracker", nil, http.StatusOK)
}
