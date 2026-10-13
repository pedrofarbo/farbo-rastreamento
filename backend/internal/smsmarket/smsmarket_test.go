package smsmarket

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// call é o que chegou na SMS Market de mentira.
type call struct {
	method, path string
	query, form  url.Values
	user, pass   string
}

// A SMS Market de mentira: guarda o que chegou e responde o que o teste mandar.
func fakeAPI(t *testing.T, answer func(c call) (int, string)) (*Client, *[]call) {
	t.Helper()
	var calls []call
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		user, pass, _ := r.BasicAuth()
		c := call{method: r.Method, path: r.URL.Path, query: r.URL.Query(), form: r.PostForm, user: user, pass: pass}
		calls = append(calls, c)
		if r.URL.Query().Get("password") != "" || r.PostForm.Get("password") != "" {
			t.Errorf("a senha foi fora do Authorization: %v %v", r.URL.Query(), r.PostForm)
		}
		status, body := answer(c)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return NewClient(Config{BaseURL: srv.URL + "/webservice-rest/", User: "farbo", Password: "senha-secreta"}), &calls
}

func TestSend(t *testing.T) {
	c, calls := fakeAPI(t, func(call) (int, string) {
		return 200, `{"success":true,"responseCode":"000","responseDescription":"Success queued","credit":"1","balance":"99984","id":"813831"}`
	})
	msg, err := c.Send(context.Background(), "+55 (34) 99999-0011", "APN,zap.vivo.com.br,vivo,vivo#")
	if err != nil || msg.ID != "813831" || msg.Status != StatusQueued {
		t.Fatalf("envio = %+v %v", msg, err)
	}
	got := (*calls)[0]
	f := got.form
	if got.method != http.MethodPost || got.path != "/webservice-rest/send-single" || got.user != "farbo" || got.pass != "senha-secreta" {
		t.Errorf("pedido = %s %s (%s)", got.method, got.path, got.user)
	}
	// SMS interativo (o chip responde), sem o 55 no número, com o nosso id.
	if f.Get("type") != "2" || f.Get("country_code") != "55" || f.Get("number") != "34999990011" ||
		f.Get("content") != "APN,zap.vivo.com.br,vivo,vivo#" || !strings.HasPrefix(f.Get("campaign_id"), "farbo-") {
		t.Errorf("campos = %v", f)
	}
	// Só a tabela básica: sem UTF-8 nem partes.
	if f.Has("utf8") || f.Has("concatenation") {
		t.Errorf("utf8/partes à toa: %v", f)
	}

	// Colchetes (fora da tabela básica) e texto longo.
	if _, err := c.Send(context.Background(), "34999990011", "SERVER,1,[host],5000#"); err != nil {
		t.Fatal(err)
	}
	if f := (*calls)[1].form; f.Get("utf8") != "1" || f.Has("concatenation") || f.Get("number") != "34999990011" {
		t.Errorf("com colchetes = %v", f)
	}
	if _, err := c.Send(context.Background(), "3433334444", strings.Repeat("A", 161)); err != nil {
		t.Fatal(err)
	}
	if f := (*calls)[2].form; f.Get("concatenation") != "1" || f.Has("utf8") || f.Get("number") != "3433334444" {
		t.Errorf("longo = %v", f)
	}
}

func TestSendRefusals(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		body       string
		definitive bool
		message    string
	}{
		{"sem saldo", 402, `{"success":false,"responseCode":"080","responseDescription":"Insufficient or expired balance"}`, true,
			"saldo de SMS insuficiente ou vencido"},
		{"IP não liberado", 403, `{"success":false,"responseCode":"091","responseDescription":"API access denied for this IP"}`, true,
			"IP do servidor não está liberado"},
		{"senha errada", 401, `{"success":false,"responseCode":"010","responseDescription":"User or password is invalid"}`, true,
			"usuário ou senha"},
		{"número inválido", 200, `{"success":false,"responseCode":"050","responseDescription":"Empty or invalid number"}`, true,
			"número inválido"},
		{"manutenção", 200, `{"success":false,"responseCode":"100","responseDescription":"This service is currently under maintenance"}`, false,
			"manutenção"},
		{"erro do servidor", 502, `<html>Bad Gateway</html>`, false, "Bad Gateway"},
		{"código desconhecido", 200, `{"success":false,"responseCode":"999","responseDescription":"Algo novo"}`, true, "Algo novo"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := fakeAPI(t, func(call) (int, string) { return tc.status, tc.body })
			_, err := c.Send(context.Background(), "5534999990011", "x")
			var apiErr *APIError
			if !errors.As(err, &apiErr) || IsDefinitive(err) != tc.definitive || !strings.Contains(apiErr.Message, tc.message) {
				t.Errorf("erro = %#v (definitivo %v)", err, IsDefinitive(err))
			}
		})
	}

	// Número fora do Brasil (ou incompleto): recusado sem chamar a API.
	c, calls := fakeAPI(t, func(call) (int, string) { return 200, `{}` })
	for _, n := range []string{"999", "+1 415 555 2671 00", ""} {
		if _, err := c.Send(context.Background(), n, "x"); !IsDefinitive(err) {
			t.Errorf("%q: %v", n, err)
		}
	}
	if len(*calls) != 0 {
		t.Errorf("chamou a API com número inválido: %d", len(*calls))
	}
	// Falha de rede: não é definitiva.
	if IsDefinitive(errors.New("dial tcp: timeout")) {
		t.Error("rede é passageira")
	}
}

func TestFetch(t *testing.T) {
	status := "1"
	c, calls := fakeAPI(t, func(call) (int, string) {
		if status == "" {
			// Como a SMS Market responde de verdade: sem a lista.
			return 200, `{"success":true,"responseCode":"200","responseDescription":"Search success","messageCount":0}`
		}
		return 200, `{"success":true,"responseCode":"200","responseDescription":"Successful search","messageCount":1,"messages":[` +
			`{"id":"813831","number":"5534999990011","content":"APN#","campaign_id":"farbo-1","status":"` + status + `","carrier_name":"VIVO"}]}`
	})
	msg, err := c.Fetch(context.Background(), "813831")
	if err != nil || msg.Status != StatusDelivered || msg.Carrier != "VIVO" || msg.ErrorMessage != "" {
		t.Fatalf("entregue = %+v %v", msg, err)
	}
	if got := (*calls)[0]; got.method != http.MethodGet || got.path != "/webservice-rest/mt_id" || got.query.Get("id") != "813831" ||
		got.query.Get("timezone") != "-03:00" {
		t.Errorf("consulta = %s %s %v", got.method, got.path, got.query)
	}

	for code, want := range map[string]string{"-1": StatusQueued, "3": StatusQueued, "0": StatusSent, "2": StatusDelivered,
		"-6": StatusCanceled, "-3": StatusFailed, "9": StatusFailed} {
		status = code
		msg, err := c.Fetch(context.Background(), "813831")
		if err != nil || msg.Status != want {
			t.Errorf("status %s = %+v %v", code, msg, err)
		}
	}
	status = "-5"
	if msg, _ := c.Fetch(context.Background(), "813831"); msg.ErrorCode != "-5" || msg.ErrorMessage != "número na lista de bloqueio da SMS Market" {
		t.Errorf("falha explicada = %+v", msg)
	}
	// Ainda sem notícia: sem status.
	status = ""
	if msg, err := c.Fetch(context.Background(), "813831"); err != nil || msg.Status != "" {
		t.Errorf("sem notícia = %+v %v", msg, err)
	}
}

func TestInbox(t *testing.T) {
	c, calls := fakeAPI(t, func(call) (int, string) {
		return 200, `{"success":true,"responseCode":"200","responseDescription":"Successful search","messageCount":2,"messages":[` +
			`{"id":"9001","number":"5534999990011","content":"APN:zap.vivo.com.br OK","campaign_id":"farbo-abc","schedule":"2026-10-13 10:03:00","carrier_name":"VIVO"},` +
			`{"id":9002,"number":"5521988887777","content":"oi","campaign_id":"","schedule":"2026-10-13 10:05:00","carrier_name":"CLARO"}]}`
	})
	c.now = func() time.Time { return time.Date(2026, 10, 13, 22, 30, 0, 0, time.UTC) }
	replies, err := c.Inbox(context.Background(), time.Date(2026, 10, 12, 22, 30, 0, 0, time.UTC))
	if err != nil || len(replies) != 2 {
		t.Fatalf("respostas = %+v %v", replies, err)
	}
	if r := replies[0]; r.ID != "9001" || r.From != "5534999990011" || r.Body != "APN:zap.vivo.com.br OK" || r.SentID != "farbo-abc" {
		t.Errorf("resposta = %+v", r)
	}
	if replies[1].ID != "9002" {
		t.Errorf("id numérico = %+v", replies[1])
	}

	// Nada recebido (a resposta real vem sem a lista).
	empty, _ := fakeAPI(t, func(call) (int, string) {
		return 200, `{"success":true,"responseCode":"200","responseDescription":"Search success","messageCount":0}`
	})
	if replies, err := empty.Inbox(context.Background(), time.Now().Add(-time.Hour)); err != nil || len(replies) != 0 {
		t.Errorf("sem respostas = %+v %v", replies, err)
	}
	// O período em Brasília, e todas (não só as novas).
	q := (*calls)[0].query
	if (*calls)[0].path != "/webservice-rest/mo" || q.Get("start_date") != "2026-10-12 19:30:00" ||
		q.Get("end_date") != "2026-10-13 19:30:00" || q.Get("timezone") != "-03:00" {
		t.Errorf("consulta = %s %v", (*calls)[0].path, q)
	}
}

func TestBalance(t *testing.T) {
	// Como a conta responde de verdade: os saldos de SMS em balance_1 e
	// balance_2 (3 e 4 são de WhatsApp); vale o menor.
	body := `{"success":true,"responseCode":"200","responseDescription":"Search success","balance_1":"6","balance_2":"6",` +
		`"balance_3":"10","balance_4":"10","activation":"2026-10-13 19:58:27","expiration":"2027-10-13 23:59:59"}`
	c, calls := fakeAPI(t, func(call) (int, string) { return 200, body })
	n, err := c.Balance(context.Background())
	if err != nil || n != 6 || (*calls)[0].path != "/webservice-rest/balance" || (*calls)[0].method != http.MethodGet {
		t.Fatalf("saldo = %d %v", n, err)
	}
	body = `{"success":true,"responseCode":"200","balance_1":"120","balance_2":"95","balance_3":"0","balance_4":"0"}`
	if n, err := c.Balance(context.Background()); err != nil || n != 95 {
		t.Errorf("o menor dos de SMS (WhatsApp zerado não conta) = %d %v", n, err)
	}
	// Como a documentação mostra (sms e whatsapp).
	body = `{"success":true,"responseCode":"200","responseDescription":"Successful search","sms":"7289","whatsapp":"606"}`
	if n, err := c.Balance(context.Background()); err != nil || n != 7289 {
		t.Errorf("formato da documentação = %d %v", n, err)
	}
	// Sem saldo de SMS na resposta: erro (o contador mostra o motivo).
	body = `{"success":true,"responseCode":"200","balance_3":"10"}`
	if _, err := c.Balance(context.Background()); err == nil {
		t.Error("sem saldo de SMS deveria ser erro")
	}
	body = `{"success":true,"responseCode":"200","balance_1":"muitos"}`
	if _, err := c.Balance(context.Background()); err == nil {
		t.Error("saldo ilegível deveria ser erro")
	}
	c, _ = fakeAPI(t, func(call) (int, string) {
		return 401, `{"success":false,"responseCode":"010","responseDescription":"User or password is invalid"}`
	})
	if _, err := c.Balance(context.Background()); err == nil || !strings.Contains(err.Error(), "usuário ou senha") {
		t.Errorf("senha errada = %v", err)
	}
}

func TestEnabledAndBasicTable(t *testing.T) {
	if (Config{User: "farbo"}).Enabled() || (Config{Password: "x"}).Enabled() || !(Config{User: "farbo", Password: "x"}).Enabled() {
		t.Error("precisa do usuário e da senha")
	}
	for s, want := range map[string]bool{
		"APN,zap.vivo.com.br,vivo,vivo#": true, "TIMER,30,180#": true, "Olá": false, "[a]": false, "{x}": false, "~": false, "a\nb": true,
	} {
		if isBasic(s) != want {
			t.Errorf("isBasic(%q) = %v", s, !want)
		}
	}
}
