package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
)

// A central pega o link do convite para mandar por outro meio (WhatsApp),
// sem o e-mail: só o admin; o link cria a senha e entra; um link novo derruba
// o anterior; cliente desativado não ganha link. Precisa de
// FARBO_TEST_DATABASE_URL.
func TestCustomerInviteLink(t *testing.T) {
	env, _ := newLeadsEnv(t, func(d *Deps) {
		d.Config.Mail.AppURL = "https://painel.test/"
		d.Config.Auth.InviteTTL = 72 * time.Hour
		d.Auth = auth.NewService(auth.NewRepository(d.DB), d.Config.Auth, nil, d.Log)
	})
	admin := env.login(auth.RoleAdmin + "@leads.test")
	var eva struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/customers", map[string]any{
		"name": "Eva Souza", "email": "eva@convite.test", "phone": "(11) 93333-0000", "document": "", "password": userPassword,
	}, http.StatusCreated), &eva)
	path := "/api/customers/" + eva.ID + "/invite-link"

	env.must(env.login(auth.RoleOperator+"@leads.test"), http.MethodPost, path, nil, http.StatusForbidden)

	type link struct {
		URL       string    `json:"url"`
		ExpiresAt time.Time `json:"expiresAt"`
	}
	issue := func() (link, string) {
		t.Helper()
		var l link
		_ = json.Unmarshal(env.must(admin, http.MethodPost, path, nil, http.StatusOK), &l)
		prefix := "https://painel.test/redefinir-senha#"
		if !strings.HasPrefix(l.URL, prefix) || !strings.HasSuffix(l.URL, "&boasvindas=1") || !l.ExpiresAt.After(time.Now()) {
			t.Fatalf("link = %+v", l)
		}
		fragment, err := url.ParseQuery(strings.TrimPrefix(l.URL, prefix))
		if err != nil || fragment.Get("token") == "" {
			t.Fatalf("token no link %q: %v", l.URL, err)
		}
		return l, fragment.Get("token")
	}

	// Um link novo derruba o anterior; com o novo, a cliente cria a senha e entra.
	_, first := issue()
	_, second := issue()
	env.must("", http.MethodPost, "/api/auth/reset-password/validate", map[string]string{"token": first}, http.StatusGone)
	env.must("", http.MethodPost, "/api/auth/reset-password/validate", map[string]string{"token": second}, http.StatusNoContent)
	const fresh = "senha-criada-pelo-link-123"
	env.must("", http.MethodPost, "/api/auth/reset-password",
		map[string]string{"token": second, "password": fresh}, http.StatusNoContent)
	env.must("", http.MethodPost, "/api/auth/login", map[string]string{"email": "eva@convite.test", "password": fresh}, http.StatusOK)

	// Desativada, não ganha link.
	env.must(admin, http.MethodPatch, "/api/customers/"+eva.ID, map[string]any{
		"name": "Eva Souza", "phone": "(11) 93333-0000", "document": "", "active": false,
	}, http.StatusOK)
	env.must(admin, http.MethodPost, path, nil, http.StatusConflict)
}
