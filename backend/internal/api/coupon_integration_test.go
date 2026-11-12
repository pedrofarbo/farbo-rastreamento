package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/contract"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/orders"
)

// O cupom de desconto da contratação direta pelo site: sem cupom valendo, o
// site nem mostra o campo; a central cria (porcentagem ou valor fixo, em
// quantas cobranças, validade e limite de usos); pelo link de um
// representante, não vale; a contratação com o cupom sai com o desconto nas
// primeiras cobranças e o valor normal depois; o limite de usos, a validade
// e o cupom desligado recusam; o cupom usado não sai (só desligado).
// Precisa de FARBO_TEST_DATABASE_URL.
func TestCheckoutCoupons(t *testing.T) {
	var bs *billing.Service
	env, _ := newLeadsEnv(t, func(d *Deps) {
		bs = d.Billing
		d.Config.Catalog.SetupDueDays = 0
		d.Orders = orders.NewService(d.DB, d.Billing, d.Vehicles, d.Config.Catalog, d.Log)
		doc, err := contract.Render(contract.Params{Company: config.Company{Name: "Farbo Rastreadores"}, HistoryOptions: []int{7, 14, 30}})
		if err != nil {
			t.Fatal(err)
		}
		svc := contract.NewService(d.DB, doc, &recordingContractMailer{}, d.Log)
		svc.SetSync()
		d.Contract = svc
	})
	ctx := context.Background()
	clock := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	bs.SetClock(func() time.Time { return clock })
	admin := env.login(auth.RoleAdmin + "@leads.test")

	type offerView struct {
		PlanCents       int    `json:"planCents"`
		ContractVersion string `json:"contractVersion"`
		Coupons         bool   `json:"coupons"`
	}
	offer := func(ref string) offerView {
		t.Helper()
		var o offerView
		_ = json.Unmarshal(env.must("", http.MethodGet, "/api/public/checkout?ref="+ref, nil, http.StatusOK), &o)
		return o
	}
	// Sem nenhum cupom, o campo não aparece.
	site := offer("")
	if site.Coupons {
		t.Fatalf("oferta sem cupom = %+v", site)
	}

	// A central: o formato e os limites; o código repetido.
	for _, bad := range []map[string]any{
		{"code": "a b", "percent": 10, "charges": 1, "active": true},
		{"code": "DOIS", "percent": 10, "amountCents": 500, "charges": 1, "active": true},
		{"code": "NADA", "charges": 1, "active": true},
		{"code": "MUITO", "percent": 101, "charges": 1, "active": true},
		{"code": "LONGO", "percent": 10, "charges": 61, "active": true},
	} {
		env.must(admin, http.MethodPost, "/api/coupons", bad, http.StatusBadRequest)
	}
	var welcome orders.Coupon
	_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/coupons", map[string]any{
		"code": " bem-vindo ", "description": "Instagram", "percent": 10, "charges": 2, "active": true,
	}, http.StatusCreated), &welcome)
	if welcome.Code != "BEM-VINDO" || *welcome.Percent != 10 || welcome.Charges != 2 || welcome.Uses != 0 {
		t.Fatalf("cupom criado = %+v", welcome)
	}
	env.must(admin, http.MethodPost, "/api/coupons", map[string]any{"code": "BEM-VINDO", "percent": 5, "charges": 1, "active": true},
		http.StatusConflict)

	// O representante: pelo link dele, o cupom não vale.
	env.must(admin, http.MethodPost, "/api/affiliates", map[string]any{"name": "Rita Representante", "code": "rita", "active": true},
		http.StatusCreated)
	if !offer("").Coupons || offer("rita").Coupons {
		t.Fatalf("o campo do cupom: direto = %v, pelo link = %v", offer("").Coupons, offer("rita").Coupons)
	}
	var shown orders.CouponOffer
	_ = json.Unmarshal(env.must("", http.MethodGet, "/api/public/checkout/coupon?code=bem-vindo", nil, http.StatusOK), &shown)
	if shown.Code != "BEM-VINDO" || shown.Percent == nil || *shown.Percent != 10 || shown.Charges != 2 || shown.AmountCents != nil {
		t.Fatalf("cupom no site = %+v", shown)
	}
	env.must("", http.MethodGet, "/api/public/checkout/coupon?code=NAOEXISTE", nil, http.StatusBadRequest)
	env.must("", http.MethodGet, "/api/public/checkout/coupon?code=BEM-VINDO&ref=rita", nil, http.StatusBadRequest)

	address := map[string]any{"zipCode": "01305-000", "street": "Rua Augusta", "number": "500", "district": "Consolação",
		"city": "São Paulo", "state": "SP"}
	type checkoutResult struct {
		Subscription billing.Subscription `json:"subscription"`
		FirstInvoice *billing.Invoice     `json:"firstInvoice"`
	}
	body := func(email, document, coupon, ref string) map[string]any {
		return map[string]any{
			"name": "Gil Souza", "email": email, "phone": "(11) 95555-0000", "document": document, "password": "senha-forte-1",
			"vehicle": map[string]any{"kind": "CAR", "brand": "Fiat", "model": "Uno", "plate": ""}, "address": address,
			"contractVersion": site.ContractVersion, "accept": true, "equipmentMode": "sale", "coupon": coupon, "ref": ref,
		}
	}
	checkout := func(email, document, coupon string) checkoutResult {
		t.Helper()
		var r checkoutResult
		_ = json.Unmarshal(env.must("", http.MethodPost, "/api/public/checkout", body(email, document, coupon, ""), http.StatusCreated), &r)
		return r
	}
	refused := func(email, document, coupon, ref, want string) {
		t.Helper()
		status, out := env.do("", http.MethodPost, "/api/public/checkout", body(email, document, coupon, ref))
		if status != http.StatusBadRequest || !strings.Contains(string(out), want) {
			t.Fatalf("contratação com o cupom %s: %d %s", coupon, status, out)
		}
		var n int
		_ = env.db.QueryRow(ctx, `SELECT count(*) FROM users WHERE email = $1`, email).Scan(&n)
		if n != 0 {
			t.Fatalf("a contratação recusada deixou a conta %s", email)
		}
	}
	refused("rita@cupom.test", "123.456.781-43", "BEM-VINDO", "rita", "sem o link de um representante")

	// Com o cupom: as 2 primeiras cobranças com 10%, depois o valor normal.
	price := site.PlanCents
	discounted := price - (price*10+50)/100
	gil := checkout("gil@cupom.test", "123.456.781-43", "bem-vindo")
	sub := gil.Subscription
	if sub.CouponCode != "BEM-VINDO" || sub.PriceCents != price || sub.PromoPriceCents == nil || *sub.PromoPriceCents != discounted ||
		sub.PromoUntil == nil || sub.PromoUntil.String() != sub.NextDueDate.AddDate(0, 2, 0).Format(time.DateOnly) {
		t.Fatalf("assinatura com o cupom = %+v", sub)
	}
	if gil.FirstInvoice == nil || gil.FirstInvoice.AmountCents != discounted || !strings.Contains(gil.FirstInvoice.Description, "cupom BEM-VINDO") {
		t.Fatalf("1ª cobrança com o cupom = %+v", gil.FirstInvoice)
	}
	if _, err := env.db.Exec(ctx, `UPDATE invoices SET status = 'PAID', paid_at = NOW(), paid_via = 'MANUAL'
		WHERE customer_id = $1`, sub.CustomerID); err != nil {
		t.Fatal(err)
	}
	clock = time.Date(2027, 2, 15, 12, 0, 0, 0, time.UTC)
	bs.GenerateInvoices(ctx)
	rows, err := env.db.Query(ctx, `SELECT amount_cents, description FROM invoices WHERE subscription_id = $1 ORDER BY due_date`, sub.ID)
	if err != nil {
		t.Fatal(err)
	}
	var amounts []int
	var descriptions []string
	for rows.Next() {
		var cents int
		var description string
		if err := rows.Scan(&cents, &description); err != nil {
			t.Fatal(err)
		}
		amounts, descriptions = append(amounts, cents), append(descriptions, description)
	}
	rows.Close()
	if len(amounts) < 3 || amounts[0] != discounted || amounts[1] != discounted || amounts[2] != price ||
		strings.Contains(descriptions[2], "cupom") {
		t.Fatalf("as cobranças = %v %q", amounts, descriptions)
	}
	clock = time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)

	// A troca de plano pela central não põe a promoção de pré-lançamento no
	// lugar do desconto do cupom.
	var changed billing.Subscription
	_ = json.Unmarshal(env.must(admin, http.MethodPatch, "/api/subscriptions/"+sub.ID.String(),
		map[string]any{"planName": "Plano Outro", "priceCents": price}, http.StatusOK), &changed)
	if changed.PromoPriceCents == nil || *changed.PromoPriceCents != discounted || changed.CouponCode != "BEM-VINDO" {
		t.Fatalf("plano trocado = %+v", changed)
	}

	// O valor fixo, com uma vaga só: a segunda contratação é recusada.
	var fixed orders.Coupon
	_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/coupons", map[string]any{
		"code": "VINTE", "amountCents": 2000, "charges": 1, "maxUses": 1, "active": true,
	}, http.StatusCreated), &fixed)
	lia := checkout("lia@cupom.test", "987.654.320-29", "VINTE")
	if lia.FirstInvoice == nil || lia.FirstInvoice.AmountCents != price-2000 ||
		lia.Subscription.PromoUntil.String() != lia.Subscription.NextDueDate.AddDate(0, 1, 0).Format(time.DateOnly) {
		t.Fatalf("cupom de valor fixo = %+v / %+v", lia.Subscription, lia.FirstInvoice)
	}
	refused("ana@cupom.test", "456.123.789-55", "VINTE", "", "máximo de vezes")

	// Vencido e desligado.
	yesterday := billing.Date{Time: clock.AddDate(0, 0, -1)}
	env.must(admin, http.MethodPut, "/api/coupons/"+welcome.ID.String(), map[string]any{
		"percent": 10, "charges": 2, "validUntil": yesterday, "active": true,
	}, http.StatusOK)
	refused("ana@cupom.test", "456.123.789-55", "BEM-VINDO", "", "venceu")
	env.must(admin, http.MethodPut, "/api/coupons/"+welcome.ID.String(), map[string]any{"percent": 10, "charges": 2, "active": false},
		http.StatusOK)
	refused("ana@cupom.test", "456.123.789-55", "BEM-VINDO", "", "não está mais valendo")
	refused("ana@cupom.test", "456.123.789-55", "OUTRO", "", "cupom não encontrado")
	// Sem nenhum valendo (o VINTE esgotou, o BEM-VINDO desligado), o campo some.
	if offer("").Coupons {
		t.Fatal("o campo do cupom sem nenhum valendo")
	}

	// A lista da central, com os usos; o usado não sai, o sem uso sai.
	var list []orders.Coupon
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/coupons", nil, http.StatusOK), &list)
	uses := map[string]int{}
	for _, c := range list {
		uses[c.Code] = c.Uses
	}
	if len(list) != 2 || uses["BEM-VINDO"] != 1 || uses["VINTE"] != 1 {
		t.Fatalf("lista dos cupons = %+v", list)
	}
	env.must(admin, http.MethodDelete, "/api/coupons/"+welcome.ID.String(), nil, http.StatusConflict)
	var unused orders.Coupon
	_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/coupons", map[string]any{"code": "SOBRA", "percent": 5, "charges": 1, "active": true},
		http.StatusCreated), &unused)
	env.must(admin, http.MethodDelete, "/api/coupons/"+unused.ID.String(), nil, http.StatusNoContent)
	// O cliente não mexe nos cupons.
	env.must(gilToken(t, env), http.MethodGet, "/api/coupons", nil, http.StatusForbidden)
}

// gilToken entra com a conta da contratação com o cupom.
func gilToken(t *testing.T, env *credEnv) string {
	t.Helper()
	out := env.must("", http.MethodPost, "/api/auth/login",
		map[string]string{"email": "gil@cupom.test", "password": "senha-forte-1"}, http.StatusOK)
	var tokens struct {
		AccessToken string `json:"accessToken"`
	}
	_ = json.Unmarshal(out, &tokens)
	return tokens.AccessToken
}
