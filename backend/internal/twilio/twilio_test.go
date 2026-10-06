package twilio

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// O exemplo da documentação do Twilio (docs/usage/security): a URL, os
// parâmetros e a assinatura esperada com o Auth Token 12345.
func TestSignatureDocumentedExample(t *testing.T) {
	params := url.Values{
		"CallSid": {"CA1234567890ABCDE"},
		"Caller":  {"+14158675310"},
		"Digits":  {"1234"},
		"From":    {"+14158675310"},
		"To":      {"+18005551212"},
	}
	const fullURL = "https://example.com/myapp.php?foo=1&bar=2"
	if got := Sign("12345", fullURL, params); got != "L/OH5YylLD5NRKLltdqwSvS0BnU=" {
		t.Fatalf("assinatura = %s", got)
	}
	if !ValidSignature("12345", fullURL, params, "L/OH5YylLD5NRKLltdqwSvS0BnU=") {
		t.Error("assinatura válida recusada")
	}
	params.Set("Digits", "9999")
	if ValidSignature("12345", fullURL, params, "L/OH5YylLD5NRKLltdqwSvS0BnU=") {
		t.Error("corpo mexido aceito")
	}
	if ValidSignature("", fullURL, params, "x") || ValidSignature("12345", fullURL, params, "") {
		t.Error("sem token ou sem assinatura aceito")
	}
}

func TestSendAndFetch(t *testing.T) {
	var gotPath, gotUser, gotPass string
	var gotForm url.Values
	status := http.StatusCreated
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotUser, gotPass, _ = r.BasicAuth()
		_ = r.ParseForm()
		gotForm = r.PostForm
		w.WriteHeader(status)
		if status >= 300 {
			_, _ = io.WriteString(w, `{"code": 21211, "message": "The 'To' number +5511 is not a valid phone number.", "status": 400}`)
			return
		}
		_, _ = io.WriteString(w, `{"sid": "SM123", "status": "queued", "to": "+5511999998888", "from": "+15005550006", "error_code": null}`)
	}))
	t.Cleanup(srv.Close)
	c := NewClient(Config{BaseURL: srv.URL, AccountSID: "AC1", AuthToken: "tok", From: "+15005550006"})
	msg, err := c.Send(context.Background(), "+5511999998888", "APN,zap.vivo.com.br#", "https://x.test/api/twilio/status")
	if err != nil || msg.SID != "SM123" || msg.Status != "queued" {
		t.Fatalf("envio = %+v %v", msg, err)
	}
	if gotPath != "/Accounts/AC1/Messages.json" || gotUser != "AC1" || gotPass != "tok" ||
		gotForm.Get("To") != "+5511999998888" || gotForm.Get("From") != "+15005550006" ||
		gotForm.Get("Body") != "APN,zap.vivo.com.br#" || gotForm.Get("StatusCallback") != "https://x.test/api/twilio/status" {
		t.Errorf("pedido = %s %s:%s %v", gotPath, gotUser, gotPass, gotForm)
	}
	// Com Messaging Service, vai o SID dele no lugar do From.
	c = NewClient(Config{BaseURL: srv.URL, AccountSID: "AC1", AuthToken: "tok", MessagingServiceSID: "MG9"})
	_, _ = c.Send(context.Background(), "+5511999998888", "x", "")
	if gotForm.Get("MessagingServiceSid") != "MG9" || gotForm.Has("From") || gotForm.Has("StatusCallback") {
		t.Errorf("com Messaging Service: %v", gotForm)
	}
	status = http.StatusOK
	if m, err := c.Fetch(context.Background(), "SM123"); err != nil || m.SID != "SM123" || gotPath != "/Accounts/AC1/Messages/SM123.json" {
		t.Errorf("consulta = %+v %v (%s)", m, err, gotPath)
	}
	status = http.StatusBadRequest
	_, err = c.Send(context.Background(), "+5511", "x", "")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != 21211 || !IsDefinitive(err) {
		t.Errorf("recusa = %v", err)
	}
	if IsDefinitive(errors.New("dial tcp: timeout")) || IsDefinitive(&APIError{HTTPStatus: 503}) || IsDefinitive(&APIError{HTTPStatus: 429}) {
		t.Error("falha temporária tratada como recusa")
	}
	if !Final("delivered") || Final("sent") || !Failed("undelivered") || Failed("delivered") {
		t.Error("status finais")
	}
}
