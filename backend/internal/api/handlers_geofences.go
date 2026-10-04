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
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleCreateGeofence(w http.ResponseWriter, r *http.Request) {
	var in geofences.Input
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
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
