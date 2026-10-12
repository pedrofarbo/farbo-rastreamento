package abacatepay

import (
	"context"
	"io"
	"net/http"
	"testing"
)

// O boleto como a AbacatePay devolve (resposta real do sandbox, out/2026): a
// linha digitável vem em identificationNumber, e o Pix junto.
func TestCreateBoletoSendsPayloadAndReadsLine(t *testing.T) {
	api := &fakeAPI{respond: func(w http.ResponseWriter) {
		_, _ = io.WriteString(w, `{"data":{"id":"bole_abc","amount":4990,"status":"PENDING","devMode":true,
			"barCode":"00191878900000050000000002759288002193297817",
			"identificationNumber":"00190000090275928800021932978170187890000005000","providerNumber":"0000",
			"url":"https://app.abacatepay.com/pay/bole_abc/boleto","brCode":"00020101021126580014BR.GOV.BCB.PIX",
			"brCodeBase64":"data:image/png;base64,AAA","platformFee":250,"receiptUrl":null,
			"expiresAt":"2026-10-20T23:59:59.715Z","createdAt":"2026-10-12T22:42:43.715Z","metadata":{"invoiceId":"inv-1"}},
			"success":true,"error":null}`)
	}}
	client := NewClient(api.server(t).URL, "abc_dev_chave")
	boleto, err := client.CreateBoleto(context.Background(), BoletoRequest{
		AmountCents: 4990, Description: "Farbo Rastreadores — Plano Mensal · R$ 49,90", DueDate: "2026-10-20",
		ExternalID: "charge-1", Customer: BoletoCustomer{Name: "Maria", TaxID: "52998224725"},
		Metadata: map[string]string{"invoiceId": "inv-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := api.lastBody["data"].(map[string]any)
	customer, _ := data["customer"].(map[string]any)
	if api.lastBody["method"] != "BOLETO" || data["dueDate"] != "2026-10-20" || data["amount"] != 4990.0 ||
		data["description"] != "Farbo Rastreadores - Plano Mensal - 49,90 reais" || customer["taxId"] != "52998224725" {
		t.Fatalf("corpo = %+v", api.lastBody)
	}
	// Sem e-mail e celular, os campos não vão (no boleto são opcionais).
	if _, ok := customer["email"]; ok {
		t.Errorf("e-mail vazio foi: %+v", customer)
	}
	if boleto.ID != "bole_abc" || boleto.Line != "00190000090275928800021932978170187890000005000" || len(boleto.BarCode) != 44 ||
		boleto.URL == "" || boleto.BrCode == "" || boleto.PlatformFee != 250 || boleto.ExpiresAt == nil {
		t.Fatalf("boleto = %+v", boleto)
	}
}

func TestGetChargeUsesTransparentGet(t *testing.T) {
	api := &fakeAPI{respond: func(w http.ResponseWriter) {
		_, _ = io.WriteString(w, `{"data":{"id":"bole_abc","status":"PAID","expiresAt":null},"success":true,"error":null}`)
	}}
	status, err := NewClient(api.server(t).URL, "k").GetCharge(context.Background(), "bole_abc")
	if err != nil || status.Status != StatusPaid || api.lastPath != "/transparents/get" || api.lastQuery != "id=bole_abc" {
		t.Fatalf("status=%+v err=%v caminho=%s?%s", status, err, api.lastPath, api.lastQuery)
	}
}

func TestEventChargeIDsIncludeBoleto(t *testing.T) {
	ev, err := ParseEvent([]byte(`{"id":"log_1","event":"transparent.completed","data":{"transparent":{"id":"bole_abc"},"payment":{"id":"pix_char_x"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	ids := ev.ChargeIDs()
	if len(ids) != 2 {
		t.Fatalf("ids = %v", ids)
	}
}
