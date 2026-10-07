package api

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/twofactor"
)

// codeTwoFactorExpired: a verificação acabou; a tela volta para a senha.
const codeTwoFactorExpired = "TWO_FACTOR_EXPIRED"

// loginResult é a sessão aberta, com o aparelho confiável (quando pedido)
// e os códigos de recuperação (na ativação obrigatória).
type loginResult struct {
	*auth.Tokens
	DeviceToken   string   `json:"deviceToken,omitempty"`
	RecoveryCodes []string `json:"recoveryCodes,omitempty"`
}

// twoFactorPending é a resposta do login que ainda espera o segundo fator.
type twoFactorPending struct {
	TwoFactor *twofactor.Challenge `json:"twoFactor"`
}

// writeTwoFactorError traduz as recusas da verificação.
func (s *Server) writeTwoFactorError(w http.ResponseWriter, err error) {
	var e twofactor.Error
	switch {
	case errors.As(err, &e) && e.Expired:
		writeCode(w, http.StatusGone, e.Message, codeTwoFactorExpired)
	case errors.As(err, &e):
		writeError(w, http.StatusBadRequest, e.Message)
	default:
		handleStoreError(w, err, "não encontrado")
	}
}

// openSession abre a sessão depois da senha (ou da biometria), ou responde
// o que falta: o código, ou a ativação (a equipe).
func (s *Server) openSession(w http.ResponseWriter, r *http.Request, user *auth.User, deviceToken string, biometric bool) {
	via := ""
	if s.TwoFactor != nil {
		outcome, err := s.TwoFactor.BeginLogin(r.Context(), user, deviceToken, biometric)
		if err != nil {
			s.writeTwoFactorError(w, err)
			return
		}
		if !outcome.Pass {
			writeJSON(w, http.StatusOK, twoFactorPending{TwoFactor: outcome.Challenge})
			return
		}
		via = outcome.Via
	}
	tokens, err := s.Auth.LoginVerified(r.Context(), user.ID, r.UserAgent())
	if err != nil {
		handleStoreError(w, err, "usuário não encontrado")
		return
	}
	meta := map[string]any{}
	if biometric {
		meta["method"] = "biometric"
	}
	if via == twofactor.MethodTrusted {
		meta["segundoFator"] = "aparelho confiável"
	}
	s.Audit.Record(r.Context(), &audit.Entry{
		UserID: &tokens.User.ID, Action: audit.ActionLogin, Result: "OK", IPAddress: clientIP(r), Metadata: meta,
	})
	writeJSON(w, http.StatusOK, loginResult{Tokens: tokens})
}

// finishTwoFactor abre a sessão de quem passou pelo segundo fator.
func (s *Server) finishTwoFactor(w http.ResponseWriter, r *http.Request, res *twofactor.Result) {
	tokens, err := s.Auth.LoginVerified(r.Context(), res.UserID, r.UserAgent())
	if err != nil {
		if errors.Is(err, auth.ErrInactiveUser) {
			writeCode(w, http.StatusGone, "usuário desativado", codeTwoFactorExpired)
			return
		}
		handleStoreError(w, err, "usuário não encontrado")
		return
	}
	s.Audit.Record(r.Context(), &audit.Entry{
		UserID: &tokens.User.ID, Action: audit.ActionLogin, Result: "OK", IPAddress: clientIP(r),
		Metadata: map[string]any{"segundoFator": res.Used, "aparelhoConfiavel": res.DeviceToken != ""},
	})
	writeJSON(w, http.StatusOK, loginResult{Tokens: tokens, DeviceToken: res.DeviceToken, RecoveryCodes: res.RecoveryCodes})
}

// failedFactor registra o código errado no login.
func (s *Server) failedFactor(r *http.Request, err error) {
	var e twofactor.Error
	if errors.As(err, &e) {
		s.Audit.Record(r.Context(), &audit.Entry{
			Action: audit.ActionTwoFactorFailed, Result: "DENIED", IPAddress: clientIP(r),
			Metadata: map[string]any{"motivo": e.Message},
		})
	}
}

type twoFactorCodeRequest struct {
	Challenge   string `json:"challenge"`
	Code        string `json:"code"`
	TrustDevice bool   `json:"trustDevice"`
}

// handleTwoFactorVerify confere o código do login e abre a sessão.
func (s *Server) handleTwoFactorVerify(w http.ResponseWriter, r *http.Request) {
	var req twoFactorCodeRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	res, err := s.TwoFactor.Verify(r.Context(), req.Challenge, req.Code, req.TrustDevice, r.UserAgent())
	if err != nil {
		s.failedFactor(r, err)
		s.writeTwoFactorError(w, err)
		return
	}
	s.finishTwoFactor(w, r, res)
}

// handleTwoFactorResend manda outro código por e-mail.
func (s *Server) handleTwoFactorResend(w http.ResponseWriter, r *http.Request) {
	var req twoFactorCodeRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	if err := s.TwoFactor.Resend(r.Context(), req.Challenge); err != nil {
		s.writeTwoFactorError(w, err)
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}

// handleTwoFactorSetupEmail: a ativação obrigatória da equipe, passo 1 — o
// código do e-mail; devolve o QR Code do app.
func (s *Server) handleTwoFactorSetupEmail(w http.ResponseWriter, r *http.Request) {
	var req twoFactorCodeRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	enrollment, err := s.TwoFactor.SetupEmail(r.Context(), req.Challenge, req.Code)
	if err != nil {
		s.failedFactor(r, err)
		s.writeTwoFactorError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, enrollment)
}

// handleTwoFactorSetupConfirm: passo 2 — o primeiro código do app; ativa e
// abre a sessão, com os códigos de recuperação.
func (s *Server) handleTwoFactorSetupConfirm(w http.ResponseWriter, r *http.Request) {
	var req twoFactorCodeRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	res, err := s.TwoFactor.SetupConfirm(r.Context(), req.Challenge, req.Code, req.TrustDevice, r.UserAgent())
	if err != nil {
		s.failedFactor(r, err)
		s.writeTwoFactorError(w, err)
		return
	}
	s.recordUserAudit(r, res.UserID, audit.ActionTwoFactorEnabled, map[string]any{"method": twofactor.MethodTOTP, "noLogin": true})
	s.finishTwoFactor(w, r, res)
}

// recordUserAudit registra uma ação sobre a verificação de alguém.
func (s *Server) recordUserAudit(r *http.Request, userID uuid.UUID, action string, meta map[string]any) {
	actor := userID
	if principal, ok := auth.FromContext(r.Context()); ok {
		actor = principal.UserID
	}
	meta["user"] = userID
	s.Audit.Record(r.Context(), &audit.Entry{UserID: &actor, Action: action, Result: "OK", IPAddress: clientIP(r), Metadata: meta})
}

// --- A verificação de quem está conectado (Minha conta → Segurança) -------------

// currentUser é quem está conectado.
func (s *Server) currentUser(w http.ResponseWriter, r *http.Request) (*auth.User, bool) {
	principal, ok := auth.FromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "autenticação obrigatória")
		return nil, false
	}
	user, err := s.Auth.GetUser(r.Context(), principal.UserID)
	if err != nil {
		handleStoreError(w, err, "usuário não encontrado")
		return nil, false
	}
	return user, true
}

func (s *Server) handleTwoFactorStatus(w http.ResponseWriter, r *http.Request) {
	user, ok := s.currentUser(w, r)
	if !ok {
		return
	}
	status, err := s.TwoFactor.Status(r.Context(), user)
	if err != nil {
		handleStoreError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, status)
}

type twoFactorChangeRequest struct {
	Method   string `json:"method"`
	Password string `json:"password"`
	Code     string `json:"code"`
}

// handleTwoFactorStart começa a ativação (ou a troca de método), com a senha.
func (s *Server) handleTwoFactorStart(w http.ResponseWriter, r *http.Request) {
	user, ok := s.currentUser(w, r)
	if !ok {
		return
	}
	var req twoFactorChangeRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	enrollment, err := s.TwoFactor.Start(r.Context(), user, req.Method, req.Password)
	if err != nil {
		s.writeTwoFactorError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, enrollment)
}

// handleTwoFactorConfirm conclui a ativação e entrega os códigos de recuperação.
func (s *Server) handleTwoFactorConfirm(w http.ResponseWriter, r *http.Request) {
	user, ok := s.currentUser(w, r)
	if !ok {
		return
	}
	var req twoFactorChangeRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	codes, method, err := s.TwoFactor.Confirm(r.Context(), user, req.Code)
	if err != nil {
		s.writeTwoFactorError(w, err)
		return
	}
	s.recordUserAudit(r, user.ID, audit.ActionTwoFactorEnabled, map[string]any{"method": method})
	writeJSON(w, http.StatusOK, map[string]any{"recoveryCodes": codes, "method": method})
}

// handleTwoFactorActionCode manda o código por e-mail para confirmar uma
// mudança (de quem usa o e-mail).
func (s *Server) handleTwoFactorActionCode(w http.ResponseWriter, r *http.Request) {
	user, ok := s.currentUser(w, r)
	if !ok {
		return
	}
	if err := s.TwoFactor.SendActionCode(r.Context(), user); err != nil {
		s.writeTwoFactorError(w, err)
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}

// handleTwoFactorDisable desliga a verificação (só o cliente), com a senha e o código.
func (s *Server) handleTwoFactorDisable(w http.ResponseWriter, r *http.Request) {
	user, ok := s.currentUser(w, r)
	if !ok {
		return
	}
	var req twoFactorChangeRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	if err := s.TwoFactor.Disable(r.Context(), user, req.Password, req.Code); err != nil {
		s.writeTwoFactorError(w, err)
		return
	}
	s.recordUserAudit(r, user.ID, audit.ActionTwoFactorDisabled, map[string]any{})
	writeJSON(w, http.StatusNoContent, nil)
}

// handleTwoFactorRecoveryCodes gera códigos de recuperação novos.
func (s *Server) handleTwoFactorRecoveryCodes(w http.ResponseWriter, r *http.Request) {
	user, ok := s.currentUser(w, r)
	if !ok {
		return
	}
	var req twoFactorChangeRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	codes, err := s.TwoFactor.RegenerateCodes(r.Context(), user, req.Password, req.Code)
	if err != nil {
		s.writeTwoFactorError(w, err)
		return
	}
	s.recordUserAudit(r, user.ID, audit.ActionTwoFactorCodes, map[string]any{})
	writeJSON(w, http.StatusOK, map[string]any{"recoveryCodes": codes})
}

// handleTwoFactorRevokeDevice tira a confiança de um aparelho (sem id: de todos).
func (s *Server) handleTwoFactorRevokeDevice(w http.ResponseWriter, r *http.Request) {
	user, ok := s.currentUser(w, r)
	if !ok {
		return
	}
	id := uuid.Nil
	if chi.URLParam(r, "id") != "" {
		parsed, err := urlUUID(r, "id")
		if err != nil {
			writeError(w, http.StatusBadRequest, "id inválido")
			return
		}
		id = parsed
	}
	if err := s.TwoFactor.RevokeDevice(r.Context(), user.ID, id); err != nil {
		handleStoreError(w, err, "aparelho não encontrado")
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}

// handleResetTwoFactor: o admin apaga a verificação de alguém (perdeu o
// celular e os códigos). A equipe ativa de novo no próximo login.
func (s *Server) handleResetTwoFactor(w http.ResponseWriter, r *http.Request) {
	id, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}
	if _, err := s.Auth.GetUser(r.Context(), id); err != nil {
		handleStoreError(w, err, "usuário não encontrado")
		return
	}
	if err := s.TwoFactor.Reset(r.Context(), id); err != nil {
		if errors.Is(err, database.ErrNotFound) {
			writeError(w, http.StatusNotFound, "usuário não encontrado")
			return
		}
		handleStoreError(w, err, "")
		return
	}
	s.recordUserAudit(r, id, audit.ActionTwoFactorReset, map[string]any{})
	writeJSON(w, http.StatusNoContent, nil)
}
