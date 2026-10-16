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
// clientes da central as traz à parte, para não contá-las; o cliente continua
// vendo todas as ativas na conta dele. Precisa de FARBO_TEST_DATABASE_URL.
func TestFreeSubscriptionsCount(t *testing.T) {
	env, _ := newLeadsEnv(t)
	ctx := context.Background()
	authSvc := auth.NewService(auth.NewRepository(env.db), config.Auth{BcryptCost: bcrypt.MinCost}, nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	ana, err := authSvc.CreateUser(ctx, "ana@gratis.test", "Ana", auth.RoleCustomer, userPassword)
	if err != nil {
		t.Fatal(err)
	}
	// Duas pagas, uma grátis e uma grátis encerrada.
	for _, sub := range []struct {
		cents  int
		status string
	}{{4990, "ACTIVE"}, {3490, "ACTIVE"}, {0, "ACTIVE"}, {0, "CANCELED"}} {
		if _, err := env.db.Exec(ctx, `
			INSERT INTO subscriptions (customer_id, plan_name, price_cents, due_day, next_due_date, status)
			VALUES ($1, 'Plano', $2, 10, CURRENT_DATE + 30, $3)`, ana.ID, sub.cents, sub.status); err != nil {
			t.Fatal(err)
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

	var account billing.Account
	_ = json.Unmarshal(env.must(env.login("ana@gratis.test"), http.MethodGet, "/api/me/account", nil, http.StatusOK), &account)
	if account.ActiveSubscriptions != 3 {
		t.Errorf("na conta do cliente = %+v", account)
	}
}
