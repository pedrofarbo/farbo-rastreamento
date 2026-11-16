package api

import (
	"net/http"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
)

// handleOrgHealth: a saúde dos rastreadores dos associados (os sem sinal, o
// corte de energia, o corte seguido de silêncio).
func (s *Server) handleOrgHealth(w http.ResponseWriter, r *http.Request) {
	orgID, _ := orgOf(r)
	health, err := s.Organizations.TrackerHealth(r.Context(), orgID, brokerOf(r))
	if !writeOrgError(w, r, err, "organização não encontrada") {
		return
	}
	writeJSON(w, http.StatusOK, health)
}

// handleOrgHealthNotify: avisa agora, no app, o associado do veículo com o
// rastreador sem sinal; a resposta diz em quantos aparelhos chegou.
func (s *Server) handleOrgHealthNotify(w http.ResponseWriter, r *http.Request) {
	orgID, _ := orgOf(r)
	vehicleID, err := urlUUID(r, "vehicleId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}
	delivered, err := s.Organizations.NotifyTrackerVehicle(r.Context(), orgID, vehicleID, brokerOf(r), personOf(r))
	if !writeOrgError(w, r, err, "veículo não encontrado") {
		return
	}
	s.recordAudit(r, audit.ActionOrgAnnouncement, &vehicleID, nil, map[string]any{
		"organizationId": orgID, "change": "tracker health notice", "delivered": delivered,
	})
	writeJSON(w, http.StatusOK, map[string]int{"delivered": delivered})
}

type orgHealthSettingsRequest struct {
	AutoNotice bool `json:"autoNotice"`
}

// handleOrgHealthSettings liga ou desliga o aviso sozinho ao associado.
func (s *Server) handleOrgHealthSettings(w http.ResponseWriter, r *http.Request) {
	orgID, _ := orgOf(r)
	var req orgHealthSettingsRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	if err := s.Organizations.SetHealthAutoNotice(r.Context(), orgID, req.AutoNotice); !writeOrgError(w, r, err, "organização não encontrada") {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
