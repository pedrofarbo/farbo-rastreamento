package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/adjustment"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/contract"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/mail"
)

// fakeIPCA devolve o IPCA de cada ano pedido (o mês final é maio do ano).
type fakeIPCA struct {
	mu    sync.Mutex
	years map[int][]float64
	asked []string
}

func (f *fakeIPCA) MonthlyIPCA(_ context.Context, first, last billing.Date) ([]float64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, first.Format("2006-01")+".."+last.Format("2006-01"))
	months, ok := f.years[last.Year()]
	if !ok {
		return nil, fmt.Errorf("%w: falta %s", adjustment.ErrNotPublished, last.Format("01/2006"))
	}
	return months, nil
}

// fakeAdjustmentMailer guarda os avisos.
type fakeAdjustmentMailer struct {
	mu        sync.Mutex
	notices   map[string][]mail.PriceAdjustmentNotice
	canceled  map[string]int
	summaries []mail.PriceAdjustmentSummary
}

func (m *fakeAdjustmentMailer) Notice(_ context.Context, to, _ string, n mail.PriceAdjustmentNotice) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.notices[to] = append(m.notices[to], n)
	return nil
}

func (m *fakeAdjustmentMailer) Canceled(_ context.Context, to, _ string, _ mail.PriceAdjustmentNotice) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.canceled[to]++
	return nil
}

func (m *fakeAdjustmentMailer) Summary(_ context.Context, _ string, s mail.PriceAdjustmentSummary) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.summaries = append(m.summaries, s)
	return nil
}

// twelve é um ano de IPCA mensal igual.
func twelve(v float64) []float64 {
	out := make([]float64, 12)
	for i := range out {
		out[i] = v
	}
	return out
}

// O reajuste anual de ponta a ponta, ano a ano: só quem tem 12 meses, fora
// da promoção e com o contrato que prevê o reajuste; um e-mail por cliente;
// o preço novo nas faturas a partir de agosto (geradas antes); o veto (e o
// prazo dele); índice negativo; aviso atrasado; e o aviso do contrato novo a
// quem aceitou o anterior. Precisa de FARBO_TEST_DATABASE_URL.
func TestPriceAdjustmentEndToEnd(t *testing.T) {
	loc, _ := time.LoadLocation("America/Sao_Paulo")
	now := time.Date(2027, 6, 30, 10, 0, 0, 0, loc)
	var mu sync.Mutex
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	at := func(t time.Time) { mu.Lock(); now = t; mu.Unlock() }
	source := &fakeIPCA{years: map[int][]float64{}}
	mailer := &fakeAdjustmentMailer{notices: map[string][]mail.PriceAdjustmentNotice{}, canceled: map[string]int{}}
	var svc *adjustment.Service
	env, _ := newLeadsEnv(t, func(d *Deps) {
		svc = adjustment.NewService(d.DB, source, mailer, adjustment.Config{
			Enabled: true, Location: loc, InvoiceLeadDays: 10, FromHour: 9, ToHour: 20,
		}, slog.New(slog.NewTextHandler(io.Discard, nil)))
		svc.SetClock(clock)
		svc.SetSync()
		d.Adjustment = svc
	})
	ctx := context.Background()
	admin := env.login(auth.RoleAdmin + "@leads.test")
	authSvc := auth.NewService(auth.NewRepository(env.db), config.Auth{BcryptCost: 4}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	customer := func(email string, contractVersions ...string) uuid.UUID {
		t.Helper()
		u, err := authSvc.CreateUser(ctx, email, strings.Split(email, "@")[0], auth.RoleCustomer, userPassword)
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range contractVersions {
			if _, err := env.db.Exec(ctx, `INSERT INTO contract_acceptances (user_id, version, content_sha256, name, document)
				VALUES ($1, $2, 'x', 'x', '52998224725')`, u.ID, v); err != nil {
				t.Fatal(err)
			}
		}
		return u.ID
	}
	subscription := func(customerID uuid.UUID, created, nextDue string, price int, promoUntil string) uuid.UUID {
		t.Helper()
		var id uuid.UUID
		var promo any
		if promoUntil != "" {
			promo = promoUntil
		}
		if err := env.db.QueryRow(ctx, `
			INSERT INTO subscriptions (customer_id, plan_name, price_cents, due_day, next_due_date, created_at,
				promo_price_cents, promo_until)
			VALUES ($1, 'Plano Mensal', $2, 25, $3, $4, CASE WHEN $5::date IS NULL THEN NULL ELSE 3490 END, $5::date)
			RETURNING id`, customerID, price, nextDue, created+" 12:00:00-03", promo).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	priceOf := func(id uuid.UUID) (price int, next *int, from *time.Time) {
		t.Helper()
		if err := env.db.QueryRow(ctx, `SELECT price_cents, next_price_cents, next_price_from FROM subscriptions WHERE id = $1`, id).
			Scan(&price, &next, &from); err != nil {
			t.Fatal(err)
		}
		return
	}

	// Ana: duas mensalidades antigas e o contrato novo (as duas reajustam, num e-mail só).
	ana := customer("ana@reajuste.test", "1", "2")
	anaCar := subscription(ana, "2026-06-15", "2027-07-25", 6990, "")
	anaMoto := subscription(ana, "2026-07-20", "2027-07-25", 4990, "")
	// Bia: contratou em março de 2027 (menos de 12 meses em agosto de 2027).
	bia := customer("bia@reajuste.test", "2")
	biaSub := subscription(bia, "2027-03-01", "2027-07-25", 6990, "")
	// Caio: antiga, mas só aceitou o contrato sem a cláusula.
	caio := customer("caio@reajuste.test", "1")
	caioSub := subscription(caio, "2026-01-10", "2027-07-25", 6990, "")
	// Duda: antiga, com a promoção ainda valendo em agosto de 2027.
	duda := customer("duda@reajuste.test", "2")
	dudaSub := subscription(duda, "2026-08-01", "2027-07-25", 6990, "2027-09-01")
	// Edu: cancelada.
	edu := customer("edu@reajuste.test", "2")
	eduSub := subscription(edu, "2026-01-10", "2027-07-25", 6990, "")
	if _, err := env.db.Exec(ctx, `UPDATE subscriptions SET status = 'CANCELED', canceled_at = NOW() WHERE id = $1`, eduSub); err != nil {
		t.Fatal(err)
	}

	// Antes de julho, nada; em julho antes das 9h, nada; sem o IPCA de maio, espera.
	svc.Work(ctx)
	at(time.Date(2027, 7, 1, 8, 0, 0, 0, loc))
	svc.Work(ctx)
	at(time.Date(2027, 7, 1, 10, 0, 0, 0, loc))
	svc.Work(ctx)
	var count int
	_ = env.db.QueryRow(ctx, `SELECT COUNT(*) FROM price_adjustments`).Scan(&count)
	if count != 0 || len(source.asked) != 1 || source.asked[0] != "2026-06..2027-05" {
		t.Fatalf("antes do IPCA: %d reajustes, pedidos %v", count, source.asked)
	}

	// 2027: IPCA de 0,4% ao mês (4,91% no ano).
	source.years[2027] = twelve(0.4)
	svc.Work(ctx)
	svc.Work(ctx) // de novo: não repete
	if p, next, from := priceOf(anaCar); p != 6990 || next == nil || *next != 7333 || from.Format(time.DateOnly) != "2027-08-01" {
		t.Fatalf("carro da Ana = %d %v %v", p, next, from)
	}
	for name, id := range map[string]uuid.UUID{"Bia": biaSub, "Caio": caioSub, "Duda": dudaSub, "Edu": eduSub} {
		if _, next, _ := priceOf(id); next != nil {
			t.Errorf("%s reajustada: %d", name, *next)
		}
	}
	if n := mailer.notices["ana@reajuste.test"]; len(n) != 1 || len(n[0].Lines) != 2 || n[0].Rate != "4,91%" ||
		n[0].Period != "junho de 2026 a maio de 2027" || n[0].EffectiveFrom.Format(time.DateOnly) != "2027-08-01" {
		t.Fatalf("aviso da Ana = %+v", n)
	}
	if len(mailer.notices) != 1 || len(mailer.summaries) != 1 || mailer.summaries[0].Customers != 1 ||
		mailer.summaries[0].Subscriptions != 2 || mailer.summaries[0].MonthlyDiffCents != (7333-6990)+(5235-4990) ||
		mailer.summaries[0].CancelUntil.Format(time.DateOnly) != "2027-07-21" {
		t.Fatalf("avisos = %v, resumos = %+v", mailer.notices, mailer.summaries)
	}

	// A tela do admin: o reajuste do ano, com as mensalidades; o próximo é 2028.
	var overview adjustment.Overview
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/price-adjustments", nil, http.StatusOK), &overview)
	if len(overview.Adjustments) != 1 || overview.Adjustments[0].Rate != "4,91%" || !overview.Adjustments[0].CanCancel ||
		len(overview.Adjustments[0].Items) != 2 || overview.Upcoming == nil || overview.Upcoming.Year != 2028 {
		t.Fatalf("visão = %+v", overview)
	}

	// O veto (antes de as faturas de agosto saírem): preços de volta e aviso.
	at(time.Date(2027, 7, 2, 10, 0, 0, 0, loc))
	env.must(admin, http.MethodPost, "/api/price-adjustments/2027/cancel", nil, http.StatusNoContent)
	if _, next, _ := priceOf(anaCar); next != nil || mailer.canceled["ana@reajuste.test"] != 1 {
		t.Errorf("depois do veto: %v, avisos %v", next, mailer.canceled)
	}
	env.must(admin, http.MethodPost, "/api/price-adjustments/2027/cancel", nil, http.StatusConflict)
	svc.Work(ctx) // o ano já foi decidido: não volta
	if _, next, _ := priceOf(anaCar); next != nil {
		t.Errorf("reajuste vetado voltou")
	}

	// 2028: agora a Bia tem 12 meses e a promoção da Duda acabou; o Caio
	// continua no contrato antigo.
	source.years[2028] = twelve(0.3)
	if _, err := env.db.Exec(ctx, `UPDATE subscriptions SET next_due_date = '2028-07-25' WHERE status = 'ACTIVE'`); err != nil {
		t.Fatal(err)
	}
	at(time.Date(2028, 7, 1, 9, 30, 0, 0, loc))
	svc.Work(ctx)
	for name, c := range map[string]struct {
		id   uuid.UUID
		want int
	}{"Ana": {anaCar, 7246}, "Bia": {biaSub, 7246}, "Duda": {dudaSub, 7246}, "Caio": {caioSub, 0}} {
		_, next, _ := priceOf(c.id)
		if (c.want == 0) != (next == nil) || (next != nil && *next != c.want) {
			t.Errorf("2028, %s = %v", name, next)
		}
	}
	// As faturas: a de julho no preço antigo; a de agosto (gerada em julho) no novo.
	if _, err := billing.NewRepository(env.db).GenerateDue(ctx, billing.NewDate(2028, 8, 25)); err != nil {
		t.Fatal(err)
	}
	var july, august int
	_ = env.db.QueryRow(ctx, `SELECT amount_cents FROM invoices WHERE subscription_id = $1 AND due_date = '2028-07-25'`, anaCar).Scan(&july)
	_ = env.db.QueryRow(ctx, `SELECT amount_cents FROM invoices WHERE subscription_id = $1 AND due_date = '2028-08-25'`, anaCar).Scan(&august)
	if july != 6990 || august != 7246 {
		t.Errorf("faturas: julho %d, agosto %d", july, august)
	}
	// Depois de 21/07 não dá mais para vetar.
	at(time.Date(2028, 7, 22, 10, 0, 0, 0, loc))
	if _, body := env.do(admin, http.MethodPost, "/api/price-adjustments/2028/cancel", nil); !strings.Contains(string(body), "21/07/2028") {
		t.Errorf("veto fora do prazo = %s", body)
	}
	// Em agosto, o preço novo vira o da assinatura.
	at(time.Date(2028, 8, 1, 3, 0, 0, 0, loc))
	svc.Work(ctx)
	if p, next, _ := priceOf(anaCar); p != 7246 || next != nil {
		t.Errorf("aplicado: %d %v", p, next)
	}
	var status string
	_ = env.db.QueryRow(ctx, `SELECT status FROM price_adjustments WHERE year = 2028`).Scan(&status)
	if status != adjustment.StatusApplied {
		t.Errorf("situação de 2028 = %s", status)
	}

	// 2029: IPCA negativo, a mensalidade fica.
	source.years[2029] = twelve(-0.1)
	at(time.Date(2029, 7, 1, 10, 0, 0, 0, loc))
	svc.Work(ctx)
	if p, next, _ := priceOf(anaCar); p != 7246 || next != nil {
		t.Errorf("2029 com deflação: %d %v", p, next)
	}
	if last := mailer.summaries[len(mailer.summaries)-1]; !last.NoChange || last.Year != 2029 {
		t.Errorf("resumo de 2029 = %+v", last)
	}

	// 2030: o IPCA de maio só saiu em 20/07; o reajuste vale 30 dias depois.
	at(time.Date(2030, 7, 20, 10, 0, 0, 0, loc))
	source.years[2030] = twelve(0.2)
	svc.Work(ctx)
	if _, next, from := priceOf(anaCar); next == nil || from.Format(time.DateOnly) != "2030-08-19" {
		t.Errorf("aviso atrasado = %v %v", next, from)
	}

	// 2031: sem IPCA até setembro, o ano fica sem reajuste.
	source.years[2031] = twelve(0.5)
	at(time.Date(2031, 9, 1, 10, 0, 0, 0, loc))
	svc.Work(ctx)
	_ = env.db.QueryRow(ctx, `SELECT COUNT(*) FROM price_adjustments WHERE year = 2031`).Scan(&count)
	if count != 0 {
		t.Errorf("reajuste fora de época")
	}

	// O contrato novo: quem aceitou só o anterior recebe o aviso, uma vez.
	doc, err := contract.Render(contract.Params{Company: config.Company{Name: "Farbo"}, SuspendAfterDays: 10, HistoryOptions: []int{7, 14, 30}})
	if err != nil {
		t.Fatal(err)
	}
	contractMailer := &recordingContractMailer{}
	contractSvc := contract.NewService(env.db, doc, contractMailer, slog.New(slog.NewTextHandler(io.Discard, nil)))
	contractSvc.NotifyChanges(ctx)
	contractSvc.NotifyChanges(ctx)
	if len(contractMailer.updates) != 1 || contractMailer.updates[0] != "caio@reajuste.test:"+contract.Version {
		t.Errorf("avisos do contrato novo = %v", contractMailer.updates)
	}
	_ = caio
	_ = anaMoto
	_ = edu
}
