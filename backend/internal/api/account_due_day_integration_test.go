package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
)

// O vencimento da conta: todas as assinaturas ativas do cliente vencem no
// mesmo dia. O veículo novo entra no dia da conta (mesmo que a central peça
// outro); mudar o dia de uma assinatura muda a conta inteira (e o plano
// dela); o plano da conta com outro dia leva todas junto; numa conta antiga
// com dias diferentes, vale o mais comum. Precisa de FARBO_TEST_DATABASE_URL.
func TestAccountDueDay(t *testing.T) {
	var bs *billing.Service
	env, _ := newLeadsEnv(t, func(d *Deps) { bs = d.Billing })
	ctx := context.Background()
	bs.SetClock(func() time.Time { return time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC) })
	admin := env.login(auth.RoleAdmin + "@leads.test")
	var frota struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/customers", map[string]any{
		"name": "Frota", "email": "frota@dia.test", "phone": "(11) 97777-0000", "document": "", "password": userPassword,
	}, http.StatusCreated), &frota)
	customer := env.login("frota@dia.test")
	env.must(customer, http.MethodPut, "/api/me/address", map[string]any{
		"zipCode": "01305-000", "street": "Rua Augusta", "number": "500", "district": "Consolação", "city": "São Paulo", "state": "SP",
	}, http.StatusOK)

	vehicle := func(plate string, dueDay int) billing.Subscription {
		t.Helper()
		var out struct {
			Subscription billing.Subscription `json:"subscription"`
		}
		_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/customers/"+frota.ID+"/trackers", map[string]any{
			"vehicle": map[string]any{"name": "Van " + plate, "plate": plate}, "equipmentCents": 0,
			"plan": map[string]any{"planName": "Plano Frota", "priceCents": 4990, "dueDay": dueDay},
		}, http.StatusCreated), &out)
		return out.Subscription
	}
	days := func() map[int]int {
		t.Helper()
		rows, err := env.db.Query(ctx, `SELECT due_day, count(*) FROM subscriptions WHERE customer_id = $1 AND status = 'ACTIVE' GROUP BY due_day`, frota.ID)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		out := map[int]int{}
		for rows.Next() {
			var day, n int
			_ = rows.Scan(&day, &n)
			out[day] = n
		}
		return out
	}
	date := func(d billing.Date) string { return d.Format(time.DateOnly) }

	// O primeiro veículo escolhe o dia (10); o segundo pede o 25 e entra no 10.
	first := vehicle("DIA1A11", 10)
	second := vehicle("DIA2B22", 25)
	if second.DueDay != 10 || date(second.NextDueDate) != "2026-10-10" {
		t.Fatalf("segundo veículo = dia %d, primeira em %s", second.DueDay, date(second.NextDueDate))
	}
	// O cliente vê o dia da conta ao contratar outro.
	var catalog struct {
		DefaultDueDay int `json:"defaultDueDay"`
	}
	_ = json.Unmarshal(env.must(customer, http.MethodGet, "/api/catalog", nil, http.StatusOK), &catalog)
	if catalog.DefaultDueDay != 10 {
		t.Errorf("o cliente vê o vencimento %d", catalog.DefaultDueDay)
	}

	// As mensalidades de outubro já saíram (dia 10, as duas).
	if _, err := billing.NewRepository(env.db).GenerateDue(ctx, billing.NewDate(2026, 10, 31)); err != nil {
		t.Fatal(err)
	}

	// Mudar o dia de uma assinatura muda a conta: as duas vão para o 20, e as
	// mensalidades de outubro em aberto, para 20/10.
	var changed billing.Subscription
	_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/subscriptions/"+first.ID.String()+"/due-day",
		map[string]any{"dueDay": 20}, http.StatusOK), &changed)
	if changed.ID != first.ID || changed.DueDay != 20 {
		t.Errorf("a resposta = %+v", changed)
	}
	if got := days(); len(got) != 1 || got[20] != 2 {
		t.Errorf("depois de mudar uma: %v", got)
	}
	var october []string
	rows, err := env.db.Query(ctx, `SELECT to_char(due_date, 'YYYY-MM-DD') FROM invoices WHERE subscription_id = ANY($1) ORDER BY 1`,
		[]uuid.UUID{first.ID, second.ID})
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var d string
		_ = rows.Scan(&d)
		october = append(october, d)
	}
	rows.Close()
	if len(october) != 2 || october[0] != "2026-10-20" || october[1] != "2026-10-20" {
		t.Errorf("as mensalidades de outubro = %v", october)
	}

	// O plano da conta com o dia 5: a conta toda vai junto.
	var plan struct {
		DueDay int `json:"dueDay"`
	}
	_ = json.Unmarshal(env.must(admin, http.MethodPut, "/api/customers/"+frota.ID+"/plan",
		map[string]any{"planName": "Plano Frota", "priceCents": 3990, "dueDay": 5}, http.StatusOK), &plan)
	if got := days(); plan.DueDay != 5 || len(got) != 1 || got[5] != 2 {
		t.Errorf("plano no dia 5: plano %d, assinaturas %v", plan.DueDay, got)
	}
	// E mudar pela assinatura leva o plano junto.
	env.must(admin, http.MethodPost, "/api/subscriptions/"+second.ID.String()+"/due-day", map[string]any{"dueDay": 15}, http.StatusOK)
	_ = env.db.QueryRow(ctx, `SELECT due_day FROM customer_plans WHERE customer_id = $1`, frota.ID).Scan(&plan.DueDay)
	if got := days(); plan.DueDay != 15 || len(got) != 1 || got[15] != 2 {
		t.Errorf("pela assinatura no dia 15: plano %d, assinaturas %v", plan.DueDay, got)
	}

	// Uma conta de antes da regra, com dias diferentes (15, 15 e 28): o
	// veículo novo entra no mais comum.
	if _, err := env.db.Exec(ctx, `
		INSERT INTO subscriptions (customer_id, plan_name, price_cents, due_day, next_due_date)
		VALUES ($1, 'Plano Antigo', 4990, 28, '2026-10-28')`, frota.ID); err != nil {
		t.Fatal(err)
	}
	if third := vehicle("DIA3C33", 28); third.DueDay != 15 {
		t.Errorf("na conta com dias diferentes, o novo entrou no %d", third.DueDay)
	}
}
