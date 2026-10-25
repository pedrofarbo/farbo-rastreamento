package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/devices"
)

// O MRR com as associações: a mensalidade dos veículos instalados entra na
// coluna das associações e no total — antes da fatura sair, pelos veículos
// que contam hoje; depois, pela fatura (sem contar duas vezes) — e a
// instalação e o proporcional ficam de fora. Precisa de FARBO_TEST_DATABASE_URL.
func TestMRRWithOrganizations(t *testing.T) {
	notifier := &recordingOrgNotifier{}
	env := newCredEnvWith(t, credEnvOptions{orgs: notifier})
	ctx := context.Background()
	if _, err := env.auth.CreateUser(ctx, "admin@mrr-org.test", "Central", auth.RoleAdmin, userPassword); err != nil {
		t.Fatal(err)
	}
	central := env.tokenFor("admin@mrr-org.test")
	var created struct {
		Organization struct {
			ID uuid.UUID `json:"id"`
		} `json:"organization"`
	}
	_ = json.Unmarshal(env.must(central, http.MethodPost, "/api/organizations", map[string]any{
		"kind": "association", "name": "Associação MRR", "document": "11.222.333/0001-81",
		"email": "contato@mrr-org.test", "priceCents": 3000, "dueDay": 28, "installFeeCents": 5000,
		"admin": map[string]string{"name": "Gil Gestor", "email": "gil@mrr-org.test"},
	}, http.StatusCreated), &created)
	orgID := created.Organization.ID
	rows := []map[string]any{
		{"name": "Ana", "email": "ana@mrr-org.test", "vehicle": map[string]any{"name": "Gol", "plate": "MRR1A11"}},
		{"name": "Beto", "email": "beto@mrr-org.test", "vehicle": map[string]any{"name": "Uno", "plate": "MRR2B22"}},
	}
	env.must(central, http.MethodPost, "/api/organizations/"+orgID.String()+"/associates/import", map[string]any{"rows": rows}, http.StatusCreated)
	for i, plate := range []string{"MRR1A11", "MRR2B22"} {
		dev, err := env.devices.Create(ctx, devices.Input{IMEIConfirmed: true, IMEI: "86924706123050" + string(rune('0'+i)), Protocol: "gt06"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := env.db.Exec(ctx, `UPDATE vehicles SET device_id = $2 WHERE plate = $1`, plate, dev.ID); err != nil {
			t.Fatal(err)
		}
	}
	if err := env.orgs.SyncVehicles(ctx); err != nil {
		t.Fatal(err)
	}

	type month struct {
		Month                string `json:"month"`
		MRRCents             int64  `json:"mrrCents"`
		OrganizationsCents   int64  `json:"organizationsCents"`
		OrganizationVehicles int    `json:"organizationVehicles"`
		InstallmentsCents    int64  `json:"installmentsCents"`
		TotalCents           int64  `json:"totalCents"`
	}
	type mrr struct {
		OrganizationsCents int64   `json:"organizationsCents"`
		TotalCents         int64   `json:"totalCents"`
		Months             []month `json:"months"`
		Forecast           struct {
			OrganizationsCents   int64 `json:"organizationsCents"`
			OrganizationVehicles int   `json:"organizationVehicles"`
			MonthlyCents         int64 `json:"monthlyCents"`
			InstallmentsCents    int64 `json:"installmentsCents"`
			TotalCents           int64 `json:"totalCents"`
		} `json:"forecast"`
	}
	read := func() mrr {
		t.Helper()
		var m mrr
		_ = json.Unmarshal(env.must(central, http.MethodGet, "/api/customers/mrr", nil, http.StatusOK), &m)
		return m
	}
	// O 1º vencimento é o dia 28 deste mês (ou do que vem, depois do dia 28).
	thisMonth := time.Now().UTC().Day() <= 28
	check := func(when string, m mrr) {
		t.Helper()
		cur := m.Months[0]
		wantCur := int64(0)
		if thisMonth {
			wantCur = 6000
		}
		if cur.OrganizationsCents != wantCur || m.OrganizationsCents != wantCur ||
			cur.TotalCents != cur.MRRCents+cur.OrganizationsCents+cur.InstallmentsCents || m.TotalCents != cur.TotalCents {
			t.Fatalf("%s: mês atual = %+v (resumo %d/%d)", when, cur, m.OrganizationsCents, m.TotalCents)
		}
		if thisMonth && cur.OrganizationVehicles != 2 {
			t.Fatalf("%s: veículos = %d", when, cur.OrganizationVehicles)
		}
		f := m.Forecast
		if f.OrganizationsCents != 6000 || f.OrganizationVehicles != 2 || f.TotalCents != f.MonthlyCents+f.OrganizationsCents+f.InstallmentsCents {
			t.Fatalf("%s: previsão = %+v", when, f)
		}
	}
	check("antes da fatura", read())

	// A fatura sai (com o proporcional e a instalação): a mensalidade é a mesma.
	var inv struct {
		AmountCents int64 `json:"amountCents"`
	}
	_ = json.Unmarshal(env.must(central, http.MethodPost, "/api/organizations/"+orgID.String()+"/invoices/generate", nil, http.StatusCreated), &inv)
	if inv.AmountCents <= 6000 {
		t.Fatalf("a fatura devia ter as taxas além da mensalidade: %+v", inv)
	}
	check("depois da fatura", read())
}
