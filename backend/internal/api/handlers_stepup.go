package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/stepup"
)

// Confirmação extra de ações sensíveis (o cliente desligar o motor): a
// biometria do aparelho (WebAuthn) ou a senha. Ver o pacote stepup.

// stepUpHeader leva o comprovante da confirmação até a ação.
const stepUpHeader = "X-Step-Up-Token"

// Códigos de erro que o app usa para decidir o próximo passo. Senha errada é
// 403, nunca 401: o 401 faria o app achar que a sessão acabou.
const (
	codeStepUpRequired  = "STEP_UP_REQUIRED"
	codeWrongPassword   = "WRONG_PASSWORD"
	codeBiometricFailed = "BIOMETRIC_FAILED"
	codeNoBiometric     = "NO_BIOMETRIC"
)

func writeCode(w http.ResponseWriter, status int, message, code string) {
	writeJSON(w, status, map[string]string{"error": message, "code": code})
}

// writeStepUpError traduz os erros do stepup em resposta.
func (s *Server) writeStepUpError(w http.ResponseWriter, r *http.Request, err error, purpose string) {
	reason := err.Error()
	switch {
	case errors.Is(err, stepup.ErrWrongPassword):
		writeCode(w, http.StatusForbidden, "senha incorreta", codeWrongPassword)
	case errors.Is(err, stepup.ErrTooManyAttempts):
		writeError(w, http.StatusTooManyRequests, err.Error())
	case errors.Is(err, stepup.ErrUnknownCredential):
		writeCode(w, http.StatusForbidden, err.Error(), codeNoBiometric)
	case errors.Is(err, stepup.ErrVerification):
		writeCode(w, http.StatusForbidden, "não foi possível confirmar com a biometria", codeBiometricFailed)
	case errors.Is(err, stepup.ErrUnknownPurpose):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, stepup.ErrTooManyCredentials), errors.Is(err, stepup.ErrAlreadyRegistered):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, auth.ErrInactiveUser):
		writeError(w, http.StatusForbidden, err.Error())
	default:
		reason = "erro interno"
		s.Log.Error("falha na confirmação extra", "err", err)
		writeError(w, http.StatusInternalServerError, "erro interno")
	}
	s.recordAudit(r, audit.ActionStepUpFailed, nil, nil, map[string]any{"purpose": purpose, "reason": reason})
}

func (s *Server) principal(r *http.Request) (auth.Principal, bool) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || principal == nil {
		return auth.Principal{}, false
	}
	return *principal, true
}

func (s *Server) handleListBiometrics(w http.ResponseWriter, r *http.Request) {
	p, _ := s.principal(r)
	creds, err := s.StepUp.Credentials(r.Context(), p.UserID)
	if err != nil {
		handleStoreError(w, err, "usuário não encontrado")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rpId": s.StepUp.RPID(), "credentials": creds})
}

// handleBiometricOptions começa o cadastro da biometria (pede a senha).
func (s *Server) handleBiometricOptions(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	p, _ := s.principal(r)
	user, err := s.Auth.GetUser(r.Context(), p.UserID)
	if err != nil {
		handleStoreError(w, err, "usuário não encontrado")
		return
	}
	options, err := s.StepUp.RegistrationOptions(r.Context(), user.ID, user.Email, user.Name, req.Password)
	if err != nil {
		s.writeStepUpError(w, r, err, "register")
		return
	}
	writeJSON(w, http.StatusOK, options)
}

func (s *Server) handleRegisterBiometric(w http.ResponseWriter, r *http.Request) {
	var req stepup.RegistrationResponse
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	p, _ := s.principal(r)
	cred, err := s.StepUp.Register(r.Context(), p.UserID, req)
	if err != nil {
		s.writeStepUpError(w, r, err, "register")
		return
	}
	s.recordAudit(r, audit.ActionBiometricAdded, nil, nil, map[string]any{"credential": cred.ID, "name": cred.Name})
	writeJSON(w, http.StatusCreated, cred)
}

func (s *Server) handleDeleteBiometric(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}
	p, _ := s.principal(r)
	if err := s.StepUp.DeleteCredential(r.Context(), p.UserID, id); err != nil {
		if errors.Is(err, database.ErrNotFound) {
			writeError(w, http.StatusNotFound, "biometria não encontrada")
			return
		}
		handleStoreError(w, err, "biometria não encontrada")
		return
	}
	s.recordAudit(r, audit.ActionBiometricRemoved, nil, nil, map[string]any{"credential": id})
	writeJSON(w, http.StatusNoContent, nil)
}

// handleStepUpOptions começa a confirmação de uma ação com a biometria.
func (s *Server) handleStepUpOptions(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Purpose string `json:"purpose"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	p, _ := s.principal(r)
	options, err := s.StepUp.AssertionOptions(r.Context(), p.UserID, req.Purpose)
	if err != nil {
		if errors.Is(err, stepup.ErrUnknownCredential) {
			writeCode(w, http.StatusNotFound, err.Error(), codeNoBiometric)
			return
		}
		s.writeStepUpError(w, r, err, req.Purpose)
		return
	}
	writeJSON(w, http.StatusOK, options)
}

func (s *Server) handleStepUpBiometric(w http.ResponseWriter, r *http.Request) {
	var req struct {
		stepup.AssertionResponse
		Purpose string `json:"purpose"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	p, _ := s.principal(r)
	grant, err := s.StepUp.VerifyAssertion(r.Context(), p.UserID, req.Purpose, req.AssertionResponse)
	if err != nil {
		s.writeStepUpError(w, r, err, req.Purpose)
		return
	}
	s.recordAudit(r, audit.ActionStepUp, nil, nil, map[string]any{"purpose": req.Purpose, "method": grant.Method})
	writeJSON(w, http.StatusOK, grant)
}

func (s *Server) handleStepUpPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Purpose  string `json:"purpose"`
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	p, _ := s.principal(r)
	grant, err := s.StepUp.VerifyPassword(r.Context(), p.UserID, req.Purpose, req.Password)
	if err != nil {
		s.writeStepUpError(w, r, err, req.Purpose)
		return
	}
	s.recordAudit(r, audit.ActionStepUp, nil, nil, map[string]any{"purpose": req.Purpose, "method": grant.Method})
	writeJSON(w, http.StatusOK, grant)
}

// --- Entrar com a biometria (app do cliente) ------------------------------------

// handleBiometricLoginOptions começa o login com a biometria deste aparelho.
// O app manda o id da credencial que guardou ao ativar o Face ID.
func (s *Server) handleBiometricLoginOptions(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CredentialID string `json:"credentialId"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	options, err := s.StepUp.LoginOptions(r.Context(), req.CredentialID)
	if err != nil {
		if errors.Is(err, stepup.ErrUnknownCredential) || errors.Is(err, stepup.ErrVerification) {
			// Removida (em Conta, ou de outro aparelho): o app volta para a senha.
			writeCode(w, http.StatusNotFound, "biometria não cadastrada", codeNoBiometric)
			return
		}
		s.Log.Error("falha ao começar o login com biometria", "err", err)
		writeError(w, http.StatusInternalServerError, "erro interno")
		return
	}
	writeJSON(w, http.StatusOK, options)
}

// handleBiometricLogin confere a biometria e abre a sessão, igual ao login
// com senha.
func (s *Server) handleBiometricLogin(w http.ResponseWriter, r *http.Request) {
	var req stepup.AssertionResponse
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	userID, err := s.StepUp.VerifyLogin(r.Context(), req)
	if err == nil {
		var tokens *auth.Tokens
		tokens, err = s.Auth.LoginVerified(r.Context(), userID, r.UserAgent())
		if err == nil {
			s.Audit.Record(r.Context(), &audit.Entry{
				UserID: &tokens.User.ID, Action: audit.ActionLogin, Result: "OK", IPAddress: clientIP(r),
				Metadata: map[string]any{"method": stepup.MethodBiometric},
			})
			writeJSON(w, http.StatusOK, tokens)
			return
		}
	}
	if !errors.Is(err, stepup.ErrVerification) && !errors.Is(err, stepup.ErrUnknownCredential) &&
		!errors.Is(err, auth.ErrInactiveUser) {
		s.Log.Error("falha no login com biometria", "err", err)
		writeError(w, http.StatusInternalServerError, "erro interno")
		return
	}
	s.Audit.Record(r.Context(), &audit.Entry{
		Action: audit.ActionLoginFailed, Result: "DENIED", IPAddress: clientIP(r),
		Metadata: map[string]any{"method": stepup.MethodBiometric, "reason": err.Error()},
	})
	writeCode(w, http.StatusUnauthorized, "não foi possível entrar com a biometria", codeBiometricFailed)
}

// requireStepUp exige do cliente o comprovante da confirmação (biometria ou
// senha) para a ação. A equipe da central segue como antes. Devolve false se
// já respondeu.
func (s *Server) requireStepUp(w http.ResponseWriter, r *http.Request, purpose string) bool {
	customerID, isCustomer := customerOf(r)
	if !isCustomer || s.StepUp == nil {
		return true
	}
	method, err := s.StepUp.Consume(r.Context(), customerID, purpose, r.Header.Get(stepUpHeader))
	if err != nil {
		if !errors.Is(err, stepup.ErrNoGrant) {
			s.Log.Error("falha ao conferir a confirmação extra", "err", err)
			writeError(w, http.StatusInternalServerError, "erro interno")
			return false
		}
		writeCode(w, http.StatusForbidden, "confirme com a biometria ou a senha para continuar", codeStepUpRequired)
		return false
	}
	s.Log.Info("ação confirmada", "purpose", purpose, "method", method)
	return true
}
