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

// As assinaturas grátis (mensalidade zero, como as de cortesia): a lista de
// clientes da central as traz à parte, para não contá-las, e o MRR não as
// soma; o cliente continua vendo todas as ativas na conta dele. Precisa de
// FARBO_TEST_DATABASE_URL.
func TestFreeSubscriptionsCount(t *testing.T) {
	env, _ := newLeadsEnv(t)
	ctx := context.Background()
	authSvc := auth.NewService(auth.NewRepository(env.db), config.Auth{BcryptCost: bcrypt.MinCost}, nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	ana, err := authSvc.CreateUser(ctx, "ana@gratis.test", "Ana", auth.RoleCustomer, userPassword)
	if err != nil {
		t.Fatal(err)
	}
	// Duas pagas, uma grátis e uma grátis encerrada; as ativas com a
	// mensalidade deste mês já gerada (uma paga) e a próxima no mês que vem.
	for _, sub := range []struct {
		cents   int
		status  string
		invoice string
	}{{4990, "ACTIVE", "PAID"}, {3490, "ACTIVE", "OPEN"}, {0, "ACTIVE", "PAID"}, {0, "CANCELED", ""}} {
		var id string
		if err := env.db.QueryRow(ctx, `
			INSERT INTO subscriptions (customer_id, plan_name, price_cents, due_day, next_due_date, status)
			VALUES ($1, 'Plano', $2, 10, (date_trunc('month', CURRENT_DATE) + interval '1 month 9 days')::date, $3)
			RETURNING id`, ana.ID, sub.cents, sub.status).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if sub.invoice != "" {
			if _, err := env.db.Exec(ctx, `
				INSERT INTO invoices (customer_id, subscription_id, description, amount_cents, due_date, status)
				VALUES ($1, $2, 'Mensalidade', $3, CURRENT_DATE, $4)`, ana.ID, id, sub.cents, sub.invoice); err != nil {
				t.Fatal(err)
			}
		}
	}

	var list []*billing.CustomerSummary
	_ = json.Unmarshal(env.must(env.login(auth.RoleAdmin+"@leads.test"), http.MethodGet, "/api/customers", nil, http.StatusOK), &list)
	var row *billing.CustomerSummary
	for _, c := range list {
		if c.ID == ana.ID {
			row = c
		}
	}
	if row == nil || row.ActiveSubscriptions != 3 || row.FreeSubscriptions != 1 {
		t.Fatalf("na lista da central = %+v", row)
	}

	// O MRR: as mensalidades deste mês (a grátis fica de fora), as duas a 1ª da
	// assinatura; só o admin.
	env.must(env.login(auth.RoleOperator+"@leads.test"), http.MethodGet, "/api/customers/mrr", nil, http.StatusForbidden)
	var mrr billing.MRR
	_ = json.Unmarshal(env.must(env.login(auth.RoleAdmin+"@leads.test"), http.MethodGet, "/api/customers/mrr", nil, http.StatusOK), &mrr)
	if mrr.CurrentCents != 8480 || mrr.Subscriptions != 2 || len(mrr.Months) != 6 || mrr.Months[0].NewCents != 8480 || mrr.Months[0].NewCount != 2 {
		t.Errorf("MRR = %+v", mrr)
	}

	var account billing.Account
	_ = json.Unmarshal(env.must(env.login("ana@gratis.test"), http.MethodGet, "/api/me/account", nil, http.StatusOK), &account)
	if account.ActiveSubscriptions != 3 {
		t.Errorf("na conta do cliente = %+v", account)
	}
}
