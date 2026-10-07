package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/dunning"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/mail"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/payments"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/payments/abacatepay"
)

// fakePixGateway é a AbacatePay de mentira: cria o Pix e paga na simulação.
type fakePixGateway struct {
	mu   sync.Mutex
	paid map[string]bool
	n    int
}

func (g *fakePixGateway) CreatePix(_ context.Context, req abacatepay.PixRequest) (*abacatepay.Pix, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.n++
	exp := time.Now().Add(24 * time.Hour)
	return &abacatepay.Pix{ID: "pix_char_" + strings.Repeat("x", g.n), Amount: req.AmountCents, Status: abacatepay.StatusPending,
		DevMode: true, BrCode: "00020101021226-copia-e-cola", BrCodeBase64: "data:image/png;base64,AAAA", ExpiresAt: &exp}, nil
}

func (g *fakePixGateway) CheckPix(_ context.Context, id string) (*abacatepay.PixStatus, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.paid[id] {
		return &abacatepay.PixStatus{ID: id, Status: abacatepay.StatusPaid}, nil
	}
	return &abacatepay.PixStatus{ID: id, Status: abacatepay.StatusPending}, nil
}

func (g *fakePixGateway) SimulatePayment(_ context.Context, id string) (*abacatepay.Pix, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.paid == nil {
		g.paid = map[string]bool{}
	}
	g.paid[id] = true
	return &abacatepay.Pix{ID: id, Status: abacatepay.StatusPaid, DevMode: true}, nil
}

func (g *fakePixGateway) RefundPix(context.Context, string, string) (*abacatepay.Refund, error) {
	return nil, nil
}

// recordingInvoiceMailer guarda os lembretes ("etapa → para quem: faturas").
type recordingInvoiceMailer struct {
	mu   sync.Mutex
	sent []string
	last mail.InvoiceReminder
}

func (m *recordingInvoiceMailer) Reminder(_ context.Context, to, _ string, r mail.InvoiceReminder) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, r.Kind+" → "+to+": "+strconv.Itoa(len(r.Invoices)))
	m.last = r
	return nil
}

func (m *recordingInvoiceMailer) take() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := m.sent
	m.sent = nil
	return out
}

// A régua de cobrança de ponta a ponta: os lembretes saem na etapa certa,
// uma vez cada, só em horário comercial, juntando as faturas que vencem
// juntas, e param quando a fatura é paga; o link abre o Pix sem login (só o
// primeiro nome, nada mais) e quita a fatura; a central lembra quando quer
// e vê o último lembrete na ficha. Precisa de FARBO_TEST_DATABASE_URL.
func TestDunningEndToEnd(t *testing.T) {
	mailer := &recordingInvoiceMailer{}
	pusher := &recordingPusher{}
	var svc *dunning.Service
	var billingSvc *billing.Service
	// O "hoje" da régua: 10/10, às 10h (horário comercial).
	clock := time.Date(2026, 10, 10, 10, 0, 0, 0, time.UTC)
	env, _ := newLeadsEnv(t, func(d *Deps) {
		d.Config.Payments = config.Payments{AbacatePayAPIKey: "abc_dev_teste", PixExpiresIn: 24 * time.Hour}
		d.Config.Billing.SuspendAfterDays = 10
		billingSvc = d.Billing
		billingSvc.SetClock(func() time.Time { return clock })
		d.Payments = payments.NewService(payments.NewRepository(d.DB), d.Billing, &fakePixGateway{}, d.Audit, d.Config.Payments, d.Log)
		svc = dunning.NewService(d.DB, d.Billing, mailer, dunning.Config{
			SuspendAfterDays: 10, Location: time.UTC, FromHour: 9, ToHour: 20,
			AppURL: "https://painel.farbo.test", LinkSecret: d.Config.Auth.JWTSecret,
		}, d.Log)
		svc.SetPusher(pusher)
		svc.SetClock(func() time.Time { return clock })
		d.Dunning = svc
	})
	ctx := context.Background()
	authSvc := auth.NewService(auth.NewRepository(env.db), config.Auth{BcryptCost: bcrypt.MinCost}, nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	lia, err := authSvc.CreateUser(ctx, "lia@cobranca.test", "Lia Martins Souza", auth.RoleCustomer, userPassword)
	if err != nil {
		t.Fatal(err)
	}
	admin := env.login(auth.RoleAdmin + "@leads.test")
	invoice := func(desc string, cents int, due billing.Date) uuid.UUID {
		t.Helper()
		var out struct {
			ID uuid.UUID `json:"id"`
		}
		body := env.must(admin, http.MethodPost, "/api/customers/"+lia.ID.String()+"/invoices",
			map[string]any{"description": desc, "amountCents": cents, "dueDate": due.String()}, http.StatusCreated)
		if err := json.Unmarshal(body, &out); err != nil {
			t.Fatal(err)
		}
		return out.ID
	}
	today := billing.NewDate(2026, 10, 10)
	// Duas que vencem em 3 dias (juntas), uma que vence hoje, uma vencida há
	// 3 dias, uma há 9 (a suspensão é com 11) e uma em 20 dias (gerada).
	soonA := invoice("Plano Mensal — Moto", 6990, today.AddDays(3))
	invoice("Plano Mensal — Carro", 6990, today.AddDays(3))
	todayID := invoice("Rastreador J16 GT06", 15000, today)
	lateID := invoice("Plano Mensal — setembro", 6990, today.AddDays(-3))
	invoice("Plano Mensal — agosto", 6990, today.AddDays(-9))
	invoice("Plano Mensal — novembro", 6990, today.AddDays(20))

	// De madrugada, nada.
	clock = time.Date(2026, 10, 10, 6, 0, 0, 0, time.UTC)
	if n := svc.Work(ctx); n != 0 || len(mailer.take()) != 0 {
		t.Fatalf("de madrugada: %d", n)
	}
	clock = time.Date(2026, 10, 10, 10, 0, 0, 0, time.UTC)
	if n := svc.Work(ctx); n != 6 {
		t.Fatalf("lembretes = %d", n)
	}
	if got := mailer.take(); !sameSet(got, "DUE_SOON → lia@cobranca.test: 2", "DUE_TODAY → lia@cobranca.test: 1",
		"OVERDUE → lia@cobranca.test: 1", "SUSPENSION_SOON → lia@cobranca.test: 1", "ISSUED → lia@cobranca.test: 1") {
		t.Fatalf("e-mails = %v", got)
	}
	pushed := pusher.take()[lia.ID]
	if len(pushed) != 5 {
		t.Fatalf("push = %+v", pushed)
	}
	// Na mesma hora (ou na próxima volta), nada repete.
	if n := svc.Work(ctx); n != 0 || len(mailer.take()) != 0 {
		t.Fatalf("repetiu: %d", n)
	}

	// A ficha mostra o último lembrete e o link de cada fatura em aberto.
	var detail struct {
		Reminders    map[string]dunning.Reminder `json:"reminders"`
		PaymentLinks map[string]string           `json:"paymentLinks"`
	}
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/customers/"+lia.ID.String(), nil, http.StatusOK), &detail)
	if detail.Reminders[soonA.String()].Kind != "DUE_SOON" || !detail.Reminders[soonA.String()].Emailed ||
		len(detail.PaymentLinks) != 6 || !strings.HasPrefix(detail.PaymentLinks[todayID.String()], "https://painel.farbo.test/pagar/") {
		t.Fatalf("ficha = %+v", detail)
	}

	// O link: sem login, só o primeiro nome; gera o Pix e quita a fatura.
	token := strings.TrimPrefix(detail.PaymentLinks[todayID.String()], "https://painel.farbo.test/pagar/")
	public := env.must("", http.MethodGet, "/api/public/invoices/"+token, nil, http.StatusOK)
	var pv struct {
		FirstName     string `json:"firstName"`
		AmountCents   int    `json:"amountCents"`
		Status        string `json:"status"`
		OnlinePayment bool   `json:"onlinePayment"`
	}
	_ = json.Unmarshal(public, &pv)
	if pv.FirstName != "Lia" || pv.AmountCents != 15000 || pv.Status != "OPEN" || !pv.OnlinePayment {
		t.Fatalf("link = %s", public)
	}
	for _, leak := range []string{"Souza", "lia@", lia.ID.String()} {
		if strings.Contains(string(public), leak) {
			t.Errorf("o link mostra %q: %s", leak, public)
		}
	}
	env.must("", http.MethodGet, "/api/public/invoices/inventado", nil, http.StatusNotFound)
	var charge struct {
		ID     string `json:"id"`
		BrCode string `json:"brCode"`
	}
	_ = json.Unmarshal(env.must("", http.MethodPost, "/api/public/invoices/"+token+"/pix", nil, http.StatusOK), &charge)
	if charge.ID == "" || charge.BrCode == "" {
		t.Fatalf("pix = %+v", charge)
	}
	// O Pix de outra fatura não passa por este link.
	lateToken := strings.TrimPrefix(detail.PaymentLinks[lateID.String()], "https://painel.farbo.test/pagar/")
	env.must("", http.MethodGet, "/api/public/invoices/"+lateToken+"/charges/"+charge.ID, nil, http.StatusNotFound)
	env.must("", http.MethodPost, "/api/public/invoices/"+token+"/charges/"+charge.ID+"/simulate", nil, http.StatusOK)
	_ = json.Unmarshal(env.must("", http.MethodGet, "/api/public/invoices/"+token, nil, http.StatusOK), &pv)
	if pv.Status != "PAID" {
		t.Fatalf("depois do Pix = %+v", pv)
	}

	// Paga, sai da régua; a central lembra as outras quando quer.
	clock = clock.AddDate(0, 0, 3) // 13/10: a de hoje passaria a "vencida há 3 dias"
	billingSvc.SetClock(func() time.Time { return clock })
	svc.Work(ctx)
	// Só as duas que vencem hoje (13/10); a paga ficou de fora.
	if got := mailer.take(); len(got) != 1 || got[0] != "DUE_TODAY → lia@cobranca.test: 2" {
		t.Fatalf("no dia 13 = %v", got)
	}
	for _, inv := range mailer.last.Invoices {
		if inv.Description == "Rastreador J16 GT06" {
			t.Errorf("lembrou a fatura paga: %+v", inv)
		}
	}
	env.must(admin, http.MethodPost, "/api/invoices/"+todayID.String()+"/remind", nil, http.StatusConflict)
	var manual dunning.Reminder
	_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/invoices/"+lateID.String()+"/remind", nil, http.StatusOK), &manual)
	if manual.Kind != "MANUAL" || !manual.Emailed || manual.Pushed != 1 {
		t.Fatalf("manual = %+v", manual)
	}
	if got := mailer.take(); len(got) != 1 || got[0] != "MANUAL → lia@cobranca.test: 1" || mailer.last.DaysLate != 6 {
		t.Fatalf("manual = %v %+v", got, mailer.last)
	}
	var audited int
	if err := env.db.QueryRow(ctx, `SELECT COUNT(*) FROM audit_logs WHERE action = $1`, audit.ActionInvoiceReminded).Scan(&audited); err != nil || audited != 1 {
		t.Errorf("auditoria = %d (%v)", audited, err)
	}
}
