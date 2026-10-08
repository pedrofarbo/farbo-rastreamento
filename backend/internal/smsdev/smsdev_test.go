package smsdev

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

// O SMSDev de mentira: guarda o que chegou e responde o que o teste mandar.
func fakeAPI(t *testing.T, answer func(path string, form url.Values) (int, string)) (*Client, *[]url.Values, *[]string) {
	t.Helper()
	var forms []url.Values
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("método %s", r.Method)
		}
		if r.URL.RawQuery != "" {
			t.Errorf("a chave (ou qualquer coisa) foi na URL: %s", r.URL.RawQuery)
		}
		_ = r.ParseForm()
		forms, paths = append(forms, r.PostForm), append(paths, r.URL.Path)
		status, body := answer(r.URL.Path, r.PostForm)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return NewClient(Config{BaseURL: srv.URL + "/v1", APIKey: "chave-secreta"}), &forms, &paths
}

func TestSend(t *testing.T) {
	c, forms, paths := fakeAPI(t, func(string, url.Values) (int, string) {
		return 200, `[{"situacao":"OK","codigo":"1","id":"637849052","descricao":"MENSAGEM NA FILA"}]`
	})
	msg, err := c.Send(context.Background(), "+55 11 99999-8888", "APN,zap.vivo.com.br#")
	if err != nil || msg.ID != "637849052" || msg.Status != StatusQueued {
		t.Fatalf("envio = %+v %v", msg, err)
	}
	f := (*forms)[0]
	if (*paths)[0] != "/v1/send" || f.Get("key") != "chave-secreta" || f.Get("type") != "9" ||
		f.Get("number") != "5511999998888" || f.Get("msg") != "APN,zap.vivo.com.br#" {
		t.Errorf("pedido = %s %v", (*paths)[0], f)
	}

	// Resposta como objeto, com o id numérico.
	c, _, _ = fakeAPI(t, func(string, url.Values) (int, string) {
		return 200, `{"situacao":"OK","codigo":1,"id":42,"descricao":"MENSAGEM NA FILA"}`
	})
	if msg, err := c.Send(context.Background(), "5511999998888", "x"); err != nil || msg.ID != "42" {
		t.Errorf("objeto = %+v %v", msg, err)
	}

	// Recusa (sem saldo, número inválido): definitiva.
	c, _, _ = fakeAPI(t, func(string, url.Values) (int, string) {
		return 200, `{"situacao":"ERRO","codigo":"300","descricao":"SALDO INSUFICIENTE"}`
	})
	_, err = c.Send(context.Background(), "5511999998888", "x")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "300" || !strings.Contains(err.Error(), "SALDO INSUFICIENTE") || !IsDefinitive(err) {
		t.Errorf("recusa = %v", err)
	}
	// Servidor fora: tenta de novo depois.
	c, _, _ = fakeAPI(t, func(string, url.Values) (int, string) { return 502, "Bad Gateway" })
	if _, err := c.Send(context.Background(), "5511999998888", "x"); err == nil || IsDefinitive(err) {
		t.Errorf("502 = %v", err)
	}
	if IsDefinitive(errors.New("SMSDev indisponível: timeout")) || IsDefinitive(&APIError{HTTPStatus: 429}) {
		t.Error("falha temporária tratada como recusa")
	}
}

func TestFetchMapsTheSituation(t *testing.T) {
	situacao := "RECEBIDA"
	c, forms, paths := fakeAPI(t, func(string, url.Values) (int, string) {
		return 200, `{"situacao":"OK","codigo":"1","data_envio":"21/10/2019 11:08:58","operadora":"VIVO","descricao":"` + situacao + `"}`
	})
	for desc, want := range map[string]string{
		"RECEBIDA": StatusDelivered, "ENVIADA": StatusSent, "FILA": StatusQueued, "APROVACAO": StatusQueued,
		"ERRO": StatusFailed, "BLACK LIST": StatusFailed, "CANCELADA": StatusCanceled, "OUTRA": "",
	} {
		situacao = desc
		msg, err := c.Fetch(context.Background(), "637849052")
		if err != nil || msg.Status != want || msg.Carrier != "VIVO" {
			t.Errorf("%s = %+v %v", desc, msg, err)
		}
		if Failed(want) && msg.ErrorMessage == "" {
			t.Errorf("%s sem motivo", desc)
		}
	}
	if (*paths)[0] != "/v1/dlr" || (*forms)[0].Get("id") != "637849052" {
		t.Errorf("consulta = %s %v", (*paths)[0], (*forms)[0])
	}

	// Um id que o SMSDev ainda não conhece: a resposta vem vazia — sem
	// situação, sem erro.
	c, _, _ = fakeAPI(t, func(string, url.Values) (int, string) { return 200, "" })
	if msg, err := c.Fetch(context.Background(), "1"); err != nil || msg.Status != "" {
		t.Errorf("vazia = %+v %v", msg, err)
	}
}

func TestBalance(t *testing.T) {
	c, forms, paths := fakeAPI(t, func(string, url.Values) (int, string) {
		return 200, `{"situacao":"OK","saldo_sms":"1234","descricao":"SALDO ATUAL"}`
	})
	if n, err := c.Balance(context.Background()); err != nil || n != 1234 {
		t.Errorf("saldo = %d %v", n, err)
	}
	if (*paths)[0] != "/v1/balance" || (*forms)[0].Get("key") != "chave-secreta" {
		t.Errorf("pedido = %s %v", (*paths)[0], (*forms)[0])
	}
	c, _, _ = fakeAPI(t, func(string, url.Values) (int, string) {
		return 200, `{"situacao":"ERRO","codigo":"500","descricao":"CHAVE INVALIDA"}`
	})
	if _, err := c.Balance(context.Background()); err == nil || !strings.Contains(err.Error(), "CHAVE INVALIDA") {
		t.Errorf("chave errada = %v", err)
	}
}

func TestInbox(t *testing.T) {
	c, forms, paths := fakeAPI(t, func(string, url.Values) (int, string) {
		return 200, `[
			{"situacao":"OK","data_read":"08/10/2026 10:00:00","telefone":"5511999998888","id":"637849052","refer":"",
			 "msg_sent":"APN,zap.vivo.com.br,vivo,Senh4#","id_sms_read":"901","descricao":"APN OK"},
			{"situacao":"OK","telefone":5521988887777,"id":637849053,"id_sms_read":902,"descricao":"oi"}
		]`
	})
	since := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	replies, err := c.Inbox(context.Background(), since)
	if err != nil || len(replies) != 2 {
		t.Fatalf("respostas = %+v %v", replies, err)
	}
	if r := replies[0]; r.ID != "901" || r.SentID != "637849052" || r.From != "5511999998888" || r.Body != "APN OK" {
		t.Errorf("primeira = %+v", r)
	}
	if r := replies[1]; r.ID != "902" || r.From != "5521988887777" {
		t.Errorf("segunda (números) = %+v", r)
	}
	f := (*forms)[0]
	if (*paths)[0] != "/v1/inbox" || f.Get("status") != "1" || f.Get("date_from") != "07/10/2026" || f.Get("date_to") == "" {
		t.Errorf("pedido = %s %v", (*paths)[0], f)
	}

	// Nenhuma resposta (como a API de verdade responde): lista vazia.
	c, _, _ = fakeAPI(t, func(string, url.Values) (int, string) {
		return 200, `{"situacao":"OK","descricao":"SEM MENSAGENS NA CAIXA DE ENTRADA."}`
	})
	if replies, err := c.Inbox(context.Background(), since); err != nil || len(replies) != 0 {
		t.Errorf("vazia = %+v %v", replies, err)
	}
	if (Config{APIKey: " "}).Enabled() || !(Config{APIKey: "k"}).Enabled() {
		t.Error("Enabled")
	}
}
