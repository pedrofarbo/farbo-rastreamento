package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/contract"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/payments"
)

// codeContractRequired: o cliente ainda não aceitou o contrato em vigor.
const codeContractRequired = "CONTRACT_REQUIRED"

func writeContractRequired(w http.ResponseWriter) {
	writeCode(w, http.StatusForbidden, "aceite o contrato de prestação de serviços para continuar", codeContractRequired)
}

// requireContract barra o cliente que ainda não aceitou o contrato em vigor
// (a tela mostra o contrato antes de tudo; isto é a garantia no servidor).
// As rotas do próprio contrato passam.
func (s *Server) requireContract(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		customerID, isCustomer := customerOf(r)
		if !isCustomer || s.Contract == nil || strings.HasPrefix(r.URL.Path, "/api/me/contract") {
			next.ServeHTTP(w, r)
			return
		}
		required, err := s.Contract.Required(r.Context(), customerID)
		if err != nil {
			s.Log.Error("falha ao conferir o aceite do contrato", "user", customerID, "err", err)
			writeError(w, http.StatusInternalServerError, "erro interno")
			return
		}
		if required {
			writeContractRequired(w)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// handlePublicContract: o contrato em vigor (a página pública e a tela do aceite).
func (s *Server) handlePublicContract(w http.ResponseWriter, r *http.Request) {
	if s.Contract == nil {
		writeError(w, http.StatusNotFound, "contrato indisponível")
		return
	}
	writeJSON(w, http.StatusOK, s.Contract.Current())
}

// handleMyContract: o contrato, se o cliente precisa aceitar e o aceite dele.
func (s *Server) handleMyContract(w http.ResponseWriter, r *http.Request) {
	if s.Contract == nil {
		writeError(w, http.StatusNotFound, "contrato indisponível")
		return
	}
	customerID, _ := customerOf(r)
	st, err := s.Contract.Status(r.Context(), customerID)
	if err != nil {
		handleStoreError(w, err, "cliente não encontrado")
		return
	}
	writeJSON(w, http.StatusOK, st)
}

type acceptContractRequest struct {
	// Version: a versão que a tela mostrou (se mudou, o aceite é recusado).
	Version string `json:"version"`
	// Document: o CPF (ou CNPJ), obrigatório para as notas fiscais.
	Document string `json:"document"`
}

// handleAcceptContract registra o aceite eletrônico (com o IP e o navegador)
// e grava o CPF no cadastro.
func (s *Server) handleAcceptContract(w http.ResponseWriter, r *http.Request) {
	if s.Contract == nil {
		writeError(w, http.StatusNotFound, "contrato indisponível")
		return
	}
	var req acceptContractRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	customerID, _ := customerOf(r)
	a, err := s.Contract.Accept(r.Context(), customerID, req.Version, req.Document, clientIP(r), r.UserAgent())
	var rule contract.RuleError
	if errors.As(err, &rule) {
		writeError(w, http.StatusBadRequest, rule.Message)
		return
	}
	if err != nil {
		handleStoreError(w, err, "cliente não encontrado")
		return
	}
	s.recordBillingAudit(r, audit.ActionContractAccepted, map[string]any{
		"customerId": customerID, "version": a.Version, "sha256": a.SHA256,
	})
	st, err := s.Contract.Status(r.Context(), customerID)
	if err != nil {
		handleStoreError(w, err, "cliente não encontrado")
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// taxIDOf valida o CPF/CNPJ do cadastro feito pela central: vazio passa (o
// cliente informa no primeiro acesso, junto com o aceite do contrato); se
// vier, precisa ser válido e é guardado só com os números.
func taxIDOf(w http.ResponseWriter, doc string) (string, bool) {
	doc = strings.TrimSpace(doc)
	if doc == "" {
		return "", true
	}
	if !payments.ValidTaxID(doc) {
		writeError(w, http.StatusBadRequest, "CPF (ou CNPJ) inválido: confira os números")
		return "", false
	}
	return payments.Digits(doc), true
}
