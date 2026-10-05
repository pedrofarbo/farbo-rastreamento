package abacatepay

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
)

// PublicWebhookKey é a chave pública com que a AbacatePay assina os webhooks
// (HMAC-SHA256 do corpo cru, em base64, no cabeçalho X-Webhook-Signature).
// É a mesma para todas as contas e vem da documentação oficial.
const PublicWebhookKey = "t9dXRhHHo3yDEj5pVDYz0frf7q6bMKyMRmxxCPIPp3RCplBfXRxqlC6ZpiWmOqj4L63qEaeUOtrCI8P0VMUgo6iIga2ri9ogaHFs0WIIywSMg0q7RmBfybe1E5XJcfC4IW3alNqym0tXoAKkzvfEjZxV6bE0oG2zJrNNYmUCKZyV0KZ3JS8Votf9EAWWYdiDkMkpbMdPggfh1EqHlVkMiTady6jOR3hyzGEHrIz2Ret0xHKMbiqkr9HS1JhNHDX9"

// SignatureHeader carrega a assinatura do corpo.
const SignatureHeader = "X-Webhook-Signature"

// Sign calcula a assinatura esperada de um corpo.
func Sign(body []byte, key string) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write(body)
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// VerifySignature confere a assinatura do cabeçalho contra o corpo cru, em
// tempo constante.
func VerifySignature(body []byte, signature, key string) bool {
	signature = strings.TrimSpace(signature)
	if signature == "" {
		return false
	}
	return hmac.Equal([]byte(Sign(body, key)), []byte(signature))
}

// VerifySecret compara o ?webhookSecret= recebido com o configurado, em tempo
// constante.
func VerifySecret(received, expected string) bool {
	if expected == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(received), []byte(expected)) == 1
}

// Event é o envelope comum dos webhooks v2.
type Event struct {
	ID      string          `json:"id"`
	Event   string          `json:"event"`
	DevMode bool            `json:"devMode"`
	Data    json.RawMessage `json:"data"`
}

func ParseEvent(body []byte) (*Event, error) {
	var ev Event
	if err := json.Unmarshal(body, &ev); err != nil {
		return nil, err
	}
	if ev.ID == "" || ev.Event == "" {
		return nil, errors.New("webhook sem id ou sem tipo de evento")
	}
	return &ev, nil
}

// ChargeIDs devolve os ids de Pix citados no evento, onde quer que estejam no
// payload. O conteúdo do webhook não é usado para decidir nada: ele só diz
// quais cobranças consultar de novo na API, que é a fonte da verdade.
func (e *Event) ChargeIDs() []string { return e.idsWithPrefix("pix_char_") }

// idsWithPrefix devolve, sem repetir, os textos do payload que começam com o
// prefixo.
func (e *Event) idsWithPrefix(prefix string) []string {
	var root any
	if err := json.Unmarshal(e.Data, &root); err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	var walk func(v any)
	walk = func(v any) {
		switch value := v.(type) {
		case map[string]any:
			for _, child := range value {
				walk(child)
			}
		case []any:
			for _, child := range value {
				walk(child)
			}
		case string:
			if strings.HasPrefix(value, prefix) && !seen[value] {
				seen[value] = true
				out = append(out, value)
			}
		}
	}
	walk(root)
	return out
}
