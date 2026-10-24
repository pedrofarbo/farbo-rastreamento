package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/fleet"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/reports"
)

// A gestão da frota da conta (internal/fleet): os motoristas, os custos, as
// multas e as locações (/me), e os relatórios de custo e de jornada
// (/reports). Cada um vê e mexe só nos veículos que vê.

func writeFleetOpsError(w http.ResponseWriter, err error, notFound string) bool {
	var input *fleet.InputError
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

// fleetVehicles são os veículos da conta que a pessoa vê (a lista e o conjunto).
func (s *Server) fleetVehicles(w http.ResponseWriter, r *http.Request) (uuid.UUID, []uuid.UUID, map[uuid.UUID]bool, bool) {
	account, _ := customerOf(r)
	mine, err := s.myVehicles(r)
	if err != nil {
		handleStoreError(w, err, "veículos não encontrados")
		return uuid.Nil, nil, nil, false
	}
	ids := make([]uuid.UUID, 0, len(mine))
	for id := range mine {
		ids = append(ids, id)
	}
	return account, ids, mine, true
}

// fleetVehicle confere que o veículo do pedido é da conta e que a pessoa o vê.
func (s *Server) fleetVehicle(w http.ResponseWriter, r *http.Request, id uuid.UUID) (uuid.UUID, bool) {
	account, _, mine, ok := s.fleetVehicles(w, r)
	if !ok {
		return uuid.Nil, false
	}
	if !mine[id] {
		writeError(w, http.StatusNotFound, "veículo não encontrado")
		return uuid.Nil, false
	}
	return account, true
}

// odometer é o hodômetro de agora de um veículo (para a locação).
func (s *Server) odometer(ctx context.Context, vehicleID uuid.UUID) (*float64, error) {
	if s.Maintenance == nil {
		return nil, nil
	}
	list, err := s.Maintenance.Odometers(ctx, []uuid.UUID{vehicleID})
	if err != nil {
		return nil, err
	}
	return list[vehicleID].Km, nil
}

// ---------------------------------------------------------------------------
// Motoristas
// ---------------------------------------------------------------------------

func (s *Server) handleDrivers(w http.ResponseWriter, r *http.Request) {
	account, _ := customerOf(r)
	list, err := s.Fleet.Drivers(r.Context(), account)
	if !writeFleetOpsError(w, err, "motoristas não encontrados") {
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleCreateDriver(w http.ResponseWriter, r *http.Request) {
	account, _ := customerOf(r)
	var in fleet.DriverInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	d, err := s.Fleet.CreateDriver(r.Context(), account, in)
	if !writeFleetOpsError(w, err, "motorista não encontrado") {
		return
	}
	s.recordAudit(r, audit.ActionFleetChanged, nil, nil, map[string]any{"driverId": d.ID, "change": "driver created"})
	writeJSON(w, http.StatusCreated, d)
}

func (s *Server) handleUpdateDriver(w http.ResponseWriter, r *http.Request) {
	account, _ := customerOf(r)
	id, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}
	var in fleet.DriverInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	d, err := s.Fleet.UpdateDriver(r.Context(), account, id, in)
	if !writeFleetOpsError(w, err, "motorista não encontrado") {
		return
	}
	s.recordAudit(r, audit.ActionFleetChanged, nil, nil, map[string]any{"driverId": d.ID, "change": "driver updated"})
	writeJSON(w, http.StatusOK, d)
}

// handleAssignDriver: quem dirige o veículo a partir de agora (nulo: ninguém).
func (s *Server) handleAssignDriver(w http.ResponseWriter, r *http.Request) {
	vehicleID, err := urlUUID(r, "vehicleId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}
	account, ok := s.fleetVehicle(w, r, vehicleID)
	if !ok {
		return
	}
	var req struct {
		DriverID *uuid.UUID `json:"driverId"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	if !writeFleetOpsError(w, s.Fleet.Assign(r.Context(), account, vehicleID, req.DriverID, personOf(r)), "motorista não encontrado") {
		return
	}
	s.recordAudit(r, audit.ActionFleetChanged, &vehicleID, nil, map[string]any{"driverId": req.DriverID, "change": "driver assigned"})
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Custos
// ---------------------------------------------------------------------------

// periodOf lê ?de= e ?ate= (AAAA-MM-DD); sem eles, o mês corrente.
func (s *Server) periodOf(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	now := time.Now().In(s.Reports.Location())
	from := r.URL.Query().Get("de")
	to := r.URL.Query().Get("ate")
	if from == "" {
		from = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).Format(time.DateOnly)
	}
	if to == "" {
		to = now.Format(time.DateOnly)
	}
	a, err1 := time.Parse(time.DateOnly, from)
	b, err2 := time.Parse(time.DateOnly, to)
	if err1 != nil || err2 != nil || b.Before(a) || b.Sub(a) > 400*24*time.Hour {
		writeError(w, http.StatusBadRequest, "período inválido (de ?de= a ?ate=, AAAA-MM-DD, até 400 dias)")
		return "", "", false
	}
	return from, to, true
}

func (s *Server) handleExpenses(w http.ResponseWriter, r *http.Request) {
	account, ids, _, ok := s.fleetVehicles(w, r)
	if !ok {
		return
	}
	from, to, ok := s.periodOf(w, r)
	if !ok {
		return
	}
	list, err := s.Fleet.Expenses(r.Context(), account, ids, from, to)
	if !writeFleetOpsError(w, err, "custos não encontrados") {
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleCreateExpense(w http.ResponseWriter, r *http.Request) {
	var in fleet.ExpenseInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	account, ok := s.fleetVehicle(w, r, in.VehicleID)
	if !ok {
		return
	}
	e, err := s.Fleet.CreateExpense(r.Context(), account, personOf(r), in)
	if !writeFleetOpsError(w, err, "veículo não encontrado") {
		return
	}
	s.recordAudit(r, audit.ActionFleetChanged, &e.VehicleID, nil, map[string]any{
		"expenseId": e.ID, "change": "expense created", "kind": e.Kind, "amountCents": e.AmountCents,
	})
	writeJSON(w, http.StatusCreated, e)
}

func (s *Server) handleDeleteExpense(w http.ResponseWriter, r *http.Request) {
	account, _, mine, ok := s.fleetVehicles(w, r)
	if !ok {
		return
	}
	id, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}
	list, err := s.Fleet.Expenses(r.Context(), account, keys(mine), "1900-01-01", "2999-12-31")
	if !writeFleetOpsError(w, err, "custo não encontrado") {
		return
	}
	found := false
	for _, e := range list {
		found = found || e.ID == id
	}
	if !found {
		writeError(w, http.StatusNotFound, "custo não encontrado")
		return
	}
	e, err := s.Fleet.DeleteExpense(r.Context(), account, id)
	if !writeFleetOpsError(w, err, "custo não encontrado") {
		return
	}
	s.recordAudit(r, audit.ActionFleetChanged, &e.VehicleID, nil, map[string]any{
		"expenseId": e.ID, "change": "expense deleted", "amountCents": e.AmountCents,
	})
	w.WriteHeader(http.StatusNoContent)
}

func keys(m map[uuid.UUID]bool) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(m))
	for id := range m {
		out = append(out, id)
	}
	return out
}

// handleReportCosts: os custos de cada veículo no período (do relatório),
// com o custo por km e o consumo.
func (s *Server) handleReportCosts(w http.ResponseWriter, r *http.Request) {
	q, ok := s.reportScope(w, r)
	if !ok {
		return
	}
	if q.account == uuid.Nil || s.Fleet == nil {
		writeError(w, http.StatusNotFound, "os custos são da frota de uma conta")
		return
	}
	ids := make([]uuid.UUID, len(q.vehicles))
	for i, v := range q.vehicles {
		ids[i] = v.ID
	}
	rows, err := s.Fleet.Costs(r.Context(), q.account, ids, day(q.from), day(q.to))
	if !writeFleetOpsError(w, err, "custos não encontrados") {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"from": day(q.from), "to": day(q.to), "vehicles": q.vehicles, "rows": rows})
}

// ---------------------------------------------------------------------------
// Multas
// ---------------------------------------------------------------------------

func (s *Server) handleFines(w http.ResponseWriter, r *http.Request) {
	account, ids, _, ok := s.fleetVehicles(w, r)
	if !ok {
		return
	}
	list, err := s.Fleet.Fines(r.Context(), account, ids)
	if !writeFleetOpsError(w, err, "multas não encontradas") {
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleCreateFine(w http.ResponseWriter, r *http.Request) {
	var in fleet.FineInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	account, ok := s.fleetVehicle(w, r, in.VehicleID)
	if !ok {
		return
	}
	f, err := s.Fleet.CreateFine(r.Context(), account, personOf(r), in)
	if !writeFleetOpsError(w, err, "veículo não encontrado") {
		return
	}
	s.recordAudit(r, audit.ActionFleetChanged, &f.VehicleID, nil, map[string]any{"fineId": f.ID, "change": "fine created"})
	writeJSON(w, http.StatusCreated, f)
}

// fineFor é a multa da rota, de um veículo que a pessoa vê.
func (s *Server) fineFor(w http.ResponseWriter, r *http.Request) (uuid.UUID, *fleet.Fine, bool) {
	account, ids, _, ok := s.fleetVehicles(w, r)
	if !ok {
		return uuid.Nil, nil, false
	}
	id, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return uuid.Nil, nil, false
	}
	list, err := s.Fleet.Fines(r.Context(), account, ids)
	if !writeFleetOpsError(w, err, "multa não encontrada") {
		return uuid.Nil, nil, false
	}
	for _, f := range list {
		if f.ID == id {
			return account, f, true
		}
	}
	writeError(w, http.StatusNotFound, "multa não encontrada")
	return uuid.Nil, nil, false
}

func (s *Server) handleUpdateFine(w http.ResponseWriter, r *http.Request) {
	account, current, ok := s.fineFor(w, r)
	if !ok {
		return
	}
	var in fleet.FineInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	f, err := s.Fleet.UpdateFine(r.Context(), account, current.ID, in)
	if !writeFleetOpsError(w, err, "multa não encontrada") {
		return
	}
	s.recordAudit(r, audit.ActionFleetChanged, &f.VehicleID, nil, map[string]any{"fineId": f.ID, "change": "fine updated", "status": f.Status})
	writeJSON(w, http.StatusOK, f)
}

func (s *Server) handleDeleteFine(w http.ResponseWriter, r *http.Request) {
	account, current, ok := s.fineFor(w, r)
	if !ok {
		return
	}
	if !writeFleetOpsError(w, s.Fleet.DeleteFine(r.Context(), account, current.ID), "multa não encontrada") {
		return
	}
	s.recordAudit(r, audit.ActionFleetChanged, &current.VehicleID, nil, map[string]any{"fineId": current.ID, "change": "fine deleted"})
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Locações
// ---------------------------------------------------------------------------

func (s *Server) handleRentals(w http.ResponseWriter, r *http.Request) {
	account, ids, _, ok := s.fleetVehicles(w, r)
	if !ok {
		return
	}
	list, err := s.Fleet.Rentals(r.Context(), account, ids)
	if !writeFleetOpsError(w, err, "locações não encontradas") {
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleCreateRental(w http.ResponseWriter, r *http.Request) {
	var in fleet.RentalInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	account, ok := s.fleetVehicle(w, r, in.VehicleID)
	if !ok {
		return
	}
	rental, err := s.Fleet.CreateRental(r.Context(), account, personOf(r), in, s.odometer)
	if !writeFleetOpsError(w, err, "veículo não encontrado") {
		return
	}
	s.recordAudit(r, audit.ActionFleetChanged, &rental.VehicleID, nil, map[string]any{"rentalId": rental.ID, "change": "rental created"})
	writeJSON(w, http.StatusCreated, rental)
}

// rentalFor é a locação da rota, de um veículo que a pessoa vê.
func (s *Server) rentalFor(w http.ResponseWriter, r *http.Request) (uuid.UUID, *fleet.Rental, bool) {
	account, ids, _, ok := s.fleetVehicles(w, r)
	if !ok {
		return uuid.Nil, nil, false
	}
	id, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return uuid.Nil, nil, false
	}
	list, err := s.Fleet.Rentals(r.Context(), account, ids)
	if !writeFleetOpsError(w, err, "locação não encontrada") {
		return uuid.Nil, nil, false
	}
	for _, rental := range list {
		if rental.ID == id {
			return account, rental, true
		}
	}
	writeError(w, http.StatusNotFound, "locação não encontrada")
	return uuid.Nil, nil, false
}

func (s *Server) handleReturnRental(w http.ResponseWriter, r *http.Request) {
	account, current, ok := s.rentalFor(w, r)
	if !ok {
		return
	}
	var req struct {
		KmEnd *float64 `json:"kmEnd"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	rental, err := s.Fleet.ReturnRental(r.Context(), account, current.ID, req.KmEnd, s.odometer)
	if !writeFleetOpsError(w, err, "locação não encontrada") {
		return
	}
	s.recordAudit(r, audit.ActionFleetChanged, &rental.VehicleID, nil, map[string]any{"rentalId": rental.ID, "change": "rental returned"})
	writeJSON(w, http.StatusOK, rental)
}

func (s *Server) handleExtendRental(w http.ResponseWriter, r *http.Request) {
	account, current, ok := s.rentalFor(w, r)
	if !ok {
		return
	}
	var req struct {
		EndsAt time.Time `json:"endsAt"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	rental, err := s.Fleet.ExtendRental(r.Context(), account, current.ID, req.EndsAt)
	if !writeFleetOpsError(w, err, "locação não encontrada") {
		return
	}
	s.recordAudit(r, audit.ActionFleetChanged, &rental.VehicleID, nil, map[string]any{"rentalId": rental.ID, "change": "rental extended"})
	writeJSON(w, http.StatusOK, rental)
}

// ---------------------------------------------------------------------------
// Jornada
// ---------------------------------------------------------------------------

// maxJourneyVehicles: acima disso, a jornada pede um grupo ou um veículo.
const maxJourneyVehicles = 150

type journeyRow struct {
	reports.Journey
	VehicleID  uuid.UUID  `json:"vehicleId"`
	DriverID   *uuid.UUID `json:"driverId"`
	DriverName string     `json:"driverName"`
}

// handleReportJourney: as jornadas de cada veículo no período, com o
// motorista de cada uma e o que passou da lei (?tipo=passageiros: o limite
// de direção contínua de 4 h; sem ele, o de cargas, 5h30).
func (s *Server) handleReportJourney(w http.ResponseWriter, r *http.Request) {
	q, ok := s.reportScope(w, r)
	if !ok {
		return
	}
	if len(q.vehicles) > maxJourneyVehicles {
		writeError(w, http.StatusBadRequest, "escolha um grupo ou um veículo: a jornada vai até "+strconv.Itoa(maxJourneyVehicles)+" veículos de cada vez")
		return
	}
	limit := reports.ContinuousCargo
	if r.URL.Query().Get("tipo") == "passageiros" {
		limit = reports.ContinuousPassengers
	}
	from, to := q.from, q.to.AddDate(0, 0, 1)
	var assignments []fleet.Assignment
	if s.Fleet != nil {
		ids := make([]uuid.UUID, len(q.vehicles))
		for i, v := range q.vehicles {
			ids[i] = v.ID
		}
		var err error
		if assignments, err = s.Fleet.Assignments(r.Context(), ids, from.Add(-24*time.Hour), to); err != nil {
			handleStoreError(w, err, "motoristas não encontrados")
			return
		}
	}
	rows := []journeyRow{}
	for _, v := range q.vehicles {
		journeys, err := s.Reports.Journey(r.Context(), v, from, to, limit)
		if err != nil {
			handleStoreError(w, err, "jornada não encontrada")
			return
		}
		for _, j := range journeys {
			row := journeyRow{Journey: j, VehicleID: v.ID}
			for _, a := range assignments {
				if a.VehicleID == v.ID && !a.StartsAt.After(j.Start) && (a.EndsAt == nil || a.EndsAt.After(j.Start)) {
					id := a.DriverID
					row.DriverID, row.DriverName = &id, a.DriverName
				}
			}
			rows = append(rows, row)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"from": day(q.from), "to": day(q.to), "vehicles": q.vehicles, "rows": rows,
		"continuousLimitSeconds": int(limit.Seconds()),
	})
}
