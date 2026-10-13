package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/orders"
)

// A 1ª mensalidade vence com o pedido, no dia da contratação, e se paga
// junto com ele (já com a 1ª parcela do rastreador); a 2ª vence no dia da
// conta, pelo menos um mês depois, e as seguintes saem na antecedência de
// sempre. A central pode marcar a cobrança do pedido para depois: a 1ª
// mensalidade vai junto, e sai na hora mesmo assim. A 1ª cancelada não
// volta. Precisa de FARBO_TEST_DATABASE_URL.
func TestFirstInvoiceUpfront(t *testing.T) {
	var bs *billing.Service
	env, _ := newLeadsEnv(t, func(d *Deps) {
		bs = d.Billing
		// Como em produção: a cobrança do pedido vence no dia.
		d.Config.Catalog.SetupDueDays = 0
		d.Orders = orders.NewService(d.DB, d.Billing, d.Vehicles, d.Config.Catalog, d.Log)
	})
	ctx := context.Background()
	today := func(day, month int) {
		bs.SetClock(func() time.Time { return time.Date(2026, time.Month(month), day, 12, 0, 0, 0, time.UTC) })
	}
	// 13/10/2026, com a conta no dia 10: a 2ª não pode vencer em 10/11 (menos
	// de um mês depois), vence em 10/12.
	today(13, 10)

	admin := env.login(auth.RoleAdmin + "@leads.test")
	var davi struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/customers", map[string]any{
		"name": "Davi Lima", "email": "davi@adiantada.test", "phone": "(11) 94444-0000", "document": "",
		"password": userPassword,
	}, http.StatusCreated), &davi)
	token := env.login("davi@adiantada.test")
	env.must(token, http.MethodPut, "/api/me/address", map[string]any{
		"zipCode": "01305-000", "street": "Rua Augusta", "number": "500", "district": "Consolação", "city": "São Paulo", "state": "SP",
	}, http.StatusOK)

	type result struct {
		Subscription billing.Subscription `json:"subscription"`
		SetupInvoice *billing.Invoice     `json:"setupInvoice"`
		FirstInvoice *billing.Invoice     `json:"firstInvoice"`
	}
	order := func(plate string, installments int) result {
		t.Helper()
		var r result
		_ = json.Unmarshal(env.must(token, http.MethodPost, "/api/me/trackers", map[string]any{
			"vehicle": map[string]any{"name": "Moto " + plate, "plate": plate}, "installments": installments,
		}, http.StatusCreated), &r)
		return r
	}
	invoices := func(subID string) []billing.Invoice {
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
	nextDue := func(subID string) string {
		t.Helper()
		var next time.Time
		_ = env.db.QueryRow(ctx, `SELECT next_due_date FROM subscriptions WHERE id = $1`, subID).Scan(&next)
		return next.Format(time.DateOnly)
	}

	// Parcelado e sem frete: nada de equipamento a pagar, mas a 1ª
	// mensalidade, com a 1ª parcela, vence hoje; a permanência vai até a
	// mensalidade com a 10ª (de 10/12 em diante, a 9ª depois dela).
	r := order("ADI1A11", 10)
	id := r.Subscription.ID.String()
	if r.SetupInvoice != nil {
		t.Errorf("fatura do pedido = %+v", r.SetupInvoice)
	}
	if f := r.FirstInvoice; f == nil || f.DueDate.String() != "2026-10-13" || f.AmountCents != 6990+1500 ||
		f.Status != billing.InvoiceOpen || f.Description != "Plano Mensal — outubro/2026 + parcela 1/10 do rastreador" {
		t.Fatalf("1ª mensalidade = %+v", r.FirstInvoice)
	}
	if c := r.Subscription.CommitmentUntil; c == nil || c.String() != "2027-08-10" {
		t.Errorf("permanência = %v", c)
	}
	if got := nextDue(id); got != "2026-12-10" {
		t.Errorf("2ª mensalidade em %s, quer 2026-12-10", got)
	}

	// A rotina de novo não adianta a 2ª; ela sai na antecedência, com a 2ª
	// parcela.
	bs.GenerateInvoices(ctx)
	if got := invoices(id); len(got) != 1 {
		t.Errorf("a 2ª saiu adiantada: %+v", got)
	}
	today(30, 11)
	bs.GenerateInvoices(ctx)
	if got := invoices(id); len(got) != 2 || got[1].DueDate.String() != "2026-12-10" || got[1].AmountCents != 6990+1500 {
		t.Errorf("a 2ª, na antecedência = %+v", got)
	}

	// À vista: o equipamento e a 1ª mensalidade vencem hoje, para pagar juntos.
	today(13, 10)
	r = order("ADI2B22", 0)
	if r.SetupInvoice == nil || r.SetupInvoice.DueDate.String() != "2026-10-13" || r.SetupInvoice.AmountCents != 15000 ||
		r.FirstInvoice == nil || r.FirstInvoice.DueDate.String() != "2026-10-13" || r.FirstInvoice.AmountCents != 6990 {
		t.Fatalf("à vista: pedido %+v, 1ª mensalidade %+v", r.SetupInvoice, r.FirstInvoice)
	}

	// A central cancela a 1ª: ela não volta sozinha (nem a 2ª sai no lugar).
	env.must(admin, http.MethodPost, "/api/invoices/"+r.FirstInvoice.ID.String()+"/cancel", nil, http.StatusOK)
	bs.GenerateInvoices(ctx)
	if got := invoices(r.Subscription.ID.String()); len(got) != 1 || got[0].Status != billing.InvoiceCanceled {
		t.Errorf("depois de cancelar a 1ª = %+v", got)
	}

	// A central marca a cobrança do pedido para daqui a um mês: a 1ª
	// mensalidade vence junto e já fica em aberto (fora da antecedência).
	var placed result
	_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/customers/"+davi.ID+"/trackers", map[string]any{
		"vehicle":        map[string]any{"name": "Carro", "plate": "ADI3C33"},
		"equipmentCents": 15000, "setupDueDate": "2026-11-13",
		"plan": map[string]any{"planName": "Plano Mensal", "priceCents": 6990, "dueDay": 10},
	}, http.StatusCreated), &placed)
	if placed.FirstInvoice == nil || placed.FirstInvoice.DueDate.String() != "2026-11-13" {
		t.Fatalf("1ª mensalidade com a cobrança marcada = %+v", placed.FirstInvoice)
	}
	// A 2ª, pelo menos um mês depois da 1ª.
	if got := nextDue(placed.Subscription.ID.String()); got != "2027-01-10" {
		t.Errorf("2ª mensalidade em %s, quer 2027-01-10", got)
	}
}
