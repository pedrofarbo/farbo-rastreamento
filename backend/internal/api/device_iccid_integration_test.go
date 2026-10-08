package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/devices"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/vehicles"
)

// O ICCID do chip no cadastro do rastreador: gravado só com os dígitos (a
// marcação "SP" da etiqueta sai), um chip por rastreador, visível para a
// equipe e nunca para o cliente. Precisa de FARBO_TEST_DATABASE_URL.
func TestDeviceICCID(t *testing.T) {
	env := newCredEnv(t)
	ctx := context.Background()
	authSvc := auth.NewService(auth.NewRepository(env.db), config.Auth{BcryptCost: bcrypt.MinCost}, nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	users := map[string]*auth.User{}
	for _, role := range []string{auth.RoleAdmin, auth.RoleOperator, auth.RoleCustomer} {
		u, err := authSvc.CreateUser(ctx, role+"@iccid.test", role, role, userPassword)
		if err != nil {
			t.Fatal(err)
		}
		users[role] = u
	}
	admin := env.login(auth.RoleAdmin + "@iccid.test")

	var dev devices.View
	_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/devices", map[string]any{
		"imei": "869247061230033", "protocol": "gt06", "phoneNumber": "+5534999990000", "iccid": "89553202100093795330SP",
	}, http.StatusCreated), &dev)
	if dev.ICCID != "89553202100093795330" {
		t.Fatalf("ICCID gravado = %q", dev.ICCID)
	}

	// Dígito errado: recusado, com o motivo.
	if out := env.must(admin, http.MethodPost, "/api/devices", map[string]any{
		"imei": "869247061230044", "iccid": "89553202100093795331",
	}, http.StatusBadRequest); !strings.Contains(string(out), "último dígito não confere") {
		t.Errorf("dígito errado: %s", out)
	}
	// O mesmo chip em outro rastreador: recusado, dizendo onde está.
	if out := env.must(admin, http.MethodPost, "/api/devices", map[string]any{
		"imei": "869247061230044", "iccid": "8955 3202 1000 9379 5330",
	}, http.StatusBadRequest); !strings.Contains(string(out), "já está no rastreador de IMEI 869247061230033") {
		t.Errorf("chip repetido: %s", out)
	}
	// Editar o próprio rastreador com o mesmo chip continua valendo.
	path := "/api/devices/" + dev.ID.String()
	_ = json.Unmarshal(env.must(admin, http.MethodPatch, path, map[string]any{
		"imei": "869247061230033", "protocol": "gt06", "phoneNumber": "+5534999990000", "iccid": "89553202100093795330",
		"notes": "chip da Algar",
	}, http.StatusOK), &dev)
	if dev.ICCID != "89553202100093795330" || dev.Notes != "chip da Algar" {
		t.Errorf("editado = %+v", dev)
	}

	// A equipe vê; o cliente, dono do veículo, não.
	var seen devices.View
	_ = json.Unmarshal(env.must(env.login(auth.RoleOperator+"@iccid.test"), http.MethodGet, path, nil, http.StatusOK), &seen)
	if seen.ICCID != "89553202100093795330" {
		t.Errorf("operador vê ICCID %q", seen.ICCID)
	}
	if _, err := env.vehicles.Create(ctx, vehicles.Input{Name: "Moto", DeviceID: &dev.ID, OwnerID: &users[auth.RoleCustomer].ID}); err != nil {
		t.Fatal(err)
	}
	if err := env.owners.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	out := env.must(env.login(auth.RoleCustomer+"@iccid.test"), http.MethodGet, "/api/vehicles", nil, http.StatusOK)
	if strings.Contains(string(out), "8955320210009379533") || !strings.Contains(string(out), "869247061230033") {
		t.Errorf("o cliente vê o ICCID (ou não vê o rastreador): %s", out)
	}

	// Sem ICCID, o chip fica livre para outro rastreador.
	env.must(admin, http.MethodPatch, path, map[string]any{"imei": "869247061230033", "iccid": ""}, http.StatusOK)
	_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/devices", map[string]any{
		"imei": "869247061230044", "iccid": "89553202100093795330",
	}, http.StatusCreated), &dev)
	if dev.ICCID != "89553202100093795330" {
		t.Errorf("chip reaproveitado = %q", dev.ICCID)
	}
}
