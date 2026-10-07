package api

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/finance"
)

// O caixa bate com a AbacatePay. O caso real de 06/10/2026: um cliente
// pagou R$ 5,00 por Pix (a AbacatePay ficou com R$ 0,80) e um Pix de R$ 1,00
// a um fornecedor chegou como R$ 0,20 (R$ 0,80 de tarifa). No extrato dela:
// entradas R$ 5,00, saídas R$ 0,20, taxas R$ 1,60, variação R$ 3,20. A
// tarifa do Pix recebido é lançada uma vez só, e o Pix de testes não tem
// tarifa. Precisa de FARBO_TEST_DATABASE_URL.
func TestCashMatchesAbacatePay(t *testing.T) {
	var fs *finance.Service
	env, _ := newLeadsEnv(t, func(d *Deps) { fs = d.Finance })
	ctx := context.Background()
	loc, _ := time.LoadLocation("America/Sao_Paulo")
	fs.SetClock(func() time.Time { return time.Date(2026, 10, 7, 12, 0, 0, 0, loc) })
	if _, err := fs.SaveSettings(ctx, finance.Settings{OpeningDate: billing.NewDate(2026, 10, 5)}); err != nil {
		t.Fatal(err)
	}

	authSvc := auth.NewService(auth.NewRepository(env.db), config.Auth{BcryptCost: 4}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	pedro, err := authSvc.CreateUser(ctx, "pedro@caixa.test", "Pedro", auth.RoleCustomer, userPassword)
	if err != nil {
		t.Fatal(err)
	}
	// A venda: fatura de R$ 5,00 paga por Pix às 14h46 de 06/10 (a
	// AbacatePay não informou a tarifa ao gerar: vale a tabela, R$ 0,80).
	charge := func(invoice string, cents int, devMode bool) uuid.UUID {
		t.Helper()
		var inv, ch uuid.UUID
		paid := time.Date(2026, 10, 6, 14, 46, 0, 0, loc)
		// A do sandbox fica em aberto: só serve para ver que não gera tarifa.
		status, paidAt := "PAID", &paid
		if devMode {
			status, paidAt = "OPEN", nil
		}
		if err := env.db.QueryRow(ctx, `INSERT INTO invoices (customer_id, description, amount_cents, due_date, status, paid_at, paid_via)
			VALUES ($1, $2, $3, '2026-10-06', $4, $5, 'PIX') RETURNING id`, pedro.ID, invoice, cents, status, paidAt).Scan(&inv); err != nil {
			t.Fatal(err)
		}
		if err := env.db.QueryRow(ctx, `INSERT INTO payment_charges (invoice_id, provider, provider_charge_id, amount_cents, status, dev_mode, paid_at)
			VALUES ($1, 'abacatepay', $2, $3, 'PAID', $4, $5) RETURNING id`, inv, "pix_"+uuid.NewString()[:8], cents, devMode, paid).Scan(&ch); err != nil {
			t.Fatal(err)
		}
		return ch
	}
	sale := charge("Teste", 500, false)
	charge("Teste do sandbox", 700, true)

	// O Pix ao fornecedor: chegaram R$ 0,20, e a tarifa de R$ 0,80.
	var categories []finance.Category
	categories, err = fs.Categories(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]uuid.UUID{}
	for _, c := range categories {
		ids[c.Name] = c.ID
	}
	for _, e := range []struct {
		description string
		category    string
		cents       int64
	}{{"Teste", "Outras despesas", 20}, {"Tarifa do Pix (AbacatePay): Teste", "Taxas de pagamento", 80}} {
		paidOn := billing.NewDate(2026, 10, 6)
		if _, err := fs.CreateEntries(ctx, finance.EntryInput{
			Kind: finance.KindPayable, Description: e.description, CategoryID: ids[e.category], AmountCents: e.cents,
			DueDate: paidOn, PaidOn: &paidOn, PaymentMethod: "PIX",
		}, nil); err != nil {
			t.Fatal(err)
		}
	}

	// A tarifa do Pix recebido entra uma vez, no dia do pagamento; a do
	// sandbox, não.
	fs.RecordReceivedFees(ctx)
	fs.RecordReceivedFees(ctx)
	var fees int
	var day time.Time
	var cents int64
	if err := env.db.QueryRow(ctx, `SELECT COUNT(*), MIN(paid_on), MIN(paid_cents) FROM finance_entries
		WHERE description LIKE 'Tarifa do Pix recebido%'`).Scan(&fees, &day, &cents); err != nil || fees != 1 ||
		day.Format(time.DateOnly) != "2026-10-06" || cents != 80 {
		t.Fatalf("tarifas do Pix recebido = %d %v %d (%v)", fees, day, cents, err)
	}
	var linked bool
	_ = env.db.QueryRow(ctx, `SELECT fee_entry_id IS NOT NULL FROM payment_charges WHERE id = $1`, sale).Scan(&linked)
	if !linked {
		t.Error("a cobrança não ficou ligada à tarifa")
	}

	// O caixa de outubro: o mesmo resumo da AbacatePay.
	flow, err := fs.CashFlow(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	m := flow.Months[len(flow.Months)-1]
	if m.Month != "2026-10" || m.InCents != 500 || m.OutCents-m.FeesCents != 20 || m.FeesCents != 160 ||
		m.NetCents != 320 || flow.BalanceCents != 320 {
		t.Errorf("caixa de outubro = %+v, saldo %d", m, flow.BalanceCents)
	}
	if balance, err := fs.Balance(ctx); err != nil || balance != 320 {
		t.Errorf("saldo = %d (%v)", balance, err)
	}
}
