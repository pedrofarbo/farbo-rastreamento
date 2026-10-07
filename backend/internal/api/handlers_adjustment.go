package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/adjustment"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
)

// handlePriceAdjustments mostra o reajuste anual: o próximo e os anteriores,
// com as mensalidades reajustadas.
func (s *Server) handlePriceAdjustments(w http.ResponseWriter, r *http.Request) {
	out, err := s.Adjustment.Overview(r.Context())
	if err != nil {
		handleStoreError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// handleCancelPriceAdjustment: o admin desiste do reajuste do ano (antes de
// as faturas com o preço novo começarem a sair); os clientes são avisados.
func (s *Server) handleCancelPriceAdjustment(w http.ResponseWriter, r *http.Request) {
	year, err := strconv.Atoi(chi.URLParam(r, "year"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "ano inválido")
		return
	}
	if err := s.Adjustment.Cancel(r.Context(), year, actorOf(r)); err != nil {
		var e adjustment.Error
		if errors.As(err, &e) {
			writeError(w, http.StatusConflict, e.Message)
			return
		}
		handleStoreError(w, err, "reajuste não encontrado")
		return
	}
	s.recordAudit(r, audit.ActionPriceAdjustmentCanceled, nil, nil, map[string]any{"year": year})
	writeJSON(w, http.StatusNoContent, nil)
}
