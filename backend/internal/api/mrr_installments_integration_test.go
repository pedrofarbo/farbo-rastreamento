package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
)

// As parcelas do rastreador no resumo do MRR: por mês de vencimento, as das
// faturas (a cancelada não conta) e, no mês atual, a próxima de cada
// assinatura que ainda vai entrar numa mensalidade do mês; quanto já foi
// pago; e o total com as mensalidades. Precisa de FARBO_TEST_DATABASE_URL.
func TestMRRInstallments(t *testing.T) {
	env, _ := newLeadsEnv(t)
	ctx := context.Background()
	db := env.db
	authSvc := auth.NewService(auth.NewRepository(db), config.Auth{BcryptCost: bcrypt.MinCost}, nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	bia, err := authSvc.CreateUser(ctx, "bia@parcelas.test", "Bia", auth.RoleCustomer, userPassword)
	if err != nil {
		t.Fatal(err)
	}
	exec := func(query string, args ...any) string {
		t.Helper()
		var id string
		if err := db.QueryRow(ctx, query, args...).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	// A assinatura de R$ 34,90 com o rastreador em 10x: a próxima mensalidade
	// ainda é deste mês (dia 28).
	sub := exec(`
		INSERT INTO subscriptions (customer_id, plan_name, price_cents, due_day, next_due_date, installments, commitment_until)
		VALUES ($1, 'Plano', 3490, 28, date_trunc('month', CURRENT_DATE)::date + 27, 10, CURRENT_DATE + 300) RETURNING id`, bia.ID)
	invoice := func(due, status string, cents int) string {
		t.Helper()
		return exec(`
			INSERT INTO invoices (customer_id, subscription_id, description, amount_cents, due_date, status)
			VALUES ($1, $2, 'Fatura', $3, (`+due+`)::date, $4) RETURNING id`, bia.ID, sub, cents, status)
	}
	installment := func(number, cents int, invoiceID any, waived bool) {
		t.Helper()
		exec(`
			INSERT INTO equipment_installments (subscription_id, number, amount_cents, invoice_id, waived_at)
			VALUES ($1, $2, $3, $4, CASE WHEN $5 THEN NOW() END) RETURNING id`, sub, number, cents, invoiceID, waived)
	}
	// Mês passado: paga. Este mês: uma paga, uma numa fatura cancelada (não
	// conta) e a próxima ainda sem fatura (entra na mensalidade do dia 28).
	installment(1, 1500, invoice("date_trunc('month', CURRENT_DATE) - interval '1 month' + interval '9 days'", "PAID", 1500), false)
	installment(2, 1200, invoice("date_trunc('month', CURRENT_DATE) + interval '9 days'", "PAID", 4690), false)
	installment(3, 1100, invoice("date_trunc('month', CURRENT_DATE) + interval '10 days'", "CANCELED", 4590), false)
	installment(4, 1000, nil, false)
	installment(5, 900, nil, false)
	installment(6, 800, nil, true)

	var mrr billing.MRR
	_ = json.Unmarshal(env.must(env.login(auth.RoleAdmin+"@leads.test"), http.MethodGet, "/api/customers/mrr", nil, http.StatusOK), &mrr)
	if len(mrr.Months) != 6 {
		t.Fatalf("MRR = %+v", mrr)
	}
	cur, prev := mrr.Months[0], mrr.Months[1]
	if cur.MRRCents != 3490 || cur.InstallmentsCents != 2200 || cur.InstallmentsPaidCents != 1200 || cur.TotalCents != 5690 {
		t.Errorf("este mês = %+v", cur)
	}
	if prev.InstallmentsCents != 1500 || prev.InstallmentsPaidCents != 1500 || prev.TotalCents != prev.MRRCents+1500 {
		t.Errorf("mês passado = %+v", prev)
	}
	if mrr.CurrentCents != 3490 || mrr.InstallmentsCents != 2200 || mrr.TotalCents != 5690 {
		t.Errorf("hoje = %+v", mrr)
	}
	// A previsão do mês que vem: a mensalidade do dia 28 dele, com a parcela
	// seguinte à que vai na deste mês (a 5ª, de R$ 9,00).
	if f := mrr.Forecast; f == nil || f.Monthlies != 1 || f.MonthlyCents != 3490 || f.InstallmentsCents != 900 || f.TotalCents != 4390 {
		t.Errorf("previsão = %+v", mrr.Forecast)
	}
}
