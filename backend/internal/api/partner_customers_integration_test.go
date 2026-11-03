package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/affiliates"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/contract"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/orders"
)

// Os clientes na página do afiliado: só com a senha na página; com ela, o
// nome, os veículos (a placa e o rastreador) e o pagamento mês a mês — pago,
// vencido, sem fatura. Quem contratou pelo site e não pagou não aparece.
// Precisa de FARBO_TEST_DATABASE_URL.
func TestPartnerCustomers(t *testing.T) {
	env, _ := newLeadsEnv(t, func(d *Deps) {
		d.Config.Catalog.SetupDueDays = 0
		d.Orders = orders.NewService(d.DB, d.Billing, d.Vehicles, d.Config.Catalog, d.Log)
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
	var rafael affiliates.Affiliate
	_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/affiliates", map[string]any{
		"name": "Rafael Magalhães", "code": "rafael", "active": true,
	}, http.StatusCreated), &rafael)
	page := "/api/public/partner/" + rafael.ReportToken

	var o struct {
		ContractVersion string `json:"contractVersion"`
	}
	_ = json.Unmarshal(env.must("", http.MethodGet, "/api/public/checkout", nil, http.StatusOK), &o)
	address := map[string]any{"zipCode": "01305-000", "street": "Rua Augusta", "number": "500", "district": "Consolação",
		"city": "São Paulo", "state": "SP"}
	checkout := func(name, email, document, plate string) uuid.UUID {
		t.Helper()
		env.must("", http.MethodPost, "/api/public/checkout", map[string]any{
			"name": name, "email": email, "phone": "(11) 95555-0000", "document": document, "password": "senha-forte-1",
			"vehicle": map[string]any{"kind": "CAR", "brand": "Fiat", "model": "Uno", "plate": plate}, "address": address,
			"contractVersion": o.ContractVersion, "accept": true, "ref": "rafael",
		}, http.StatusCreated)
		var id uuid.UUID
		if err := env.db.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, email).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	ana := checkout("Ana Souza", "ana@rafael.test", "529.982.247-25", "ANA1A11")
	checkout("Duane Sem Pagar", "duane@rafael.test", "390.533.447-05", "DUA2B22")
	// A Ana pagou o mês; o rastreador já está no carro; e ficou devendo o mês passado.
	if _, err := env.db.Exec(ctx, `UPDATE invoices SET status = 'PAID', paid_at = NOW() WHERE customer_id = $1`, ana); err != nil {
		t.Fatal(err)
	}
	var device uuid.UUID
	if err := env.db.QueryRow(ctx, `INSERT INTO devices (imei) VALUES ('869247061905555') RETURNING id`).Scan(&device); err != nil {
		t.Fatal(err)
	}
	if _, err := env.db.Exec(ctx, `UPDATE vehicles SET device_id = $2 WHERE owner_id = $1`, ana, device); err != nil {
		t.Fatal(err)
	}
	lastMonth := time.Now().AddDate(0, -1, 0)
	if _, err := env.db.Exec(ctx, `INSERT INTO invoices (customer_id, description, amount_cents, due_date, status)
		VALUES ($1, 'Mensalidade de antes', 6990, $2, 'OPEN')`, ana, time.Date(lastMonth.Year(), lastMonth.Month(), 5, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}

	// Sem a senha na página, os clientes não aparecem.
	if status, out := env.do("", http.MethodGet, page+"/customers", nil); status != http.StatusForbidden || !containsCode(out, "PASSWORD_REQUIRED") {
		t.Fatalf("sem senha = %d %s", status, out)
	}
	env.must(admin, http.MethodPatch, "/api/affiliates/"+rafael.ID.String(), map[string]any{
		"name": "Rafael Magalhães", "code": "rafael", "active": true, "reportPassword": "senha-do-rafael",
	}, http.StatusOK)
	_, raw := env.do("", http.MethodPost, page+"/session", map[string]any{"password": "senha-do-rafael"})
	var login struct {
		Session string `json:"session"`
	}
	_ = json.Unmarshal(raw, &login)
	status, out := env.doWith("", http.MethodGet, page+"/customers", nil, map[string]string{partnerSessionHeader: login.Session})
	if status != http.StatusOK {
		t.Fatalf("com a senha = %d %s", status, out)
	}
	var list []affiliates.PartnerCustomer
	_ = json.Unmarshal(out, &list)
	if len(list) != 1 || list[0].Name != "Ana Souza" {
		t.Fatalf("clientes = %+v", list)
	}
	ana1 := list[0]
	if len(ana1.Vehicles) != 1 || ana1.Vehicles[0].Plate != "ANA1A11" || ana1.Vehicles[0].IMEI != "869247061905555" {
		t.Errorf("veículos = %+v", ana1.Vehicles)
	}
	if len(ana1.Months) != 6 || ana1.Months[0].Month != time.Now().Format("2006-01") ||
		ana1.Months[0].Status != affiliates.PaymentPaid || ana1.Months[1].Status != affiliates.PaymentOverdue ||
		ana1.Months[2].Status != affiliates.PaymentNone {
		t.Errorf("meses = %+v", ana1.Months)
	}
	// A página diz que tem senha (a seção dos clientes aparece).
	var report struct {
		Protected bool `json:"protected"`
	}
	_, out = env.doWith("", http.MethodGet, page, nil, map[string]string{partnerSessionHeader: login.Session})
	_ = json.Unmarshal(out, &report)
	if !report.Protected {
		t.Errorf("página = %s", out)
	}
}
