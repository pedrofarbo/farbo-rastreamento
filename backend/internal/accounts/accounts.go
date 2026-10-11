// Package accounts é a conta empresa: o cliente titular dá às pessoas da
// equipe dele, cada uma com o próprio login, o acesso aos veículos da conta.
// Gestor opera tudo, menos a própria conta (equipe, cadastro, contrato,
// pedidos); Visualizador só acompanha. Cada pessoa pode ficar restrita a
// alguns grupos da frota: vê, opera e recebe alertas só dos veículos deles,
// e o que é da conta inteira (faturas, grupos) fica de fora. Quem é da
// equipe não tem veículos próprios e é de uma conta só. O que cada papel pode fica nas rotas da API
// (auth.Principal); aqui ficam o cadastro da equipe e a consulta do vínculo.
package accounts

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/mail"
)

// O tamanho da equipe cresce com a frota: MinMembers pessoas, ou uma a cada
// VehiclesPerMember veículos ativos, até MaxMembers.
const (
	MinMembers        = 30
	MaxMembers        = 200
	VehiclesPerMember = 5
)

// LimitFor é o tamanho máximo da equipe de uma conta com tantos veículos ativos.
func LimitFor(activeVehicles int) int {
	return min(MaxMembers, max(MinMembers, activeVehicles/VehiclesPerMember))
}

// InputError é um dado recusado; a mensagem vai para a tela.
type InputError struct{ Message string }

func (e *InputError) Error() string { return e.Message }

// Member é uma pessoa da equipe.
type Member struct {
	UserID    uuid.UUID `json:"userId"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"createdAt"`
	// Restricted: só os veículos dos grupos em Groups; sem restrição, todos.
	Restricted bool        `json:"restricted"`
	Groups     []uuid.UUID `json:"groups"`
}

// Input é quem entra na equipe. Groups vazio: todos os veículos da conta;
// com grupos, só os veículos deles.
type Input struct {
	Name   string      `json:"name"`
	Email  string      `json:"email"`
	Role   string      `json:"role"`
	Groups []uuid.UUID `json:"groups"`
}

// Accounts é o que o cadastro da equipe precisa das contas (auth.Service).
type Accounts interface {
	GetUser(ctx context.Context, id uuid.UUID) (*auth.User, error)
	FindByEmail(ctx context.Context, email string) (*auth.User, error)
	Register(ctx context.Context, in auth.NewUser) (*auth.User, error)
	IssueInvite(ctx context.Context, user *auth.User) (string, time.Duration, error)
	RevokeSessions(ctx context.Context, userID uuid.UUID) error
}

// Notifier manda os e-mails da equipe (mail.TeamMailer).
type Notifier interface {
	MemberInvited(ctx context.Context, to string, n mail.TeamNotice, token string, ttl time.Duration) error
	MemberAdded(ctx context.Context, to string, n mail.TeamNotice) error
}

type Service struct {
	db       *database.DB
	accounts Accounts
	notifier Notifier
	log      *slog.Logger
	// runAsync manda os e-mails fora da requisição; os testes trocam por uma
	// chamada síncrona.
	runAsync func(func())
}

func NewService(db *database.DB, accounts Accounts, notifier Notifier, log *slog.Logger) *Service {
	return &Service{
		db: db, accounts: accounts, notifier: notifier, log: log.With("component", "accounts"),
		runAsync: func(f func()) { go f() },
	}
}

// SetSync faz os e-mails saírem dentro da chamada (testes).
func (s *Service) SetSync() { s.runAsync = func(f func()) { f() } }

// Lookup é o vínculo do usuário com a conta de um cliente (nil: não é da
// equipe de ninguém). É a consulta que o login e cada requisição fazem.
func (s *Service) Lookup(ctx context.Context, userID uuid.UUID) (*auth.Membership, error) {
	var m auth.Membership
	err := s.db.QueryRow(ctx, `
		SELECT m.account_id, o.name, m.role, m.restricted, `+memberGroups+`
		FROM account_members m JOIN users o ON o.id = m.account_id
		WHERE m.user_id = $1`, userID).Scan(&m.AccountID, &m.OwnerName, &m.Role, &m.Restricted, &m.Groups)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, database.MapError(err)
	}
	return &m, nil
}

// memberGroups são os grupos de quem está restrito (m é account_members).
const memberGroups = `COALESCE((SELECT array_agg(g.group_id ORDER BY g.group_id) FROM account_member_groups g WHERE g.user_id = m.user_id), '{}')`

const memberColumns = `u.id, u.name, u.email, m.role, u.active, m.created_at, m.restricted, ` + memberGroups

func scanMember(row database.Scanner) (*Member, error) {
	var m Member
	if err := row.Scan(&m.UserID, &m.Name, &m.Email, &m.Role, &m.Active, &m.CreatedAt, &m.Restricted, &m.Groups); err != nil {
		return nil, database.MapError(err)
	}
	return &m, nil
}

// List é a equipe da conta, na ordem em que entrou.
func (s *Service) List(ctx context.Context, accountID uuid.UUID) ([]*Member, error) {
	rows, err := s.db.Query(ctx, `
		SELECT `+memberColumns+` FROM account_members m JOIN users u ON u.id = m.user_id
		WHERE m.account_id = $1 ORDER BY m.created_at, u.name`, accountID)
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
	return out, rows.Err()
}

func (s *Service) get(ctx context.Context, accountID, userID uuid.UUID) (*Member, error) {
	return scanMember(s.db.QueryRow(ctx, `
		SELECT `+memberColumns+` FROM account_members m JOIN users u ON u.id = m.user_id
		WHERE m.account_id = $1 AND m.user_id = $2`, accountID, userID))
}

// Add põe a pessoa na equipe. Sem conta com o e-mail, ela é criada e a
// pessoa recebe o convite para criar a senha; com uma conta de cliente sem
// nada próprio (alguém que só acompanhava um veículo compartilhado, por
// exemplo), só o aviso. Devolve se a conta foi criada agora.
func (s *Service) Add(ctx context.Context, accountID uuid.UUID, in Input) (*Member, bool, error) {
	in.Name = strings.Join(strings.Fields(in.Name), " ")
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	switch {
	case in.Name == "" || len([]rune(in.Name)) > 120:
		return nil, false, &InputError{"informe o nome da pessoa (até 120 caracteres)"}
	case !strings.Contains(in.Email, "@") || strings.ContainsAny(in.Email, " \t\r\n<>") || len(in.Email) > 254:
		return nil, false, &InputError{"informe um e-mail válido"}
	case !auth.ValidMemberRole(in.Role):
		return nil, false, &InputError{"escolha o papel: Gestor ou Visualizador"}
	}

	owner, err := s.accounts.GetUser(ctx, accountID)
	if err != nil {
		return nil, false, err
	}
	if strings.EqualFold(owner.Email, in.Email) {
		return nil, false, &InputError{"esse é o e-mail da sua conta"}
	}
	limit, count, err := s.Limit(ctx, accountID)
	if err != nil {
		return nil, false, err
	}
	if count >= limit {
		return nil, false, &InputError{fmt.Sprintf("a equipe desta conta pode ter até %d pessoas (cresce com a frota)", limit)}
	}
	groups, err := s.checkGroups(ctx, accountID, in.Groups)
	if err != nil {
		return nil, false, err
	}

	user, created, err := s.memberAccount(ctx, in)
	if err != nil {
		return nil, false, err
	}
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO account_members (user_id, account_id, role, invited_by, restricted) VALUES ($1, $2, $3, $2, $4)`,
			user.ID, accountID, in.Role, len(groups) > 0); err != nil {
			return err
		}
		return setGroups(ctx, tx, user.ID, groups)
	})
	if err != nil {
		if errors.Is(database.MapError(err), database.ErrConflict) {
			return nil, false, &InputError{"essa pessoa já é da equipe de uma conta"}
		}
		return nil, false, database.MapError(err)
	}
	member, err := s.get(ctx, accountID, user.ID)
	if err != nil {
		return nil, false, err
	}

	notice := mail.TeamNotice{OwnerName: owner.Name, MemberName: member.Name, Role: member.Role}
	if created {
		token, ttl, err := s.accounts.IssueInvite(ctx, user)
		if err != nil {
			s.log.Error("pessoa na equipe, mas o convite não foi gerado", "user", user.ID, "err", err)
		} else {
			s.notify("convite da equipe", func(ctx context.Context) error {
				return s.notifier.MemberInvited(ctx, user.Email, notice, token, ttl)
			})
		}
	} else {
		s.notify("aviso da equipe", func(ctx context.Context) error {
			return s.notifier.MemberAdded(ctx, user.Email, notice)
		})
	}
	return member, created, nil
}

// memberAccount acha a conta de cliente com o e-mail — só se ela não tiver
// nada próprio (veículos, assinaturas, equipe) e não for de outra equipe —
// ou cria uma, sem senha conhecida (a pessoa cria pelo convite).
func (s *Service) memberAccount(ctx context.Context, in Input) (*auth.User, bool, error) {
	for attempt := 0; attempt < 2; attempt++ {
		user, err := s.accounts.FindByEmail(ctx, in.Email)
		switch {
		case err == nil:
			if user.Role != auth.RoleCustomer {
				return nil, false, &InputError{"esse e-mail não pode entrar na equipe de uma conta"}
			}
			var owns, member bool
			if err := s.db.QueryRow(ctx, `
				SELECT EXISTS (SELECT 1 FROM vehicles WHERE owner_id = $1)
					OR EXISTS (SELECT 1 FROM subscriptions WHERE customer_id = $1)
					OR EXISTS (SELECT 1 FROM account_members WHERE account_id = $1),
				EXISTS (SELECT 1 FROM account_members WHERE user_id = $1)`, user.ID).Scan(&owns, &member); err != nil {
				return nil, false, database.MapError(err)
			}
			switch {
			case member:
				return nil, false, &InputError{"essa pessoa já é da equipe de uma conta"}
			case owns:
				return nil, false, &InputError{"esse e-mail já é de um cliente com veículos próprios: use outro e-mail para a pessoa da equipe"}
			case !user.Active:
				// Saiu de uma equipe antes (a conta fica desativada): volta.
				if _, err := s.db.Exec(ctx, `UPDATE users SET active = TRUE, updated_at = NOW() WHERE id = $1`, user.ID); err != nil {
					return nil, false, database.MapError(err)
				}
				user.Active = true
				return user, true, nil
			}
			return user, false, nil
		case !errors.Is(err, database.ErrNotFound):
			return nil, false, err
		}

		password, err := auth.RandomPassword()
		if err != nil {
			return nil, false, err
		}
		user, err = s.accounts.Register(ctx, auth.NewUser{
			Email: in.Email, Name: in.Name, Role: auth.RoleCustomer, Password: password,
		})
		if err == nil {
			return user, true, nil
		}
		// Criada ao mesmo tempo por outro pedido: na volta, ela já existe.
		if !errors.Is(err, database.ErrConflict) {
			return nil, false, &InputError{err.Error()}
		}
	}
	return nil, false, database.ErrConflict
}

// SetRole troca o papel de alguém da equipe (vale na próxima requisição dela).
func (s *Service) SetRole(ctx context.Context, accountID, userID uuid.UUID, role string) (*Member, error) {
	if !auth.ValidMemberRole(role) {
		return nil, &InputError{"escolha o papel: Gestor ou Visualizador"}
	}
	tag, err := s.db.Exec(ctx, `
		UPDATE account_members SET role = $3, updated_at = NOW() WHERE account_id = $1 AND user_id = $2`,
		accountID, userID, role)
	if err != nil {
		return nil, database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return nil, database.ErrNotFound
	}
	return s.get(ctx, accountID, userID)
}

// SetGroups troca os veículos que a pessoa vê: com grupos, só os deles;
// sem nenhum, todos os da conta. Vale na próxima requisição dela.
func (s *Service) SetGroups(ctx context.Context, accountID, userID uuid.UUID, groups []uuid.UUID) (*Member, error) {
	groups, err := s.checkGroups(ctx, accountID, groups)
	if err != nil {
		return nil, err
	}
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE account_members SET restricted = $3, updated_at = NOW() WHERE account_id = $1 AND user_id = $2`,
			accountID, userID, len(groups) > 0)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return database.ErrNotFound
		}
		return setGroups(ctx, tx, userID, groups)
	})
	if err != nil {
		return nil, database.MapError(err)
	}
	return s.get(ctx, accountID, userID)
}

// checkGroups confere que os grupos são da conta e tira os repetidos.
func (s *Service) checkGroups(ctx context.Context, accountID uuid.UUID, groups []uuid.UUID) ([]uuid.UUID, error) {
	unique := make([]uuid.UUID, 0, len(groups))
	for _, g := range groups {
		if !slices.Contains(unique, g) {
			unique = append(unique, g)
		}
	}
	if len(unique) == 0 {
		return unique, nil
	}
	var found int
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM vehicle_groups WHERE owner_id = $1 AND id = ANY($2)`,
		accountID, unique).Scan(&found); err != nil {
		return nil, database.MapError(err)
	}
	if found != len(unique) {
		return nil, &InputError{"um dos grupos escolhidos não existe mais nesta conta"}
	}
	return unique, nil
}

// setGroups grava os grupos da pessoa (os anteriores saem).
func setGroups(ctx context.Context, tx pgx.Tx, userID uuid.UUID, groups []uuid.UUID) error {
	if _, err := tx.Exec(ctx, `DELETE FROM account_member_groups WHERE user_id = $1`, userID); err != nil {
		return err
	}
	if len(groups) == 0 {
		return nil
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO account_member_groups (user_id, group_id) SELECT $1, g FROM unnest($2::uuid[]) AS g`, userID, groups)
	return err
}

// Limit é o tamanho máximo da equipe da conta (cresce com a frota) e
// quantas pessoas ela tem.
func (s *Service) Limit(ctx context.Context, accountID uuid.UUID) (limit, used int, err error) {
	var vehicles int
	err = s.db.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM subscriptions WHERE customer_id = $1 AND status = 'ACTIVE'),
			(SELECT count(*) FROM account_members WHERE account_id = $1)`, accountID).Scan(&vehicles, &used)
	return LimitFor(vehicles), used, database.MapError(err)
}

// Remove tira a pessoa da equipe: o acesso acaba na hora (a consulta do
// vínculo é a cada requisição) e as sessões dela são encerradas. Se ela não
// acompanha nenhum veículo compartilhado, a conta é desativada (o login
// existia só para a equipe); dá para chamá-la de volta depois.
func (s *Service) Remove(ctx context.Context, accountID, userID uuid.UUID) (*Member, error) {
	member, err := s.get(ctx, accountID, userID)
	if err != nil {
		return nil, err
	}
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM account_members WHERE account_id = $1 AND user_id = $2`, accountID, userID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return database.ErrNotFound
		}
		_, err = tx.Exec(ctx, `
			UPDATE users SET active = FALSE, updated_at = NOW()
			WHERE id = $1 AND role = 'customer'
				AND NOT EXISTS (SELECT 1 FROM vehicle_shares WHERE guest_id = $1)
				AND NOT EXISTS (SELECT 1 FROM vehicles WHERE owner_id = $1)
				AND NOT EXISTS (SELECT 1 FROM subscriptions WHERE customer_id = $1)`, userID)
		return err
	})
	if err != nil {
		return nil, database.MapError(err)
	}
	if err := s.accounts.RevokeSessions(ctx, userID); err != nil {
		s.log.Error("pessoa saiu da equipe, mas as sessões não foram encerradas", "user", userID, "err", err)
	}
	return member, nil
}

// notify manda o e-mail fora da requisição: a falha fica no log, sem
// desfazer o que já foi gravado.
func (s *Service) notify(what string, send func(ctx context.Context) error) {
	if s.notifier == nil {
		return
	}
	s.runAsync(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := send(ctx); err != nil {
			s.log.Error("e-mail da equipe não saiu", "what", what, "err", err)
		}
	})
}
