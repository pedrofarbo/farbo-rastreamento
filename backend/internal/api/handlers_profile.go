package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/payments"
)

type myProfileRequest struct {
	Name  string `json:"name"`
	Phone string `json:"phone"`
	// Document: o CPF (ou CNPJ), obrigatório para as notas fiscais.
	Document string `json:"document"`
}

// phoneOf confere o telefone (opcional): com o DDD, 10 ou 11 dígitos (o 55
// do país, se vier, sai). Devolve no formato (11) 91234-5678.
func phoneOf(raw string) (string, bool) {
	d := payments.Digits(raw)
	if d == "" {
		return "", true
	}
	if (len(d) == 12 || len(d) == 13) && strings.HasPrefix(d, "55") {
		d = d[2:]
	}
	switch len(d) {
	case 11:
		return "(" + d[:2] + ") " + d[2:7] + "-" + d[7:], true
	case 10:
		return "(" + d[:2] + ") " + d[2:6] + "-" + d[6:], true
	}
	return "", false
}

// handleUpdateMyProfile: o cliente atualiza o próprio cadastro (Meus dados,
// no app): nome, telefone e CPF/CNPJ. O e-mail é o login e não muda aqui.
func (s *Server) handleUpdateMyProfile(w http.ResponseWriter, r *http.Request) {
	customerID, _ := customerOf(r)
	var req myProfileRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	name := strings.Join(strings.Fields(req.Name), " ")
	if name == "" || len([]rune(name)) > 120 {
		writeError(w, http.StatusBadRequest, "informe o nome (até 120 caracteres)")
		return
	}
	phone, ok := phoneOf(req.Phone)
	if !ok {
		writeError(w, http.StatusBadRequest, "telefone inválido: informe o DDD e o número")
		return
	}
	if strings.TrimSpace(req.Document) == "" {
		writeError(w, http.StatusBadRequest, "o CPF é obrigatório: é com ele que emitimos as notas fiscais")
		return
	}
	document, ok := taxIDOf(w, req.Document)
	if !ok {
		return
	}
	before, err := s.Auth.GetUser(r.Context(), customerID)
	if err != nil {
		handleStoreError(w, err, "cliente não encontrado")
		return
	}
	user, err := s.Auth.UpdateContact(r.Context(), customerID, name, phone, document)
	var input *auth.InputError
	if errors.As(err, &input) {
		writeError(w, http.StatusBadRequest, input.Reason)
		return
	}
	if err != nil {
		handleStoreError(w, err, "cliente não encontrado")
		return
	}
	changed := []string{}
	for field, differs := range map[string]bool{
		"name": before.Name != user.Name, "phone": before.Phone != user.Phone, "document": before.Document != user.Document,
	} {
		if differs {
			changed = append(changed, field)
		}
	}
	if len(changed) > 0 {
		s.recordBillingAudit(r, audit.ActionCustomerUpdated,
			map[string]any{"customerId": customerID, "self": true, "changed": changed})
	}
	writeJSON(w, http.StatusOK, user)
}
