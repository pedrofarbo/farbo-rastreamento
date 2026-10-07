package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

var (
	ErrInvalidCredentials = errors.New("e-mail ou senha inválidos")
	ErrInvalidToken       = errors.New("token inválido ou expirado")
	ErrInactiveUser       = errors.New("usuário desativado")
)

const issuer = "tracker-platform"

// dummyHash é um bcrypt válido de uma senha que ninguém tem. Existe só para
// igualar o tempo de resposta do login quando o e-mail não existe.
const dummyHash = "$2a$12$TfwVGhu.uQbVoziiu/9kLu85osKWoyO8pkR1ho7GQ.nHGiwQ67idW"

type Claims struct {
	Role  string `json:"role"`
	Email string `json:"email"`
	jwt.RegisteredClaims
}

// Tokens é o par devolvido no login e no refresh.
type Tokens struct {
	AccessToken  string    `json:"accessToken"`
	RefreshToken string    `json:"refreshToken"`
	ExpiresAt    time.Time `json:"expiresAt"`
	User         *User     `json:"user"`
}

type Service struct {
	repo     *Repository
	users    userStore
	sessions sessionStore
	resets   resetStore
	notifier Notifier
	cfg      config.Auth
	log      *slog.Logger

	// keyID identifica o JWT_SECRET atual (ver signingKeyID). Cada refresh
	// token é gravado com ele e só é aceito enquanto o segredo for o mesmo.
	keyID string

	// runAsync dispara os e-mails fora da requisição; os testes trocam por
	// uma chamada síncrona.
	runAsync func(func())

	// guard decide se uma sessão pode renovar (a equipe sem a verificação
	// em duas etapas, não); nil libera todas.
	guard func(ctx context.Context, user *User) error
}

// SetSessionGuard liga a regra que decide se uma sessão pode renovar.
func (s *Service) SetSessionGuard(guard func(ctx context.Context, user *User) error) { s.guard = guard }

func NewService(repo *Repository, cfg config.Auth, notifier Notifier, log *slog.Logger) *Service {
	if cfg.BcryptCost < bcrypt.MinCost || cfg.BcryptCost > bcrypt.MaxCost {
		cfg.BcryptCost = bcrypt.DefaultCost
	}
	return &Service{
		repo:     repo,
		users:    repo,
		sessions: repo,
		resets:   repo,
		notifier: notifier,
		cfg:      cfg,
		log:      log.With("component", "auth"),
		keyID:    signingKeyID(cfg.JWTSecret),
		runAsync: func(f func()) { go f() },
	}
}

// userStore é o que o cadastro e o primeiro acesso precisam do banco. O
// *Repository implementa; os testes usam um dublê em memória.
type userStore interface {
	Count(ctx context.Context) (int, error)
	Create(ctx context.Context, u *User) error
}

// sessionStore guarda os refresh tokens (as sessões). O *Repository
// implementa; os testes usam um dublê em memória.
type sessionStore interface {
	GetByID(ctx context.Context, id uuid.UUID) (*User, error)
	StoreRefreshToken(ctx context.Context, userID uuid.UUID, tokenHash, keyID string, expiresAt time.Time, userAgent string) error
	ConsumeRefreshToken(ctx context.Context, tokenHash, keyID string) (*refreshRecord, error)
	RevokeRefreshTokensNotSignedBy(ctx context.Context, keyID string) (int64, error)
}

// signingKeyID identifica o JWT_SECRET sem revelá-lo: é um HMAC com o
// próprio segredo como chave, sobre um rótulo fixo — o mesmo que qualquer JWT
// emitido já é. Quem lê o banco não ganha nada que um token não dê.
func signingKeyID(secret []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte("farbo:refresh-token-signing-key:v1"))
	return hex.EncodeToString(mac.Sum(nil))[:32]
}

// RevokeSessionsFromOldKeys encerra as sessões abertas com outro JWT_SECRET.
// Roda na subida, antes de a API atender.
//
// Os access tokens antigos param de valer sozinhos quando o segredo muda (a
// assinatura não confere mais). Os refresh tokens, não: são opacos e ficam no
// banco. Sem isto, quem roubou um refresh token continuaria renovando o acesso
// depois da troca do segredo feita justamente para cortá-lo. Refresh já recusa
// token de outra chave; aqui eles também saem como revogados no banco.
//
// Sessões de antes desta regra não têm chave gravada: não dá para saber com
// que segredo foram abertas, então também são encerradas (uma vez só, na
// primeira subida desta versão).
func (s *Service) RevokeSessionsFromOldKeys(ctx context.Context) error {
	revoked, err := s.sessions.RevokeRefreshTokensNotSignedBy(ctx, s.keyID)
	if err != nil {
		return fmt.Errorf("encerrando sessões de outro JWT_SECRET: %w", err)
	}
	if revoked > 0 {
		s.log.Warn("sessões abertas com outro JWT_SECRET foram encerradas; os usuários precisam entrar de novo",
			"sessoes", revoked)
	}
	return nil
}

// PasswordError explica por que uma senha foi recusada. A mensagem é para
// quem está digitando e pode ir direto para a tela.
type PasswordError struct{ Reason string }

func (e *PasswordError) Error() string { return e.Reason }

// ValidatePassword aplica a regra de senha do cadastro e da redefinição.
func ValidatePassword(password string) error {
	if len([]rune(password)) < 10 {
		return &PasswordError{Reason: "a senha precisa ter ao menos 10 caracteres"}
	}
	// O bcrypt só considera os primeiros 72 bytes; acima disso recusamos em
	// vez de ignorar o resto da senha em silêncio.
	if len(password) > 72 {
		return &PasswordError{Reason: "a senha pode ter no máximo 72 bytes (cerca de 72 letras sem acento)"}
	}
	return nil
}

func (s *Service) HashPassword(plain string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(plain), s.cfg.BcryptCost)
	return string(hash), err
}

// Login autentica e emite o par de tokens.
func (s *Service) Login(ctx context.Context, email, password, userAgent string) (*Tokens, error) {
	user, err := s.Authenticate(ctx, email, password)
	if err != nil {
		return nil, err
	}
	return s.issue(ctx, user, userAgent)
}

// Authenticate confere o e-mail e a senha, sem abrir a sessão (a
// verificação em duas etapas decide o que falta).
func (s *Service) Authenticate(ctx context.Context, email, password string) (*User, error) {
	user, err := s.repo.GetByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			// Compara contra um hash descartável para que a resposta demore o
			// mesmo com e sem usuário: sem isso dá para descobrir e-mails
			// cadastrados apenas medindo o tempo de resposta.
			_ = bcrypt.CompareHashAndPassword([]byte(dummyHash), []byte(password))
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}
	if !user.Active {
		return nil, ErrInactiveUser
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		return nil, ErrInvalidCredentials
	}
	return user, nil
}

// Refresh troca um refresh token válido por um novo par (rotação de token).
func (s *Service) Refresh(ctx context.Context, refreshToken, userAgent string) (*Tokens, error) {
	record, err := s.sessions.ConsumeRefreshToken(ctx, hashToken(refreshToken), s.keyID)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			return nil, ErrInvalidToken
		}
		return nil, err
	}

	user, err := s.sessions.GetByID(ctx, record.UserID)
	if err != nil {
		return nil, ErrInvalidToken
	}
	if !user.Active {
		return nil, ErrInactiveUser
	}
	if s.guard != nil {
		if err := s.guard(ctx, user); err != nil {
			s.log.Info("sessão não renovada", "user", user.ID, "motivo", err.Error())
			return nil, ErrInvalidToken
		}
	}
	return s.issue(ctx, user, userAgent)
}

func (s *Service) Logout(ctx context.Context, refreshToken string) error {
	if strings.TrimSpace(refreshToken) == "" {
		return nil
	}
	return s.repo.RevokeRefreshToken(ctx, hashToken(refreshToken))
}

func (s *Service) issue(ctx context.Context, user *User, userAgent string) (*Tokens, error) {
	expiresAt := time.Now().Add(s.cfg.AccessTokenTTL)

	claims := Claims{
		Role:  user.Role,
		Email: user.Email,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   user.ID.String(),
			Issuer:    issuer,
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			ID:        uuid.NewString(),
		},
	}
	access, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.cfg.JWTSecret)
	if err != nil {
		return nil, fmt.Errorf("assinando token: %w", err)
	}

	refresh, err := newOpaqueToken()
	if err != nil {
		return nil, err
	}
	if err := s.sessions.StoreRefreshToken(ctx, user.ID, hashToken(refresh), s.keyID,
		time.Now().Add(s.cfg.RefreshTokenTTL), userAgent); err != nil {
		return nil, err
	}

	return &Tokens{
		AccessToken:  access,
		RefreshToken: refresh,
		ExpiresAt:    expiresAt,
		User:         user,
	}, nil
}

// Parse valida o access token.
func (s *Service) Parse(token string) (*Claims, error) {
	parsed, err := jwt.ParseWithClaims(token, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("algoritmo de assinatura inesperado: %v", t.Header["alg"])
		}
		return s.cfg.JWTSecret, nil
	}, jwt.WithIssuer(issuer), jwt.WithValidMethods([]string{"HS256"}))
	if err != nil || !parsed.Valid {
		return nil, ErrInvalidToken
	}
	claims, ok := parsed.Claims.(*Claims)
	if !ok {
		return nil, ErrInvalidToken
	}
	return claims, nil
}

func (s *Service) GetUser(ctx context.Context, id uuid.UUID) (*User, error) {
	return s.repo.GetByID(ctx, id)
}

// FindByEmail acha o usuário pelo e-mail (sem diferença de maiúsculas).
func (s *Service) FindByEmail(ctx context.Context, email string) (*User, error) {
	return s.repo.GetByEmail(ctx, email)
}

// ListTeam lista a equipe da central (os clientes ficam em Clientes).
func (s *Service) ListTeam(ctx context.Context) ([]*User, error) { return s.repo.ListTeam(ctx) }

// LoginVerified abre a sessão de quem já provou quem é por outro meio — a
// biometria do aparelho, conferida pelo pacote stepup.
func (s *Service) LoginVerified(ctx context.Context, id uuid.UUID, userAgent string) (*Tokens, error) {
	user, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if !user.Active {
		return nil, ErrInactiveUser
	}
	return s.issue(ctx, user, userAgent)
}

// PasswordMatches confere a senha de um usuário ativo já logado: é a
// confirmação de ações sensíveis (desligar o motor, cadastrar biometria).
func (s *Service) PasswordMatches(ctx context.Context, id uuid.UUID, password string) (bool, error) {
	user, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return false, err
	}
	if !user.Active {
		return false, ErrInactiveUser
	}
	return bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) == nil, nil
}

// NewUser é o cadastro completo de um usuário.
type NewUser struct {
	Email    string
	Name     string
	Role     string
	Password string
	Phone    string
	Document string
}

// Profile são os dados editáveis de um usuário já cadastrado.
type Profile struct {
	Name     string
	Phone    string
	Document string
	Active   bool
}

// CreateUser cadastra um usuário já com a senha cifrada.
func (s *Service) CreateUser(ctx context.Context, email, name, role, password string) (*User, error) {
	return s.Register(ctx, NewUser{Email: email, Name: name, Role: role, Password: password})
}

// Register cadastra um usuário com todos os dados de perfil.
func (s *Service) Register(ctx context.Context, in NewUser) (*User, error) {
	email := strings.TrimSpace(strings.ToLower(in.Email))
	name, role, password := strings.TrimSpace(in.Name), in.Role, in.Password
	if !strings.Contains(email, "@") {
		return nil, fmt.Errorf("e-mail inválido")
	}
	if !ValidRole(role) {
		return nil, fmt.Errorf("perfil inválido: %q", role)
	}
	if err := ValidatePassword(password); err != nil {
		return nil, err
	}

	hash, err := s.HashPassword(password)
	if err != nil {
		return nil, err
	}
	user := &User{
		Email: email, Name: name, Role: role, PasswordHash: hash, Active: true,
		Phone: strings.TrimSpace(in.Phone), Document: strings.TrimSpace(in.Document),
	}
	if err := s.users.Create(ctx, user); err != nil {
		return nil, err
	}
	return user, nil
}

// UpdateProfile altera nome, contato e situação de um usuário.
func (s *Service) UpdateProfile(ctx context.Context, id uuid.UUID, p Profile) (*User, error) {
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" {
		return nil, fmt.Errorf("o nome é obrigatório")
	}
	p.Phone, p.Document = strings.TrimSpace(p.Phone), strings.TrimSpace(p.Document)
	return s.repo.UpdateProfile(ctx, id, p)
}

// UpdateContact é o próprio cliente atualizando nome, telefone e CPF/CNPJ
// (já validados por quem chama).
func (s *Service) UpdateContact(ctx context.Context, id uuid.UUID, name, phone, document string) (*User, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, &InputError{"o nome é obrigatório"}
	}
	return s.repo.UpdateContact(ctx, id, name, strings.TrimSpace(phone), strings.TrimSpace(document))
}

// InputError é um dado inválido que quem pediu consegue corrigir.
type InputError struct{ Reason string }

func (e *InputError) Error() string { return e.Reason }

// UpdateMember altera nome, perfil e situação de alguém da equipe (ver
// Repository.UpdateMember).
func (s *Service) UpdateMember(ctx context.Context, actor, id uuid.UUID, m Member) (before, after *User, err error) {
	m.Name = strings.TrimSpace(m.Name)
	if m.Name == "" {
		return nil, nil, &InputError{"o nome é obrigatório"}
	}
	if !TeamRole(m.Role) {
		return nil, nil, &InputError{"escolha o perfil: administrador, operador ou visualização"}
	}
	return s.repo.UpdateMember(ctx, actor, id, m)
}

// RandomPassword gera uma senha que ninguém conhece, para contas que vão
// definir a senha pelo link de convite.
func RandomPassword() (string, error) { return newOpaqueToken() }

// EnsureBootstrapUser cria o primeiro administrador quando o banco está vazio.
//
// Nunca com o e-mail ou a senha de exemplo, em nenhum ambiente: a senha do
// .env.example é pública, e o primeiro administrador criado com ela seria de
// quem a lesse primeiro. Nesse caso a subida falha sem criar ninguém. (Fora
// do desenvolvimento config.Load já recusa esses valores antes disto.)
func (s *Service) EnsureBootstrapUser(ctx context.Context, cfg config.Bootstrap) error {
	count, err := s.users.Count(ctx)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	if cfg.AdminEmail == "" || cfg.AdminPassword == "" {
		s.log.Warn("nenhum usuário cadastrado e ADMIN_EMAIL/ADMIN_PASSWORD não definidos; " +
			"defina as variáveis para criar o primeiro acesso")
		return nil
	}
	if config.IsPlaceholder(cfg.AdminPassword) {
		return fmt.Errorf("ADMIN_PASSWORD é a senha de exemplo ou um padrão conhecido: o administrador " +
			"inicial não foi criado; defina uma senha própria e suba de novo")
	}
	if config.IsPlaceholder(cfg.AdminEmail) {
		return fmt.Errorf("ADMIN_EMAIL é o e-mail de exemplo: o administrador inicial não foi criado; " +
			"use o e-mail real de quem administra e suba de novo")
	}

	user, err := s.CreateUser(ctx, cfg.AdminEmail, cfg.AdminName, RoleAdmin, cfg.AdminPassword)
	if errors.Is(err, database.ErrConflict) {
		// Outra instância, subindo junto, criou o mesmo administrador entre
		// a contagem e o cadastro.
		s.log.Info("usuário administrador inicial já criado por outra instância")
		return nil
	}
	if err != nil {
		return fmt.Errorf("criando usuário inicial: %w", err)
	}
	s.log.Info("usuário administrador inicial criado", "email", user.Email)
	return nil
}

// CleanupExpiredTokens roda periodicamente.
func (s *Service) CleanupExpiredTokens(ctx context.Context) {
	if removed, err := s.repo.DeleteExpiredRefreshTokens(ctx); err != nil {
		s.log.Warn("falha ao limpar refresh tokens", "err", err)
	} else if removed > 0 {
		s.log.Info("refresh tokens antigos removidos", "count", removed)
	}
	if removed, err := s.repo.DeleteStalePasswordResets(ctx); err != nil {
		s.log.Warn("falha ao limpar pedidos de redefinição de senha", "err", err)
	} else if removed > 0 {
		s.log.Info("pedidos de redefinição de senha vencidos removidos", "count", removed)
	}
}

func newOpaqueToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("gerando token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
