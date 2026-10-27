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
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/leakcheck"
)

// O estoque de chips: o cadastro (ICCID conferido, sem repetir, senha do APN
// nunca de volta), a escolha no cadastro do rastreador (a linha, o APN e a
// senha vão para ele; o chip passa a "em uso" e não vai para outro), o chip
// digitado no rastreador entrando no estoque sozinho, a correção do chip
// chegando ao rastreador e a retirada só do livre. Só o administrador.
// Precisa de FARBO_TEST_DATABASE_URL.
func TestChips(t *testing.T) {
	env := newCredEnv(t)
	ctx := context.Background()
	authSvc := auth.NewService(auth.NewRepository(env.db), config.Auth{BcryptCost: bcrypt.MinCost}, nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	for _, role := range []string{auth.RoleAdmin, auth.RoleOperator} {
		if _, err := authSvc.CreateUser(ctx, role+"@chips.test", role, role, userPassword); err != nil {
			t.Fatal(err)
		}
	}
	admin := env.login(auth.RoleAdmin + "@chips.test")
	env.must(env.login(auth.RoleOperator+"@chips.test"), http.MethodGet, "/api/chips", nil, http.StatusForbidden)

	call := func(method, path string, body any, want int) []byte {
		t.Helper()
		out := env.must(admin, method, path, body, want)
		if found := leakcheck.Find(out, "S3nhaChip"); len(found) > 0 {
			t.Fatalf("%s %s mostra a senha do APN: %v", method, path, found)
		}
		return out
	}
	var chip devices.Chip
	_ = json.Unmarshal(call(http.MethodPost, "/api/chips", map[string]any{
		"iccid": "8955 3202 1000 9379 5348 SP", "phoneNumber": "(34) 99999-0001", "carrier": "Algar",
		"apn": "ss.algar.br", "apnUser": "algar", "apnPassword": "S3nhaChip",
	}, http.StatusCreated), &chip)
	if chip.ICCID != "89553202100093795348" || !chip.APNPasswordSet || chip.Device != nil || chip.Carrier != "Algar" {
		t.Fatalf("chip = %+v", chip)
	}
	for body, want := range map[string]string{
		`{"iccid": "89553202100093795348"}`:               "já está cadastrado",
		`{"iccid": "89553202100093795349"}`:               "último dígito não confere",
		`{"iccid": ""}`:                                   "informe o ICCID",
		`{"iccid": "89553202100093795355", "apn": "a,b"}`: "vírgula",
	} {
		var in map[string]any
		_ = json.Unmarshal([]byte(body), &in)
		if out := call(http.MethodPost, "/api/chips", in, http.StatusBadRequest); !strings.Contains(string(out), want) {
			t.Errorf("%s: %s", body, out)
		}
	}

	// Escolhido no cadastro do rastreador: os dados vêm do chip (os digitados não valem).
	var dev devices.Device
	_ = json.Unmarshal(call(http.MethodPost, "/api/devices", map[string]any{
		"imei": "869247061230055", "imeiConfirmed": true, "chipId": chip.ID, "phoneNumber": "11 90000-0000", "iccid": "",
	}, http.StatusCreated), &dev)
	stored, err := env.devices.Get(ctx, dev.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ICCID != "89553202100093795348" || stored.PhoneNumber != "(34) 99999-0001" || stored.APN != "ss.algar.br" ||
		stored.APNUser != "algar" || stored.APNPassword != "S3nhaChip" {
		t.Fatalf("rastreador com o chip = %+v", stored)
	}
	var list []devices.Chip
	_ = json.Unmarshal(call(http.MethodGet, "/api/chips", nil, http.StatusOK), &list)
	if len(list) != 1 || list[0].Device == nil || list[0].Device.IMEI != "869247061230055" {
		t.Fatalf("estoque = %+v", list)
	}
	// Em uso: não vai para outro rastreador, nem sai do estoque.
	if out := call(http.MethodPost, "/api/devices", map[string]any{"imei": "869247061230066", "imeiConfirmed": true, "chipId": chip.ID},
		http.StatusBadRequest); !strings.Contains(string(out), "já está no rastreador de IMEI 869247061230055") {
		t.Errorf("chip em uso escolhido de novo: %s", out)
	}
	if out := call(http.MethodDelete, "/api/chips/"+chip.ID.String(), nil, http.StatusBadRequest); !strings.Contains(string(out), "tire-o do rastreador") {
		t.Errorf("apagar em uso: %s", out)
	}

	// Corrigir o chip corrige o rastreador (para lá vão os SMS); senha em branco mantém a atual.
	_ = json.Unmarshal(call(http.MethodPatch, "/api/chips/"+chip.ID.String(), map[string]any{
		"iccid": "89553202100093795348", "phoneNumber": "(34) 98888-0002", "carrier": "Algar", "apn": "ss.algar.br", "apnUser": "algar",
	}, http.StatusOK), &chip)
	if stored, _ = env.devices.Get(ctx, dev.ID); stored.PhoneNumber != "(34) 98888-0002" || stored.APNPassword != "S3nhaChip" || !chip.APNPasswordSet {
		t.Errorf("depois de corrigir o chip: rastreador %q senha %q", stored.PhoneNumber, stored.APNPassword)
	}

	// O chip digitado no rastreador entra no estoque sozinho.
	call(http.MethodPost, "/api/devices", map[string]any{
		"imei": "869247061230077", "imeiConfirmed": true, "iccid": "89553202100093795363", "phoneNumber": "(34) 97777-0003", "apn": "zap.vivo.com.br",
	}, http.StatusCreated)
	_ = json.Unmarshal(call(http.MethodGet, "/api/chips", nil, http.StatusOK), &list)
	var typed *devices.Chip
	for i := range list {
		if list[i].ICCID == "89553202100093795363" {
			typed = &list[i]
		}
	}
	if len(list) != 2 || typed == nil || typed.PhoneNumber != "(34) 97777-0003" || typed.APN != "zap.vivo.com.br" ||
		typed.Device == nil || typed.Device.IMEI != "869247061230077" {
		t.Fatalf("chip digitado no rastreador = %+v", list)
	}

	// Tirado do rastreador (ICCID apagado), fica livre e sai do estoque.
	call(http.MethodPatch, "/api/devices/"+dev.ID.String(), map[string]any{"imei": "869247061230055", "imeiConfirmed": true, "iccid": ""}, http.StatusOK)
	call(http.MethodDelete, "/api/chips/"+chip.ID.String(), nil, http.StatusNoContent)
	_ = json.Unmarshal(call(http.MethodGet, "/api/chips", nil, http.StatusOK), &list)
	if len(list) != 1 {
		t.Errorf("depois de apagar = %+v", list)
	}
}

// TestChipsBulk: a importação de um lote de chips — os bons entram, os
// recusados voltam com o motivo e a posição; o limite por envio. Precisa de
// FARBO_TEST_DATABASE_URL.
func TestChipsBulk(t *testing.T) {
	env := newCredEnv(t)
	ctx := context.Background()
	authSvc := auth.NewService(auth.NewRepository(env.db), config.Auth{BcryptCost: bcrypt.MinCost}, nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	for _, role := range []string{auth.RoleAdmin, auth.RoleOperator} {
		if _, err := authSvc.CreateUser(ctx, role+"@lote.test", role, role, userPassword); err != nil {
			t.Fatal(err)
		}
	}
	admin := env.login(auth.RoleAdmin + "@lote.test")
	env.must(env.login(auth.RoleOperator+"@lote.test"), http.MethodPost, "/api/chips/bulk", map[string]any{"chips": []any{}}, http.StatusForbidden)
	env.must(admin, http.MethodPost, "/api/chips", map[string]any{"iccid": "89553202100093795330"}, http.StatusCreated)

	lote := func(iccid string) map[string]any {
		return map[string]any{"iccid": iccid, "carrier": "Algar", "apn": "ss.algar.br", "apnUser": "algar", "apnPassword": "algar", "notes": "Lote 12"}
	}
	var res struct {
		Total   int `json:"total"`
		Created int `json:"created"`
		Errors  []struct {
			Index int    `json:"index"`
			ICCID string `json:"iccid"`
			Error string `json:"error"`
		} `json:"errors"`
	}
	_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/chips/bulk", map[string]any{"chips": []any{
		lote("89553202100093795348"), lote("89553202100093795330"), lote("89553202100093795349"), lote("8955 3202 1000 9379 5355 SP"),
	}}, http.StatusCreated), &res)
	if res.Total != 4 || res.Created != 2 || len(res.Errors) != 2 ||
		res.Errors[0].Index != 1 || !strings.Contains(res.Errors[0].Error, "já está cadastrado") ||
		res.Errors[1].Index != 2 || !strings.Contains(res.Errors[1].Error, "último dígito não confere") {
		t.Fatalf("lote = %+v", res)
	}
	var list []devices.Chip
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/chips", nil, http.StatusOK), &list)
	var fromLot int
	for _, c := range list {
		if c.Notes == "Lote 12" && c.Carrier == "Algar" && c.APN == "ss.algar.br" && c.APNPasswordSet {
			fromLot++
		}
	}
	if len(list) != 3 || fromLot != 2 {
		t.Errorf("estoque = %+v", list)
	}

	big := make([]any, 1001)
	for i := range big {
		big[i] = lote("89553202100093795363")
	}
	env.must(admin, http.MethodPost, "/api/chips/bulk", map[string]any{"chips": big}, http.StatusBadRequest)
	env.must(admin, http.MethodPost, "/api/chips/bulk", map[string]any{"chips": []any{}}, http.StatusBadRequest)
}
