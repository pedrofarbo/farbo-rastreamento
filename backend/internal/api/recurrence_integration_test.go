package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/affiliates"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/contract"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/orders"
)

// Os planos semestral e anual: desligados até a central ligar (no site, nas
// opções do pedido; no link de um representante, no cadastro dele, com os
// valores dele). Pelo link com eles ligados, a contratação semestral sai com
// o valor do período e o rastreador incluso (nada somado; o comodato, em
// TestPeriodPlansLoan);
// paga, rende 6 meses de comissão; a fatura seguinte vem 6 meses depois, no
// dia da conta; cancelada com o período pago, a assinatura fica ativa até o
// fim dele, não renova e encerra sozinha na data. Precisa de
// FARBO_TEST_DATABASE_URL.
func TestPeriodPlans(t *testing.T) {
	var bs *billing.Service
	var aff *affiliates.Service
	env, _ := newLeadsEnv(t, func(d *Deps) {
		bs, aff = d.Billing, d.Affiliates
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
		Periods []struct {
			Months   int    `json:"months"`
			PlanName string `json:"planName"`
			Cents    int    `json:"cents"`
		} `json:"periods"`
		ContractVersion string `json:"contractVersion"`
	}
	offer := func(ref string) offerView {
		t.Helper()
		var o offerView
		path := "/api/public/checkout"
		if ref != "" {
			path += "?ref=" + ref
		}
		_ = json.Unmarshal(env.must("", http.MethodGet, path, nil, http.StatusOK), &o)
		return o
	}
	// Desligado por padrão: só o mensal.
	if o := offer(""); len(o.Periods) != 0 {
		t.Fatalf("oferta do site com o semestral desligado = %+v", o)
	}
	var options orders.Options
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/order-options", nil, http.StatusOK), &options)
	if options.Recurrence != (orders.Recurrence{SemiannualCents: 20000, AnnualCents: 40000}) {
		t.Fatalf("padrão = %+v", options.Recurrence)
	}

	// O representante: o dele ligado, com os valores dele; o outro, sem.
	var rafa, ana affiliates.Affiliate
	_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/affiliates", map[string]any{
		"name": "Rafa Representante", "code": "rafa", "active": true, "commissionCents": 500,
		"recurrence": map[string]any{"enabled": true, "semiannualCents": 25000, "annualCents": 45000},
	}, http.StatusCreated), &rafa)
	_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/affiliates", map[string]any{
		"name": "Ana Afiliada", "code": "ana", "active": true,
	}, http.StatusCreated), &ana)
	if !rafa.Recurrence.Enabled || rafa.Recurrence.SemiannualCents != 25000 || ana.Recurrence.Enabled ||
		ana.Recurrence.SemiannualCents != 20000 || ana.Recurrence.AnnualCents != 40000 {
		t.Fatalf("cadastros = %+v / %+v", rafa.Recurrence, ana.Recurrence)
	}
	// Ligado sem valor, não.
	env.must(admin, http.MethodPost, "/api/affiliates", map[string]any{"name": "Sem Valor", "code": "sem-valor", "active": true,
		"recurrence": map[string]any{"enabled": true, "semiannualCents": 0, "annualCents": 40000}}, http.StatusBadRequest)
	// A edição sem o campo (telas antigas) não desliga.
	env.must(admin, http.MethodPatch, "/api/affiliates/"+rafa.ID.String(), map[string]any{
		"name": "Rafa Representante", "code": "rafa", "active": true}, http.StatusOK)

	if o := offer("rafa"); len(o.Periods) != 2 || o.Periods[0].Months != 6 || o.Periods[0].Cents != 25000 ||
		o.Periods[0].PlanName != "Plano Semestral" || o.Periods[1].Months != 12 || o.Periods[1].Cents != 45000 {
		t.Fatalf("oferta do link do Rafa = %+v", o)
	}
	if o := offer("ana"); len(o.Periods) != 0 {
		t.Fatalf("oferta do link da Ana = %+v", o)
	}
	// No site, ligado nas opções do pedido (o link da Ana continua sem).
	options.Recurrence.Enabled = true
	env.must(admin, http.MethodPut, "/api/order-options", options, http.StatusOK)
	if o := offer(""); len(o.Periods) != 2 || o.Periods[0].Cents != 20000 || o.Periods[1].Cents != 40000 {
		t.Fatalf("oferta do site ligada = %+v", o)
	}
	if o := offer("ana"); len(o.Periods) != 0 {
		t.Fatalf("oferta da Ana com o site ligado = %+v", o)
	}

	body := func(email string, extra map[string]any) map[string]any {
		b := map[string]any{
			"name": "Caio Semestral", "email": email, "phone": "(11) 95555-0000", "document": "529.982.247-25",
			"password": "senha-forte-1", "vehicle": map[string]any{"kind": "CAR", "brand": "Fiat", "model": "Uno", "plate": ""},
			"address": map[string]any{"zipCode": "01305-000", "street": "Rua Augusta", "number": "500", "district": "Consolação",
				"city": "São Paulo", "state": "SP"},
			"contractVersion": offer("").ContractVersion, "accept": true, "ref": "rafa", "periodMonths": 6,
		}
		for k, v := range extra {
			b[k] = v
		}
		return b
	}
	// Pelo link da Ana, não.
	env.must("", http.MethodPost, "/api/public/checkout", body("caio@site.test", map[string]any{"ref": "ana"}), http.StatusBadRequest)

	var r struct {
		Subscription billing.Subscription `json:"subscription"`
		FirstInvoice *billing.Invoice     `json:"firstInvoice"`
	}
	_ = json.Unmarshal(env.must("", http.MethodPost, "/api/public/checkout", body("caio@site.test", nil), http.StatusCreated), &r)
	sub := r.Subscription
	if sub.PeriodMonths != 6 || sub.PriceCents != 25000 || sub.PlanName != "Plano Semestral" || sub.TrackerCents != 0 ||
		sub.EquipmentMode != billing.EquipmentSale || r.FirstInvoice == nil || r.FirstInvoice.AmountCents != 25000 ||
		r.FirstInvoice.PeriodMonths != 6 || r.FirstInvoice.Description != "Plano Semestral — novembro/2026 a abril/2027" {
		t.Fatalf("contratação semestral = %+v / %+v", sub, r.FirstInvoice)
	}

	// Paga: 6 meses de comissão, de novembro a abril.
	if _, err := env.db.Exec(ctx, `UPDATE invoices SET status = 'PAID', paid_at = NOW(), paid_via = 'MANUAL'
		WHERE customer_id = $1`, sub.CustomerID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := aff.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	var months []string
	var total int
	rows, err := env.db.Query(ctx, `SELECT to_char(month, 'YYYY-MM'), amount_cents FROM affiliate_commissions
		WHERE subscription_id = $1 ORDER BY month`, sub.ID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var m string
		var cents int
		_ = rows.Scan(&m, &cents)
		months, total = append(months, m), total+cents
	}
	rows.Close()
	if len(months) != 6 || months[0] != "2026-11" || months[5] != "2027-04" || total != 6*500 {
		t.Fatalf("comissões = %v (total %d)", months, total)
	}
	// Uma segunda passada não duplica nem apaga.
	if created, removed, err := aff.Reconcile(ctx); err != nil || created != 0 || removed != 0 {
		t.Fatalf("de novo: criadas %d, apagadas %d (%v)", created, removed, err)
	}

	// O MRR conta o semestral pelo valor dividido pelos meses.
	var mrr struct {
		CurrentCents int64 `json:"currentCents"`
	}
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/customers/mrr", nil, http.StatusOK), &mrr)
	if mrr.CurrentCents != 25000/6 {
		t.Errorf("MRR = %d", mrr.CurrentCents)
	}

	// A próxima: 6 meses depois, no dia da conta, com os meses que ela cobre.
	got, err := bs.GetSubscription(ctx, sub.ID)
	if err != nil || got.NextDueDate.String() != "2027-06-10" {
		t.Fatalf("próximo vencimento = %+v (%v)", got, err)
	}
	clock = time.Date(2027, 6, 5, 12, 0, 0, 0, time.UTC)
	bs.GenerateInvoices(ctx)
	var next billing.Invoice
	if err := env.db.QueryRow(ctx, `SELECT description, amount_cents, period_months FROM invoices
		WHERE subscription_id = $1 AND due_date = '2027-06-10'`, sub.ID).Scan(&next.Description, &next.AmountCents, &next.PeriodMonths); err != nil {
		t.Fatalf("fatura do 2º semestre: %v", err)
	}
	if next.Description != "Plano Semestral — junho/2027 a novembro/2027" || next.AmountCents != 25000 || next.PeriodMonths != 6 {
		t.Fatalf("fatura do 2º semestre = %+v", next)
	}

	// Cancelada com o período pago: ativa até o fim dele (a do 2º semestre,
	// ainda não paga, sai); não renova; de novo, recusado.
	var canceled billing.Subscription
	_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/subscriptions/"+sub.ID.String()+"/cancel", nil, http.StatusOK), &canceled)
	if canceled.Status != billing.SubscriptionActive || canceled.EndsOn == nil || canceled.EndsOn.String() != "2027-06-10" {
		t.Fatalf("cancelada = %+v", canceled)
	}
	var status string
	_ = env.db.QueryRow(ctx, `SELECT status FROM invoices WHERE subscription_id = $1 AND due_date = '2027-06-10'`, sub.ID).Scan(&status)
	if status != "CANCELED" {
		t.Errorf("fatura do 2º semestre depois do cancelamento = %s", status)
	}
	env.must(admin, http.MethodPost, "/api/subscriptions/"+sub.ID.String()+"/cancel", nil, http.StatusBadRequest)
	bs.GenerateInvoices(ctx)
	var open int
	_ = env.db.QueryRow(ctx, `SELECT count(*) FROM invoices WHERE subscription_id = $1 AND status = 'OPEN'`, sub.ID).Scan(&open)
	if open != 0 {
		t.Errorf("faturas novas depois do cancelamento = %d", open)
	}
	// No fim do período, encerra sozinha.
	clock = time.Date(2027, 6, 10, 12, 0, 0, 0, time.UTC)
	bs.GenerateInvoices(ctx)
	if got, err := bs.GetSubscription(ctx, sub.ID); err != nil || got.Status != billing.SubscriptionCanceled {
		t.Fatalf("no fim do período = %+v (%v)", got, err)
	}
}

// A comissão do semestral e do anual com regra própria: o valor fixo por
// semestre pago e o percentual do ano pago, cada um uma comissão só, no mês
// do pagamento; sem regra própria, a do mensal pelos meses (TestPeriodPlans).
// A página do representante mostra as regras. Precisa de
// FARBO_TEST_DATABASE_URL.
func TestPeriodCommissionRules(t *testing.T) {
	var bs *billing.Service
	var aff *affiliates.Service
	env, _ := newLeadsEnv(t, func(d *Deps) {
		bs, aff = d.Billing, d.Affiliates
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
	bs.SetClock(func() time.Time { return time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC) })
	admin := env.login(auth.RoleAdmin + "@leads.test")

	// Regra inválida, não.
	env.must(admin, http.MethodPost, "/api/affiliates", map[string]any{"name": "Errada", "code": "errada", "active": true,
		"semiannualCommission": map[string]any{"type": "percent", "bps": 0}}, http.StatusBadRequest)
	var bia affiliates.Affiliate
	_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/affiliates", map[string]any{
		"name": "Bia Representante", "code": "bia", "active": true, "commissionCents": 500, "reportPassword": "",
		"recurrence":           map[string]any{"enabled": true, "semiannualCents": 20000, "annualCents": 40000},
		"semiannualCommission": map[string]any{"type": "fixed", "cents": 3000},
		"annualCommission":     map[string]any{"type": "percent", "bps": 1000},
	}, http.StatusCreated), &bia)
	if bia.SemiannualCommission != (affiliates.PeriodCommission{Type: "fixed", Cents: 3000}) ||
		bia.AnnualCommission != (affiliates.PeriodCommission{Type: "percent", Bps: 1000}) {
		t.Fatalf("regras = %+v / %+v", bia.SemiannualCommission, bia.AnnualCommission)
	}
	// A edição sem os campos (telas antigas) não muda as regras.
	var edited affiliates.Affiliate
	_ = json.Unmarshal(env.must(admin, http.MethodPatch, "/api/affiliates/"+bia.ID.String(), map[string]any{
		"name": "Bia Representante", "code": "bia", "active": true}, http.StatusOK), &edited)
	if edited.SemiannualCommission.Type != "fixed" || edited.AnnualCommission.Type != "percent" {
		t.Fatalf("depois da edição = %+v / %+v", edited.SemiannualCommission, edited.AnnualCommission)
	}

	var version string
	var offer struct {
		ContractVersion string `json:"contractVersion"`
	}
	_ = json.Unmarshal(env.must("", http.MethodGet, "/api/public/checkout", nil, http.StatusOK), &offer)
	version = offer.ContractVersion
	checkout := func(email, document string, months int) billing.Subscription {
		t.Helper()
		var r struct {
			Subscription billing.Subscription `json:"subscription"`
		}
		_ = json.Unmarshal(env.must("", http.MethodPost, "/api/public/checkout", map[string]any{
			"name": "Cliente " + email, "email": email, "phone": "(11) 95555-0000", "document": document, "password": "senha-forte-1",
			"vehicle": map[string]any{"kind": "CAR", "brand": "Fiat", "model": "Uno", "plate": ""},
			"address": map[string]any{"zipCode": "01305-000", "street": "Rua Augusta", "number": "500", "district": "Consolação",
				"city": "São Paulo", "state": "SP"},
			"contractVersion": version, "accept": true, "ref": "bia", "periodMonths": months,
		}, http.StatusCreated), &r)
		return r.Subscription
	}
	semester := checkout("semestre@site.test", "529.982.247-25", 6)
	year := checkout("ano@site.test", "390.533.447-05", 12)
	if _, err := env.db.Exec(ctx, `UPDATE invoices SET status = 'PAID', paid_at = NOW(), paid_via = 'MANUAL'
		WHERE subscription_id = ANY($1)`, []any{semester.ID, year.ID}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := aff.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	commissions := func(sub billing.Subscription) map[string]int {
		t.Helper()
		out := map[string]int{}
		rows, err := env.db.Query(ctx, `SELECT to_char(month, 'YYYY-MM'), amount_cents FROM affiliate_commissions WHERE subscription_id = $1`, sub.ID)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		for rows.Next() {
			var m string
			var cents int
			_ = rows.Scan(&m, &cents)
			out[m] = cents
		}
		return out
	}
	if got := commissions(semester); len(got) != 1 || got["2026-11"] != 3000 {
		t.Errorf("comissão do semestral = %v", got)
	}
	if got := commissions(year); len(got) != 1 || got["2026-11"] != 4000 {
		t.Errorf("comissão do anual = %v", got)
	}
	// De novo: nada muda.
	if created, removed, err := aff.Reconcile(ctx); err != nil || created != 0 || removed != 0 {
		t.Fatalf("de novo: criadas %d, apagadas %d (%v)", created, removed, err)
	}

	// A página dela mostra as regras.
	var report struct {
		SemiannualCommission *affiliates.PeriodCommission `json:"semiannualCommission"`
		AnnualCommission     *affiliates.PeriodCommission `json:"annualCommission"`
	}
	_ = json.Unmarshal(env.must("", http.MethodGet, "/api/public/partner/"+bia.ReportToken, nil, http.StatusOK), &report)
	if report.SemiannualCommission == nil || report.SemiannualCommission.Cents != 3000 || report.AnnualCommission == nil ||
		report.AnnualCommission.Bps != 1000 {
		t.Errorf("página = %+v / %+v", report.SemiannualCommission, report.AnnualCommission)
	}
}
