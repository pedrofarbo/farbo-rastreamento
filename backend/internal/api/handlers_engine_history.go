package api

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/commands"
)

// engineHistoryItem é um bloqueio ou desbloqueio no rastreio do app.
type engineHistoryItem struct {
	ID             uuid.UUID  `json:"id"`
	Command        string     `json:"command"`
	Status         string     `json:"status"`
	RequestedAt    time.Time  `json:"requestedAt"`
	AcknowledgedAt *time.Time `json:"acknowledgedAt"`
	Error          string     `json:"error"`
	// Who é quem pediu, como quem vê a lista entende: "Você", o nome de
	// alguém da conta ou de quem tem acesso compartilhado, "Central Farbo"
	// (para a equipe, o nome) ou "Sistema". WhoKind: you, account, shared,
	// central ou system.
	Who     string `json:"who"`
	WhoKind string `json:"whoKind"`
	// UserAgent: de qual aparelho saiu (o da equipe da central não vai para o
	// cliente); ConfirmedWith: biometric ou password.
	UserAgent     string `json:"userAgent"`
	ConfirmedWith string `json:"confirmedWith"`
}

// handleEngineHistory: o rastreio de bloqueio e desbloqueio do veículo
// (o dono, a equipe da conta e a central).
func (s *Server) handleEngineHistory(w http.ResponseWriter, r *http.Request) {
	vehicle, device, ok := s.vehicleFromURL(w, r, true)
	if !ok {
		return
	}
	list, err := s.Commands.EngineHistory(r.Context(), device, queryInt(r, "limit", 30))
	if err != nil {
		handleStoreError(w, err, "comandos não encontrados")
		return
	}
	principal, _ := auth.FromContext(r.Context())
	staff := principal != nil && !principal.IsCustomer()
	out := make([]engineHistoryItem, 0, len(list))
	for _, e := range list {
		item := engineHistoryItem{
			ID: e.ID, Command: e.Command.Command, Status: e.Status, RequestedAt: e.RequestedAt,
			AcknowledgedAt: e.AcknowledgedAt, Error: e.Error, UserAgent: e.UserAgent, ConfirmedWith: e.ConfirmedWith,
		}
		item.Who, item.WhoKind = whoRequested(e, principal, vehicle.OwnerID)
		if item.WhoKind == "central" && !staff {
			item.Who, item.UserAgent = "Central Farbo", ""
		}
		out = append(out, item)
	}
	writeJSON(w, http.StatusOK, out)
}

// whoRequested diz quem pediu o comando, do ponto de vista de quem vê.
func whoRequested(e *commands.EngineEntry, viewer *auth.Principal, owner *uuid.UUID) (string, string) {
	switch {
	case e.RequestedBy == nil:
		return "Sistema", "system"
	case e.RequesterRole != auth.RoleCustomer:
		return e.RequesterName, "central"
	case viewer != nil && *e.RequestedBy == viewer.UserID:
		return "Você", "you"
	case owner != nil && e.RequesterAccount != nil && *e.RequesterAccount == *owner:
		return e.RequesterName, "account"
	default:
		return e.RequesterName, "shared"
	}
}
