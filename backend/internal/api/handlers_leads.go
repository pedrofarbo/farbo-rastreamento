package api

import (
	"errors"
	"net/http"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/leads"
)

func writeLeadError(w http.ResponseWriter, err error) {
	var invalid leads.ValidationError
	if errors.As(err, &invalid) {
		writeError(w, http.StatusBadRequest, invalid.Message)
		return
	}
	handleStoreError(w, err, "pré-cliente não encontrado")
}

type publicLeadRequest struct {
	Name         string `json:"name"`
	Email        string `json:"email"`
	Phone        string `json:"phone"`
	City         string `json:"city"`
	Plan         string `json:"plan"`
	VehicleType  string `json:"vehicleType"`
	VehicleCount int    `json:"vehicleCount"`
	Message      string `json:"message"`
	Consent      bool   `json:"consent"`
	// JoinLaunch: marcou "entrar também na lista de pré-lançamento".
	JoinLaunch bool `json:"joinLaunch"`
	// Website é a isca: campo escondido que só robô preenche.
	Website string `json:"website"`
}

// handlePublicCreateLead: o cadastro de interesse da landing. Público, com
// limite por IP; a resposta não devolve nada do que foi gravado.
func (s *Server) handlePublicCreateLead(w http.ResponseWriter, r *http.Request) {
	var req publicLeadRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	if req.Website != "" {
		// Robô: finge que deu certo, para ele não tentar de outro jeito.
		s.Log.Info("cadastro de interesse descartado (isca preenchida)", "ip", clientIP(r))
		writeJSON(w, http.StatusCreated, map[string]string{"status": "ok"})
		return
	}
	if _, err := s.Leads.Submit(r.Context(), leads.Input{
		Name: req.Name, Email: req.Email, Phone: req.Phone, City: req.City, Plan: req.Plan,
		VehicleType: req.VehicleType, VehicleCount: req.VehicleCount, Message: req.Message, Consent: req.Consent,
		JoinLaunch: req.JoinLaunch,
	}); err != nil {
		writeLeadError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "ok"})
}

func (s *Server) handleListLeads(w http.ResponseWriter, r *http.Request) {
	list, err := s.Leads.Repo().List(r.Context(), r.URL.Query().Get("status"))
	if err != nil {
		writeLeadError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// handleLeadStats: o número de pré-clientes novos, para o menu.
func (s *Server) handleLeadStats(w http.ResponseWriter, r *http.Request) {
	n, err := s.Leads.Repo().CountNew(r.Context())
	if err != nil {
		writeLeadError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"new": n})
}

func (s *Server) handleUpdateLead(w http.ResponseWriter, r *http.Request) {
	id, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}
	var req struct {
		Status string `json:"status"`
		Notes  string `json:"notes"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	lead, err := s.Leads.Repo().Update(r.Context(), id, req.Status, req.Notes)
	if err != nil {
		writeLeadError(w, err)
		return
	}
	s.recordAudit(r, audit.ActionLeadUpdated, nil, nil, map[string]any{"leadId": id, "status": req.Status})
	writeJSON(w, http.StatusOK, lead)
}

type publicWaitlistRequest struct {
	Name    string `json:"name"`
	Email   string `json:"email"`
	Phone   string `json:"phone"`
	Consent bool   `json:"consent"`
	// Website é a isca, como no pré-cadastro.
	Website string `json:"website"`
}

// handlePublicJoinWaitlist: "me avise quando lançar", da landing. Público,
// com o mesmo limite por IP do pré-cadastro.
func (s *Server) handlePublicJoinWaitlist(w http.ResponseWriter, r *http.Request) {
	var req publicWaitlistRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	if req.Website != "" {
		s.Log.Info("inscrição no lançamento descartada (isca preenchida)", "ip", clientIP(r))
		writeJSON(w, http.StatusCreated, map[string]string{"status": "ok"})
		return
	}
	if _, err := s.Leads.JoinWaitlist(r.Context(), leads.WaitlistInput{
		Name: req.Name, Email: req.Email, Phone: req.Phone, Consent: req.Consent,
	}); err != nil {
		writeLeadError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "ok"})
}

func (s *Server) handleListWaitlist(w http.ResponseWriter, r *http.Request) {
	list, err := s.Leads.Repo().Waitlist(r.Context())
	if err != nil {
		writeLeadError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// handleRemoveFromWaitlist: a pessoa pediu para sair da lista.
func (s *Server) handleRemoveFromWaitlist(w http.ResponseWriter, r *http.Request) {
	id, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}
	if err := s.Leads.Repo().RemoveFromWaitlist(r.Context(), id); err != nil {
		handleStoreError(w, err, "inscrição não encontrada")
		return
	}
	s.recordAudit(r, audit.ActionWaitlistRemoved, nil, nil, map[string]any{"entryId": id})
	writeJSON(w, http.StatusNoContent, nil)
}
