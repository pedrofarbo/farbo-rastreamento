package api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
)

// O painel das associações e seguradoras: quem é da equipe de uma
// organização (RoleOrgMember) entra só nas rotas do painel dela e nas de
// veículo, e vê só os veículos dos associados. Fora delas, nada: muitas rotas
// tratam quem não é cliente como a equipe da central, então a regra aqui é a
// da lista do que pode, e não a do que não pode.

// codeOrgRole acompanha o 403 de quem é da organização e não tem o papel
// para a ação.
const codeOrgRole = "ORG_ROLE"

// orgAllowed são as rotas que a equipe de uma organização usa.
var orgAllowed = []string{
	"/api/org/",
	"/api/vehicles",
	"/api/reports/",
	"/api/step-up/",
	"/api/security/two-factor",
	"/api/geocoding/reverse",
}

// orgGate barra quem é de uma organização fora das rotas do painel dela.
func (s *Server) orgGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, ok := auth.FromContext(r.Context())
		if !ok || !principal.IsOrgMember() {
			next.ServeHTTP(w, r)
			return
		}
		if principal.Org == nil {
			writeCode(w, http.StatusForbidden, "seu acesso ao painel da organização foi encerrado", codeOrgRole)
			return
		}
		path := r.URL.Path
		for _, prefix := range orgAllowed {
			if path == strings.TrimSuffix(prefix, "/") || strings.HasPrefix(path, prefix) {
				next.ServeHTTP(w, r)
				return
			}
		}
		writeCode(w, http.StatusForbidden, "seu acesso é ao painel da organização", codeOrgRole)
	})
}

// orgOf é a organização que a requisição enxerga (quem é da equipe dela).
func orgOf(r *http.Request) (uuid.UUID, bool) {
	if principal, ok := auth.FromContext(r.Context()); ok && principal.IsOrgMember() && principal.Org != nil {
		return principal.Org.OrganizationID, true
	}
	return uuid.Nil, false
}

// requireOrgMember: as rotas do painel da organização.
func (s *Server) requireOrgMember(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := orgOf(r); !ok || s.Organizations == nil {
			writeError(w, http.StatusForbidden, "seu perfil não permite esta operação")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireOrgOperator barra o Visualizador da organização (cadastros e
// comandos são do Administrador e do Operador). Para os demais perfis, não
// muda nada: cada rota segue as regras dela.
func (s *Server) requireOrgOperator(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if principal, ok := auth.FromContext(r.Context()); ok && principal.IsOrgMember() && !principal.CanOperateOrg() {
			writeCode(w, http.StatusForbidden, "seu acesso à organização é só para acompanhar", codeOrgRole)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireOrgRegistrar: cadastrar associados e veículos (o Administrador, o
// Operador e o corretor — o handler põe o corretor nos dele).
func (s *Server) requireOrgRegistrar(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if principal, ok := auth.FromContext(r.Context()); ok && principal.IsOrgMember() && !principal.CanRegisterOrg() {
			writeCode(w, http.StatusForbidden, "seu acesso à organização é só para acompanhar", codeOrgRole)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireOrgStaff barra o corretor no que é da organização inteira
// (recuperação, comunicados, ocorrências, vistorias).
func (s *Server) requireOrgStaff(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if principal, ok := auth.FromContext(r.Context()); ok && principal.IsOrgBroker() {
			writeCode(w, http.StatusForbidden, "seu acesso é aos seus associados", codeOrgRole)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// brokerOf é o corretor que chama (nil para os demais).
func brokerOf(r *http.Request) *uuid.UUID {
	if principal, ok := auth.FromContext(r.Context()); ok && principal.IsOrgBroker() {
		id := principal.UserID
		return &id
	}
	return nil
}

// requireOrgAdmin: a equipe e as faturas da organização são do Administrador.
func (s *Server) requireOrgAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if principal, ok := auth.FromContext(r.Context()); !ok || !principal.IsOrgAdmin() {
			writeCode(w, http.StatusForbidden, "só o Administrador da organização pode fazer isso", codeOrgRole)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireActiveOrg suspende o painel da organização com fatura vencida além
// do limite (como o do cliente); as faturas continuam abertas para pagar.
func (s *Server) requireActiveOrg(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		orgID, ok := orgOf(r)
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		suspended, err := s.Billing.IsSuspended(r.Context(), orgID)
		if err != nil {
			s.Log.Error("falha ao verificar a suspensão da organização", "organization", orgID, "err", err)
			writeError(w, http.StatusInternalServerError, "erro interno")
			return
		}
		if suspended {
			writeJSON(w, http.StatusPaymentRequired, map[string]string{
				"error": fmt.Sprintf("painel suspenso: há fatura vencida há mais de %d dias. "+
					"Regularize o pagamento em Faturas para voltar a acompanhar os veículos", s.Billing.SuspendAfterDays()),
				"code": codeAccountSuspended,
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}
