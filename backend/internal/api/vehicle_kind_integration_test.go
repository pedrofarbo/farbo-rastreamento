package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/vehicles"
)

// O tipo do veículo (carro, moto ou caminhão), que o mapa desenha: carro por
// padrão, o escolhido quando vem, mantido numa edição que não manda o tipo e
// recusado fora dos três. Precisa de FARBO_TEST_DATABASE_URL.
func TestVehicleKind(t *testing.T) {
	env := newCredEnv(t)
	authSvc := auth.NewService(auth.NewRepository(env.db), config.Auth{BcryptCost: bcrypt.MinCost}, nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, err := authSvc.CreateUser(context.Background(), "admin@tipo.test", "Admin", auth.RoleAdmin, userPassword); err != nil {
		t.Fatal(err)
	}
	admin := env.login("admin@tipo.test")

	var car, moto, truck vehicles.Vehicle
	_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/vehicles", map[string]any{"name": "Subaru"}, http.StatusCreated), &car)
	_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/vehicles", map[string]any{"name": "CG", "kind": "motorcycle"}, http.StatusCreated), &moto)
	_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/vehicles", map[string]any{"name": "FH 540", "kind": "truck"}, http.StatusCreated), &truck)
	if car.Kind != vehicles.KindCar || moto.Kind != vehicles.KindMotorcycle || truck.Kind != vehicles.KindTruck {
		t.Fatalf("tipos = %q, %q, %q", car.Kind, moto.Kind, truck.Kind)
	}
	env.must(admin, http.MethodPost, "/api/vehicles", map[string]any{"name": "Trator", "kind": "TRACTOR"}, http.StatusBadRequest)

	// Editar sem o tipo (tela antiga) não transforma a moto em carro.
	var edited vehicles.Vehicle
	_ = json.Unmarshal(env.must(admin, http.MethodPatch, "/api/vehicles/"+moto.ID.String(), map[string]any{"name": "CG 160"}, http.StatusOK), &edited)
	if edited.Kind != vehicles.KindMotorcycle || edited.Name != "CG 160" {
		t.Errorf("editada sem o tipo = %+v", edited)
	}
	_ = json.Unmarshal(env.must(admin, http.MethodPatch, "/api/vehicles/"+moto.ID.String(), map[string]any{"name": "CG 160", "kind": "CAR"}, http.StatusOK), &edited)
	if edited.Kind != vehicles.KindCar {
		t.Errorf("trocada para carro = %q", edited.Kind)
	}
}
