package api

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/stepup"
)

// handleMySessions: os aparelhos com a conta de quem está logado aberta,
// com "este aparelho" marcado.
func (s *Server) handleMySessions(w http.ResponseWriter, r *http.Request) {
	principal, _ := auth.FromContext(r.Context())
	list, err := s.Auth.Sessions(r.Context(), principal.UserID, principal.SessionID)
	if err != nil {
		handleStoreError(w, err, "usuário não encontrado")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": list})
}

// handleSignOutOthers encerra as sessões dos outros aparelhos (na hora) e
// devolve o token de acesso novo deste.
func (s *Server) handleSignOutOthers(w http.ResponseWriter, r *http.Request) {
	principal, _ := auth.FromContext(r.Context())
	tokens, revoked, err := s.Auth.SignOutOthers(r.Context(), principal.UserID, principal.SessionID)
	switch {
	case errors.Is(err, auth.ErrInvalidToken):
		// Token de antes de a sessão ir nele: entrando de novo, já vai.
		writeError(w, http.StatusConflict, "entre de novo neste aparelho e tente outra vez")
		return
	case err != nil:
		handleStoreError(w, err, "usuário não encontrado")
		return
	}
	s.recordAudit(r, audit.ActionSignedOutOthers, nil, nil, map[string]any{"sessions": revoked})
	writeJSON(w, http.StatusOK, map[string]any{"accessToken": tokens.AccessToken, "expiresAt": tokens.ExpiresAt, "signedOut": revoked})
}

// handleCustomerSessions: os aparelhos com a conta do cliente aberta.
func (s *Server) handleCustomerSessions(w http.ResponseWriter, r *http.Request) {
	id, ok := s.customerFromURL(w, r)
	if !ok {
		return
	}
	list, err := s.Auth.Sessions(r.Context(), id, uuid.Nil)
	if err != nil {
		handleStoreError(w, err, "cliente não encontrado")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": list})
}

// handleCustomerSignOut: a central desconecta todas as sessões do cliente.
func (s *Server) handleCustomerSignOut(w http.ResponseWriter, r *http.Request) {
	id, ok := s.customerFromURL(w, r)
	if !ok {
		return
	}
	revoked, err := s.Auth.SignOutAll(r.Context(), id)
	if err != nil {
		handleStoreError(w, err, "cliente não encontrado")
		return
	}
	s.recordAudit(r, audit.ActionCustomerSignedOut, nil, nil, map[string]any{"customerId": id, "sessions": revoked})
	writeJSON(w, http.StatusOK, map[string]any{"signedOut": revoked})
}

// handleSetCustomerPassword: a central define a senha nova do cliente (com a
// confirmação de quem faz). Todas as sessões dele caem e ele recebe o aviso
// de senha alterada por e-mail.
func (s *Server) handleSetCustomerPassword(w http.ResponseWriter, r *http.Request) {
	id, ok := s.customerFromURL(w, r)
	if !ok {
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if !decodeOr400(w, r, &req) {
		return
	}
	if err := auth.ValidatePassword(req.Password); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.requireTeamStepUp(w, r, stepup.PurposeCustomerPassword) {
		return
	}
	_, err := s.Auth.SetPassword(r.Context(), id, req.Password)
	var invalid *auth.PasswordError
	switch {
	case errors.As(err, &invalid):
		writeError(w, http.StatusBadRequest, invalid.Reason)
		return
	case errors.Is(err, auth.ErrInactiveUser), errors.Is(err, database.ErrNotFound):
		writeError(w, http.StatusConflict, "o acesso do cliente está desativado: reative antes de definir a senha")
		return
	case err != nil:
		handleStoreError(w, err, "cliente não encontrado")
		return
	}
	s.recordAudit(r, audit.ActionCustomerPasswordSet, nil, nil, map[string]any{"customerId": id})
	w.WriteHeader(http.StatusNoContent)
}
