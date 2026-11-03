package pagarme

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"strings"
)

// Event é a notificação da Pagar.me. O conteúdo só diz o que olhar: quem
// decide é a consulta à API (com a chave secreta), nunca o corpo do webhook.
type Event struct {
	ID   string          `json:"id"`
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// ParseEvent lê o corpo do webhook.
func ParseEvent(body []byte) (*Event, error) {
	var ev Event
	if err := json.Unmarshal(body, &ev); err != nil {
		return nil, err
	}
	if ev.ID == "" || ev.Type == "" {
		return nil, errors.New("evento sem id ou tipo")
	}
	return &ev, nil
}

// ChargeIDs são as cobranças citadas: a própria (charge.*) ou as do pedido
// (order.*).
func (e *Event) ChargeIDs() []string {
	var data struct {
		ID      string `json:"id"`
		Charges []struct {
			ID string `json:"id"`
		} `json:"charges"`
	}
	if json.Unmarshal(e.Data, &data) != nil {
		return nil
	}
	switch {
	case strings.HasPrefix(e.Type, "charge.") && data.ID != "":
		return []string{data.ID}
	case strings.HasPrefix(e.Type, "order."):
		ids := make([]string, 0, len(data.Charges))
		for _, c := range data.Charges {
			if c.ID != "" {
				ids = append(ids, c.ID)
			}
		}
		return ids
	}
	return nil
}

// RecipientID é o recebedor citado (recipient.*), ou "".
func (e *Event) RecipientID() string {
	if !strings.HasPrefix(e.Type, "recipient.") {
		return ""
	}
	var data struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(e.Data, &data) != nil {
		return ""
	}
	return data.ID
}

// VerifySecret confere o segredo que vai no endereço do webhook cadastrado na
// Pagar.me (?secret=...). Segredo vazio na configuração recusa todos.
func VerifySecret(got, want string) bool {
	if want == "" || got == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}
