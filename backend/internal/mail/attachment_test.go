package mail

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	netmail "net/mail"
	"strings"
	"testing"
	"time"
)

// Com anexo, o e-mail vira multipart/mixed: o texto e o HTML (alternative)
// primeiro, depois o arquivo em base64 — com o nome acentuado intacto.
func TestBuildMessageWithAttachment(t *testing.T) {
	from := &netmail.Address{Name: "Farbo", Address: "nao-responda@farbo.test"}
	to := &netmail.Address{Address: "frota@cliente.test"}
	data := []byte("\xEF\xBB\xBFVeículo;Km\r\nVan 01;12,0\r\n" + strings.Repeat("Van;1,0\r\n", 40))
	raw, err := buildMessage(from, to, Message{
		Subject: "Relatório semanal", Text: "olá", HTML: "<p>olá</p>",
		Attachments: []Attachment{{Name: "relatório-semanal.csv", ContentType: "text/csv; charset=utf-8", Data: data}},
	}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	msg, err := netmail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	mediaType, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/mixed" {
		t.Fatalf("Content-Type = %q (%v)", msg.Header.Get("Content-Type"), err)
	}
	parts := multipart.NewReader(msg.Body, params["boundary"])

	first, err := parts.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	if inner, _, _ := mime.ParseMediaType(first.Header.Get("Content-Type")); inner != "multipart/alternative" {
		t.Fatalf("primeira parte = %q", first.Header.Get("Content-Type"))
	}

	file, err := parts.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	if file.FileName() != "relatório-semanal.csv" {
		t.Errorf("nome do anexo = %q", file.FileName())
	}
	for _, line := range strings.Split(string(mustRead(t, file)), "\r\n") {
		if len(line) > 76 {
			t.Fatalf("linha do base64 com %d caracteres", len(line))
		}
	}
	// O multipart.Reader não decodifica o base64 do anexo: refaz a leitura.
	file2 := attachmentPart(t, raw)
	if !bytes.Equal(file2, data) {
		t.Errorf("anexo decodificado difere: %q", file2)
	}
	if _, err := parts.NextPart(); err != io.EOF {
		t.Errorf("parte a mais: %v", err)
	}

	// Sem anexo continua multipart/alternative.
	plain, _ := buildMessage(from, to, Message{Subject: "x", Text: "a", HTML: "<p>a</p>"}, time.Now())
	if !strings.Contains(string(plain), "Content-Type: multipart/alternative;") {
		t.Error("sem anexo deixou de ser alternative")
	}
}

func mustRead(t *testing.T, r io.Reader) []byte {
	t.Helper()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// attachmentPart devolve o conteúdo decodificado do anexo.
func attachmentPart(t *testing.T, raw []byte) []byte {
	t.Helper()
	msg, _ := netmail.ReadMessage(bytes.NewReader(raw))
	_, params, _ := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	parts := multipart.NewReader(msg.Body, params["boundary"])
	_, _ = parts.NextPart()
	file, err := parts.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	encoded := strings.ReplaceAll(string(mustRead(t, file)), "\r\n", "")
	out, err := base64Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func base64Decode(s string) ([]byte, error) { return base64.StdEncoding.DecodeString(s) }
