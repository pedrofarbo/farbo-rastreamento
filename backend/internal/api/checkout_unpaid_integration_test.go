package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/affiliates"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/contract"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/orders"
)

// Quem contrata pelo site só vira cliente quando paga: sem pagamento, a
// conta não aparece na lista de clientes, nos números do afiliado nem em
// Pedidos; vencida a cobrança (o Pix na hora; o boleto, com a folga da
// compensação), o cadastro sai — a conta e o veículo —, e o pré-cliente fica
// com a anotação. Pagou, é cliente e não sai. Precisa de
// FARBO_TEST_DATABASE_URL.
func TestUnpaidCheckoutIsNotACustomer(t *testing.T) {
	var svc *orders.Service
	env, _ := newLeadsEnv(t, func(d *Deps) {
		d.Config.Catalog.SetupDueDays = 0
		d.Orders = orders.NewService(d.DB, d.Billing, d.Vehicles, d.Config.Catalog, d.Log)
		svc = d.Orders
		doc, err := contract.Render(contract.Params{Company: config.Company{Name: "Farbo Rastreadores"}, HistoryOptions: []int{7, 14, 30}})
		if err != nil {
			t.Fatal(err)
		}
		c := contract.NewService(d.DB, doc, &recordingContractMailer{}, d.Log)
		c.SetSync()
		d.Contract = c
	})
	ctx := context.Background()
	admin := env.login(auth.RoleAdmin + "@leads.test")
	env.must(admin, http.MethodPost, "/api/affiliates", map[string]any{"name": "Rafael Magalhães", "code": "rafael", "active": true},
		http.StatusCreated)

	var o struct {
		ContractVersion string `json:"contractVersion"`
	}
	_ = json.Unmarshal(env.must("", http.MethodGet, "/api/public/checkout", nil, http.StatusOK), &o)
	address := map[string]any{"zipCode": "01305-000", "street": "Rua Augusta", "number": "500", "district": "Consolação",
		"city": "São Paulo", "state": "SP"}
	checkout := func(name, email, document string) uuid.UUID {
		t.Helper()
		env.must("", http.MethodPost, "/api/public/checkout", map[string]any{
			"name": name, "email": email, "phone": "(11) 95555-0000", "document": document, "password": "senha-forte-1",
			"vehicle": map[string]any{"kind": "CAR", "brand": "Fiat", "model": "Uno", "plate": ""}, "address": address,
			"contractVersion": o.ContractVersion, "accept": true, "ref": "rafael",
		}, http.StatusCreated)
		var id uuid.UUID
		if err := env.db.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, email).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	duane := checkout("Duane Victoria", "duane@site.test", "529.982.247-25")
	paid := checkout("Paula Pagou", "paula@site.test", "390.533.447-05")
	if _, err := env.db.Exec(ctx, `UPDATE invoices SET status = 'PAID', paid_at = NOW() WHERE customer_id = $1`, paid); err != nil {
		t.Fatal(err)
	}

	// Sem pagamento, a Duane não é cliente: nem na lista, nem no afiliado, nem em Pedidos.
	var customers []struct {
		ID uuid.UUID `json:"id"`
	}
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/customers", nil, http.StatusOK), &customers)
	listed := map[uuid.UUID]bool{}
	for _, c := range customers {
		listed[c.ID] = true
	}
	if listed[duane] || !listed[paid] {
		t.Errorf("lista de clientes: duane %v, paula %v", listed[duane], listed[paid])
	}
	var list []affiliates.Affiliate
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/affiliates", nil, http.StatusOK), &list)
	if len(list) != 1 || list[0].Customers != 1 || list[0].ActiveVehicles != 1 {
		t.Errorf("afiliado = %+v", list)
	}
	raw := env.must(admin, http.MethodGet, "/api/fulfillments", nil, http.StatusOK)
	if strings.Contains(string(raw), duane.String()) || !strings.Contains(string(raw), paid.String()) {
		t.Errorf("pedidos = %s", raw)
	}

	// O Pix gerado (vale 24 h) e um boleto (compensa dias depois).
	charge := func(method string, expires time.Time) {
		t.Helper()
		if _, err := env.db.Exec(ctx, `
			INSERT INTO payment_charges (invoice_id, provider, provider_charge_id, amount_cents, status, method, expires_at)
			SELECT id, 'abacatepay', $2, amount_cents, 'PENDING', $3, $4 FROM invoices WHERE customer_id = $1 LIMIT 1`,
			duane, uuid.NewString(), method, expires); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	charge("PIX", now.Add(24*time.Hour))
	exists := func() bool {
		var n int
		_ = env.db.QueryRow(ctx, `SELECT count(*) FROM users WHERE id = $1`, duane).Scan(&n)
		return n == 1
	}
	// Dentro do prazo do Pix, fica.
	if n, err := svc.PurgeUnpaidCheckouts(ctx, now.Add(12*time.Hour)); err != nil || n != 0 || !exists() {
		t.Fatalf("antes de o Pix vencer: %d %v", n, err)
	}
	// Com um boleto que ainda pode compensar, também fica.
	charge("BOLETO", now.Add(48*time.Hour))
	if n, _ := svc.PurgeUnpaidCheckouts(ctx, now.Add(4*24*time.Hour)); n != 0 || !exists() {
		t.Fatalf("com o boleto compensando: %d", n)
	}
	// Vencido tudo: o cadastro sai; quem pagou, não.
	n, err := svc.PurgeUnpaidCheckouts(ctx, now.Add(8*24*time.Hour))
	if err != nil || n != 1 || exists() {
		t.Fatalf("vencido: %d %v (existe %v)", n, err, exists())
	}
	var vehicles, paidLeft int
	_ = env.db.QueryRow(ctx, `SELECT count(*) FROM vehicles WHERE name = 'Fiat Uno' AND owner_id IS NULL`).Scan(&vehicles)
	_ = env.db.QueryRow(ctx, `SELECT count(*) FROM users WHERE id = $1`, paid).Scan(&paidLeft)
	if vehicles != 0 || paidLeft != 1 {
		t.Errorf("veículo sem dono = %d, quem pagou = %d", vehicles, paidLeft)
	}
	var status, notes string
	if err := env.db.QueryRow(ctx, `SELECT status, notes FROM leads WHERE lower(email) = 'duane@site.test'`).Scan(&status, &notes); err != nil {
		t.Fatal(err)
	}
	if status != "NEW" || !strings.Contains(notes, "não pagou: o cadastro foi apagado") {
		t.Errorf("pré-cliente = %s %q", status, notes)
	}
}
