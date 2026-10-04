package api

import (
	"net/http"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/analytics"
)

// handlePublicAnalytics recebe uma visita ou um evento da landing (sem login).
// Sem cookies e sem dados pessoais: ver o pacote analytics.
func (s *Server) handlePublicAnalytics(w http.ResponseWriter, r *http.Request) {
	var hit analytics.Hit
	if err := decodeJSON(w, r, &hit); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	if err := s.Analytics.Record(r.Context(), hit, clientIP(r), r.UserAgent(), r.Host); err != nil {
		s.Log.Warn("visita da landing não gravada", "err", err)
	}
	// A landing não espera resposta (sendBeacon): sempre 204.
	writeJSON(w, http.StatusNoContent, nil)
}

// handleLandingAnalytics: o painel das visitas dos últimos ?days= dias.
func (s *Server) handleLandingAnalytics(w http.ResponseWriter, r *http.Request) {
	summary, err := s.Analytics.Summary(r.Context(), queryInt(r, "days", 30))
	if err != nil {
		handleStoreError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, summary)
}
