package api

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/maintenance"
)

// O hodômetro e a manutenção dos veículos (internal/maintenance). A frota
// inteira fica em /me/maintenance (do cliente: titular e equipe, cada um com
// os veículos que vê); cada veículo, em /vehicles/{id}/maintenance.

func writeMaintenanceError(w http.ResponseWriter, err error, notFound string) bool {
	var input *maintenance.InputError
	switch {
	case err == nil:
		return true
	case errors.As(err, &input):
		writeError(w, http.StatusBadRequest, input.Message)
	default:
		handleStoreError(w, err, notFound)
	}
	return false
}

// myVehicles são os veículos da conta que a requisição vê.
func (s *Server) myVehicles(r *http.Request) (map[uuid.UUID]bool, error) {
	account, _ := customerOf(r)
	list, err := s.Vehicles.ListByOwner(r.Context(), account)
	if err != nil {
		return nil, err
	}
	out := map[uuid.UUID]bool{}
	for _, v := range list {
		if seesVehicle(r, v) {
			out[v.ID] = true
		}
	}
	return out, nil
}

func (s *Server) handleMyMaintenance(w http.ResponseWriter, r *http.Request) {
	mine, err := s.myVehicles(r)
	if err != nil {
		handleStoreError(w, err, "veículos não encontrados")
		return
	}
	ids := make([]uuid.UUID, 0, len(mine))
	for id := range mine {
		ids = append(ids, id)
	}
	items, err := s.Maintenance.Items(r.Context(), ids)
	if !writeMaintenanceError(w, err, "manutenções não encontradas") {
		return
	}
	counts := map[string]int{}
	for _, it := range items {
		counts[it.Status]++
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "counts": counts})
}

type maintenanceCreateRequest struct {
	maintenance.ItemInput
	VehicleIDs []uuid.UUID `json:"vehicleIds"`
}

func (s *Server) handleCreateMaintenance(w http.ResponseWriter, r *http.Request) {
	var req maintenanceCreateRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	mine, err := s.myVehicles(r)
	if err != nil {
		handleStoreError(w, err, "veículos não encontrados")
		return
	}
	for _, id := range req.VehicleIDs {
		if !mine[id] {
			writeError(w, http.StatusNotFound, "veículo não encontrado")
			return
		}
	}
	created, err := s.Maintenance.Create(r.Context(), req.VehicleIDs, req.ItemInput)
	if !writeMaintenanceError(w, err, "veículo não encontrado") {
		return
	}
	s.recordAudit(r, audit.ActionMaintenanceChanged, nil, nil,
		map[string]any{"action": "create", "name": req.Name, "vehicles": created})
	writeJSON(w, http.StatusCreated, map[string]int{"created": created})
}

// maintenanceItem confere que a manutenção da rota é de um veículo que a
// requisição vê.
func (s *Server) maintenanceItem(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return uuid.Nil, false
	}
	return id, s.ownItems(w, r, []uuid.UUID{id})
}

// ownItems confere que todas as manutenções são de veículos que a
// requisição vê.
func (s *Server) ownItems(w http.ResponseWriter, r *http.Request, ids []uuid.UUID) bool {
	vehicleOf, err := s.Maintenance.ItemVehicles(r.Context(), ids)
	if err != nil {
		handleStoreError(w, err, "manutenção não encontrada")
		return false
	}
	mine, err := s.myVehicles(r)
	if err != nil {
		handleStoreError(w, err, "manutenção não encontrada")
		return false
	}
	for _, id := range ids {
		if vehicle, ok := vehicleOf[id]; !ok || !mine[vehicle] {
			writeError(w, http.StatusNotFound, "manutenção não encontrada")
			return false
		}
	}
	return true
}

func (s *Server) handleUpdateMaintenance(w http.ResponseWriter, r *http.Request) {
	id, ok := s.maintenanceItem(w, r)
	if !ok {
		return
	}
	var in maintenance.ItemInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	if !writeMaintenanceError(w, s.Maintenance.Update(r.Context(), id, in), "manutenção não encontrada") {
		return
	}
	s.recordAudit(r, audit.ActionMaintenanceChanged, nil, nil, map[string]any{"action": "update", "itemId": id, "name": in.Name})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeleteMaintenance(w http.ResponseWriter, r *http.Request) {
	id, ok := s.maintenanceItem(w, r)
	if !ok {
		return
	}
	if !writeMaintenanceError(w, s.Maintenance.Delete(r.Context(), id), "manutenção não encontrada") {
		return
	}
	s.recordAudit(r, audit.ActionMaintenanceChanged, nil, nil, map[string]any{"action": "delete", "itemId": id})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMaintenanceDone(w http.ResponseWriter, r *http.Request) {
	var in maintenance.DoneInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	if len(in.ItemIDs) > maintenance.MaxBulk {
		writeError(w, http.StatusBadRequest, "manutenções demais de uma vez")
		return
	}
	if !s.ownItems(w, r, in.ItemIDs) {
		return
	}
	done, err := s.Maintenance.Done(r.Context(), in, personOf(r))
	if !writeMaintenanceError(w, err, "manutenção não encontrada") {
		return
	}
	s.recordAudit(r, audit.ActionMaintenanceChanged, nil, nil,
		map[string]any{"action": "done", "items": done, "date": in.Date})
	writeJSON(w, http.StatusOK, map[string]int{"done": done})
}

// handleVehicleMaintenance: o hodômetro, as manutenções e o histórico do
// veículo.
func (s *Server) handleVehicleMaintenance(w http.ResponseWriter, r *http.Request) {
	vehicle, _, ok := s.vehicleFromURL(w, r, false)
	if !ok {
		return
	}
	ctx := r.Context()
	odometers, err := s.Maintenance.Odometers(ctx, []uuid.UUID{vehicle.ID})
	if !writeMaintenanceError(w, err, "veículo não encontrado") {
		return
	}
	items, err := s.Maintenance.Items(ctx, []uuid.UUID{vehicle.ID})
	if !writeMaintenanceError(w, err, "veículo não encontrado") {
		return
	}
	records, err := s.Maintenance.Records(ctx, vehicle.ID, 50)
	if !writeMaintenanceError(w, err, "veículo não encontrado") {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"odometer": odometers[vehicle.ID], "items": items, "records": records})
}

func (s *Server) handleSetOdometer(w http.ResponseWriter, r *http.Request) {
	vehicle, _, ok := s.vehicleFromURL(w, r, false)
	if !ok {
		return
	}
	var req struct {
		Km float64 `json:"km"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	odometer, err := s.Maintenance.SetOdometer(r.Context(), vehicle.ID, req.Km)
	if !writeMaintenanceError(w, err, "veículo não encontrado") {
		return
	}
	s.recordAudit(r, audit.ActionMaintenanceChanged, &vehicle.ID, vehicle.DeviceID,
		map[string]any{"action": "odometer", "km": req.Km})
	writeJSON(w, http.StatusOK, odometer)
}
