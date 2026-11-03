package api

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/affiliates"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/appointments"
)

// A gestão da instalação pelo representante, pela página dele (com a senha):
// os clientes que indicou esperando a instalação e a retirada do rastreador,
// e os agendamentos — ele agenda, remarca e cancela; o link do instalador
// conclui, como na central (que vê tudo na agenda).

// partnerManager é o representante da página com a gestão ligada (o link e
// a senha conferidos); sem a gestão, 404.
func (s *Server) partnerManager(w http.ResponseWriter, r *http.Request) (*affiliates.Manager, bool) {
	if s.Appointments == nil || !s.partnerAllowed(w, r) {
		if s.Appointments == nil {
			writeError(w, http.StatusNotFound, "a agenda de instalação está desligada")
		}
		return nil, false
	}
	m, err := s.Affiliates.Manager(r.Context(), chi.URLParam(r, "token"))
	if err != nil {
		affiliateError(w, err, "")
		return nil, false
	}
	if m == nil {
		writeError(w, http.StatusNotFound, "a gestão das instalações não está ligada para você: fale com a Farbo")
		return nil, false
	}
	w.Header().Set("Cache-Control", "no-store")
	return m, true
}

func writePartnerInstallError(w http.ResponseWriter, err error) bool {
	if errors.Is(err, appointments.ErrNotReferred) {
		writeError(w, http.StatusNotFound, err.Error())
		return false
	}
	return writeAppointmentError(w, err, "agendamento não encontrado")
}

func (s *Server) handlePartnerInstallations(w http.ResponseWriter, r *http.Request) {
	m, ok := s.partnerManager(w, r)
	if !ok {
		return
	}
	queue, err := s.Appointments.PartnerQueue(r.Context(), m.ID)
	if !writePartnerInstallError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, queue)
}

func (s *Server) handlePartnerSchedule(w http.ResponseWriter, r *http.Request) {
	m, ok := s.partnerManager(w, r)
	if !ok {
		return
	}
	var in appointments.PartnerInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	a, err := s.Appointments.PartnerSchedule(r.Context(), m.ID, m.Name, in)
	if !writePartnerInstallError(w, err) {
		return
	}
	s.Log.Info("representante agendou", "affiliate", m.ID, "appointment", a.ID, "kind", a.Kind)
	writeJSON(w, http.StatusCreated, a)
}

func (s *Server) handlePartnerReschedule(w http.ResponseWriter, r *http.Request) {
	m, ok := s.partnerManager(w, r)
	if !ok {
		return
	}
	id, ok := idOr400(w, r)
	if !ok {
		return
	}
	var in appointments.PartnerInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	a, err := s.Appointments.PartnerReschedule(r.Context(), m.ID, id, in)
	if !writePartnerInstallError(w, err) {
		return
	}
	s.Log.Info("representante remarcou", "affiliate", m.ID, "appointment", a.ID)
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) handlePartnerCancel(w http.ResponseWriter, r *http.Request) {
	m, ok := s.partnerManager(w, r)
	if !ok {
		return
	}
	id, ok := idOr400(w, r)
	if !ok {
		return
	}
	a, err := s.Appointments.PartnerCancel(r.Context(), m.ID, id)
	if !writePartnerInstallError(w, err) {
		return
	}
	s.Log.Info("representante cancelou", "affiliate", m.ID, "appointment", a.ID)
	writeJSON(w, http.StatusOK, a)
}
