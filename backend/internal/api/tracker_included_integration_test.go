package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
)

// O rastreador incluso (a Farbo não vende o rastreador), numa empresa: tudo
// na fatura do mês da conta. Abaixo do preço do site, a parte do rastreador
// (R$ 12) soma nas 10 primeiras mensalidades e depois volta a mensalidade
// do plano; no preço do site, ela já está dentro. Nenhum pedido gera fatura
// de equipamento. Encerrada antes da 10ª, a parte que falta vence numa
// fatura só (ou é dispensada); a parte não muda com o dia de vencimento.
// Precisa de FARBO_TEST_DATABASE_URL.
func TestTrackerIncluded(t *testing.T) {
	var bs *billing.Service
	env, _ := newLeadsEnv(t, func(d *Deps) { bs = d.Billing })
	ctx := context.Background()
	// 07/10/2026: a primeira mensalidade (dia 10) já sai com o pedido.
	bs.SetClock(func() time.Time { return time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC) })

	admin := env.login(auth.RoleAdmin + "@leads.test")
	var opts struct {
		Checkout struct {
			PlanCents    int `json:"planCents"`
			InsanosCents int `json:"insanosCents"`
		} `json:"checkout"`
		Tracker struct {
			Cents  int `json:"cents"`
			Months int `json:"months"`
		} `json:"tracker"`
	}
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/order-options", nil, http.StatusOK), &opts)
	if opts.Checkout.PlanCents != 6990 || opts.Checkout.InsanosCents != 2790 || opts.Tracker.Cents != 1200 || opts.Tracker.Months != 10 {
		t.Fatalf("opções do pedido = %+v", opts)
	}

	var frota struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/customers", map[string]any{
		"name": "Frota Inclusa", "email": "frota@incluso.test", "phone": "(11) 97777-0000", "document": "11.222.333/0001-81",
		"password": userPassword,
	}, http.StatusCreated), &frota)
	env.must(env.login("frota@incluso.test"), http.MethodPut, "/api/me/address", map[string]any{
		"zipCode": "01305-000", "street": "Rua Augusta", "number": "500", "district": "Consolação", "city": "São Paulo", "state": "SP",
	}, http.StatusOK)

	type result struct {
		Subscription billing.Subscription `json:"subscription"`
		SetupInvoice *billing.Invoice     `json:"setupInvoice"`
	}
	order := func(plate string, priceCents int, extra map[string]any) result {
		t.Helper()
		body := map[string]any{
			"vehicle": map[string]any{"name": "Carro " + plate, "plate": plate},
			"plan":    map[string]any{"planName": "Plano Frota", "priceCents": priceCents, "dueDay": 10},
		}
		for k, v := range extra {
			body[k] = v
		}
		var out result
		_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/customers/"+frota.ID+"/trackers", body, http.StatusCreated), &out)
		return out
	}
	monthly := func(subID string) []billing.Invoice {
		t.Helper()
		rows, err := env.db.Query(ctx, `SELECT description, amount_cents, due_date, status FROM invoices
			WHERE subscription_id = $1 ORDER BY due_date`, subID)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []billing.Invoice
		for rows.Next() {
			var inv billing.Invoice
			if err := rows.Scan(&inv.Description, &inv.AmountCents, &inv.DueDate.Time, &inv.Status); err != nil {
				t.Fatal(err)
			}
			out = append(out, inv)
		}
		return out
	}

	// Abaixo do preço do site (R$ 49,90): pela regra, + R$ 12 nas 10
	// primeiras (de outubro/2026 a julho/2027) e depois R$ 49,90.
	added := order("INC1A11", 4990, nil)
	sub := added.Subscription
	if added.SetupInvoice != nil {
		t.Fatalf("o rastreador não é vendido: fatura do pedido = %+v", added.SetupInvoice)
	}
	if sub.TrackerCents != 1200 || sub.TrackerMonths != 10 || !sub.TrackerAdded || sub.TrackerUntil == nil ||
		sub.TrackerUntil.Format(time.DateOnly) != "2027-08-10" || sub.EquipmentMode != billing.EquipmentSale || sub.PriceCents != 4990 {
		t.Fatalf("assinatura com o rastreador incluso = %+v", sub)
	}
	repo := billing.NewRepository(env.db)
	for i := 0; i < 2; i++ {
		if _, err := repo.GenerateDue(ctx, billing.NewDate(2027, 9, 10)); err != nil {
			t.Fatal(err)
		}
	}
	invoices := monthly(sub.ID.String())
	if len(invoices) != 12 {
		t.Fatalf("mensalidades = %d: %+v", len(invoices), invoices)
	}
	for i, inv := range invoices {
		want := 4990 + 1200
		if i >= 10 {
			want = 4990
		}
		if inv.AmountCents != want || strings.Contains(inv.Description, "rastreador") {
			t.Errorf("mensalidade %d (%s) = %d %q, quer %d sem falar do rastreador", i+1, inv.DueDate.Format(time.DateOnly),
				inv.AmountCents, inv.Description, want)
		}
	}

	// No preço do site: a parte do rastreador já está dentro (só conta para
	// o encerramento antecipado).
	inside := order("INC2B22", 6990, nil).Subscription
	if inside.TrackerCents != 1200 || inside.TrackerMonths != 10 || inside.TrackerAdded {
		t.Fatalf("no preço do site = %+v", inside)
	}
	if invoices := monthly(inside.ID.String()); len(invoices) != 1 || invoices[0].AmountCents != 6990 {
		t.Fatalf("mensalidade no preço do site = %+v", invoices)
	}
	// O aparelho do cliente: sem rastreador incluso.
	own := order("INC3C33", 4990, map[string]any{"tracker": map[string]any{"cents": 0, "months": 0}}).Subscription
	if own.TrackerCents != 0 || own.TrackerUntil != nil {
		t.Fatalf("aparelho do cliente = %+v", own)
	}
	env.must(admin, http.MethodPost, "/api/customers/"+frota.ID+"/trackers", map[string]any{
		"vehicle": map[string]any{"name": "Errado", "plate": "INC9Z99"}, "tracker": map[string]any{"cents": 1200, "months": 0},
		"plan": map[string]any{"planName": "Plano Frota", "priceCents": 3990, "dueDay": 10},
	}, http.StatusBadRequest)

	cancel := func(id string, body any, want int) []byte {
		t.Helper()
		return env.must(admin, http.MethodPost, "/api/subscriptions/"+id+"/cancel", body, want)
	}
	var canceled struct {
		billing.Subscription
		BalanceInvoice *billing.Invoice `json:"balanceInvoice"`
	}

	// Encerrar antes da 10ª: tem de dizer o que fazer com o saldo. A
	// mensalidade de outubro (ainda não vencida) cai: faltam as 10 partes.
	if out := cancel(inside.ID.String(), nil, http.StatusConflict); !strings.Contains(string(out), "EQUIPMENT_BALANCE") {
		t.Errorf("sem a escolha: %s", out)
	}
	_ = json.Unmarshal(cancel(inside.ID.String(), map[string]string{"equipmentBalance": "CHARGE"}, http.StatusOK), &canceled)
	balance := canceled.BalanceInvoice
	if canceled.Status != billing.SubscriptionCanceled || balance == nil || balance.AmountCents != 10*1200 ||
		balance.SubscriptionID != nil ||
		!strings.HasPrefix(balance.Description, "Rastreador incluso: saldo de 10 de 10 mensalidades (assinatura encerrada antes da 10ª)") ||
		!strings.Contains(balance.Description, "INC2B22") {
		t.Fatalf("encerrada com o saldo = %+v / %+v", canceled.Subscription, balance)
	}

	// Com 4 mensalidades pagas (outubro a janeiro) e as outras canceladas
	// ao encerrar: faltam 6 partes. Dispensar não cobra nada.
	if _, err := env.db.Exec(ctx, `UPDATE invoices SET status = 'PAID', paid_at = NOW(), paid_via = 'MANUAL'
		WHERE subscription_id = $1 AND due_date <= '2027-01-10'`, sub.ID); err != nil {
		t.Fatal(err)
	}
	bs.SetClock(func() time.Time { return time.Date(2027, 1, 20, 12, 0, 0, 0, time.UTC) })
	cents, months, err := repo.TrackerBalance(ctx, &sub, bs.Today())
	if err != nil || cents != 6*1200 || months != 6 {
		t.Fatalf("saldo = %d em %d mensalidades (%v)", cents, months, err)
	}
	_ = json.Unmarshal(cancel(sub.ID.String(), map[string]string{"equipmentBalance": "WAIVE"}, http.StatusOK), &canceled)
	if canceled.BalanceInvoice != nil {
		t.Errorf("dispensado gerou fatura: %+v", canceled.BalanceInvoice)
	}

	// O aparelho do cliente encerra sem perguntar.
	_ = json.Unmarshal(cancel(own.ID.String(), nil, http.StatusOK), &canceled)
	if canceled.Status != billing.SubscriptionCanceled || canceled.BalanceInvoice != nil {
		t.Errorf("aparelho do cliente = %+v", canceled)
	}

	// Outra empresa, com a parte escolhida pela central (+ R$ 20 nas 3
	// primeiras), muda o dia de vencimento: a parte acompanha o dia novo e
	// continua nas mesmas 3 mensalidades.
	bs.SetClock(func() time.Time { return time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC) })
	_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/customers", map[string]any{
		"name": "Dia Novo", "email": "dia@incluso.test", "phone": "(11) 97777-0001", "document": "45.723.174/0001-10",
		"password": userPassword,
	}, http.StatusCreated), &frota)
	var custom result
	_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/customers/"+frota.ID+"/trackers", map[string]any{
		"vehicle": map[string]any{"name": "Van", "plate": "INC5E55"}, "tracker": map[string]any{"cents": 2000, "months": 3, "added": true},
		"plan": map[string]any{"planName": "Plano Frota", "priceCents": 3990, "dueDay": 10},
	}, http.StatusCreated), &custom)
	if s := custom.Subscription; s.TrackerCents != 2000 || s.TrackerMonths != 3 || s.TrackerUntil.Format(time.DateOnly) != "2027-01-10" {
		t.Fatalf("parte escolhida = %+v", s)
	}
	env.must(admin, http.MethodPost, "/api/subscriptions/"+custom.Subscription.ID.String()+"/due-day", map[string]any{"dueDay": 20},
		http.StatusOK)
	for i := 0; i < 2; i++ {
		if _, err := repo.GenerateDue(ctx, billing.NewDate(2027, 2, 20)); err != nil {
			t.Fatal(err)
		}
	}
	var got []string
	for _, inv := range monthly(custom.Subscription.ID.String()) {
		got = append(got, inv.DueDate.Format("01-02")+"="+strconv.Itoa(inv.AmountCents))
	}
	if strings.Join(got, " ") != "10-20=5990 11-20=5990 12-20=5990 01-20=3990 02-20=3990" {
		t.Errorf("mensalidades no dia novo = %v", got)
	}
}
