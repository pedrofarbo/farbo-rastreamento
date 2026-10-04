package auth

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// ---------------------------------------------------------------------------
// Dublês
// ---------------------------------------------------------------------------

type resetRecord struct {
	userID    uuid.UUID
	expiresAt time.Time
	usedAt    *time.Time
	createdAt time.Time
}

// memoryResetStore imita as regras do Repository: um pedido novo apaga os
// pendentes, o token vale uma vez e a troca revoga as sessões.
type memoryResetStore struct {
	mu       sync.Mutex
	users    map[uuid.UUID]*User
	resets   map[string]*resetRecord
	sessions map[uuid.UUID]int // refresh tokens ativos por usuário
}

func newMemoryResetStore(users ...*User) *memoryResetStore {
	store := &memoryResetStore{
		users:    map[uuid.UUID]*User{},
		resets:   map[string]*resetRecord{},
		sessions: map[uuid.UUID]int{},
	}
	for _, u := range users {
		store.users[u.ID] = u
	}
	return store
}

func (m *memoryResetStore) GetByEmail(_ context.Context, email string) (*User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, u := range m.users {
		if strings.EqualFold(u.Email, email) {
			copied := *u
			return &copied, nil
		}
	}
	return nil, database.ErrNotFound
}

func (m *memoryResetStore) GetByID(_ context.Context, id uuid.UUID) (*User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[id]
	if !ok {
		return nil, database.ErrNotFound
	}
	copied := *u
	return &copied, nil
}

func (m *memoryResetStore) HasRecentPasswordReset(_ context.Context, userID uuid.UUID, within time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.resets {
		if r.userID == userID && time.Since(r.createdAt) < within {
			return true, nil
		}
	}
	return false, nil
}

func (m *memoryResetStore) CreatePasswordReset(_ context.Context, userID uuid.UUID, tokenHash string, expiresAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for hash, r := range m.resets {
		if r.userID == userID && r.usedAt == nil {
			delete(m.resets, hash)
		}
	}
	m.resets[tokenHash] = &resetRecord{userID: userID, expiresAt: expiresAt, createdAt: time.Now()}
	return nil
}

func (m *memoryResetStore) usable(tokenHash string) (*resetRecord, bool) {
	r, ok := m.resets[tokenHash]
	if !ok || r.usedAt != nil || time.Now().After(r.expiresAt) || !m.users[r.userID].Active {
		return nil, false
	}
	return r, true
}

func (m *memoryResetStore) PasswordResetUser(_ context.Context, tokenHash string) (uuid.UUID, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.usable(tokenHash)
	if !ok {
		return uuid.Nil, database.ErrNotFound
	}
	return r.userID, nil
}

func (m *memoryResetStore) ResetPassword(_ context.Context, tokenHash, passwordHash string) (uuid.UUID, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.usable(tokenHash)
	if !ok {
		return uuid.Nil, database.ErrNotFound
	}
	now := time.Now()
	r.usedAt = &now
	m.users[r.userID].PasswordHash = passwordHash
	m.sessions[r.userID] = 0
	return r.userID, nil
}

// expire força o vencimento de todos os pedidos, como se o tempo tivesse passado.
func (m *memoryResetStore) expire() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.resets {
		r.expiresAt = time.Now().Add(-time.Second)
		r.createdAt = time.Now().Add(-time.Hour)
	}
}

type sentReset struct {
	to, name, token string
	ttl             time.Duration
}

type recordingNotifier struct {
	mu      sync.Mutex
	resets  []sentReset
	invites []sentReset
	changed []string
}

func (n *recordingNotifier) PasswordReset(_ context.Context, to, name, token string, ttl time.Duration) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.resets = append(n.resets, sentReset{to: to, name: name, token: token, ttl: ttl})
	return nil
}

func (n *recordingNotifier) PasswordChanged(_ context.Context, to, _ string, _ time.Time) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.changed = append(n.changed, to)
	return nil
}

func (n *recordingNotifier) Invite(_ context.Context, to, name, _, token string, ttl time.Duration) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.invites = append(n.invites, sentReset{to: to, name: name, token: token, ttl: ttl})
	return nil
}

func (n *recordingNotifier) lastToken(t *testing.T) string {
	t.Helper()
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.resets) == 0 {
		t.Fatal("nenhum e-mail de redefinição foi enviado")
	}
	return n.resets[len(n.resets)-1].token
}

func newResetFixture(t *testing.T, users ...*User) (*Service, *memoryResetStore, *recordingNotifier) {
	t.Helper()
	store := newMemoryResetStore(users...)
	notifier := &recordingNotifier{}
	cfg := config.Auth{BcryptCost: bcrypt.MinCost, PasswordResetTTL: time.Hour, InviteTTL: 72 * time.Hour}
	svc := NewService(nil, cfg, notifier, slog.New(slog.NewTextHandler(io.Discard, nil)))
	svc.resets = store
	svc.runAsync = func(f func()) { f() }
	return svc, store, notifier
}

func activeUser(email string) *User {
	return &User{ID: uuid.New(), Email: email, Name: "Maria Silva", Role: RoleViewer, Active: true}
}

// ---------------------------------------------------------------------------
// Testes
// ---------------------------------------------------------------------------

func TestRequestPasswordResetSendsLinkForActiveUser(t *testing.T) {
	user := activeUser("maria@exemplo.com")
	svc, store, notifier := newResetFixture(t, user)

	got, outcome, err := svc.RequestPasswordReset(context.Background(), "  Maria@Exemplo.com ")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if outcome != ResetSent || got == nil || got.ID != user.ID {
		t.Fatalf("desfecho = %q, usuário = %v; esperado SENT para %s", outcome, got, user.ID)
	}
	if len(notifier.resets) != 1 {
		t.Fatalf("e-mails enviados = %d, esperado 1", len(notifier.resets))
	}
	sent := notifier.resets[0]
	if sent.to != user.Email || sent.ttl != time.Hour {
		t.Fatalf("e-mail para %q com validade %s", sent.to, sent.ttl)
	}
	// Só o hash vai para o banco; o token em claro fica apenas no e-mail.
	if _, stored := store.resets[sent.token]; stored {
		t.Fatal("o token em claro foi guardado no banco")
	}
	if _, stored := store.resets[hashToken(sent.token)]; !stored {
		t.Fatal("o hash do token não foi guardado")
	}
}

func TestRequestPasswordResetSilentForUnknownOrInactive(t *testing.T) {
	inactive := activeUser("inativo@exemplo.com")
	inactive.Active = false
	svc, store, notifier := newResetFixture(t, inactive)

	user, outcome, err := svc.RequestPasswordReset(context.Background(), "ninguem@exemplo.com")
	if err != nil || user != nil || outcome != ResetUnknownEmail {
		t.Fatalf("e-mail sem conta: usuário=%v desfecho=%q err=%v", user, outcome, err)
	}

	_, outcome, err = svc.RequestPasswordReset(context.Background(), inactive.Email)
	if err != nil || outcome != ResetInactiveUser {
		t.Fatalf("usuário inativo: desfecho=%q err=%v", outcome, err)
	}

	if len(notifier.resets) != 0 || len(store.resets) != 0 {
		t.Fatalf("nada deveria ser enviado nem gravado: e-mails=%d tokens=%d",
			len(notifier.resets), len(store.resets))
	}
}

func TestRequestPasswordResetThrottlesRepeatedRequests(t *testing.T) {
	user := activeUser("maria@exemplo.com")
	svc, _, notifier := newResetFixture(t, user)

	if _, outcome, _ := svc.RequestPasswordReset(context.Background(), user.Email); outcome != ResetSent {
		t.Fatalf("primeiro pedido: %q", outcome)
	}
	if _, outcome, _ := svc.RequestPasswordReset(context.Background(), user.Email); outcome != ResetThrottled {
		t.Fatalf("segundo pedido imediato: %q, esperado THROTTLED", outcome)
	}
	if len(notifier.resets) != 1 {
		t.Fatalf("e-mails enviados = %d, esperado 1", len(notifier.resets))
	}
}

func TestNewRequestInvalidatesPreviousLink(t *testing.T) {
	user := activeUser("maria@exemplo.com")
	svc, store, notifier := newResetFixture(t, user)
	ctx := context.Background()

	svc.RequestPasswordReset(ctx, user.Email)
	first := notifier.lastToken(t)

	// Passado o intervalo mínimo, um pedido novo substitui o anterior.
	for _, r := range store.resets {
		r.createdAt = time.Now().Add(-2 * resetCooldown)
	}
	svc.RequestPasswordReset(ctx, user.Email)
	second := notifier.lastToken(t)

	if err := svc.CheckResetToken(ctx, first); !errors.Is(err, ErrInvalidResetToken) {
		t.Fatalf("link antigo deveria ter sido invalidado; err=%v", err)
	}
	if err := svc.CheckResetToken(ctx, second); err != nil {
		t.Fatalf("link novo deveria valer: %v", err)
	}
}

func TestResetPasswordChangesPasswordOnceAndRevokesSessions(t *testing.T) {
	user := activeUser("maria@exemplo.com")
	svc, store, notifier := newResetFixture(t, user)
	store.sessions[user.ID] = 3
	ctx := context.Background()

	svc.RequestPasswordReset(ctx, user.Email)
	token := notifier.lastToken(t)

	if err := svc.CheckResetToken(ctx, token); err != nil {
		t.Fatalf("token recém-emitido deveria ser válido: %v", err)
	}

	got, err := svc.ResetPassword(ctx, token, "nova-senha-segura")
	if err != nil {
		t.Fatalf("redefinição falhou: %v", err)
	}
	if got.ID != user.ID {
		t.Fatalf("usuário devolvido = %s, esperado %s", got.ID, user.ID)
	}
	if bcrypt.CompareHashAndPassword([]byte(store.users[user.ID].PasswordHash), []byte("nova-senha-segura")) != nil {
		t.Fatal("a senha nova não foi gravada")
	}
	if store.sessions[user.ID] != 0 {
		t.Fatal("as sessões abertas deveriam ter sido revogadas")
	}
	if len(notifier.changed) != 1 || notifier.changed[0] != user.Email {
		t.Fatalf("aviso de senha alterada = %v", notifier.changed)
	}

	// O mesmo link não serve de novo.
	if _, err := svc.ResetPassword(ctx, token, "outra-senha-segura"); !errors.Is(err, ErrInvalidResetToken) {
		t.Fatalf("reuso do token: err=%v, esperado ErrInvalidResetToken", err)
	}
}

func TestResetPasswordRejectsExpiredAndUnknownTokens(t *testing.T) {
	user := activeUser("maria@exemplo.com")
	svc, store, notifier := newResetFixture(t, user)
	ctx := context.Background()

	if _, err := svc.ResetPassword(ctx, "token-que-nao-existe", "nova-senha-segura"); !errors.Is(err, ErrInvalidResetToken) {
		t.Fatalf("token desconhecido: err=%v", err)
	}
	if _, err := svc.ResetPassword(ctx, "   ", "nova-senha-segura"); !errors.Is(err, ErrInvalidResetToken) {
		t.Fatalf("token vazio: err=%v", err)
	}

	svc.RequestPasswordReset(ctx, user.Email)
	token := notifier.lastToken(t)
	store.expire()

	if err := svc.CheckResetToken(ctx, token); !errors.Is(err, ErrInvalidResetToken) {
		t.Fatalf("token vencido na validação: err=%v", err)
	}
	if _, err := svc.ResetPassword(ctx, token, "nova-senha-segura"); !errors.Is(err, ErrInvalidResetToken) {
		t.Fatalf("token vencido na troca: err=%v", err)
	}
	if len(notifier.changed) != 0 {
		t.Fatal("não deveria haver aviso de senha alterada")
	}
}

func TestResetPasswordRejectsUserDeactivatedAfterRequest(t *testing.T) {
	user := activeUser("maria@exemplo.com")
	svc, store, notifier := newResetFixture(t, user)
	ctx := context.Background()

	svc.RequestPasswordReset(ctx, user.Email)
	token := notifier.lastToken(t)
	store.users[user.ID].Active = false

	if _, err := svc.ResetPassword(ctx, token, "nova-senha-segura"); !errors.Is(err, ErrInvalidResetToken) {
		t.Fatalf("usuário desativado: err=%v, esperado ErrInvalidResetToken", err)
	}
}

func TestResetPasswordValidatesNewPasswordFirst(t *testing.T) {
	user := activeUser("maria@exemplo.com")
	svc, _, notifier := newResetFixture(t, user)
	ctx := context.Background()

	svc.RequestPasswordReset(ctx, user.Email)
	token := notifier.lastToken(t)

	var weak *PasswordError
	if _, err := svc.ResetPassword(ctx, token, "curta"); !errors.As(err, &weak) {
		t.Fatalf("senha curta: err=%v, esperado PasswordError", err)
	}
	// Senha recusada não gasta o link.
	if err := svc.CheckResetToken(ctx, token); err != nil {
		t.Fatalf("o link deveria continuar valendo após senha recusada: %v", err)
	}
}

func TestValidatePassword(t *testing.T) {
	cases := []struct {
		name     string
		password string
		ok       bool
	}{
		{"curta", "123456789", false},
		{"mínimo", "1234567890", true},
		{"acentos contam como letra", "çãõéíóúâêô", true},
		{"72 bytes", strings.Repeat("a", 72), true},
		{"acima de 72 bytes", strings.Repeat("a", 73), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidatePassword(tc.password)
			if (err == nil) != tc.ok {
				t.Fatalf("ValidatePassword(%q) = %v, esperado ok=%v", tc.password, err, tc.ok)
			}
		})
	}
}

func TestInviteUserSendsLongerLivedLink(t *testing.T) {
	user := activeUser("cliente@exemplo.com")
	svc, _, notifier := newResetFixture(t, user)
	ctx := context.Background()

	if err := svc.InviteUser(ctx, user); err != nil {
		t.Fatalf("convite falhou: %v", err)
	}
	if len(notifier.invites) != 1 || notifier.invites[0].ttl != 72*time.Hour {
		t.Fatalf("convites = %+v, esperado 1 com validade de 72h", notifier.invites)
	}
	token := notifier.invites[0].token

	// O link do convite funciona como o da redefinição: define a senha uma vez.
	if _, err := svc.ResetPassword(ctx, token, "senha-escolhida-pelo-cliente"); err != nil {
		t.Fatalf("definir senha pelo convite: %v", err)
	}
	if _, err := svc.ResetPassword(ctx, token, "outra-senha-qualquer"); !errors.Is(err, ErrInvalidResetToken) {
		t.Fatalf("convite reutilizado: err=%v", err)
	}
}

func TestInviteUserRefusesInactiveUser(t *testing.T) {
	user := activeUser("cliente@exemplo.com")
	user.Active = false
	svc, _, notifier := newResetFixture(t, user)

	if err := svc.InviteUser(context.Background(), user); !errors.Is(err, ErrInactiveUser) {
		t.Fatalf("err = %v, esperado ErrInactiveUser", err)
	}
	if len(notifier.invites) != 0 {
		t.Fatal("usuário inativo não deveria receber convite")
	}
}
