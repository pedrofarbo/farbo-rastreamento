package support

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go/option"
)

// fakeClaude responde a Messages API com as respostas da fila, guardando o
// que recebeu.
type fakeClaude struct {
	t         *testing.T
	mu        sync.Mutex
	responses []string
	requests  []map[string]any
	betas     []string
}

func (f *fakeClaude) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/v1/messages" {
		f.t.Errorf("caminho inesperado: %s", r.URL.Path)
	}
	body, _ := io.ReadAll(r.Body)
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		f.t.Errorf("corpo inválido: %v", err)
	}
	f.mu.Lock()
	f.requests = append(f.requests, req)
	f.betas = append(f.betas, r.Header.Get("anthropic-beta"))
	if len(f.responses) == 0 {
		f.mu.Unlock()
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"api_error","message":"sem resposta na fila"}}`))
		return
	}
	resp := f.responses[0]
	f.responses = f.responses[1:]
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(resp))
}

func (f *fakeClaude) request(i int) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	if i >= len(f.requests) {
		f.t.Fatalf("pedido %d não chegou (%d pedidos)", i, len(f.requests))
	}
	return f.requests[i]
}

func claudeMessage(stop string, content string) string {
	return `{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5","content":` + content +
		`,"stop_reason":"` + stop + `","stop_sequence":null,"usage":{"input_tokens":100,"output_tokens":20,` +
		`"cache_read_input_tokens":80,"cache_creation_input_tokens":0}}`
}

func toolUse(name, input string) string {
	return claudeMessage("tool_use", `[{"type":"text","text":"Vou verificar."},`+
		`{"type":"tool_use","id":"toolu_1","name":"`+name+`","input":`+input+`}]`)
}

func finalText(text string) string {
	b, _ := json.Marshal(text)
	return claudeMessage("end_turn", `[{"type":"text","text":`+string(b)+`}]`)
}

func newFakeAssistant(t *testing.T, model string, responses ...string) (*Assistant, *fakeClaude) {
	t.Helper()
	fake := &fakeClaude{t: t, responses: responses}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	a, err := NewAssistant("chave-teste", model, "low", Facts{PlanName: "Plano Mensal", PlanPrice: "R$ 69,90"},
		time.UTC, option.WithBaseURL(srv.URL), option.WithMaxRetries(0))
	if err != nil {
		t.Fatal(err)
	}
	return a, fake
}

type recordingTools struct {
	transferred string
	orders      int
	city        string
}

func (r *recordingTools) Transfer(_ context.Context, reason string) (string, error) {
	r.transferred = reason
	return "transferida", nil
}

func (r *recordingTools) Orders(context.Context) (string, error) {
	r.orders++
	return "Pedidos do cliente (1): rastreador em trânsito.", nil
}

func (r *recordingTools) Installers(_ context.Context, city string) (string, error) {
	r.city = city
	return "Prestadores: Oficina do Zé", nil
}

func TestAssistantToolLoopAndRequestShape(t *testing.T) {
	a, fake := newFakeAssistant(t, "claude-opus-5",
		toolUse(toolOrders, `{}`),
		finalText("Seu rastreador está *em trânsito*."),
	)
	tools := &recordingTools{}
	reply, err := a.Reply(context.Background(), []Turn{
		{FromContact: true, Text: "Cadê meu rastreador?"},
	}, Situation{ContactName: "Ana", CustomerName: "Ana Souza", IsCustomer: true, Now: time.Now()}, tools)
	if err != nil {
		t.Fatal(err)
	}
	if reply.Text != "Seu rastreador está *em trânsito*." || reply.Transferred || tools.orders != 1 {
		t.Fatalf("resposta = %+v, consultas = %d", reply, tools.orders)
	}
	if reply.Usage.Rounds != 2 || reply.Usage.CacheRead != 160 {
		t.Errorf("uso = %+v", reply.Usage)
	}

	first := fake.request(0)
	if first["model"] != "claude-opus-5" || first["fallbacks"] != "default" {
		t.Errorf("modelo/fallback = %v / %v", first["model"], first["fallbacks"])
	}
	if !strings.Contains(fake.betas[0], "server-side-fallback-2026-07-01") {
		t.Errorf("beta do fallback ausente: %q", fake.betas[0])
	}
	if cfg, _ := first["output_config"].(map[string]any); cfg["effort"] != "low" {
		t.Errorf("output_config = %v", first["output_config"])
	}
	system, _ := first["system"].([]any)
	if len(system) != 1 || system[0].(map[string]any)["cache_control"] == nil {
		t.Errorf("instruções sem cache: %v", system)
	}
	tools0, _ := first["tools"].([]any)
	if len(tools0) != 3 || tools0[0].(map[string]any)["strict"] != true {
		t.Errorf("ferramentas = %v", tools0)
	}
	schema := tools0[0].(map[string]any)["input_schema"].(map[string]any)
	if schema["additionalProperties"] != false {
		t.Errorf("schema sem additionalProperties=false: %v", schema)
	}
	// O contexto vai como mensagem de sistema depois da fala do contato.
	msgs := first["messages"].([]any)
	last := msgs[len(msgs)-1].(map[string]any)
	if last["role"] != "system" || !strings.Contains(toJSON(last), "Ana Souza") {
		t.Errorf("última mensagem = %v", last)
	}

	// Na volta, o resultado da ferramenta segue o pedido da IA.
	second := fake.request(1)
	msgs = second["messages"].([]any)
	if !strings.Contains(toJSON(msgs[len(msgs)-1]), `"tool_result"`) ||
		msgs[len(msgs)-2].(map[string]any)["role"] != "assistant" {
		t.Errorf("segunda chamada sem o resultado da ferramenta: %s", toJSON(msgs))
	}
}

func TestAssistantTransfer(t *testing.T) {
	a, _ := newFakeAssistant(t, "claude-opus-5",
		toolUse(toolTransfer, `{"motivo":"quer contratar 2 planos"}`),
		finalText("Vou te passar para alguém da equipe."),
	)
	tools := &recordingTools{}
	reply, err := a.Reply(context.Background(), []Turn{{FromContact: true, Text: "quero fechar"}},
		Situation{Now: time.Now()}, tools)
	if err != nil {
		t.Fatal(err)
	}
	if !reply.Transferred || tools.transferred != "quer contratar 2 planos" || reply.Text == "" {
		t.Fatalf("resposta = %+v, motivo = %q", reply, tools.transferred)
	}
}

func TestAssistantRefusal(t *testing.T) {
	a, _ := newFakeAssistant(t, "claude-opus-5", claudeMessage("refusal", `[]`))
	reply, err := a.Reply(context.Background(), []Turn{{FromContact: true, Text: "..."}}, Situation{Now: time.Now()},
		&recordingTools{})
	if err != nil || !reply.Refused {
		t.Fatalf("resposta = %+v, %v", reply, err)
	}
}

func TestAssistantOtherModelsSkipUnsupportedFeatures(t *testing.T) {
	a, fake := newFakeAssistant(t, "claude-haiku-4-5", finalText("Olá!"))
	if _, err := a.Reply(context.Background(), []Turn{{FromContact: true, Text: "oi"}},
		Situation{CustomerName: "Ana", IsCustomer: true, Now: time.Now()}, &recordingTools{}); err != nil {
		t.Fatal(err)
	}
	req := fake.request(0)
	if req["fallbacks"] != nil || req["output_config"] != nil || fake.betas[0] != "" {
		t.Errorf("recurso não suportado foi enviado: fallbacks=%v output_config=%v beta=%q",
			req["fallbacks"], req["output_config"], fake.betas[0])
	}
	msgs := req["messages"].([]any)
	last := msgs[len(msgs)-1].(map[string]any)
	if last["role"] != "user" || !strings.Contains(toJSON(last), "contexto_do_sistema") {
		t.Errorf("sem mensagem de sistema, o contexto vai na fala do contato: %v", last)
	}
}

func toJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
