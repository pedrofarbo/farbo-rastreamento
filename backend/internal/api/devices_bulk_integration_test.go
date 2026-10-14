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
)

func TestDevicesBulk(t *testing.T) {
	env := newCredEnv(t)
	ctx := context.Background()
	authSvc := auth.NewService(auth.NewRepository(env.db), config.Auth{BcryptCost: bcrypt.MinCost}, nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)))

	for _, role := range []string{auth.RoleAdmin, auth.RoleOperator} {
		if _, err := authSvc.CreateUser(ctx, role+"@bulk.test", role, role, userPassword); err != nil {
			t.Fatal(err)
		}
	}
	admin := env.login(auth.RoleAdmin + "@bulk.test")
	operator := env.login(auth.RoleOperator + "@bulk.test")

	// Operador não tem permissão de criar em massa (403)
	env.must(operator, http.MethodPost, "/api/devices/bulk", map[string]any{
		"devices": []map[string]any{
			{"imei": "869247061230001"},
		},
	}, http.StatusForbidden)

	// Admin cria 2 rastreadores válidos
	var resp bulkDevicesResponse
	out := env.must(admin, http.MethodPost, "/api/devices/bulk", map[string]any{
		"devices": []map[string]any{
			{
				"imei": "869247061230011", "imeiConfirmed": true,
				"phoneNumber": "(34) 99999-0011",
				"model":       "TK910",
				"protocol":    "gt06",
			},
			{
				"imei": "869247061230022", "imeiConfirmed": true,
				"phoneNumber": "(34) 99999-0022",
				"model":       "Coban",
			},
		},
	}, http.StatusCreated)
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("unmarshal bulk response: %v", err)
	}

	if resp.Total != 2 || resp.Created != 2 || len(resp.Errors) != 0 || len(resp.Devices) != 2 {
		t.Fatalf("resposta inesperada: %+v", resp)
	}
	if resp.Devices[0].IMEI != "869247061230011" || resp.Devices[0].PhoneNumber != "(34) 99999-0011" {
		t.Errorf("device 0 incompatível: %+v", resp.Devices[0])
	}
	if resp.Devices[1].IMEI != "869247061230022" || resp.Devices[1].PhoneNumber != "(34) 99999-0022" {
		t.Errorf("device 1 incompatível: %+v", resp.Devices[1])
	}

	// Criar novamente: um duplicado e um novo válido
	out2 := env.must(admin, http.MethodPost, "/api/devices/bulk", map[string]any{
		"devices": []map[string]any{
			{
				"imei": "869247061230011", "imeiConfirmed": true, // já existe
			},
			{
				"imei": "869247061230033", "imeiConfirmed": true, // novo
				"phoneNumber": "34999990033",
			},
			{
				"imei": "123", // inválido (menos de 15 dígitos)
			},
		},
	}, http.StatusCreated)

	var resp2 bulkDevicesResponse
	if err := json.Unmarshal(out2, &resp2); err != nil {
		t.Fatalf("unmarshal bulk response 2: %v", err)
	}
	if resp2.Total != 3 || resp2.Created != 1 || len(resp2.Errors) != 2 {
		t.Fatalf("resposta parcial inesperada: %+v", resp2)
	}
	if resp2.Devices[0].IMEI != "869247061230033" {
		t.Errorf("device criado incorreto: %+v", resp2.Devices[0])
	}

	// Todos com erro
	out3 := env.must(admin, http.MethodPost, "/api/devices/bulk", map[string]any{
		"devices": []map[string]any{
			{"imei": "111"},
		},
	}, http.StatusOK)
	var resp3 bulkDevicesResponse
	if err := json.Unmarshal(out3, &resp3); err != nil {
		t.Fatalf("unmarshal bulk response 3: %v", err)
	}
	if resp3.Created != 0 || len(resp3.Errors) != 1 {
		t.Fatalf("esperava 0 criados e 1 erro: %+v", resp3)
	}
}
