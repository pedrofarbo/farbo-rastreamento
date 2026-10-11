package api

import (
	"errors"
	"net/http"
	"slices"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/vehicles"
)

// A organização da frota da conta: os grupos de veículos e os nomes dos dois
// campos personalizados. Todos da conta veem; o titular e o Gestor mudam.

func writeFleetError(w http.ResponseWriter, err error, notFound string) {
	var validation vehicles.ValidationError
	switch {
	case errors.As(err, &validation):
		writeError(w, http.StatusBadRequest, validation.Message)
	case errors.Is(err, database.ErrNotFound):
		writeError(w, http.StatusNotFound, notFound)
	default:
		handleStoreError(w, err, notFound)
	}
}

func (s *Server) handleMyFleet(w http.ResponseWriter, r *http.Request) {
	owner, _ := customerOf(r)
	s.writeFleet(w, r, owner)
}

// fleetGroupsFor: quem está restrito a grupos vê só os grupos dele.
func fleetGroupsFor(r *http.Request, groups []vehicles.Group) []vehicles.Group {
	principal := restrictedPrincipal(r)
	if principal == nil {
		return groups
	}
	return slices.DeleteFunc(groups, func(g vehicles.Group) bool { return !principal.SeesGroup(&g.ID) })
}

// handleCustomerFleet: a central vê a organização da frota de um cliente
// (para os relatórios dele e o Novo veículo pela ficha).
func (s *Server) handleCustomerFleet(w http.ResponseWriter, r *http.Request) {
	owner, ok := s.customerFromURL(w, r)
	if !ok {
		return
	}
	s.writeFleet(w, r, owner)
}

func (s *Server) writeFleet(w http.ResponseWriter, r *http.Request, owner uuid.UUID) {
	groups, err := s.Vehicles.Groups(r.Context(), owner)
	if err != nil {
		writeFleetError(w, err, "conta não encontrada")
		return
	}
	labels, err := s.Vehicles.Labels(r.Context(), owner)
	if err != nil {
		writeFleetError(w, err, "conta não encontrada")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"groups": fleetGroupsFor(r, groups), "labels": labels,
		"maxGroups": vehicles.MaxGroups, "maxCustomLength": vehicles.MaxCustomLength, "maxLabelLength": vehicles.MaxLabelLength,
	})
}

func (s *Server) handleCreateFleetGroup(w http.ResponseWriter, r *http.Request) {
	owner, _ := customerOf(r)
	var req struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	group, err := s.Vehicles.CreateGroup(r.Context(), owner, req.Name)
	if err != nil {
		writeFleetError(w, err, "conta não encontrada")
		return
	}
	writeJSON(w, http.StatusCreated, group)
}

func (s *Server) handleRenameFleetGroup(w http.ResponseWriter, r *http.Request) {
	owner, _ := customerOf(r)
	id, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	if err := s.Vehicles.RenameGroup(r.Context(), owner, id, req.Name); err != nil {
		writeFleetError(w, err, "grupo não encontrado")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeleteFleetGroup(w http.ResponseWriter, r *http.Request) {
	owner, _ := customerOf(r)
	id, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}
	if err := s.Vehicles.DeleteGroup(r.Context(), owner, id); err != nil {
		writeFleetError(w, err, "grupo não encontrado")
		return
	}
	s.vehiclesChanged(r)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSetFleetLabels(w http.ResponseWriter, r *http.Request) {
	owner, _ := customerOf(r)
	var req vehicles.FieldLabels
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	labels, err := s.Vehicles.SetLabels(r.Context(), owner, req)
	if err != nil {
		writeFleetError(w, err, "conta não encontrada")
		return
	}
	writeJSON(w, http.StatusOK, labels)
}
