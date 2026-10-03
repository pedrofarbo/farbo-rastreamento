package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/fulfillment"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/installers"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/support"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/telemetry"
	ws "github.com/pedrofarbo/farbo-rastreamento/backend/internal/websocket"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/whatsapp"
)

// Atendimento pelo WhatsApp pelas rotas de verdade: webhook assinado, papéis
// e resposta da equipe. Sem IA (só a equipe responde). Precisa de
// FARBO_TEST_DATABASE_URL.
func TestWhatsAppRoutesEndToEnd(t *testing.T) {
	db := integrationDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{
		HTTP: config.HTTP{RateLimitRPS: 1000, RateLimitBurst: 1000},
		Auth: config.Auth{
			JWTSecret:      []byte("segredo-de-teste-integracao-0123456789abcdef"),
			AccessTokenTTL: time.Hour, RefreshTokenTTL: time.Hour, BcryptCost: bcrypt.MinCost,
		},
		Billing: config.Billing{Timezone: "UTC"},
		WhatsApp: config.WhatsApp{
			AccessToken: "tok", PhoneNumberID: "111", AppSecret: "chave-do-app", VerifyToken: "token-combinado-123",
			AIDebounce: time.Millisecond, AIMaxRepliesPerDay: 40, AIHistory: 40,
		},
	}

	var mu sync.Mutex
	var sent []string
	graph := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Text struct {
				Body string `json:"body"`
			} `json:"text"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		sent = append(sent, body.Text.Body)
		n := len(sent)
		mu.Unlock()
		_, _ = fmt.Fprintf(w, `{"messages":[{"id":"wamid.out%d"}]}`, n)
	}))
	t.Cleanup(graph.Close)

	authSvc := auth.NewService(auth.NewRepository(db), cfg.Auth, nil, log)
	for _, role := range []string{auth.RoleAdmin, auth.RoleOperator, auth.RoleViewer, auth.RoleCustomer} {
		if _, err := authSvc.CreateUser(ctx, role+"@wa.test", role, role, userPassword); err != nil {
			t.Fatal(err)
		}
	}
	supportSvc := support.NewService(ctx, cfg.WhatsApp, support.NewRepository(db),
		whatsapp.New(graph.URL, "v24.0", "111", "tok"), nil,
		fulfillment.NewRepository(db), installers.NewRepository(db), nil, time.UTC, log)
	hub := ws.NewHub(log, nil)
	server := NewServer(Deps{
		Config: cfg, Log: log, Metrics: telemetry.NewMetrics(), DB: db, Auth: authSvc,
		Audit: audit.NewService(audit.NewRepository(db), log), Support: supportSvc,
		WS: ws.NewHandler(hub, nil), Hub: hub,
	})
	srv := httptest.NewServer(server.Handler())
	t.Cleanup(srv.Close)
	env := &credEnv{t: t, db: db, srv: srv}

	// O contato escreve: webhook assinado pela chave do aplicativo.
	payload := []byte(`{"object":"whatsapp_business_account","entry":[{"changes":[{"field":"messages","value":{
		"metadata":{"phone_number_id":"111"},"contacts":[{"profile":{"name":"Bruno"},"wa_id":"5511977776666"}],
		"messages":[{"from":"5511977776666","id":"wamid.in1","timestamp":"` + fmt.Sprint(time.Now().Unix()) + `",
		"type":"text","text":{"body":"Meu rastreador parou"}}]}}]}]}`)
	mac := hmac.New(sha256.New, []byte("chave-do-app"))
	mac.Write(payload)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/whatsapp/webhook", bytes.NewReader(payload))
	req.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("webhook: %d", resp.StatusCode)
	}

	// Só admin e operador entram no atendimento.
	if status, _ := env.do("", http.MethodGet, "/api/whatsapp", nil); status != http.StatusUnauthorized {
		t.Errorf("sem login: %d", status)
	}
	for _, role := range []string{auth.RoleViewer, auth.RoleCustomer} {
		if status, _ := env.do(env.login(role+"@wa.test"), http.MethodGet, "/api/whatsapp/conversations", nil); status != http.StatusForbidden {
			t.Errorf("%s: %d, quer 403", role, status)
		}
	}
	op := env.login(auth.RoleOperator + "@wa.test")

	var status whatsAppStatus
	_ = json.Unmarshal(env.must(op, http.MethodGet, "/api/whatsapp", nil, http.StatusOK), &status)
	if !status.Configured || status.AIReady || status.Attention != 1 {
		t.Errorf("status = %+v (sem IA, a conversa espera a equipe)", status)
	}

	var list []support.Conversation
	_ = json.Unmarshal(env.must(op, http.MethodGet, "/api/whatsapp/conversations?filter=attention", nil, http.StatusOK), &list)
	if len(list) != 1 || list[0].ContactName != "Bruno" || list[0].LastMessage == nil ||
		list[0].LastMessage.Body != "Meu rastreador parou" || list[0].Phone != "+55 11 97777-6666" {
		t.Fatalf("lista = %+v", list)
	}
	id := list[0].ID.String()

	var details struct {
		Mode     string            `json:"mode"`
		Messages []support.Message `json:"messages"`
	}
	_ = json.Unmarshal(env.must(op, http.MethodGet, "/api/whatsapp/conversations/"+id, nil, http.StatusOK), &details)
	if len(details.Messages) != 1 || details.Messages[0].Author != support.AuthorContact {
		t.Fatalf("detalhes = %+v", details)
	}

	// A equipe responde: sai pelo WhatsApp e a fila esvazia.
	env.must(op, http.MethodPost, "/api/whatsapp/conversations/"+id+"/messages",
		map[string]string{"text": "Oi Bruno, vamos ver isso."}, http.StatusCreated)
	mu.Lock()
	if len(sent) != 1 || sent[0] != "Oi Bruno, vamos ver isso." {
		t.Errorf("enviado = %v", sent)
	}
	mu.Unlock()
	_ = json.Unmarshal(env.must(op, http.MethodGet, "/api/whatsapp", nil, http.StatusOK), &status)
	if status.Attention != 0 {
		t.Errorf("fila depois da resposta = %d", status.Attention)
	}

	// Sem IA ligada, não dá para devolver; mensagem vazia é recusada.
	env.must(op, http.MethodPut, "/api/whatsapp/conversations/"+id+"/mode", map[string]string{"mode": "BOT"}, http.StatusConflict)
	env.must(op, http.MethodPost, "/api/whatsapp/conversations/"+id+"/messages", map[string]string{"text": "  "}, http.StatusConflict)
	env.must(op, http.MethodGet, "/api/whatsapp/conversations/00000000-0000-0000-0000-000000000000", nil, http.StatusNotFound)
}
