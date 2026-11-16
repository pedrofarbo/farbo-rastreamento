package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/addresses"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/adhesions"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/contract"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/inspections"
)

// writeAdhesionError traduz os erros da adesão (e os do cadastro do
// associado e do veículo na aprovação).
func writeAdhesionError(w http.ResponseWriter, r *http.Request, err error, notFound string) bool {
	var input *adhesions.InputError
	var inspection *inspections.InputError
	var address addresses.ValidationError
	switch {
	case err == nil:
		return true
	case errors.As(err, &input):
		writeError(w, http.StatusBadRequest, input.Message)
	case errors.As(err, &inspection):
		writeError(w, http.StatusBadRequest, inspection.Message)
	case errors.As(err, &address):
		writeError(w, http.StatusBadRequest, address.Message)
	default:
		return writeOrgError(w, r, err, notFound)
	}
	return false
}

// publicAdhesionView é o que o link de acompanhamento mostra: só o primeiro
// nome, sem o CPF, o e-mail, o celular nem o endereço.
type publicAdhesionView struct {
	OrganizationName  string     `json:"organizationName"`
	Status            string     `json:"status"`
	FirstName         string     `json:"firstName"`
	Plate             string     `json:"plate"`
	VehicleName       string     `json:"vehicleName"`
	InspectionStatus  string     `json:"inspectionStatus"`
	InspectionLink    string     `json:"inspectionLink"`
	InspectionExpired bool       `json:"inspectionExpired"`
	InspectionNote    string     `json:"inspectionNote"`
	AcceptedAt        *time.Time `json:"acceptedAt"`
	InstallPreference string     `json:"installPreference"`
	ReviewNote        string     `json:"reviewNote"`
	// FollowToken: o link de acompanhamento (na resposta da adesão nova).
	FollowToken string `json:"followToken,omitempty"`
}

func (s *Server) toPublicAdhesion(a *adhesions.Adhesion) publicAdhesionView {
	first, _, _ := strings.Cut(strings.TrimSpace(a.Name), " ")
	out := publicAdhesionView{
		OrganizationName: a.OrganizationName, Status: a.Status, FirstName: first, Plate: a.Plate,
		VehicleName: a.VehicleName(), AcceptedAt: a.AcceptedAt, InstallPreference: a.InstallPref,
	}
	if a.Status == adhesions.StatusRejected {
		out.ReviewNote = a.ReviewNote
	}
	if in := a.Inspection; in != nil {
		out.InspectionStatus = in.Status
		out.InspectionLink = s.Inspections.PublicURL(in)
		out.InspectionExpired = in.Expired(s.Inspections.Now())
		if in.RedoOf != nil && in.Status == inspections.StatusPending {
			out.InspectionNote = in.RequestNote
		}
	}
	return out
}

func noIndex(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
}

// handlePublicAdhesionOrg: a associação do link de adesão (e se está aberto).
func (s *Server) handlePublicAdhesionOrg(w http.ResponseWriter, r *http.Request) {
	noIndex(w)
	org, err := s.Adhesions.OrgByToken(r.Context(), chi.URLParam(r, "token"))
	if !writeAdhesionError(w, r, err, "link não encontrado") {
		return
	}
	writeJSON(w, http.StatusOK, org)
}

// handlePublicAdhesionStart: a adesão nova; a resposta traz o link de
// acompanhamento (e, nele, o da vistoria).
func (s *Server) handlePublicAdhesionStart(w http.ResponseWriter, r *http.Request) {
	noIndex(w)
	var in adhesions.Input
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	a, err := s.Adhesions.Start(r.Context(), chi.URLParam(r, "token"), in)
	if !writeAdhesionError(w, r, err, "link não encontrado") {
		return
	}
	view := s.toPublicAdhesion(a)
	_, view.FollowToken, _ = strings.Cut(a.FollowURL, "/adesao/acompanhar/")
	writeJSON(w, http.StatusCreated, view)
}

func (s *Server) handlePublicAdhesion(w http.ResponseWriter, r *http.Request) {
	noIndex(w)
	a, err := s.Adhesions.ByFollowToken(r.Context(), chi.URLParam(r, "token"))
	if !writeAdhesionError(w, r, err, "link não encontrado") {
		return
	}
	writeJSON(w, http.StatusOK, s.toPublicAdhesion(a))
}

type adhesionAcceptRequest struct {
	Version    string `json:"version"`
	Preference string `json:"preference"`
}

// handlePublicAdhesionAccept: o aceite do termo do associado em vigor (a
// versão lida na tela tem que ser a de agora) e a preferência da instalação.
func (s *Server) handlePublicAdhesionAccept(w http.ResponseWriter, r *http.Request) {
	noIndex(w)
	var req adhesionAcceptRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	var term *contract.Document
	if s.Contract != nil {
		term = s.Contract.AssociateTerm()
	}
	if term == nil {
		writeError(w, http.StatusServiceUnavailable, "o termo do associado está indisponível agora: tente mais tarde")
		return
	}
	if req.Version != term.Version {
		writeError(w, http.StatusBadRequest, "o termo foi atualizado: recarregue a página para ler a versão nova")
		return
	}
	a, err := s.Adhesions.Accept(r.Context(), chi.URLParam(r, "token"), adhesions.AcceptInput{
		TermVersion: term.Version, TermSHA256: term.SHA256, Preference: req.Preference, IP: clientIP(r), UserAgent: r.UserAgent(),
	})
	if !writeAdhesionError(w, r, err, "link não encontrado") {
		return
	}
	writeJSON(w, http.StatusOK, s.toPublicAdhesion(a))
}

// ---------------------------------------------------------------------------
// O painel da associação
// ---------------------------------------------------------------------------

func (s *Server) handleOrgAdhesions(w http.ResponseWriter, r *http.Request) {
	orgID, _ := orgOf(r)
	list, err := s.Adhesions.List(r.Context(), orgID, r.URL.Query().Get("situacao"))
	if !writeAdhesionError(w, r, err, "adesões não encontradas") {
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleOrgAdhesion(w http.ResponseWriter, r *http.Request) {
	orgID, _ := orgOf(r)
	id, err := urlUUID(r, "adhesionId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}
	a, err := s.Adhesions.Get(r.Context(), orgID, id)
	if !writeAdhesionError(w, r, err, "adesão não encontrada") {
		return
	}
	writeJSON(w, http.StatusOK, a)
}

// handleOrgApproveAdhesion: cadastra o associado e o veículo; o aceite do
// termo feito no link vale como o aceite eletrônico da conta nova (se a
// versão ainda for a de agora; senão, ele aceita no primeiro acesso).
func (s *Server) handleOrgApproveAdhesion(w http.ResponseWriter, r *http.Request) {
	orgID, _ := orgOf(r)
	id, err := urlUUID(r, "adhesionId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}
	a, err := s.Adhesions.Approve(r.Context(), orgID, id, personOf(r))
	if !writeAdhesionError(w, r, err, "adesão não encontrada") {
		return
	}
	if s.Contract != nil && a.AssociateID != nil && a.TermVersion != "" {
		ip, ua := a.Acceptance()
		if _, err := s.Contract.Accept(r.Context(), *a.AssociateID, a.TermVersion, a.Document, ip, ua); err != nil {
			s.Log.Info("o aceite do termo da adesão não passou para a conta (o associado aceita no primeiro acesso)",
				"adhesion", a.ID, "err", err)
		}
	}
	s.recordAudit(r, audit.ActionOrgAssociateChanged, a.VehicleID, nil, map[string]any{
		"organizationId": orgID, "adhesionId": a.ID, "change": "adhesion approved", "associateId": a.AssociateID,
	})
	writeJSON(w, http.StatusOK, a)
}

type adhesionRejectRequest struct {
	Note string `json:"note"`
}

func (s *Server) handleOrgRejectAdhesion(w http.ResponseWriter, r *http.Request) {
	orgID, _ := orgOf(r)
	id, err := urlUUID(r, "adhesionId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}
	var req adhesionRejectRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	a, err := s.Adhesions.Reject(r.Context(), orgID, id, personOf(r), req.Note)
	if !writeAdhesionError(w, r, err, "adesão não encontrada") {
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) handleOrgAdhesionSettings(w http.ResponseWriter, r *http.Request) {
	orgID, _ := orgOf(r)
	out, err := s.Adhesions.SettingsOf(r.Context(), orgID)
	if !writeAdhesionError(w, r, err, "organização não encontrada") {
		return
	}
	writeJSON(w, http.StatusOK, out)
}

type adhesionSettingsRequest struct {
	Enabled bool `json:"enabled"`
}

func (s *Server) handleOrgSetAdhesionSettings(w http.ResponseWriter, r *http.Request) {
	orgID, _ := orgOf(r)
	var req adhesionSettingsRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	out, err := s.Adhesions.SetEnabled(r.Context(), orgID, req.Enabled)
	if !writeAdhesionError(w, r, err, "organização não encontrada") {
		return
	}
	writeJSON(w, http.StatusOK, out)
}
