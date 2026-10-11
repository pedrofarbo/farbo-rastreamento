package api

import (
	"context"
	"errors"
	"net/http"
	"slices"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/geofences"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/tracking"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/vehicles"
)

// fenceOwner é o dono das cercas que a requisição enxerga: o cliente vê e
// mexe só nas dele; a equipe, só nas da central (dono nulo).
func fenceOwner(r *http.Request) *uuid.UUID {
	if customerID, isCustomer := customerOf(r); isCustomer {
		return &customerID
	}
	return nil
}

func (s *Server) handleListGeofences(w http.ResponseWriter, r *http.Request) {
	list, err := s.Geofences.ListByOwner(r.Context(), fenceOwner(r))
	if err != nil {
		handleStoreError(w, err, "cercas não encontradas")
		return
	}
	// Quem está restrito a grupos vê as cercas dos veículos dele, só com eles.
	visible, err := s.visibleVehicles(r)
	if err != nil {
		handleStoreError(w, err, "cercas não encontradas")
		return
	}
	if visible != nil {
		list = visibleFences(list, visible)
	}
	writeJSON(w, http.StatusOK, list)
}

// restrictedFence confere, para quem está restrito a grupos, a cerca que ele
// quer mexer: precisa vigiar algum veículo dele. Devolve os veículos da
// cerca que ele não vê (ficam como estão) e false se já respondeu.
func (s *Server) restrictedFence(w http.ResponseWriter, r *http.Request, id uuid.UUID, visible map[uuid.UUID]bool) ([]uuid.UUID, bool) {
	list, err := s.Geofences.ListByOwner(r.Context(), fenceOwner(r))
	if err != nil {
		handleStoreError(w, err, "cerca não encontrada")
		return nil, false
	}
	i := slices.IndexFunc(list, func(g *geofences.Geofence) bool { return g.ID == id })
	if i < 0 || len(visibleFences(list[i:i+1], visible)) == 0 {
		writeError(w, http.StatusNotFound, "cerca não encontrada")
		return nil, false
	}
	hidden := []uuid.UUID{}
	for _, v := range list[i].VehicleIDs {
		if !visible[v] {
			hidden = append(hidden, v)
		}
	}
	return hidden, true
}

// onlyVisible recusa a escolha de veículo que a pessoa restrita não vê. Sem
// veículo nenhum, só se allowEmpty (a cerca segue com os de outros grupos).
func onlyVisible(w http.ResponseWriter, chosen []uuid.UUID, visible map[uuid.UUID]bool, allowEmpty bool) bool {
	if len(chosen) == 0 && !allowEmpty {
		writeError(w, http.StatusBadRequest, "escolha os veículos que a cerca vigia")
		return false
	}
	for _, id := range chosen {
		if !visible[id] {
			writeCode(w, http.StatusForbidden, "só dá para escolher veículos dos seus grupos", codeRestrictedAccess)
			return false
		}
	}
	return true
}

// handleGeofenceLimit: quantas cercas o cliente pode ter (cresce com a
// frota) e quantas já tem. As da central não têm limite (limit nulo).
func (s *Server) handleGeofenceLimit(w http.ResponseWriter, r *http.Request) {
	owner := fenceOwner(r)
	if owner == nil {
		writeJSON(w, http.StatusOK, map[string]any{"limit": nil, "used": 0})
		return
	}
	limit, used, err := s.Geofences.Limit(r.Context(), *owner)
	if err != nil {
		handleStoreError(w, err, "cercas não encontradas")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"limit": limit, "used": used})
}

func (s *Server) handleCreateGeofence(w http.ResponseWriter, r *http.Request) {
	var in geofences.Input
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}

	visible, err := s.visibleVehicles(r)
	if err != nil {
		handleStoreError(w, err, "veículos não encontrados")
		return
	}
	if visible != nil && !onlyVisible(w, in.VehicleIDs, visible, false) {
		return
	}

	owner := fenceOwner(r)
	fence, err := s.Geofences.Create(r.Context(), owner, in)
	if err != nil {
		writeGeofenceError(w, err)
		return
	}

	s.recordAudit(r, audit.ActionGeofenceChanged, nil, nil,
		map[string]any{"action": "create", "geofenceId": fence.ID, "name": fence.Name, "ownerId": owner})
	writeJSON(w, http.StatusCreated, fence)
}

func (s *Server) handleUpdateGeofence(w http.ResponseWriter, r *http.Request) {
	id, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}

	var in geofences.Input
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}

	visible, err := s.visibleVehicles(r)
	if err != nil {
		handleStoreError(w, err, "veículos não encontrados")
		return
	}
	if visible != nil {
		// Os veículos de outros grupos continuam na cerca, como estavam.
		hidden, ok := s.restrictedFence(w, r, id, visible)
		if !ok || !onlyVisible(w, in.VehicleIDs, visible, len(hidden) > 0) {
			return
		}
		in.VehicleIDs = append(in.VehicleIDs, hidden...)
	}

	owner := fenceOwner(r)
	fence, err := s.Geofences.Update(r.Context(), id, owner, in)
	if err != nil {
		writeGeofenceError(w, err)
		return
	}

	s.recordAudit(r, audit.ActionGeofenceChanged, nil, nil,
		map[string]any{"action": "update", "geofenceId": fence.ID, "ownerId": owner})
	writeJSON(w, http.StatusOK, fence)
}

func (s *Server) handleDeleteGeofence(w http.ResponseWriter, r *http.Request) {
	id, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}
	visible, err := s.visibleVehicles(r)
	if err != nil {
		handleStoreError(w, err, "veículos não encontrados")
		return
	}
	if visible != nil {
		hidden, ok := s.restrictedFence(w, r, id, visible)
		if !ok {
			return
		}
		if len(hidden) > 0 {
			writeCode(w, http.StatusForbidden,
				"esta cerca também vigia veículos de outros grupos: tire dela os seus veículos em vez de apagá-la", codeRestrictedAccess)
			return
		}
	}

	owner := fenceOwner(r)
	if err := s.Geofences.Delete(r.Context(), id, owner); err != nil {
		handleStoreError(w, err, "cerca não encontrada")
		return
	}

	s.recordAudit(r, audit.ActionGeofenceChanged, nil, nil,
		map[string]any{"action": "delete", "geofenceId": id, "ownerId": owner})
	writeJSON(w, http.StatusNoContent, nil)
}

func writeGeofenceError(w http.ResponseWriter, err error) {
	var validation geofences.ValidationError
	if errors.As(err, &validation) {
		writeError(w, http.StatusBadRequest, validation.Message)
		return
	}
	handleStoreError(w, err, "cerca não encontrada")
}

// fenceBaseline marca, pela última posição, quais veículos vigiados pela
// cerca já estão dentro dela — antes de ela valer para a ingestão. Assim
// criar a cerca "Casa" com o carro na garagem não avisa "entrou em Casa", e
// aumentar ou mover a cerca não avisa "saiu". Daí em diante, só mudança de
// verdade vira evento.
func (s *Server) fenceBaseline(ctx context.Context, fence *geofences.Geofence) {
	if s.Vehicles == nil || s.Positions == nil || s.States == nil {
		return
	}
	var (
		list []*vehicles.Vehicle
		err  error
	)
	if fence.OwnerID == nil {
		list, err = s.Vehicles.List(ctx)
	} else {
		list, err = s.Vehicles.ListByOwner(ctx, *fence.OwnerID)
	}
	if err != nil {
		s.Log.Warn("cerca salva sem marcar quem já está dentro", "geofence", fence.ID, "err", err)
		return
	}

	// As últimas posições dos veículos vigiados, numa consulta só (a cerca da
	// central vale para a frota toda).
	watches := func(vehicle *vehicles.Vehicle) bool {
		return vehicle.DeviceID != nil && fence.Active &&
			fence.AppliesTo(geofences.Subject{VehicleID: &vehicle.ID, OwnerID: vehicle.OwnerID})
	}
	watched := []uuid.UUID{}
	for _, vehicle := range list {
		if watches(vehicle) {
			watched = append(watched, *vehicle.DeviceID)
		}
	}
	latest, err := s.Positions.LatestFor(ctx, watched)
	if err != nil {
		s.Log.Warn("cerca salva sem marcar quem já está dentro", "geofence", fence.ID, "err", err)
		return
	}

	for _, vehicle := range list {
		if vehicle.DeviceID == nil {
			continue
		}
		deviceID := *vehicle.DeviceID
		inside := false
		if watches(vehicle) {
			position := latest[deviceID]
			inside = position != nil && fence.Contains(position.Latitude, position.Longitude)
		}
		st := s.States.Get(deviceID)
		if was := st != nil && slices.Contains(st.InsideFences, fence.ID); was == inside {
			continue
		}
		s.States.Update(ctx, deviceID, func(st *tracking.State) {
			st.InsideFences = slices.DeleteFunc(st.InsideFences, func(id uuid.UUID) bool { return id == fence.ID })
			if inside {
				st.InsideFences = append(st.InsideFences, fence.ID)
			}
		})
	}
}
