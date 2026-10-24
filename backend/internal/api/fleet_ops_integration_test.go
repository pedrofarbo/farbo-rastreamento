package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/mail"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/vehicles"
)

type recordingFleetNotifier struct {
	mu   sync.Mutex
	sent []string
}

func (n *recordingFleetNotifier) RentalOverdue(_ context.Context, to, _ string, r mail.RentalNotice) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.sent = append(n.sent, to+": "+r.VehicleName+" — "+r.RenterName)
	return nil
}

// A gestão da frota de ponta a ponta: o motorista no veículo; os custos e o
// consumo de um tanque cheio ao outro; a multa com o motorista da hora; a
// locação (uma em aberto por veículo), o aviso de atraso uma vez e a
// devolução; e ninguém de fora mexe. Precisa de FARBO_TEST_DATABASE_URL.
func TestFleetOperations(t *testing.T) {
	notifier := &recordingFleetNotifier{}
	env := newCredEnvWith(t, credEnvOptions{fleet: true, fleetOps: notifier})
	ctx := context.Background()
	owner, err := env.auth.CreateUser(ctx, "frota@onibus.test", "Viação Serra", auth.RoleCustomer, userPassword)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.auth.CreateUser(ctx, "outro@cliente.test", "Outro", auth.RoleCustomer, userPassword); err != nil {
		t.Fatal(err)
	}
	bus, err := env.vehicles.Create(ctx, vehicles.Input{Name: "Ônibus 12", Plate: "BUS1A12", OwnerID: &owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	van, err := env.vehicles.Create(ctx, vehicles.Input{Name: "Van 3", Plate: "VAN3B33", OwnerID: &owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	tok, outro := env.tokenFor("frota@onibus.test"), env.tokenFor("outro@cliente.test")

	// O motorista e o veículo dele.
	var driver struct {
		ID          uuid.UUID `json:"id"`
		VehicleName string    `json:"vehicleName"`
	}
	env.must(tok, http.MethodPost, "/api/me/drivers", map[string]any{"name": ""}, http.StatusBadRequest)
	_ = json.Unmarshal(env.must(tok, http.MethodPost, "/api/me/drivers", map[string]any{
		"name": "José Motorista", "licenseNumber": "01234567890", "licenseCategory": "d", "licenseExpires": "2028-05-10",
	}, http.StatusCreated), &driver)
	env.must(tok, http.MethodPut, "/api/me/vehicles/"+bus.ID.String()+"/driver", map[string]any{"driverId": driver.ID}, http.StatusNoContent)
	env.must(outro, http.MethodPut, "/api/me/vehicles/"+bus.ID.String()+"/driver", map[string]any{"driverId": driver.ID}, http.StatusNotFound)
	var drivers []struct {
		VehicleName     string `json:"vehicleName"`
		LicenseCategory string `json:"licenseCategory"`
	}
	_ = json.Unmarshal(env.must(tok, http.MethodGet, "/api/me/drivers", nil, http.StatusOK), &drivers)
	if len(drivers) != 1 || drivers[0].VehicleName != "Ônibus 12" || drivers[0].LicenseCategory != "D" {
		t.Fatalf("motoristas = %+v", drivers)
	}
	// Trocar de veículo encerra o anterior.
	env.must(tok, http.MethodPut, "/api/me/vehicles/"+van.ID.String()+"/driver", map[string]any{"driverId": driver.ID}, http.StatusNoContent)
	_ = json.Unmarshal(env.must(tok, http.MethodGet, "/api/me/drivers", nil, http.StatusOK), &drivers)
	if drivers[0].VehicleName != "Van 3" {
		t.Fatalf("depois da troca = %+v", drivers)
	}

	// Os custos: dois tanques cheios (500 km com 100 l = 5 km/l) e um pedágio.
	today := time.Now().UTC().Format(time.DateOnly)
	for _, e := range []map[string]any{
		{"vehicleId": bus.ID, "kind": "fuel", "spentOn": today, "amountCents": 60000, "liters": 100, "odometerKm": 10000, "fullTank": true},
		{"vehicleId": bus.ID, "kind": "fuel", "spentOn": today, "amountCents": 30000, "liters": 50, "odometerKm": 10200},
		{"vehicleId": bus.ID, "kind": "fuel", "spentOn": today, "amountCents": 30000, "liters": 50, "odometerKm": 10500, "fullTank": true},
		{"vehicleId": bus.ID, "kind": "toll", "spentOn": today, "amountCents": 1250, "description": "Pedágio"},
	} {
		env.must(tok, http.MethodPost, "/api/me/expenses", e, http.StatusCreated)
	}
	env.must(tok, http.MethodPost, "/api/me/expenses", map[string]any{"vehicleId": bus.ID, "kind": "fuel", "spentOn": today, "amountCents": 0},
		http.StatusBadRequest)
	env.must(outro, http.MethodPost, "/api/me/expenses", map[string]any{"vehicleId": bus.ID, "kind": "toll", "spentOn": today, "amountCents": 100},
		http.StatusNotFound)
	var costs struct {
		Rows []struct {
			VehicleID  uuid.UUID        `json:"vehicleId"`
			TotalCents int64            `json:"totalCents"`
			ByKind     map[string]int64 `json:"byKind"`
			KmPerLiter *float64         `json:"kmPerLiter"`
			Liters     float64          `json:"liters"`
		} `json:"rows"`
	}
	_ = json.Unmarshal(env.must(tok, http.MethodGet, "/api/reports/costs", nil, http.StatusOK), &costs)
	var busCost *struct {
		VehicleID  uuid.UUID        `json:"vehicleId"`
		TotalCents int64            `json:"totalCents"`
		ByKind     map[string]int64 `json:"byKind"`
		KmPerLiter *float64         `json:"kmPerLiter"`
		Liters     float64          `json:"liters"`
	}
	for i := range costs.Rows {
		if costs.Rows[i].VehicleID == bus.ID {
			busCost = &costs.Rows[i]
		}
	}
	if busCost == nil || busCost.TotalCents != 121250 || busCost.ByKind["fuel"] != 120000 || busCost.Liters != 200 ||
		busCost.KmPerLiter == nil || *busCost.KmPerLiter != 5 {
		t.Fatalf("custos = %+v", costs)
	}

	// A multa: o motorista é quem dirigia a van na hora.
	var fine struct {
		ID         uuid.UUID `json:"id"`
		DriverName string    `json:"driverName"`
		Status     string    `json:"status"`
	}
	_ = json.Unmarshal(env.must(tok, http.MethodPost, "/api/me/fines", map[string]any{
		"vehicleId": van.ID, "occurredAt": time.Now(), "description": "Velocidade até 20% acima",
		"code": "745-50", "amountCents": 13016, "points": 4,
	}, http.StatusCreated), &fine)
	if fine.DriverName != "José Motorista" || fine.Status != "pending" {
		t.Fatalf("multa = %+v", fine)
	}
	_ = json.Unmarshal(env.must(tok, http.MethodPatch, "/api/me/fines/"+fine.ID.String(), map[string]any{
		"occurredAt": time.Now().Add(-time.Minute), "description": "Velocidade até 20% acima", "amountCents": 13016, "points": 4,
		"status": "paid", "driverIndicated": true,
	}, http.StatusOK), &fine)
	if fine.Status != "paid" {
		t.Fatalf("multa paga = %+v", fine)
	}
	env.must(outro, http.MethodDelete, "/api/me/fines/"+fine.ID.String(), nil, http.StatusNotFound)

	// A locação: uma em aberto por veículo; atrasada, avisa uma vez; devolvida.
	clock := time.Now()
	env.fleetOps.SetClock(func() time.Time { return clock })
	var rental struct {
		ID      uuid.UUID `json:"id"`
		Overdue bool      `json:"overdue"`
		KmStart *float64  `json:"kmStart"`
		KmEnd   *float64  `json:"kmEnd"`
	}
	_ = json.Unmarshal(env.must(tok, http.MethodPost, "/api/me/rentals", map[string]any{
		"vehicleId": bus.ID, "renterName": "Turismo Alegria", "renterPhone": "11999990000",
		"startsAt": clock.Add(-2 * time.Hour), "endsAt": clock.Add(time.Hour), "kmStart": 10500,
	}, http.StatusCreated), &rental)
	env.must(tok, http.MethodPost, "/api/me/rentals", map[string]any{
		"vehicleId": bus.ID, "renterName": "Outra", "startsAt": clock, "endsAt": clock.Add(time.Hour),
	}, http.StatusBadRequest)
	if n, err := env.fleetOps.NotifyOverdue(ctx); err != nil || n != 0 {
		t.Fatalf("antes da hora: %d %v", n, err)
	}
	clock = clock.Add(2 * time.Hour)
	if n, err := env.fleetOps.NotifyOverdue(ctx); err != nil || n != 1 || notifier.sent[0] != "frota@onibus.test: Ônibus 12 — Turismo Alegria" {
		t.Fatalf("atrasada: %d %v %v", n, err, notifier.sent)
	}
	if n, _ := env.fleetOps.NotifyOverdue(ctx); n != 0 {
		t.Fatalf("avisou de novo: %d", n)
	}
	var rentals []struct {
		Overdue bool `json:"overdue"`
	}
	_ = json.Unmarshal(env.must(tok, http.MethodGet, "/api/me/rentals", nil, http.StatusOK), &rentals)
	if len(rentals) != 1 || !rentals[0].Overdue {
		t.Fatalf("locações = %+v", rentals)
	}
	env.must(tok, http.MethodPost, "/api/me/rentals/"+rental.ID.String()+"/return", map[string]any{"kmEnd": 100}, http.StatusBadRequest)
	_ = json.Unmarshal(env.must(tok, http.MethodPost, "/api/me/rentals/"+rental.ID.String()+"/return", map[string]any{"kmEnd": 10820},
		http.StatusOK), &rental)
	if rental.Overdue || rental.KmEnd == nil || *rental.KmEnd != 10820 {
		t.Fatalf("devolvida = %+v", rental)
	}

	// A jornada (sem posições: nenhuma) responde para a conta.
	var journey struct {
		Rows                   []any `json:"rows"`
		ContinuousLimitSeconds int   `json:"continuousLimitSeconds"`
	}
	_ = json.Unmarshal(env.must(tok, http.MethodGet, "/api/reports/journey?tipo=passageiros", nil, http.StatusOK), &journey)
	if len(journey.Rows) != 0 || journey.ContinuousLimitSeconds != 4*3600 {
		t.Fatalf("jornada = %+v", journey)
	}
}
