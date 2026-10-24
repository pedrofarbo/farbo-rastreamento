package auth

import (
	"context"

	"github.com/google/uuid"
)

// A conta empresa: quem é da equipe de um cliente entra com o próprio login
// e enxerga a conta do titular, com o papel que ele deu.
const (
	// MemberManager (Gestor) opera tudo da conta — comandos, cercas, faturas
	// —, menos a própria conta: equipe, cadastro, contrato e pedidos.
	MemberManager = "manager"
	// MemberViewer (Visualizador) só acompanha: mapa, histórico, eventos e
	// cercas, sem mexer em nada.
	MemberViewer = "viewer"
)

// ValidMemberRole diz se o papel na equipe existe.
func ValidMemberRole(role string) bool { return role == MemberManager || role == MemberViewer }

// Membership é o vínculo de quem é da equipe de um cliente.
type Membership struct {
	// AccountID é o titular da conta (o cliente dono dos veículos).
	AccountID uuid.UUID `json:"accountId"`
	// OwnerName é o nome do titular, para a tela ("Conta de Fulano").
	OwnerName string `json:"ownerName"`
	Role      string `json:"role"`
	// Restricted: a pessoa só vê os veículos dos grupos em Groups (sem
	// grupo nenhum, nenhum veículo); sem restrição, a frota inteira.
	Restricted bool        `json:"restricted"`
	Groups     []uuid.UUID `json:"groups"`
}

// MembershipLookup acha o vínculo de um usuário (nil: não é da equipe de
// ninguém). Fica fora do pacote (accounts) e é ligado no main.
type MembershipLookup func(ctx context.Context, userID uuid.UUID) (*Membership, error)

// SetMemberships liga a consulta da conta empresa. Sem ela, todo cliente é
// titular da própria conta.
func (s *Service) SetMemberships(lookup MembershipLookup) { s.memberships = lookup }

// Membership devolve o vínculo do usuário com a conta de um cliente (nil se
// não houver).
func (s *Service) Membership(ctx context.Context, userID uuid.UUID) (*Membership, error) {
	if s.memberships == nil {
		return nil, nil
	}
	return s.memberships(ctx, userID)
}

// RevokeSessions encerra todas as sessões do usuário (o acesso acaba na
// próxima renovação do token).
func (s *Service) RevokeSessions(ctx context.Context, userID uuid.UUID) error {
	return s.repo.RevokeAllForUser(ctx, userID)
}

// A equipe de uma associação ou seguradora: cada pessoa entra com o próprio
// login (RoleOrgMember) no painel da organização, com um destes papéis.
const (
	// OrgAdmin faz tudo no painel: associados, veículos, bloqueio, a equipe
	// e as faturas.
	OrgAdmin = "admin"
	// OrgOperator cadastra associados e veículos e bloqueia e desbloqueia.
	OrgOperator = "operator"
	// OrgViewer só acompanha: mapa, histórico, associados.
	OrgViewer = "viewer"
	// OrgBroker é o corretor da seguradora: vê e cadastra só os associados
	// que são dele (e os veículos deles), sem bloqueio nem o resto do painel.
	OrgBroker = "broker"
)

// ValidOrgRole diz se o papel na organização existe.
func ValidOrgRole(role string) bool {
	return role == OrgAdmin || role == OrgOperator || role == OrgViewer || role == OrgBroker
}

// OrgMembership é o vínculo de quem é da equipe de uma organização.
type OrgMembership struct {
	OrganizationID uuid.UUID `json:"organizationId"`
	// Name é o nome da organização, para a tela ("Associação X").
	Name string `json:"name"`
	// Kind: association (associação) ou insurer (seguradora).
	Kind string `json:"kind"`
	Role string `json:"role"`
}

// OrgMembershipLookup acha o vínculo de alguém da equipe de uma organização
// (nil: não é de nenhuma). Fica fora do pacote (organizations) e é ligado no
// main.
type OrgMembershipLookup func(ctx context.Context, userID uuid.UUID) (*OrgMembership, error)

// SetOrgMemberships liga a consulta da equipe das organizações. Sem ela,
// ninguém entra no painel de uma organização.
func (s *Service) SetOrgMemberships(lookup OrgMembershipLookup) { s.orgMemberships = lookup }

// OrgMembership devolve o vínculo do usuário com uma organização (nil se não
// houver).
func (s *Service) OrgMembership(ctx context.Context, userID uuid.UUID) (*OrgMembership, error) {
	if s.orgMemberships == nil {
		return nil, nil
	}
	return s.orgMemberships(ctx, userID)
}
