package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"

	"github.com/google/uuid"
)

type contextKey string

const principalKey contextKey = "auth.principal"

// Principal é o usuário autenticado da requisição.
type Principal struct {
	UserID uuid.UUID
	Email  string
	Role   string
	// Membership: quem é da equipe de um cliente enxerga a conta do titular,
	// com o papel que ele deu. Nulo para o próprio titular e para a equipe da
	// central.
	Membership *Membership
}

func (p *Principal) Is(roles ...string) bool {
	for _, role := range roles {
		if p.Role == role {
			return true
		}
	}
	return false
}

// IsCustomer: cliente final, restrito aos próprios veículos e faturas.
func (p *Principal) IsCustomer() bool { return p.Is(RoleCustomer) }

// Account é a conta de cliente que a requisição enxerga: a do titular, para
// quem é da equipe dele; a própria, para o titular.
func (p *Principal) Account() uuid.UUID {
	if p.Membership != nil {
		return p.Membership.AccountID
	}
	return p.UserID
}

// IsAccountOwner: o cliente titular da conta (não alguém da equipe dele).
func (p *Principal) IsAccountOwner() bool { return p.IsCustomer() && p.Membership == nil }

// CanOperateAccount: o titular e o Gestor mexem na conta (comandos, cercas,
// faturas); o Visualizador só acompanha.
func (p *Principal) CanOperateAccount() bool {
	return p.IsCustomer() && (p.Membership == nil || p.Membership.Role == MemberManager)
}

// SeesWholeAccount: o titular e quem da equipe não está restrito a grupos.
// O que é da conta inteira — faturas, envios, os grupos e os campos da
// frota, os e-mails a mais dos alertas — fica só com eles.
func (p *Principal) SeesWholeAccount() bool {
	return p.IsCustomer() && (p.Membership == nil || !p.Membership.Restricted)
}

// SeesGroup diz se a pessoa vê os veículos do grupo (nil: sem grupo). Só
// quem da equipe está restrito a grupos deixa de ver algum; a conta em si
// quem confere é quem chama (Account).
func (p *Principal) SeesGroup(group *uuid.UUID) bool {
	if p.Membership == nil || !p.Membership.Restricted {
		return true
	}
	return group != nil && slices.Contains(p.Membership.Groups, *group)
}

// CanSendCommands: quem só visualiza não aciona o veículo. Vale para a
// equipe; o cliente tem rotas próprias, limitadas aos veículos dele.
func (p *Principal) CanSendCommands() bool { return p.Is(RoleAdmin, RoleOperator) }

// CanManage: alterações de cadastro são de administrador.
func (p *Principal) CanManage() bool { return p.Is(RoleAdmin) }

func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, principalKey, p)
}

// FromContext devolve o usuário autenticado, se houver.
func FromContext(ctx context.Context) (*Principal, bool) {
	p, ok := ctx.Value(principalKey).(*Principal)
	return p, ok
}

// Middleware exige um access token válido.
//
// O token vem no header Authorization. Para o WebSocket, em que o navegador
// não deixa definir headers, aceitamos também ?token= na query string.
func (s *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r)
		if token == "" {
			writeUnauthorized(w, "token de acesso ausente")
			return
		}

		claims, err := s.Parse(token)
		if err != nil {
			writeUnauthorized(w, "token inválido ou expirado")
			return
		}
		userID, err := uuid.Parse(claims.Subject)
		if err != nil {
			writeUnauthorized(w, "token malformado")
			return
		}

		principal := &Principal{UserID: userID, Email: claims.Email, Role: claims.Role}
		// Quem é da equipe de um cliente: conferido a cada requisição, para a
		// remoção (ou a troca de papel) valer na hora.
		if claims.Role == RoleCustomer {
			if principal.Membership, err = s.Membership(r.Context(), userID); err != nil {
				s.log.Error("falha ao conferir a conta empresa", "user", userID, "err", err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "erro interno"})
				return
			}
		}
		next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), principal)))
	})
}

// RequireRole restringe a rota aos perfis informados.
func RequireRole(roles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal, ok := FromContext(r.Context())
			if !ok {
				writeUnauthorized(w, "autenticação obrigatória")
				return
			}
			if !principal.Is(roles...) {
				writeJSON(w, http.StatusForbidden, map[string]string{
					"error": "seu perfil não permite esta operação",
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func bearerToken(r *http.Request) string {
	header := r.Header.Get("Authorization")
	if after, found := strings.CutPrefix(header, "Bearer "); found {
		return strings.TrimSpace(after)
	}
	// O navegador não deixa pôr cabeçalho no WebSocket: só ali o token vem na
	// URL. Nas demais rotas ele não vale — token em URL acaba em log de proxy
	// e no Referer de quem recebe o link.
	if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return strings.TrimSpace(r.URL.Query().Get("token"))
	}
	return ""
}

func writeUnauthorized(w http.ResponseWriter, message string) {
	writeJSON(w, http.StatusUnauthorized, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
