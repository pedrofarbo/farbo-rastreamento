package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/affiliates"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
)

// A senha da página do afiliado: a central define (o hash não sai da API);
// sem a sessão da entrada, a página e o cadastro de recebedor pedem a senha;
// a senha errada não entra; a certa dá a sessão; trocar a senha derruba as
// sessões; tirar a senha volta ao link sozinho. Precisa de
// FARBO_TEST_DATABASE_URL.
func TestAffiliateReportPassword(t *testing.T) {
	env, _ := newLeadsEnv(t)
	admin := env.login(auth.RoleAdmin + "@leads.test")

	env.must(admin, http.MethodPost, "/api/affiliates", map[string]any{"name": "Curta", "active": true, "reportPassword": "curta"},
		http.StatusBadRequest)
	raw := env.must(admin, http.MethodPost, "/api/affiliates", map[string]any{
		"name": "Rui Representante", "code": "rui", "active": true, "reportPassword": "senha-do-rui-1",
	}, http.StatusCreated)
	var rui affiliates.Affiliate
	_ = json.Unmarshal(raw, &rui)
	if !rui.ReportPassword || strings.Contains(string(raw), "$2a$") || strings.Contains(string(raw), "senha-do-rui") {
		t.Fatalf("afiliado com senha = %s", raw)
	}
	page := "/api/public/partner/" + rui.ReportToken
	with := func(session string) map[string]string { return map[string]string{partnerSessionHeader: session} }

	// Sem a sessão: a página e o cadastro de recebedor pedem a senha.
	for _, call := range []struct{ method, path string }{
		{http.MethodGet, page}, {http.MethodPost, page + "/recipient"}, {http.MethodPost, page + "/recipient/kyc-link"},
	} {
		status, out := env.doWith("", call.method, call.path, map[string]any{}, with("123.abc"))
		if call.method == http.MethodGet {
			status, out = env.doWith("", call.method, call.path, nil, with("123.abc"))
		}
		if status != http.StatusUnauthorized || !containsCode(out, "PARTNER_PASSWORD") {
			t.Fatalf("%s %s sem a sessão = %d %s", call.method, call.path, status, out)
		}
	}
	// O QR Code (só o link de indicação, que é público) não pede.
	if status, _ := env.do("", http.MethodGet, page+"/qr?format=svg", nil); status != http.StatusOK {
		t.Errorf("QR Code = %d", status)
	}

	login := func(password string) (int, string) {
		t.Helper()
		status, out := env.do("", http.MethodPost, page+"/session", map[string]any{"password": password})
		var body struct {
			Session string `json:"session"`
		}
		_ = json.Unmarshal(out, &body)
		return status, body.Session
	}
	if status, _ := login("senha-errada"); status != http.StatusUnauthorized {
		t.Fatalf("senha errada = %d", status)
	}
	status, session := login("senha-do-rui-1")
	if status != http.StatusOK || session == "" {
		t.Fatalf("senha certa = %d %q", status, session)
	}
	if status, out := env.doWith("", http.MethodGet, page, nil, with(session)); status != http.StatusOK ||
		!strings.Contains(string(out), "Rui Representante") {
		t.Fatalf("com a sessão = %d %s", status, out)
	}
	// Outra página não aceita a sessão desta.
	other := env.must(admin, http.MethodPost, "/api/affiliates", map[string]any{
		"name": "Ana", "code": "ana", "active": true, "reportPassword": "senha-do-rui-1",
	}, http.StatusCreated)
	var ana affiliates.Affiliate
	_ = json.Unmarshal(other, &ana)
	if status, _ := env.doWith("", http.MethodGet, "/api/public/partner/"+ana.ReportToken, nil, with(session)); status != http.StatusUnauthorized {
		t.Errorf("sessão de outra página = %d", status)
	}

	// Editar sem mexer na senha: ela fica, e a sessão também.
	update := func(body map[string]any) {
		t.Helper()
		body["name"], body["code"], body["active"] = "Rui Representante", "rui", true
		env.must(admin, http.MethodPatch, "/api/affiliates/"+rui.ID.String(), body, http.StatusOK)
	}
	update(map[string]any{})
	if status, _ := env.doWith("", http.MethodGet, page, nil, with(session)); status != http.StatusOK {
		t.Errorf("depois de editar sem a senha = %d", status)
	}
	// Trocar a senha derruba a sessão.
	update(map[string]any{"reportPassword": "outra-senha-2"})
	if status, _ := env.doWith("", http.MethodGet, page, nil, with(session)); status != http.StatusUnauthorized {
		t.Errorf("sessão depois de trocar a senha = %d", status)
	}
	if status, _ := login("outra-senha-2"); status != http.StatusOK {
		t.Errorf("senha nova = %d", status)
	}
	// Sem a senha: o link basta.
	update(map[string]any{"reportPassword": ""})
	if status, _ := env.do("", http.MethodGet, page, nil); status != http.StatusOK {
		t.Errorf("sem a senha = %d", status)
	}
	if status, _ := login("outra-senha-2"); status != http.StatusBadRequest {
		t.Errorf("entrar numa página sem senha = %d", status)
	}
}
