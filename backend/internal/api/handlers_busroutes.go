package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/busroutes"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/tracking"
)

// Os itinerários (as linhas de ônibus) da conta: a linha e os pontos, as
// viagens e o progresso ao vivo (/me/routes); e o link do passageiro
// (/public/routes/{token}).

func writeRouteError(w http.ResponseWriter, err error, notFound string) bool {
	var input *busroutes.InputError
	switch {
	case err == nil:
		return true
	case errors.As(err, &input):
		writeError(w, http.StatusBadRequest, input.Message)
	case errors.Is(err, busroutes.ErrLinkEnded):
		writeError(w, http.StatusGone, err.Error())
	default:
		handleStoreError(w, err, notFound)
	}
	return false
}

func (s *Server) routeFromURL(w http.ResponseWriter, r *http.Request) (uuid.UUID, *busroutes.Route, bool) {
	account, _ := customerOf(r)
	id, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return uuid.Nil, nil, false
	}
	route, err := s.BusRoutes.Route(r.Context(), account, id)
	if !writeRouteError(w, err, "linha não encontrada") {
		return uuid.Nil, nil, false
	}
	return account, route, true
}

func (s *Server) handleRoutes(w http.ResponseWriter, r *http.Request) {
	account, _ := customerOf(r)
	list, err := s.BusRoutes.Routes(r.Context(), account)
	if !writeRouteError(w, err, "linhas não encontradas") {
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleGetRoute(w http.ResponseWriter, r *http.Request) {
	if _, route, ok := s.routeFromURL(w, r); ok {
		writeJSON(w, http.StatusOK, route)
	}
}

func (s *Server) handleSaveRoute(w http.ResponseWriter, r *http.Request) {
	account, _ := customerOf(r)
	var id *uuid.UUID
	if raw := chi.URLParam(r, "id"); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "id inválido")
			return
		}
		id = &parsed
	}
	var in busroutes.Input
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	route, err := s.BusRoutes.Save(r.Context(), account, id, in)
	if !writeRouteError(w, err, "linha não encontrada") {
		return
	}
	s.recordAudit(r, audit.ActionFleetChanged, nil, nil, map[string]any{"routeId": route.ID, "change": "route saved", "stops": len(route.Stops)})
	status := http.StatusOK
	if id == nil {
		status = http.StatusCreated
	}
	writeJSON(w, status, route)
}

func (s *Server) handleDeleteRoute(w http.ResponseWriter, r *http.Request) {
	account, route, ok := s.routeFromURL(w, r)
	if !ok {
		return
	}
	if !writeRouteError(w, s.BusRoutes.Delete(r.Context(), account, route.ID), "linha não encontrada") {
		return
	}
	s.recordAudit(r, audit.ActionFleetChanged, nil, nil, map[string]any{"routeId": route.ID, "change": "route deleted"})
	w.WriteHeader(http.StatusNoContent)
}

// handleCreateTrips: o veículo na linha, na hora pedida (e nos dias seguintes, com days).
func (s *Server) handleCreateTrips(w http.ResponseWriter, r *http.Request) {
	_, route, ok := s.routeFromURL(w, r)
	if !ok {
		return
	}
	var req struct {
		VehicleID uuid.UUID `json:"vehicleId"`
		DepartsAt time.Time `json:"departsAt"`
		Days      int       `json:"days"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	if _, ok := s.fleetVehicle(w, r, req.VehicleID); !ok {
		return
	}
	trips, err := s.BusRoutes.CreateTrips(r.Context(), route.ID, req.VehicleID, personOf(r), req.DepartsAt, req.Days)
	if !writeRouteError(w, err, "linha não encontrada") {
		return
	}
	writeJSON(w, http.StatusCreated, trips)
}

func (s *Server) handleCancelTrip(w http.ResponseWriter, r *http.Request) {
	account, _ := customerOf(r)
	id, err := urlUUID(r, "tripId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}
	if !writeRouteError(w, s.BusRoutes.CancelTrip(r.Context(), account, id), "viagem não encontrada") {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// tripPoints são as posições da viagem (no período dela, até agora).
func (s *Server) tripPoints(ctx context.Context, route *busroutes.Route, t *busroutes.Trip) ([]busroutes.Point, error) {
	if t.DeviceID == nil || t.Canceled {
		return nil, nil
	}
	from, until := busroutes.Window(route, t)
	now := time.Now()
	if now.Before(from) {
		return nil, nil
	}
	if until.After(now) {
		until = now
	}
	result, err := s.Positions.History(ctx, tracking.HistoryQuery{DeviceID: *t.DeviceID, From: from, To: until, Limit: 5000, OnlyValid: true})
	if err != nil {
		return nil, err
	}
	out := make([]busroutes.Point, 0, len(result.Positions))
	for _, p := range result.Positions {
		out = append(out, busroutes.Point{At: p.GPSTimestamp, Latitude: p.Latitude, Longitude: p.Longitude, SpeedKmh: p.SpeedKmh})
	}
	return out, nil
}

// dayOf lê ?dia= (AAAA-MM-DD; sem ele, hoje) e devolve o começo e o fim.
func (s *Server) dayOf(w http.ResponseWriter, r *http.Request) (time.Time, time.Time, bool) {
	loc := s.Reports.Location()
	now := time.Now().In(loc)
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	if raw := r.URL.Query().Get("dia"); raw != "" {
		d, err := time.ParseInLocation(time.DateOnly, raw, loc)
		if err != nil {
			writeError(w, http.StatusBadRequest, "dia inválido (AAAA-MM-DD)")
			return time.Time{}, time.Time{}, false
		}
		day = d
	}
	return day, day.AddDate(0, 0, 1), true
}

// handleRouteDay: as viagens do dia na linha, com o progresso de cada uma.
func (s *Server) handleRouteDay(w http.ResponseWriter, r *http.Request) {
	_, route, ok := s.routeFromURL(w, r)
	if !ok {
		return
	}
	from, to, ok := s.dayOf(w, r)
	if !ok {
		return
	}
	trips, err := s.BusRoutes.Trips(r.Context(), route.ID, from, to)
	if !writeRouteError(w, err, "viagens não encontradas") {
		return
	}
	now := time.Now()
	out := make([]busroutes.TripProgress, 0, len(trips))
	for _, t := range trips {
		points, err := s.tripPoints(r.Context(), route, t)
		if err != nil {
			handleStoreError(w, err, "posições não encontradas")
			return
		}
		out = append(out, busroutes.Progress(route, t, points, now))
	}
	writeJSON(w, http.StatusOK, map[string]any{"route": route, "trips": out})
}

// publicRouteView é o que o passageiro vê: a linha, os pontos e os ônibus
// que estão rodando (ou saem na próxima hora) — sem placa.
type publicRouteView struct {
	Name   string                   `json:"name"`
	Color  string                   `json:"color"`
	Stops  []busroutes.Stop         `json:"stops"`
	Trips  []busroutes.TripProgress `json:"trips"`
	Update time.Time                `json:"updatedAt"`
}

func (s *Server) handlePublicRoute(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	route, err := s.BusRoutes.ByToken(r.Context(), chi.URLParam(r, "token"))
	if !writeRouteError(w, err, "linha não encontrada") {
		return
	}
	now := time.Now()
	last := 0
	if len(route.Stops) > 0 {
		last = route.Stops[len(route.Stops)-1].OffsetMin
	}
	trips, err := s.BusRoutes.Trips(r.Context(), route.ID, now.Add(-time.Duration(last)*time.Minute-3*time.Hour), now.Add(time.Hour))
	if !writeRouteError(w, err, "linha não encontrada") {
		return
	}
	view := publicRouteView{Name: route.Name, Color: route.Color, Stops: route.Stops, Trips: []busroutes.TripProgress{}, Update: now}
	for _, t := range trips {
		points, err := s.tripPoints(r.Context(), route, t)
		if err != nil {
			handleStoreError(w, err, "posições não encontradas")
			return
		}
		p := busroutes.Progress(route, t, points, now)
		if p.Status == busroutes.TripRunning || p.Status == busroutes.TripScheduled {
			p.VehicleID = uuid.Nil
			view.Trips = append(view.Trips, p)
		}
	}
	writeJSON(w, http.StatusOK, view)
}
