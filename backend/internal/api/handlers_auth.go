package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
)

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"time":   time.Now().UTC(),
	})
}

// handleReady só responde 200 quando as dependências realmente atendem.
func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	if err := s.DB.Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status":   "degraded",
			"database": err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":            "ready",
		"database":          "ok",
		"trackerSessions":   s.Conns.Count(),
		"websocketClients":  s.Hub.Count(),
		"registeredDrivers": len(s.Registry.All()),
	})
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}

	tokens, err := s.Auth.Login(r.Context(), req.Email, req.Password, r.UserAgent())
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) || errors.Is(err, auth.ErrInactiveUser) {
			s.Audit.Record(r.Context(), &audit.Entry{
				Action: audit.ActionLoginFailed, Result: "DENIED", IPAddress: clientIP(r),
				Metadata: map[string]any{"email": req.Email},
			})
			// Mensagem única para não revelar se o e-mail existe.
			writeError(w, http.StatusUnauthorized, "e-mail ou senha inválidos")
			return
		}
		handleStoreError(w, err, "usuário não encontrado")
		return
	}

	s.Audit.Record(r.Context(), &audit.Entry{
		UserID: &tokens.User.ID, Action: audit.ActionLogin,
		Result: "OK", IPAddress: clientIP(r),
	})
	writeJSON(w, http.StatusOK, tokens)
}

type refreshRequest struct {
	RefreshToken string `json:"refreshToken"`
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}

	tokens, err := s.Auth.Refresh(r.Context(), req.RefreshToken, r.UserAgent())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "sessão expirada; faça login novamente")
		return
	}
	writeJSON(w, http.StatusOK, tokens)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	if err := s.Auth.Logout(r.Context(), req.RefreshToken); err != nil {
		handleStoreError(w, err, "token não encontrado")
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}

type forgotPasswordRequest struct {
	Email string `json:"email"`
}

// forgotPasswordMessage é a resposta para qualquer e-mail, com ou sem conta:
// quem pergunta não descobre quais endereços estão cadastrados.
const forgotPasswordMessage = "Se houver uma conta com esse e-mail, enviaremos um link para redefinir a senha."

func (s *Server) handleForgotPassword(w http.ResponseWriter, r *http.Request) {
	var req forgotPasswordRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	email := strings.TrimSpace(req.Email)
	if !strings.Contains(email, "@") || len(email) > 254 {
		writeError(w, http.StatusBadRequest, "informe um e-mail válido")
		return
	}

	user, outcome, err := s.Auth.RequestPasswordReset(r.Context(), email)
	if err != nil {
		s.Log.Error("falha ao processar pedido de redefinição de senha", "err", err)
		writeError(w, http.StatusInternalServerError,
			"não foi possível processar o pedido agora; tente novamente em instantes")
		return
	}

	entry := &audit.Entry{
		Action: audit.ActionPasswordResetRequested, Result: string(outcome), IPAddress: clientIP(r),
	}
	if user != nil {
		entry.UserID = &user.ID
	} else {
		entry.Metadata = map[string]any{"email": email}
	}
	s.Audit.Record(r.Context(), entry)

	writeJSON(w, http.StatusAccepted, map[string]string{"message": forgotPasswordMessage})
}

type resetPasswordRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

// invalidResetLinkMessage acompanha o 410: o link não serve mais, e a tela
// oferece pedir outro. Senha fraca volta como 400, para a tela distinguir.
const invalidResetLinkMessage = "este link de redefinição é inválido ou expirou; peça um novo"

func (s *Server) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	var req resetPasswordRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}

	user, err := s.Auth.ResetPassword(r.Context(), req.Token, req.Password)
	if err != nil {
		var weak *auth.PasswordError
		switch {
		case errors.As(err, &weak):
			writeError(w, http.StatusBadRequest, weak.Reason)
		case errors.Is(err, auth.ErrInvalidResetToken):
			s.Audit.Record(r.Context(), &audit.Entry{
				Action: audit.ActionPasswordResetFailed, Result: "INVALID_TOKEN", IPAddress: clientIP(r),
			})
			writeError(w, http.StatusGone, invalidResetLinkMessage)
		default:
			s.Log.Error("falha ao redefinir senha", "err", err)
			writeError(w, http.StatusInternalServerError, "erro interno")
		}
		return
	}

	s.Audit.Record(r.Context(), &audit.Entry{
		UserID: &user.ID, Action: audit.ActionPasswordReset, Result: "OK", IPAddress: clientIP(r),
	})
	writeJSON(w, http.StatusNoContent, nil)
}

type resetTokenRequest struct {
	Token string `json:"token"`
}

// handleCheckResetToken deixa a tela avisar que o link expirou antes de a
// pessoa digitar a senha nova. Não consome o token.
func (s *Server) handleCheckResetToken(w http.ResponseWriter, r *http.Request) {
	var req resetTokenRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}

	if err := s.Auth.CheckResetToken(r.Context(), req.Token); err != nil {
		if errors.Is(err, auth.ErrInvalidResetToken) {
			writeError(w, http.StatusGone, invalidResetLinkMessage)
			return
		}
		s.Log.Error("falha ao validar link de redefinição", "err", err)
		writeError(w, http.StatusInternalServerError, "erro interno")
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "autenticação obrigatória")
		return
	}
	user, err := s.Auth.GetUser(r.Context(), principal.UserID)
	if err != nil {
		handleStoreError(w, err, "usuário não encontrado")
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func (s *Server) handleListProtocols(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.Registry.Descriptors())
}
