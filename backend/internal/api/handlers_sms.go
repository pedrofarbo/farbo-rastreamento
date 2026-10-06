package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/smssetup"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/twilio"
)

// Configuração do rastreador por SMS (Twilio) na ativação. Ver o pacote
// smssetup.

func smsError(w http.ResponseWriter, err error, notFound string) {
	var v smssetup.ValidationError
	if errors.As(err, &v) {
		writeError(w, http.StatusBadRequest, v.Message)
		return
	}
	handleStoreError(w, err, notFound)
}

// smsWebhookURLs são os endereços que o Twilio chama (a tela mostra o de
// respostas, para configurar o número no Twilio).
func (s *Server) smsWebhookURLs() (status, inbound string) {
	base := s.Config.SMS.WebhookBaseURL
	if base == "" {
		return "", ""
	}
	return base + "/api/twilio/status", base + "/api/twilio/inbound"
}

// handleDeviceSMSSetup: os comandos que vão sair (redigidos), o que falta no
// cadastro e a última configuração do rastreador.
func (s *Server) handleDeviceSMSSetup(w http.ResponseWriter, r *http.Request) {
	id, ok := idOr400(w, r)
	if !ok {
		return
	}
	_, inbound := s.smsWebhookURLs()
	out := map[string]any{"enabled": s.SMSSetup.Enabled(), "from": s.SMSSetup.From(), "inboundUrl": inbound}
	plan, err := s.SMSSetup.Plan(r.Context(), id)
	if err != nil {
		smsError(w, err, "rastreador não encontrado")
		return
	}
	out["plan"] = plan
	session, err := s.SMSSetup.Latest(r.Context(), id)
	if err != nil {
		smsError(w, err, "rastreador não encontrado")
		return
	}
	out["session"] = session
	writeJSON(w, http.StatusOK, out)
}

// handleStartSMSSetup manda a configuração por SMS para o chip do rastreador.
func (s *Server) handleStartSMSSetup(w http.ResponseWriter, r *http.Request) {
	id, ok := idOr400(w, r)
	if !ok {
		return
	}
	var req struct {
		FulfillmentID *uuid.UUID `json:"fulfillmentId"`
		Unlock        bool       `json:"unlock"`
		Query         bool       `json:"query"`
	}
	if !decodeOr400(w, r, &req) {
		return
	}
	session, err := s.SMSSetup.Start(r.Context(), id, req.FulfillmentID, smssetup.Options{Unlock: req.Unlock, Query: req.Query}, actor(r))
	if err != nil {
		s.recordAudit(r, audit.ActionSMSSetup, nil, nil, map[string]any{"device": id, "result": "erro", "error": err.Error()})
		smsError(w, err, "rastreador não encontrado")
		return
	}
	s.recordAudit(r, audit.ActionSMSSetup, nil, nil, map[string]any{
		"device": id, "session": session.ID, "fulfillment": req.FulfillmentID, "steps": len(session.Steps), "status": session.Status,
	})
	writeJSON(w, http.StatusCreated, session)
}

func (s *Server) handleCancelSMSSetup(w http.ResponseWriter, r *http.Request) {
	id, ok := idOr400(w, r)
	if !ok {
		return
	}
	session, err := s.SMSSetup.Cancel(r.Context(), id)
	if err != nil {
		smsError(w, err, "configuração não encontrada")
		return
	}
	s.recordAudit(r, audit.ActionSMSSetup, nil, nil, map[string]any{"session": id, "result": "cancelada"})
	writeJSON(w, http.StatusOK, session)
}

// twilioForm lê e confere o webhook do Twilio: a assinatura é da URL pública
// chamada (a base configurada + o caminho) com os parâmetros do corpo.
func (s *Server) twilioForm(w http.ResponseWriter, r *http.Request) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return false
	}
	fullURL := s.Config.SMS.WebhookBaseURL + r.URL.RequestURI()
	if !twilio.ValidSignature(s.Config.SMS.TwilioAuthToken, fullURL, r.PostForm, r.Header.Get(twilio.SignatureHeader)) {
		s.Log.Warn("webhook do Twilio com assinatura inválida", "ip", clientIP(r), "path", r.URL.Path)
		writeError(w, http.StatusForbidden, "assinatura inválida")
		return false
	}
	return true
}

// handleTwilioStatus: o Twilio avisa o andamento de um SMS enviado.
func (s *Server) handleTwilioStatus(w http.ResponseWriter, r *http.Request) {
	if !s.twilioForm(w, r) {
		return
	}
	form := r.PostForm
	if err := s.SMSSetup.UpdateStatus(r.Context(), form.Get("MessageSid"), form.Get("MessageStatus"), form.Get("ErrorCode"), ""); err != nil {
		s.Log.Error("falha ao gravar o status do SMS", "sid", form.Get("MessageSid"), "err", err)
		writeError(w, http.StatusInternalServerError, "falha ao gravar")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleTwilioInbound: um SMS chegou no número do Twilio — a resposta do
// rastreador. Responde TwiML vazio (sem mandar nada de volta).
func (s *Server) handleTwilioInbound(w http.ResponseWriter, r *http.Request) {
	if !s.twilioForm(w, r) {
		return
	}
	form := r.PostForm
	if _, err := s.SMSSetup.Inbound(r.Context(), form.Get("From"), strings.TrimSpace(form.Get("Body")), form.Get("MessageSid")); err != nil {
		s.Log.Error("falha ao gravar o SMS recebido", "err", err)
		writeError(w, http.StatusInternalServerError, "falha ao gravar")
		return
	}
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><Response></Response>`))
}
