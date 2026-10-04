package auth

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// ErrInvalidResetToken cobre token inexistente, vencido, já usado ou de
// usuário desativado — de propósito sem distinguir os casos.
var ErrInvalidResetToken = errors.New("link de redefinição inválido ou expirado")

// Notifier envia os e-mails da conta. A implementação fica no pacote mail.
type Notifier interface {
	PasswordReset(ctx context.Context, to, name, token string, ttl time.Duration) error
	PasswordChanged(ctx context.Context, to, name string, at time.Time) error
	// Invite tem o perfil: o convite da equipe é outro que o do cliente.
	Invite(ctx context.Context, to, name, role, token string, ttl time.Duration) error
}

// ResetOutcome diz o que aconteceu com um pedido de redefinição. Serve só
// para a auditoria: a resposta HTTP é sempre a mesma, para não revelar quais
// e-mails têm conta.
type ResetOutcome string

const (
	ResetSent         ResetOutcome = "SENT"
	ResetUnknownEmail ResetOutcome = "UNKNOWN_EMAIL"
	ResetInactiveUser ResetOutcome = "INACTIVE_USER"
	ResetThrottled    ResetOutcome = "THROTTLED"
)

const (
	// resetCooldown é o intervalo mínimo entre dois e-mails para a mesma
	// conta, contra quem tenta lotar a caixa de alguém de pedidos. O limite
	// por IP fica na rota.
	resetCooldown = time.Minute
	// notifyTimeout limita cada envio de e-mail em segundo plano.
	notifyTimeout = 30 * time.Second
)

// resetStore é o que o fluxo de redefinição precisa do banco. O *Repository
// implementa; os testes usam um dublê em memória.
type resetStore interface {
	GetByEmail(ctx context.Context, email string) (*User, error)
	GetByID(ctx context.Context, id uuid.UUID) (*User, error)
	HasRecentPasswordReset(ctx context.Context, userID uuid.UUID, within time.Duration) (bool, error)
	CreatePasswordReset(ctx context.Context, userID uuid.UUID, tokenHash string, expiresAt time.Time) error
	PasswordResetUser(ctx context.Context, tokenHash string) (uuid.UUID, error)
	ResetPassword(ctx context.Context, tokenHash, passwordHash string) (uuid.UUID, error)
}

// RequestPasswordReset gera um token e envia o link por e-mail, se o e-mail
// for de uma conta ativa. Devolve o usuário (nil quando não há conta) e o
// desfecho, ambos só para auditoria.
//
// O e-mail sai em segundo plano: esperar o SMTP deixaria a resposta mais lenta
// justamente quando a conta existe, e a diferença de tempo revelaria isso.
func (s *Service) RequestPasswordReset(ctx context.Context, email string) (*User, ResetOutcome, error) {
	email = strings.TrimSpace(strings.ToLower(email))

	user, err := s.resets.GetByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			return nil, ResetUnknownEmail, nil
		}
		return nil, "", err
	}
	if !user.Active {
		return user, ResetInactiveUser, nil
	}

	recent, err := s.resets.HasRecentPasswordReset(ctx, user.ID, resetCooldown)
	if err != nil {
		return nil, "", err
	}
	if recent {
		return user, ResetThrottled, nil
	}

	token, err := newOpaqueToken()
	if err != nil {
		return nil, "", err
	}
	ttl := s.cfg.PasswordResetTTL
	if err := s.resets.CreatePasswordReset(ctx, user.ID, hashToken(token), time.Now().Add(ttl)); err != nil {
		return nil, "", err
	}

	s.notify("redefinição de senha", user.ID, func(ctx context.Context) error {
		return s.notifier.PasswordReset(ctx, user.Email, user.Name, token, ttl)
	})
	return user, ResetSent, nil
}

// InviteUser manda ao usuário recém-criado o link para ele criar a própria
// senha. Usa o mesmo mecanismo da redefinição, com validade mais longa, e
// invalida convites ou pedidos anteriores ainda não usados.
func (s *Service) InviteUser(ctx context.Context, user *User) error {
	token, ttl, err := s.IssueInvite(ctx, user)
	if err != nil {
		return err
	}
	s.notify("convite", user.ID, func(ctx context.Context) error {
		return s.notifier.Invite(ctx, user.Email, user.Name, user.Role, token, ttl)
	})
	return nil
}

// IssueInvite gera o link de convite (para criar a senha) sem mandar o
// e-mail: serve a quem manda um convite com outro texto (ex.: o acesso a um
// veículo compartilhado). Invalida convites e pedidos anteriores.
func (s *Service) IssueInvite(ctx context.Context, user *User) (token string, ttl time.Duration, err error) {
	if !user.Active {
		return "", 0, ErrInactiveUser
	}
	if token, err = newOpaqueToken(); err != nil {
		return "", 0, err
	}
	ttl = s.cfg.InviteTTL
	if err := s.resets.CreatePasswordReset(ctx, user.ID, hashToken(token), time.Now().Add(ttl)); err != nil {
		return "", 0, err
	}
	return token, ttl, nil
}

// CheckResetToken diz se o link ainda pode ser usado, sem consumi-lo.
func (s *Service) CheckResetToken(ctx context.Context, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return ErrInvalidResetToken
	}
	if _, err := s.resets.PasswordResetUser(ctx, hashToken(token)); err != nil {
		if errors.Is(err, database.ErrNotFound) {
			return ErrInvalidResetToken
		}
		return err
	}
	return nil
}

// ResetPassword troca a senha usando o token do e-mail. Na mesma operação o
// token é consumido e todas as sessões abertas do usuário são encerradas.
func (s *Service) ResetPassword(ctx context.Context, token, password string) (*User, error) {
	if err := ValidatePassword(password); err != nil {
		return nil, err
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, ErrInvalidResetToken
	}

	// O bcrypt vem antes de olhar o token: assim a resposta leva o mesmo
	// tempo com token válido ou não.
	hash, err := s.HashPassword(password)
	if err != nil {
		return nil, err
	}

	userID, err := s.resets.ResetPassword(ctx, hashToken(token), hash)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			return nil, ErrInvalidResetToken
		}
		return nil, err
	}

	user, err := s.resets.GetByID(ctx, userID)
	if err != nil {
		// A senha já foi trocada; só o e-mail de aviso fica sem destinatário.
		s.log.Error("senha redefinida, mas o usuário não foi relido para o aviso", "user", userID, "err", err)
		return &User{ID: userID}, nil
	}

	changedAt := time.Now()
	s.notify("aviso de senha alterada", user.ID, func(ctx context.Context) error {
		return s.notifier.PasswordChanged(ctx, user.Email, user.Name, changedAt)
	})
	return user, nil
}

// notify envia um e-mail em segundo plano, desligado do contexto da
// requisição (que acaba assim que a resposta sai).
func (s *Service) notify(kind string, userID uuid.UUID, send func(ctx context.Context) error) {
	if s.notifier == nil {
		s.log.Error("e-mail não enviado: nenhum Notifier configurado", "email", kind, "user", userID)
		return
	}
	s.runAsync(func() {
		ctx, cancel := context.WithTimeout(context.Background(), notifyTimeout)
		defer cancel()
		if err := send(ctx); err != nil {
			s.log.Error("falha ao enviar e-mail", "email", kind, "user", userID, "err", err)
		}
	})
}
