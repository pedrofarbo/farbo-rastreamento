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
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/finance"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/payments"
)

// O boleto com Pix como forma de pagamento: pede o CPF/CNPJ do cadastro;
// vence com a fatura (a vencida fica com os 3 dias úteis da AbacatePay); é
// reaproveitado como o Pix, sem se misturar com ele; serve a uma fatura, a
// várias e ao link sem login; pago, quita a fatura "via boleto", pela tela
// ou pela varredura; não tem simulação nem estorno pela AbacatePay; a tarifa
// do boleto entra no caixa. Precisa de FARBO_TEST_DATABASE_URL.
func TestBoletoPayment(t *testing.T) {
	gw := &fakePixGateway{}
	var paySvc *payments.Service
	var fs *finance.Service
	clock := time.Date(2026, 10, 10, 10, 0, 0, 0, time.UTC)
	env, _ := newLeadsEnv(t, func(d *Deps) {
		d.Config.Payments = config.Payments{AbacatePayAPIKey: "abc_dev_teste", PixExpiresIn: 24 * time.Hour}
		d.Billing.SetClock(func() time.Time { return clock })
		paySvc = payments.NewService(payments.NewRepository(d.DB), d.Billing, gw, d.Audit, d.Config.Payments, d.Log)
		d.Payments = paySvc
		svc := dunning.NewService(d.DB, d.Billing, &recordingInvoiceMailer{}, dunning.Config{
			SuspendAfterDays: 10, Location: time.UTC, FromHour: 9, ToHour: 20,
			AppURL: "https://painel.farbo.test", LinkSecret: d.Config.Auth.JWTSecret,
		}, d.Log)
		svc.SetClock(func() time.Time { return clock })
		d.Dunning = svc
		fs = d.Finance
	})
	ctx := context.Background()
	authSvc := auth.NewService(auth.NewRepository(env.db), config.Auth{BcryptCost: bcrypt.MinCost}, nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	cliente, err := authSvc.CreateUser(ctx, "boleto@pagamento.test", "Transportes Boleto Ltda", auth.RoleCustomer, userPassword)
	if err != nil {
		t.Fatal(err)
	}
	admin := env.login(auth.RoleAdmin + "@leads.test")
	customer := env.login("boleto@pagamento.test")
	invoice := func(desc string, cents int, due billing.Date) uuid.UUID {
		t.Helper()
		var out struct {
			ID uuid.UUID `json:"id"`
		}
		body := env.must(admin, http.MethodPost, "/api/customers/"+cliente.ID.String()+"/invoices",
			map[string]any{"description": desc, "amountCents": cents, "dueDate": due.String()}, http.StatusCreated)
		if err := json.Unmarshal(body, &out); err != nil {
			t.Fatal(err)
		}
		return out.ID
	}
	type charge struct {
		ID               uuid.UUID   `json:"id"`
		Method           string      `json:"method"`
		ProviderChargeID string      `json:"providerChargeId"`
		AmountCents      int         `json:"amountCents"`
		Status           string      `json:"status"`
		BrCode           string      `json:"brCode"`
		BoletoLine       string      `json:"boletoLine"`
		BoletoURL        string      `json:"boletoUrl"`
		InvoiceStatus    string      `json:"invoiceStatus"`
		InvoiceIDs       []uuid.UUID `json:"invoiceIds"`
	}
	post := func(token, path string, body any, want int) (charge, string) {
		t.Helper()
		var c charge
		raw := env.must(token, http.MethodPost, path, body, want)
		_ = json.Unmarshal(raw, &c)
		return c, string(raw)
	}

	outubro := invoice("Plano Frota — outubro/2026", 4990, billing.NewDate(2026, 10, 20))

	// Sem CPF/CNPJ no cadastro, o boleto não sai (o Pix sai).
	if _, raw := post(customer, "/api/me/invoices/"+outubro.String()+"/boleto", nil, http.StatusConflict); !strings.Contains(raw, "CPF ou CNPJ") {
		t.Errorf("sem documento: %s", raw)
	}
	if _, err := env.db.Exec(ctx, `UPDATE users SET document = '529.982.247-25' WHERE id = $1`, cliente.ID); err != nil {
		t.Fatal(err)
	}

	// O boleto vence com a fatura e traz a linha digitável, o link e o Pix.
	b, _ := post(customer, "/api/me/invoices/"+outubro.String()+"/boleto", nil, http.StatusOK)
	if b.Method != payments.MethodBoleto || !strings.HasPrefix(b.ProviderChargeID, "bole_") || b.AmountCents != 4990 ||
		len(b.BoletoLine) != 47 || b.BoletoURL == "" || b.BrCode == "" {
		t.Fatalf("boleto = %+v", b)
	}
	if gw.lastBoleto.DueDate != "2026-10-20" || gw.lastBoleto.Customer.TaxID != "52998224725" ||
		gw.lastBoleto.Customer.Name != "Transportes Boleto Ltda" {
		t.Errorf("pedido à AbacatePay = %+v", gw.lastBoleto)
	}
	// Pedir de novo devolve o mesmo; o Pix é outra cobrança.
	if again, _ := post(customer, "/api/me/invoices/"+outubro.String()+"/boleto", nil, http.StatusOK); again.ID != b.ID {
		t.Errorf("não reaproveitou o boleto: %v ≠ %v", again.ID, b.ID)
	}
	if p, _ := post(customer, "/api/me/invoices/"+outubro.String()+"/pix", nil, http.StatusOK); p.ID == b.ID || p.Method != payments.MethodPix {
		t.Errorf("o Pix virou o boleto: %+v", p)
	}

	// Vencida (ou vencendo hoje): sem data, a AbacatePay dá 3 dias úteis.
	atrasada := invoice("Plano Frota — setembro/2026", 4990, billing.NewDate(2026, 10, 5))
	post(customer, "/api/me/invoices/"+atrasada.String()+"/boleto", nil, http.StatusOK)
	if gw.lastBoleto.DueDate != "" {
		t.Errorf("boleto da vencida com data %q", gw.lastBoleto.DueDate)
	}

	// Sem simulação no sandbox; pago no provedor, a tela vê e a fatura quita
	// "via boleto".
	if _, raw := post(customer, "/api/me/charges/"+b.ID.String()+"/simulate", nil, http.StatusConflict); !strings.Contains(raw, "boleto") {
		t.Errorf("simulação: %s", raw)
	}
	gw.pay(b.ProviderChargeID)
	// A tela reconsulta no máximo a cada 3 s.
	if _, err := env.db.Exec(ctx, `UPDATE payment_charges SET checked_at = NOW() - INTERVAL '1 minute' WHERE id = $1`, b.ID); err != nil {
		t.Fatal(err)
	}
	var paid charge
	_ = json.Unmarshal(env.must(customer, http.MethodGet, "/api/me/charges/"+b.ID.String(), nil, http.StatusOK), &paid)
	var via string
	_ = env.db.QueryRow(ctx, `SELECT paid_via FROM invoices WHERE id = $1`, outubro).Scan(&via)
	if paid.Status != "PAID" || paid.InvoiceStatus != billing.InvoicePaid || via != payments.PaidViaBoleto {
		t.Fatalf("depois de pago: %+v via %q", paid, via)
	}
	// A AbacatePay não estorna boleto.
	if _, raw := post(admin, "/api/charges/"+b.ID.String()+"/refund", map[string]string{"reason": "duplicado"}, http.StatusConflict); !strings.Contains(raw, "não estorna boleto") {
		t.Errorf("estorno: %s", raw)
	}

	// Várias de uma vez (a fatura do mês da frota) e pelo link sem login.
	due := billing.NewDate(2026, 10, 25)
	van, carro := invoice("Plano Frota (Van)", 4990, due), invoice("Plano Frota (Carro)", 3990, due)
	group, _ := post(customer, "/api/me/invoices/boleto", map[string]any{"invoiceIds": []uuid.UUID{van, carro}}, http.StatusOK)
	if group.Method != payments.MethodBoleto || group.AmountCents != 8980 || len(group.InvoiceIDs) != 2 || gw.lastBoleto.DueDate != "2026-10-25" {
		t.Fatalf("boleto do mês = %+v (%+v)", group, gw.lastBoleto)
	}
	var detail struct {
		PaymentLinks map[string]string `json:"paymentLinks"`
	}
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/customers/"+cliente.ID.String(), nil, http.StatusOK), &detail)
	token := strings.TrimPrefix(detail.PaymentLinks[van.String()], "https://painel.farbo.test/pagar/")
	if public, _ := post("", "/api/public/invoices/"+token+"/boleto-group", nil, http.StatusOK); public.ID != group.ID {
		t.Errorf("o link gerou outro boleto: %v ≠ %v", public.ID, group.ID)
	}
	if public, _ := post("", "/api/public/invoices/"+token+"/boleto", nil, http.StatusOK); public.Method != payments.MethodBoleto || len(public.InvoiceIDs) != 0 {
		t.Errorf("boleto de uma pelo link = %+v", public)
	}

	// A varredura também dá baixa (o boleto pago sem a tela aberta).
	gw.pay(group.ProviderChargeID)
	if _, err := env.db.Exec(ctx, `UPDATE payment_charges SET checked_at = NOW() - INTERVAL '1 hour' WHERE id = $1`, group.ID); err != nil {
		t.Fatal(err)
	}
	paySvc.SyncPending(ctx)
	for _, id := range []uuid.UUID{van, carro} {
		var status string
		_ = env.db.QueryRow(ctx, `SELECT status || '/' || paid_via FROM invoices WHERE id = $1`, id).Scan(&status)
		if status != "PAID/BOLETO" {
			t.Errorf("fatura %v depois da varredura: %s", id, status)
		}
	}

	// A tarifa do boleto (a informada, R$ 2,50) entra no caixa, como a do Pix.
	if _, err := env.db.Exec(ctx, `UPDATE payment_charges SET dev_mode = FALSE WHERE id = $1`, b.ID); err != nil {
		t.Fatal(err)
	}
	fs.RecordReceivedFees(ctx)
	var cents int64
	var method string
	if err := env.db.QueryRow(ctx, `SELECT paid_cents, payment_method FROM finance_entries
		WHERE description LIKE 'Tarifa do boleto recebido%'`).Scan(&cents, &method); err != nil || cents != 250 || method != "BOLETO" {
		t.Errorf("tarifa do boleto = %d %s (%v)", cents, method, err)
	}
}
