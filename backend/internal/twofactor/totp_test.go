package twofactor

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

// Os vetores da RFC 6238 (SHA-1, chave "12345678901234567890"), nos 6
// dígitos finais que o app mostra.
func TestTOTPMatchesRFC6238(t *testing.T) {
	key := []byte("12345678901234567890")
	for _, c := range []struct {
		unix int64
		code string
	}{
		{59, "287082"}, {1111111109, "081804"}, {1111111111, "050471"},
		{1234567890, "005924"}, {2000000000, "279037"}, {20000000000, "353130"},
	} {
		if got := hotp(key, uint64(c.unix/totpPeriod), 6); got != c.code {
			t.Errorf("T=%d: %s, esperado %s", c.unix, got, c.code)
		}
	}
}

func TestMatchTOTPToleratesOneStep(t *testing.T) {
	secret := b32.EncodeToString([]byte("12345678901234567890"))
	at := time.Unix(1111111109, 0)
	if step := matchTOTP(secret, "081804", at); step != 1111111109/30 {
		t.Errorf("código do passo atual: %d", step)
	}
	// 30 s depois, o código anterior ainda vale; 90 s depois, não.
	if step := matchTOTP(secret, "081804", at.Add(30*time.Second)); step != 1111111109/30 {
		t.Errorf("passo anterior: %d", step)
	}
	if step := matchTOTP(secret, "081804", at.Add(90*time.Second)); step != 0 {
		t.Errorf("código velho aceito: %d", step)
	}
	if matchTOTP(secret, "81804", at) != 0 || matchTOTP("não-é-base32!", "081804", at) != 0 {
		t.Error("aceitou código ou segredo inválido")
	}
}

func TestSecretAndURL(t *testing.T) {
	secret, err := newSecret()
	if err != nil || len(secret) != 32 {
		t.Fatalf("segredo = %q (%v)", secret, err)
	}
	raw := otpauthURL(secret, "ana@farbo.test")
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "otpauth" || u.Host != "totp" || strings.Contains(raw, "+") {
		t.Fatalf("URL = %s (%v)", raw, err)
	}
	if u.Path != "/Farbo Rastreadores:ana@farbo.test" || u.Query().Get("secret") != secret ||
		u.Query().Get("issuer") != "Farbo Rastreadores" || u.Query().Get("digits") != "6" {
		t.Errorf("URL = %s", raw)
	}
}
