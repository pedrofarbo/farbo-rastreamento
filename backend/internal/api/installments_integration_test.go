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
)

// O rastreador parcelado sem juros: no pedido, só o frete; as parcelas
// somadas às mensalidades, a 1ª na 1ª (a de uma fatura cancelada volta na
// seguinte), a assinatura presa até a última e, encerrada antes, o saldo
// numa fatura só — ou dispensado. Precisa de FARBO_TEST_DATABASE_URL.
func TestEquipmentInstallments(t *testing.T) {
	var bs *billing.Service
	env, _ := newLeadsEnv(t, func(d *Deps) { bs = d.Billing })
	ctx := context.Background()
	// 07/10/2026: a primeira mensalidade (dia 10) já sai com o pedido.
	bs.SetClock(func() time.Time { return time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC) })

	admin := env.login(auth.RoleAdmin + "@leads.test")
	var carla struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/customers", map[string]any{
		"name": "Carla Dias", "email": "carla@parcela.test", "phone": "(11) 97777-0000", "document": "",
		"password": userPassword,
	}, http.StatusCreated), &carla)
	token := env.login("carla@parcela.test")
	env.must(token, http.MethodPut, "/api/me/address", map[string]any{
		"zipCode": "01305-000", "street": "Rua Augusta", "number": "500", "district": "Consolação", "city": "São Paulo", "state": "SP",
	}, http.StatusOK)

	type result struct {
		Subscription billing.Subscription `json:"subscription"`
		SetupInvoice *billing.Invoice     `json:"setupInvoice"`
	}
	order := func(plate string, installments, want int) (result, []byte) {
		t.Helper()
		body := env.must(token, http.MethodPost, "/api/me/trackers", map[string]any{
			"vehicle": map[string]any{"name": "Moto " + plate, "plate": plate}, "installments": installments,
		}, want)
		var out result
		_ = json.Unmarshal(body, &out)
		return out, body
	}
	subscription := func(id string) billing.Subscription {
		t.Helper()
		var list []billing.Subscription
		_ = json.Unmarshal(env.must(token, http.MethodGet, "/api/me/subscriptions", nil, http.StatusOK), &list)
		for _, s := range list {
			if s.ID.String() == id {
				return s
			}
		}
		t.Fatalf("assinatura %s sumiu", id)
		return billing.Subscription{}
	}
	monthly := func(subID string) []billing.Invoice {
		t.Helper()
		rows, err := env.db.Query(ctx, `SELECT description, amount_cents, due_date, status FROM invoices
			WHERE subscription_id = $1 ORDER BY due_date`, subID)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []billing.Invoice
		for rows.Next() {
			var inv billing.Invoice
			if err := rows.Scan(&inv.Description, &inv.AmountCents, &inv.DueDate.Time, &inv.Status); err != nil {
				t.Fatal(err)
			}
			out = append(out, inv)
		}
		return out
	}

	// Acima do teto do catálogo (10x), recusado.
	if _, body := order("PAR1A11", 11, http.StatusBadRequest); !strings.Contains(string(body), "até 10 vezes") {
		t.Errorf("11x: %s", body)
	}

	// 10x de R$ 15,00, sem frete: o pedido não gera fatura; a mensalidade de
	// outubro leva a 1ª, e a permanência vai até a com a 10ª (julho/2027).
	r, _ := order("PAR1A11", 10, http.StatusCreated)
	sub := r.Subscription
	if r.SetupInvoice != nil {
		t.Fatalf("fatura do pedido = %+v", r.SetupInvoice)
	}
	if sub.Installments != 10 || sub.CommitmentUntil == nil || sub.CommitmentUntil.Format(time.DateOnly) != "2027-07-10" {
		t.Fatalf("assinatura = %+v", sub)
	}
	invoices := monthly(sub.ID.String())
	if len(invoices) != 1 || invoices[0].AmountCents != 6990+1500 ||
		invoices[0].Description != "Plano Mensal — outubro/2026 + parcela 1/10 do rastreador" {
		t.Fatalf("mensalidades = %+v", invoices)
	}
	if s := subscription(sub.ID.String()); s.EquipmentCents != 15000 || s.InstallmentCents != 1500 || s.InstallmentsPaid != 0 ||
		s.InstallmentsDueCents != 15000 {
		t.Errorf("andamento = %+v", s)
	}

	// Paga a de outubro: 1 de 10.
	var october string
	_ = env.db.QueryRow(ctx, `SELECT id FROM invoices WHERE subscription_id = $1`, sub.ID).Scan(&october)
	env.must(admin, http.MethodPost, "/api/invoices/"+october+"/pay", nil, http.StatusOK)
	if s := subscription(sub.ID.String()); s.InstallmentsPaid != 1 || s.InstallmentsDueCents != 13500 {
		t.Errorf("depois da 1ª = %+v", s)
	}

	// A central cancela a mensalidade de novembro: a 2ª parcela vai na de
	// dezembro.
	repo := billing.NewRepository(env.db)
	if _, err := repo.GenerateDue(ctx, billing.NewDate(2026, 11, 10)); err != nil {
		t.Fatal(err)
	}
	invoices = monthly(sub.ID.String())
	if len(invoices) != 2 || invoices[1].AmountCents != 8490 || !strings.HasSuffix(invoices[1].Description, "+ parcela 2/10 do rastreador") {
		t.Fatalf("mensalidade de novembro = %+v", invoices)
	}
	var november string
	_ = env.db.QueryRow(ctx, `SELECT id FROM invoices WHERE subscription_id = $1 AND due_date = '2026-11-10'`, sub.ID).Scan(&november)
	env.must(admin, http.MethodPost, "/api/invoices/"+november+"/cancel", nil, http.StatusOK)
	if _, err := repo.GenerateDue(ctx, billing.NewDate(2026, 12, 10)); err != nil {
		t.Fatal(err)
	}
	invoices = monthly(sub.ID.String())
	if len(invoices) != 3 || invoices[2].AmountCents != 8490 || !strings.HasSuffix(invoices[2].Description, "+ parcela 2/10 do rastreador") {
		t.Fatalf("mensalidades depois do cancelamento = %+v", invoices)
	}

	// Encerrar antes da última parcela: tem de dizer o que fazer com o saldo.
	cancel := func(id string, body any, want int) []byte {
		t.Helper()
		return env.must(admin, http.MethodPost, "/api/subscriptions/"+id+"/cancel", body, want)
	}
	if out := cancel(sub.ID.String(), nil, http.StatusConflict); !strings.Contains(string(out), "EQUIPMENT_BALANCE") {
		t.Errorf("sem a escolha: %s", out)
	}
	cancel(sub.ID.String(), map[string]string{"equipmentBalance": "TALVEZ"}, http.StatusBadRequest)
	if s := subscription(sub.ID.String()); s.Status != billing.SubscriptionActive {
		t.Fatalf("a recusa encerrou a assinatura: %+v", s)
	}

	// Cobrar: a mensalidade de dezembro (ainda não vencida) é cancelada e a
	// parcela dela volta; as 9 que faltam vencem numa fatura só, em 3 dias.
	var canceled struct {
		billing.Subscription
		BalanceInvoice *billing.Invoice `json:"balanceInvoice"`
	}
	_ = json.Unmarshal(cancel(sub.ID.String(), map[string]string{"equipmentBalance": "CHARGE"}, http.StatusOK), &canceled)
	balance := canceled.BalanceInvoice
	if canceled.Status != billing.SubscriptionCanceled || balance == nil || balance.AmountCents != 13500 ||
		balance.DueDate.Format(time.DateOnly) != "2026-10-10" || balance.SubscriptionID != nil ||
		balance.Description != "Saldo do rastreador: parcelas 2 a 10 de 10 (assinatura encerrada antes da última parcela)" {
		t.Fatalf("encerrada com o saldo = %+v / %+v", canceled.Subscription, balance)
	}
	if s := subscription(sub.ID.String()); s.InstallmentsPaid != 1 || s.InstallmentsDueCents != 13500 {
		t.Errorf("depois do encerramento = %+v", s)
	}
	var linked int
	_ = env.db.QueryRow(ctx, `SELECT count(*) FROM equipment_installments WHERE invoice_id = $1`, balance.ID).Scan(&linked)
	if linked != 9 {
		t.Errorf("parcelas na fatura do saldo = %d", linked)
	}

	// Dispensar (arrependimento): pedido em 3x de R$ 120,00 pela central
	// (40,00 cada); nada é cobrado — a mensalidade de outubro, com a 1ª,
	// ainda não venceu e é cancelada junto.
	adminOrder := func(plate string, equipment, installments, want int) (result, []byte) {
		t.Helper()
		body := env.must(admin, http.MethodPost, "/api/customers/"+carla.ID+"/trackers", map[string]any{
			"vehicle":        map[string]any{"name": "Carro " + plate, "plate": plate},
			"equipmentCents": equipment, "installments": installments,
			"plan": map[string]any{"planName": "Plano Mensal", "priceCents": 6990, "dueDay": 10},
		}, want)
		var out result
		_ = json.Unmarshal(body, &out)
		return out, body
	}
	if _, body := adminOrder("PAR0Z00", 0, 3, http.StatusBadRequest); !strings.Contains(string(body), "não há valor de equipamento") {
		t.Errorf("parcelar sem equipamento: %s", body)
	}
	r, _ = adminOrder("PAR2B22", 12000, 3, http.StatusCreated)
	if r.SetupInvoice != nil || r.Subscription.CommitmentUntil.Format(time.DateOnly) != "2026-12-10" {
		t.Fatalf("3x = %+v / %+v", r.SetupInvoice, r.Subscription)
	}
	if invoices := monthly(r.Subscription.ID.String()); len(invoices) != 1 || invoices[0].AmountCents != 6990+4000 {
		t.Fatalf("3x: mensalidades = %+v", invoices)
	}
	_ = json.Unmarshal(cancel(r.Subscription.ID.String(), map[string]string{"equipmentBalance": "WAIVE"}, http.StatusOK), &canceled)
	if canceled.BalanceInvoice != nil {
		t.Errorf("dispensado gerou fatura: %+v", canceled.BalanceInvoice)
	}
	if s := subscription(r.Subscription.ID.String()); s.InstallmentsDueCents != 0 {
		t.Errorf("dispensado: nada a pagar, %+v", s)
	}

	// Tudo pago: encerra sem perguntar. 7x de R$ 120,00 (17,16 + 6 × 17,14)
	// pela central, com a assinatura começando já em outubro.
	r, _ = adminOrder("PAR3C33", 12000, 7, http.StatusCreated)
	if invoices := monthly(r.Subscription.ID.String()); len(invoices) != 1 || invoices[0].AmountCents != 6990+1716 {
		t.Fatalf("7x: 1ª = %+v", invoices)
	}
	if _, err := billing.NewRepository(env.db).GenerateDue(ctx, billing.NewDate(2027, 4, 10)); err != nil {
		t.Fatal(err)
	}
	if _, err := env.db.Exec(ctx, `UPDATE invoices SET status = 'PAID', paid_at = NOW(), paid_via = 'MANUAL'
		WHERE customer_id = $1 AND status = 'OPEN'`, carla.ID); err != nil {
		t.Fatal(err)
	}
	if s := subscription(r.Subscription.ID.String()); s.InstallmentsPaid != 7 || s.InstallmentsDueCents != 0 || s.EquipmentCents != 12000 {
		t.Errorf("quitado = %+v", s)
	}
	_ = json.Unmarshal(cancel(r.Subscription.ID.String(), nil, http.StatusOK), &canceled)
	if canceled.Status != billing.SubscriptionCanceled || canceled.BalanceInvoice != nil {
		t.Errorf("encerrar quitado = %+v", canceled)
	}
}
