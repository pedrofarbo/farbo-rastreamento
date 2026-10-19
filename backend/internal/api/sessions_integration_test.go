package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/stepup"
)

// As sessões pelo app e pela central: o cliente vê os aparelhos e sai dos
// outros (o outro cai na hora, este continua); a central vê as sessões do
// cliente, desconecta todas e define a senha nova (com a confirmação de quem
// faz), que derruba quem estava logado. Precisa de FARBO_TEST_DATABASE_URL.
func TestSessionsEndToEnd(t *testing.T) {
	var stepUpSvc *stepup.Service
	env, _ := newLeadsEnv(t, func(d *Deps) {
		stepUpSvc = stepup.NewService(d.DB, config.StepUp{RPID: "localhost", Origins: []string{"http://localhost"}}, d.Auth)
		d.StepUp = stepUpSvc
	})
	ctx := context.Background()
	authSvc := auth.NewService(auth.NewRepository(env.db), config.Auth{BcryptCost: bcrypt.MinCost}, nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	jair, err := authSvc.CreateUser(ctx, "jair@sessoes.test", "Jair", auth.RoleCustomer, userPassword)
	if err != nil {
		t.Fatal(err)
	}
	type tokens struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
	}
	login := func(password string, want int) tokens {
		t.Helper()
		var out tokens
		_ = json.Unmarshal(env.must("", http.MethodPost, "/api/auth/login",
			map[string]string{"email": "jair@sessoes.test", "password": password}, want), &out)
		return out
	}
	type session struct {
		ID        uuid.UUID `json:"id"`
		UserAgent string    `json:"userAgent"`
		Current   bool      `json:"current"`
	}
	sessions := func(token, path string) []session {
		t.Helper()
		var out struct {
			Sessions []session `json:"sessions"`
		}
		_ = json.Unmarshal(env.must(token, http.MethodGet, path, nil, http.StatusOK), &out)
		return out.Sessions
	}

	// Dois aparelhos.
	mine, other := login(userPassword, http.StatusOK), login(userPassword, http.StatusOK)
	list := sessions(mine.AccessToken, "/api/auth/sessions")
	if len(list) != 2 || (list[0].Current == list[1].Current) {
		t.Fatalf("sessões = %+v", list)
	}

	// Sair dos outros (o corte é no segundo cheio).
	time.Sleep(1100 * time.Millisecond)
	var fresh struct {
		AccessToken string `json:"accessToken"`
		SignedOut   int    `json:"signedOut"`
	}
	_ = json.Unmarshal(env.must(mine.AccessToken, http.MethodPost, "/api/auth/sessions/sign-out-others", nil, http.StatusOK), &fresh)
	if fresh.SignedOut != 1 || fresh.AccessToken == "" {
		t.Fatalf("sair dos outros = %+v", fresh)
	}
	env.must(other.AccessToken, http.MethodGet, "/api/auth/sessions", nil, http.StatusUnauthorized)
	env.must("", http.MethodPost, "/api/auth/refresh", map[string]string{"refreshToken": other.RefreshToken}, http.StatusUnauthorized)
	if list := sessions(fresh.AccessToken, "/api/auth/sessions"); len(list) != 1 || !list[0].Current {
		t.Errorf("depois de sair dos outros = %+v", list)
	}

	// A central: vê a sessão que sobrou e desconecta todas.
	admin := env.login(auth.RoleAdmin + "@leads.test")
	if list := sessions(admin, "/api/customers/"+jair.ID.String()+"/sessions"); len(list) != 1 || list[0].Current {
		t.Errorf("as sessões na central = %+v", list)
	}
	time.Sleep(1100 * time.Millisecond)
	env.must(admin, http.MethodPost, "/api/customers/"+jair.ID.String()+"/sessions/sign-out", nil, http.StatusOK)
	env.must(fresh.AccessToken, http.MethodGet, "/api/auth/sessions", nil, http.StatusUnauthorized)

	// A senha nova pela central: sem a confirmação, não; com ela, a antiga
	// para de entrar e quem estava logado cai.
	again := login(userPassword, http.StatusOK)
	time.Sleep(1100 * time.Millisecond)
	setPassword := func(password, grant string) int {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"password": password})
		req, _ := http.NewRequest(http.MethodPost, env.srv.URL+"/api/customers/"+jair.ID.String()+"/password", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+admin)
		req.Header.Set("Content-Type", "application/json")
		if grant != "" {
			req.Header.Set("X-Step-Up-Token", grant)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if code := setPassword("curta", ""); code != http.StatusBadRequest {
		t.Errorf("senha curta: %d", code)
	}
	if code := setPassword("SenhaNova-da-Central-2026", ""); code != http.StatusForbidden {
		t.Errorf("sem a confirmação: %d", code)
	}
	var adminID uuid.UUID
	if err := env.db.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, auth.RoleAdmin+"@leads.test").Scan(&adminID); err != nil {
		t.Fatal(err)
	}
	grant, err := stepUpSvc.VerifyPassword(ctx, adminID, stepup.PurposeCustomerPassword, userPassword)
	if err != nil {
		t.Fatal(err)
	}
	if code := setPassword("SenhaNova-da-Central-2026", grant.Token); code != http.StatusNoContent {
		t.Fatalf("com a confirmação: %d", code)
	}
	env.must(again.AccessToken, http.MethodGet, "/api/auth/sessions", nil, http.StatusUnauthorized)
	// (Direto no serviço: a rota de login tem limite de tentativas por IP.)
	if _, err := authSvc.Authenticate(ctx, "jair@sessoes.test", userPassword); err == nil {
		t.Error("a senha antiga ainda entra")
	}
	if _, err := authSvc.Authenticate(ctx, "jair@sessoes.test", "SenhaNova-da-Central-2026"); err != nil {
		t.Errorf("a senha nova não entra: %v", err)
	}

	var actions []string
	rows, err := env.db.Query(ctx, `SELECT action FROM audit_logs WHERE action IN ('AUTH_SIGNED_OUT_OTHERS', 'CUSTOMER_SIGNED_OUT', 'CUSTOMER_PASSWORD_SET') ORDER BY created_at`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var a string
		_ = rows.Scan(&a)
		actions = append(actions, a)
	}
	rows.Close()
	if len(actions) != 3 || actions[0] != "AUTH_SIGNED_OUT_OTHERS" || actions[1] != "CUSTOMER_SIGNED_OUT" || actions[2] != "CUSTOMER_PASSWORD_SET" {
		t.Errorf("auditoria = %v", actions)
	}
}
