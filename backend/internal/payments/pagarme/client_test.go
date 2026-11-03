package pagarme

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fake grava o último pedido e responde o que o teste mandar.
type fake struct {
	method, path, auth, idempotency string
	body                            map[string]any
	status                          int
	reply                           string
}

func (f *fake) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.method, f.path = r.Method, r.URL.RequestURI()
		f.auth, f.idempotency = r.Header.Get("Authorization"), r.Header.Get("Idempotency-Key")
		raw, _ := io.ReadAll(r.Body)
		f.body = nil
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &f.body)
		}
		w.Header().Set("Content-Type", "application/json")
		if f.status != 0 {
			w.WriteHeader(f.status)
		}
		_, _ = io.WriteString(w, f.reply)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestCreatePixOrder(t *testing.T) {
	f := &fake{reply: `{"id":"or_1","code":"c-1","status":"pending","charges":[{"id":"ch_1","amount":4990,"status":"pending",
		"payment_method":"pix","customer":{"id":"cus_1"},"last_transaction":{"status":"waiting_payment",
		"qr_code":"00020101...","qr_code_url":"https://api.pagar.me/qr.png","expires_at":"2026-11-02T15:00:00Z"}}]}`}
	c := NewClient(f.server(t).URL, "sk_test_abc")
	order, err := c.CreateOrder(context.Background(), OrderRequest{
		Code: "c-1", AmountCents: 4990, Description: "Farbo — mensalidade", Method: MethodPix, PixExpiresIn: time.Hour,
		Customer: &Customer{Name: "Ana", Email: "ana@x.test", Document: "52998224725", Type: "individual",
			Phones: Phones{MobilePhone: &Phone{CountryCode: "55", AreaCode: "11", Number: "999998888"}}},
		Split: []SplitRule{{Amount: 699, RecipientID: "rp_af", Type: "flat"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if f.method != http.MethodPost || f.path != "/orders" || f.idempotency != "c-1" || !strings.HasPrefix(f.auth, "Basic ") {
		t.Fatalf("pedido: %s %s idempotency=%q auth=%q", f.method, f.path, f.idempotency, f.auth)
	}
	pay := f.body["payments"].([]any)[0].(map[string]any)
	if pay["payment_method"] != "pix" || pay["pix"].(map[string]any)["expires_in"] != float64(3600) {
		t.Fatalf("pagamento = %v", pay)
	}
	if split := pay["split"].([]any)[0].(map[string]any); split["recipient_id"] != "rp_af" || split["amount"] != float64(699) {
		t.Fatalf("split = %v", split)
	}
	if item := f.body["items"].([]any)[0].(map[string]any); item["amount"] != float64(4990) || f.body["closed"] != true {
		t.Fatalf("item = %v", f.body)
	}
	ch := order.FirstCharge()
	if ch == nil || ch.ID != "ch_1" || ch.LastTransaction.QRCode != "00020101..." || ch.Outcome() != OutcomePending {
		t.Fatalf("cobrança = %+v", ch)
	}
	if !c.TestMode() || NewClient("", "sk_live").TestMode() {
		t.Fatal("modo de testes pela chave")
	}
}

func TestCardOrderAndSavedCard(t *testing.T) {
	f := &fake{reply: `{"id":"or_2","code":"c-2","charges":[{"id":"ch_2","status":"failed","payment_method":"credit_card",
		"last_transaction":{"status":"not_authorized","acquirer_message":"Transação não autorizada","card":{"id":"card_1","last_four_digits":"0010","brand":"Visa"}}}]}`}
	c := NewClient(f.server(t).URL, "sk_test_abc")
	order, err := c.CreateOrder(context.Background(), OrderRequest{
		Code: "c-2", AmountCents: 6990, Method: MethodCreditCard, CardToken: "token_x", StatementDescriptor: "FARBO",
		BillingAddress: &Address{Line1: "10, Rua A, Centro", ZipCode: "01001000", City: "São Paulo", State: "SP", Country: "BR"},
		Customer:       &Customer{Name: "Ana"},
	})
	if err != nil {
		t.Fatal(err)
	}
	card := f.body["payments"].([]any)[0].(map[string]any)["credit_card"].(map[string]any)
	if card["card_token"] != "token_x" || card["operation_type"] != "auth_and_capture" || card["installments"] != float64(1) ||
		card["card"].(map[string]any)["billing_address"].(map[string]any)["zip_code"] != "01001000" {
		t.Fatalf("cartão = %v", card)
	}
	ch := order.FirstCharge()
	if ch.Outcome() != OutcomeFailed || ch.LastTransaction.FailureMessage() != "Transação não autorizada" {
		t.Fatalf("recusa = %+v", ch.LastTransaction)
	}

	// O cartão salvo: pelo id, com o cliente da Pagar.me.
	if _, err := c.CreateOrder(context.Background(), OrderRequest{Code: "c-3", AmountCents: 100, Method: MethodCreditCard,
		CardID: "card_1", CustomerID: "cus_1"}); err != nil {
		t.Fatal(err)
	}
	card = f.body["payments"].([]any)[0].(map[string]any)["credit_card"].(map[string]any)
	if card["card_id"] != "card_1" || card["card"] != nil || f.body["customer_id"] != "cus_1" || f.body["customer"] != nil {
		t.Fatalf("cartão salvo = %v", f.body)
	}
}

func TestBoletoChargeLookupAndRefund(t *testing.T) {
	f := &fake{reply: `{"id":"or_3","charges":[{"id":"ch_3","status":"pending","payment_method":"boleto",
		"last_transaction":{"status":"generated","line":"34191.75462","pdf":"https://api.pagar.me/pdf","due_at":"2026-11-10T00:00:00Z"}}]}`}
	c := NewClient(f.server(t).URL, "sk_test_abc")
	due := time.Date(2026, 11, 10, 3, 0, 0, 0, time.UTC)
	if _, err := c.CreateOrder(context.Background(), OrderRequest{Code: "c-4", AmountCents: 6990, Method: MethodBoleto,
		BoletoDueAt: due, BoletoInstructions: "Não receber após o vencimento"}); err != nil {
		t.Fatal(err)
	}
	if boleto := f.body["payments"].([]any)[0].(map[string]any)["boleto"].(map[string]any); boleto["due_at"] != "2026-11-10T03:00:00Z" {
		t.Fatalf("boleto = %v", boleto)
	}

	f.reply = `{"id":"ch_3","status":"paid","payment_method":"boleto","paid_at":"2026-11-05T12:00:00Z","paid_amount":6990}`
	ch, err := c.GetCharge(context.Background(), "ch_3")
	if err != nil || f.method != http.MethodGet || f.path != "/charges/ch_3" || ch.Outcome() != OutcomePaid {
		t.Fatalf("consulta: %s %s %+v %v", f.method, f.path, ch, err)
	}
	f.reply = `{"id":"ch_3","status":"canceled","payment_method":"pix","paid_at":"2026-11-05T12:00:00Z","paid_amount":6990}`
	ch, err = c.CancelCharge(context.Background(), "ch_3", "refund-1")
	if err != nil || f.method != http.MethodDelete || f.idempotency != "refund-1" || ch.Outcome() != OutcomeRefunded {
		t.Fatalf("estorno: %s %+v %v", f.method, ch, err)
	}

	f.status, f.reply = http.StatusUnprocessableEntity, `{"message":"The request is invalid.","errors":{"customer.document":["inválido"]}}`
	_, err = c.GetCharge(context.Background(), "ch_x")
	var apiErr *APIError
	if !asAPIError(err, &apiErr) || apiErr.HTTPStatus != 422 || apiErr.Message != "The request is invalid.; customer.document: inválido" {
		t.Fatalf("erro = %v", err)
	}
}

func asAPIError(err error, target **APIError) bool {
	e, ok := err.(*APIError)
	if ok {
		*target = e
	}
	return ok
}

func TestOutcome(t *testing.T) {
	paidAt := time.Now()
	cases := []struct {
		charge Charge
		want   string
	}{
		{Charge{Status: "pending", PaymentMethod: MethodPix}, OutcomePending},
		{Charge{Status: "processing", PaymentMethod: MethodCreditCard}, OutcomePending},
		{Charge{Status: "underpaid", PaymentMethod: MethodBoleto}, OutcomePending},
		{Charge{Status: "overpaid", PaymentMethod: MethodBoleto}, OutcomePaid},
		{Charge{Status: "paid", PaymentMethod: MethodPix, LastTransaction: &Transaction{Status: "pending_refund"}}, OutcomeRefunding},
		{Charge{Status: "canceled", PaymentMethod: MethodPix}, OutcomeExpired},
		{Charge{Status: "failed", PaymentMethod: MethodPix}, OutcomeExpired},
		{Charge{Status: "canceled", PaymentMethod: MethodBoleto}, OutcomeCanceled},
		{Charge{Status: "canceled", PaymentMethod: MethodCreditCard, PaidAt: &paidAt}, OutcomeRefunded},
		{Charge{Status: "chargedback", PaymentMethod: MethodCreditCard}, OutcomeRefunded},
		{Charge{Status: "failed", PaymentMethod: MethodCreditCard}, OutcomeFailed},
	}
	for _, c := range cases {
		if got := c.charge.Outcome(); got != c.want {
			t.Errorf("%s/%s = %s, queria %s", c.charge.PaymentMethod, c.charge.Status, got, c.want)
		}
	}
}

func TestWebhookEvent(t *testing.T) {
	ev, err := ParseEvent([]byte(`{"id":"hook_1","type":"charge.paid","data":{"id":"ch_1","status":"paid"}}`))
	if err != nil || len(ev.ChargeIDs()) != 1 || ev.ChargeIDs()[0] != "ch_1" {
		t.Fatalf("charge.paid = %+v %v", ev, err)
	}
	ev, _ = ParseEvent([]byte(`{"id":"hook_2","type":"order.paid","data":{"id":"or_1","charges":[{"id":"ch_1"},{"id":"ch_2"}]}}`))
	if ids := ev.ChargeIDs(); len(ids) != 2 || ids[1] != "ch_2" {
		t.Fatalf("order.paid = %v", ids)
	}
	ev, _ = ParseEvent([]byte(`{"id":"hook_3","type":"recipient.updated","data":{"id":"rp_1","status":"active"}}`))
	if ev.RecipientID() != "rp_1" || len(ev.ChargeIDs()) != 0 {
		t.Fatalf("recipient.updated = %+v", ev)
	}
	if _, err := ParseEvent([]byte(`{"type":"charge.paid"}`)); err == nil {
		t.Fatal("evento sem id passou")
	}
	if !VerifySecret("abc", "abc") || VerifySecret("abc", "") || VerifySecret("", "") || VerifySecret("abd", "abc") {
		t.Fatal("segredo do webhook")
	}
}

func TestRecipientBalance(t *testing.T) {
	f := &fake{reply: `{"currency":"BRL","available_amount":123456,"waiting_funds_amount":15990,"transferred_amount":50000}`}
	c := NewClient(f.server(t).URL, "sk_test_abc")
	b, err := c.Balance(context.Background(), "rp_farbo")
	if err != nil || f.method != http.MethodGet || f.path != "/recipients/rp_farbo/balance" {
		t.Fatalf("consulta: %s %s %v", f.method, f.path, err)
	}
	if b.AvailableAmount != 123456 || b.WaitingFundsAmount != 15990 || b.TransferredAmount != 50000 {
		t.Errorf("saldo = %+v", b)
	}
}
