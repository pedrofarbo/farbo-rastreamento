package api

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/accounts"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/stepup"
)

// A conta empresa: o titular dá às pessoas da equipe o acesso à conta. O
// que é da conta (veículos, cercas, faturas) vem de customerOf — a conta do
// titular —, e o que é da pessoa (alertas, notificações, biometria, os
// veículos que outros compartilharam com ela) vem de personOf.

// codeAccountRole acompanha o 403 de quem é da equipe e não tem o papel
// para a ação (a tela explica em vez de mostrar um erro genérico).
const codeAccountRole = "ACCOUNT_ROLE"

// personOf é quem está logado, como pessoa (não a conta que enxerga).
func personOf(r *http.Request) uuid.UUID {
	if principal, ok := auth.FromContext(r.Context()); ok {
		return principal.UserID
	}
	return uuid.Nil
}

// requireAccountOwner deixa passar só o titular da conta: a equipe, a
// conta (cadastro, contrato, pedidos, equipe) e os compartilhamentos.
func (s *Server) requireAccountOwner(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if principal, ok := auth.FromContext(r.Context()); ok && principal.IsCustomer() && !principal.IsAccountOwner() {
			writeCode(w, http.StatusForbidden, "só o titular da conta pode fazer isso", codeAccountRole)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireAccountOperator barra o Visualizador da equipe de um cliente:
// comandos, cercas, edição dos veículos e faturas são do titular e do
// Gestor. A equipe da central segue as regras dela.
func (s *Server) requireAccountOperator(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if principal, ok := auth.FromContext(r.Context()); ok && principal.IsCustomer() && !principal.CanOperateAccount() {
			writeCode(w, http.StatusForbidden, "seu acesso a esta conta é só para acompanhar", codeAccountRole)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeTeamError(w http.ResponseWriter, err error, notFound string) bool {
	var input *accounts.InputError
	switch {
	case err == nil:
		return true
	case errors.As(err, &input):
		writeError(w, http.StatusBadRequest, input.Message)
	case errors.Is(err, database.ErrNotFound):
		writeError(w, http.StatusNotFound, notFound)
	default:
		handleStoreError(w, err, notFound)
	}
	return false
}

// handleMyTeam: a equipe da conta (só o titular).
func (s *Server) handleMyTeam(w http.ResponseWriter, r *http.Request) {
	accountID, _ := customerOf(r)
	list, err := s.Accounts.List(r.Context(), accountID)
	if !writeTeamError(w, err, "equipe não encontrada") {
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// handleAddTeamMember põe alguém na equipe. Pede a confirmação extra, como
// compartilhar um veículo: quem pega o celular do titular por um instante
// não dá a si mesmo o acesso à frota.
func (s *Server) handleAddTeamMember(w http.ResponseWriter, r *http.Request) {
	var in accounts.Input
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	if !s.requireStepUp(w, r, stepup.PurposeTeamMember) {
		return
	}
	accountID, _ := customerOf(r)
	member, created, err := s.Accounts.Add(r.Context(), accountID, in)
	if !writeTeamError(w, err, "conta não encontrada") {
		return
	}
	s.recordBillingAudit(r, audit.ActionAccountMemberAdded, map[string]any{
		"customerId": accountID, "memberId": member.UserID, "role": member.Role, "invited": created,
	})
	writeJSON(w, http.StatusCreated, member)
}

type teamRoleRequest struct {
	Role string `json:"role"`
}

// handleUpdateTeamMember troca o papel de alguém da equipe.
func (s *Server) handleUpdateTeamMember(w http.ResponseWriter, r *http.Request) {
	userID, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}
	var req teamRoleRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	accountID, _ := customerOf(r)
	member, err := s.Accounts.SetRole(r.Context(), accountID, userID, req.Role)
	if !writeTeamError(w, err, "pessoa não encontrada na equipe") {
		return
	}
	s.recordBillingAudit(r, audit.ActionAccountMemberUpdated, map[string]any{
		"customerId": accountID, "memberId": member.UserID, "role": member.Role,
	})
	writeJSON(w, http.StatusOK, member)
}

// handleRemoveTeamMember tira alguém da equipe: o acesso acaba na hora.
func (s *Server) handleRemoveTeamMember(w http.ResponseWriter, r *http.Request) {
	userID, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}
	accountID, _ := customerOf(r)
	member, err := s.Accounts.Remove(r.Context(), accountID, userID)
	if !writeTeamError(w, err, "pessoa não encontrada na equipe") {
		return
	}
	s.recordBillingAudit(r, audit.ActionAccountMemberRemoved, map[string]any{
		"customerId": accountID, "memberId": member.UserID, "email": member.Email,
	})
	w.WriteHeader(http.StatusNoContent)
}

// handleCustomerTeam: a central vê a equipe da conta de um cliente.
func (s *Server) handleCustomerTeam(w http.ResponseWriter, r *http.Request) {
	customerID, ok := s.customerFromURL(w, r)
	if !ok {
		return
	}
	list, err := s.Accounts.List(r.Context(), customerID)
	if !writeTeamError(w, err, "cliente não encontrado") {
		return
	}
	writeJSON(w, http.StatusOK, list)
}
