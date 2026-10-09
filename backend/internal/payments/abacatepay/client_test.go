package abacatepay

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// fakeAPI imita a AbacatePay: confere a chave e guarda a última requisição.
type fakeAPI struct {
	lastPath  string
	lastQuery string
	lastBody  map[string]any
	lastAuth  string
	respond   func(w http.ResponseWriter)
}

func (f *fakeAPI) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.lastPath, f.lastQuery, f.lastAuth = r.URL.Path, r.URL.RawQuery, r.Header.Get("Authorization")
		f.lastBody = nil
		if raw, _ := io.ReadAll(r.Body); len(raw) > 0 {
			_ = json.Unmarshal(raw, &f.lastBody)
		}
		w.Header().Set("Content-Type", "application/json")
		f.respond(w)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestCreatePixSendsDocumentedPayload(t *testing.T) {
	api := &fakeAPI{respond: func(w http.ResponseWriter) {
		_, _ = io.WriteString(w, `{"data":{"id":"pix_char_1","amount":6990,"status":"PENDING","devMode":true,
			"brCode":"000201...","brCodeBase64":"data:image/png;base64,AAA","platformFee":80,
			"expiresAt":"2026-09-30T18:00:00.000Z","createdAt":"2026-09-29T18:00:00.000Z","metadata":{"invoiceId":"inv-1"}},
			"success":true,"error":null}`)
	}}
	client := NewClient(api.server(t).URL, "abc_dev_chave")

	pix, err := client.CreatePix(context.Background(), PixRequest{
		AmountCents: 6990, Description: "Plano Mensal — outubro/2026", ExpiresIn: 24 * time.Hour,
		ExternalID: "charge-1", Metadata: map[string]string{"invoiceId": "inv-1"},
		Customer: &Customer{Name: "Maria", Email: "m@x.com", TaxID: "52998224725", Cellphone: "11999990000"},
	})
	if err != nil {
		t.Fatalf("CreatePix: %v", err)
	}
	if api.lastPath != "/transparents/create" || api.lastAuth != "Bearer abc_dev_chave" {
		t.Fatalf("caminho=%q auth=%q", api.lastPath, api.lastAuth)
	}
	if api.lastBody["method"] != "PIX" {
		t.Fatalf("method = %v, esperado PIX", api.lastBody["method"])
	}
	data, _ := api.lastBody["data"].(map[string]any)
	if data["amount"] != float64(6990) || data["expiresIn"] != float64(86400) || data["externalId"] != "charge-1" {
		t.Fatalf("data = %v", data)
	}
	customer, _ := data["customer"].(map[string]any)
	if customer["taxId"] != "52998224725" || customer["cellphone"] != "11999990000" {
		t.Fatalf("customer = %v", customer)
	}
	if pix.ID != "pix_char_1" || pix.Status != StatusPending || !pix.DevMode || pix.ExpiresAt == nil {
		t.Fatalf("pix = %+v", pix)
	}
}

func TestCreatePixOmitsOptionalFields(t *testing.T) {
	api := &fakeAPI{respond: func(w http.ResponseWriter) {
		_, _ = io.WriteString(w, `{"data":{"id":"pix_char_2","amount":100,"status":"PENDING"},"success":true,"error":null}`)
	}}
	client := NewClient(api.server(t).URL, "k")
	if _, err := client.CreatePix(context.Background(), PixRequest{AmountCents: 100}); err != nil {
		t.Fatal(err)
	}
	data, _ := api.lastBody["data"].(map[string]any)
	for _, field := range []string{"customer", "expiresIn", "description", "externalId", "metadata"} {
		if _, present := data[field]; present {
			t.Errorf("campo %q não deveria ir vazio", field)
		}
	}
}

func TestCheckAndSimulateUseQueryID(t *testing.T) {
	api := &fakeAPI{respond: func(w http.ResponseWriter) {
		_, _ = io.WriteString(w, `{"data":{"id":"pix_char_3","status":"PAID"},"success":true,"error":null}`)
	}}
	client := NewClient(api.server(t).URL, "k")

	status, err := client.CheckPix(context.Background(), "pix_char_3")
	if err != nil || status.Status != StatusPaid {
		t.Fatalf("CheckPix: %+v %v", status, err)
	}
	if api.lastPath != "/transparents/check" || api.lastQuery != "id=pix_char_3" {
		t.Fatalf("check: caminho=%q query=%q", api.lastPath, api.lastQuery)
	}

	if _, err := client.SimulatePayment(context.Background(), "pix_char_3"); err != nil {
		t.Fatal(err)
	}
	if api.lastPath != "/transparents/simulate-payment" || api.lastQuery != "id=pix_char_3" {
		t.Fatalf("simulate: caminho=%q query=%q", api.lastPath, api.lastQuery)
	}
}

func TestErrorsBecomeAPIError(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"chave inválida", 401, `{"success":false,"data":null,"error":"Invalid or inactive API key"}`, "Invalid or inactive API key"},
		{"CPF inválido", 400, `{"success":false,"data":null,"error":"Invalid taxId"}`, "Invalid taxId"},
		{"erro como objeto", 422, `{"success":false,"data":null,"error":{"message":"amount too low"}}`, "amount too low"},
		{"200 com success=false", 200, `{"success":false,"data":null,"error":"Pix QR Code not found"}`, "Pix QR Code not found"},
		{"corpo que não é JSON", 502, `<html>bad gateway</html>`, "resposta fora do formato esperado"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api := &fakeAPI{respond: func(w http.ResponseWriter) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}}
			_, err := NewClient(api.server(t).URL, "k").CheckPix(context.Background(), "x")
			var apiErr *APIError
			if !errors.As(err, &apiErr) || apiErr.Message != tc.want || apiErr.HTTPStatus != tc.status {
				t.Fatalf("err = %v, esperado APIError %d %q", err, tc.status, tc.want)
			}
		})
	}
	if !IsNotFound(&APIError{HTTPStatus: 400, Message: "Pix QR Code not found"}) {
		t.Error("IsNotFound deveria reconhecer a mensagem da AbacatePay")
	}
}

func TestIsDevKey(t *testing.T) {
	if !IsDevKey("abc_dev_123") || IsDevKey("abc_prod_123") || IsDevKey("") {
		t.Fatal("IsDevKey errado")
	}
}

func TestSanitizeDescription(t *testing.T) {
	cases := map[string]string{
		"Farbo Rastreadores \u2014 Plano Mensal \u2014 mar\u00e7o/2026":        "Farbo Rastreadores - Plano Mensal - mar\u00e7o/2026",
		"Instala\u00e7\u00e3o (a\u00e7\u00e3o \u00fanica) 100% ok, #1 & \"x\"": "Instala\u00e7\u00e3o (a\u00e7\u00e3o \u00fanica) 100% ok, #1 & \"x\"",
		"en \u2013 dash e \u201caspas\u201d curvas":                            "en - dash e \"aspas\" curvas",
		"Moto \U0001F697 da Carla \u2764\uFE0F":                                "Moto da Carla",
		"quebra\nde   linha\tsobrando  ":                                       "quebra de linha sobrando",
		// A fatura que travou o Pix em produção ("·" e "$" não passam).
		"Farbo Rastreadores \u2014 Rastreador J16 GT06 \u00b7 promo\u00e7\u00e3o + frete Jadlog (R$ 20,00)": "Farbo Rastreadores - Rastreador J16 GT06 - promo\u00e7\u00e3o + frete Jadlog (20,00 reais)",
		"rastreador de R$ 1.020,00 em 10x \u2022 1\u00aa parcela":                                           "rastreador de 1.020,00 reais em 10x - 1\u00aa parcela",
		"<b>[x]</b> {y} | a\\b ^~` \u00a7 2\u00d73 30\u00b0 \u20ac5 R$ US$ ok!?*=_@;":                       "bx/b y ab 2x3 30\u00ba 5 R US ok!?*=_@;",
	}
	for input, want := range cases {
		if got := sanitizeDescription(input); got != want {
			t.Errorf("sanitizeDescription(%q) = %q, esperado %q", input, got, want)
		}
	}
	plain := plainDescription("Farbo Rastreadores \u2014 Especial Insanos MC \u2014 outubro/2026 \u00b7 promo\u00e7\u00e3o de pr\u00e9-lan\u00e7amento + parcela 1/10")
	if plain != "Farbo Rastreadores - Especial Insanos MC - outubro/2026 - promocao de pre-lancamento parcela 1/10" {
		t.Errorf("plainDescription = %q", plain)
	}
}

// Um caractere que a lista não conhece: o Pix sai de novo, com a descrição
// mínima, em vez de falhar para o cliente.
func TestCreatePixRetriesWithPlainDescription(t *testing.T) {
	var descriptions []any
	api := &fakeAPI{}
	api.respond = func(w http.ResponseWriter) {
		data, _ := api.lastBody["data"].(map[string]any)
		descriptions = append(descriptions, data["description"])
		if len(descriptions) == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"data":null,"success":false,"error":"Disallowed character in description: \"\u00e7\" (U+00E7). Remove this character."}`)
			return
		}
		_, _ = io.WriteString(w, `{"data":{"id":"pix_char_3","amount":100,"status":"PENDING"},"success":true,"error":null}`)
	}
	pix, err := NewClient(api.server(t).URL, "k").CreatePix(context.Background(), PixRequest{AmountCents: 100, Description: "Instala\u00e7\u00e3o + chip"})
	if err != nil || pix.ID != "pix_char_3" {
		t.Fatalf("CreatePix = %+v, %v", pix, err)
	}
	if len(descriptions) != 2 || descriptions[0] != "Instala\u00e7\u00e3o + chip" || descriptions[1] != "Instalacao chip" {
		t.Errorf("descrições enviadas = %q", descriptions)
	}
}

func TestRefundPixAcceptsBothResponseShapes(t *testing.T) {
	// Formato que a API devolve de fato.
	api := &fakeAPI{respond: func(w http.ResponseWriter) {
		_, _ = io.WriteString(w, `{"success":true,"data":{"id":"tran_abc","status":"COMPLETE","amount":6990,
			"reason":"duplicidade","originalId":"pix_char_1","createdAt":"2026-09-29T18:49:32.107Z"},"error":null}`)
	}}
	refund, err := NewClient(api.server(t).URL, "k").RefundPix(context.Background(), "pix_char_1", "  duplicidade ")
	if err != nil {
		t.Fatal(err)
	}
	if api.lastPath != "/transparents/refund" || api.lastBody["id"] != "pix_char_1" || api.lastBody["reason"] != "duplicidade" {
		t.Fatalf("requisição: caminho=%q corpo=%v", api.lastPath, api.lastBody)
	}
	if refund.RefundID() != "tran_abc" || refund.Status != "COMPLETE" || refund.OriginalID != "pix_char_1" {
		t.Fatalf("estorno = %+v", refund)
	}

	// Formato da documentação.
	api.respond = func(w http.ResponseWriter) {
		_, _ = io.WriteString(w, `{"success":true,"data":{"refundPublicId":"tran_doc"},"error":null}`)
	}
	refund, err = NewClient(api.server(t).URL, "k").RefundPix(context.Background(), "pix_char_1", "")
	if err != nil || refund.RefundID() != "tran_doc" {
		t.Fatalf("formato da documentação: %+v %v", refund, err)
	}
	if _, present := api.lastBody["reason"]; present {
		t.Error("motivo vazio não deveria ir na requisição")
	}
}
