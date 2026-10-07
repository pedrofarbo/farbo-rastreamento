package twofactor

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// O código do app autenticador (TOTP, RFC 6238): HMAC-SHA1, 6 dígitos, um
// a cada 30 s — o padrão que o Google Authenticator, o Microsoft
// Authenticator e os demais entendem.
const (
	totpPeriod = 30
	totpDigits = 6
	// totpSkew: aceita o código do passo anterior e do seguinte (relógio
	// do celular um pouco adiantado ou atrasado).
	totpSkew = 1
	// Issuer é o nome que aparece no app autenticador.
	Issuer = "Farbo Rastreadores"
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// newSecret sorteia o segredo (160 bits, o tamanho da chave do SHA-1) em
// base32, como o app recebe.
func newSecret() (string, error) {
	raw := make([]byte, 20)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return b32.EncodeToString(raw), nil
}

// otpauthURL é o que o QR Code leva para o app autenticador.
func otpauthURL(secret, account string) string {
	label := url.PathEscape(Issuer + ":" + account)
	q := url.Values{}
	q.Set("secret", secret)
	q.Set("issuer", Issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", fmt.Sprint(totpDigits))
	q.Set("period", fmt.Sprint(totpPeriod))
	return "otpauth://totp/" + label + "?" + strings.ReplaceAll(q.Encode(), "+", "%20")
}

// hotp é o código do passo (RFC 4226).
func hotp(key []byte, step uint64, digits int) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], step)
	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	mod := uint32(1)
	for range digits {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", digits, value%mod)
}

// totpStep é o passo de 30 s de um instante.
func totpStep(at time.Time) int64 { return at.Unix() / totpPeriod }

// matchTOTP diz em qual passo (perto de agora) o código confere; 0 se em
// nenhum. Quem chama recusa um passo igual ou anterior ao último aceito.
func matchTOTP(secret, code string, at time.Time) int64 {
	key, err := b32.DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil || len(code) != totpDigits {
		return 0
	}
	now := totpStep(at)
	for delta := int64(-totpSkew); delta <= totpSkew; delta++ {
		step := now + delta
		if step > 0 && hmac.Equal([]byte(hotp(key, uint64(step), totpDigits)), []byte(code)) {
			return step
		}
	}
	return 0
}
