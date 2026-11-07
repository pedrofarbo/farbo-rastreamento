package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/affiliates"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
)

// A comissão em percentual: o afiliado ganha uma fração da mensalidade paga
// de cada veículo, sem a parcela do rastreador que vem na mesma fatura. O
// tipo fica quando a edição não diz qual, e a página do afiliado mostra o
// percentual. Precisa de FARBO_TEST_DATABASE_URL.
func TestAffiliatePercentCommission(t *testing.T) {
	env, _ := newLeadsEnv(t)
	ctx := context.Background()
	db := env.db
	admin := env.login(auth.RoleAdmin + "@leads.test")
	call := func(method, path string, body any, want int, out any) {
		t.Helper()
		raw := env.must(admin, method, path, body, want)
		if out != nil {
			if err := json.Unmarshal(raw, out); err != nil {
				t.Fatalf("%s %s: %v (%s)", method, path, err, raw)
			}
		}
	}

	// O percentual vai de 0,01% a 100%.
	for _, bps := range []int{0, 10_001} {
		env.must(admin, http.MethodPost, "/api/affiliates", map[string]any{
			"name": "Errado", "commissionType": "percent", "commissionBps": bps, "active": true,
		}, http.StatusBadRequest)
	}
	env.must(admin, http.MethodPost, "/api/affiliates", map[string]any{"name": "Errado", "commissionType": "meio", "active": true},
		http.StatusBadRequest)
	var lia affiliates.Affiliate
	call(http.MethodPost, "/api/affiliates", map[string]any{
		"name": "Lia Percentual", "code": "lia", "commissionType": "percent", "commissionBps": 1250, "active": true,
	}, http.StatusCreated, &lia)
	if lia.CommissionType != affiliates.CommissionPercent || lia.CommissionBps != 1250 {
		t.Fatalf("lia = %+v", lia)
	}

	var customer struct {
		ID string `json:"id"`
	}
	call(http.MethodPost, "/api/customers", map[string]any{
		"name": "Rui", "email": "rui@percentual.test", "password": "senha-do-cliente-123",
	}, http.StatusCreated, &customer)
	call(http.MethodPut, "/api/customers/"+customer.ID+"/affiliate", map[string]any{"affiliateId": lia.ID}, http.StatusOK, nil)

	brt, _ := time.LoadLocation("America/Sao_Paulo")
	now := time.Now().In(brt)
	due := time.Date(now.Year(), now.Month(), 10, 0, 0, 0, 0, time.UTC)
	subscription := func() string {
		t.Helper()
		var id string
		if err := db.QueryRow(ctx, `
			INSERT INTO subscriptions (customer_id, plan_name, price_cents, due_day, next_due_date)
			VALUES ($1, 'Plano Mensal', 6990, 10, $2) RETURNING id`, customer.ID, due.AddDate(0, 1, 0)).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	paidInvoice := func(sub string, cents int) string {
		t.Helper()
		var id string
		if err := db.QueryRow(ctx, `
			INSERT INTO invoices (customer_id, subscription_id, description, amount_cents, due_date, status, paid_at, paid_via)
			VALUES ($1, $2, 'Mensalidade', $3, $4, 'PAID', NOW(), 'MANUAL') RETURNING id`, customer.ID, sub, cents, due).
			Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	// O carro: a mensalidade (R$ 69,90) com a parcela de um rastreador
	// comprado antes (R$ 20) na mesma fatura; a moto, a mensalidade com o
	// desconto de indicação (R$ 39,90) mais a parte do rastreador incluso
	// (R$ 12, nas primeiras). Nenhuma das duas partes rende comissão.
	car, bike := subscription(), subscription()
	carInvoice := paidInvoice(car, 6990+2000)
	if _, err := db.Exec(ctx, `
		INSERT INTO equipment_installments (subscription_id, number, amount_cents, invoice_id) VALUES ($1, 1, 2000, $2)`,
		car, carInvoice); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `UPDATE subscriptions SET tracker_cents = 1200, tracker_months = 10, tracker_until = $2,
		tracker_added = TRUE WHERE id = $1`, bike, due.AddDate(0, 10, 0)); err != nil {
		t.Fatal(err)
	}
	paidInvoice(bike, 3990+1200)

	var list []*affiliates.Affiliate
	call(http.MethodGet, "/api/affiliates", nil, http.StatusOK, &list)
	var got *affiliates.Affiliate
	for _, a := range list {
		if a.ID == lia.ID {
			got = a
		}
	}
	// 12,5% de 69,90 = 8,74 (a parcela fica fora); 12,5% de 39,90 = 4,99 (a
	// parte do rastreador incluso fica fora).
	if got == nil || got.PendingCents != 874+499 {
		t.Fatalf("comissões da lia = %+v", got)
	}

	// Editar sem dizer o tipo mantém o percentual.
	call(http.MethodPatch, "/api/affiliates/"+lia.ID.String(), map[string]any{
		"name": "Lia Percentual", "code": "lia", "active": true,
	}, http.StatusOK, &lia)
	if lia.CommissionType != affiliates.CommissionPercent || lia.CommissionBps != 1250 {
		t.Fatalf("lia depois da edição = %+v", lia)
	}
	var report affiliates.Report
	_ = json.Unmarshal(env.must("", http.MethodGet, "/api/public/partner/"+lia.ReportToken, nil, http.StatusOK), &report)
	if report.CommissionType != affiliates.CommissionPercent || report.CommissionBps != 1250 || report.ToReceiveCents != 874+499 {
		t.Fatalf("página da lia = %+v", report)
	}

	// Com os impostos da Farbo (6%), o percentual é da mensalidade líquida:
	// 12,5% de 69,90 × 0,94 = 8,21. As comissões já geradas ficam.
	env.must(admin, http.MethodPut, "/api/affiliates/settings", map[string]any{"defaultCommissionCents": 600, "commissionTaxBps": 3001},
		http.StatusBadRequest)
	var settings affiliates.Settings
	call(http.MethodPut, "/api/affiliates/settings", map[string]any{"defaultCommissionCents": 600, "commissionTaxBps": 600},
		http.StatusOK, &settings)
	if settings.CommissionTaxBps != 600 || settings.DefaultCommissionCents != 600 {
		t.Fatalf("configuração = %+v", settings)
	}
	paidInvoice(subscription(), 6990)
	call(http.MethodGet, "/api/affiliates", nil, http.StatusOK, &list)
	for _, a := range list {
		if a.ID == lia.ID {
			got = a
		}
	}
	if got.PendingCents != 874+499+821 {
		t.Fatalf("comissões com os impostos = %d", got.PendingCents)
	}
	_ = json.Unmarshal(env.must("", http.MethodGet, "/api/public/partner/"+lia.ReportToken, nil, http.StatusOK), &report)
	if report.CommissionTaxBps != 600 {
		t.Errorf("página da lia sem os impostos = %+v", report)
	}
}
