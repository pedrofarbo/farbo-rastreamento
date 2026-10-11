package api

import (
	"errors"
	"net/http"

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
		"groups": groups, "labels": labels,
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
