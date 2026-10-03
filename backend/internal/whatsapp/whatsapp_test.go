package whatsapp

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
	"testing"
)

func sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestVerifySignature(t *testing.T) {
	body := []byte(`{"object":"whatsapp_business_account"}`)
	if !VerifySignature("segredo", body, sign("segredo", body)) {
		t.Fatal("assinatura válida recusada")
	}
	for name, header := range map[string]string{
		"outra chave":     sign("outro", body),
		"sem prefixo":     sign("segredo", body)[len("sha256="):],
		"vazio":           "",
		"hex inválido":    "sha256=zz",
		"corpo diferente": sign("segredo", []byte(`{}`)),
	} {
		if VerifySignature("segredo", body, header) {
			t.Errorf("%s: assinatura aceita", name)
		}
	}
	if VerifySignature("", body, sign("", body)) {
		t.Error("sem chave configurada, nada pode passar")
	}
}

const webhook = `{
  "object": "whatsapp_business_account",
  "entry": [{"id": "WABA", "changes": [
    {"field": "messages", "value": {
      "messaging_product": "whatsapp",
      "metadata": {"display_phone_number": "5511900000000", "phone_number_id": "111"},
      "contacts": [{"profile": {"name": "Ana"}, "wa_id": "5511988887777"}],
      "messages": [
        {"from": "5511988887777", "id": "wamid.1", "timestamp": "1790000000", "type": "text", "text": {"body": "Quanto custa?"}},
        {"from": "5511988887777", "id": "wamid.2", "timestamp": "1790000001", "type": "audio", "audio": {"id": "m1"}},
        {"from": "5511988887777", "id": "wamid.3", "timestamp": "1790000002", "type": "interactive",
         "interactive": {"type": "button_reply", "button_reply": {"id": "b1", "title": "Falar com atendente"}}},
        {"from": "5511988887777", "id": "wamid.4", "timestamp": "1790000003", "type": "image", "image": {"caption": "meu carro"}}
      ]
    }},
    {"field": "messages", "value": {
      "metadata": {"phone_number_id": "222"},
      "messages": [{"from": "5511000000000", "id": "wamid.outro", "timestamp": "1", "type": "text", "text": {"body": "de outro número"}}]
    }},
    {"field": "messages", "value": {
      "metadata": {"phone_number_id": "111"},
      "statuses": [
        {"id": "wamid.out1", "status": "delivered", "timestamp": "1790000010"},
        {"id": "wamid.out2", "status": "failed", "timestamp": "1790000011",
         "errors": [{"code": 131047, "title": "Re-engagement message", "message": "More than 24 hours"}]}
      ]
    }}
  ]}]
}`

func TestPayloadEvents(t *testing.T) {
	var p Payload
	if err := json.Unmarshal([]byte(webhook), &p); err != nil {
		t.Fatal(err)
	}
	inbound, statuses := p.Events("111")
	if len(inbound) != 4 {
		t.Fatalf("mensagens = %d, quer 4 (a do outro número fica de fora)", len(inbound))
	}
	want := []struct{ kind, body string }{
		{"text", "Quanto custa?"}, {"audio", ""}, {"text", "Falar com atendente"}, {"image", "meu carro"},
	}
	for i, w := range want {
		if inbound[i].Kind != w.kind || inbound[i].Body != w.body {
			t.Errorf("mensagem %d = %s %q, quer %s %q", i, inbound[i].Kind, inbound[i].Body, w.kind, w.body)
		}
	}
	if inbound[0].Name != "Ana" || inbound[0].From != "5511988887777" || inbound[0].At.Unix() != 1790000000 {
		t.Errorf("contato/hora errados: %+v", inbound[0])
	}
	if len(statuses) != 2 || statuses[0].Status != "delivered" || statuses[1].Status != "failed" {
		t.Fatalf("status = %+v", statuses)
	}
	if statuses[1].Error == "" {
		t.Error("falha sem motivo")
	}
}

func TestSendText(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v24.0/111/messages" || r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("requisição errada: %s %s", r.URL.Path, r.Header.Get("Authorization"))
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		_, _ = w.Write([]byte(`{"messaging_product":"whatsapp","messages":[{"id":"wamid.ok"}]}`))
	}))
	defer srv.Close()

	id, err := New(srv.URL, "v24.0", "111", "tok").SendText(context.Background(), "5511988887777", "Olá!")
	if err != nil || id != "wamid.ok" {
		t.Fatalf("SendText = %q, %v", id, err)
	}
	text, _ := got["text"].(map[string]any)
	if got["to"] != "5511988887777" || got["type"] != "text" || text["body"] != "Olá!" {
		t.Errorf("corpo enviado = %v", got)
	}
}

func TestSendTextWindowClosed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"Re-engagement message","type":"OAuthException","code":131047,
			"error_data":{"messaging_product":"whatsapp","details":"More than 24 hours have passed"}}}`))
	}))
	defer srv.Close()

	_, err := New(srv.URL, "v24.0", "111", "tok").SendText(context.Background(), "5511988887777", "oi")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || !apiErr.WindowClosed() {
		t.Fatalf("erro = %v, quer janela fechada", err)
	}
}
