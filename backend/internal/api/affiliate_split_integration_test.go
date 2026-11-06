package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/affiliates"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/payments"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/payments/pagarme"
)

// fakeRecipients é o cadastro de recebedores da Pagar.me de mentira.
type fakeRecipients struct {
	mu      sync.Mutex
	created []pagarme.RecipientRequest
	status  map[string]string
}

func (f *fakeRecipients) CreateRecipient(_ context.Context, req pagarme.RecipientRequest) (*pagarme.Recipient, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created = append(f.created, req)
	if f.status == nil {
		f.status = map[string]string{}
	}
	f.status["rp_lia"] = pagarme.RecipientRegistration
	return &pagarme.Recipient{ID: "rp_lia", Code: req.Code, Status: pagarme.RecipientRegistration}, nil
}

func (f *fakeRecipients) GetRecipient(_ context.Context, id string) (*pagarme.Recipient, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return &pagarme.Recipient{ID: id, Status: f.status[id]}, nil
}

func (f *fakeRecipients) CreateKYCLink(context.Context, string) (*pagarme.KYCLink, error) {
	exp := time.Now().Add(20 * time.Minute)
	return &pagarme.KYCLink{URL: "https://kyc.pagar.me/abc", Base64: "iVBOR", ExpirationDate: &exp}, nil
}

func (f *fakeRecipients) set(id, status string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status[id] = status
}

// O split dos afiliados de ponta a ponta: o afiliado se cadastra como
// recebedor pela página dele (e pede a prova de vida); enquanto não está
// ativo, a comissão acumula; ativo (o webhook avisa), o pagamento do cliente
// indicado vai pela Pagar.me com o split — a comissão do mês e a acumulada
// para o afiliado, o resto (com a tarifa e o chargeback) para a Farbo — e as
// comissões ficam pagas pelo split, sem fechamento. A cobrança vencida solta
// a reserva; o estorno desfaz a parte do afiliado. Precisa de
// FARBO_TEST_DATABASE_URL.
func TestAffiliateSplit(t *testing.T) {
	pm := &fakePagarme{}
	recipients := &fakeRecipients{}
	clock := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	var affSvc *affiliates.Service
	env, _ := newLeadsEnv(t, func(d *Deps) {
		d.Config.Payments = config.Payments{
			PixExpiresIn: 24 * time.Hour, PagarmeSecretKey: "sk_test_x", PagarmeWebhookSecret: "segredo-pm",
			PagarmeSplit: true, PagarmeRecipientID: "rp_farbo", AbacatePayPixFeeCents: 80, PagarmePixFeeBps: 99,
		}
		d.Billing.SetClock(func() time.Time { return clock })
		svc := payments.NewService(payments.NewRepository(d.DB), d.Billing, nil, d.Audit, d.Config.Payments, d.Log)
		svc.SetPagarme(pm)
		svc.SetSplit(d.Affiliates, "rp_farbo")
		d.Affiliates.SetSplit(true)
		d.Affiliates.SetRecipients(recipients)
		d.Payments = svc
		affSvc = d.Affiliates
	})
	ctx := context.Background()
	admin := env.login(auth.RoleAdmin + "@leads.test")

	var lia affiliates.Affiliate
	_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/affiliates", map[string]any{
		"name": "Lia Split", "code": "lia", "commissionCents": 600, "active": true,
	}, http.StatusCreated), &lia)
	partner := "/api/public/partner/" + lia.ReportToken

	// A página mostra o cadastro de recebedor; os dados vão para a Pagar.me.
	var report affiliates.Report
	_ = json.Unmarshal(env.must("", http.MethodGet, partner, nil, http.StatusOK), &report)
	if report.Recipient == nil || !report.Recipient.Enabled || report.Recipient.Status != "" {
		t.Fatalf("recebedor na página = %+v", report.Recipient)
	}
	env.must("", http.MethodPost, partner+"/recipient/kyc-link", nil, http.StatusBadRequest)
	pf := map[string]any{
		"kind": "individual", "document": "529.982.247-25", "name": "Lia da Silva", "email": "lia@afiliada.test",
		"phone": "(21) 99464-7568", "birthdate": "12/10/1995", "monthlyIncomeCents": 500000, "occupation": "Influenciadora",
		"zipCode": "20040-020", "street": "Av. Rio Branco", "number": "100", "district": "Centro", "city": "Rio de Janeiro",
		"state": "RJ", "bank": "260", "branch": "0001", "account": "1234567", "accountDigit": "8", "accountType": "checking",
	}
	bad := map[string]any{}
	for k, v := range pf {
		bad[k] = v
	}
	bad["birthdate"] = "12/10/2015"
	if out := string(env.must("", http.MethodPost, partner+"/recipient", bad, http.StatusBadRequest)); !strings.Contains(out, "18 anos") {
		t.Fatalf("menor de idade = %s", out)
	}
	var view affiliates.RecipientView
	_ = json.Unmarshal(env.must("", http.MethodPost, partner+"/recipient", pf, http.StatusCreated), &view)
	if view.Status != pagarme.RecipientRegistration {
		t.Fatalf("recebedor criado = %+v", view)
	}
	if c := recipients.created[0]; c.Code != lia.ID.String() || c.RegisterInformation.Document != "52998224725" ||
		c.RegisterInformation.PhoneNumbers[0].DDD != "21" || c.DefaultBankAccount.HolderDocument != "52998224725" ||
		c.DefaultBankAccount.HolderName != "Lia da Silva" || c.RegisterInformation.Address.ZipCode != "20040020" {
		t.Fatalf("pedido do recebedor = %+v", c)
	}
	env.must("", http.MethodPost, partner+"/recipient", pf, http.StatusBadRequest) // só uma vez
	var link struct {
		URL            string     `json:"url"`
		ExpirationDate *time.Time `json:"expirationDate"`
	}
	_ = json.Unmarshal(env.must("", http.MethodPost, partner+"/recipient/kyc-link", nil, http.StatusOK), &link)
	if link.URL != "https://kyc.pagar.me/abc" || link.ExpirationDate == nil {
		t.Fatalf("link da prova de vida = %+v", link)
	}

	// O cliente indicado, com o cadastro que a Pagar.me pede.
	var cust struct {
		ID uuid.UUID `json:"id"`
	}
	_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/customers", map[string]any{
		"name": "Rui Indicado", "email": "rui@split.test", "password": userPassword,
	}, http.StatusCreated), &cust)
	if _, err := env.db.Exec(ctx, `UPDATE users SET document = '529.982.247-25', phone = '(11) 99999-0000' WHERE id = $1`, cust.ID); err != nil {
		t.Fatal(err)
	}
	env.must(admin, http.MethodPut, "/api/customers/"+cust.ID.String()+"/affiliate", map[string]any{"affiliateId": lia.ID}, http.StatusOK)
	tok := env.login("rui@split.test")
	var sub string
	if err := env.db.QueryRow(ctx, `
		INSERT INTO subscriptions (customer_id, plan_name, price_cents, due_day, next_due_date)
		VALUES ($1, 'Plano Mensal', 14990, 10, $2) RETURNING id`, cust.ID, clock.AddDate(0, 3, 0)).Scan(&sub); err != nil {
		t.Fatal(err)
	}
	invoice := func(due time.Time) uuid.UUID {
		t.Helper()
		var id uuid.UUID
		if err := env.db.QueryRow(ctx, `
			INSERT INTO invoices (customer_id, subscription_id, description, amount_cents, due_date)
			VALUES ($1, $2, 'Plano Mensal', 14990, $3) RETURNING id`, cust.ID, sub, due).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	type charge struct {
		ID               uuid.UUID `json:"id"`
		Provider         string    `json:"provider"`
		Status           string    `json:"status"`
		ProviderChargeID string    `json:"providerChargeId"`
	}
	payPix := func(inv uuid.UUID) charge {
		t.Helper()
		var c charge
		_ = json.Unmarshal(env.must(tok, http.MethodPost, "/api/me/invoices/"+inv.String()+"/pix", nil, http.StatusOK), &c)
		return c
	}
	settle := func(c charge) {
		t.Helper()
		pm.pay(c.ProviderChargeID)
		var out charge
		_ = json.Unmarshal(env.must(tok, http.MethodPost, "/api/me/charges/"+c.ID.String()+"/simulate", nil, http.StatusOK), &out)
		if out.Status != "PAID" {
			t.Fatalf("não pagou: %+v", out)
		}
	}
	commissions := func() map[string]int {
		t.Helper()
		if _, _, err := affSvc.Reconcile(ctx); err != nil {
			t.Fatal(err)
		}
		out := map[string]int{}
		rows, err := env.db.Query(ctx, `
			SELECT to_char(c.month, 'MM') || CASE WHEN c.split_charge_id IS NULL THEN '' ELSE '/split' END, c.amount_cents
			FROM affiliate_commissions c WHERE c.affiliate_id = $1`, lia.ID)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		for rows.Next() {
			var k string
			var v int
			_ = rows.Scan(&k, &v)
			out[k] = v
		}
		return out
	}

	// Recebedor ainda sem a prova de vida: sem split; a comissão acumula.
	nov := invoice(time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC))
	first := payPix(nov)
	if pm.last().Split != nil {
		t.Fatalf("split antes do recebedor ativo: %+v", pm.last().Split)
	}
	settle(first)
	if got := commissions(); got["11"] != 600 || len(got) != 1 {
		t.Fatalf("comissão acumulada = %v", got)
	}

	// A Pagar.me aprova o recebedor: o webhook atualiza.
	recipients.set("rp_lia", pagarme.RecipientActive)
	env.must("", http.MethodPost, "/api/payments/pagarme/webhook?secret=segredo-pm",
		map[string]any{"id": "hook_rp", "type": "recipient.updated", "data": map[string]any{"id": "rp_lia"}}, http.StatusOK)
	_ = json.Unmarshal(env.must("", http.MethodGet, partner, nil, http.StatusOK), &report)
	if report.Recipient.Status != pagarme.RecipientActive {
		t.Fatalf("recebedor depois do webhook = %+v", report.Recipient)
	}

	// Ativo: o pagamento leva o split — a do mês (R$ 6) e a acumulada (R$ 6).
	dec := invoice(time.Date(2026, 12, 10, 0, 0, 0, 0, time.UTC))
	second := payPix(dec)
	if second.Provider != "pagarme" {
		t.Fatalf("com split, o Pix vai pela Pagar.me: %+v", second)
	}
	split := pm.last().Split
	if len(split) != 2 || split[0].RecipientID != "rp_lia" || split[0].Amount != 1200 || split[0].Options.Liable ||
		split[1].RecipientID != "rp_farbo" || split[1].Amount != 14990-1200 || !split[1].Options.Liable ||
		!split[1].Options.ChargeProcessingFee || !split[1].Options.ChargeRemainderFee {
		t.Fatalf("regras do split = %+v", split)
	}
	settle(second)
	if got := commissions(); got["11/split"] != 600 || got["12/split"] != 600 || len(got) != 2 {
		t.Fatalf("comissões pagas pelo split = %v", got)
	}
	var list []*affiliates.Affiliate
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/affiliates", nil, http.StatusOK), &list)
	for _, a := range list {
		if a.ID == lia.ID && (a.SplitPaidCents != 1200 || a.PendingCents != 0 || a.RecipientStatus != "active") {
			t.Fatalf("afiliada na lista = %+v", a)
		}
	}
	// Sem fechamento com o split ligado.
	var closing []affiliates.ClosingLine
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/affiliates/closing?month=2026-12", nil, http.StatusOK), &closing)
	if len(closing) != 0 {
		t.Fatalf("fechamento com split = %+v", closing)
	}
	env.must(admin, http.MethodPost, "/api/affiliates/closing", map[string]any{"month": "2026-12", "dueDate": "2027-01-10"},
		http.StatusBadRequest)

	// Uma comissão acumulada (o janeiro pago por fora) e o Pix de fevereiro
	// que vence sem pagar: a reserva volta.
	jan := invoice(time.Date(2027, 1, 10, 0, 0, 0, 0, time.UTC))
	env.must(admin, http.MethodPost, "/api/invoices/"+jan.String()+"/pay", nil, http.StatusOK)
	if got := commissions(); got["01"] != 600 {
		t.Fatalf("janeiro pago por fora = %v", got)
	}
	feb := payPix(invoice(time.Date(2027, 2, 10, 0, 0, 0, 0, time.UTC)))
	if s := pm.last().Split; len(s) != 2 || s[0].Amount != 1200 {
		t.Fatalf("split de fevereiro (com a acumulada de janeiro) = %+v", s)
	}
	if got := commissions(); got["01/split"] != 600 {
		t.Fatalf("janeiro reservado = %v", got)
	}
	pm.mu.Lock()
	pm.charges[feb.ProviderChargeID].Status = "canceled"
	pm.mu.Unlock()
	if _, err := env.db.Exec(ctx, `UPDATE payment_charges SET checked_at = NULL WHERE id = $1`, feb.ID); err != nil {
		t.Fatal(err)
	}
	var expired charge
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/charges/"+feb.ID.String(), nil, http.StatusOK), &expired)
	if expired.Status != "EXPIRED" {
		t.Fatalf("Pix de fevereiro = %+v", expired)
	}
	if got := commissions(); got["01"] != 600 {
		t.Fatalf("a reserva do Pix vencido não voltou: %v", got)
	}

	// O estorno do Pix de dezembro: a Pagar.me desfaz a parte da afiliada; a
	// de dezembro some (a fatura reabriu) e a de novembro volta a acumular.
	env.must(admin, http.MethodPost, "/api/charges/"+second.ID.String()+"/refund", map[string]string{"reason": "pago em dobro"}, http.StatusOK)
	got := commissions()
	if _, ok := got["12/split"]; ok || got["11"] != 600 || got["01"] != 600 {
		t.Fatalf("depois do estorno = %v", got)
	}

	// No percentual (10%) com os impostos da Farbo (6%), a parte do mês sai da
	// mensalidade líquida: 10% de 149,90 × 0,94 = 14,09; as acumuladas (R$ 6
	// cada) vão como nasceram.
	env.must(admin, http.MethodPatch, "/api/affiliates/"+lia.ID.String(), map[string]any{
		"name": lia.Name, "code": lia.Code, "active": true, "commissionType": "percent", "commissionBps": 1000,
	}, http.StatusOK)
	env.must(admin, http.MethodPut, "/api/affiliates/settings", map[string]any{"defaultCommissionCents": 600, "commissionTaxBps": 600},
		http.StatusOK)
	payPix(invoice(time.Date(2027, 3, 10, 0, 0, 0, 0, time.UTC)))
	if s := pm.last().Split; len(s) != 2 || s[0].Amount != 1409+600+600 || s[1].Amount != 14990-(1409+600+600) {
		t.Fatalf("split no percentual com os impostos = %+v", s)
	}
}
