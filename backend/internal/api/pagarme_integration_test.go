package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/dunning"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/payments"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/payments/pagarme"
)

// fakePagarme é a Pagar.me de mentira: guarda os pedidos e as cobranças, e o
// teste muda o status de cada uma. cardMode "decline" recusa o cartão.
type fakePagarme struct {
	mu       sync.Mutex
	n        int
	orders   []pagarme.OrderRequest
	charges  map[string]*pagarme.Charge
	cardMode string
}

func (f *fakePagarme) CreateOrder(_ context.Context, req pagarme.OrderRequest) (*pagarme.Order, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.n++
	f.orders = append(f.orders, req)
	if f.charges == nil {
		f.charges = map[string]*pagarme.Charge{}
	}
	ch := &pagarme.Charge{ID: fmt.Sprintf("ch_%d", f.n), Code: req.Code, Amount: req.AmountCents, Status: "pending",
		PaymentMethod: req.Method, Customer: &pagarme.CustomerRef{ID: "cus_1"}, LastTransaction: &pagarme.Transaction{}}
	exp := time.Now().Add(24 * time.Hour)
	switch req.Method {
	case pagarme.MethodPix:
		ch.LastTransaction.QRCode, ch.LastTransaction.ExpiresAt = "00020101021226-pagarme-copia-e-cola", &exp
	case pagarme.MethodBoleto:
		due := req.BoletoDueAt
		ch.LastTransaction.Line, ch.LastTransaction.PDF, ch.LastTransaction.DueAt = "23793.38128 60000.000003", "https://pagar.me/boleto.pdf", &due
	case pagarme.MethodCreditCard:
		ch.LastTransaction.Card = &pagarme.Card{ID: "card_1", Brand: "Visa", LastFourDigits: "0010"}
		if f.cardMode == "decline" {
			ch.Status, ch.LastTransaction.AcquirerMessage = "failed", "Saldo insuficiente"
		} else {
			now := time.Now()
			ch.Status, ch.PaidAt, ch.PaidAmount = "paid", &now, req.AmountCents
		}
	}
	f.charges[ch.ID] = ch
	copied := *ch
	return &pagarme.Order{ID: fmt.Sprintf("or_%d", f.n), Code: req.Code, Charges: []*pagarme.Charge{&copied}}, nil
}

func (f *fakePagarme) GetCharge(_ context.Context, id string) (*pagarme.Charge, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ch, ok := f.charges[id]
	if !ok {
		return nil, &pagarme.APIError{HTTPStatus: 404, Message: "não encontrada"}
	}
	copied := *ch
	return &copied, nil
}

func (f *fakePagarme) CancelCharge(_ context.Context, id, _ string) (*pagarme.Charge, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ch := f.charges[id]
	ch.Status = "canceled"
	copied := *ch
	return &copied, nil
}

func (f *fakePagarme) FindOrder(context.Context, string) (*pagarme.Order, error) { return nil, nil }
func (f *fakePagarme) TestMode() bool                                            { return true }

func (f *fakePagarme) pay(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Now()
	f.charges[id].Status, f.charges[id].PaidAt, f.charges[id].PaidAmount = "paid", &now, f.charges[id].Amount
}

func (f *fakePagarme) last() pagarme.OrderRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.orders[len(f.orders)-1]
}

// Os dois gateways de ponta a ponta: o Pix vai pela Pagar.me até o valor em
// que a tarifa dela (0,99%) fica abaixo da fixa da AbacatePay (R$ 0,80), e
// pela AbacatePay acima disso ou quando o cadastro não tem o que a Pagar.me
// exige; o webhook da Pagar.me (com o segredo) dá baixa; o estorno reabre a
// fatura; o boleto e o cartão vão pela Pagar.me (com o endereço), o cartão
// fica salvo na carteira dela e a recusa volta com o motivo do banco.
// Precisa de FARBO_TEST_DATABASE_URL.
func TestPagarmeGateway(t *testing.T) {
	abacate := &fakePixGateway{}
	pm := &fakePagarme{}
	clock := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	env, _ := newLeadsEnv(t, func(d *Deps) {
		d.Config.Payments = config.Payments{
			AbacatePayAPIKey: "abc_dev_teste", PixExpiresIn: 24 * time.Hour,
			PagarmeSecretKey: "sk_test_x", PagarmePublicKey: "pk_test_x", PagarmeWebhookSecret: "segredo-pm",
			PagarmeStatementDescriptor: "FARBO", AbacatePayPixFeeCents: 80, PagarmePixFeeBps: 99,
		}
		d.Billing.SetClock(func() time.Time { return clock })
		svc := payments.NewService(payments.NewRepository(d.DB), d.Billing, abacate, d.Audit, d.Config.Payments, d.Log)
		svc.SetPagarme(pm)
		if err := svc.SetCardSecret("segredo-de-teste"); err != nil {
			t.Fatal(err)
		}
		d.Payments = svc
		// Os links de pagamento (sem login), para o cartão neles.
		d.Dunning = dunning.NewService(d.DB, d.Billing, &recordingInvoiceMailer{}, dunning.Config{
			Location: time.UTC, FromHour: 9, ToHour: 20, AppURL: "https://painel.farbo.test", LinkSecret: d.Config.Auth.JWTSecret,
		}, d.Log)
	})
	ctx := context.Background()
	admin := env.login(auth.RoleAdmin + "@leads.test")
	newCustomer := func(email, phone string) (uuid.UUID, string) {
		t.Helper()
		var c struct {
			ID uuid.UUID `json:"id"`
		}
		_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/customers", map[string]any{
			"name": "Cliente " + email, "email": email, "password": userPassword,
		}, http.StatusCreated), &c)
		if _, err := env.db.Exec(ctx, `UPDATE users SET document = '529.982.247-25', phone = $2 WHERE id = $1`, c.ID, phone); err != nil {
			t.Fatal(err)
		}
		return c.ID, env.login(email)
	}
	invoice := func(owner uuid.UUID, cents int) uuid.UUID {
		t.Helper()
		var out struct {
			ID uuid.UUID `json:"id"`
		}
		_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/customers/"+owner.String()+"/invoices",
			map[string]any{"description": "Mensalidade", "amountCents": cents, "dueDate": billing.Date{Time: clock.AddDate(0, 0, 8)}.String()},
			http.StatusCreated), &out)
		return out.ID
	}
	type charge struct {
		ID               uuid.UUID `json:"id"`
		Provider         string    `json:"provider"`
		Method           string    `json:"method"`
		Status           string    `json:"status"`
		ProviderChargeID string    `json:"providerChargeId"`
		BrCode           string    `json:"brCode"`
		QRCodeImage      string    `json:"qrCodeImage"`
		BoletoLine       string    `json:"boletoLine"`
		BoletoURL        string    `json:"boletoUrl"`
		CardLast4        string    `json:"cardLast4"`
		InvoiceStatus    string    `json:"invoiceStatus"`
	}
	post := func(token, path string, body any, want int) (charge, string) {
		t.Helper()
		var c charge
		raw := env.must(token, http.MethodPost, path, body, want)
		_ = json.Unmarshal(raw, &c)
		return c, string(raw)
	}
	invoiceState := func(id uuid.UUID) string {
		var s string
		_ = env.db.QueryRow(ctx, `SELECT status || '/' || paid_via FROM invoices WHERE id = $1`, id).Scan(&s)
		return s
	}

	ana, anaTok := newCustomer("ana@pagarme.test", "(11) 99999-0000")
	semCel, semCelTok := newCustomer("semcel@pagarme.test", "")

	// R$ 49,90: 0,99% dá R$ 0,49, menos que os R$ 0,80 da AbacatePay.
	small := invoice(ana, 4990)
	pix, _ := post(anaTok, "/api/me/invoices/"+small.String()+"/pix", nil, http.StatusOK)
	if pix.Provider != "pagarme" || pix.BrCode != "00020101021226-pagarme-copia-e-cola" ||
		!strings.HasPrefix(pix.QRCodeImage, "data:image/png;base64,") {
		t.Fatalf("Pix pequeno = %+v", pix)
	}
	if o := pm.last(); o.Method != pagarme.MethodPix || o.Customer == nil || o.Customer.Phones.MobilePhone.AreaCode != "11" ||
		o.Customer.Document != "52998224725" || o.Customer.Type != "individual" || o.PixExpiresIn != 24*time.Hour {
		t.Fatalf("pedido do Pix = %+v", o)
	}
	// R$ 149,90: 0,99% dá R$ 1,48, mais que os R$ 0,80: AbacatePay.
	big := invoice(ana, 14990)
	if c, _ := post(anaTok, "/api/me/invoices/"+big.String()+"/pix", nil, http.StatusOK); c.Provider != "abacatepay" {
		t.Fatalf("Pix grande = %+v", c)
	}
	// Sem celular, a Pagar.me não aceita o pagador: vai pela AbacatePay.
	if c, _ := post(semCelTok, "/api/me/invoices/"+invoice(semCel, 4990).String()+"/pix", nil, http.StatusOK); c.Provider != "abacatepay" {
		t.Fatalf("Pix sem celular = %+v", c)
	}

	// O webhook: sem o segredo, recusado; com ele, a cobrança é reconsultada.
	hook := func(secret string, ev map[string]any, want int) string {
		t.Helper()
		return string(env.must("", http.MethodPost, "/api/payments/pagarme/webhook?secret="+secret, ev, want))
	}
	paid := map[string]any{"id": "hook_1", "type": "charge.paid", "data": map[string]any{"id": pix.ProviderChargeID}}
	hook("errado", paid, http.StatusUnauthorized)
	pm.pay(pix.ProviderChargeID)
	hook("segredo-pm", paid, http.StatusOK)
	if got := invoiceState(small); got != "PAID/PIX" {
		t.Fatalf("fatura depois do webhook = %s", got)
	}
	if out := hook("segredo-pm", paid, http.StatusOK); !strings.Contains(out, "duplicado") {
		t.Fatalf("webhook repetido = %s", out)
	}

	// O estorno pela central reabre a fatura.
	refunded, _ := post(admin, "/api/charges/"+pix.ID.String()+"/refund", map[string]string{"reason": "pago em dobro"}, http.StatusOK)
	if refunded.Status != "REFUNDED" || invoiceState(small) != "OPEN/" {
		t.Fatalf("estorno = %+v, fatura %s", refunded, invoiceState(small))
	}

	// O boleto pela Pagar.me pede o endereço.
	boletoInvoice := invoice(ana, 6990)
	if _, raw := post(anaTok, "/api/me/invoices/"+boletoInvoice.String()+"/boleto", nil, http.StatusOK); !strings.Contains(raw, "abacatepay") {
		t.Fatalf("boleto sem endereço deveria ir pela AbacatePay: %s", raw)
	}
	if _, err := env.db.Exec(ctx, `
		INSERT INTO customer_addresses (customer_id, zip_code, street, number, complement, district, city, state)
		VALUES ($1, '01001000', 'Praça da Sé', '100', 'sala 2', 'Sé', 'São Paulo', 'SP')`, ana); err != nil {
		t.Fatal(err)
	}
	boleto, _ := post(anaTok, "/api/me/invoices/"+invoice(ana, 7990).String()+"/boleto", nil, http.StatusOK)
	if boleto.Provider != "pagarme" || boleto.BoletoLine == "" || boleto.BoletoURL != "https://pagar.me/boleto.pdf" {
		t.Fatalf("boleto = %+v", boleto)
	}
	if o := pm.last(); o.Customer.Address == nil || o.Customer.Address.Line1 != "100, Praça da Sé, Sé" || o.Customer.Address.Line2 != "sala 2" {
		t.Fatalf("endereço do boleto = %+v", o.Customer.Address)
	}

	// O cartão: a chave pública da Pagar.me vai para a tela.
	var view struct {
		Enabled   bool   `json:"enabled"`
		Provider  string `json:"provider"`
		PublicKey string `json:"publicKey"`
	}
	_ = json.Unmarshal(env.must(anaTok, http.MethodGet, "/api/me/card", nil, http.StatusOK), &view)
	if !view.Enabled || view.Provider != "pagarme" || view.PublicKey != "pk_test_x" {
		t.Fatalf("cartão = %+v", view)
	}
	cardInvoice := invoice(ana, 9990)
	card, _ := post(anaTok, "/api/me/invoices/"+cardInvoice.String()+"/card", map[string]any{"token": "token_abc", "save": true}, http.StatusOK)
	if card.Provider != "pagarme" || card.Status != "PAID" || card.CardLast4 != "0010" || invoiceState(cardInvoice) != "PAID/CARD" {
		t.Fatalf("cartão pago = %+v %s", card, invoiceState(cardInvoice))
	}
	if o := pm.last(); o.CardToken != "token_abc" || o.BillingAddress == nil || o.StatementDescriptor != "FARBO" {
		t.Fatalf("pedido do cartão = %+v", o)
	}
	// O cartão salvo paga a próxima pela carteira da Pagar.me.
	if c, _ := post(anaTok, "/api/me/invoices/"+invoice(ana, 9990).String()+"/card", map[string]any{"useSaved": true}, http.StatusOK); c.Status != "PAID" {
		t.Fatalf("cartão salvo = %+v", c)
	}
	if o := pm.last(); o.CardID != "card_1" || o.CustomerID != "cus_1" || o.CardToken != "" {
		t.Fatalf("pedido do cartão salvo = %+v", o)
	}
	// A recusa volta com o motivo do banco.
	pm.cardMode = "decline"
	if _, raw := post(anaTok, "/api/me/invoices/"+invoice(ana, 9990).String()+"/card", map[string]any{"token": "token_def"},
		http.StatusPaymentRequired); !strings.Contains(raw, "Saldo insuficiente") {
		t.Fatalf("recusa = %s", raw)
	}
	pm.cardMode = ""

	// O cartão nos links sem login (o da fatura e o de todas em aberto): a
	// chave pública, sem os dados do pagador na página; paga com os do
	// cadastro, sem salvar o cartão.
	var detail struct {
		PaymentLinks map[string]string `json:"paymentLinks"`
		PayAllURL    string            `json:"payAllUrl"`
	}
	linked := invoice(ana, 4990)
	other := invoice(ana, 3000)
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/customers/"+ana.String(), nil, http.StatusOK), &detail)
	invoiceToken := strings.TrimPrefix(detail.PaymentLinks[linked.String()], "https://painel.farbo.test/pagar/")
	raw := env.must("", http.MethodGet, "/api/public/invoices/"+invoiceToken+"/card", nil, http.StatusOK)
	var pub struct {
		Enabled    bool   `json:"enabled"`
		Provider   string `json:"provider"`
		PublicKey  string `json:"publicKey"`
		PayerReady bool   `json:"payerReady"`
		PublicLink bool   `json:"publicLink"`
	}
	_ = json.Unmarshal(raw, &pub)
	if !pub.Enabled || pub.Provider != "pagarme" || pub.PublicKey != "pk_test_x" || !pub.PayerReady || !pub.PublicLink {
		t.Fatalf("cartão do link = %s", raw)
	}
	for _, leak := range []string{"529", "@pagarme.test", "Praça", "01001000"} {
		if strings.Contains(string(raw), leak) {
			t.Errorf("o cartão do link mostra %q: %s", leak, raw)
		}
	}
	byLink, _ := post("", "/api/public/invoices/"+invoiceToken+"/card", map[string]any{"token": "token_link"}, http.StatusOK)
	if byLink.Status != "PAID" || invoiceState(linked) != "PAID/CARD" || invoiceState(other) == "PAID/CARD" {
		t.Fatalf("cartão pelo link = %+v (%s / %s)", byLink, invoiceState(linked), invoiceState(other))
	}
	if o := pm.last(); o.CardToken != "token_link" || o.BillingAddress == nil || (o.Customer == nil && o.CustomerID == "") {
		t.Fatalf("pedido do cartão pelo link = %+v", o)
	}
	// O link de todas: as em aberto que sobraram, num cartão só.
	accountToken := strings.TrimPrefix(detail.PayAllURL, "https://painel.farbo.test/pagar/conta/")
	env.must("", http.MethodGet, "/api/public/accounts/"+accountToken+"/card", nil, http.StatusOK)
	all, _ := post("", "/api/public/accounts/"+accountToken+"/card", map[string]any{"token": "token_todas"}, http.StatusOK)
	if all.Status != "PAID" || invoiceState(other) != "PAID/CARD" {
		t.Fatalf("cartão de todas = %+v (%s)", all, invoiceState(other))
	}
	env.must("", http.MethodPost, "/api/public/accounts/"+accountToken+"/card", map[string]any{"token": "token_x"}, http.StatusConflict)
	// O link errado não paga.
	env.must("", http.MethodPost, "/api/public/invoices/inventado/card", map[string]any{"token": "token_x"}, http.StatusNotFound)
}
