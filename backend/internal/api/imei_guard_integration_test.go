package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/devices"
)

// O caso real de 13/10/2026: o rastreador 865893088121639 cadastrado como
// 8658930088121639. O cadastro agora só aceita o IMEI suspeito conferido; o
// rastreador que tenta conectar sem cadastro fica anotado, com o cadastro de
// IMEI parecido; corrigido o cadastro, sai da lista. Precisa de
// FARBO_TEST_DATABASE_URL.
func TestIMEIGuardAndUnknownAttempts(t *testing.T) {
	var devs *devices.Service
	env, _ := newLeadsEnv(t, func(d *Deps) { devs = d.Devices })
	ctx := context.Background()
	admin := env.login(auth.RoleAdmin + "@leads.test")

	// O IMEI com um zero a mais: recusado, com o código para a tela avisar.
	status, body := env.do(admin, http.MethodPost, "/api/devices", map[string]any{"imei": "8658930088121639", "protocol": "gt06"})
	if status != http.StatusBadRequest || !strings.Contains(string(body), `"code":"IMEI_SUSPECT"`) ||
		!strings.Contains(string(body), "16 dígitos") {
		t.Fatalf("IMEI de 16 dígitos = %d %s", status, body)
	}
	// 15 dígitos com o verificador errado também.
	status, body = env.do(admin, http.MethodPost, "/api/devices", map[string]any{"imei": "865893008812163", "protocol": "gt06"})
	if status != http.StatusBadRequest || !strings.Contains(string(body), "o último dígito") {
		t.Fatalf("verificador errado = %d %s", status, body)
	}
	// Conferido na etiqueta, passa (e um IMEI certo passa sem confirmar).
	var wrong devices.View
	_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/devices", map[string]any{
		"imei": "8658930088121639", "protocol": "gt06", "imeiConfirmed": true,
	}, http.StatusCreated), &wrong)
	env.must(admin, http.MethodPost, "/api/devices", map[string]any{"imei": "869999000000102", "protocol": "gt06"}, http.StatusCreated)
	// Editar o suspeito sem confirmar de novo: o aviso volta.
	if status, body := env.do(admin, http.MethodPatch, "/api/devices/"+wrong.ID.String(),
		map[string]any{"imei": "8658930088121639", "protocol": "gt06"}); status != http.StatusBadRequest ||
		!strings.Contains(string(body), "IMEI_SUSPECT") {
		t.Errorf("edição sem confirmar = %d %s", status, body)
	}

	// O rastreador de verdade tenta conectar: anotado, com o cadastro parecido.
	for range 3 {
		if err := devs.RecordUnknown(ctx, "865893088121639", "gt06", "187.32.97.125:58772"); err != nil {
			t.Fatal(err)
		}
	}
	env.must(env.login(auth.RoleOperator+"@leads.test"), http.MethodGet, "/api/devices/unknown", nil, http.StatusForbidden)
	var list []devices.UnknownAttempt
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/devices/unknown", nil, http.StatusOK), &list)
	// As tentativas dentro de 30 s vão juntas na próxima gravação: por ora, 1.
	if len(list) != 1 || list[0].IMEI != "865893088121639" || list[0].Attempts != 1 || list[0].Protocol != "gt06" ||
		list[0].RemoteAddr != "187.32.97.125:58772" || list[0].Suggestion == nil ||
		list[0].Suggestion.IMEI != "8658930088121639" || list[0].Suggestion.DeviceID != wrong.ID {
		t.Fatalf("tentativas = %+v (sugestão %+v)", list, list[0].Suggestion)
	}

	// Corrigido o cadastro, o rastreador deixa de ser desconhecido.
	env.must(admin, http.MethodPatch, "/api/devices/"+wrong.ID.String(),
		map[string]any{"imei": "865893088121639", "protocol": "gt06"}, http.StatusOK)
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/devices/unknown", nil, http.StatusOK), &list)
	if len(list) != 0 {
		t.Errorf("depois de corrigir = %+v", list)
	}
}
