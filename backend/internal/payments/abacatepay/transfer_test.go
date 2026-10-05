package abacatepay

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
)

func TestSendPixSendsDocumentedPayload(t *testing.T) {
	api := &fakeAPI{respond: func(w http.ResponseWriter) {
		_, _ = io.WriteString(w, `{"data":{"id":"tran_abc123","status":"COMPLETE","devMode":true,
			"receiptUrl":"https://app.abacatepay.com/receipt/tran_abc123","amount":45000,"platformFee":80,
			"externalId":"farbo-pix-1","createdAt":"2026-10-05T18:38:28.573Z","updatedAt":"2026-10-05T18:38:28.573Z"},
			"error":null,"success":true}`)
	}}
	client := NewClient(api.server(t).URL, "abc_dev_chave")
	got, err := client.SendPix(context.Background(), TransferRequest{
		AmountCents: 45000, ExternalID: "farbo-pix-1", Description: "Honorários — outubro 🧾",
		Key: "52998224725", KeyType: KeyCPF,
	})
	if err != nil {
		t.Fatal(err)
	}
	if api.lastPath != "/pix/send" || api.lastAuth != "Bearer abc_dev_chave" {
		t.Errorf("chamada = %s %s", api.lastPath, api.lastAuth)
	}
	pix, _ := api.lastBody["pix"].(map[string]any)
	if api.lastBody["amount"] != float64(45000) || api.lastBody["externalId"] != "farbo-pix-1" ||
		api.lastBody["description"] != "Honorários - outubro" || pix["key"] != "52998224725" || pix["type"] != "CPF" {
		t.Errorf("corpo = %+v", api.lastBody)
	}
	if got.ID != "tran_abc123" || got.Status != TransferComplete || got.PlatformFee != 80 || !got.DevMode ||
		got.ReceiptURL == "" || got.Amount != 45000 {
		t.Errorf("envio = %+v", got)
	}
}

func TestTransferLookupAndBalance(t *testing.T) {
	api := &fakeAPI{respond: func(w http.ResponseWriter) {
		_, _ = io.WriteString(w, `{"data":{"id":"tran_x","status":"FAILED","amount":100,"platformFee":0},"success":true,"error":null}`)
	}}
	client := NewClient(api.server(t).URL, "k")
	got, err := client.GetTransfer(context.Background(), "tran_x")
	if err != nil || got.Status != TransferFailed || api.lastPath != "/pix/get" || api.lastQuery != "id=tran_x" {
		t.Errorf("consulta = %+v %v (%s?%s)", got, err, api.lastPath, api.lastQuery)
	}
	api.respond = func(w http.ResponseWriter) {
		_, _ = io.WriteString(w, `{"success":true,"data":{"id":"store_1","name":"Farbo","balance":{"available":509460,"pending":10,"blocked":5}},"error":null}`)
	}
	balance, err := client.Balance(context.Background())
	if err != nil || balance.Available != 509460 || balance.Pending != 10 || balance.Blocked != 5 || api.lastPath != "/stores/get" {
		t.Errorf("saldo = %+v %v", balance, err)
	}
}

func TestTransferErrors(t *testing.T) {
	status := http.StatusBadRequest
	api := &fakeAPI{respond: func(w http.ResponseWriter) {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, `{"success":false,"data":null,"error":"Saldo insuficiente"}`)
	}}
	client := NewClient(api.server(t).URL, "k")
	_, err := client.SendPix(context.Background(), TransferRequest{AmountCents: 100, ExternalID: "x", Key: "a@b.c", KeyType: KeyEmail})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Message != "Saldo insuficiente" || !IsDefinitive(err) {
		t.Errorf("recusa = %v (definitiva: %v)", err, IsDefinitive(err))
	}
	// Erro do servidor dela ou da rede: o Pix pode ter saído.
	status = http.StatusBadGateway
	_, err = client.SendPix(context.Background(), TransferRequest{AmountCents: 100, ExternalID: "x", Key: "a@b.c", KeyType: KeyEmail})
	if err == nil || IsDefinitive(err) {
		t.Errorf("502 tratado como recusa: %v", err)
	}
	if IsDefinitive(errors.New("dial tcp: i/o timeout")) {
		t.Error("falha de rede tratada como recusa")
	}
	ev := &Event{ID: "e1", Event: "transfer.failed", Data: []byte(`{"transfer":{"id":"tran_9","externalId":"farbo-pix-1"},"pix_char_x":"pix_char_1"}`)}
	if ids := ev.TransferIDs(); len(ids) != 1 || ids[0] != "tran_9" || !ev.IsTransferEvent() {
		t.Errorf("evento = %v %v", ids, ev.IsTransferEvent())
	}
	if (&Event{Event: "transparent.completed"}).IsTransferEvent() {
		t.Error("pagamento recebido tratado como envio")
	}
}
