package api

import (
	"net/http"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/infra"
)

// handleInfraStatus: a máquina, o processo, o banco, o Redis, os backups e as
// conexões abertas agora.
func (s *Server) handleInfraStatus(w http.ResponseWriter, r *http.Request) {
	status := s.Infra.Status(r.Context())
	if s.Conns != nil {
		status.Live.TrackerConnections = s.Conns.Count()
	}
	if s.Hub != nil {
		status.Live.RealtimeClients = s.Hub.Count()
	}
	writeJSON(w, http.StatusOK, status)
}

// handleInfraLogs: os avisos e erros do servidor (?hours=&level=&q=&limit=).
func (s *Server) handleInfraLogs(w http.ResponseWriter, r *http.Request) {
	page, err := s.Infra.Logs(r.Context(), infra.LogQuery{
		Hours:  queryInt(r, "hours", 24),
		Level:  r.URL.Query().Get("level"),
		Search: r.URL.Query().Get("q"),
		Limit:  queryInt(r, "limit", 100),
	})
	if err != nil {
		handleStoreError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, page)
}
