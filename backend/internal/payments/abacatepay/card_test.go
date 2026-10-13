package abacatepay

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"
)

// A cobrança no cartão como a documentação mostra: o token do formulário na
// primeira vez, o cardToken (salvo) depois; a resposta traz o cardToken, a
// bandeira e os 4 últimos dígitos.
func TestCreateCard(t *testing.T) {
	api := &fakeAPI{respond: func(w http.ResponseWriter) {
		_, _ = io.WriteString(w, `{"data":{"id":"card_Wk3a","amount":4700,"status":"APPROVED","devMode":true,
			"cardToken":"dev_card_4242","brand":"VISA","lastFour":"4242","receiptUrl":null,
			"createdAt":"2026-10-12T14:20:31.512Z","updatedAt":"2026-10-12T14:20:31.512Z",
			"expiresAt":"2026-10-12T23:59:59.999Z","metadata":{"chargeId":"c1"}},"success":true,"error":null}`)
	}}
	client := NewClient(api.server(t).URL, "abc_dev_chave")
	customer := Customer{Name: "Maria", Email: "m@x.com", TaxID: "52998224725", Cellphone: "11999990000"}

	card, err := client.CreateCard(context.Background(), CardRequest{AmountCents: 4700, Token: "ctok_1", Customer: customer,
		Metadata: map[string]string{"chargeId": "c1"}})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := api.lastBody["data"].(map[string]any)
	if api.lastBody["method"] != "CARD" || data["token"] != "ctok_1" || data["cardToken"] != nil || data["installmentCount"] != nil ||
		data["description"] != nil || data["externalId"] != nil {
		t.Fatalf("corpo = %+v", api.lastBody)
	}
	if card.ID != "card_Wk3a" || card.Status != StatusApproved || card.CardToken != "dev_card_4242" || card.Brand != "VISA" || card.LastFour != "4242" {
		t.Fatalf("cartão = %+v", card)
	}

	// O cartão salvo.
	if _, err := client.CreateCard(context.Background(), CardRequest{AmountCents: 30000, CardToken: "dev_card_4242", Customer: customer}); err != nil {
		t.Fatal(err)
	}
	data, _ = api.lastBody["data"].(map[string]any)
	if data["cardToken"] != "dev_card_4242" || data["token"] != nil {
		t.Fatalf("corpo do salvo = %+v", api.lastBody)
	}
}

// A recusa volta como erro da API, com a mensagem dela.
func TestCreateCardDeclined(t *testing.T) {
	api := &fakeAPI{respond: func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"data":null,"success":false,"error":"Não foi possível processar o pagamento com este cartão"}`)
	}}
	_, err := NewClient(api.server(t).URL, "k").CreateCard(context.Background(), CardRequest{AmountCents: 100, CardToken: "x"})
	if apiErr, ok := err.(*APIError); !ok || apiErr.Message != "Não foi possível processar o pagamento com este cartão" {
		t.Fatalf("erro = %v", err)
	}
}

// Depois de um erro de rede, a cobrança é procurada pela metadata, página a
// página.
func TestFindCard(t *testing.T) {
	pages := []string{
		`{"data":[{"id":"card_1","status":"APPROVED","metadata":{"debitId":"outro"}}],"success":true,"error":null,"pagination":{"hasMore":true,"next":"card_1"}}`,
		`{"data":[{"id":"card_2","status":"APPROVED","metadata":{"debitId":"d-42"}}],"success":true,"error":null,"pagination":{"hasMore":false,"next":null}}`,
	}
	n := 0
	api := &fakeAPI{respond: func(w http.ResponseWriter) {
		_, _ = io.WriteString(w, pages[n])
		n++
	}}
	client := NewClient(api.server(t).URL, "k")
	card, err := client.FindCard(context.Background(), time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC), "debitId", "d-42")
	if err != nil || card == nil || card.ID != "card_2" {
		t.Fatalf("achou %+v, %v", card, err)
	}
	if api.lastPath != "/transparents/list" || api.lastQuery != "after=card_1&limit=100&method=CARD&startDate=2026-10-12" {
		t.Errorf("consulta = %s?%s", api.lastPath, api.lastQuery)
	}
}
