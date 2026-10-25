package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/contract"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/orders"
)

// TestCancelOrder: a central cancela o pedido antes do envio — as cobranças
// em aberto caem, o pago volta como valor a devolver, a assinatura acaba e o
// veículo sai da conta; enviado, não cancela; só o administrador. Precisa de
// FARBO_TEST_DATABASE_URL.
func TestCancelOrder(t *testing.T) {
	env, _ := newLeadsEnv(t, func(d *Deps) {
		d.Config.Catalog.SetupDueDays = 0
		d.Orders = orders.NewService(d.DB, d.Billing, d.Vehicles, d.Config.Catalog, d.Log)
	})
	ctx := context.Background()
	address := map[string]any{"zipCode": "01305-000", "street": "Rua Augusta", "number": "500", "district": "Consolação",
		"city": "São Paulo", "state": "SP"}
	place := func(email, document string) {
		t.Helper()
		env.must("", http.MethodPost, "/api/public/checkout", map[string]any{
			"name": "Cliente Teste", "email": email, "phone": "(11) 95555-0000", "document": document, "password": "senha-forte-1",
			"vehicle": map[string]any{"kind": "MOTORCYCLE", "brand": "Honda", "model": "CG 160", "plate": ""},
			"address": address, "contractVersion": contract.Version, "accept": true, "farboInstall": true,
		}, http.StatusCreated)
	}
	place("lia@cancela.test", "529.982.247-25")
	place("rui@cancela.test", "390.533.447-05")

	orderOf := func(email string) (fulfillmentID, vehicleID, subscriptionID string) {
		t.Helper()
		if err := env.db.QueryRow(ctx, `SELECT f.id, f.vehicle_id, f.subscription_id FROM fulfillments f JOIN users u ON u.id = f.customer_id
			WHERE u.email = $1`, email).Scan(&fulfillmentID, &vehicleID, &subscriptionID); err != nil {
			t.Fatalf("pedido de %s: %v", email, err)
		}
		return
	}
	lia, liaVehicle, liaSub := orderOf("lia@cancela.test")
	// O rastreador e a instalação já pagos; a 1ª mensalidade, em aberto.
	var paid int
	if err := env.db.QueryRow(ctx, `UPDATE invoices SET status = 'PAID', paid_at = NOW()
		WHERE customer_id = (SELECT id FROM users WHERE email = 'lia@cancela.test') AND subscription_id IS NULL
		RETURNING amount_cents`).Scan(&paid); err != nil {
		t.Fatal(err)
	}

	admin := env.login(auth.RoleAdmin + "@leads.test")
	path := "/api/fulfillments/" + lia + "/cancel"
	env.must(env.login(auth.RoleOperator+"@leads.test"), http.MethodPost, path, map[string]any{"reason": "teste"}, http.StatusForbidden)
	env.must(admin, http.MethodPost, path, map[string]any{"reason": " "}, http.StatusBadRequest)

	var res struct {
		CanceledInvoices int      `json:"canceledInvoices"`
		PaidCents        int      `json:"paidCents"`
		PaidInvoices     []string `json:"paidInvoices"`
		VehicleName      string   `json:"vehicleName"`
	}
	_ = json.Unmarshal(env.must(admin, http.MethodPost, path, map[string]any{"reason": "cliente desistiu"}, http.StatusOK), &res)
	if res.CanceledInvoices < 1 || res.PaidCents == 0 || len(res.PaidInvoices) == 0 || res.VehicleName == "" {
		t.Fatalf("cancelamento = %+v (pago no banco: %d)", res, paid)
	}
	var open, vehicles, fulfillments int
	var subStatus string
	_ = env.db.QueryRow(ctx, `SELECT count(*) FROM invoices WHERE customer_id = (SELECT id FROM users WHERE email = 'lia@cancela.test')
		AND status = 'OPEN'`).Scan(&open)
	_ = env.db.QueryRow(ctx, `SELECT count(*) FROM vehicles WHERE id = $1`, liaVehicle).Scan(&vehicles)
	_ = env.db.QueryRow(ctx, `SELECT count(*) FROM fulfillments WHERE id = $1`, lia).Scan(&fulfillments)
	_ = env.db.QueryRow(ctx, `SELECT status FROM subscriptions WHERE id = $1`, liaSub).Scan(&subStatus)
	if open != 0 || vehicles != 0 || fulfillments != 0 || subStatus != "CANCELED" {
		t.Fatalf("depois: faturas abertas %d, veículo %d, pedido %d, assinatura %s", open, vehicles, fulfillments, subStatus)
	}
	var audited int
	_ = env.db.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE action = 'ORDER_CANCELED' AND metadata->>'reason' = 'cliente desistiu'`).Scan(&audited)
	if audited != 1 {
		t.Errorf("auditoria do cancelamento = %d", audited)
	}

	// O do Rui já saiu para entrega: não cancela por aqui.
	rui, _, _ := orderOf("rui@cancela.test")
	if _, err := env.db.Exec(ctx, `UPDATE fulfillments SET tracker_status = 'SHIPPED' WHERE id = $1`, rui); err != nil {
		t.Fatal(err)
	}
	if status, out := env.do(admin, http.MethodPost, "/api/fulfillments/"+rui+"/cancel", map[string]any{"reason": "teste"}); status != http.StatusBadRequest ||
		!strings.Contains(string(out), "saiu para entrega") {
		t.Fatalf("pedido enviado = %d %s", status, out)
	}
}
