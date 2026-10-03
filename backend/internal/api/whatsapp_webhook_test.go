package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/support"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/whatsapp"
)

func whatsAppServer() *Server {
	cfg := &config.Config{WhatsApp: config.WhatsApp{
		AccessToken: "tok", PhoneNumberID: "111", AppSecret: "chave-do-app", VerifyToken: "token-combinado-123",
	}}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := support.NewService(context.Background(), cfg.WhatsApp, nil, whatsapp.New("http://127.0.0.1:1", "v24.0", "111", "tok"),
		nil, nil, nil, nil, time.UTC, log)
	return &Server{Deps: Deps{Config: cfg, Log: log, Support: svc}}
}

func TestWhatsAppVerifyHandshake(t *testing.T) {
	s := whatsAppServer()
	cases := []struct {
		query string
		code  int
		body  string
	}{
		{"hub.mode=subscribe&hub.verify_token=token-combinado-123&hub.challenge=987", http.StatusOK, "987"},
		{"hub.mode=subscribe&hub.verify_token=outro&hub.challenge=987", http.StatusForbidden, ""},
		{"hub.mode=unsubscribe&hub.verify_token=token-combinado-123&hub.challenge=987", http.StatusForbidden, ""},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		s.handleWhatsAppVerify(rec, httptest.NewRequest(http.MethodGet, "/api/whatsapp/webhook?"+tc.query, nil))
		if rec.Code != tc.code || (tc.body != "" && rec.Body.String() != tc.body) {
			t.Errorf("%s: %d %q", tc.query, rec.Code, rec.Body.String())
		}
	}
}

func TestWhatsAppWebhookRequiresSignature(t *testing.T) {
	s := whatsAppServer()
	body := `{"object":"whatsapp_business_account","entry":[]}`
	for name, header := range map[string]string{"sem assinatura": "", "assinatura errada": "sha256=00"} {
		req := httptest.NewRequest(http.MethodPost, "/api/whatsapp/webhook", strings.NewReader(body))
		if header != "" {
			req.Header.Set("X-Hub-Signature-256", header)
		}
		rec := httptest.NewRecorder()
		s.handleWhatsAppWebhook(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: %d, quer 401", name, rec.Code)
		}
	}
}
