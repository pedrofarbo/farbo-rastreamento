package auth

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// As sessões abertas de cada pessoa: a lista dos aparelhos, sair dos outros
// (o app do cliente), desconectar todas e a senha nova pela central. Valem
// na hora: o token de acesso emitido antes do corte é recusado.

// cutoffTTL: por quanto tempo o middleware confia no corte que já leu.
const cutoffTTL = 10 * time.Second

// Session é um aparelho com a conta aberta.
type Session struct {
	ID        uuid.UUID `json:"id"`
	UserAgent string    `json:"userAgent"`
	// LastSeenAt é a última renovação da sessão (o app renova ao abrir e a
	// cada 15 minutos de uso).
	LastSeenAt time.Time `json:"lastSeenAt"`
	ExpiresAt  time.Time `json:"expiresAt"`
	// Current: é o aparelho de quem está vendo a lista.
	Current bool `json:"current"`
}

type cutoffEntry struct {
	at     *time.Time
	loaded time.Time
}

// issuedAfterCut diz se o token foi emitido depois do último corte das
// sessões do usuário (sem corte, vale).
func (s *Service) issuedAfterCut(ctx context.Context, userID uuid.UUID, claims *Claims) (bool, error) {
	if s.sessions == nil {
		return true, nil
	}
	var cut *time.Time
	if v, ok := s.cutoffs.Load(userID); ok && time.Since(v.(cutoffEntry).loaded) < cutoffTTL {
		cut = v.(cutoffEntry).at
	} else {
		at, err := s.sessions.SessionsValidAfter(ctx, userID)
		if err != nil && !errors.Is(err, database.ErrNotFound) {
			return false, err
		}
		cut = at
		s.cutoffs.Store(userID, cutoffEntry{at: at, loaded: time.Now()})
	}
	if cut == nil {
		return true, nil
	}
	return claims.IssuedAt != nil && !claims.IssuedAt.Time.Before(*cut), nil
}

// cut encerra as sessões (menos keep) e já passa a recusar os tokens antigos
// nesta instância.
func (s *Service) cut(ctx context.Context, userID uuid.UUID, keep *uuid.UUID) (int64, error) {
	at, revoked, err := s.sessions.CutSessions(ctx, userID, keep)
	if err != nil {
		return 0, err
	}
	s.cutoffs.Store(userID, cutoffEntry{at: &at, loaded: time.Now()})
	return revoked, nil
}

// Sessions lista os aparelhos com a conta do usuário aberta, marcando o
// atual (current: a sessão do token de quem pede; zero para a central).
func (s *Service) Sessions(ctx context.Context, userID, current uuid.UUID) ([]Session, error) {
	list, err := s.sessions.ActiveSessions(ctx, userID, s.keyID)
	if err != nil {
		return nil, err
	}
	for i := range list {
		list[i].Current = current != uuid.Nil && list[i].ID == current
	}
	return list, nil
}

// SignOutOthers encerra as outras sessões do usuário e devolve um token de
// acesso novo para a atual (o antigo é de antes do corte). Devolve também
// quantas caíram.
func (s *Service) SignOutOthers(ctx context.Context, userID, current uuid.UUID) (*Tokens, int64, error) {
	if current == uuid.Nil {
		return nil, 0, ErrInvalidToken
	}
	user, err := s.sessions.GetByID(ctx, userID)
	if err != nil {
		return nil, 0, err
	}
	revoked, err := s.cut(ctx, userID, &current)
	if err != nil {
		return nil, 0, err
	}
	access, expiresAt, err := s.accessToken(user, current)
	if err != nil {
		return nil, 0, err
	}
	return &Tokens{AccessToken: access, ExpiresAt: expiresAt, User: user}, revoked, nil
}

// SignOutAll encerra todas as sessões do usuário (a central, na conta de um
// cliente). Devolve quantas caíram.
func (s *Service) SignOutAll(ctx context.Context, userID uuid.UUID) (int64, error) {
	return s.cut(ctx, userID, nil)
}

// SetPassword é a senha nova definida pela central: grava, encerra todas as
// sessões (quem estava logado, com a senha antiga ou não, sai) e avisa a
// pessoa por e-mail.
func (s *Service) SetPassword(ctx context.Context, userID uuid.UUID, password string) (*User, error) {
	if err := ValidatePassword(password); err != nil {
		return nil, err
	}
	user, err := s.sessions.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if !user.Active {
		return nil, ErrInactiveUser
	}
	hash, err := s.HashPassword(password)
	if err != nil {
		return nil, err
	}
	if err := s.repo.SetPassword(ctx, userID, hash); err != nil {
		return nil, err
	}
	if _, err := s.cut(ctx, userID, nil); err != nil {
		return nil, err
	}
	changedAt := time.Now()
	s.notify("aviso de senha alterada", user.ID, func(ctx context.Context) error {
		return s.notifier.PasswordChanged(ctx, user.Email, user.Name, changedAt)
	})
	return user, nil
}
