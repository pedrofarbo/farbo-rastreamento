package api

import (
	"errors"
	"net/http"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/reportmail"
)

// Os relatórios da frota por e-mail: cada pessoa da conta (o titular ou
// alguém da equipe) agenda os seus. O envio fica em internal/reportmail.

// reportPerson é quem agenda, com o acesso dele à conta.
func reportPerson(r *http.Request) reportmail.Person {
	principal, _ := auth.FromContext(r.Context())
	p := reportmail.Person{UserID: principal.UserID, AccountID: principal.Account(),
		CanAddRecipients: principal.CanOperateAccount()}
	if m := principal.Membership; m != nil && m.Restricted {
		p.Restricted, p.Groups = true, m.Groups
	}
	return p
}

func (s *Server) handleMyReportSchedules(w http.ResponseWriter, r *http.Request) {
	list, err := s.ReportMail.List(r.Context(), personOf(r))
	if err != nil {
		handleStoreError(w, err, "relatórios não encontrados")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"schedules": list, "maxSchedules": reportmail.MaxPerUser, "maxExtraRecipients": reportmail.MaxExtraRecipients,
		"canAddRecipients": reportPerson(r).CanAddRecipients,
	})
}

func (s *Server) handleCreateReportSchedule(w http.ResponseWriter, r *http.Request) {
	var in reportmail.Input
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	schedule, err := s.ReportMail.Create(r.Context(), reportPerson(r), in)
	var input *reportmail.InputError
	if errors.As(err, &input) {
		writeError(w, http.StatusBadRequest, input.Message)
		return
	}
	if err != nil {
		handleStoreError(w, err, "conta não encontrada")
		return
	}
	writeJSON(w, http.StatusCreated, schedule)
}

func (s *Server) handleDeleteReportSchedule(w http.ResponseWriter, r *http.Request) {
	id, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}
	if err := s.ReportMail.Delete(r.Context(), personOf(r), id); err != nil {
		handleStoreError(w, err, "relatório agendado não encontrado")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
