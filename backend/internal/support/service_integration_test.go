package support

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/fulfillment"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/installers"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/whatsapp"
)

func supportDB(t *testing.T) *database.DB {
	t.Helper()
	dsn := os.Getenv("FARBO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("defina FARBO_TEST_DATABASE_URL (Postgres descartável) para rodar o teste com banco")
	}
	ctx := context.Background()
	schema := "test_support_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		_ = admin.Close(context.Background())
	})
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	db := &database.DB{Pool: pool}
	if err := db.Migrate(ctx, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatal(err)
	}
	return db
}

// fakeWhatsApp faz o papel da Cloud API: guarda o que foi enviado.
type fakeWhatsApp struct {
	mu    sync.Mutex
	sent  []string // corpos enviados
	reads int
}

func (f *fakeWhatsApp) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	f.mu.Lock()
	defer f.mu.Unlock()
	if body["status"] == "read" {
		f.reads++
		_, _ = w.Write([]byte(`{"success":true}`))
		return
	}
	text, _ := body["text"].(map[string]any)
	f.sent = append(f.sent, fmt.Sprint(text["body"]))
	_, _ = fmt.Fprintf(w, `{"messages":[{"id":"wamid.out%d"}]}`, len(f.sent))
}

func (f *fakeWhatsApp) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

func (f *fakeWhatsApp) last() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sent[len(f.sent)-1]
}

type recordingNotifier struct {
	mu       sync.Mutex
	handoffs []Handoff
}

func (n *recordingNotifier) Handoff(_ context.Context, h Handoff) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.handoffs = append(n.handoffs, h)
	return nil
}

func (n *recordingNotifier) count() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.handoffs)
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("esperando: %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

var seq int

func inboundPayload(from, id, text string) whatsapp.Payload {
	seq++
	raw := fmt.Sprintf(`{"object":"whatsapp_business_account","entry":[{"changes":[{"field":"messages","value":{
		"metadata":{"phone_number_id":"111"},
		"contacts":[{"profile":{"name":"Ana"},"wa_id":%q}],
		"messages":[{"from":%q,"id":%q,"timestamp":"%d","type":"text","text":{"body":%q}}]}}]}]}`,
		from, from, id, time.Now().Unix()+int64(seq), text)
	var p whatsapp.Payload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		panic(err)
	}
	return p
}

func statusPayload(id, status string) whatsapp.Payload {
	raw := fmt.Sprintf(`{"entry":[{"changes":[{"field":"messages","value":{"metadata":{"phone_number_id":"111"},
		"statuses":[{"id":%q,"status":%q,"timestamp":"1"}]}}]}]}`, id, status)
	var p whatsapp.Payload
	_ = json.Unmarshal([]byte(raw), &p)
	return p
}

func TestSupportEndToEnd(t *testing.T) {
	db := supportDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	var customerID uuid.UUID
	if err := db.QueryRow(ctx, `INSERT INTO users (email, name, role, password_hash, phone)
		VALUES ('ana@cliente.test', 'Ana Souza', 'customer', 'x', '(11) 98888-7777') RETURNING id`).Scan(&customerID); err != nil {
		t.Fatal(err)
	}

	wa := &fakeWhatsApp{}
	waSrv := httptest.NewServer(wa)
	t.Cleanup(waSrv.Close)
	claude := &fakeClaude{t: t, responses: []string{
		finalText("Olá, Ana! O plano custa R$ 69,90 por mês."),
		toolUse(toolTransfer, `{"motivo":"quer contratar 2 planos"}`),
		finalText("Vou te passar para alguém da equipe."),
		// Depois disso a fila acaba: a próxima chamada falha (500).
	}}
	claudeSrv := httptest.NewServer(claude)
	t.Cleanup(claudeSrv.Close)
	ai, err := NewAssistant("chave-teste", "claude-opus-5", "low", Facts{}, time.UTC,
		option.WithBaseURL(claudeSrv.URL), option.WithMaxRetries(0))
	if err != nil {
		t.Fatal(err)
	}
	notifier := &recordingNotifier{}
	cfg := config.WhatsApp{PhoneNumberID: "111", AIDebounce: 10 * time.Millisecond, AIMaxRepliesPerDay: 40, AIHistory: 40}
	svc := NewService(ctx, cfg, NewRepository(db), whatsapp.New(waSrv.URL, "v24.0", "111", "tok"), ai,
		fulfillment.NewRepository(db), installers.NewRepository(db), notifier, time.UTC,
		slog.New(slog.NewTextHandler(io.Discard, nil)))

	// 1. Primeira mensagem, de um celular antigo sem o nono dígito: acha a
	// cliente e a IA responde.
	const from = "551188887777"
	if err := svc.Receive(ctx, inboundPayload(from, "wamid.in1", "Quanto custa?")); err != nil {
		t.Fatal(err)
	}
	eventually(t, "resposta da IA", func() bool { return wa.count() == 1 })
	if !strings.Contains(wa.last(), "R$ 69,90") {
		t.Errorf("resposta = %q", wa.last())
	}
	// O WhatsApp falso conta o envio antes de o serviço gravar a resposta:
	// espera ela chegar ao banco (os status da etapa 3 dependem dela).
	var list []*Conversation
	eventually(t, "resposta gravada", func() bool {
		var err error
		list, err = svc.Repo().List(ctx, false, 10)
		return err == nil && len(list) == 1 && list[0].LastMessage != nil && list[0].LastMessage.Author == AuthorBot
	})
	conv := list[0]
	if conv.CustomerID == nil || *conv.CustomerID != customerID || conv.CustomerName != "Ana Souza" {
		t.Errorf("cliente não identificado pelo número: %+v", conv)
	}
	if conv.Mode != ModeBot || !conv.WindowOpen || conv.LastMessage == nil || conv.LastMessage.Author != AuthorBot {
		t.Errorf("conversa = %+v", conv)
	}
	if !strings.Contains(toJSON(claude.request(0)), "Ana Souza") {
		t.Error("a IA não soube que é cliente")
	}

	// 2. Webhook repetido: nada novo.
	if err := svc.Receive(ctx, inboundPayload(from, "wamid.in1", "Quanto custa?")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if wa.count() != 1 {
		t.Fatalf("mensagem repetida gerou resposta (%d envios)", wa.count())
	}

	// 3. Status fora de ordem: fica o mais avançado.
	for _, st := range []string{"delivered", "read", "delivered"} {
		if err := svc.Receive(ctx, statusPayload("wamid.out1", st)); err != nil {
			t.Fatal(err)
		}
	}
	msgs, _ := svc.Repo().Messages(ctx, conv.ID, 50)
	if got := msgs[len(msgs)-1].Status; got != "read" {
		t.Errorf("status = %q, quer read", got)
	}

	// 4. A IA transfere: equipe avisada, conversa na fila.
	if err := svc.Receive(ctx, inboundPayload(from, "wamid.in2", "Quero contratar 2 planos")); err != nil {
		t.Fatal(err)
	}
	eventually(t, "resposta da transferência", func() bool { return wa.count() == 2 })
	eventually(t, "aviso à equipe", func() bool { return notifier.count() == 1 })
	conv, _ = svc.Repo().Get(ctx, conv.ID)
	if conv.Mode != ModeHuman || !conv.NeedsAttention || conv.HandoffReason != "quer contratar 2 planos" {
		t.Errorf("depois da transferência: %+v", conv)
	}

	// 5. Com a equipe, a IA fica quieta.
	if err := svc.Receive(ctx, inboundPayload(from, "wamid.in3", "Alô?")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if wa.count() != 2 {
		t.Fatalf("a IA respondeu conversa da equipe")
	}

	// 6. A equipe responde: sai da fila.
	var agentID uuid.UUID
	if err := db.QueryRow(ctx, `INSERT INTO users (email, name, role, password_hash)
		VALUES ('op@farbo.test', 'Operador', 'operator', 'x') RETURNING id`).Scan(&agentID); err != nil {
		t.Fatal(err)
	}
	msg, err := svc.SendAgent(ctx, conv.ID, agentID, "Oi Ana, aqui é da equipe!")
	if err != nil || msg.Author != AuthorAgent || msg.AgentName != "Operador" {
		t.Fatalf("resposta da equipe = %+v, %v", msg, err)
	}
	conv, _ = svc.Repo().Get(ctx, conv.ID)
	if conv.NeedsAttention {
		t.Error("respondida pela equipe e ainda na fila")
	}
	if n, _ := svc.Repo().CountAttention(ctx); n != 0 {
		t.Errorf("fila = %d", n)
	}

	// 7. Devolvida à IA sem nada pendente: nada sai.
	if _, err := svc.SetMode(ctx, conv.ID, ModeBot); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if wa.count() != 3 {
		t.Fatalf("envios = %d, quer 3", wa.count())
	}

	// 8. A IA falha: a conversa vai para a equipe com um aviso ao contato.
	if err := svc.Receive(ctx, inboundPayload(from, "wamid.in4", "E a instalação?")); err != nil {
		t.Fatal(err)
	}
	eventually(t, "aviso de transferência após falha", func() bool { return wa.count() == 4 })
	if !strings.Contains(wa.last(), "alguém da equipe") {
		t.Errorf("aviso = %q", wa.last())
	}
	eventually(t, "equipe avisada da falha", func() bool { return notifier.count() == 2 })
	conv, _ = svc.Repo().Get(ctx, conv.ID)
	if conv.Mode != ModeHuman || !conv.NeedsAttention {
		t.Errorf("depois da falha: %+v", conv)
	}

	// 9. Fora da janela de 24 h, a equipe não consegue responder.
	if _, err := db.Exec(ctx, `UPDATE whatsapp_conversations SET last_inbound_at = NOW() - INTERVAL '25 hours'`); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SendAgent(ctx, conv.ID, agentID, "Ainda está aí?"); err == nil {
		t.Fatal("respondeu fora da janela de 24 h")
	}
}
