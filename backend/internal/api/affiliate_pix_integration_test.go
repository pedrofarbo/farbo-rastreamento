package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/affiliates"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/finance"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/payments/abacatepay"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/stepup"
)

// Pagar o fechamento do afiliado por Pix pela AbacatePay (de mentira): o Pix
// sai para a chave atual do afiliado (a trocada depois do primeiro
// fechamento também), a conta é baixada na Empresa e o fechamento aparece
// pago, com o comprovante; um Pix sem resposta segura o desfazer, e um
// recusado não. Precisa de FARBO_TEST_DATABASE_URL.
func TestAffiliatePayoutByPix(t *testing.T) {
	fake := &fakePixSender{}
	var stepUpSvc *stepup.Service
	env, _ := newLeadsEnv(t, func(d *Deps) {
		fs := finance.NewService(d.DB, &fakeFinanceMailer{}, d.Log)
		fs.SetPixSender(fake, true)
		d.Finance = fs
		stepUpSvc = stepup.NewService(d.DB, config.StepUp{RPID: "localhost", Origins: []string{"http://localhost"}}, d.Auth)
		d.StepUp = stepUpSvc
	})
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
	var adminID uuid.UUID
	if err := db.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, auth.RoleAdmin+"@leads.test").Scan(&adminID); err != nil {
		t.Fatal(err)
	}
	send := func(entryID uuid.UUID) (int, string) {
		t.Helper()
		g, err := stepUpSvc.VerifyPassword(ctx, adminID, stepup.PurposeSupplierPix, userPassword)
		if err != nil {
			t.Fatal(err)
		}
		req, _ := http.NewRequest(http.MethodPost, env.srv.URL+"/api/finance/entries/"+entryID.String()+"/pix", nil)
		req.Header.Set("Authorization", "Bearer "+admin)
		req.Header.Set("X-Step-Up-Token", g.Token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(body)
	}
	payouts := func() []*affiliates.Payout {
		t.Helper()
		var list []*affiliates.Payout
		call(http.MethodGet, "/api/affiliates/payouts", nil, http.StatusOK, &list)
		return list
	}

	// O João, com um cliente indicado e a mensalidade do mês paga.
	var joao affiliates.Affiliate
	call(http.MethodPost, "/api/affiliates", map[string]any{"name": "João Motoca", "handle": "joao.moto", "pixKey": "joao@pix.test", "active": true},
		http.StatusCreated, &joao)
	var ana struct {
		ID string `json:"id"`
	}
	call(http.MethodPost, "/api/customers", map[string]any{"name": "Ana", "email": "ana@indicada.test", "password": "senha-do-cliente-123"},
		http.StatusCreated, &ana)
	if _, err := db.Exec(ctx, `INSERT INTO affiliate_referrals (customer_id, affiliate_id, source) VALUES ($1, $2, 'admin')`,
		ana.ID, joao.ID); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	cur := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	var sub string
	if err := db.QueryRow(ctx, `
		INSERT INTO subscriptions (customer_id, plan_name, price_cents, due_day, next_due_date)
		VALUES ($1, 'Plano Mensal', 6990, 10, $2) RETURNING id`, ana.ID, cur.AddDate(0, 1, 9)).Scan(&sub); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `
		INSERT INTO invoices (customer_id, subscription_id, description, amount_cents, due_date, status, paid_at, paid_via)
		VALUES ($1, $2, 'Mensalidade', 6990, $3, 'PAID', NOW(), 'MANUAL')`, ana.ID, sub, cur.AddDate(0, 0, 9)); err != nil {
		t.Fatal(err)
	}
	month := cur.Format("2006-01")
	due := now.AddDate(0, 0, 5).Format(time.DateOnly)
	closeMonth := func() *affiliates.Payout {
		t.Helper()
		var created []*affiliates.Payout
		call(http.MethodPost, "/api/affiliates/closing", map[string]any{"month": month, "dueDate": due}, http.StatusCreated, &created)
		if len(created) != 1 || created[0].AmountCents != 600 || created[0].EntryID == nil || created[0].PixStatus != "" {
			t.Fatalf("fechamento = %+v", created)
		}
		return created[0]
	}
	first := closeMonth()

	// A chave trocada depois do fechamento vai para o fornecedor: é para ela
	// que o Pix sai.
	call(http.MethodPatch, "/api/affiliates/"+joao.ID.String(), map[string]any{
		"name": "João Motoca", "handle": "joao.moto", "code": "joao-moto", "pixKey": "joao.novo@pix.test", "active": true,
	}, http.StatusOK, nil)
	var plan struct {
		Plan *finance.PixPlan `json:"plan"`
	}
	call(http.MethodGet, "/api/finance/entries/"+first.EntryID.String()+"/pix", nil, http.StatusOK, &plan)
	if plan.Plan == nil || plan.Plan.Key != "joao.novo@pix.test" || plan.Plan.KeyType != abacatepay.KeyEmail ||
		plan.Plan.SupplierName != "João Motoca" || plan.Plan.AmountCents != 600 {
		t.Fatalf("plano do Pix = %+v", plan.Plan)
	}

	// Sem resposta da AbacatePay: o Pix fica a conferir e o fechamento não se
	// desfaz (o dinheiro pode ter saído).
	fake.respond = func(abacatepay.TransferRequest) (*abacatepay.Transfer, error) {
		return nil, errors.New("connection reset by peer")
	}
	if code, body := send(*first.EntryID); code != http.StatusBadRequest || !strings.Contains(body, "não respondeu") {
		t.Fatalf("sem resposta = %d %s", code, body)
	}
	if p := payouts()[0]; p.Status != "OPEN" || p.PixStatus != finance.PixUnknown {
		t.Fatalf("fechamento com o Pix a conferir = %+v", p)
	}
	if raw := env.must(admin, http.MethodDelete, "/api/affiliates/payouts/"+first.ID.String(), nil, http.StatusBadRequest); !strings.Contains(string(raw), "confira o Pix") {
		t.Errorf("desfazer com o Pix a conferir = %s", raw)
	}

	// Conferido no painel: não saiu. Aí dá para desfazer (a tentativa sai
	// junto com a conta) e fechar de novo.
	var transferID string
	if err := db.QueryRow(ctx, `SELECT id FROM finance_pix_transfers WHERE entry_id = $1`, *first.EntryID).Scan(&transferID); err != nil {
		t.Fatal(err)
	}
	call(http.MethodPost, "/api/finance/pix/"+transferID+"/resolve", map[string]any{"sent": false}, http.StatusOK, nil)
	if p := payouts()[0]; p.Status != "OPEN" || p.PixStatus != finance.PixFailed {
		t.Fatalf("fechamento com o Pix que não saiu = %+v", p)
	}
	env.must(admin, http.MethodDelete, "/api/affiliates/payouts/"+first.ID.String(), nil, http.StatusNoContent)
	var left int
	_ = db.QueryRow(ctx, `SELECT count(*) FROM finance_pix_transfers`).Scan(&left)
	if left != 0 {
		t.Errorf("sobraram %d envios do fechamento desfeito", left)
	}
	second := closeMonth()

	// Pago por Pix: a conta é baixada, a tarifa entra nas contas pagas e o
	// fechamento aparece pago, com o comprovante.
	fake.respond = fake.completes
	if code, body := send(*second.EntryID); code != http.StatusOK {
		t.Fatalf("Pix = %d %s", code, body)
	}
	if n := len(fake.sent); n != 2 || fake.sent[1].Key != "joao.novo@pix.test" || fake.sent[1].AmountCents != 680 {
		t.Fatalf("envios = %+v", fake.sent)
	}
	p := payouts()[0]
	if p.Status != "PAID" || p.PixStatus != finance.PixComplete || p.ReceiptURL == "" || p.PaidOn == nil {
		t.Fatalf("fechamento pago por Pix = %+v", p)
	}
	var list []*affiliates.Affiliate
	call(http.MethodGet, "/api/affiliates", nil, http.StatusOK, &list)
	if len(list) != 1 || list[0].PaidCents != 600 || list[0].OpenCents != 0 {
		t.Errorf("joão depois do Pix = %+v", list)
	}
	var fees int
	_ = db.QueryRow(ctx, `SELECT count(*) FROM finance_entries WHERE status = 'PAID' AND amount_cents = 80`).Scan(&fees)
	if fees != 1 {
		t.Errorf("tarifas lançadas = %d", fees)
	}
	env.must(admin, http.MethodDelete, "/api/affiliates/payouts/"+second.ID.String(), nil, http.StatusBadRequest)
}
