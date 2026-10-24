package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/livelinks"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/stepup"
)

// A localização ao vivo por um tempo: o cliente gera um link sem login para
// alguém acompanhar o veículo por algumas horas, e encerra quando quiser.

type liveLinksView struct {
	Links     []*livelinks.Link `json:"links"`
	Durations []int             `json:"durations"`
	MaxActive int               `json:"maxActive"`
}

// handleVehicleLiveLinks: os links ativos do veículo.
func (s *Server) handleVehicleLiveLinks(w http.ResponseWriter, r *http.Request) {
	vehicle, _, ok := s.vehicleFromURL(w, r, false)
	if !ok {
		return
	}
	links, err := s.LiveLinks.Active(r.Context(), vehicle.ID)
	if err != nil {
		s.Log.Error("falha ao listar os links ao vivo", "vehicle", vehicle.ID, "err", err)
		writeError(w, http.StatusInternalServerError, "erro interno")
		return
	}
	writeJSON(w, http.StatusOK, liveLinksView{Links: links, Durations: livelinks.Durations, MaxActive: livelinks.MaxActive})
}

type createLiveLinkRequest struct {
	Hours int    `json:"hours"`
	Label string `json:"label"`
}

// handleCreateLiveLink gera um link (com a confirmação extra: quem pega o
// celular não manda a localização para fora).
func (s *Server) handleCreateLiveLink(w http.ResponseWriter, r *http.Request) {
	vehicle, _, ok := s.vehicleFromURL(w, r, true)
	if !ok {
		return
	}
	var req createLiveLinkRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	if !s.requireStepUp(w, r, stepup.PurposeVehicleShare) {
		return
	}
	link, err := s.LiveLinks.Create(r.Context(), vehicle.ID, personOf(r), req.Hours, req.Label)
	var inputErr *livelinks.InputError
	if errors.As(err, &inputErr) {
		writeError(w, http.StatusUnprocessableEntity, inputErr.Message)
		return
	}
	if err != nil {
		s.Log.Error("falha ao gerar o link ao vivo", "vehicle", vehicle.ID, "err", err)
		writeError(w, http.StatusInternalServerError, "erro interno")
		return
	}
	s.recordAudit(r, audit.ActionLiveLinkCreated, &vehicle.ID, nil, map[string]any{
		"linkId": link.ID, "hours": req.Hours, "label": link.Label, "expiresAt": link.ExpiresAt,
	})
	writeJSON(w, http.StatusCreated, link)
}

// handleRevokeLiveLink encerra o link antes do prazo.
func (s *Server) handleRevokeLiveLink(w http.ResponseWriter, r *http.Request) {
	vehicle, _, ok := s.vehicleFromURL(w, r, false)
	if !ok {
		return
	}
	id, err := urlUUID(r, "linkId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}
	if err := s.LiveLinks.Revoke(r.Context(), vehicle.ID, id); err != nil {
		handleStoreError(w, err, "link não encontrado ou já encerrado")
		return
	}
	s.recordAudit(r, audit.ActionLiveLinkRevoked, &vehicle.ID, nil, map[string]any{"linkId": id})
	w.WriteHeader(http.StatusNoContent)
}

// publicLiveView é o que o link ao vivo mostra: o veículo, onde ele está e
// o primeiro nome de quem mandou — nada mais do dono.
type publicLiveView struct {
	Vehicle   publicVehicle `json:"vehicle"`
	SharedBy  string        `json:"sharedBy"`
	ExpiresAt time.Time     `json:"expiresAt"`
	publicLocation
}

// handlePublicLive: a posição ao vivo pelo link, enquanto ele valer.
func (s *Server) handlePublicLive(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	gone := func() {
		writeError(w, http.StatusGone, "este link de localização não está mais ativo")
	}
	link, err := s.LiveLinks.ByToken(r.Context(), chi.URLParam(r, "token"))
	if errors.Is(err, livelinks.ErrLinkEnded) {
		gone()
		return
	}
	if err != nil {
		handleStoreError(w, err, "link não encontrado")
		return
	}
	vehicle, err := s.Vehicles.Get(r.Context(), link.VehicleID)
	if err != nil {
		handleStoreError(w, err, "link não encontrado")
		return
	}
	// Com a conta suspensa, o link para junto (senão contornaria a suspensão).
	if vehicle.OwnerID == nil {
		gone()
		return
	}
	if suspended, err := s.Billing.IsSuspended(r.Context(), *vehicle.OwnerID); err != nil || suspended {
		gone()
		return
	}
	view := publicLiveView{Vehicle: publicVehicleOf(vehicle), ExpiresAt: link.ExpiresAt}
	view.SharedBy, _, _ = strings.Cut(strings.TrimSpace(link.CreatedByName), " ")
	view.publicLocation = s.publicLocationOf(r, vehicle)
	writeJSON(w, http.StatusOK, view)
}
