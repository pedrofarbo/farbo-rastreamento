package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
)

// Meus dados: o cliente atualiza nome, telefone e CPF (validados e
// guardados no formato certo), sem mexer na situação da conta nem no
// e-mail; a equipe não usa esta rota. Precisa de FARBO_TEST_DATABASE_URL.
func TestMyProfile(t *testing.T) {
	env, _ := newLeadsEnv(t)
	admin := env.login(auth.RoleAdmin + "@leads.test")
	env.must(admin, http.MethodPost, "/api/customers", map[string]any{
		"name": "Lia", "email": "lia@perfil.test", "document": "529.982.247-25", "password": userPassword,
	}, http.StatusCreated)
	lia := env.login("lia@perfil.test")
	put := func(body map[string]any, want int) auth.User {
		t.Helper()
		var u auth.User
		_ = json.Unmarshal(env.must(lia, http.MethodPut, "/api/me/profile", body, want), &u)
		return u
	}

	u := put(map[string]any{"name": "  Lia   Martins Souza ", "phone": "11 98765-4321", "document": "390.533.447-05"}, http.StatusOK)
	if u.Name != "Lia Martins Souza" || u.Phone != "(11) 98765-4321" || u.Document != "39053344705" || !u.Active || u.Email != "lia@perfil.test" {
		t.Fatalf("depois de salvar = %+v", u)
	}
	var me auth.User
	_ = json.Unmarshal(env.must(lia, http.MethodGet, "/api/auth/me", nil, http.StatusOK), &me)
	if me.Name != "Lia Martins Souza" || me.Document != "39053344705" {
		t.Errorf("/auth/me = %+v", me)
	}
	// Fixo com o 55 do país; telefone vazio também vale.
	if u := put(map[string]any{"name": "Lia Martins", "phone": "+55 (11) 3333-4444", "document": "39053344705"}, http.StatusOK); u.Phone != "(11) 3333-4444" {
		t.Errorf("fixo = %q", u.Phone)
	}
	if u := put(map[string]any{"name": "Lia Martins", "phone": "", "document": "39053344705"}, http.StatusOK); u.Phone != "" {
		t.Errorf("sem telefone = %q", u.Phone)
	}
	// O que é recusado.
	put(map[string]any{"name": " ", "phone": "", "document": "39053344705"}, http.StatusBadRequest)
	put(map[string]any{"name": "Lia", "phone": "1234", "document": "39053344705"}, http.StatusBadRequest)
	put(map[string]any{"name": "Lia", "phone": "", "document": ""}, http.StatusBadRequest)
	put(map[string]any{"name": "Lia", "phone": "", "document": "111.111.111-11"}, http.StatusBadRequest)
	put(map[string]any{"name": "Lia", "email": "outro@x.test", "document": "39053344705"}, http.StatusBadRequest)
	// A equipe não usa esta rota.
	env.must(admin, http.MethodPut, "/api/me/profile", map[string]any{"name": "Admin", "document": "39053344705"}, http.StatusForbidden)

	var audited int
	if err := env.db.QueryRow(t.Context(), `SELECT COUNT(*) FROM audit_logs WHERE action = $1 AND metadata->>'self' = 'true'`,
		audit.ActionCustomerUpdated).Scan(&audited); err != nil || audited != 3 {
		t.Errorf("auditoria = %d (%v)", audited, err)
	}
}
