// Package organizations são as associações e seguradoras: a organização
// contrata o rastreamento para os associados dela e paga uma fatura por mês
// (os veículos com rastreador instalado, no preço combinado com ela). Quem é
// da equipe da organização entra num painel próprio, que mostra só os
// veículos dos associados; o associado usa o app da Farbo como qualquer
// cliente, sem faturas nem pedido de veículo — quem cuida disso é a
// organização.
//
// A organização é também um usuário (role organization, sem login): o nome,
// o CNPJ, o e-mail e o telefone dela são o pagador das faturas, onde o Pix, o
// boleto e a régua de cobrança já procuram. O que cada papel pode fica nas
// rotas da API (auth.Principal); aqui ficam o cadastro e as consultas.
package organizations

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/fulfillment"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/mail"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/phone"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/vehicles"
)

// Os tipos de organização.
const (
	KindAssociation = "association"
	KindInsurer     = "insurer"
)

// ValidKind diz se o tipo existe.
func ValidKind(kind string) bool { return kind == KindAssociation || kind == KindInsurer }

// O rastreador dos associados: em comodato (emprestado, volta na
// desinstalação) ou vendido.
const (
	EquipmentLoan = "loan"
	EquipmentSale = "sale"
)

// InputError é um dado recusado; a mensagem vai para a tela.
type InputError struct{ Message string }

func (e *InputError) Error() string { return e.Message }

// Organization é a associação ou seguradora, com os números do painel.
type Organization struct {
	ID       uuid.UUID `json:"id"`
	Kind     string    `json:"kind"`
	Name     string    `json:"name"`
	Document string    `json:"document"`
	// Email e Phone são o contato de cobrança (o pagador das faturas).
	Email      string `json:"email"`
	Phone      string `json:"phone"`
	PriceCents int    `json:"priceCents"`
	DueDay     int    `json:"dueDay"`
	Terms
	Notes     string    `json:"notes"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"createdAt"`

	// Associates: os associados ativos; Vehicles, os veículos deles;
	// Installed, os que já têm rastreador (os que entram na fatura).
	Associates int `json:"associates"`
	Vehicles   int `json:"vehicles"`
	Installed  int `json:"installed"`
	Members    int `json:"members"`
	// As faturas da organização em aberto, e as vencidas delas.
	OpenInvoices    int `json:"openInvoices"`
	OverdueInvoices int `json:"overdueInvoices"`
}

// Terms é o rastreador dos associados e as taxas, definidos pela central.
type Terms struct {
	// EquipmentMode: loan (comodato) ou sale (venda, EquipmentCents por
	// rastreador instalado).
	EquipmentMode  string `json:"equipmentMode"`
	EquipmentCents int    `json:"equipmentCents"`
	// As taxas de instalação e de desinstalação (zero: sem taxa).
	InstallFeeCents   int `json:"installFeeCents"`
	UninstallFeeCents int `json:"uninstallFeeCents"`
}

// Input é o cadastro da organização (o documento já vem conferido).
type Input struct {
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Document   string `json:"document"`
	Email      string `json:"email"`
	Phone      string `json:"phone"`
	PriceCents int    `json:"priceCents"`
	DueDay     int    `json:"dueDay"`
	Terms
	Notes string `json:"notes"`
	// Active só vale na edição (nulo: mantém).
	Active *bool `json:"active"`
}

// Member é alguém da equipe da organização.
type Member struct {
	UserID    uuid.UUID `json:"userId"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"createdAt"`
	// LastSeenAt: a última vez que entrou (sessão aberta ou renovada).
	LastSeenAt *time.Time `json:"lastSeenAt"`
}

// MemberInput é quem entra na equipe.
type MemberInput struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

// Associate é o associado: o cliente cujo rastreamento a organização paga.
type Associate struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Phone     string    `json:"phone"`
	Document  string    `json:"document"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"createdAt"`
	Vehicles  int       `json:"vehicles"`
	Installed int       `json:"installed"`
	// LastSeenAt: a última vez que entrou no app (nulo: ainda não entrou).
	LastSeenAt *time.Time `json:"lastSeenAt"`
	// BrokerID e BrokerName: o corretor do associado (nulo: nenhum).
	BrokerID   *uuid.UUID `json:"brokerId"`
	BrokerName string     `json:"brokerName"`
}

// AssociateInput é o cadastro do associado (o documento já vem conferido).
// Active só vale na edição (nulo: mantém).
type AssociateInput struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Phone    string `json:"phone"`
	Document string `json:"document"`
	Active   *bool  `json:"active"`
	// BrokerID: o corretor (nulo mantém; "" tira).
	BrokerID *string `json:"brokerId"`
}

// Accounts é o que a organização precisa das contas (auth.Service).
type Accounts interface {
	FindByEmail(ctx context.Context, email string) (*auth.User, error)
	Register(ctx context.Context, in auth.NewUser) (*auth.User, error)
	IssueInvite(ctx context.Context, user *auth.User) (string, time.Duration, error)
	InviteUser(ctx context.Context, user *auth.User) error
	GetUser(ctx context.Context, id uuid.UUID) (*auth.User, error)
	RevokeSessions(ctx context.Context, userID uuid.UUID) error
}

// Notifier manda o convite de quem entra na equipe (mail.OrgMailer).
type Notifier interface {
	MemberInvited(ctx context.Context, to string, n mail.OrgNotice, token string, ttl time.Duration) error
	// Announcement: o comunicado da organização, por e-mail ao associado.
	Announcement(ctx context.Context, to string, n mail.OrgAnnouncement) error
}

type Service struct {
	db       *database.DB
	accounts Accounts
	notifier Notifier
	billing  *billing.Service
	pusher   Pusher
	log      *slog.Logger
	// runAsync manda os e-mails fora da requisição; os testes trocam por uma
	// chamada síncrona.
	runAsync func(func())
}

func NewService(db *database.DB, accounts Accounts, notifier Notifier, billingSvc *billing.Service, log *slog.Logger) *Service {
	return &Service{
		db: db, accounts: accounts, notifier: notifier, billing: billingSvc,
		log: log.With("component", "organizations"), runAsync: func(f func()) { go f() },
	}
}

// SetSync faz os e-mails saírem dentro da chamada (testes).
func (s *Service) SetSync() { s.runAsync = func(f func()) { f() } }

// Lookup é o vínculo de quem é da equipe de uma organização (nil: não é, ou
// a organização foi desativada). É a consulta que o login e cada requisição
// fazem.
func (s *Service) Lookup(ctx context.Context, userID uuid.UUID) (*auth.OrgMembership, error) {
	var m auth.OrgMembership
	err := s.db.QueryRow(ctx, `
		SELECT m.organization_id, u.name, o.kind, m.role
		FROM organization_members m
		JOIN organizations o ON o.id = m.organization_id
		JOIN users u ON u.id = o.id AND u.active
		WHERE m.user_id = $1`, userID).Scan(&m.OrganizationID, &m.Name, &m.Kind, &m.Role)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, database.MapError(err)
	}
	return &m, nil
}

// ---------------------------------------------------------------------------
// Cadastro
// ---------------------------------------------------------------------------

// selectOrganization: $1 é o "hoje" da cobrança (as faturas vencidas).
const selectOrganization = `
	SELECT u.id, o.kind, u.name, u.document, u.email, u.phone, o.price_cents, o.due_day,
		o.equipment_mode, o.equipment_cents, o.install_fee_cents, o.uninstall_fee_cents, o.notes, u.active, o.created_at,
		(SELECT count(*) FROM users a WHERE a.organization_id = o.id AND a.active),
		(SELECT count(*) FROM vehicles v JOIN users a ON a.id = v.owner_id WHERE a.organization_id = o.id AND a.active),
		(SELECT count(*) FROM vehicles v JOIN users a ON a.id = v.owner_id
			WHERE a.organization_id = o.id AND a.active AND v.device_id IS NOT NULL),
		(SELECT count(*) FROM organization_members m WHERE m.organization_id = o.id),
		(SELECT count(*) FROM invoices i WHERE i.customer_id = o.id AND i.status = 'OPEN'),
		(SELECT count(*) FROM invoices i WHERE i.customer_id = o.id AND i.status = 'OPEN' AND i.due_date < $1::date)
	FROM organizations o JOIN users u ON u.id = o.id`

func scanOrganization(row database.Scanner) (*Organization, error) {
	var o Organization
	if err := row.Scan(&o.ID, &o.Kind, &o.Name, &o.Document, &o.Email, &o.Phone, &o.PriceCents, &o.DueDay,
		&o.EquipmentMode, &o.EquipmentCents, &o.InstallFeeCents, &o.UninstallFeeCents, &o.Notes,
		&o.Active, &o.CreatedAt, &o.Associates, &o.Vehicles, &o.Installed, &o.Members, &o.OpenInvoices,
		&o.OverdueInvoices); err != nil {
		return nil, database.MapError(err)
	}
	return &o, nil
}

// List são as organizações, da mais nova à mais antiga.
func (s *Service) List(ctx context.Context) ([]*Organization, error) {
	rows, err := s.db.Query(ctx, selectOrganization+` ORDER BY o.created_at DESC`, s.billing.Today().String())
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	out := []*Organization{}
	for rows.Next() {
		o, err := scanOrganization(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, database.MapError(rows.Err())
}

// Get é uma organização.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (*Organization, error) {
	return scanOrganization(s.db.QueryRow(ctx, selectOrganization+` WHERE o.id = $2`, s.billing.Today().String(), id))
}

func clean(in *Input) error {
	in.Kind = strings.TrimSpace(in.Kind)
	in.Name = strings.Join(strings.Fields(in.Name), " ")
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	in.Notes = strings.TrimSpace(in.Notes)
	switch {
	case !ValidKind(in.Kind):
		return &InputError{"escolha o tipo: associação ou seguradora"}
	case len([]rune(in.Name)) < 2 || len([]rune(in.Name)) > 120:
		return &InputError{"informe o nome da organização (até 120 caracteres)"}
	case !validEmail(in.Email):
		return &InputError{"informe um e-mail de cobrança válido"}
	case in.PriceCents < 0 || in.PriceCents > 100_000_00:
		return &InputError{"o preço por veículo vai de R$ 0 a R$ 100.000"}
	case in.DueDay < 1 || in.DueDay > 28:
		return &InputError{"o dia de vencimento vai de 1 a 28"}
	case len([]rune(in.Notes)) > 1000:
		return &InputError{"as observações passam de 1.000 caracteres"}
	}
	return cleanTerms(&in.Terms)
}

// cleanTerms confere o rastreador e as taxas.
func cleanTerms(t *Terms) error {
	if t.EquipmentMode == "" {
		t.EquipmentMode = EquipmentLoan
	}
	switch {
	case t.EquipmentMode != EquipmentLoan && t.EquipmentMode != EquipmentSale:
		return &InputError{"escolha o rastreador: comodato ou venda"}
	case t.EquipmentCents < 0 || t.EquipmentCents > 100_000_00,
		t.InstallFeeCents < 0 || t.InstallFeeCents > 100_000_00,
		t.UninstallFeeCents < 0 || t.UninstallFeeCents > 100_000_00:
		return &InputError{"os valores do rastreador e das taxas vão de R$ 0 a R$ 100.000"}
	}
	if t.EquipmentMode == EquipmentLoan {
		t.EquipmentCents = 0
	}
	return nil
}

func validEmail(email string) bool {
	return strings.Contains(email, "@") && !strings.ContainsAny(email, " \t\r\n<>") && len(email) <= 254
}

// Create cadastra a organização (sem a equipe: ela entra por AddMember).
func (s *Service) Create(ctx context.Context, in Input, actor uuid.UUID) (*Organization, error) {
	if err := clean(&in); err != nil {
		return nil, err
	}
	var id uuid.UUID
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		// Sem senha: a organização não entra no painel (quem entra é a equipe).
		if err := tx.QueryRow(ctx, `
			INSERT INTO users (email, name, role, phone, document, password_hash)
			VALUES ($1, $2, $3, $4, $5, '!') RETURNING id`,
			in.Email, in.Name, auth.RoleOrganization, phone.Format(in.Phone), in.Document).Scan(&id); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO organizations (id, kind, price_cents, due_day, notes, created_by,
				equipment_mode, equipment_cents, install_fee_cents, uninstall_fee_cents)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
			id, in.Kind, in.PriceCents, in.DueDay, in.Notes, nullable(actor),
			in.EquipmentMode, in.EquipmentCents, in.InstallFeeCents, in.UninstallFeeCents)
		return err
	})
	if err != nil {
		return nil, database.MapError(err)
	}
	return s.Get(ctx, id)
}

// Update altera o cadastro. Desativada, a equipe dela perde o acesso ao
// painel na hora (os associados continuam com o app).
func (s *Service) Update(ctx context.Context, id uuid.UUID, in Input) (*Organization, error) {
	if err := clean(&in); err != nil {
		return nil, err
	}
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE users SET email = $2, name = $3, phone = $4, document = $5, active = COALESCE($6, active), updated_at = NOW()
			WHERE id = $1 AND role = $7`,
			id, in.Email, in.Name, phone.Format(in.Phone), in.Document, in.Active, auth.RoleOrganization)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return database.ErrNotFound
		}
		_, err = tx.Exec(ctx, `
			UPDATE organizations SET kind = $2, price_cents = $3, due_day = $4, notes = $5,
				equipment_mode = $6, equipment_cents = $7, install_fee_cents = $8, uninstall_fee_cents = $9, updated_at = NOW()
			WHERE id = $1`,
			id, in.Kind, in.PriceCents, in.DueDay, in.Notes,
			in.EquipmentMode, in.EquipmentCents, in.InstallFeeCents, in.UninstallFeeCents)
		return err
	})
	if err != nil {
		return nil, database.MapError(err)
	}
	return s.Get(ctx, id)
}

func nullable(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

// active diz se a organização existe e está ativa.
func (s *Service) active(ctx context.Context, q database.Querier, id uuid.UUID) (*Organization, error) {
	var o Organization
	err := q.QueryRow(ctx, `
		SELECT u.id, u.name, o.kind, u.active FROM organizations o JOIN users u ON u.id = o.id WHERE o.id = $1`,
		id).Scan(&o.ID, &o.Name, &o.Kind, &o.Active)
	if err != nil {
		return nil, database.MapError(err)
	}
	if !o.Active {
		return nil, &InputError{"a organização está desativada"}
	}
	return &o, nil
}

// ---------------------------------------------------------------------------
// Equipe
// ---------------------------------------------------------------------------

const memberColumns = `u.id, u.name, u.email, m.role, u.active, m.created_at,
	(SELECT max(t.created_at) FROM refresh_tokens t WHERE t.user_id = u.id)`

func scanMember(row database.Scanner) (*Member, error) {
	var m Member
	if err := row.Scan(&m.UserID, &m.Name, &m.Email, &m.Role, &m.Active, &m.CreatedAt, &m.LastSeenAt); err != nil {
		return nil, database.MapError(err)
	}
	return &m, nil
}

// Members é a equipe da organização, na ordem em que entrou.
func (s *Service) Members(ctx context.Context, orgID uuid.UUID) ([]*Member, error) {
	rows, err := s.db.Query(ctx, `
		SELECT `+memberColumns+` FROM organization_members m JOIN users u ON u.id = m.user_id
		WHERE m.organization_id = $1 ORDER BY m.created_at, u.name`, orgID)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	out := []*Member{}
	for rows.Next() {
		m, err := scanMember(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, database.MapError(rows.Err())
}

func (s *Service) member(ctx context.Context, orgID, userID uuid.UUID) (*Member, error) {
	return scanMember(s.db.QueryRow(ctx, `
		SELECT `+memberColumns+` FROM organization_members m JOIN users u ON u.id = m.user_id
		WHERE m.organization_id = $1 AND m.user_id = $2`, orgID, userID))
}

func roleMessage() error {
	return &InputError{"escolha o papel: Administrador, Operador ou Visualizador"}
}

// AddMember põe a pessoa na equipe da organização e manda o convite para ela
// criar a senha. O e-mail precisa ser novo na Farbo (ou de alguém que já foi
// da equipe de uma organização e saiu).
func (s *Service) AddMember(ctx context.Context, orgID uuid.UUID, in MemberInput, invitedBy uuid.UUID) (*Member, error) {
	in.Name = strings.Join(strings.Fields(in.Name), " ")
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	switch {
	case in.Name == "" || len([]rune(in.Name)) > 120:
		return nil, &InputError{"informe o nome da pessoa (até 120 caracteres)"}
	case !validEmail(in.Email):
		return nil, &InputError{"informe um e-mail válido"}
	case !auth.ValidOrgRole(in.Role):
		return nil, roleMessage()
	}
	org, err := s.active(ctx, s.db, orgID)
	if err != nil {
		return nil, err
	}

	user, err := s.memberAccount(ctx, in)
	if err != nil {
		return nil, err
	}
	if _, err := s.db.Exec(ctx, `
		INSERT INTO organization_members (user_id, organization_id, role, invited_by) VALUES ($1, $2, $3, $4)`,
		user.ID, orgID, in.Role, nullable(invitedBy)); err != nil {
		if errors.Is(database.MapError(err), database.ErrConflict) {
			return nil, &InputError{"essa pessoa já é da equipe de uma organização"}
		}
		return nil, database.MapError(err)
	}
	member, err := s.member(ctx, orgID, user.ID)
	if err != nil {
		return nil, err
	}
	s.invite(user, mail.OrgNotice{OrgName: org.Name, MemberName: member.Name, Kind: org.Kind, Role: member.Role})
	return member, nil
}

// memberAccount cria o login da pessoa (sem senha conhecida: ela cria pelo
// convite), ou reativa o de quem já foi da equipe de uma organização.
func (s *Service) memberAccount(ctx context.Context, in MemberInput) (*auth.User, error) {
	for attempt := 0; attempt < 2; attempt++ {
		user, err := s.accounts.FindByEmail(ctx, in.Email)
		switch {
		case err == nil:
			if user.Role != auth.RoleOrgMember {
				return nil, &InputError{"esse e-mail já tem outro acesso na Farbo: use outro e-mail para a pessoa da organização"}
			}
			var member bool
			if err := s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM organization_members WHERE user_id = $1)`,
				user.ID).Scan(&member); err != nil {
				return nil, database.MapError(err)
			}
			if member {
				return nil, &InputError{"essa pessoa já é da equipe de uma organização"}
			}
			if _, err := s.db.Exec(ctx, `UPDATE users SET active = TRUE, name = $2, updated_at = NOW() WHERE id = $1`,
				user.ID, in.Name); err != nil {
				return nil, database.MapError(err)
			}
			user.Active, user.Name = true, in.Name
			return user, nil
		case !errors.Is(err, database.ErrNotFound):
			return nil, err
		}
		password, err := auth.RandomPassword()
		if err != nil {
			return nil, err
		}
		user, err = s.accounts.Register(ctx, auth.NewUser{Email: in.Email, Name: in.Name, Role: auth.RoleOrgMember, Password: password})
		if err == nil {
			return user, nil
		}
		// Criada ao mesmo tempo por outro pedido: na volta, ela já existe.
		if !errors.Is(err, database.ErrConflict) {
			return nil, &InputError{err.Error()}
		}
	}
	return nil, database.ErrConflict
}

// invite gera o link e manda o convite fora da requisição.
func (s *Service) invite(user *auth.User, notice mail.OrgNotice) {
	if s.notifier == nil {
		return
	}
	s.runAsync(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		token, ttl, err := s.accounts.IssueInvite(ctx, user)
		if err == nil {
			err = s.notifier.MemberInvited(ctx, user.Email, notice, token, ttl)
		}
		if err != nil {
			s.log.Error("convite da equipe da organização não saiu", "user", user.ID, "err", err)
		}
	})
}

// ResendMemberInvite manda de novo o convite (o link anterior deixa de valer).
func (s *Service) ResendMemberInvite(ctx context.Context, orgID, userID uuid.UUID) (*Member, error) {
	org, err := s.active(ctx, s.db, orgID)
	if err != nil {
		return nil, err
	}
	member, err := s.member(ctx, orgID, userID)
	if err != nil {
		return nil, err
	}
	user, err := s.accounts.GetUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	s.invite(user, mail.OrgNotice{OrgName: org.Name, MemberName: member.Name, Kind: org.Kind, Role: member.Role})
	return member, nil
}

// lockAdmins trava a equipe da organização e diz quantos administradores ela
// tem: duas mudanças ao mesmo tempo não a deixam sem nenhum.
func lockAdmins(ctx context.Context, tx pgx.Tx, orgID uuid.UUID) (int, error) {
	rows, err := tx.Query(ctx, `
		SELECT role FROM organization_members WHERE organization_id = $1 ORDER BY user_id FOR UPDATE`, orgID)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	admins := 0
	for rows.Next() {
		var role string
		if err := rows.Scan(&role); err != nil {
			return 0, err
		}
		if role == auth.OrgAdmin {
			admins++
		}
	}
	return admins, rows.Err()
}

var errLastAdmin = &InputError{"a organização precisa de pelo menos um Administrador"}

// SetMemberRole troca o papel de alguém da equipe (vale na próxima
// requisição dela).
func (s *Service) SetMemberRole(ctx context.Context, orgID, userID uuid.UUID, role string) (*Member, error) {
	if !auth.ValidOrgRole(role) {
		return nil, roleMessage()
	}
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		admins, err := lockAdmins(ctx, tx, orgID)
		if err != nil {
			return err
		}
		var current string
		if err := tx.QueryRow(ctx, `SELECT role FROM organization_members WHERE organization_id = $1 AND user_id = $2`,
			orgID, userID).Scan(&current); err != nil {
			return err
		}
		if current == auth.OrgAdmin && role != auth.OrgAdmin && admins <= 1 {
			return errLastAdmin
		}
		_, err = tx.Exec(ctx, `UPDATE organization_members SET role = $3, updated_at = NOW()
			WHERE organization_id = $1 AND user_id = $2`, orgID, userID, role)
		return err
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return s.member(ctx, orgID, userID)
}

// RemoveMember tira a pessoa da equipe: o acesso acaba na hora, as sessões
// são encerradas e o login fica desativado (dá para chamá-la de volta).
func (s *Service) RemoveMember(ctx context.Context, orgID, userID uuid.UUID) (*Member, error) {
	member, err := s.member(ctx, orgID, userID)
	if err != nil {
		return nil, err
	}
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		admins, err := lockAdmins(ctx, tx, orgID)
		if err != nil {
			return err
		}
		if member.Role == auth.OrgAdmin && admins <= 1 {
			return errLastAdmin
		}
		if _, err := tx.Exec(ctx, `DELETE FROM organization_members WHERE organization_id = $1 AND user_id = $2`,
			orgID, userID); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE users SET active = FALSE, updated_at = NOW() WHERE id = $1 AND role = $2`,
			userID, auth.RoleOrgMember)
		return err
	})
	if err != nil {
		return nil, mapErr(err)
	}
	if err := s.accounts.RevokeSessions(ctx, userID); err != nil {
		s.log.Error("pessoa saiu da equipe da organização, mas as sessões não foram encerradas", "user", userID, "err", err)
	}
	return member, nil
}

// mapErr deixa passar as recusas (InputError) e traduz as do banco.
func mapErr(err error) error {
	var input *InputError
	if errors.As(err, &input) {
		return input
	}
	return database.MapError(err)
}

// ---------------------------------------------------------------------------
// Associados
// ---------------------------------------------------------------------------

const associateColumns = `u.id, u.name, u.email, u.phone, u.document, u.active, u.created_at,
	(SELECT count(*) FROM vehicles v WHERE v.owner_id = u.id),
	(SELECT count(*) FROM vehicles v WHERE v.owner_id = u.id AND v.device_id IS NOT NULL),
	(SELECT max(t.created_at) FROM refresh_tokens t WHERE t.user_id = u.id),
	u.broker_id, COALESCE((SELECT b.name FROM users b WHERE b.id = u.broker_id), '')`

func scanAssociate(row database.Scanner) (*Associate, error) {
	var a Associate
	if err := row.Scan(&a.ID, &a.Name, &a.Email, &a.Phone, &a.Document, &a.Active, &a.CreatedAt,
		&a.Vehicles, &a.Installed, &a.LastSeenAt, &a.BrokerID, &a.BrokerName); err != nil {
		return nil, database.MapError(err)
	}
	return &a, nil
}

// Associates são os associados da organização, por nome.
func (s *Service) Associates(ctx context.Context, orgID uuid.UUID, broker *uuid.UUID) ([]*Associate, error) {
	rows, err := s.db.Query(ctx, `
		SELECT `+associateColumns+` FROM users u WHERE u.organization_id = $1 AND u.role = $2
			AND ($3::uuid IS NULL OR u.broker_id = $3) ORDER BY u.name`,
		orgID, auth.RoleCustomer, broker)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	out := []*Associate{}
	for rows.Next() {
		a, err := scanAssociate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, database.MapError(rows.Err())
}

// Associate é um associado da organização (de outra: não encontrado).
func (s *Service) Associate(ctx context.Context, orgID, id uuid.UUID) (*Associate, error) {
	return scanAssociate(s.db.QueryRow(ctx, `
		SELECT `+associateColumns+` FROM users u WHERE u.id = $1 AND u.organization_id = $2 AND u.role = $3`,
		id, orgID, auth.RoleCustomer))
}

func cleanAssociate(in *AssociateInput) error {
	in.Name = strings.Join(strings.Fields(in.Name), " ")
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	if in.Name == "" || len([]rune(in.Name)) > 120 {
		return &InputError{"informe o nome do associado (até 120 caracteres)"}
	}
	return nil
}

// CreateAssociate cadastra o associado e manda o convite para ele criar a
// senha do app (o mesmo dos clientes da Farbo). O e-mail precisa ser novo:
// um cliente que já existe entra na organização pela central.
func (s *Service) CreateAssociate(ctx context.Context, orgID uuid.UUID, in AssociateInput) (*Associate, error) {
	if err := cleanAssociate(&in); err != nil {
		return nil, err
	}
	if !validEmail(in.Email) {
		return nil, &InputError{"informe um e-mail válido: é por ele que o associado recebe o acesso ao app"}
	}
	if _, err := s.active(ctx, s.db, orgID); err != nil {
		return nil, err
	}
	if in.BrokerID != nil {
		if _, err := s.checkBroker(ctx, orgID, *in.BrokerID); err != nil {
			return nil, err
		}
	}
	if _, err := s.accounts.FindByEmail(ctx, in.Email); err == nil {
		return nil, &InputError{"esse e-mail já tem cadastro na Farbo. Para incluir essa pessoa como associada, fale com a central"}
	} else if !errors.Is(err, database.ErrNotFound) {
		return nil, err
	}
	password, err := auth.RandomPassword()
	if err != nil {
		return nil, err
	}
	user, err := s.accounts.Register(ctx, auth.NewUser{
		Email: in.Email, Name: in.Name, Role: auth.RoleCustomer, Password: password, Phone: in.Phone, Document: in.Document,
	})
	if err != nil {
		if errors.Is(err, database.ErrConflict) {
			return nil, &InputError{"esse e-mail já tem cadastro na Farbo. Para incluir essa pessoa como associada, fale com a central"}
		}
		return nil, &InputError{err.Error()}
	}
	if _, err := s.db.Exec(ctx, `UPDATE users SET organization_id = $2, updated_at = NOW() WHERE id = $1`,
		user.ID, orgID); err != nil {
		return nil, database.MapError(err)
	}
	if err := s.setBroker(ctx, orgID, user.ID, in.BrokerID); err != nil {
		return nil, err
	}
	if err := s.accounts.InviteUser(ctx, user); err != nil {
		s.log.Error("associado cadastrado, mas o convite não foi gerado", "user", user.ID, "err", err)
	}
	return s.Associate(ctx, orgID, user.ID)
}

// setBroker põe (ou tira, com "") o corretor do associado; nil não muda. O
// corretor precisa ser da equipe da organização, com o papel de corretor.
func (s *Service) setBroker(ctx context.Context, orgID, associateID uuid.UUID, brokerID *string) error {
	if brokerID == nil {
		return nil
	}
	broker, err := s.checkBroker(ctx, orgID, *brokerID)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx, `UPDATE users SET broker_id = $3, updated_at = NOW() WHERE id = $1 AND organization_id = $2`,
		associateID, orgID, broker)
	return database.MapError(err)
}

// checkBroker confere o corretor ("" é nenhum).
func (s *Service) checkBroker(ctx context.Context, orgID uuid.UUID, brokerID string) (*uuid.UUID, error) {
	if brokerID == "" {
		return nil, nil
	}
	id, err := uuid.Parse(brokerID)
	if err != nil {
		return nil, &InputError{"corretor inválido"}
	}
	var ok bool
	if err := s.db.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM organization_members WHERE user_id = $1 AND organization_id = $2 AND role = $3)`,
		id, orgID, auth.OrgBroker).Scan(&ok); err != nil {
		return nil, database.MapError(err)
	}
	if !ok {
		return nil, &InputError{"o corretor precisa ser da equipe, com o papel de Corretor"}
	}
	return &id, nil
}

// UpdateAssociate altera nome, telefone e documento do associado e, com
// Active, desliga (o acesso ao app cai na hora e os veículos saem da fatura)
// ou religa.
func (s *Service) UpdateAssociate(ctx context.Context, orgID, id uuid.UUID, in AssociateInput) (*Associate, error) {
	if err := cleanAssociate(&in); err != nil {
		return nil, err
	}
	if in.BrokerID != nil {
		if _, err := s.checkBroker(ctx, orgID, *in.BrokerID); err != nil {
			return nil, err
		}
	}
	tag, err := s.db.Exec(ctx, `
		UPDATE users SET name = $3, phone = $4, document = $5, active = COALESCE($6, active), updated_at = NOW()
		WHERE id = $1 AND organization_id = $2 AND role = $7`,
		id, orgID, in.Name, phone.Format(in.Phone), in.Document, in.Active, auth.RoleCustomer)
	if err != nil {
		return nil, database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return nil, database.ErrNotFound
	}
	if err := s.setBroker(ctx, orgID, id, in.BrokerID); err != nil {
		return nil, err
	}
	if in.Active != nil && !*in.Active {
		if err := s.accounts.RevokeSessions(ctx, id); err != nil {
			s.log.Error("associado desligado, mas as sessões não foram encerradas", "user", id, "err", err)
		}
	}
	return s.Associate(ctx, orgID, id)
}

// ResendAssociateInvite manda de novo o convite do app.
func (s *Service) ResendAssociateInvite(ctx context.Context, orgID, id uuid.UUID) (*Associate, error) {
	a, err := s.Associate(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	if !a.Active {
		return nil, &InputError{"o associado está desligado; religue antes de convidar"}
	}
	user, err := s.accounts.GetUser(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.accounts.InviteUser(ctx, user); err != nil {
		return nil, err
	}
	return a, nil
}

// LinkCustomer põe um cliente que já existe na organização (pela central): o
// rastreamento dele passa a ser pago por ela. Quem tem assinatura paga ativa
// precisa encerrá-la antes, para não pagar duas vezes.
func (s *Service) LinkCustomer(ctx context.Context, orgID, customerID uuid.UUID) error {
	if _, err := s.active(ctx, s.db, orgID); err != nil {
		return err
	}
	var role string
	var member, paid bool
	err := s.db.QueryRow(ctx, `
		SELECT u.role,
			EXISTS (SELECT 1 FROM account_members m WHERE m.user_id = u.id),
			EXISTS (SELECT 1 FROM subscriptions s WHERE s.customer_id = u.id AND s.status = 'ACTIVE' AND s.price_cents > 0)
		FROM users u WHERE u.id = $1`, customerID).Scan(&role, &member, &paid)
	if err != nil {
		return database.MapError(err)
	}
	switch {
	case role != auth.RoleCustomer:
		return database.ErrNotFound
	case member:
		return &InputError{"essa pessoa é da equipe da conta de outro cliente, não tem veículos próprios"}
	case paid:
		return &InputError{"o cliente tem assinatura paga ativa: encerre-a antes de passar o rastreamento para a organização"}
	}
	_, err = s.db.Exec(ctx, `UPDATE users SET organization_id = $2, updated_at = NOW() WHERE id = $1`, customerID, orgID)
	return database.MapError(err)
}

// UnlinkCustomer tira o cliente da organização: ele volta a ser um cliente
// comum (sem assinatura, até contratar).
func (s *Service) UnlinkCustomer(ctx context.Context, customerID uuid.UUID) error {
	tag, err := s.db.Exec(ctx, `UPDATE users SET organization_id = NULL, updated_at = NOW()
		WHERE id = $1 AND role = $2 AND organization_id IS NOT NULL`, customerID, auth.RoleCustomer)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return database.ErrNotFound
	}
	return nil
}

// OfCustomer é a organização do associado (nil: não é de nenhuma).
func (s *Service) OfCustomer(ctx context.Context, customerID uuid.UUID) (*Organization, error) {
	var orgID *uuid.UUID
	if err := s.db.QueryRow(ctx, `SELECT organization_id FROM users WHERE id = $1`, customerID).Scan(&orgID); err != nil {
		return nil, database.MapError(err)
	}
	if orgID == nil {
		return nil, nil
	}
	return s.Get(ctx, *orgID)
}

// RegisterVehicle cadastra o veículo do associado e abre o pedido de
// instalação (o chip e o rastreador, na fila de pedidos da central). Sem
// assinatura: o veículo entra na fatura da organização quando o rastreador
// for instalado.
func (s *Service) RegisterVehicle(ctx context.Context, orgID, associateID uuid.UUID, in vehicles.Input) (*vehicles.Vehicle, error) {
	if err := vehicles.Validate(in); err != nil {
		return nil, err
	}
	a, err := s.Associate(ctx, orgID, associateID)
	if err != nil {
		return nil, err
	}
	if !a.Active {
		return nil, &InputError{"o associado está desligado; religue antes de cadastrar um veículo"}
	}
	in.OwnerID, in.DeviceID, in.GroupID = &associateID, nil, nil
	var vehicle *vehicles.Vehicle
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT 1 FROM users WHERE id = $1 FOR UPDATE`, associateID); err != nil {
			return err
		}
		var err error
		if vehicle, err = vehicles.InsertWith(ctx, tx, in); err != nil {
			return err
		}
		_, err = fulfillment.Create(ctx, tx, associateID, vehicle.ID, nil)
		return err
	})
	if err != nil {
		return nil, database.MapError(err)
	}
	return vehicle, nil
}
