package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/finance"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/mail"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/payments"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/payments/abacatepay"
)

// recordingCardMailer guarda os avisos de cartão recusado.
type recordingCardMailer struct {
	mu   sync.Mutex
	sent []mail.CardDeclined
}

func (m *recordingCardMailer) Declined(_ context.Context, d mail.CardDeclined) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, d)
	return nil
}

func (m *recordingCardMailer) take() []mail.CardDeclined {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := m.sent
	m.sent = nil
	return out
}

// O cartão de crédito de ponta a ponta: pagar a fatura com o cartão do
// formulário guardando o cartão (cifrado) com o débito automático; a recusa;
// a resposta perdida (a conferência acha a cobrança, ou vê que ela não
// chegou); o débito automático no vencimento (uma cobrança para as faturas
// do dia, uma vez), as recusas com aviso e a última tentativa; o repasse da
// adquirente sem quitar de novo; o estorno; a central desligando e apagando;
// e a tarifa no caixa. Precisa de FARBO_TEST_DATABASE_URL.
func TestCardPayment(t *testing.T) {
	gw := &fakePixGateway{}
	notifier := &recordingCardMailer{}
	var paySvc *payments.Service
	var fs *finance.Service
	clock := time.Date(2026, 10, 10, 10, 0, 0, 0, time.UTC)
	env, _ := newLeadsEnv(t, func(d *Deps) {
		d.Config.Payments = config.Payments{AbacatePayAPIKey: "abc_dev_teste", PublicKey: "pk_teste", PixExpiresIn: 24 * time.Hour}
		d.Billing.SetClock(func() time.Time { return clock })
		paySvc = payments.NewService(payments.NewRepository(d.DB), d.Billing, gw, d.Audit, d.Config.Payments, d.Log)
		if err := paySvc.SetCardSecret("segredo-de-teste"); err != nil {
			t.Fatal(err)
		}
		paySvc.SetDebit(payments.DebitConfig{
			Payer: func(ctx context.Context, id uuid.UUID) (*payments.Payer, error) {
				var p payments.Payer
				err := d.DB.QueryRow(ctx, `SELECT name, email, document, phone FROM users WHERE id = $1`, id).
					Scan(&p.Name, &p.Email, &p.Document, &p.Phone)
				return &p, err
			},
			Notifier: notifier, Location: time.UTC,
		})
		d.Payments = paySvc
		fs = d.Finance
	})
	ctx := context.Background()
	authSvc := auth.NewService(auth.NewRepository(env.db), config.Auth{BcryptCost: bcrypt.MinCost}, nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	cliente, err := authSvc.CreateUser(ctx, "cartao@pagamento.test", "Transportes Cartão Ltda", auth.RoleCustomer, userPassword)
	if err != nil {
		t.Fatal(err)
	}
	semCelular, err := authSvc.CreateUser(ctx, "semcel@pagamento.test", "Sem Celular", auth.RoleCustomer, userPassword)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.db.Exec(ctx, `UPDATE users SET document = '529.982.247-25', phone = '(11) 99999-0000' WHERE id = $1;`, cliente.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := env.db.Exec(ctx, `UPDATE users SET document = '529.982.247-25' WHERE id = $1`, semCelular.ID); err != nil {
		t.Fatal(err)
	}
	admin := env.login(auth.RoleAdmin + "@leads.test")
	customer := env.login("cartao@pagamento.test")
	invoice := func(owner uuid.UUID, desc string, cents int, due billing.Date) uuid.UUID {
		t.Helper()
		var out struct {
			ID uuid.UUID `json:"id"`
		}
		body := env.must(admin, http.MethodPost, "/api/customers/"+owner.String()+"/invoices",
			map[string]any{"description": desc, "amountCents": cents, "dueDate": due.String()}, http.StatusCreated)
		if err := json.Unmarshal(body, &out); err != nil {
			t.Fatal(err)
		}
		return out.ID
	}
	type charge struct {
		ID            uuid.UUID `json:"id"`
		Method        string    `json:"method"`
		Status        string    `json:"status"`
		AmountCents   int       `json:"amountCents"`
		CardBrand     string    `json:"cardBrand"`
		CardLast4     string    `json:"cardLast4"`
		InvoiceStatus string    `json:"invoiceStatus"`
	}
	pay := func(path string, body map[string]any, want int) (charge, string) {
		t.Helper()
		var c charge
		raw := env.must(customer, http.MethodPost, path, body, want)
		_ = json.Unmarshal(raw, &c)
		return c, string(raw)
	}
	invoiceState := func(id uuid.UUID) string {
		var s string
		_ = env.db.QueryRow(ctx, `SELECT status || '/' || paid_via FROM invoices WHERE id = $1`, id).Scan(&s)
		return s
	}
	var view struct {
		Enabled   bool   `json:"enabled"`
		PublicKey string `json:"publicKey"`
		Payer     *struct {
			TaxID     string `json:"taxId"`
			Cellphone string `json:"cellphone"`
		} `json:"payer"`
		Saved *struct {
			Brand     string     `json:"brand"`
			Last4     string     `json:"last4"`
			AutoDebit bool       `json:"autoDebit"`
			FailedAt  *time.Time `json:"failedAt"`
		} `json:"saved"`
	}
	readView := func() string {
		t.Helper()
		raw := env.must(customer, http.MethodGet, "/api/me/card", nil, http.StatusOK)
		view.Saved = nil
		_ = json.Unmarshal(raw, &view)
		return string(raw)
	}

	// O cartão ligado, sem cartão salvo.
	readView()
	if !view.Enabled || view.PublicKey != "pk_teste" || view.Saved != nil || view.Payer == nil ||
		view.Payer.TaxID != "52998224725" || view.Payer.Cellphone != "11999990000" {
		t.Fatalf("cartão da conta = %+v", view)
	}

	// Paga a fatura com o cartão do formulário e guarda com o débito automático.
	outubro := invoice(cliente.ID, "Plano Frota — outubro/2026", 4990, billing.NewDate(2026, 10, 20))
	c, _ := pay("/api/me/invoices/"+outubro.String()+"/card", map[string]any{"token": "ctok_1", "autoDebit": true}, http.StatusOK)
	if c.Method != payments.MethodCard || c.Status != abacatepay.StatusApproved || c.CardBrand != "VISA" || c.CardLast4 != "4242" ||
		c.InvoiceStatus != billing.InvoicePaid || invoiceState(outubro) != "PAID/CARD" {
		t.Fatalf("pago no cartão: %+v (%s)", c, invoiceState(outubro))
	}
	if gw.lastCard.Token != "ctok_1" || gw.lastCard.AmountCents != 4990 || gw.lastCard.Customer.Cellphone != "11999990000" ||
		gw.lastCard.Metadata["chargeId"] != c.ID.String() {
		t.Errorf("pedido à AbacatePay = %+v", gw.lastCard)
	}
	// O cartão salvo: cifrado no banco, e o token não sai na resposta.
	var sealed string
	var consentBy *uuid.UUID
	_ = env.db.QueryRow(ctx, `SELECT card_token, consent_by FROM customer_cards WHERE customer_id = $1`, cliente.ID).Scan(&sealed, &consentBy)
	if sealed == "" || strings.Contains(sealed, "dev_card_4242") || consentBy == nil || *consentBy != cliente.ID {
		t.Errorf("cartão guardado = %q, consentimento de %v", sealed, consentBy)
	}
	if raw := readView(); view.Saved == nil || !view.Saved.AutoDebit || view.Saved.Last4 != "4242" || strings.Contains(raw, "dev_card") {
		t.Fatalf("cartão salvo = %s", raw)
	}

	// Sem celular no cadastro, o cartão não sai.
	other := env.login("semcel@pagamento.test")
	alheia := invoice(semCelular.ID, "Plano Mensal", 4990, billing.NewDate(2026, 10, 20))
	if raw := string(env.must(other, http.MethodPost, "/api/me/invoices/"+alheia.String()+"/card", map[string]any{"token": "ctok_x"},
		http.StatusConflict)); !strings.Contains(raw, "celular") {
		t.Errorf("sem celular: %s", raw)
	}

	// Recusado: 402, a cobrança FAILED e a fatura em aberto.
	novembro := invoice(cliente.ID, "Plano Frota — novembro/2026", 4990, billing.NewDate(2026, 11, 20))
	gw.cardMode = "decline"
	if _, raw := pay("/api/me/invoices/"+novembro.String()+"/card", map[string]any{"token": "ctok_2"}, http.StatusPaymentRequired); !strings.Contains(raw, "o cartão foi recusado") {
		t.Errorf("recusa: %s", raw)
	}
	if invoiceState(novembro) != "OPEN/" {
		t.Errorf("fatura depois da recusa: %s", invoiceState(novembro))
	}

	// A resposta se perde, mas a cobrança passou: a conferência acha e quita.
	gw.cardMode = "network"
	c, _ = pay("/api/me/invoices/"+novembro.String()+"/card", map[string]any{"useSaved": true}, http.StatusAccepted)
	if c.Status != payments.StatusCreating || gw.lastCard.CardToken != "dev_card_4242" {
		t.Fatalf("sem resposta: %+v (pedido %+v)", c, gw.lastCard)
	}
	gw.cardMode = ""
	paySvc.SyncPending(ctx) // ainda dentro do primeiro minuto: espera
	if invoiceState(novembro) != "OPEN/" {
		t.Fatal("a conferência não esperou o primeiro minuto")
	}
	if _, err := env.db.Exec(ctx, `UPDATE payment_charges SET created_at = NOW() - INTERVAL '2 minutes' WHERE id = $1`, c.ID); err != nil {
		t.Fatal(err)
	}
	paySvc.SyncPending(ctx)
	if invoiceState(novembro) != "PAID/CARD" {
		t.Errorf("depois da conferência: %s", invoiceState(novembro))
	}

	// A resposta se perde e a cobrança nem chegou: depois de 30 min, FAILED.
	dezembro := invoice(cliente.ID, "Plano Frota — dezembro/2026", 4990, billing.NewDate(2026, 12, 20))
	gw.cardMode = "lost"
	lost, _ := pay("/api/me/invoices/"+dezembro.String()+"/card", map[string]any{"useSaved": true}, http.StatusAccepted)
	gw.cardMode = ""
	if _, err := env.db.Exec(ctx, `UPDATE payment_charges SET created_at = NOW() - INTERVAL '31 minutes' WHERE id = $1`, lost.ID); err != nil {
		t.Fatal(err)
	}
	paySvc.SyncPending(ctx)
	var lostStatus string
	_ = env.db.QueryRow(ctx, `SELECT status FROM payment_charges WHERE id = $1`, lost.ID).Scan(&lostStatus)
	if lostStatus != abacatepay.StatusFailed || invoiceState(dezembro) != "OPEN/" {
		t.Errorf("cobrança perdida: %s, fatura %s", lostStatus, invoiceState(dezembro))
	}
	if _, err := env.db.Exec(ctx, `UPDATE invoices SET status = 'CANCELED' WHERE id = $1`, dezembro); err != nil {
		t.Fatal(err)
	}

	// O débito automático no vencimento: as duas do dia numa cobrança só, uma vez.
	van, carro := invoice(cliente.ID, "Plano Frota (Van)", 4990, billing.NewDate(2026, 10, 10)), invoice(cliente.ID, "Plano Frota (Carro)", 3990, billing.NewDate(2026, 10, 10))
	if n, err := paySvc.DebitRound(ctx); err != nil || n != 1 {
		t.Fatalf("débito: %d, %v", n, err)
	}
	if gw.lastCard.AmountCents != 8980 || gw.lastCard.CardToken != "dev_card_4242" || gw.lastCard.Metadata["debitId"] == "" {
		t.Errorf("débito pedido = %+v", gw.lastCard)
	}
	if invoiceState(van) != "PAID/CARD" || invoiceState(carro) != "PAID/CARD" {
		t.Errorf("faturas do débito: %s %s", invoiceState(van), invoiceState(carro))
	}
	if n, _ := paySvc.DebitRound(ctx); n != 0 {
		t.Errorf("cobrou de novo: %d", n)
	}

	// Recusado: avisa e tenta nos dois dias seguintes; a terceira é a última.
	clock = clock.AddDate(0, 0, 1)
	moto := invoice(cliente.ID, "Plano Frota (Moto)", 3990, billing.NewDate(2026, 10, 11))
	gw.cardMode = "decline"
	for day, retry := range []bool{true, true, false} {
		if n, err := paySvc.DebitRound(ctx); err != nil || n != 0 {
			t.Fatalf("dia %d: %d, %v", day, n, err)
		}
		sent := notifier.take()
		if len(sent) != 1 || sent[0].Retry != retry || sent[0].To != "cartao@pagamento.test" || sent[0].Last4 != "4242" ||
			sent[0].AmountCents != 3990 || sent[0].DueDate != "11/10/2026" {
			t.Fatalf("dia %d, aviso = %+v", day, sent)
		}
		if n, _ := paySvc.DebitRound(ctx); n != 0 || len(notifier.take()) != 0 {
			t.Fatalf("dia %d: tentou de novo no mesmo dia", day)
		}
		clock = clock.AddDate(0, 0, 1)
	}
	if paySvc.DebitRound(ctx); len(notifier.take()) != 0 || invoiceState(moto) != "OPEN/" {
		t.Errorf("depois da última tentativa: %s", invoiceState(moto))
	}
	if readView(); view.Saved == nil || view.Saved.FailedAt == nil {
		t.Errorf("a recusa não ficou no cartão: %+v", view.Saved)
	}

	// Sem resposta no débito: a conferência acha a cobrança e o débito segue.
	clock = time.Date(2026, 10, 25, 10, 0, 0, 0, time.UTC)
	gw.cardMode = "network"
	frete := invoice(cliente.ID, "Frete", 2240, billing.NewDate(2026, 10, 25))
	paySvc.DebitRound(ctx)
	gw.cardMode = ""
	if _, err := env.db.Exec(ctx, `UPDATE payment_charges SET created_at = NOW() - INTERVAL '2 minutes' WHERE status = 'CREATING'`); err != nil {
		t.Fatal(err)
	}
	paySvc.SyncPending(ctx)
	paySvc.DebitRound(ctx)
	var debit string
	_ = env.db.QueryRow(ctx, `SELECT status FROM card_debits WHERE due_date = '2026-10-25'`).Scan(&debit)
	if invoiceState(frete) != "PAID/CARD" || debit != "APPROVED" {
		t.Errorf("débito sem resposta: fatura %s, débito %s", invoiceState(frete), debit)
	}
	if readView(); view.Saved.FailedAt != nil {
		t.Error("a recusa antiga não saiu do cartão depois do débito aprovado")
	}

	// O repasse da adquirente (APPROVED → PAID) não quita de novo.
	gw.setStatus(gw.cards[0].ID, abacatepay.StatusPaid)
	var first uuid.UUID
	_ = env.db.QueryRow(ctx, `SELECT id FROM payment_charges WHERE provider_charge_id = $1`, gw.cards[0].ID).Scan(&first)
	if _, err := paySvc.Refresh(ctx, first, true); err != nil {
		t.Fatal(err)
	}
	var unmatched int
	_ = env.db.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action = 'PAYMENT_UNMATCHED'`).Scan(&unmatched)
	if unmatched != 0 || invoiceState(outubro) != "PAID/CARD" {
		t.Errorf("repasse: %d sem par, fatura %s", unmatched, invoiceState(outubro))
	}

	// O cartão tem estorno: a fatura volta a ficar em aberto.
	var refunded charge
	_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/charges/"+first.String()+"/refund",
		map[string]string{"reason": "cobrança em duplicidade"}, http.StatusOK), &refunded)
	if refunded.Status != abacatepay.StatusRefunded || invoiceState(outubro) != "OPEN/" {
		t.Errorf("estorno: %+v, fatura %s", refunded, invoiceState(outubro))
	}

	// A tarifa do cartão entra quando a AbacatePay informa; sem ela, não.
	gw.cardFee = 175
	nov2 := invoice(cliente.ID, "Avulsa", 1000, billing.NewDate(2026, 11, 5))
	withFee, _ := pay("/api/me/invoices/"+nov2.String()+"/card", map[string]any{"useSaved": true}, http.StatusOK)
	if _, err := env.db.Exec(ctx, `UPDATE payment_charges SET dev_mode = FALSE WHERE method = 'CARD'`); err != nil {
		t.Fatal(err)
	}
	fs.RecordReceivedFees(ctx)
	var fees int
	var cents int64
	_ = env.db.QueryRow(ctx, `SELECT count(*), COALESCE(sum(paid_cents), 0) FROM finance_entries
		WHERE description LIKE 'Tarifa do cartão recebido%' AND payment_method = 'CARD'`).Scan(&fees, &cents)
	if fees != 1 || cents != 175 {
		t.Errorf("tarifas do cartão = %d (%d centavos); a cobrança com tarifa era %v", fees, cents, withFee.ID)
	}

	// A central vê o cartão, só desliga o débito e apaga o cartão.
	var detail struct {
		Card *struct {
			Last4     string `json:"last4"`
			AutoDebit bool   `json:"autoDebit"`
		} `json:"card"`
	}
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/customers/"+cliente.ID.String(), nil, http.StatusOK), &detail)
	if detail.Card == nil || detail.Card.Last4 != "4242" || !detail.Card.AutoDebit {
		t.Fatalf("cartão na ficha = %+v", detail.Card)
	}
	env.must(admin, http.MethodPut, "/api/customers/"+cliente.ID.String()+"/card/auto-debit", map[string]bool{"enabled": true}, http.StatusBadRequest)
	env.must(admin, http.MethodPut, "/api/customers/"+cliente.ID.String()+"/card/auto-debit", map[string]bool{"enabled": false}, http.StatusOK)
	if readView(); view.Saved == nil || view.Saved.AutoDebit {
		t.Errorf("débito depois da central: %+v", view.Saved)
	}
	// O cliente liga de novo (a autorização é dele) e depois apaga.
	env.must(customer, http.MethodPut, "/api/me/card/auto-debit", map[string]bool{"enabled": true}, http.StatusOK)
	env.must(admin, http.MethodDelete, "/api/customers/"+cliente.ID.String()+"/card", nil, http.StatusNoContent)
	if readView(); view.Saved != nil {
		t.Errorf("o cartão ficou: %+v", view.Saved)
	}
	env.must(customer, http.MethodPost, "/api/me/invoices/"+novembro.String()+"/card", map[string]any{"useSaved": true}, http.StatusConflict)
}
