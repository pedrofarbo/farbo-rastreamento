package auth

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// memorySessionStore imita as regras do Repository para usuários e refresh
// tokens: o token vale uma vez, só com a mesma chave e dentro da validade.
type memorySessionStore struct {
	mu       sync.Mutex
	users    map[uuid.UUID]*User
	sessions map[string]*memorySession
	cutoffs  map[uuid.UUID]time.Time
	created  int
}

type memorySession struct {
	id        uuid.UUID
	userID    uuid.UUID
	keyID     string
	expiresAt time.Time
	revoked   bool
	userAgent string
}

func newMemorySessionStore(users ...*User) *memorySessionStore {
	m := &memorySessionStore{users: map[uuid.UUID]*User{}, sessions: map[string]*memorySession{}, cutoffs: map[uuid.UUID]time.Time{}}
	for _, u := range users {
		m.users[u.ID] = u
	}
	return m
}

func (m *memorySessionStore) Count(context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.users), nil
}

func (m *memorySessionStore) Create(_ context.Context, u *User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u.ID = uuid.New()
	m.users[u.ID] = u
	m.created++
	return nil
}

func (m *memorySessionStore) GetByID(_ context.Context, id uuid.UUID) (*User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if u, ok := m.users[id]; ok {
		return u, nil
	}
	return nil, database.ErrNotFound
}

func (m *memorySessionStore) StoreRefreshToken(_ context.Context, userID uuid.UUID, tokenHash, keyID string, expiresAt time.Time, userAgent string) (uuid.UUID, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := uuid.New()
	m.sessions[tokenHash] = &memorySession{id: id, userID: userID, keyID: keyID, expiresAt: expiresAt, userAgent: userAgent}
	return id, nil
}

func (m *memorySessionStore) ActiveSessions(_ context.Context, userID uuid.UUID, keyID string) ([]Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Session{}
	for _, s := range m.sessions {
		if s.userID == userID && s.keyID == keyID && !s.revoked && s.expiresAt.After(time.Now()) {
			out = append(out, Session{ID: s.id, UserAgent: s.userAgent, ExpiresAt: s.expiresAt})
		}
	}
	return out, nil
}

func (m *memorySessionStore) CutSessions(_ context.Context, userID uuid.UUID, keep *uuid.UUID) (time.Time, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var n int64
	for _, s := range m.sessions {
		if s.userID == userID && !s.revoked && (keep == nil || s.id != *keep) {
			s.revoked = true
			n++
		}
	}
	at := time.Now().Truncate(time.Second)
	m.cutoffs[userID] = at
	return at, n, nil
}

func (m *memorySessionStore) SessionsValidAfter(_ context.Context, userID uuid.UUID) (*time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if at, ok := m.cutoffs[userID]; ok {
		return &at, nil
	}
	return nil, nil
}

func (m *memorySessionStore) ConsumeRefreshToken(_ context.Context, tokenHash, keyID string) (*refreshRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[tokenHash]
	if !ok || s.keyID != keyID || s.revoked || !s.expiresAt.After(time.Now()) {
		return nil, database.ErrNotFound
	}
	s.revoked = true
	return &refreshRecord{ID: s.id, UserID: s.userID}, nil
}

func (m *memorySessionStore) RevokeRefreshTokensNotSignedBy(_ context.Context, keyID string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var n int64
	for _, s := range m.sessions {
		if !s.revoked && s.keyID != keyID {
			s.revoked = true
			n++
		}
	}
	return n, nil
}

func (m *memorySessionStore) active() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, s := range m.sessions {
		if !s.revoked {
			n++
		}
	}
	return n
}

func serviceWithSecret(t *testing.T, secret string, store *memorySessionStore) *Service {
	t.Helper()
	cfg := config.Auth{
		JWTSecret: []byte(secret), AccessTokenTTL: 15 * time.Minute, RefreshTokenTTL: time.Hour,
		BcryptCost: bcrypt.MinCost,
	}
	svc := NewService(nil, cfg, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	svc.users = store
	svc.sessions = store
	return svc
}

const (
	oldSecret = "2c0f7d3b9e8a4f61a5d0c7e2b9f4a8d13e6c5b7a9d2f0e4c"
	newSecret = "9a4e1f7c2b8d6e0a3f5c9b1d7e2a4c8f6b0d3e9a1c5f7b2d"
)

func adminUser() *User {
	return &User{ID: uuid.New(), Email: "operacao@farbo.com.br", Role: RoleAdmin, Active: true}
}

func TestAccessTokenSignedWithOldSecretIsRejectedAfterRotation(t *testing.T) {
	user := adminUser()
	store := newMemorySessionStore(user)
	before := serviceWithSecret(t, oldSecret, store)
	tokens, err := before.issue(context.Background(), user, "teste")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := before.Parse(tokens.AccessToken); err != nil {
		t.Fatalf("controle: com o mesmo segredo o token vale: %v", err)
	}

	after := serviceWithSecret(t, newSecret, store)
	if _, err := after.Parse(tokens.AccessToken); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("token assinado com o segredo antigo foi aceito depois da troca: %v", err)
	}

	// E pela porta de verdade: o middleware devolve 401 ao token antigo e
	// deixa passar o emitido com o segredo novo.
	protected := after.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	call := func(token string) int {
		req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		protected.ServeHTTP(rec, req)
		return rec.Code
	}
	if code := call(tokens.AccessToken); code != http.StatusUnauthorized {
		t.Errorf("token antigo: esperava 401, veio %d", code)
	}
	fresh, err := after.issue(context.Background(), user, "teste")
	if err != nil {
		t.Fatal(err)
	}
	if code := call(fresh.AccessToken); code != http.StatusNoContent {
		t.Errorf("token do segredo novo: esperava 204, veio %d", code)
	}
}

func TestTokenForgedWithPublicExampleSecretIsRejected(t *testing.T) {
	// O ataque da issue: assinar claims de administrador com o JWT_SECRET
	// que estava no .env.example. Com o segredo da instalação, não passa.
	forged, err := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{
		Role: RoleAdmin, Email: "invasor@exemplo.com",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject: uuid.NewString(), Issuer: issuer,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}).SignedString([]byte("troque-por-uma-string-aleatoria-de-no-minimo-32-caracteres"))
	if err != nil {
		t.Fatal(err)
	}
	svc := serviceWithSecret(t, newSecret, newMemorySessionStore())
	if _, err := svc.Parse(forged); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("token forjado com o segredo de exemplo foi aceito: %v", err)
	}
}

func TestRefreshTokenFromOldSecretIsRejectedAfterRotation(t *testing.T) {
	user := adminUser()
	store := newMemorySessionStore(user)
	before := serviceWithSecret(t, oldSecret, store)

	// Controle: sem troca de segredo, o refresh funciona (e roda o token).
	first, err := before.issue(context.Background(), user, "teste")
	if err != nil {
		t.Fatal(err)
	}
	stolen, err := before.Refresh(context.Background(), first.RefreshToken, "teste")
	if err != nil {
		t.Fatalf("controle: refresh com o mesmo segredo devia funcionar: %v", err)
	}

	after := serviceWithSecret(t, newSecret, store)
	if _, err := after.Refresh(context.Background(), stolen.RefreshToken, "teste"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("refresh token emitido com o segredo antigo foi aceito depois da troca: %v", err)
	}
}

func TestRevokeSessionsFromOldKeys(t *testing.T) {
	user := adminUser()
	store := newMemorySessionStore(user)
	before := serviceWithSecret(t, oldSecret, store)
	for i := 0; i < 3; i++ {
		if _, err := before.issue(context.Background(), user, "teste"); err != nil {
			t.Fatal(err)
		}
	}
	// Sessão de antes desta versão: sem chave gravada.
	_, _ = store.StoreRefreshToken(context.Background(), user.ID, "legado", "", time.Now().Add(time.Hour), "")

	after := serviceWithSecret(t, newSecret, store)
	kept, err := after.issue(context.Background(), user, "teste")
	if err != nil {
		t.Fatal(err)
	}
	if err := after.RevokeSessionsFromOldKeys(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := store.active(); n != 1 {
		t.Fatalf("só a sessão do segredo atual devia sobrar, sobraram %d", n)
	}
	if _, err := after.Refresh(context.Background(), kept.RefreshToken, "teste"); err != nil {
		t.Errorf("a sessão aberta com o segredo atual devia continuar valendo: %v", err)
	}

	// Subir de novo com o mesmo segredo não derruba ninguém.
	if err := after.RevokeSessionsFromOldKeys(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := store.active(); n != 1 {
		t.Errorf("reiniciar com o mesmo segredo derrubou sessões: %d ativas", n)
	}
}

func TestSigningKeyID(t *testing.T) {
	a, b := signingKeyID([]byte(oldSecret)), signingKeyID([]byte(newSecret))
	if a == b || a == "" {
		t.Fatalf("segredos diferentes precisam de identificadores diferentes: %q %q", a, b)
	}
	if a != signingKeyID([]byte(oldSecret)) {
		t.Error("o identificador precisa ser estável para o mesmo segredo")
	}
	if strings.Contains(oldSecret, a) || strings.Contains(a, oldSecret[:8]) {
		t.Error("o identificador não pode conter o segredo")
	}
}

func TestBootstrapNeverUsesExamplePassword(t *testing.T) {
	for _, tc := range []struct {
		name, email, password, want string
	}{
		{"senha do .env.example", "operacao@farbo.com.br", "uma-senha-com-10-ou-mais-caracteres", "ADMIN_PASSWORD"},
		{"senha de exemplo em maiúsculas", "operacao@farbo.com.br", "UMA_SENHA_COM_10_OU_MAIS_CARACTERES", "ADMIN_PASSWORD"},
		{"padrão óbvio", "operacao@farbo.com.br", "admin12345", "ADMIN_PASSWORD"},
		{"e-mail do .env.example", "voce@exemplo.com", "Kx9!vT2#pQ7&mW4z", "ADMIN_EMAIL"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newMemorySessionStore()
			svc := serviceWithSecret(t, newSecret, store)
			err := svc.EnsureBootstrapUser(context.Background(), config.Bootstrap{
				AdminEmail: tc.email, AdminPassword: tc.password, AdminName: "Administrador",
			})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("esperava recusa apontando %s, veio %v", tc.want, err)
			}
			if store.created != 0 {
				t.Fatalf("banco vazio ganhou %d usuário(s) com credencial de exemplo", store.created)
			}
		})
	}
}

func TestBootstrapWithOwnCredentials(t *testing.T) {
	store := newMemorySessionStore()
	svc := serviceWithSecret(t, newSecret, store)
	err := svc.EnsureBootstrapUser(context.Background(), config.Bootstrap{
		AdminEmail: "Operacao@Farbo.com.br", AdminPassword: "Kx9!vT2#pQ7&mW4z", AdminName: "Administrador",
	})
	if err != nil {
		t.Fatalf("credenciais próprias deviam criar o administrador: %v", err)
	}
	if store.created != 1 {
		t.Fatalf("esperava 1 usuário criado, veio %d", store.created)
	}
	for _, u := range store.users {
		if u.Role != RoleAdmin || u.Email != "operacao@farbo.com.br" {
			t.Errorf("usuário inicial errado: %+v", u)
		}
		if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte("Kx9!vT2#pQ7&mW4z")) != nil {
			t.Error("a senha gravada não confere")
		}
	}
}

func TestBootstrapIgnoresExampleWhenUsersExist(t *testing.T) {
	// Em desenvolvimento, com o banco já povoado, o exemplo esquecido no .env
	// não cria nada e não impede a subida.
	store := newMemorySessionStore(adminUser())
	svc := serviceWithSecret(t, newSecret, store)
	err := svc.EnsureBootstrapUser(context.Background(), config.Bootstrap{
		AdminEmail: "voce@exemplo.com", AdminPassword: "uma-senha-com-10-ou-mais-caracteres",
	})
	if err != nil || store.created != 0 {
		t.Fatalf("com usuários cadastrados não há bootstrap: err=%v criados=%d", err, store.created)
	}
}

// racingUsers simula duas instâncias subindo juntas: esta contou zero
// usuários, mas a outra criou o mesmo administrador antes do cadastro.
type racingUsers struct{ *memorySessionStore }

func (racingUsers) Count(context.Context) (int, error) { return 0, nil }

func (racingUsers) Create(context.Context, *User) error { return database.ErrConflict }

func TestBootstrapToleratesConcurrentInstance(t *testing.T) {
	store := newMemorySessionStore()
	svc := serviceWithSecret(t, newSecret, store)
	svc.users = racingUsers{store}
	err := svc.EnsureBootstrapUser(context.Background(), config.Bootstrap{
		AdminEmail: "operacao@farbo.com.br", AdminPassword: "Kx9!vT2#pQ7&mW4z", AdminName: "Administrador",
	})
	if err != nil {
		t.Fatalf("o administrador criado pela outra instância não pode derrubar esta: %v", err)
	}
}
