package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
)

// A importação de associados por planilha: tudo é conferido antes (o que
// tem problema volta com a linha, e nada entra); as linhas com o mesmo
// e-mail são um associado com vários veículos; cada veículo entra na fila
// de instalação. Precisa de
// FARBO_TEST_DATABASE_URL.
func TestOrganizationImport(t *testing.T) {
	notifier := &recordingOrgNotifier{}
	env := newCredEnvWith(t, credEnvOptions{orgs: notifier})
	ctx := context.Background()
	if _, err := env.auth.CreateUser(ctx, "admin@importa.test", "Central", auth.RoleAdmin, userPassword); err != nil {
		t.Fatal(err)
	}
	if _, err := env.auth.CreateUser(ctx, "ja@cliente.test", "Já Cliente", auth.RoleCustomer, userPassword); err != nil {
		t.Fatal(err)
	}
	central := env.tokenFor("admin@importa.test")
	var created struct {
		Organization struct {
			ID uuid.UUID `json:"id"`
		} `json:"organization"`
	}
	_ = json.Unmarshal(env.must(central, http.MethodPost, "/api/organizations", map[string]any{
		"kind": "insurer", "name": "Seguradora Teste", "document": "11.222.333/0001-81",
		"email": "contato@seguradora.test", "priceCents": 4000, "dueDay": 10,
		"admin": map[string]string{"name": "Sil Seguros", "email": "sil@seguradora.test"},
	}, http.StatusCreated), &created)
	env.must("", http.MethodPost, "/api/auth/reset-password",
		map[string]string{"token": notifier.token("sil@seguradora.test"), "password": userPassword}, http.StatusNoContent)
	sil := env.tokenFor("sil@seguradora.test")
	if _, err := env.db.Exec(ctx, `INSERT INTO vehicles (name, plate) VALUES ('Estoque', 'OLD1A11')`); err != nil {
		t.Fatal(err)
	}

	type row map[string]any
	vehicle := func(name, plate string) map[string]any { return map[string]any{"name": name, "plate": plate} }
	bad := []row{
		{"line": 2, "name": "Rui", "email": "rui@segurado.test", "document": "529.982.247-25", "vehicle": vehicle("Gol", "abc1d23")},
		{"line": 3, "name": "", "email": "x@segurado.test"},
		{"line": 4, "name": "Lia", "email": "lia-sem-arroba"},
		{"line": 5, "name": "Mia", "email": "mia@segurado.test", "document": "111.111.111-11"},
		{"line": 6, "name": "Outro Rui", "email": "rui@segurado.test"},
		{"line": 7, "name": "Teo", "email": "teo@segurado.test", "vehicle": vehicle("Uno", "ABC1D23")},
		{"line": 8, "name": "Ju", "email": "ja@cliente.test"},
		{"line": 9, "name": "Bia", "email": "bia@segurado.test", "vehicle": vehicle("Fox", "OLD1A11")},
	}
	status, out := env.do(sil, http.MethodPost, "/api/org/associates/import", map[string]any{"rows": bad, "dryRun": true})
	var refused struct {
		Problems []struct {
			Row     int    `json:"row"`
			Message string `json:"message"`
		} `json:"problems"`
	}
	_ = json.Unmarshal(out, &refused)
	rows := []int{}
	for _, p := range refused.Problems {
		rows = append(rows, p.Row)
	}
	if status != http.StatusUnprocessableEntity || len(rows) != 7 || rows[0] != 1 || rows[6] != 7 {
		t.Fatalf("conferência = %d %s", status, out)
	}

	good := []row{
		{"name": "Rui Segurado", "email": "RUI@segurado.test", "phone": "11988887777", "document": "529.982.247-25",
			"vehicle": vehicle("Gol", "abc1d23")},
		{"name": "Rui Segurado", "email": "rui@segurado.test", "vehicle": vehicle("", "XYZ9Z99")},
		{"name": "Lia Segurada", "email": "lia@segurado.test"},
	}
	var dry struct {
		DryRun               bool
		Associates, Vehicles int
	}
	_ = json.Unmarshal(env.must(sil, http.MethodPost, "/api/org/associates/import", map[string]any{"rows": good, "dryRun": true},
		http.StatusOK), &dry)
	if !dry.DryRun || dry.Associates != 2 || dry.Vehicles != 2 {
		t.Fatalf("conferência boa = %+v", dry)
	}
	var count int
	_ = env.db.QueryRow(ctx, `SELECT count(*) FROM users WHERE organization_id = $1`, created.Organization.ID).Scan(&count)
	if count != 0 {
		t.Fatalf("a conferência não cadastra: %d", count)
	}
	var done struct{ Associates, Vehicles int }
	_ = json.Unmarshal(env.must(sil, http.MethodPost, "/api/org/associates/import", map[string]any{"rows": good},
		http.StatusCreated), &done)
	if done.Associates != 2 || done.Vehicles != 2 {
		t.Fatalf("importação = %+v", done)
	}
	var associates []struct {
		Name     string `json:"name"`
		Document string `json:"document"`
		Vehicles int    `json:"vehicles"`
	}
	_ = json.Unmarshal(env.must(sil, http.MethodGet, "/api/org/associates", nil, http.StatusOK), &associates)
	if len(associates) != 2 || associates[1].Name != "Rui Segurado" || associates[1].Vehicles != 2 || associates[1].Document != "52998224725" {
		t.Fatalf("associados = %+v", associates)
	}
	var queued int
	_ = env.db.QueryRow(ctx, `SELECT count(*) FROM fulfillments f JOIN users u ON u.id = f.customer_id WHERE u.organization_id = $1`,
		created.Organization.ID).Scan(&queued)
	if queued != 2 {
		t.Fatalf("fila de instalação = %d", queued)
	}
	// De novo: os e-mails já existem.
	env.must(sil, http.MethodPost, "/api/org/associates/import", map[string]any{"rows": good}, http.StatusUnprocessableEntity)
}
