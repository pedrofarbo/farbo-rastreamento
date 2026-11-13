package api

import (
	"context"
	"io"
	"log/slog"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
)

// A previsão do mês que vem: em 13/11, a de dezembro. A projeção do MRR conta
// toda assinatura ativa pelo valor de dezembro (a mensalidade com a parte do
// rastreador somada, a promoção que ainda vale, o reajuste; o semestral e o
// anual divididos pelos meses; também quem contratou depois do dia da conta e
// só paga a 2ª em janeiro), sem o semestral encerrado, a grátis e a
// contratação pelo site ainda não paga. E o "a cobrar" bate com o gerador:
// com o relógio no fim de dezembro, as faturas que vencem em dezembro somam o
// mesmo. Precisa de FARBO_TEST_DATABASE_URL.
func TestForecastMatchesGenerator(t *testing.T) {
	var bs *billing.Service
	env, _ := newLeadsEnv(t, func(d *Deps) { bs = d.Billing })
	ctx := context.Background()
	clock := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	bs.SetClock(func() time.Time { return clock })

	authSvc := auth.NewService(auth.NewRepository(env.db), config.Auth{BcryptCost: bcrypt.MinCost}, nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	customer := func(email string) uuid.UUID {
		t.Helper()
		u, err := authSvc.CreateUser(ctx, email, "Cliente", auth.RoleCustomer, userPassword)
		if err != nil {
			t.Fatal(err)
		}
		return u.ID
	}
	ana, beto := customer("ana@previsao.test"), customer("beto@previsao.test")
	pending := customer("pendente@previsao.test")
	if _, err := env.db.Exec(ctx, `UPDATE users SET checkout_at = NOW() WHERE id = $1`, pending); err != nil {
		t.Fatal(err)
	}
	sub := func(owner uuid.UUID, columns string, values ...any) uuid.UUID {
		t.Helper()
		var id uuid.UUID
		args := append([]any{owner}, values...)
		placeholders := ""
		for i := range values {
			placeholders += ", $" + strconv.Itoa(i+2)
		}
		if err := env.db.QueryRow(ctx, `INSERT INTO subscriptions (customer_id, plan_name, `+columns+`)
			VALUES ($1, 'Plano'`+placeholders+`) RETURNING id`, args...).Scan(&id); err != nil {
			t.Fatalf("assinatura (%s): %v", columns, err)
		}
		return id
	}
	// 1. A mensal do dia 10: R$ 69,90 em 10/12.
	sub(ana, "price_cents, due_day, next_due_date", 6990, 10, "2026-12-10")
	// 2. A do dia 25 com a parte do rastreador somada (R$ 12 até junho): a de
	// novembro ainda vai sair; a de dezembro, R$ 61,90.
	sub(ana, "price_cents, due_day, next_due_date, tracker_cents, tracker_months, tracker_until, tracker_added",
		4990, 25, "2026-11-25", 1200, 10, "2027-06-25", true)
	// 3. A promoção até 20/12: a de 05/12 ainda com ela (R$ 29,90).
	sub(ana, "price_cents, due_day, next_due_date, promo_price_cents, promo_until", 6990, 5, "2026-12-05", 2990, "2026-12-20")
	// 4. O reajuste a partir de 01/12: a de 15/12 já com ele (R$ 74,90).
	sub(beto, "price_cents, due_day, next_due_date, next_price_cents, next_price_from", 6990, 15, "2026-12-15", 7490, "2026-12-01")
	// 5. O semestral que vence em 10/12: R$ 250,00.
	sub(beto, "price_cents, due_day, next_due_date, period_months", 25000, 10, "2026-12-10", 6)
	// 6. O anual que vence em março: nada em dezembro.
	sub(beto, "price_cents, due_day, next_due_date, period_months", 45000, 10, "2027-03-10", 12)
	// 7. O semestral encerrado antes de dezembro: nada.
	sub(beto, "price_cents, due_day, next_due_date, period_months, ends_on", 25000, 10, "2026-12-10", 6, "2026-12-01")
	// 8. A grátis: nada.
	sub(beto, "price_cents, due_day, next_due_date", 0, 10, "2026-12-10")
	// 9. A do dia 2, com a de dezembro já gerada (R$ 69,90).
	early := sub(beto, "price_cents, due_day, next_due_date", 6990, 2, "2027-01-02")
	if _, err := env.db.Exec(ctx, `INSERT INTO invoices (customer_id, subscription_id, description, amount_cents, due_date)
		VALUES ($1, $2, 'Plano · dezembro', 6990, '2026-12-02')`, beto, early); err != nil {
		t.Fatal(err)
	}
	// 10. O rastreador comprado em parcelas: a de novembro leva a 1ª (R$ 10);
	// a de dezembro, a 2ª (R$ 9).
	parcels := sub(ana, "price_cents, due_day, next_due_date, installments, commitment_until", 3490, 28, "2026-11-28", 2, "2027-11-28")
	for n, cents := range []int{1000, 900} {
		if _, err := env.db.Exec(ctx, `INSERT INTO equipment_installments (subscription_id, number, amount_cents) VALUES ($1, $2, $3)`,
			parcels, n+1, cents); err != nil {
			t.Fatal(err)
		}
	}
	// 11. A contratação pelo site ainda não paga: fora da previsão.
	sub(pending, "price_cents, due_day, next_due_date", 6990, 10, "2026-12-10")
	// 12. Contratou hoje (13/11) e pagou a 1ª; a conta vence dia 10: a 2ª só
	// em 10/01. Em dezembro não há fatura, mas a assinatura conta na projeção.
	fresh := sub(beto, "price_cents, due_day, next_due_date", 5990, 10, "2027-01-10")
	if _, err := env.db.Exec(ctx, `INSERT INTO invoices (customer_id, subscription_id, description, amount_cents, due_date, status, paid_at)
		VALUES ($1, $2, 'Plano · novembro', 5990, '2026-11-13', 'PAID', NOW())`, beto, fresh); err != nil {
		t.Fatal(err)
	}

	forecast, err := bs.Forecast(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// A projeção: as 9 assinaturas (o semestral por 25000/6, o anual por 45000/12).
	wantRecurring := int64(6990 + 6190 + 2990 + 7490 + 25000/6 + 45000/12 + 6990 + 3490 + 5990)
	// O que vai ser cobrado: as faturas que vencem em dezembro (o semestral
	// pelo valor cheio; nada do anual nem de quem contratou hoje).
	wantBilled := int64(6990+6190+2990+7490+25000+6990+3490) + 900
	if forecast.Month != "2026-12" || forecast.MonthlyCents != wantRecurring || forecast.Monthlies != 9 || forecast.InstallmentsCents != 900 ||
		forecast.TotalCents != wantRecurring+900 || forecast.BilledCents != wantBilled {
		t.Fatalf("previsão de dezembro = %+v (projeção esperada %d, a cobrar %d)", forecast, wantRecurring, wantBilled)
	}

	// O gerador até o fim de dezembro: o que vence em dezembro soma a previsão.
	for _, day := range []time.Time{
		time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC), time.Date(2026, 12, 1, 12, 0, 0, 0, time.UTC),
		time.Date(2026, 12, 15, 12, 0, 0, 0, time.UTC), time.Date(2026, 12, 31, 12, 0, 0, 0, time.UTC),
	} {
		clock = day
		bs.GenerateInvoices(ctx)
	}
	var billed, parcelsBilled int64
	if err := env.db.QueryRow(ctx, `
		SELECT COALESCE(sum(i.amount_cents), 0),
			COALESCE((SELECT sum(e.amount_cents) FROM equipment_installments e JOIN invoices x ON x.id = e.invoice_id
				WHERE x.due_date >= '2026-12-01' AND x.due_date < '2027-01-01'), 0)
		FROM invoices i JOIN users u ON u.id = i.customer_id
		WHERE i.subscription_id IS NOT NULL AND i.status <> 'CANCELED' AND u.checkout_at IS NULL
			AND i.due_date >= '2026-12-01' AND i.due_date < '2027-01-01'`).Scan(&billed, &parcelsBilled); err != nil {
		t.Fatal(err)
	}
	if billed != forecast.BilledCents || parcelsBilled != forecast.InstallmentsCents {
		t.Errorf("cobrado em dezembro = %d (parcelas %d); a previsão do a cobrar era %d (parcelas %d)",
			billed, parcelsBilled, forecast.BilledCents, forecast.InstallmentsCents)
	}

	// No mês atual (novembro), o MRR também é pelas assinaturas: quem
	// contratou hoje conta, e a projeção de dezembro só cai pelo que muda de
	// preço (aqui, nada cai: a promoção ainda vale em 05/12).
	clock = time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	mrr, err := bs.MRR(ctx, 3)
	if err != nil {
		t.Fatal(err)
	}
	if cur := mrr.Months[0]; cur.Subscriptions != 9 || cur.MRRCents > forecast.MonthlyCents {
		t.Errorf("MRR de novembro = %+v; projeção de dezembro %d", cur, forecast.MonthlyCents)
	}
}
