package api

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/support"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/whatsapp"
)

// writeSupportError traduz os erros do atendimento.
func writeSupportError(w http.ResponseWriter, err error) {
	var rule support.RuleError
	var apiErr *whatsapp.APIError
	switch {
	case errors.As(err, &rule):
		writeError(w, http.StatusConflict, rule.Message)
	case errors.As(err, &apiErr):
		// Recusa da Meta (token vencido, número bloqueado...): a mensagem
		// dela vai para a tela.
		writeError(w, http.StatusBadGateway, apiErr.Error())
	default:
		handleStoreError(w, err, "conversa não encontrada")
	}
}

// handleWhatsAppVerify é o aperto de mão ao cadastrar o webhook na Meta: ela
// manda o token combinado e espera o challenge de volta.
func (s *Server) handleWhatsAppVerify(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	token := s.Config.WhatsApp.VerifyToken
	if token == "" || q.Get("hub.mode") != "subscribe" ||
		subtle.ConstantTimeCompare([]byte(q.Get("hub.verify_token")), []byte(token)) != 1 {
		writeError(w, http.StatusForbidden, "token de verificação inválido")
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, q.Get("hub.challenge"))
}

// handleWhatsAppWebhook recebe as mensagens dos contatos e os status das
// enviadas. Só vale com a assinatura da chave secreta do aplicativo.
func (s *Server) handleWhatsAppWebhook(w http.ResponseWriter, r *http.Request) {
	if s.Support == nil || !s.Support.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "WhatsApp não configurado")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	if !whatsapp.VerifySignature(s.Config.WhatsApp.AppSecret, body, r.Header.Get("X-Hub-Signature-256")) {
		s.Log.Warn("webhook do WhatsApp com assinatura inválida", "ip", clientIP(r))
		writeError(w, http.StatusUnauthorized, "assinatura inválida")
		return
	}
	var payload whatsapp.Payload
	if err := json.Unmarshal(body, &payload); err != nil {
		// Assinado, mas fora do formato: nada a aplicar, e não adianta a
		// Meta repetir.
		s.Log.Warn("webhook do WhatsApp fora do formato", "err", err)
		writeJSON(w, http.StatusOK, map[string]string{"status": "ignorado"})
		return
	}
	if err := s.Support.Receive(r.Context(), payload); err != nil {
		s.Log.Error("falha ao gravar o webhook do WhatsApp", "err", err)
		writeError(w, http.StatusInternalServerError, "erro interno")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type whatsAppStatus struct {
	// Configured: número e webhook prontos; AIReady: a IA responde.
	Configured bool   `json:"configured"`
	AIReady    bool   `json:"aiReady"`
	Model      string `json:"model"`
	// Attention: conversas esperando a equipe (o número no menu).
	Attention int `json:"attention"`
}

func (s *Server) handleWhatsAppStatus(w http.ResponseWriter, r *http.Request) {
	out := whatsAppStatus{}
	if s.Support != nil {
		out.Configured, out.AIReady = s.Support.Enabled(), s.Support.AIReady()
		if out.AIReady {
			out.Model = s.Config.WhatsApp.AIModel
		}
		n, err := s.Support.Repo().CountAttention(r.Context())
		if err != nil {
			writeSupportError(w, err)
			return
		}
		out.Attention = n
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleListConversations(w http.ResponseWriter, r *http.Request) {
	list, err := s.Support.Repo().List(r.Context(), r.URL.Query().Get("filter") == "attention", 200)
	if err != nil {
		writeSupportError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) conversationFromURL(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return uuid.Nil, false
	}
	return id, true
}

type conversationDetails struct {
	*support.Conversation
	Messages []*support.Message `json:"messages"`
}

func (s *Server) handleGetConversation(w http.ResponseWriter, r *http.Request) {
	id, ok := s.conversationFromURL(w, r)
	if !ok {
		return
	}
	conv, err := s.Support.Repo().Get(r.Context(), id)
	if err != nil {
		writeSupportError(w, err)
		return
	}
	msgs, err := s.Support.Repo().Messages(r.Context(), id, 300)
	if err != nil {
		writeSupportError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, conversationDetails{Conversation: conv, Messages: msgs})
}

// handleSendWhatsApp: a equipe responde pelo painel (e assume a conversa).
func (s *Server) handleSendWhatsApp(w http.ResponseWriter, r *http.Request) {
	id, ok := s.conversationFromURL(w, r)
	if !ok {
		return
	}
	var req struct {
		Text string `json:"text"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	msg, err := s.Support.SendAgent(r.Context(), id, actorOf(r), req.Text)
	if err != nil {
		writeSupportError(w, err)
		return
	}
	s.recordAudit(r, audit.ActionWhatsAppMessageSent, nil, nil, map[string]any{"conversationId": id})
	writeJSON(w, http.StatusCreated, msg)
}

// handleSetConversationMode: a equipe assume a conversa ou devolve para a IA.
func (s *Server) handleSetConversationMode(w http.ResponseWriter, r *http.Request) {
	id, ok := s.conversationFromURL(w, r)
	if !ok {
		return
	}
	var req struct {
		Mode string `json:"mode"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	conv, err := s.Support.SetMode(r.Context(), id, req.Mode)
	if err != nil {
		writeSupportError(w, err)
		return
	}
	s.recordAudit(r, audit.ActionWhatsAppModeChanged, nil, nil, map[string]any{"conversationId": id, "mode": req.Mode})
	writeJSON(w, http.StatusOK, conv)
}
