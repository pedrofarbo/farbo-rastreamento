package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/dunning"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/payments"
)

// A fatura do mês de uma frota num Pix só: as faturas de cada veículo que
// vencem no mesmo dia saem num Pix pelo total (pelo app ou pelo link do
// lembrete, sem login), que quita todas de uma vez; se alguma mudou depois
// do Pix, nada é quitado sozinho; o lembrete leva o "pagar todas". Precisa
// de FARBO_TEST_DATABASE_URL.
func TestGroupedInvoicePayment(t *testing.T) {
	mailer := &recordingInvoiceMailer{}
	var svc *dunning.Service
	clock := time.Date(2026, 10, 10, 10, 0, 0, 0, time.UTC)
	env, _ := newLeadsEnv(t, func(d *Deps) {
		d.Config.Payments = config.Payments{AbacatePayAPIKey: "abc_dev_teste", PixExpiresIn: 24 * time.Hour}
		d.Billing.SetClock(func() time.Time { return clock })
		d.Payments = payments.NewService(payments.NewRepository(d.DB), d.Billing, &fakePixGateway{}, d.Audit, d.Config.Payments, d.Log)
		svc = dunning.NewService(d.DB, d.Billing, mailer, dunning.Config{
			SuspendAfterDays: 10, Location: time.UTC, FromHour: 9, ToHour: 20,
			AppURL: "https://painel.farbo.test", LinkSecret: d.Config.Auth.JWTSecret,
		}, d.Log)
		svc.SetClock(func() time.Time { return clock })
		d.Dunning = svc
	})
	ctx := context.Background()
	authSvc := auth.NewService(auth.NewRepository(env.db), config.Auth{BcryptCost: bcrypt.MinCost}, nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	frota, err := authSvc.CreateUser(ctx, "frota@grupo.test", "Frota Ltda", auth.RoleCustomer, userPassword)
	if err != nil {
		t.Fatal(err)
	}
	outro, err := authSvc.CreateUser(ctx, "outro@grupo.test", "Outro", auth.RoleCustomer, userPassword)
	if err != nil {
		t.Fatal(err)
	}
	admin := env.login(auth.RoleAdmin + "@leads.test")
	customer := env.login("frota@grupo.test")
	invoice := func(owner *auth.User, desc string, cents int, due billing.Date) uuid.UUID {
		t.Helper()
		var out struct {
			ID uuid.UUID `json:"id"`
		}
		body := env.must(admin, http.MethodPost, "/api/customers/"+owner.ID.String()+"/invoices",
			map[string]any{"description": desc, "amountCents": cents, "dueDate": due.String()}, http.StatusCreated)
		if err := json.Unmarshal(body, &out); err != nil {
			t.Fatal(err)
		}
		return out.ID
	}
	status := func(id uuid.UUID) (string, *uuid.UUID) {
		var s string
		var paidBy *uuid.UUID
		if err := env.db.QueryRow(ctx, `SELECT status, paid_charge_id FROM invoices WHERE id = $1`, id).Scan(&s, &paidBy); err != nil {
			t.Fatal(err)
		}
		return s, paidBy
	}
	type charge struct {
		ID            uuid.UUID   `json:"id"`
		AmountCents   int         `json:"amountCents"`
		InvoiceIDs    []uuid.UUID `json:"invoiceIds"`
		InvoiceStatus string      `json:"invoiceStatus"`
	}
	pix := func(ids []uuid.UUID, want int) charge {
		t.Helper()
		var c charge
		_ = json.Unmarshal(env.must(customer, http.MethodPost, "/api/me/invoices/pix", map[string]any{"invoiceIds": ids}, want), &c)
		return c
	}

	// Outubro da frota: três veículos no dia 13, e um avulso noutro dia.
	due := billing.NewDate(2026, 10, 13)
	van := invoice(frota, "Plano Frota — outubro/2026 (Van 01)", 4990, due)
	carro := invoice(frota, "Plano Frota — outubro/2026 (Carro 02)", 4990, due)
	moto := invoice(frota, "Plano Frota — outubro/2026 (Moto 03)", 3990, due)
	frete := invoice(frota, "Frete do rastreador", 2240, billing.NewDate(2026, 10, 20))
	alheia := invoice(outro, "Plano Mensal — outubro/2026", 6990, due)
	month := []uuid.UUID{van, carro, moto}

	// Um Pix só, pelo total; pedir de novo devolve o mesmo.
	c := pix(month, http.StatusOK)
	if c.AmountCents != 4990+4990+3990 || len(c.InvoiceIDs) != 3 || c.InvoiceIDs[0] != van {
		t.Fatalf("Pix do mês = %+v", c)
	}
	if again := pix([]uuid.UUID{moto, van, carro, van}, http.StatusOK); again.ID != c.ID {
		t.Errorf("não reaproveitou: %v ≠ %v", again.ID, c.ID)
	}
	// Recusas: nenhuma, fatura de outro cliente.
	pix(nil, http.StatusBadRequest)
	pix([]uuid.UUID{van, alheia}, http.StatusNotFound)

	// O link do lembrete de qualquer uma do dia oferece o mês inteiro.
	var detail struct {
		PaymentLinks map[string]string `json:"paymentLinks"`
	}
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/customers/"+frota.ID.String(), nil, http.StatusOK), &detail)
	token := func(id uuid.UUID) string {
		return strings.TrimPrefix(detail.PaymentLinks[id.String()], "https://painel.farbo.test/pagar/")
	}
	var pv struct {
		AmountCents int `json:"amountCents"`
		Group       *struct {
			AmountCents int `json:"amountCents"`
			Items       []struct {
				Description string `json:"description"`
			} `json:"items"`
		} `json:"group"`
	}
	_ = json.Unmarshal(env.must("", http.MethodGet, "/api/public/invoices/"+token(carro), nil, http.StatusOK), &pv)
	if pv.AmountCents != 4990 || pv.Group == nil || pv.Group.AmountCents != 13970 || len(pv.Group.Items) != 3 {
		t.Fatalf("link do carro = %+v", pv)
	}
	_ = json.Unmarshal(env.must("", http.MethodGet, "/api/public/invoices/"+token(frete), nil, http.StatusOK), &pv)
	if pv.Group != nil {
		t.Errorf("o frete (sozinho no dia) virou grupo: %+v", pv.Group)
	}
	var public charge
	_ = json.Unmarshal(env.must("", http.MethodPost, "/api/public/invoices/"+token(moto)+"/pix-group", nil, http.StatusOK), &public)
	if public.ID != c.ID {
		t.Errorf("o link gerou outro Pix: %v ≠ %v", public.ID, c.ID)
	}
	// O Pix do mês aparece pelo link de qualquer uma do grupo; pelo do frete, não.
	env.must("", http.MethodGet, "/api/public/invoices/"+token(carro)+"/charges/"+c.ID.String(), nil, http.StatusOK)
	env.must("", http.MethodGet, "/api/public/invoices/"+token(frete)+"/charges/"+c.ID.String(), nil, http.StatusNotFound)

	// Pago: as três quitam com ele; o frete continua em aberto.
	var paid charge
	_ = json.Unmarshal(env.must(customer, http.MethodPost, "/api/me/charges/"+c.ID.String()+"/simulate", nil, http.StatusOK), &paid)
	if paid.InvoiceStatus != "PAID" {
		t.Fatalf("Pix pago = %+v", paid)
	}
	for _, id := range month {
		if s, by := status(id); s != "PAID" || by == nil || *by != c.ID {
			t.Errorf("fatura %v = %s (paga por %v)", id, s, by)
		}
	}
	if s, _ := status(frete); s != "OPEN" {
		t.Errorf("o frete = %s", s)
	}
	pix(month, http.StatusConflict)
	// Na lista de pagamentos, um só, das três.
	var withPayments struct {
		Payments []struct {
			InvoiceDescription string `json:"invoiceDescription"`
			AmountCents        int    `json:"amountCents"`
		} `json:"payments"`
	}
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/customers/"+frota.ID.String(), nil, http.StatusOK), &withPayments)
	if p := withPayments.Payments; len(p) != 1 || p[0].AmountCents != 13970 || !strings.HasPrefix(p[0].InvoiceDescription, "3 faturas pagas juntas") {
		t.Errorf("pagamentos = %+v", p)
	}

	// Novembro: a central muda uma fatura depois do Pix; pago, nada quita
	// sozinho (alguém confere).
	nov := billing.NewDate(2026, 11, 13)
	a, b := invoice(frota, "Plano Frota — novembro/2026 (Van 01)", 4990, nov), invoice(frota, "Plano Frota — novembro/2026 (Carro 02)", 4990, nov)
	c2 := pix([]uuid.UUID{a, b}, http.StatusOK)
	if _, err := env.db.Exec(ctx, `UPDATE invoices SET amount_cents = 3990 WHERE id = $1`, b); err != nil {
		t.Fatal(err)
	}
	env.must(customer, http.MethodPost, "/api/me/charges/"+c2.ID.String()+"/simulate", nil, http.StatusOK)
	for _, id := range []uuid.UUID{a, b} {
		if s, _ := status(id); s != "OPEN" {
			t.Errorf("fatura mudada, mas quitou: %v = %s", id, s)
		}
	}
	var review int
	_ = env.db.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE action = 'PAYMENT_UNMATCHED' AND result = 'REVIEW'`).Scan(&review)
	if review != 1 {
		t.Errorf("revisões = %d", review)
	}

	// O lembrete das duas de novembro sai junto, cada uma com o link dela (o
	// e-mail junta as do mesmo dia num botão só: ver mail.TestInvoiceReminderFleet).
	clock = time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)
	svc.Work(ctx)
	mailer.mu.Lock()
	last := mailer.last
	mailer.mu.Unlock()
	if len(last.Invoices) != 2 || last.Invoices[0].DueDate != last.Invoices[1].DueDate ||
		!strings.HasPrefix(last.Invoices[0].PayURL, "https://painel.farbo.test/pagar/") {
		t.Errorf("lembrete de novembro = %+v", last)
	}

	// Uma frota de 1000 veículos em dezembro: a lista traz todas as em aberto
	// (não só as mais recentes), o link de qualquer uma oferece as 1000, o
	// Pix único sai pelo total e, pago, quita todas de uma vez.
	dec := billing.NewDate(2026, 12, 13)
	if _, err := env.db.Exec(ctx, `
		INSERT INTO invoices (customer_id, description, amount_cents, due_date)
		SELECT $1, 'Plano Frota — dezembro/2026 (Veículo ' || g || ')', 4990, $2 FROM generate_series(1, 1000) g`,
		frota.ID, dec.Time); err != nil {
		t.Fatal(err)
	}
	var listed []struct {
		ID      uuid.UUID `json:"id"`
		Status  string    `json:"status"`
		DueDate string    `json:"dueDate"`
	}
	_ = json.Unmarshal(env.must(customer, http.MethodGet, "/api/me/invoices", nil, http.StatusOK), &listed)
	var fleet []uuid.UUID
	for _, inv := range listed {
		if inv.Status == "OPEN" && inv.DueDate == dec.String() {
			fleet = append(fleet, inv.ID)
		}
	}
	if len(fleet) != 1000 {
		t.Fatalf("a lista trouxe %d das 1000 de dezembro", len(fleet))
	}
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/customers/"+frota.ID.String(), nil, http.StatusOK), &detail)
	_ = json.Unmarshal(env.must("", http.MethodGet, "/api/public/invoices/"+token(fleet[500]), nil, http.StatusOK), &pv)
	if pv.Group == nil || len(pv.Group.Items) != 1000 || pv.Group.AmountCents != 1000*4990 {
		t.Fatalf("link de uma das 1000 = %+v", pv.Group)
	}
	start := time.Now()
	big := pix(fleet, http.StatusOK)
	if big.AmountCents != 1000*4990 || len(big.InvoiceIDs) != 1000 {
		t.Fatalf("Pix da frota = %d centavos, %d faturas", big.AmountCents, len(big.InvoiceIDs))
	}
	if again := pix(fleet, http.StatusOK); again.ID != big.ID {
		t.Errorf("não reaproveitou o Pix da frota: %v ≠ %v", again.ID, big.ID)
	}
	_ = json.Unmarshal(env.must(customer, http.MethodPost, "/api/me/charges/"+big.ID.String()+"/simulate", nil, http.StatusOK), &paid)
	if paid.InvoiceStatus != "PAID" {
		t.Errorf("Pix da frota pago = %+v", paid.InvoiceStatus)
	}
	var left int
	if err := env.db.QueryRow(ctx, `SELECT count(*) FROM invoices WHERE customer_id = $1 AND due_date = $2
		AND (status <> 'PAID' OR paid_charge_id IS DISTINCT FROM $3)`, frota.ID, dec.Time, big.ID).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Errorf("%d das 1000 não quitaram com o Pix da frota", left)
	}
	t.Logf("frota de 1000: Pix, reaproveitar e pagar em %v", time.Since(start).Round(time.Millisecond))

	// O histórico: a primeira página traz dezembro inteiro (as 1000 pagas,
	// sem cortar o mês) e para aí; "antes de 13/12" traz outubro.
	closed := func(path string) map[string]int {
		var list []struct {
			Status  string `json:"status"`
			DueDate string `json:"dueDate"`
		}
		_ = json.Unmarshal(env.must(customer, http.MethodGet, path, nil, http.StatusOK), &list)
		out := map[string]int{}
		for _, inv := range list {
			if inv.Status != "OPEN" {
				out[inv.DueDate]++
			}
		}
		return out
	}
	if got := closed("/api/me/invoices"); len(got) != 1 || got[dec.String()] != 1000 {
		t.Errorf("primeira página do histórico = %v", got)
	}
	if got := closed("/api/me/invoices?antes=" + dec.String()); len(got) != 1 || got[due.String()] != 3 {
		t.Errorf("antes de dezembro = %v", got)
	}
	if got := closed("/api/me/invoices?antes=" + due.String()); len(got) != 0 {
		t.Errorf("antes de outubro = %v", got)
	}
	env.must(customer, http.MethodGet, "/api/me/invoices?antes=13-12-2026", nil, http.StatusBadRequest)
}
