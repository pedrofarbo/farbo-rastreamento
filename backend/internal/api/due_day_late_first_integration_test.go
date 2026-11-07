package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
)

// O caso do 2º veículo do Victor (out/2026): a central marcou a cobrança do
// pedido para 20/11 com a conta no dia 10, e a 2ª mensalidade foi para 10/01
// (pelo menos um mês depois da 1ª). Mudar a conta para o dia 20 refaz a
// regra no dia novo: a 2ª vai para 20/12 (só trocar o dia a deixaria em
// 20/01, sem dezembro), e o rastreador incluso acompanha a mensalidade que
// falta com a parte dele.
// Precisa de FARBO_TEST_DATABASE_URL.
func TestDueDayChangeAfterLateFirstInvoice(t *testing.T) {
	var bs *billing.Service
	env, _ := newLeadsEnv(t, func(d *Deps) { bs = d.Billing })
	ctx := context.Background()
	bs.SetClock(func() time.Time { return time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC) })
	admin := env.login(auth.RoleAdmin + "@leads.test")
	var victor struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/customers", map[string]any{
		"name": "Victor", "email": "victor@dia.test", "phone": "(11) 97777-0001", "document": "", "password": userPassword,
	}, http.StatusCreated), &victor)
	env.must(env.login("victor@dia.test"), http.MethodPut, "/api/me/address", map[string]any{
		"zipCode": "01305-000", "street": "Rua Augusta", "number": "500", "district": "Consolação", "city": "São Paulo", "state": "SP",
	}, http.StatusOK)
	order := func(plate string, body map[string]any) billing.Subscription {
		t.Helper()
		body["vehicle"] = map[string]any{"name": "Moto " + plate, "plate": plate}
		body["plan"] = map[string]any{"planName": "Especial", "priceCents": 3990, "dueDay": 10}
		var out struct {
			Subscription billing.Subscription `json:"subscription"`
		}
		_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/customers/"+victor.ID+"/trackers", body, http.StatusCreated), &out)
		return out.Subscription
	}

	// A conta no dia 10; o 2º veículo com a cobrança do pedido em 20/11 e o
	// rastreador incluso nas 2 primeiras mensalidades.
	order("VIC1A11", map[string]any{"tracker": map[string]any{"cents": 0, "months": 0}})
	second := order("VIC2B22", map[string]any{"tracker": map[string]any{"cents": 1200, "months": 2, "added": true}})
	// Como ficou o 2º veículo dele, de antes da fatura única: a 1ª
	// mensalidade em 20/11 e a 2ª (a última com a parte do rastreador) em 10/01.
	if _, err := env.db.Exec(ctx, `UPDATE invoices SET due_date = '2026-11-20' WHERE subscription_id = $1`, second.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := env.db.Exec(ctx, `UPDATE subscriptions SET next_due_date = '2027-01-10', tracker_until = '2027-02-10'
		WHERE id = $1`, second.ID); err != nil {
		t.Fatal(err)
	}
	// Como está gravado (a 1ª mensalidade já saiu, em 20/11).
	stored := func() (day int, next, end string) {
		t.Helper()
		var n, c time.Time
		if err := env.db.QueryRow(ctx, `SELECT due_day, next_due_date, tracker_until FROM subscriptions WHERE id = $1`, second.ID).
			Scan(&day, &n, &c); err != nil {
			t.Fatal(err)
		}
		return day, n.Format(time.DateOnly), c.Format(time.DateOnly)
	}
	if day, next, end := stored(); day != 10 || next != "2027-01-10" || end != "2027-02-10" {
		t.Fatalf("antes: dia %d, próxima %s, rastreador até %s", day, next, end)
	}

	// A conta vai para o dia 20: a parte do rastreador que falta vai na de
	// 20/12 (a próxima), e só nela.
	env.must(admin, http.MethodPost, "/api/subscriptions/"+second.ID.String()+"/due-day", map[string]any{"dueDay": 20}, http.StatusOK)
	if day, next, end := stored(); day != 20 || next != "2026-12-20" || end != "2027-01-20" {
		t.Errorf("depois: dia %d, próxima %s, rastreador até %s (quer 2026-12-20 e 2027-01-20)", day, next, end)
	}
	var first string
	_ = env.db.QueryRow(ctx, `SELECT to_char(min(due_date), 'YYYY-MM-DD') FROM invoices WHERE subscription_id = $1`, second.ID).Scan(&first)
	if first != "2026-11-20" {
		t.Errorf("a 1ª mensalidade foi para %s", first)
	}
}
