package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/telemetry"
)

// recordingAuthNotifier guarda os convites (com o perfil) que sairiam por
// e-mail.
type recordingAuthNotifier struct {
	mu      sync.Mutex
	invites map[string]string // e-mail → perfil
}

func (n *recordingAuthNotifier) PasswordReset(context.Context, string, string, string, time.Duration) error {
	return nil
}

func (n *recordingAuthNotifier) PasswordChanged(context.Context, string, string, time.Time) error {
	return nil
}

func (n *recordingAuthNotifier) Invite(_ context.Context, to, _, role, _ string, _ time.Duration) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.invites[to] = role
	return nil
}

// inviteFor espera o convite (o e-mail sai em segundo plano).
func (n *recordingAuthNotifier) inviteFor(t *testing.T, email string) string {
	t.Helper()
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		n.mu.Lock()
		role, ok := n.invites[email]
		n.mu.Unlock()
		if ok {
			return role
		}
	}
	t.Fatalf("sem convite para %s", email)
	return ""
}

// Equipe da central de ponta a ponta: o administrador cadastra com o perfil
// (convite ou senha), muda perfil e situação; ninguém muda o próprio perfil,
// o painel nunca fica sem administrador e quem muda de perfil sai das
// sessões. Precisa de FARBO_TEST_DATABASE_URL.
func TestTeamUsersEndToEnd(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{
		HTTP: config.HTTP{RateLimitRPS: 1000, RateLimitBurst: 1000},
		Auth: config.Auth{
			JWTSecret:      []byte("segredo-de-teste-integracao-0123456789abcdef"),
			AccessTokenTTL: time.Hour, RefreshTokenTTL: time.Hour, BcryptCost: bcrypt.MinCost,
			InviteTTL: 72 * time.Hour, PasswordResetTTL: time.Hour,
		},
	}
	notifier := &recordingAuthNotifier{invites: map[string]string{}}
	authSvc := auth.NewService(auth.NewRepository(db), cfg.Auth, notifier, log)
	ids := map[string]string{}
	for email, role := range map[string]string{
		"ana@equipe.test": auth.RoleAdmin, "otto@equipe.test": auth.RoleOperator, "caio@cliente.test": auth.RoleCustomer,
	} {
		u, err := authSvc.CreateUser(ctx, email, email, role, userPassword)
		if err != nil {
			t.Fatal(err)
		}
		ids[email] = u.ID.String()
	}
	server := NewServer(Deps{
		Config: cfg, Log: log, Metrics: telemetry.NewMetrics(), DB: db, Auth: authSvc,
		Audit: audit.NewService(audit.NewRepository(db), log),
	})
	srv := httptest.NewServer(server.Handler())
	t.Cleanup(srv.Close)
	env := &credEnv{t: t, db: db, srv: srv}

	admin := env.login("ana@equipe.test")
	env.must(env.login("otto@equipe.test"), http.MethodGet, "/api/users", nil, http.StatusForbidden)

	create := func(body map[string]any, want int) map[string]any {
		var out map[string]any
		_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/users", body, want), &out)
		return out
	}

	// Sem senha: convite por e-mail, com o perfil.
	bia := create(map[string]any{"email": "Bia@Equipe.test", "name": "Bia Lima", "role": "operator"}, http.StatusCreated)
	ids["bia@equipe.test"] = bia["id"].(string)
	if bia["role"] != "operator" || bia["active"] != true || bia["email"] != "bia@equipe.test" {
		t.Fatalf("bia = %v", bia)
	}
	if role := notifier.inviteFor(t, "bia@equipe.test"); role != "operator" {
		t.Errorf("convite da bia com o perfil %q", role)
	}

	// Com senha: entra na hora, com o perfil escolhido.
	vini := create(map[string]any{"email": "vini@equipe.test", "name": "Vini", "role": "viewer", "password": userPassword},
		http.StatusCreated)
	ids["vini@equipe.test"] = vini["id"].(string)
	env.must(env.login("vini@equipe.test"), http.MethodGet, "/api/users", nil, http.StatusForbidden)

	// Cliente tem cadastro próprio; perfil, nome e e-mail são conferidos.
	create(map[string]any{"email": "x@cliente.test", "name": "X", "role": "customer"}, http.StatusBadRequest)
	create(map[string]any{"email": "y@equipe.test", "name": "Y", "role": "dono"}, http.StatusBadRequest)
	create(map[string]any{"email": "z@equipe.test", "name": " ", "role": "viewer"}, http.StatusBadRequest)
	create(map[string]any{"email": "BIA@equipe.test", "name": "Outra", "role": "viewer"}, http.StatusConflict)

	// A lista é só a equipe.
	var list []auth.User
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/users", nil, http.StatusOK), &list)
	emails := map[string]bool{}
	for _, u := range list {
		emails[u.Email] = true
	}
	if len(list) != 4 || !emails["ana@equipe.test"] || !emails["otto@equipe.test"] || !emails["bia@equipe.test"] ||
		!emails["vini@equipe.test"] || emails["caio@cliente.test"] {
		t.Fatalf("equipe = %v", emails)
	}

	update := func(token, email string, body map[string]any, want int) map[string]any {
		var out map[string]any
		_ = json.Unmarshal(env.must(token, http.MethodPatch, "/api/users/"+ids[email], body, want), &out)
		return out
	}

	// Ninguém muda o próprio perfil nem se desativa; o nome, sim.
	update(admin, "ana@equipe.test", map[string]any{"name": "Ana", "role": "operator", "active": true}, http.StatusConflict)
	update(admin, "ana@equipe.test", map[string]any{"name": "Ana", "role": "admin", "active": false}, http.StatusConflict)
	if got := update(admin, "ana@equipe.test", map[string]any{"name": "Ana Souza", "role": "admin", "active": true},
		http.StatusOK); got["name"] != "Ana Souza" {
		t.Errorf("ana = %v", got)
	}
	// O cliente não é editado por aqui.
	update(admin, "caio@cliente.test", map[string]any{"name": "Caio", "role": "operator", "active": true}, http.StatusNotFound)
	update(admin, "vini@equipe.test", map[string]any{"name": "Vini", "role": "customer", "active": true}, http.StatusBadRequest)

	// Vini vira administrador e entra com o perfil novo.
	update(admin, "vini@equipe.test", map[string]any{"name": "Vini", "role": "admin", "active": true}, http.StatusOK)
	viniAccess, viniRefresh := loginTokens(t, env, "vini@equipe.test")
	env.must(viniAccess, http.MethodGet, "/api/users", nil, http.StatusOK)

	// Ana o devolve a visualização: as sessões dele caem (o token de acesso
	// ainda diz administrador até vencer).
	update(admin, "vini@equipe.test", map[string]any{"name": "Vini", "role": "viewer", "active": true}, http.StatusOK)
	env.must("", http.MethodPost, "/api/auth/refresh", map[string]string{"refreshToken": viniRefresh}, http.StatusUnauthorized)

	// Com esse token antigo, ele tenta tirar a Ana, a única administradora
	// ativa: o painel não fica sem administrador.
	update(viniAccess, "ana@equipe.test", map[string]any{"name": "Ana Souza", "role": "viewer", "active": true},
		http.StatusConflict)

	// Desativar também derruba as sessões; desativado não recebe convite.
	_, ottoRefresh := loginTokens(t, env, "otto@equipe.test")
	if got := update(admin, "otto@equipe.test", map[string]any{"name": "Otto", "role": "operator", "active": false},
		http.StatusOK); got["active"] != false {
		t.Errorf("otto = %v", got)
	}
	env.must("", http.MethodPost, "/api/auth/refresh", map[string]string{"refreshToken": ottoRefresh}, http.StatusUnauthorized)
	env.must(admin, http.MethodPost, "/api/users/"+ids["otto@equipe.test"]+"/invite", nil, http.StatusConflict)

	// Reenviar o convite: só para a equipe.
	notifier.mu.Lock()
	delete(notifier.invites, "bia@equipe.test")
	notifier.mu.Unlock()
	env.must(admin, http.MethodPost, "/api/users/"+ids["bia@equipe.test"]+"/invite", nil, http.StatusAccepted)
	notifier.inviteFor(t, "bia@equipe.test")
	env.must(admin, http.MethodPost, "/api/users/"+ids["caio@cliente.test"]+"/invite", nil, http.StatusNotFound)

	// Tudo vai para a auditoria.
	var actions []string
	rows, err := db.Query(ctx, `SELECT action FROM audit_logs WHERE action LIKE 'USER_%' ORDER BY created_at`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			t.Fatal(err)
		}
		actions = append(actions, a)
	}
	rows.Close()
	count := map[string]int{}
	for _, a := range actions {
		count[a]++
	}
	if count[audit.ActionUserCreated] != 2 || count[audit.ActionUserUpdated] != 4 || count[audit.ActionUserInvited] != 1 {
		t.Errorf("auditoria = %v", count)
	}
}

// loginTokens entra e devolve o token de acesso e o refresh token.
func loginTokens(t *testing.T, env *credEnv, email string) (access, refresh string) {
	t.Helper()
	out := env.must("", http.MethodPost, "/api/auth/login",
		map[string]string{"email": email, "password": userPassword}, http.StatusOK)
	var tokens struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
	}
	if err := json.Unmarshal(out, &tokens); err != nil || tokens.AccessToken == "" || tokens.RefreshToken == "" {
		t.Fatalf("login de %s: %s", email, out)
	}
	return tokens.AccessToken, tokens.RefreshToken
}
