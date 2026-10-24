package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/affiliates"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
)

// Indique e ganhe de ponta a ponta: o cliente pega o link dele; quem se
// cadastra por ele e vira cliente fica indicado; quando o indicado paga a
// primeira mensalidade, quem indicou ganha o crédito, que abate da próxima
// fatura de mensalidade em aberto (sem Pix gerado) — e, se cobrir tudo, a
// fatura fica paga com o crédito. Um crédito por indicado. Precisa de
// FARBO_TEST_DATABASE_URL.
func TestReferralCredits(t *testing.T) {
	var svc *affiliates.Service
	env, _ := newLeadsEnv(t, func(d *Deps) { svc = d.Affiliates })
	ctx := context.Background()
	db := env.db
	admin := env.login(auth.RoleAdmin + "@leads.test")
	customer := func(name, email string, lead any) string {
		t.Helper()
		var d struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/customers", map[string]any{
			"name": name, "email": email, "password": "senha-do-cliente-123", "leadId": lead,
		}, http.StatusCreated), &d)
		return d.ID
	}
	jair := customer("Jair Medeiros", "jair@indica.test", nil)
	login := func(email string) string {
		t.Helper()
		out := env.must("", http.MethodPost, "/api/auth/login", map[string]string{"email": email, "password": "senha-do-cliente-123"}, http.StatusOK)
		var tokens struct {
			AccessToken string `json:"accessToken"`
		}
		_ = json.Unmarshal(out, &tokens)
		return tokens.AccessToken
	}
	jairToken := login("jair@indica.test")

	type program struct {
		Program struct {
			Code        string `json:"code"`
			RewardCents int    `json:"rewardCents"`
			Signups     int    `json:"signups"`
			Customers   int    `json:"customers"`
			EarnedCents int64  `json:"earnedCents"`
			OpenCents   int64  `json:"openCents"`
			Credits     []struct {
				ReferredName string `json:"referredName"`
			} `json:"credits"`
		} `json:"program"`
		URL string `json:"url"`
	}
	var p program
	_ = json.Unmarshal(env.must(jairToken, http.MethodGet, "/api/me/referral", nil, http.StatusOK), &p)
	if !strings.HasPrefix(p.Program.Code, "jair-") || len(p.Program.Code) != 9 || p.Program.RewardCents != 3000 ||
		!strings.HasSuffix(p.URL, "/indicacao/"+p.Program.Code) {
		t.Fatalf("programa = %+v", p)
	}
	// O mesmo link na segunda vez; fora da lista de afiliados da central.
	var again program
	_ = json.Unmarshal(env.must(jairToken, http.MethodGet, "/api/me/referral", nil, http.StatusOK), &again)
	if again.Program.Code != p.Program.Code {
		t.Fatalf("o link mudou: %s → %s", p.Program.Code, again.Program.Code)
	}
	var list []*affiliates.Affiliate
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/affiliates", nil, http.StatusOK), &list)
	if len(list) != 0 {
		t.Fatalf("o link do cliente apareceu nos afiliados: %+v", list)
	}
	var public affiliates.Public
	_ = json.Unmarshal(env.must("", http.MethodGet, "/api/public/affiliates/"+p.Program.Code, nil, http.StatusOK), &public)
	if public.Name != "Jair" {
		t.Fatalf("público = %+v", public)
	}

	// A Ana chega pelo link e vira cliente.
	env.must("", http.MethodPost, "/api/public/leads", map[string]any{
		"name": "Ana Lima", "email": "ana@indicada.test", "phone": "(11) 94444-3333", "plan": "mensal",
		"vehicleType": "moto", "vehicleCount": 1, "consent": true, "ref": p.Program.Code,
	}, http.StatusCreated)
	var leadID string
	if err := db.QueryRow(ctx, `SELECT id FROM leads WHERE email = 'ana@indicada.test'`).Scan(&leadID); err != nil {
		t.Fatal(err)
	}
	ana := customer("Ana Lima", "ana@indicada.test", leadID)

	subscription := func(customerID string) string {
		t.Helper()
		var id string
		if err := db.QueryRow(ctx, `
			INSERT INTO subscriptions (customer_id, plan_name, price_cents, due_day, next_due_date)
			VALUES ($1, 'Plano Mensal', 6990, 10, CURRENT_DATE + 60) RETURNING id`, customerID).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	invoice := func(customerID, sub string, due time.Time, paid bool) string {
		t.Helper()
		var id string
		if err := db.QueryRow(ctx, `
			INSERT INTO invoices (customer_id, subscription_id, description, amount_cents, due_date, status, paid_at, paid_via)
			VALUES ($1, $2, 'Mensalidade', 6990, $3,
				CASE WHEN $4 THEN 'PAID' ELSE 'OPEN' END, CASE WHEN $4 THEN NOW() END, CASE WHEN $4 THEN 'MANUAL' ELSE '' END)
			RETURNING id`, customerID, sub, due, paid).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	jairSub, anaSub := subscription(jair), subscription(ana)
	soon := time.Now().AddDate(0, 0, 10)
	jairNext := invoice(jair, jairSub, soon, false)
	jairLater := invoice(jair, jairSub, soon.AddDate(0, 1, 0), false)

	// Sem a mensalidade da Ana paga, nada.
	svc.Work(ctx)
	var credits int
	_ = db.QueryRow(ctx, `SELECT count(*) FROM referral_credits`).Scan(&credits)
	if credits != 0 {
		t.Fatalf("crédito antes do pagamento: %d", credits)
	}
	// Paga: o crédito de R$ 30 abate da próxima fatura do Jair.
	invoice(ana, anaSub, time.Now().AddDate(0, 0, -1), true)
	svc.Work(ctx)
	svc.Work(ctx) // de novo: nada muda
	amount := func(id string) (int, string, string) {
		t.Helper()
		var cents int
		var description, status string
		if err := db.QueryRow(ctx, `SELECT amount_cents, description, status FROM invoices WHERE id = $1`, id).
			Scan(&cents, &description, &status); err != nil {
			t.Fatal(err)
		}
		return cents, description, status
	}
	if cents, description, status := amount(jairNext); cents != 3990 || description != "Mensalidade (desconto de indicação)" || status != "OPEN" {
		t.Fatalf("fatura do jair = %d %q %s", cents, description, status)
	}
	if cents, _, _ := amount(jairLater); cents != 6990 {
		t.Fatalf("a fatura seguinte não muda: %d", cents)
	}
	_ = json.Unmarshal(env.must(jairToken, http.MethodGet, "/api/me/referral", nil, http.StatusOK), &p)
	if p.Program.Signups != 1 || p.Program.Customers != 1 || p.Program.EarnedCents != 3000 || p.Program.OpenCents != 0 ||
		len(p.Program.Credits) != 1 || p.Program.Credits[0].ReferredName != "Ana" {
		t.Fatalf("programa depois = %+v", p)
	}

	// Um crédito maior que a fatura: zera (paga com o crédito) e o resto fica
	// para a seguinte; com Pix gerado, a fatura não muda.
	env.must(admin, http.MethodPut, "/api/affiliates/settings", map[string]any{"defaultCommissionCents": 600, "customerRewardCents": 8000},
		http.StatusOK)
	if _, err := db.Exec(ctx, `
		INSERT INTO payment_charges (invoice_id, provider, provider_charge_id, amount_cents, status, expires_at)
		VALUES ($1, 'test', 'pix-1', 6990, 'PENDING', NOW() + INTERVAL '1 hour')`, jairLater); err != nil {
		t.Fatal(err)
	}
	bia := customer("Bia Souza", "bia@indicada.test", nil)
	var jairAffiliate string
	_ = db.QueryRow(ctx, `SELECT id FROM affiliates WHERE code = $1`, p.Program.Code).Scan(&jairAffiliate)
	if _, err := db.Exec(ctx, `INSERT INTO affiliate_referrals (customer_id, affiliate_id, source) VALUES ($1, $2, 'admin')`,
		bia, jairAffiliate); err != nil {
		t.Fatal(err)
	}
	invoice(bia, subscription(bia), time.Now().AddDate(0, 0, -1), true)
	svc.Work(ctx)
	if cents, _, status := amount(jairNext); cents != 0 || status != "PAID" {
		t.Fatalf("fatura zerada = %d %s", cents, status)
	}
	if cents, _, _ := amount(jairLater); cents != 6990 {
		t.Fatalf("a fatura com Pix não muda: %d", cents)
	}
	_ = json.Unmarshal(env.must(jairToken, http.MethodGet, "/api/me/referral", nil, http.StatusOK), &p)
	if p.Program.EarnedCents != 11000 || p.Program.OpenCents != 4010 {
		t.Fatalf("créditos = %+v", p.Program)
	}
}
