package api

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/smssetup"
)

// Configuração do rastreador por SMS (SMSDev) na ativação. Ver o pacote
// smssetup.

func smsError(w http.ResponseWriter, err error, notFound string) {
	var v smssetup.ValidationError
	if errors.As(err, &v) {
		writeError(w, http.StatusBadRequest, v.Message)
		return
	}
	handleStoreError(w, err, notFound)
}

// handleSMSUsage: o contador da página de rastreadores — o saldo no SMSDev,
// quantas ativações ele paga e os SMS dos últimos 30 dias.
func (s *Server) handleSMSUsage(w http.ResponseWriter, r *http.Request) {
	usage, err := s.SMSSetup.Usage(r.Context())
	if err != nil {
		smsError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, usage)
}

// handleDeviceSMSSetup: os comandos que vão sair (redigidos), o que falta no
// cadastro e a última configuração do rastreador.
func (s *Server) handleDeviceSMSSetup(w http.ResponseWriter, r *http.Request) {
	id, ok := idOr400(w, r)
	if !ok {
		return
	}
	out := map[string]any{"enabled": s.SMSSetup.Enabled(), "from": s.SMSSetup.From()}
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
