package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/twofactor"
)

// Equipe da central: administradores, operadores e quem só visualiza. Os
// clientes têm cadastro próprio (Clientes) e não passam por aqui.

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.Auth.ListTeam(r.Context())
	if err != nil {
		handleStoreError(w, err, "usuários não encontrados")
		return
	}
	// Com a verificação em duas etapas de cada um.
	type member struct {
		*auth.User
		TwoFactor twofactor.Brief `json:"twoFactor"`
	}
	briefs := map[uuid.UUID]twofactor.Brief{}
	if s.TwoFactor != nil {
		ids := make([]uuid.UUID, 0, len(users))
		for _, u := range users {
			ids = append(ids, u.ID)
		}
		if briefs, err = s.TwoFactor.Briefs(r.Context(), ids); err != nil {
			handleStoreError(w, err, "")
			return
		}
	}
	out := make([]member, 0, len(users))
	for _, u := range users {
		out = append(out, member{User: u, TwoFactor: briefs[u.ID]})
	}
	writeJSON(w, http.StatusOK, out)
}

type createUserRequest struct {
	Email string `json:"email"`
	Name  string `json:"name"`
	Role  string `json:"role"`
	// Password em branco: o usuário recebe o convite por e-mail e cria a
	// própria senha.
	Password string `json:"password"`
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	if !auth.TeamRole(req.Role) {
		writeError(w, http.StatusBadRequest,
			"escolha o perfil: administrador, operador ou visualização (clientes são cadastrados em Clientes)")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "o nome é obrigatório")
		return
	}
	password := req.Password
	invite := strings.TrimSpace(password) == ""
	if invite {
		random, err := auth.RandomPassword()
		if err != nil {
			handleStoreError(w, err, "")
			return
		}
		password = random
	}

	user, err := s.Auth.CreateUser(r.Context(), req.Email, req.Name, req.Role, password)
	if err != nil {
		if errors.Is(err, database.ErrConflict) {
			writeError(w, http.StatusConflict, "já existe um usuário com esse e-mail")
			return
		}
		// O que sobra são erros de validação, que o cliente consegue corrigir.
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if invite {
		if err := s.Auth.InviteUser(r.Context(), user); err != nil {
			s.Log.Error("falha ao gerar o convite do usuário", "user", user.ID, "err", err)
		}
	}

	s.recordAudit(r, audit.ActionUserCreated, nil, nil,
		map[string]any{"userId": user.ID, "email": user.Email, "role": user.Role, "invite": invite})
	writeJSON(w, http.StatusCreated, user)
}

type updateUserRequest struct {
	Name   string `json:"name"`
	Role   string `json:"role"`
	Active bool   `json:"active"`
}

// handleUpdateUser muda nome, perfil e situação de alguém da equipe.
// Ninguém muda o próprio perfil nem se desativa, e sempre sobra um
// administrador ativo.
func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	id, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}
	principal, ok := auth.FromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "autenticação obrigatória")
		return
	}
	var req updateUserRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}

	before, after, err := s.Auth.UpdateMember(r.Context(), principal.UserID, id,
		auth.Member{Name: req.Name, Role: req.Role, Active: req.Active})
	var input *auth.InputError
	switch {
	case err == nil:
	case errors.As(err, &input):
		writeError(w, http.StatusBadRequest, input.Reason)
		return
	case errors.Is(err, auth.ErrNotTeam), errors.Is(err, database.ErrNotFound):
		writeError(w, http.StatusNotFound, "usuário não encontrado")
		return
	case errors.Is(err, auth.ErrSelfChange):
		writeError(w, http.StatusConflict, auth.ErrSelfChange.Error()+"; peça a outro administrador")
		return
	case errors.Is(err, auth.ErrLastAdmin):
		writeError(w, http.StatusConflict, auth.ErrLastAdmin.Error())
		return
	default:
		handleStoreError(w, err, "usuário não encontrado")
		return
	}

	meta := map[string]any{"userId": id, "email": after.Email, "role": after.Role, "active": after.Active}
	if before.Role != after.Role {
		meta["previousRole"] = before.Role
	}
	s.recordAudit(r, audit.ActionUserUpdated, nil, nil, meta)
	writeJSON(w, http.StatusOK, after)
}

// handleInviteUser reenvia o convite (link para criar a senha) a alguém da
// equipe. O link anterior deixa de valer.
func (s *Server) handleInviteUser(w http.ResponseWriter, r *http.Request) {
	id, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}
	user, err := s.Auth.GetUser(r.Context(), id)
	if err != nil {
		handleStoreError(w, err, "usuário não encontrado")
		return
	}
	if !auth.TeamRole(user.Role) {
		writeError(w, http.StatusNotFound, "usuário não encontrado")
		return
	}
	if err := s.Auth.InviteUser(r.Context(), user); err != nil {
		if errors.Is(err, auth.ErrInactiveUser) {
			writeError(w, http.StatusConflict, "o usuário está desativado; reative antes de convidar")
			return
		}
		handleStoreError(w, err, "usuário não encontrado")
		return
	}
	s.recordAudit(r, audit.ActionUserInvited, nil, nil, map[string]any{"userId": id, "email": user.Email})
	writeJSON(w, http.StatusAccepted, map[string]string{"message": "convite enviado para " + user.Email})
}
