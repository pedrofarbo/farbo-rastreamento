package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/vehicles"
)

// TestVehicleDirectory: a listagem de veículos da central, paginada, com os
// filtros por placa (sem a pontuação), associação (ou sem nenhuma), nome do
// cliente, CPF/CNPJ (com ou sem a pontuação) e e-mail; só o administrador.
// Precisa de FARBO_TEST_DATABASE_URL.
func TestVehicleDirectory(t *testing.T) {
	env, _ := newLeadsEnv(t)
	ctx := context.Background()
	exec := func(query string, args ...any) string {
		t.Helper()
		var id string
		if err := env.db.QueryRow(ctx, query, args...).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	org := exec(`INSERT INTO users (email, name, role, password_hash) VALUES ('contato@protege.test', 'Associação Protege', 'organization', '') RETURNING id`)
	exec(`INSERT INTO organizations (id, kind, price_cents, due_day) VALUES ($1, 'association', 3000, 10) RETURNING id`, org)
	lia := exec(`INSERT INTO users (email, name, role, password_hash, document, organization_id)
		VALUES ('lia.souza@exemplo.test', 'Lia Souza', 'customer', 'x', '529.982.247-25', $1) RETURNING id`, org)
	empresa := exec(`INSERT INTO users (email, name, role, password_hash, document)
		VALUES ('frota@transportes.test', 'Transportes Rápidos Ltda', 'customer', 'x', '11.222.333/0001-81') RETURNING id`)
	device := exec(`INSERT INTO devices (imei, status, last_seen_at) VALUES ('869247061900077', 'ONLINE', NOW()) RETURNING id`)
	exec(`INSERT INTO vehicles (name, plate, owner_id, device_id, created_at) VALUES ('Honda CG 160', 'ABC1D23', $1, $2, NOW() - interval '3 days') RETURNING id`, lia, device)
	// 30 da empresa (a mais nova por último) e um da central, sem dono.
	for i := range 30 {
		exec(`INSERT INTO vehicles (name, plate, owner_id, created_at) VALUES ($1, $2, $3, NOW() - make_interval(mins => $4)) RETURNING id`,
			fmt.Sprintf("Caminhão %02d", i), fmt.Sprintf("TRK%04d", i), empresa, 60-i)
	}
	exec(`INSERT INTO vehicles (name, owner_id) VALUES ('Carro da central', NULL) RETURNING id`)

	admin := env.login(auth.RoleAdmin + "@leads.test")
	list := func(params url.Values) vehicles.DirectoryPage {
		t.Helper()
		var page vehicles.DirectoryPage
		if err := json.Unmarshal(env.must(admin, http.MethodGet, "/api/vehicle-directory?"+params.Encode(), nil, http.StatusOK), &page); err != nil {
			t.Fatal(err)
		}
		return page
	}
	names := func(p vehicles.DirectoryPage) []string {
		out := []string{}
		for _, e := range p.Items {
			out = append(out, e.Name)
		}
		return out
	}

	// Sem filtro: os mais novos primeiro, 25 por página.
	first := list(url.Values{})
	if first.Total != 32 || len(first.Items) != 25 || first.Page != 1 || first.PageSize != 25 || first.Items[0].Name != "Carro da central" ||
		first.Items[1].Name != "Caminhão 29" {
		t.Fatalf("primeira página = total %d, %v", first.Total, names(first))
	}
	if last := list(url.Values{"page": {"2"}}); last.Total != 32 || len(last.Items) != 7 || last.Items[6].Name != "Honda CG 160" {
		t.Fatalf("segunda página = total %d, %v", last.Total, names(last))
	}
	if beyond := list(url.Values{"page": {"9"}}); beyond.Total != 32 || len(beyond.Items) != 0 {
		t.Errorf("depois da última = total %d, %v", beyond.Total, names(beyond))
	}
	if big := list(url.Values{"pageSize": {"1000"}}); big.PageSize != vehicles.DirectoryMaxPageSize || len(big.Items) != 32 {
		t.Errorf("página grande = %d por página, %d itens", big.PageSize, len(big.Items))
	}

	// O veículo vem com o dono, a associação e o rastreador.
	one := func(params url.Values) vehicles.DirectoryEntry {
		t.Helper()
		p := list(params)
		if p.Total != 1 || len(p.Items) != 1 {
			t.Fatalf("%v: total %d, %v", params, p.Total, names(p))
		}
		return p.Items[0]
	}
	e := one(url.Values{"plate": {"abc-1d"}})
	if e.Name != "Honda CG 160" || e.CustomerName != "Lia Souza" || e.CustomerEmail != "lia.souza@exemplo.test" ||
		e.CustomerDocument != "529.982.247-25" || e.OrganizationID == nil || e.OrganizationID.String() != org ||
		e.OrganizationName != "Associação Protege" || e.DeviceIMEI != "869247061900077" || e.DeviceStatus != "ONLINE" || e.LastSeenAt == nil {
		t.Fatalf("veículo = %+v", e)
	}
	one(url.Values{"customer": {"souza"}})
	one(url.Values{"document": {"52998224725"}})
	one(url.Values{"document": {"529.982"}})
	one(url.Values{"email": {"LIA.SOUZA@"}})
	one(url.Values{"organization": {org}})
	// Os filtros somam.
	if p := list(url.Values{"organization": {org}, "email": {"frota@"}}); p.Total != 0 {
		t.Errorf("associação e e-mail de outro = %v", names(p))
	}
	if p := list(url.Values{"document": {"11.222.333/0001-81"}, "plate": {"trk002"}}); p.Total != 10 {
		t.Errorf("CNPJ e placa = total %d", p.Total)
	}
	if p := list(url.Values{"organization": {"none"}}); p.Total != 31 {
		t.Errorf("sem associação = total %d", p.Total)
	}
	env.must(admin, http.MethodGet, "/api/vehicle-directory?organization=xyz", nil, http.StatusBadRequest)

	// Só o administrador.
	env.must(env.login(auth.RoleOperator+"@leads.test"), http.MethodGet, "/api/vehicle-directory", nil, http.StatusForbidden)
}
