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
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/payments"
)

// Os acertos da central na cobrança: parcelar o rastreador de um pedido
// feito à vista (com e sem frete), o Pix de antes do acerto que não quita
// sozinho, mudar o vencimento de uma fatura e o dia de vencimento da
// assinatura. Precisa de FARBO_TEST_DATABASE_URL.
func TestBillingAdjustments(t *testing.T) {
	var bs *billing.Service
	env, _ := newLeadsEnv(t, func(d *Deps) {
		d.Config.Payments = config.Payments{AbacatePayAPIKey: "abc_dev_teste", PixExpiresIn: 24 * time.Hour}
		bs = d.Billing
		d.Payments = payments.NewService(payments.NewRepository(d.DB), d.Billing, &fakePixGateway{}, d.Audit, d.Config.Payments, d.Log)
	})
	ctx := context.Background()
	bs.SetClock(func() time.Time { return time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC) })
	admin := env.login(auth.RoleAdmin + "@leads.test")
	var bia struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/customers", map[string]any{
		"name": "Bia", "email": "bia@acerto.test", "phone": "(11) 97777-0000", "document": "", "password": userPassword,
	}, http.StatusCreated), &bia)
	token := env.login("bia@acerto.test")
	env.must(token, http.MethodPut, "/api/me/address", map[string]any{
		"zipCode": "01305-000", "street": "Rua Augusta", "number": "500", "district": "Consolação", "city": "São Paulo", "state": "SP",
	}, http.StatusOK)

	// A Bia contrata à vista (escolheu errado): o rastreador numa fatura só.
	var order struct {
		Subscription billing.Subscription `json:"subscription"`
		SetupInvoice *billing.Invoice     `json:"setupInvoice"`
	}
	_ = json.Unmarshal(env.must(token, http.MethodPost, "/api/me/trackers", map[string]any{
		"vehicle": map[string]any{"name": "Moto", "plate": "ACE1A11"},
	}, http.StatusCreated), &order)
	sub, setup := order.Subscription, order.SetupInvoice
	if setup == nil || setup.AmountCents != 15000 || sub.Installments != 0 {
		t.Fatalf("pedido à vista = %+v %+v", sub, setup)
	}
	// Ela já tinha gerado o Pix do pedido (R$ 150,00) e o da mensalidade de
	// outubro (R$ 69,90).
	pix := func(invoice string) payments.Charge {
		t.Helper()
		var c payments.Charge
		_, raw := env.do(admin, http.MethodPost, "/api/invoices/"+invoice+"/pix", nil)
		_ = json.Unmarshal(raw, &c)
		return c
	}
	var monthlyID string
	_ = env.db.QueryRow(ctx, `SELECT id FROM invoices WHERE subscription_id = $1`, sub.ID).Scan(&monthlyID)
	oldPix, oldMonthlyPix := pix(setup.ID.String()), pix(monthlyID)
	if oldPix.AmountCents != 15000 || oldMonthlyPix.AmountCents != 6990 {
		t.Fatalf("Pix de antes = %+v / %+v", oldPix, oldMonthlyPix)
	}

	finance := func(invoice string, body map[string]any, want int) []byte {
		t.Helper()
		return env.must(admin, http.MethodPost, "/api/invoices/"+invoice+"/installments", body, want)
	}
	// Recusas: mais que o teto, valor fora da fatura, mensalidade.
	finance(setup.ID.String(), map[string]any{"subscriptionId": sub.ID, "installments": 11, "equipmentCents": 15000}, http.StatusBadRequest)
	finance(setup.ID.String(), map[string]any{"subscriptionId": sub.ID, "installments": 10, "equipmentCents": 15001}, http.StatusBadRequest)
	finance(monthlyID, map[string]any{"subscriptionId": sub.ID, "installments": 10, "equipmentCents": 6990}, http.StatusBadRequest)

	// Em 10x: a fatura do pedido, que só tinha o rastreador, é cancelada; a
	// mensalidade de outubro (já gerada, ainda não vencida) leva a 1ª
	// parcela; a permanência vai até julho de 2027, como se tivesse parcelado
	// no pedido.
	var financed struct {
		Invoice      billing.Invoice      `json:"invoice"`
		Subscription billing.Subscription `json:"subscription"`
	}
	_ = json.Unmarshal(finance(setup.ID.String(), map[string]any{"subscriptionId": sub.ID, "installments": 10, "equipmentCents": 15000}, http.StatusOK), &financed)
	if financed.Invoice.Status != billing.InvoiceCanceled ||
		!strings.HasSuffix(financed.Invoice.Description, "rastreador de R$ 150,00 em 10x sem juros, nas mensalidades") {
		t.Fatalf("fatura do pedido = %+v", financed.Invoice)
	}
	if s := financed.Subscription; s.Installments != 10 || s.CommitmentUntil == nil || s.CommitmentUntil.Format(time.DateOnly) != "2027-07-10" ||
		s.InstallmentsDueCents != 15000 {
		t.Fatalf("assinatura parcelada = %+v", s)
	}
	var october billing.Invoice
	_ = env.db.QueryRow(ctx, `SELECT amount_cents, description FROM invoices WHERE id = $1`, monthlyID).Scan(&october.AmountCents, &october.Description)
	if october.AmountCents != 8490 || !strings.HasSuffix(october.Description, "+ parcela 1/10 do rastreador") {
		t.Errorf("outubro = %+v", october)
	}
	// De novo: não (já parcelado, e a fatura não está mais em aberto).
	finance(setup.ID.String(), map[string]any{"subscriptionId": sub.ID, "installments": 5, "equipmentCents": 1500}, http.StatusConflict)

	// Os Pix de antes não quitam nada sozinhos (vão para revisão): o do
	// pedido, cancelado, e o de R$ 69,90 da mensalidade, que agora é de
	// R$ 84,90 — sai um Pix novo, e esse quita.
	env.must(admin, http.MethodPost, "/api/charges/"+oldPix.ID.String()+"/simulate", nil, http.StatusOK)
	env.must(admin, http.MethodPost, "/api/charges/"+oldMonthlyPix.ID.String()+"/simulate", nil, http.StatusOK)
	status := func(invoice string) string {
		var s string
		_ = env.db.QueryRow(ctx, `SELECT status FROM invoices WHERE id = $1`, invoice).Scan(&s)
		return s
	}
	var review int
	_ = env.db.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE action = 'PAYMENT_UNMATCHED' AND result = 'REVIEW'`).Scan(&review)
	if status(setup.ID.String()) != billing.InvoiceCanceled || status(monthlyID) != billing.InvoiceOpen || review != 2 {
		t.Fatalf("Pix antigos pagos: pedido %s, outubro %s, revisões %d", status(setup.ID.String()), status(monthlyID), review)
	}
	newPix := pix(monthlyID)
	if newPix.AmountCents != 8490 || newPix.ID == oldMonthlyPix.ID {
		t.Fatalf("Pix depois do acerto = %+v", newPix)
	}
	env.must(admin, http.MethodPost, "/api/charges/"+newPix.ID.String()+"/simulate", nil, http.StatusOK)
	if status(monthlyID) != billing.InvoicePaid {
		t.Errorf("Pix novo pago: outubro %s", status(monthlyID))
	}

	// Com frete: o rastreador de R$ 150,00 em 10x numa fatura de R$ 172,40,
	// na assinatura de outro veículo — a fatura fica só com o frete.
	var second struct {
		Subscription billing.Subscription `json:"subscription"`
	}
	_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/customers/"+bia.ID+"/trackers", map[string]any{
		"vehicle": map[string]any{"name": "Carro", "plate": "ACE2B22"}, "equipmentCents": 0,
		"plan": map[string]any{"planName": "Plano Mensal", "priceCents": 6990, "dueDay": 10},
	}, http.StatusCreated), &second)
	var withFreight billing.Invoice
	_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/customers/"+bia.ID+"/invoices", map[string]any{
		"description": "Rastreador J16 GT06 + frete PAC (R$ 22,40)", "amountCents": 17240, "dueDate": "2026-10-10",
	}, http.StatusCreated), &withFreight)
	_ = json.Unmarshal(finance(withFreight.ID.String(), map[string]any{"subscriptionId": second.Subscription.ID, "installments": 10, "equipmentCents": 15000}, http.StatusOK), &financed)
	if financed.Invoice.AmountCents != 2240 || financed.Invoice.Status != billing.InvoiceOpen {
		t.Errorf("com frete, fica = %+v", financed.Invoice)
	}
	var secondMonthly string
	var secondCents int
	_ = env.db.QueryRow(ctx, `SELECT id, amount_cents FROM invoices WHERE subscription_id = $1`, second.Subscription.ID).Scan(&secondMonthly, &secondCents)
	if secondCents != 8490 {
		t.Errorf("com frete, outubro = %d", secondCents)
	}

	// O vencimento de uma fatura: para depois, sim; para trás, não; e só em
	// aberto.
	var moved billing.Invoice
	_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/invoices/"+secondMonthly+"/due-date", map[string]any{"dueDate": "2026-10-25"}, http.StatusOK), &moved)
	if moved.DueDate.Format(time.DateOnly) != "2026-10-25" {
		t.Errorf("vencimento novo = %v", moved.DueDate)
	}
	env.must(admin, http.MethodPost, "/api/invoices/"+secondMonthly+"/due-date", map[string]any{"dueDate": "2026-10-01"}, http.StatusBadRequest)
	env.must(admin, http.MethodPost, "/api/invoices/"+setup.ID.String()+"/due-date", map[string]any{"dueDate": "2026-10-30"}, http.StatusConflict)

	// O dia de vencimento da assinatura: do 10 para o 20. A mensalidade em
	// aberto vai para o dia 20 do mesmo mês, as próximas já saem no 20, e a
	// permanência acompanha.
	var changed billing.Subscription
	subPath := "/api/subscriptions/" + second.Subscription.ID.String() + "/due-day"
	env.must(admin, http.MethodPost, subPath, map[string]any{"dueDay": 31}, http.StatusBadRequest)
	_ = json.Unmarshal(env.must(admin, http.MethodPost, subPath, map[string]any{"dueDay": 20}, http.StatusOK), &changed)
	if changed.DueDay != 20 || changed.NextDueDate.Format(time.DateOnly) != "2026-11-20" || changed.CommitmentUntil.Format(time.DateOnly) != "2027-07-20" {
		t.Fatalf("dia novo = %+v", changed)
	}
	var due time.Time
	_ = env.db.QueryRow(ctx, `SELECT due_date FROM invoices WHERE id = $1`, secondMonthly).Scan(&due)
	if due.Format(time.DateOnly) != "2026-10-20" {
		t.Errorf("a mensalidade de outubro foi para %v", due)
	}
	if _, err := billing.NewRepository(env.db).GenerateDue(ctx, billing.NewDate(2026, 11, 30)); err != nil {
		t.Fatal(err)
	}
	var novDue time.Time
	var novDesc string
	_ = env.db.QueryRow(ctx, `SELECT due_date, description FROM invoices WHERE subscription_id = $1 AND due_date > '2026-11-01'`,
		second.Subscription.ID).Scan(&novDue, &novDesc)
	if novDue.Format(time.DateOnly) != "2026-11-20" || !strings.HasSuffix(novDesc, "parcela 2/10 do rastreador") {
		t.Errorf("novembro = %v %q", novDue, novDesc)
	}
}
