package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/contract"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/mail"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/vehicles"
)

type recordingContractMailer struct {
	mu   sync.Mutex
	sent []mail.ContractCopy
}

func (m *recordingContractMailer) ContractAccepted(_ context.Context, _, _ string, c mail.ContractCopy) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, c)
	return nil
}

// O contrato de ponta a ponta: o cliente sem aceite só lê e aceita o
// contrato (o resto da API responde CONTRACT_REQUIRED); o aceite exige CPF
// válido e a versão em vigor, grava o CPF no cadastro, registra IP e
// navegador e manda a cópia; quem só acompanha o veículo de outra pessoa
// fica dispensado até pedir um rastreador; a central valida o CPF e vê o
// aceite na ficha. Precisa de FARBO_TEST_DATABASE_URL.
func TestContractEndToEnd(t *testing.T) {
	mailer := &recordingContractMailer{}
	var vehicleSvc *vehicles.Service
	env, _ := newLeadsEnv(t, func(d *Deps) {
		doc, err := contract.Render(contract.Params{
			Company:          config.Company{Name: "Farbo Rastreadores", LegalName: "FARBO TECNOLOGIA DE SISTEMAS E CLOUD LTDA", CNPJ: "49.757.084/0001-00"},
			SuspendAfterDays: 10, HistoryOptions: []int{7, 14, 30},
		})
		if err != nil {
			t.Fatal(err)
		}
		svc := contract.NewService(d.DB, doc, mailer, d.Log)
		svc.SetSync()
		d.Contract = svc
		vehicleSvc = d.Vehicles
	})
	ctx := context.Background()
	admin := env.login(auth.RoleAdmin + "@leads.test")

	// A central: CPF inválido é recusado; válido é guardado só com os números.
	env.must(admin, http.MethodPost, "/api/customers", map[string]any{
		"name": "Rui", "email": "rui@contrato.test", "document": "123.456.789-00", "password": userPassword,
	}, http.StatusBadRequest)
	var created struct {
		ID       string `json:"id"`
		Document string `json:"document"`
	}
	_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/customers", map[string]any{
		"name": "Rui Prado", "email": "rui@contrato.test", "document": "529.982.247-25", "password": userPassword,
	}, http.StatusCreated), &created)
	if created.Document != "52998224725" {
		t.Fatalf("CPF guardado = %q", created.Document)
	}
	// Sem CPF: o cliente informa no primeiro acesso.
	_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/customers", map[string]any{
		"name": "Lia Martins", "email": "lia@contrato.test", "password": userPassword,
	}, http.StatusCreated), &created)
	liaID := created.ID
	lia := env.login("lia@contrato.test")

	type status struct {
		Contract struct {
			Version  string            `json:"version"`
			Title    string            `json:"title"`
			Sections []json.RawMessage `json:"sections"`
			SHA256   string            `json:"sha256"`
		} `json:"contract"`
		Required bool `json:"required"`
		Accepted *struct {
			Version  string `json:"version"`
			Document string `json:"document"`
			IP       string `json:"ip"`
		} `json:"accepted"`
		TaxID string `json:"taxId"`
	}
	read := func(token string) status {
		t.Helper()
		var st status
		_ = json.Unmarshal(env.must(token, http.MethodGet, "/api/me/contract", nil, http.StatusOK), &st)
		return st
	}
	st := read(lia)
	if !st.Required || st.Accepted != nil || st.TaxID != "" || st.Contract.Version != contract.Version ||
		len(st.Contract.Sections) != 16 || st.Contract.SHA256 == "" {
		t.Fatalf("antes do aceite = %+v", st)
	}
	// Sem o aceite, o resto não abre.
	for _, path := range []string{"/api/vehicles", "/api/me/account", "/api/me/invoices", "/api/geofences"} {
		status, body := env.do(lia, http.MethodGet, path, nil)
		if status != http.StatusForbidden || !json.Valid(body) || !containsCode(body, "CONTRACT_REQUIRED") {
			t.Errorf("%s sem aceite = %d %s", path, status, body)
		}
	}
	// O texto público (antes de contratar).
	var public struct {
		Title string `json:"title"`
	}
	_ = json.Unmarshal(env.must("", http.MethodGet, "/api/public/contract", nil, http.StatusOK), &public)
	if public.Title != st.Contract.Title {
		t.Errorf("público = %+v", public)
	}

	// O aceite: a versão em vigor e um CPF válido.
	env.must(lia, http.MethodPost, "/api/me/contract/accept", map[string]any{"version": "0", "document": "529.982.247-25"}, http.StatusBadRequest)
	env.must(lia, http.MethodPost, "/api/me/contract/accept", map[string]any{"version": contract.Version, "document": "111.111.111-11"}, http.StatusBadRequest)
	env.must(lia, http.MethodPost, "/api/me/contract/accept", map[string]any{"version": contract.Version, "document": ""}, http.StatusBadRequest)
	env.must(lia, http.MethodPost, "/api/me/contract/accept", map[string]any{"version": contract.Version, "document": "390.533.447-05"}, http.StatusOK)
	st = read(lia)
	if st.Required || st.Accepted == nil || st.Accepted.Document != "39053344705" || st.Accepted.IP == "" || st.TaxID != "39053344705" {
		t.Fatalf("depois do aceite = %+v", st)
	}
	if len(mailer.sent) != 1 || mailer.sent[0].Document != "390.533.447-05" || len(mailer.sent[0].Sections) != 16 {
		t.Fatalf("cópia = %+v", mailer.sent)
	}
	env.must(lia, http.MethodGet, "/api/vehicles", nil, http.StatusOK)
	env.must(lia, http.MethodGet, "/api/me/account", nil, http.StatusOK)
	// De novo: nada muda (um aceite por versão).
	env.must(lia, http.MethodPost, "/api/me/contract/accept", map[string]any{"version": contract.Version, "document": "390.533.447-05"}, http.StatusOK)
	var rows int
	if err := env.db.QueryRow(ctx, `SELECT COUNT(*) FROM contract_acceptances WHERE user_id = $1`, liaID).Scan(&rows); err != nil || rows != 1 {
		t.Errorf("aceites = %d (%v)", rows, err)
	}

	// Quem só acompanha o veículo de outra pessoa: dispensado, até pedir um rastreador.
	authSvc := auth.NewService(auth.NewRepository(env.db), config.Auth{BcryptCost: bcrypt.MinCost}, nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	guest, err := authSvc.CreateUser(ctx, "bia@contrato.test", "Bia", auth.RoleCustomer, userPassword)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := authSvc.CreateUser(ctx, "dono@contrato.test", "Dono", auth.RoleCustomer, userPassword)
	if err != nil {
		t.Fatal(err)
	}
	car, err := vehicleSvc.Create(ctx, vehicles.Input{Name: "Carro do Dono", OwnerID: &owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.db.Exec(ctx, `INSERT INTO vehicle_shares (vehicle_id, owner_id, guest_id) VALUES ($1, $2, $3)`,
		car.ID, owner.ID, guest.ID); err != nil {
		t.Fatal(err)
	}
	bia := env.login("bia@contrato.test")
	if st := read(bia); st.Required {
		t.Fatalf("convidada = %+v", st)
	}
	env.must(bia, http.MethodGet, "/api/vehicles", nil, http.StatusOK)
	if status, body := env.do(bia, http.MethodPost, "/api/me/trackers",
		map[string]any{"vehicle": map[string]any{"name": "Moto da Bia"}}); status != http.StatusForbidden || !containsCode(body, "CONTRACT_REQUIRED") {
		t.Errorf("pedido sem aceite = %d %s", status, body)
	}
	// O dono do veículo, sim.
	if st := read(env.login("dono@contrato.test")); !st.Required {
		t.Errorf("dono = %+v", st)
	}

	// A ficha mostra o aceite; a auditoria registra.
	var detail struct {
		Contract *struct {
			Version string `json:"version"`
		} `json:"contract"`
		ContractVersion string `json:"contractVersion"`
	}
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/customers/"+liaID, nil, http.StatusOK), &detail)
	if detail.Contract == nil || detail.Contract.Version != contract.Version || detail.ContractVersion != contract.Version {
		t.Errorf("ficha = %+v", detail)
	}
	var audited int
	if err := env.db.QueryRow(ctx, `SELECT COUNT(*) FROM audit_logs WHERE action = $1`, audit.ActionContractAccepted).Scan(&audited); err != nil || audited != 2 {
		t.Errorf("auditoria = %d (%v)", audited, err)
	}
}

func containsCode(body []byte, code string) bool {
	var out struct {
		Code string `json:"code"`
	}
	return json.Unmarshal(body, &out) == nil && out.Code == code
}
