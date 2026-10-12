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

type bulkSMSSetupRequest struct {
	DeviceIDs []uuid.UUID `json:"deviceIds"`
	Unlock    bool        `json:"unlock"`
	Query     bool        `json:"query"`
}

type BulkSMSSetupItemResult struct {
	DeviceID  uuid.UUID  `json:"deviceId"`
	IMEI      string     `json:"imei,omitempty"`
	Success   bool       `json:"success"`
	SessionID *uuid.UUID `json:"sessionId,omitempty"`
	Status    string     `json:"status,omitempty"`
	Error     string     `json:"error,omitempty"`
}

type BulkSMSSetupResponse struct {
	Total   int                      `json:"total"`
	Started int                      `json:"started"`
	Failed  int                      `json:"failed"`
	Results []BulkSMSSetupItemResult `json:"results"`
}

// handleStartSMSSetupBulk inicia a configuração por SMS de múltiplos rastreadores de uma vez.
func (s *Server) handleStartSMSSetupBulk(w http.ResponseWriter, r *http.Request) {
	if s.SMSSetup == nil || !s.SMSSetup.Enabled() {
		writeError(w, http.StatusBadRequest, "O envio de SMS não está habilitado.")
		return
	}
	var req bulkSMSSetupRequest
	if !decodeOr400(w, r, &req) {
		return
	}
	if len(req.DeviceIDs) == 0 {
		writeError(w, http.StatusBadRequest, "Selecione ao menos um rastreador.")
		return
	}
	if len(req.DeviceIDs) > 100 {
		writeError(w, http.StatusBadRequest, "Limite de no máximo 100 rastreadores por lote.")
		return
	}

	results := make([]BulkSMSSetupItemResult, 0, len(req.DeviceIDs))
	started := 0
	failed := 0
	opts := smssetup.Options{Unlock: req.Unlock, Query: req.Query}
	act := actor(r)

	for _, id := range req.DeviceIDs {
		item := BulkSMSSetupItemResult{DeviceID: id}
		if dev, err := s.Devices.Get(r.Context(), id); err == nil && dev != nil {
			item.IMEI = dev.IMEI
		}
		session, err := s.SMSSetup.Start(r.Context(), id, nil, opts, act)
		if err != nil {
			item.Success = false
			var v smssetup.ValidationError
			if errors.As(err, &v) {
				item.Error = v.Message
			} else {
				item.Error = err.Error()
			}
			failed++
		} else {
			item.Success = true
			item.SessionID = &session.ID
			item.Status = session.Status
			started++
		}
		results = append(results, item)
	}

	s.recordAudit(r, audit.ActionSMSSetup, nil, nil, map[string]any{
		"bulk":    true,
		"total":   len(req.DeviceIDs),
		"started": started,
		"failed":  failed,
	})

	writeJSON(w, http.StatusOK, BulkSMSSetupResponse{
		Total:   len(req.DeviceIDs),
		Started: started,
		Failed:  failed,
		Results: results,
	})
}

