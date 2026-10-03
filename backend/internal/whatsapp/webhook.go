package whatsapp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// VerifySignature confere o X-Hub-Signature-256 ("sha256=<hex>"): o HMAC do
// corpo cru com a chave secreta do aplicativo. Sem ele, qualquer um forja
// mensagem de contato.
func VerifySignature(appSecret string, body []byte, header string) bool {
	hexSum, ok := strings.CutPrefix(strings.TrimSpace(header), "sha256=")
	if !ok || appSecret == "" {
		return false
	}
	got, err := hex.DecodeString(hexSum)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(appSecret))
	mac.Write(body)
	return hmac.Equal(got, mac.Sum(nil))
}

// Payload é o corpo do webhook (campo "messages" da conta do WhatsApp).
type Payload struct {
	Object string `json:"object"`
	Entry  []struct {
		Changes []struct {
			Field string `json:"field"`
			Value value  `json:"value"`
		} `json:"changes"`
	} `json:"entry"`
}

type value struct {
	Metadata struct {
		PhoneNumberID string `json:"phone_number_id"`
	} `json:"metadata"`
	Contacts []struct {
		Profile struct {
			Name string `json:"name"`
		} `json:"profile"`
		WaID string `json:"wa_id"`
	} `json:"contacts"`
	Messages []rawMessage `json:"messages"`
	Statuses []rawStatus  `json:"statuses"`
}

type rawMessage struct {
	From      string `json:"from"`
	ID        string `json:"id"`
	Timestamp string `json:"timestamp"`
	Type      string `json:"type"`
	Text      *struct {
		Body string `json:"body"`
	} `json:"text"`
	// Resposta a botão de modelo.
	Button *struct {
		Text string `json:"text"`
	} `json:"button"`
	Interactive *struct {
		ButtonReply *struct {
			Title string `json:"title"`
		} `json:"button_reply"`
		ListReply *struct {
			Title string `json:"title"`
		} `json:"list_reply"`
	} `json:"interactive"`
	Image    *media `json:"image"`
	Video    *media `json:"video"`
	Document *media `json:"document"`
	Location *struct {
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
		Name      string  `json:"name"`
		Address   string  `json:"address"`
	} `json:"location"`
	Reaction *struct {
		Emoji string `json:"emoji"`
	} `json:"reaction"`
}

type media struct {
	Caption string `json:"caption"`
}

type rawStatus struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	Timestamp string `json:"timestamp"`
	Errors    []struct {
		Code    int    `json:"code"`
		Title   string `json:"title"`
		Message string `json:"message"`
	} `json:"errors"`
}

// Inbound é uma mensagem de contato, já normalizada.
type Inbound struct {
	ID   string
	From string
	Name string
	// Kind: text, audio, image, video, document, sticker, location,
	// reaction, contacts, unsupported...
	Kind string
	// Body: o texto (ou a legenda, ou o botão tocado); vazio no que não tem.
	Body string
	At   time.Time
}

// Status é a notícia de uma mensagem enviada: sent, delivered, read, failed.
type Status struct {
	ID     string
	Status string
	Error  string
}

// Events separa o que é do número configurado: o mesmo aplicativo pode ter
// mais de um número no webhook.
func (p Payload) Events(phoneNumberID string) ([]Inbound, []Status) {
	var inbound []Inbound
	var statuses []Status
	for _, entry := range p.Entry {
		for _, change := range entry.Changes {
			v := change.Value
			if change.Field != "messages" || v.Metadata.PhoneNumberID != phoneNumberID {
				continue
			}
			names := map[string]string{}
			for _, c := range v.Contacts {
				names[c.WaID] = c.Profile.Name
			}
			for _, m := range v.Messages {
				kind, body := m.content()
				inbound = append(inbound, Inbound{
					ID: m.ID, From: m.From, Name: names[m.From], Kind: kind, Body: body, At: unix(m.Timestamp),
				})
			}
			for _, s := range v.Statuses {
				st := Status{ID: s.ID, Status: s.Status}
				if len(s.Errors) > 0 {
					e := s.Errors[0]
					st.Error = strings.TrimSpace(fmt.Sprintf("%d %s %s", e.Code, e.Title, e.Message))
				}
				statuses = append(statuses, st)
			}
		}
	}
	return inbound, statuses
}

func (m rawMessage) content() (kind, body string) {
	switch {
	case m.Type == "text" && m.Text != nil:
		return "text", m.Text.Body
	case m.Type == "button" && m.Button != nil:
		return "text", m.Button.Text
	case m.Type == "interactive" && m.Interactive != nil:
		if r := m.Interactive.ButtonReply; r != nil {
			return "text", r.Title
		}
		if r := m.Interactive.ListReply; r != nil {
			return "text", r.Title
		}
	case m.Type == "image" && m.Image != nil:
		return "image", m.Image.Caption
	case m.Type == "video" && m.Video != nil:
		return "video", m.Video.Caption
	case m.Type == "document" && m.Document != nil:
		return "document", m.Document.Caption
	case m.Type == "location" && m.Location != nil:
		l := m.Location
		place := strings.TrimSpace(strings.Join([]string{l.Name, l.Address}, " "))
		return "location", strings.TrimSpace(fmt.Sprintf("%s (%.5f, %.5f)", place, l.Latitude, l.Longitude))
	case m.Type == "reaction" && m.Reaction != nil:
		return "reaction", m.Reaction.Emoji
	}
	if m.Type == "" {
		return "unsupported", ""
	}
	return m.Type, ""
}

func unix(ts string) time.Time {
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || sec <= 0 {
		return time.Now()
	}
	return time.Unix(sec, 0)
}
