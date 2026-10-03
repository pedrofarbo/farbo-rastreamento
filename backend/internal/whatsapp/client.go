// Package whatsapp fala com a API oficial do WhatsApp (Cloud API da Meta):
// envia mensagens pelo número da empresa e lê os webhooks do que chega.
package whatsapp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// MaxTextLength é o limite de um texto no WhatsApp.
const MaxTextLength = 4096

// Códigos de erro da Cloud API que mudam o que fazer.
const (
	// Passaram 24 h desde a última mensagem do contato: só modelo aprovado.
	codeReEngagement = 131047
)

type Client struct {
	baseURL       string
	phoneNumberID string
	token         string
	http          *http.Client
}

// New monta o cliente do número (baseURL sem a versão, ex.:
// https://graph.facebook.com).
func New(baseURL, version, phoneNumberID, token string) *Client {
	return &Client{
		baseURL:       strings.TrimRight(baseURL, "/") + "/" + strings.Trim(version, "/"),
		phoneNumberID: phoneNumberID,
		token:         token,
		http:          &http.Client{Timeout: 20 * time.Second},
	}
}

// APIError é a recusa da Meta, com o código dela.
type APIError struct {
	Status  int
	Code    int
	Message string
	Details string
}

func (e *APIError) Error() string {
	msg := fmt.Sprintf("WhatsApp recusou (HTTP %d, código %d): %s", e.Status, e.Code, e.Message)
	if e.Details != "" {
		msg += " — " + e.Details
	}
	return msg
}

// WindowClosed: a janela de 24 h fechou; texto livre não sai mais.
func (e *APIError) WindowClosed() bool { return e.Code == codeReEngagement }

// SendText manda um texto e devolve o id da mensagem na Meta.
func (c *Client) SendText(ctx context.Context, to, body string) (string, error) {
	var out struct {
		Messages []struct {
			ID string `json:"id"`
		} `json:"messages"`
	}
	err := c.post(ctx, map[string]any{
		"messaging_product": "whatsapp",
		"recipient_type":    "individual",
		"to":                to,
		"type":              "text",
		"text":              map[string]any{"preview_url": false, "body": body},
	}, &out)
	if err != nil {
		return "", err
	}
	if len(out.Messages) == 0 || out.Messages[0].ID == "" {
		return "", fmt.Errorf("WhatsApp não devolveu o id da mensagem enviada")
	}
	return out.Messages[0].ID, nil
}

// MarkRead marca a mensagem do contato como lida (os dois tiques azuis) e,
// com typing, mostra "digitando…" até a resposta sair (a Meta tira sozinha
// depois de 25 s).
func (c *Client) MarkRead(ctx context.Context, messageID string, typing bool) error {
	body := map[string]any{
		"messaging_product": "whatsapp",
		"status":            "read",
		"message_id":        messageID,
	}
	if typing {
		body["typing_indicator"] = map[string]string{"type": "text"}
	}
	return c.post(ctx, body, nil)
}

func (c *Client) post(ctx context.Context, body any, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/"+c.phoneNumberID+"/messages", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("WhatsApp indisponível: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		var failure struct {
			Error struct {
				Message   string `json:"message"`
				Code      int    `json:"code"`
				ErrorData struct {
					Details string `json:"details"`
				} `json:"error_data"`
			} `json:"error"`
		}
		_ = json.Unmarshal(raw, &failure)
		return &APIError{
			Status: resp.StatusCode, Code: failure.Error.Code,
			Message: failure.Error.Message, Details: failure.Error.ErrorData.Details,
		}
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}
