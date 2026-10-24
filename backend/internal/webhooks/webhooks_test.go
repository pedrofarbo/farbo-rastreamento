package webhooks

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"net"
	"testing"
)

// Só HTTPS e nada da rede interna: nem pelo endereço, nem pelo nome.
func TestCheckURL(t *testing.T) {
	s := NewService(nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for _, bad := range []string{
		"http://sistema.empresa.test/hook", "https://127.0.0.1/hook", "https://10.0.0.5/hook", "https://192.168.1.10/hook",
		"https://169.254.169.254/latest/meta-data", "https://localhost/hook", "https://[::1]/hook", "ftp://x.test/",
		"https://usuario:senha@sistema.test/hook", "nao-e-url",
	} {
		if _, err := s.checkURL(bad); err == nil {
			t.Errorf("%s passou", bad)
		}
	}
	if got, err := s.checkURL(" https://sistema.empresa.test/farbo?x=1 "); err != nil || got != "https://sistema.empresa.test/farbo?x=1" {
		t.Errorf("endereço bom = %q %v", got, err)
	}
	for _, ip := range []string{"100.64.1.1", "172.16.0.1", "0.0.0.0", "fe80::1"} {
		if !blocked(net.ParseIP(ip)) {
			t.Errorf("%s não foi barrado", ip)
		}
	}
	if blocked(net.ParseIP("8.8.8.8")) {
		t.Error("8.8.8.8 barrado")
	}
}

// A assinatura: HMAC-SHA256 do "<timestamp>.<corpo>" com o segredo.
func TestSign(t *testing.T) {
	mac := hmac.New(sha256.New, []byte("whsec_x"))
	mac.Write([]byte(`1700000000.{"a":1}`))
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if got := Sign("whsec_x", "1700000000", []byte(`{"a":1}`)); got != want {
		t.Errorf("assinatura = %s, quer %s", got, want)
	}
}
