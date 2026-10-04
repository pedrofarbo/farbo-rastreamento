package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/events"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/tracking"
)

// handleVehiclePosition devolve a última posição conhecida do veículo.
// handleVehiclePosition: a posição ao vivo, também para quem acompanha o
// veículo com acesso de terceiro.
func (s *Server) handleVehiclePosition(w http.ResponseWriter, r *http.Request) {
	_, device, _, ok := s.vehicleForViewer(w, r, true)
	if !ok {
		return
	}

	position, err := s.Positions.Latest(r.Context(), device.ID)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			writeJSON(w, http.StatusOK, map[string]any{
				"position": nil,
				"state":    s.States.Get(device.ID),
				"message":  "este rastreador ainda não enviou nenhuma posição",
			})
			return
		}
		handleStoreError(w, err, "posição não encontrada")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"position": position,
		"state":    s.States.Get(device.ID),
	})
}

// handleVehiclePositions devolve o histórico do período (§12).
//
// Nunca devolve tudo: acima do limite o banco amostra uniformemente e a
// resposta diz explicitamente que veio amostrada.
func (s *Server) handleVehiclePositions(w http.ResponseWriter, r *http.Request) {
	vehicle, device, ok := s.vehicleFromURL(w, r, true)
	if !ok {
		return
	}

	to, err := queryTime(r, "to", time.Now().UTC())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	from, err := queryTime(r, "from", to.Add(-24*time.Hour))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	from = s.clampToRetention(r.Context(), vehicle, from)
	if !from.Before(to) {
		writeError(w, http.StatusBadRequest, "o início do período precisa ser anterior ao fim")
		return
	}
	if to.Sub(from) > 366*24*time.Hour {
		writeError(w, http.StatusBadRequest, "período máximo de consulta é de 1 ano")
		return
	}

	limit := queryInt(r, "limit", s.Config.Tracking.MaxHistoryPoints)
	if limit > s.Config.Tracking.MaxHistoryPoints {
		limit = s.Config.Tracking.MaxHistoryPoints
	}

	// Paginação por cursor para exportar o histórico bruto sem amostragem.
	if cursor := queryInt(r, "after", 0); cursor > 0 || queryBool(r, "raw", false) {
		page, err := s.Positions.Page(r.Context(), tracking.PageQuery{
			DeviceID: device.ID, From: from, To: to,
			Limit: limit, AfterID: int64(cursor),
		})
		if err != nil {
			handleStoreError(w, err, "histórico não encontrado")
			return
		}
		writeJSON(w, http.StatusOK, page)
		return
	}

	result, err := s.Positions.History(r.Context(), tracking.HistoryQuery{
		DeviceID:        device.ID,
		From:            from,
		To:              to,
		Limit:           limit,
		OnlyValid:       queryBool(r, "onlyValid", false),
		Simplify:        queryBool(r, "simplify", true),
		ToleranceMeters: s.Config.Tracking.SimplifyToleranceMeters,
	})
	if err != nil {
		handleStoreError(w, err, "histórico não encontrado")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"from":       from,
		"to":         to,
		"positions":  result.Positions,
		"total":      result.Total,
		"returned":   result.Returned,
		"sampled":    result.Sampled,
		"sampleStep": result.SampleStep,
		"simplified": result.Simplified,
	})
}

func (s *Server) handleVehicleEvents(w http.ResponseWriter, r *http.Request) {
	vehicle, device, ok := s.vehicleFromURL(w, r, true)
	if !ok {
		return
	}

	to, err := queryTime(r, "to", time.Now().UTC())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	from, err := queryTime(r, "from", to.Add(-7*24*time.Hour))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	from = s.clampToRetention(r.Context(), vehicle, from)

	var types []string
	if raw := r.URL.Query()["type"]; len(raw) > 0 {
		types = raw
	}

	list, err := s.Events.List(r.Context(), events.Query{
		DeviceID: device.ID, From: from, To: to,
		Types: types, Limit: queryInt(r, "limit", 200),
	})
	if err != nil {
		handleStoreError(w, err, "eventos não encontrados")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleRecentEvents(w http.ResponseWriter, r *http.Request) {
	list, err := s.Events.ListRecent(r.Context(), queryInt(r, "limit", 100))
	if err != nil {
		handleStoreError(w, err, "eventos não encontrados")
		return
	}
	writeJSON(w, http.StatusOK, list)
}
