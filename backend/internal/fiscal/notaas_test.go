package fiscal

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// O cliente da Notaas: a emissão (o corpo como a documentação pede, com a
// chave no x-api-key), a consulta, a recusa por limite do plano, o PDF no
// redirecionamento para o CDN e a assinatura do webhook.
func TestNotaasClient(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "ntaas_chave" && !strings.HasPrefix(r.URL.Path, "/cdn/") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.Method + " " + r.URL.Path {
		case "POST /api/v1/emitir":
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &got)
			if got["referencia"] == "sem-credito" {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"error":"Créditos esgotados","errorCode":"CREDIT_LIMIT","creditsUsed":20,"creditsLimit":20}`))
				return
			}
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"queued":true,"invoiceId":"inv_abc123","status":"queued"}`))
		case "GET /api/v1/invoices/inv_abc123/status":
			_, _ = w.Write([]byte(`{"invoiceId":"inv_abc123","status":"issued","chNFSe":"3526","numeroNfe":"1234",
				"emittedAt":"2026-11-05T11:00:00.000Z","pdfUrl":"https://documents.notaas.com.br/pdf/inv_abc123.pdf"}`))
		case "GET /api/v1/invoices/inv_err/status":
			_, _ = w.Write([]byte(`{"invoiceId":"inv_err","status":"error","errorCode":"E0312","errorMessage":"Tomador inválido",
				"errors":[{"Codigo":"E0312","Descricao":"CPF do tomador","Complemento":""}]}`))
		case "GET /api/v1/invoices/inv_abc123/pdf":
			http.Redirect(w, r, "/cdn/pdf/inv_abc123.pdf", http.StatusFound)
		case "GET /api/v1/invoices/inv_abc123/xml":
			http.Redirect(w, r, "/cdn/xml/inv_abc123.xml", http.StatusFound)
		case "GET /api/v1/invoices/inv_semcdn/xml":
			// Fora do CDN: a Notaas responde com o XML.
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write([]byte(`<?xml version="1.0"?><NFSe>banco</NFSe>`))
		default:
			if r.URL.Path == "/cdn/pdf/inv_abc123.pdf" || r.URL.Path == "/cdn/xml/inv_abc123.xml" {
				// O CDN: sem a chave da API.
				if r.Header.Get("x-api-key") != "" {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if strings.HasSuffix(r.URL.Path, ".xml") {
					_, _ = w.Write([]byte(`<?xml version="1.0"?><NFSe>cdn</NFSe>`))
					return
				}
				_, _ = w.Write([]byte("%PDF-1.4 danfse"))
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	c := NewClient(srv.URL+"/api/v1", "ntaas_chave")

	id, err := c.Emit(ctx, EmitRequest{
		Reference: "fatura-1", Code: "010700", Text: "Licença de uso do software de rastreamento veicular Farbo — Plano Mensal", Total: 69.90, ISSRate: 2,
		Month: "2026-11", Taker: Taker{CPF: "52998224725", Name: "Ana Souza", Email: "ana@farbo.test",
			Address: &Address{Street: "Rua Augusta", Number: "500", District: "Consolação", City: "São Paulo", State: "SP", ZipCode: "01305000"}},
	})
	if err != nil || id != "inv_abc123" {
		t.Fatalf("emissão = %q %v", id, err)
	}
	taker, _ := got["tomador"].(map[string]any)
	address, _ := taker["endereco"].(map[string]any)
	service, _ := got["servico"].(map[string]any)
	values, _ := got["valores"].(map[string]any)
	if got["referencia"] != "fatura-1" || got["competencia"] != "2026-11" || taker["cpf"] != "52998224725" || taker["cnpj"] != nil ||
		address["cidade"] != "São Paulo" || address["uf"] != "SP" || service["codigo"] != "010700" ||
		values["total"] != 69.9 || values["aliquotaIss"] != 2.0 {
		t.Errorf("corpo da emissão = %v", got)
	}

	_, err = c.Emit(ctx, EmitRequest{Reference: "sem-credito", Taker: Taker{CPF: "52998224725", Name: "Ana"}, Text: "x", Total: 1})
	var api *APIError
	if !errors.As(err, &api) || !api.CreditLimit() || api.Temporary() || api.Status != http.StatusForbidden {
		t.Errorf("limite do plano = %v", err)
	}

	st, err := c.Status(ctx, "inv_abc123")
	if err != nil || st.Status != notaasIssued || st.Number != "1234" || st.VerificationCode != "3526" || st.EmittedAt == nil {
		t.Errorf("consulta = %+v %v", st, err)
	}
	st, _ = c.Status(ctx, "inv_err")
	if st.Status != notaasError || st.Problem() != "Tomador inválido; E0312 CPF do tomador" {
		t.Errorf("rejeição = %q", st.Problem())
	}
	pdf, err := c.PDF(ctx, "inv_abc123")
	if err != nil || string(pdf.Body) != "%PDF-1.4 danfse" {
		t.Errorf("PDF = %+v %v", pdf, err)
	}
	// O XML: do CDN (sem a chave) ou direto da Notaas; o que não existe, recusado.
	if xml, err := c.XML(ctx, "inv_abc123"); err != nil || string(xml.Body) != `<?xml version="1.0"?><NFSe>cdn</NFSe>` {
		t.Errorf("XML do CDN = %+v %v", xml, err)
	}
	if xml, err := c.XML(ctx, "inv_semcdn"); err != nil || string(xml.Body) != `<?xml version="1.0"?><NFSe>banco</NFSe>` {
		t.Errorf("XML direto = %+v %v", xml, err)
	}
	if _, err := c.XML(ctx, "inv_nada"); !errors.As(err, &api) || api.Status != http.StatusNotFound {
		t.Errorf("XML que não existe = %v", err)
	}
	if _, err := NewClient(srv.URL+"/api/v1", "outra").Status(ctx, "inv_abc123"); !errors.As(err, &api) || api.Status != 401 {
		t.Errorf("chave errada = %v", err)
	}

	body := []byte(`{"event":"nfse.issued","data":{"invoiceId":"inv_abc123"}}`)
	mac := hmac.New(sha256.New, []byte("segredo"))
	mac.Write(body)
	signature := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if !VerifySignature("segredo", body, signature) || VerifySignature("segredo", body, "sha256=00") ||
		VerifySignature("", body, signature) || VerifySignature("segredo", append(body, ' '), signature) {
		t.Error("assinatura do webhook")
	}
	if w, err := ParseWebhook(body); err != nil || w.Event != "nfse.issued" || w.Data.InvoiceID != "inv_abc123" {
		t.Errorf("webhook = %+v %v", w, err)
	}
}
