package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/finance"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/payments/abacatepay"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/telemetry"
)

// newCompanyEnv sobe a API só com a gestão da empresa, um admin e um
// operador (ver integrationDB), com o relógio em 15/10/2026 ao meio-dia de
// Brasília.
func newCompanyEnv(t *testing.T) (*credEnv, *finance.Service, *auth.Service) {
	t.Helper()
	db := integrationDB(t)
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{
		HTTP: config.HTTP{RateLimitRPS: 1000, RateLimitBurst: 1000},
		Auth: config.Auth{
			JWTSecret:      []byte("segredo-de-teste-integracao-0123456789abcdef"),
			AccessTokenTTL: time.Hour, RefreshTokenTTL: time.Hour, BcryptCost: bcrypt.MinCost,
		},
	}
	authSvc := auth.NewService(auth.NewRepository(db), cfg.Auth, nil, log)
	for _, role := range []string{auth.RoleAdmin, auth.RoleOperator} {
		if _, err := authSvc.CreateUser(ctx, role+"@receitas.test", role, role, userPassword); err != nil {
			t.Fatal(err)
		}
	}
	fs := finance.NewService(db, &fakeFinanceMailer{}, log)
	fs.SetClock(func() time.Time { return time.Date(2026, 10, 15, 15, 0, 0, 0, time.UTC) })
	server := NewServer(Deps{
		Config: cfg, Log: log, Metrics: telemetry.NewMetrics(), DB: db, Auth: authSvc,
		Audit: audit.NewService(audit.NewRepository(db), log), Finance: fs,
	})
	srv := httptest.NewServer(server.Handler())
	t.Cleanup(srv.Close)
	return &credEnv{t: t, db: db, srv: srv}, fs, authSvc
}

// Os pagamentos dos clientes na aba de receitas: as faturas pagas no mês,
// pelo dia em que entraram no caixa (em Brasília), com a forma; o total é o
// das faturas dos clientes no fluxo de caixa. Só o admin. Precisa de
// FARBO_TEST_DATABASE_URL.
func TestCustomerPaymentsEndToEnd(t *testing.T) {
	env, _, authSvc := newCompanyEnv(t)
	db, ctx := env.db, context.Background()
	carla, err := authSvc.CreateUser(ctx, "carla@receitas.test", "Carla Dias", auth.RoleCustomer, userPassword)
	if err != nil {
		t.Fatal(err)
	}
	bruno, err := authSvc.CreateUser(ctx, "bruno@receitas.test", "Bruno Frota", auth.RoleCustomer, userPassword)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := db.Exec(ctx, `
		INSERT INTO invoices (customer_id, description, amount_cents, due_date, status, paid_at, paid_via, updated_at) VALUES
			($1, 'Plano Mensal — outubro/2026', 3990, '2026-10-13', 'PAID', '2026-10-12 15:00:00+00', 'PIX', NOW()),
			-- 30/09 às 23h em Brasília (01/10 em UTC): setembro.
			($1, 'Rastreador J16', 12000, '2026-10-01', 'PAID', '2026-10-01 02:00:00+00', 'BOLETO', NOW()),
			-- 13/10 às 23h30 em Brasília (14/10 em UTC).
			($2, 'Plano Frota — outubro/2026', 5990, '2026-10-13', 'PAID', '2026-10-14 02:30:00+00', 'CARD', NOW()),
			-- Baixa antiga, sem forma nem data do pagamento: manual, no dia da baixa.
			($2, 'Antiga', 6990, '2026-10-05', 'PAID', NULL, '', '2026-10-05 12:00:00+00'),
			($1, 'A vencer', 3990, '2026-11-13', 'OPEN', NULL, '', NOW()),
			($1, 'Cancelada', 3990, '2026-10-20', 'CANCELED', NULL, '', NOW())`, carla.ID, bruno.ID); err != nil {
		t.Fatal(err)
	}

	env.must(env.login(auth.RoleOperator+"@receitas.test"), http.MethodGet, "/api/finance/customer-payments", nil, http.StatusForbidden)
	admin := env.login(auth.RoleAdmin + "@receitas.test")
	payments := func(query string) finance.CustomerPayments {
		t.Helper()
		var out finance.CustomerPayments
		if err := json.Unmarshal(env.must(admin, http.MethodGet, "/api/finance/customer-payments"+query, nil, http.StatusOK), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}

	// Outubro (o mês corrente): os mais recentes primeiro, por forma do maior
	// para o menor.
	oct := payments("")
	if oct.Month != "2026-10" || oct.Count != 3 || oct.Cents != 3990+5990+6990 || oct.Truncated || len(oct.Payments) != 3 {
		t.Fatalf("outubro = %+v", oct)
	}
	if p := oct.Payments[0]; p.Description != "Plano Frota — outubro/2026" || p.CustomerName != "Bruno Frota" || p.Method != "CARD" ||
		p.CustomerID != bruno.ID || p.DueDate.String() != "2026-10-13" || p.PaidAt.In(time.FixedZone("BRT", -3*3600)).Day() != 13 {
		t.Errorf("o mais recente = %+v", p)
	}
	if oct.Payments[1].Method != "PIX" || oct.Payments[2].Method != "MANUAL" || oct.Payments[2].Description != "Antiga" {
		t.Errorf("ordem = %+v", oct.Payments)
	}
	if len(oct.ByMethod) != 3 || oct.ByMethod[0] != (finance.MethodSum{Method: "MANUAL", Count: 1, Cents: 6990}) ||
		oct.ByMethod[2] != (finance.MethodSum{Method: "PIX", Count: 1, Cents: 3990}) {
		t.Errorf("por forma = %+v", oct.ByMethod)
	}

	// Setembro: o boleto pago às 23h do dia 30.
	if sep := payments("?month=2026-09"); sep.Count != 1 || sep.Cents != 12000 || sep.Payments[0].Method != "BOLETO" {
		t.Errorf("setembro = %+v", sep)
	}

	// O mesmo total das faturas no fluxo de caixa.
	var flow finance.CashFlow
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/finance/cashflow?months=2", nil, http.StatusOK), &flow)
	if len(flow.Months) != 2 || flow.Months[0].InvoicesCents != 12000 || flow.Months[1].InvoicesCents != oct.Cents {
		t.Errorf("caixa = %+v", flow.Months)
	}

	// Busca pelo cliente ou pela descrição, e pela forma.
	if got := payments("?q=carla"); got.Count != 1 || got.Payments[0].CustomerID != carla.ID {
		t.Errorf("busca carla = %+v", got)
	}
	if got := payments("?q=FROTA"); got.Count != 2 {
		t.Errorf("busca frota = %+v", got)
	}
	if got := payments("?q=100%25"); got.Count != 0 {
		t.Errorf("busca com %% = %+v", got)
	}
	if got := payments("?method=pix"); got.Count != 1 || got.Cents != 3990 || len(got.ByMethod) != 1 {
		t.Errorf("só Pix = %+v", got)
	}
	if got := payments("?month=2026-08"); got.Count != 0 || len(got.Payments) != 0 || got.ByMethod == nil {
		t.Errorf("mês sem pagamentos = %+v", got)
	}
	env.must(admin, http.MethodGet, "/api/finance/customer-payments?month=2026-13", nil, http.StatusBadRequest)
	env.must(admin, http.MethodGet, "/api/finance/customer-payments?method=CHEQUE", nil, http.StatusBadRequest)
}

// fakeBalance é a AbacatePay de mentira do saldo.
type fakeBalance struct {
	balance *abacatepay.Balance
	err     error
}

func (f *fakeBalance) Balance(context.Context) (*abacatepay.Balance, error) { return f.balance, f.err }

// O saldo na AbacatePay no topo da tela da empresa: o disponível, o a liberar
// e o bloqueado, consultados na hora; a falha vira um aviso; sem a chave,
// desligado. Só o admin. Precisa de FARBO_TEST_DATABASE_URL.
func TestGatewayBalance(t *testing.T) {
	env, fs, _ := newCompanyEnv(t)
	env.must(env.login(auth.RoleOperator+"@receitas.test"), http.MethodGet, "/api/finance/balance", nil, http.StatusForbidden)
	admin := env.login(auth.RoleAdmin + "@receitas.test")
	balance := func() finance.GatewayBalance {
		t.Helper()
		var out finance.GatewayBalance
		if err := json.Unmarshal(env.must(admin, http.MethodGet, "/api/finance/balance", nil, http.StatusOK), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}

	if b := balance(); b.Enabled || b.AvailableCents != nil {
		t.Errorf("sem a chave = %+v", b)
	}

	fake := &fakeBalance{balance: &abacatepay.Balance{Available: 123456, Pending: 15990, Blocked: 0}}
	fs.SetBalanceReader(fake, false)
	b := balance()
	if !b.Enabled || b.DevMode || b.AvailableCents == nil || *b.AvailableCents != 123456 || *b.PendingCents != 15990 ||
		*b.BlockedCents != 0 || b.Error != "" || b.CheckedAt.IsZero() {
		t.Errorf("saldo = %+v", b)
	}
	// Na hora: o saldo novo aparece na próxima consulta.
	fake.balance = &abacatepay.Balance{Available: 100000}
	if b := balance(); *b.AvailableCents != 100000 {
		t.Errorf("saldo novo = %d", *b.AvailableCents)
	}

	fake.err = &abacatepay.APIError{HTTPStatus: http.StatusUnauthorized, Message: "chave sem permissão"}
	if b := balance(); !b.Enabled || b.AvailableCents != nil || b.Error != "chave sem permissão" {
		t.Errorf("com falha = %+v", b)
	}
}
