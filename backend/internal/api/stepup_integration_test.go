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
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/devices"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/vehicles"
)

// O cliente só desliga o motor com a confirmação (biometria ou senha); a
// equipe segue como antes. Precisa de FARBO_TEST_DATABASE_URL.
func TestEngineCutNeedsStepUpForCustomer(t *testing.T) {
	env := newCredEnvWith(t, credEnvOptions{stepUp: true})
	ctx := context.Background()
	authSvc := auth.NewService(auth.NewRepository(env.db), config.Auth{BcryptCost: bcrypt.MinCost}, nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	ana, err := authSvc.CreateUser(ctx, "ana@stepup-api.test", "Ana", auth.RoleCustomer, userPassword)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authSvc.CreateUser(ctx, "admin@stepup-api.test", "Admin", auth.RoleAdmin, userPassword); err != nil {
		t.Fatal(err)
	}
	dev, err := env.devices.Create(ctx, devices.Input{IMEI: "869247061237001", Protocol: "gt06"})
	if err != nil {
		t.Fatal(err)
	}
	carro, err := env.vehicles.Create(ctx, vehicles.Input{Name: "Carro da Ana", DeviceID: &dev.ID, OwnerID: &ana.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := env.owners.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	env.sender.connected["869247061237001"] = true
	anaToken, admin := env.login("ana@stepup-api.test"), env.login("admin@stepup-api.test")
	cut := "/api/vehicles/" + carro.ID.String() + "/commands/engine-cut"

	cutWith := func(token, grant string) (int, []byte) {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPost, env.srv.URL+cut, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		if grant != "" {
			req.Header.Set("X-Step-Up-Token", grant)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		return res.StatusCode, body
	}
	code := func(body []byte) string {
		var out struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(body, &out)
		return out.Code
	}

	// Sem confirmação: recusado, com o código que o app usa.
	if status, body := cutWith(anaToken, ""); status != http.StatusForbidden || code(body) != "STEP_UP_REQUIRED" {
		t.Fatalf("sem confirmação: %d %s", status, body)
	}
	if status, body := cutWith(anaToken, "inventado"); status != http.StatusForbidden || code(body) != "STEP_UP_REQUIRED" {
		t.Fatalf("comprovante inventado: %d %s", status, body)
	}
	// Senha errada é 403 (não 401: o app acharia que a sessão acabou).
	status, body := env.do(anaToken, http.MethodPost, "/api/step-up/password",
		map[string]string{"purpose": "engine_cut", "password": "errada"})
	if status != http.StatusForbidden || code(body) != "WRONG_PASSWORD" {
		t.Fatalf("senha errada: %d %s", status, body)
	}
	// Sem biometria cadastrada, o app vai direto para a senha.
	status, body = env.do(anaToken, http.MethodPost, "/api/step-up/options", map[string]string{"purpose": "engine_cut"})
	if status != http.StatusNotFound || code(body) != "NO_BIOMETRIC" {
		t.Fatalf("sem biometria: %d %s", status, body)
	}
	// Senha certa: comprovante de uso único.
	var grant struct {
		Token  string `json:"token"`
		Method string `json:"method"`
	}
	if err := json.Unmarshal(env.must(anaToken, http.MethodPost, "/api/step-up/password",
		map[string]string{"purpose": "engine_cut", "password": userPassword}, http.StatusOK), &grant); err != nil || grant.Method != "password" {
		t.Fatalf("comprovante: %+v %v", grant, err)
	}
	if status, body := cutWith(anaToken, grant.Token); status != http.StatusAccepted {
		t.Fatalf("corte confirmado: %d %s", status, body)
	}
	if status, _ := cutWith(anaToken, grant.Token); status != http.StatusForbidden {
		t.Fatal("o comprovante vale uma vez só")
	}
	// Cadastro de biometria também pede a senha.
	status, body = env.do(anaToken, http.MethodPost, "/api/step-up/biometrics/options", map[string]string{"password": "errada"})
	if status != http.StatusForbidden || code(body) != "WRONG_PASSWORD" {
		t.Fatalf("cadastro de biometria sem a senha: %d %s", status, body)
	}
	// A equipe da central não passa pela confirmação.
	if status, body := cutWith(admin, ""); status != http.StatusAccepted {
		t.Fatalf("admin: %d %s", status, body)
	}
}
