package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
)

// TestSearch: a busca geral do painel. O cliente pelo nome (quem começa com
// o texto primeiro), pelo e-mail, pelo telefone ou pelo CPF/CNPJ com ou sem
// a pontuação; o veículo pela placa como for digitada; o rastreador pelo
// IMEI, pela linha ou pelo ICCID, com o veículo e o dono; o chip só quando
// está livre. Fora: a equipe da conta de um cliente e a contratação pelo site
// ainda não paga; texto curto não busca. Só o administrador.
// Precisa de FARBO_TEST_DATABASE_URL.
func TestSearch(t *testing.T) {
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
	lia := exec(`INSERT INTO users (email, name, role, password_hash, document, phone)
		VALUES ('lia.souza@exemplo.test', 'Lia Souza', 'customer', 'x', '529.982.247-25', '(34) 98888-1234') RETURNING id`)
	exec(`INSERT INTO users (email, name, role, password_hash) VALUES ('ana.lia@exemplo.test', 'Ana Lia Prado', 'customer', 'x') RETURNING id`)
	empresa := exec(`INSERT INTO users (email, name, role, password_hash, document)
		VALUES ('frota@transportes.test', 'Transportes Rápidos Ltda', 'customer', 'x', '11.222.333/0001-81') RETURNING id`)
	// Da equipe da conta da empresa: aparece na ficha dela, não como cliente.
	membro := exec(`INSERT INTO users (email, name, role, password_hash) VALUES ('lia.equipe@transportes.test', 'Lia da Equipe', 'customer', 'x') RETURNING id`)
	exec(`INSERT INTO account_members (user_id, account_id, role) VALUES ($1, $2, 'manager') RETURNING user_id`, membro, empresa)
	// A contratação pelo site ainda não paga.
	exec(`INSERT INTO users (email, name, role, password_hash, checkout_at) VALUES ('lia.site@exemplo.test', 'Lia do Site', 'customer', 'x', NOW()) RETURNING id`)

	device := exec(`INSERT INTO devices (imei, iccid, phone_number, status, model)
		VALUES ('869247061900077', '89553202100093795330', '(34) 99999-0001', 'ONLINE', 'J16') RETURNING id`)
	honda := exec(`INSERT INTO vehicles (name, plate, owner_id, device_id) VALUES ('Honda CG 160', 'ABC1D23', $1, $2) RETURNING id`, lia, device)
	exec(`INSERT INTO devices (imei, status) VALUES ('869247061955555', 'OFFLINE') RETURNING id`)
	exec(`INSERT INTO chips (iccid, phone_number, carrier) VALUES ('89553202100093795330', '(34) 99999-0001', 'Algar') RETURNING id`)
	exec(`INSERT INTO chips (iccid, phone_number, carrier) VALUES ('89553202100011112222', '(34) 97777-0002', 'Algar') RETURNING id`)

	admin := env.login(auth.RoleAdmin + "@leads.test")
	search := func(q string) searchResults {
		t.Helper()
		var out searchResults
		if err := json.Unmarshal(env.must(admin, http.MethodGet, "/api/search?q="+url.QueryEscape(q), nil, http.StatusOK), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	customers := func(r searchResults) []string {
		out := []string{}
		for _, c := range r.Customers {
			out = append(out, c.Name)
		}
		return out
	}

	// Pelo nome: quem começa com "lia" primeiro; sem a equipe e sem o site.
	if got := customers(search("Lia")); len(got) != 2 || got[0] != "Lia Souza" || got[1] != "Ana Lia Prado" {
		t.Errorf("busca por Lia = %v", got)
	}
	if got := customers(search("frota@trans")); len(got) != 1 || got[0] != "Transportes Rápidos Ltda" {
		t.Errorf("busca pelo e-mail = %v", got)
	}
	if got := customers(search("98888-1234")); len(got) != 1 || got[0] != "Lia Souza" {
		t.Errorf("busca pelo telefone = %v", got)
	}
	if got := customers(search("52998224725")); len(got) != 1 || got[0] != "Lia Souza" {
		t.Errorf("busca pelo CPF sem pontos = %v", got)
	}
	if got := customers(search("11.222.333/0001")); len(got) != 1 || got[0] != "Transportes Rápidos Ltda" {
		t.Errorf("busca pelo CNPJ com pontos = %v", got)
	}

	// A placa como for digitada, com o dono e o rastreador.
	r := search("abc-1d")
	if len(r.Vehicles) != 1 || r.Vehicles[0].ID.String() != honda || r.Vehicles[0].CustomerName != "Lia Souza" ||
		r.Vehicles[0].DeviceIMEI != "869247061900077" {
		t.Errorf("busca pela placa = %+v", r.Vehicles)
	}

	// O IMEI: o rastreador com o veículo e o dono; o outro do mesmo começo também.
	r = search("8692470619")
	if len(r.Devices) != 2 || r.Devices[0].IMEI != "869247061900077" || r.Devices[0].VehicleName != "Honda CG 160" ||
		r.Devices[0].CustomerName != "Lia Souza" || r.Devices[0].Model != "J16" || r.Devices[1].VehicleID != nil {
		t.Errorf("busca pelo IMEI = %+v", r.Devices)
	}
	// O IMEI inteiro vem primeiro.
	if r = search("869247061955555"); len(r.Devices) != 1 || r.Devices[0].IMEI != "869247061955555" {
		t.Errorf("busca pelo IMEI inteiro = %+v", r.Devices)
	}

	// O ICCID do chip num rastreador: o rastreador (o chip não se repete).
	r = search("93795330")
	if len(r.Devices) != 1 || r.Devices[0].IMEI != "869247061900077" || len(r.Chips) != 0 {
		t.Errorf("busca pelo ICCID do chip em uso = rastreadores %+v, chips %+v", r.Devices, r.Chips)
	}
	// A linha do chip livre: o chip.
	r = search("97777-0002")
	if len(r.Devices) != 0 || len(r.Chips) != 1 || r.Chips[0].ICCID != "89553202100011112222" || r.Chips[0].Carrier != "Algar" {
		t.Errorf("busca pela linha do chip livre = rastreadores %+v, chips %+v", r.Devices, r.Chips)
	}

	// Texto curto não busca; números com menos de 4 dígitos também não.
	for _, q := range []string{"", " ", "l", "12", "a1"} {
		if r := search(q); len(r.Customers)+len(r.Vehicles)+len(r.Devices)+len(r.Chips) != 0 {
			t.Errorf("busca %q = %+v", q, r)
		}
	}

	env.must(env.login(auth.RoleOperator+"@leads.test"), http.MethodGet, "/api/search?q=lia", nil, http.StatusForbidden)
}
