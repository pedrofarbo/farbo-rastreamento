package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/orders"
)

// A fatura única do mês: na contratação nada é cobrado na hora. A próxima
// fatura da conta traz o proporcional dos dias até ela, a 1ª mensalidade
// (com a 1ª parcela do rastreador), o equipamento (ou a instalação, no
// comodato) — tudo no dia da conta. Se a fatura desse dia já foi paga, vai
// para a do mês seguinte. No comodato, o encerramento lança a desinstalação.
// Precisa de FARBO_TEST_DATABASE_URL.
func TestSingleMonthlyInvoice(t *testing.T) {
	var bs *billing.Service
	env, _ := newLeadsEnv(t, func(d *Deps) {
		bs = d.Billing
		d.Config.Catalog.SetupDueDays = 0
		d.Orders = orders.NewService(d.DB, d.Billing, d.Vehicles, d.Config.Catalog, d.Log)
	})
	ctx := context.Background()
	today := func(day, month int) {
		bs.SetClock(func() time.Time { return time.Date(2026, time.Month(month), day, 12, 0, 0, 0, time.UTC) })
	}
	// 13/10/2026, com a conta no dia 10: tudo vai para 10/11, com 28 dias de
	// proporcional (de 6990 por mês: 6524).
	today(13, 10)

	admin := env.login(auth.RoleAdmin + "@leads.test")
	var davi struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/customers", map[string]any{
		"name": "Davi Lima", "email": "davi@unica.test", "phone": "(11) 94444-0000", "document": "",
		"password": userPassword,
	}, http.StatusCreated), &davi)
	token := env.login("davi@unica.test")
	env.must(token, http.MethodPut, "/api/me/address", map[string]any{
		"zipCode": "01305-000", "street": "Rua Augusta", "number": "500", "district": "Consolação", "city": "São Paulo", "state": "SP",
	}, http.StatusOK)

	type result struct {
		Subscription   billing.Subscription `json:"subscription"`
		ChargeDate     string               `json:"chargeDate"`
		SetupInvoice   *billing.Invoice     `json:"setupInvoice"`
		InstallInvoice *billing.Invoice     `json:"installInvoice"`
		ProrataInvoice *billing.Invoice     `json:"prorataInvoice"`
		FirstInvoice   *billing.Invoice     `json:"firstInvoice"`
	}
	order := func(plate string, installments int) result {
		t.Helper()
		var r result
		_ = json.Unmarshal(env.must(token, http.MethodPost, "/api/me/trackers", map[string]any{
			"vehicle": map[string]any{"name": "Moto " + plate, "plate": plate}, "installments": installments,
		}, http.StatusCreated), &r)
		return r
	}
	monthly := func(subID string) []billing.Invoice {
		t.Helper()
		rows, err := env.db.Query(ctx, `SELECT id, description, amount_cents, due_date, status FROM invoices
			WHERE subscription_id = $1 ORDER BY due_date`, subID)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []billing.Invoice
		for rows.Next() {
			var inv billing.Invoice
			if err := rows.Scan(&inv.ID, &inv.Description, &inv.AmountCents, &inv.DueDate.Time, &inv.Status); err != nil {
				t.Fatal(err)
			}
			out = append(out, inv)
		}
		return out
	}
	// A central contrata sem o limite de pedidos aguardando instalação do app.
	adminOrder := func(plate string, body map[string]any) result {
		t.Helper()
		body["vehicle"] = map[string]any{"name": "Moto " + plate, "plate": plate}
		body["plan"] = map[string]any{"planName": "Plano Mensal", "priceCents": 6990, "dueDay": 10}
		var r result
		_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/customers/"+davi.ID+"/trackers", body, http.StatusCreated), &r)
		return r
	}
	nextDue := func(subID string) string {
		t.Helper()
		var next time.Time
		_ = env.db.QueryRow(ctx, `SELECT next_due_date FROM subscriptions WHERE id = $1`, subID).Scan(&next)
		return next.Format(time.DateOnly)
	}

	// Parcelado: nada de equipamento à parte; em 10/11, a 1ª mensalidade com
	// a 1ª parcela e o proporcional. A permanência vai até a 10ª parcela.
	r := order("UNI1A11", 10)
	id := r.Subscription.ID.String()
	if r.SetupInvoice != nil || r.ChargeDate != "2026-11-10" {
		t.Errorf("pedido = %+v (vencimento %s)", r.SetupInvoice, r.ChargeDate)
	}
	if f := r.FirstInvoice; f == nil || f.DueDate.String() != "2026-11-10" || f.AmountCents != 6990+1500 ||
		f.Description != "Plano Mensal — novembro/2026 + parcela 1/10 do rastreador" {
		t.Fatalf("1ª mensalidade = %+v", r.FirstInvoice)
	}
	if p := r.ProrataInvoice; p == nil || p.DueDate.String() != "2026-11-10" || p.AmountCents != 6524 ||
		p.SubscriptionID != nil || !strings.Contains(p.Description, "proporcional de 13/10 a 10/11 (28 dias)") {
		t.Fatalf("proporcional = %+v", r.ProrataInvoice)
	}
	if c := r.Subscription.CommitmentUntil; c == nil || c.String() != "2027-08-10" {
		t.Errorf("permanência = %v", c)
	}
	if got := nextDue(id); got != "2026-12-10" {
		t.Errorf("2ª mensalidade em %s, quer 2026-12-10", got)
	}
	// A 2ª sai na antecedência de sempre, com a 2ª parcela.
	bs.GenerateInvoices(ctx)
	if got := monthly(id); len(got) != 1 {
		t.Errorf("a 2ª saiu adiantada: %+v", got)
	}
	today(30, 11)
	bs.GenerateInvoices(ctx)
	if got := monthly(id); len(got) != 2 || got[1].DueDate.String() != "2026-12-10" || got[1].AmountCents != 6990+1500 {
		t.Errorf("a 2ª, na antecedência = %+v", got)
	}

	// À vista: o equipamento também em 10/11, na mesma fatura.
	today(13, 10)
	r = order("UNI2B22", 0)
	if r.SetupInvoice == nil || r.SetupInvoice.DueDate.String() != "2026-11-10" || r.SetupInvoice.AmountCents != 15000 ||
		r.FirstInvoice == nil || r.FirstInvoice.DueDate.String() != "2026-11-10" || r.FirstInvoice.AmountCents != 6990 {
		t.Fatalf("à vista: pedido %+v, 1ª mensalidade %+v", r.SetupInvoice, r.FirstInvoice)
	}

	// A central marcando a cobrança do pedido (tela antiga): vale o dia da conta.
	var placed result
	_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/customers/"+davi.ID+"/trackers", map[string]any{
		"vehicle":        map[string]any{"name": "Carro", "plate": "UNI3C33"},
		"equipmentCents": 15000, "setupDueDate": "2026-12-13",
		"plan": map[string]any{"planName": "Plano Mensal", "priceCents": 6990, "dueDay": 10},
	}, http.StatusCreated), &placed)
	if placed.ChargeDate != "2026-11-10" || placed.SetupInvoice == nil || placed.SetupInvoice.DueDate.String() != "2026-11-10" {
		t.Fatalf("pedido pela central = %+v", placed)
	}

	// O comodato no plano do cliente: sem equipamento; a instalação na fatura.
	env.must(admin, http.MethodPut, "/api/customers/"+davi.ID+"/plan", map[string]any{
		"planName": "Plano Mensal", "priceCents": 6990, "dueDay": 10,
		"equipmentMode": "loan", "installFeeCents": 8000, "uninstallFeeCents": 5000,
	}, http.StatusOK)
	loan := adminOrder("UNI4D44", map[string]any{})
	if loan.SetupInvoice != nil || loan.InstallInvoice == nil || loan.InstallInvoice.AmountCents != 8000 ||
		loan.InstallInvoice.DueDate.String() != "2026-11-10" || loan.Subscription.EquipmentMode != "loan" ||
		loan.Subscription.UninstallFeeCents != 5000 {
		t.Fatalf("comodato = %+v / %+v / %+v", loan.SetupInvoice, loan.InstallInvoice, loan.Subscription)
	}
	// O comodato não parcela (não há o que vender).
	env.must(admin, http.MethodPost, "/api/customers/"+davi.ID+"/trackers", map[string]any{
		"vehicle": map[string]any{"name": "Moto X", "plate": "UNI9Z99"}, "installments": 3, "equipmentCents": 15000,
		"plan": map[string]any{"planName": "Plano Mensal", "priceCents": 6990, "dueDay": 10},
	}, http.StatusBadRequest)
	// Encerrar lança a desinstalação na próxima fatura; dispensar, não.
	var canceled struct {
		UninstallInvoice *billing.Invoice `json:"uninstallInvoice"`
	}
	_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/subscriptions/"+loan.Subscription.ID.String()+"/cancel", nil, http.StatusOK), &canceled)
	if u := canceled.UninstallInvoice; u == nil || u.AmountCents != 5000 || u.DueDate.String() != "2026-11-10" ||
		u.Description != "Desinstalação do rastreador (comodato): Moto UNI4D44 · UNI4D44" {
		t.Fatalf("desinstalação = %+v", canceled.UninstallInvoice)
	}
	waived := adminOrder("UNI5E55", map[string]any{})
	_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/subscriptions/"+waived.Subscription.ID.String()+"/cancel",
		map[string]any{"waiveUninstall": true}, http.StatusOK), &canceled)
	if canceled.UninstallInvoice != nil {
		t.Errorf("dispensada, mas lançou %+v", canceled.UninstallInvoice)
	}

	// A fatura de 10/11 paga: o veículo novo vai para a de 10/12, com o
	// proporcional desde hoje (58 dias).
	if _, err := env.db.Exec(ctx, `UPDATE invoices SET status = 'PAID', paid_at = NOW(), paid_via = 'MANUAL'
		WHERE customer_id = $1 AND due_date = '2026-11-10' AND status = 'OPEN'`, davi.ID); err != nil {
		t.Fatal(err)
	}
	late := adminOrder("UNI6F66", map[string]any{})
	if late.ChargeDate != "2026-12-10" || late.InstallInvoice == nil || late.InstallInvoice.DueDate.String() != "2026-12-10" ||
		late.ProrataInvoice == nil || late.ProrataInvoice.AmountCents != 13514 {
		t.Fatalf("depois da fatura paga = %s / %+v / %+v", late.ChargeDate, late.InstallInvoice, late.ProrataInvoice)
	}
}
