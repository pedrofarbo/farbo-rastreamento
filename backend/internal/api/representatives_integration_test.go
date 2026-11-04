package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/affiliates"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
)

// Os representantes oficiais autorizados na landing: só os que a central
// marcou (com a cidade) e ativos; aparecem o nome, a cidade, a região, o
// WhatsApp público (normalizado) e o link — nada do cadastro nem dos números.
// Precisa de FARBO_TEST_DATABASE_URL.
func TestPublicRepresentatives(t *testing.T) {
	env, _ := newLeadsEnv(t)
	admin := env.login(auth.RoleAdmin + "@leads.test")
	listing := func() []affiliates.Representative {
		t.Helper()
		var list []affiliates.Representative
		_ = json.Unmarshal(env.must("", http.MethodGet, "/api/public/representatives", nil, http.StatusOK), &list)
		return list
	}
	if list := listing(); len(list) != 0 {
		t.Fatalf("sem ninguém marcado = %+v", list)
	}

	// Marcado sem a cidade, ou com o WhatsApp errado: não.
	env.must(admin, http.MethodPost, "/api/affiliates", map[string]any{"name": "Sem Cidade", "active": true, "listed": true},
		http.StatusBadRequest)
	env.must(admin, http.MethodPost, "/api/affiliates", map[string]any{
		"name": "Zap Errado", "active": true, "listed": true, "listingCity": "Campinas/SP", "listingWhatsapp": "1234",
	}, http.StatusBadRequest)

	rui := map[string]any{
		"name": "Rui Representante", "handle": "rui.moto", "code": "rui", "active": true, "handDelivery": true,
		"email": "rui@privado.test", "phone": "(11) 98888-7777", "pixKey": "chave-do-rui",
		"listed": true, "listingCity": "  Campinas/SP ", "listingWhatsapp": "(19) 99999-0000",
		"listingArea": "Campinas,  Valinhos e Vinhedo", "installPrices": map[string]any{"mode": "direct", "installCents": 12000,
			"uninstallCents": 8000},
	}
	var created affiliates.Affiliate
	_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/affiliates", rui, http.StatusCreated), &created)
	if !created.Listed || created.ListingCity != "Campinas/SP" || created.ListingWhatsApp != "5519999990000" {
		t.Fatalf("cadastro = %+v", created)
	}
	// Não marcado, e marcado mas inativo: fora da landing.
	env.must(admin, http.MethodPost, "/api/affiliates", map[string]any{"name": "Ana Afiliada", "active": true}, http.StatusCreated)
	env.must(admin, http.MethodPost, "/api/affiliates", map[string]any{
		"name": "Bia Pausada", "active": false, "listed": true, "listingCity": "Americana/SP",
	}, http.StatusCreated)

	raw := env.must("", http.MethodGet, "/api/public/representatives", nil, http.StatusOK)
	for _, private := range []string{"rui@privado.test", "98888", "chave-do-rui", "reportToken", "commission"} {
		if strings.Contains(string(raw), private) {
			t.Errorf("a landing mostra %q: %s", private, raw)
		}
	}
	list := listing()
	if len(list) != 1 {
		t.Fatalf("representantes = %+v", list)
	}
	if r := list[0]; r.Name != "Rui Representante" || r.Code != "rui" || r.Handle != "rui.moto" || r.City != "Campinas/SP" ||
		r.Area != "Campinas, Valinhos e Vinhedo" || r.WhatsApp != "5519999990000" || !r.HandDelivery || !r.Installs {
		t.Errorf("representante = %+v", r)
	}

	// Desmarcado, sai.
	rui["listed"] = false
	env.must(admin, http.MethodPatch, "/api/affiliates/"+created.ID.String(), rui, http.StatusOK)
	if list := listing(); len(list) != 0 {
		t.Errorf("desmarcado = %+v", list)
	}
}
