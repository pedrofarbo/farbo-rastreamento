package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/twofactor"
)

// fakeTwoFactorMailer guarda os códigos e os avisos mandados.
type fakeTwoFactorMailer struct {
	mu     sync.Mutex
	codes  map[string][]string // e-mail → códigos, na ordem
	events []string
}

func (m *fakeTwoFactorMailer) Code(_ context.Context, to, _, code string, _ time.Duration, purpose string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.codes == nil {
		m.codes = map[string][]string{}
	}
	m.codes[to] = append(m.codes[to], purpose+":"+code)
	return nil
}

func (m *fakeTwoFactorMailer) Changed(_ context.Context, to, _, event string, _ time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, to+":"+event)
	return nil
}

// last é o último código mandado para o e-mail (com o propósito).
func (m *fakeTwoFactorMailer) last(t *testing.T, email, purpose string) string {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	list := m.codes[email]
	if len(list) == 0 || !strings.HasPrefix(list[len(list)-1], purpose+":") {
		t.Fatalf("sem código %s para %s: %v", purpose, email, list)
	}
	return strings.TrimPrefix(list[len(list)-1], purpose+":")
}

// totpAt é o código do app autenticador num instante (como o celular faz).
func totpAt(t *testing.T, secret string, at time.Time) string {
	t.Helper()
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		t.Fatal(err)
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(at.Unix()/30))
	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	return fmt.Sprintf("%06d", (binary.BigEndian.Uint32(sum[offset:offset+4])&0x7fffffff)%1000000)
}

// A verificação em duas etapas de ponta a ponta: a equipe ativa no login
// (código do e-mail, depois o app) e não renova a sessão sem ela; o código
// do app não vale duas vezes; aparelho confiável; tentativas demais; código
// de recuperação (uso único, com aviso); o cliente ativa por e-mail, troca
// para o app e desativa; o admin redefine. Precisa de FARBO_TEST_DATABASE_URL.
func TestTwoFactorEndToEnd(t *testing.T) {
	mailer := &fakeTwoFactorMailer{}
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	var clockMu sync.Mutex
	clock := func() time.Time { clockMu.Lock(); defer clockMu.Unlock(); return now }
	advance := func(d time.Duration) { clockMu.Lock(); now = now.Add(d); clockMu.Unlock() }
	var tf *twofactor.Service
	var authSvc *auth.Service
	env, _ := newLeadsEnv(t, func(d *Deps) {
		var err error
		tf, err = twofactor.NewService(d.DB, d.Auth, mailer, d.Config.Auth.JWTSecret, slog.New(slog.NewTextHandler(io.Discard, nil)))
		if err != nil {
			t.Fatal(err)
		}
		tf.SetClock(clock)
		tf.SetSync()
		d.Auth.SetSessionGuard(tf.Guard)
		d.TwoFactor = tf
		authSvc = d.Auth
		// Cada pedido de um IP (o login tem limite por IP).
		d.Config.HTTP.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}
	})
	t.Cleanup(func() { trustedProxies = nil })
	ip := 0
	nextIP := func() map[string]string {
		ip++
		return map[string]string{"X-Forwarded-For": fmt.Sprintf("198.51.%d.%d", ip/250, ip%250+1)}
	}
	ctx := context.Background()
	adminEmail := auth.RoleAdmin + "@leads.test"

	type reply struct {
		AccessToken   string               `json:"accessToken"`
		RefreshToken  string               `json:"refreshToken"`
		DeviceToken   string               `json:"deviceToken"`
		RecoveryCodes []string             `json:"recoveryCodes"`
		TwoFactor     *twofactor.Challenge `json:"twoFactor"`
		Error         string               `json:"error"`
		Code          string               `json:"code"`
		Secret        string               `json:"secret"`
		OtpauthURL    string               `json:"otpauthUrl"`
		QRCode        string               `json:"qrCode"`
	}
	post := func(token, path string, body any, want int) reply {
		t.Helper()
		status, raw := env.doWith(token, http.MethodPost, path, body, nextIP())
		if status != want {
			t.Fatalf("POST %s: esperava %d, veio %d: %s", path, want, status, raw)
		}
		var out reply
		_ = json.Unmarshal(raw, &out)
		return out
	}
	login := func(email, device string) reply {
		t.Helper()
		return post("", "/api/auth/login", map[string]string{"email": email, "password": userPassword, "deviceToken": device}, http.StatusOK)
	}

	// 1. A equipe sem a verificação: o login pede a ativação, sem sessão.
	first := login(adminEmail, "")
	if first.AccessToken != "" || first.TwoFactor == nil || first.TwoFactor.Kind != twofactor.KindSetup ||
		first.TwoFactor.EmailHint != "a***@leads.test" {
		t.Fatalf("login da equipe sem a verificação = %+v", first)
	}
	ch := first.TwoFactor.Token
	if out := post("", "/api/auth/2fa/setup/confirm", map[string]any{"challenge": ch, "code": "123456"}, http.StatusBadRequest); !strings.Contains(out.Error, "Confirme primeiro") {
		t.Errorf("app antes do e-mail = %+v", out)
	}
	if out := post("", "/api/auth/2fa/setup/email", map[string]any{"challenge": ch, "code": "000000"}, http.StatusBadRequest); !strings.Contains(out.Error, "4 tentativas restantes") {
		t.Errorf("código do e-mail errado = %+v", out)
	}
	enroll := post("", "/api/auth/2fa/setup/email", map[string]any{"challenge": ch, "code": mailer.last(t, adminEmail, twofactor.PurposeSetup)}, http.StatusOK)
	if len(enroll.Secret) != 32 || !strings.HasPrefix(enroll.OtpauthURL, "otpauth://totp/") || !strings.HasPrefix(enroll.QRCode, "<svg") {
		t.Fatalf("cadastro no app = %+v", enroll)
	}
	secret := enroll.Secret
	if out := post("", "/api/auth/2fa/setup/confirm", map[string]any{"challenge": ch, "code": "111111"}, http.StatusBadRequest); !strings.Contains(out.Error, "Código incorreto") {
		t.Errorf("código do app errado = %+v", out)
	}
	usedCode := totpAt(t, secret, clock())
	done := post("", "/api/auth/2fa/setup/confirm", map[string]any{"challenge": ch, "code": usedCode, "trustDevice": true}, http.StatusOK)
	if done.AccessToken == "" || done.DeviceToken == "" || len(done.RecoveryCodes) != 10 {
		t.Fatalf("ativação = %+v", done)
	}
	// O desafio usado não serve de novo.
	if out := post("", "/api/auth/2fa/setup/confirm", map[string]any{"challenge": ch, "code": usedCode}, http.StatusGone); out.Code != codeTwoFactorExpired {
		t.Errorf("desafio reaproveitado = %+v", out)
	}
	adminToken := done.AccessToken

	// 2. Aparelho confiável: só a senha. Sem ele, o código do app — o já
	// usado não vale de novo.
	if trusted := login(adminEmail, done.DeviceToken); trusted.AccessToken == "" {
		t.Errorf("aparelho confiável pediu código: %+v", trusted)
	}
	verify := login(adminEmail, "")
	if verify.TwoFactor == nil || verify.TwoFactor.Kind != twofactor.KindVerify || verify.TwoFactor.Method != twofactor.MethodTOTP {
		t.Fatalf("login com a verificação = %+v", verify)
	}
	post("", "/api/auth/2fa/verify", map[string]any{"challenge": verify.TwoFactor.Token, "code": usedCode}, http.StatusBadRequest)
	advance(30 * time.Second)
	if ok := post("", "/api/auth/2fa/verify", map[string]any{"challenge": verify.TwoFactor.Token, "code": totpAt(t, secret, clock())}, http.StatusOK); ok.AccessToken == "" || ok.DeviceToken != "" {
		t.Errorf("código do app = %+v", ok)
	}

	// 3. Tentativas demais: o desafio acaba e a tela volta para a senha.
	brute := login(adminEmail, "").TwoFactor.Token
	for i := 0; i < 4; i++ {
		post("", "/api/auth/2fa/verify", map[string]any{"challenge": brute, "code": "999999"}, http.StatusBadRequest)
	}
	if out := post("", "/api/auth/2fa/verify", map[string]any{"challenge": brute, "code": "999999"}, http.StatusGone); out.Code != codeTwoFactorExpired {
		t.Errorf("quinta tentativa = %+v", out)
	}
	advance(30 * time.Second)
	post("", "/api/auth/2fa/verify", map[string]any{"challenge": brute, "code": totpAt(t, secret, clock())}, http.StatusGone)

	// 4. Código de recuperação: entra uma vez, com aviso por e-mail.
	recovery := done.RecoveryCodes[0]
	rec := login(adminEmail, "").TwoFactor.Token
	if out := post("", "/api/auth/2fa/verify", map[string]any{"challenge": rec, "code": " " + strings.ToUpper(recovery) + " "}, http.StatusOK); out.AccessToken == "" {
		t.Fatalf("código de recuperação = %+v", out)
	}
	again := login(adminEmail, "").TwoFactor.Token
	post("", "/api/auth/2fa/verify", map[string]any{"challenge": again, "code": recovery}, http.StatusBadRequest)
	if !strings.Contains(strings.Join(mailer.events, ","), adminEmail+":"+twofactor.EventRecoveryUsed) {
		t.Errorf("sem aviso do código de recuperação: %v", mailer.events)
	}

	// 5. A sessão da equipe sem a verificação não renova; a do cliente, sim.
	opTokens, err := authSvc.Login(ctx, auth.RoleOperator+"@leads.test", userPassword, "teste")
	if err != nil {
		t.Fatal(err)
	}
	post("", "/api/auth/refresh", map[string]string{"refreshToken": opTokens.RefreshToken}, http.StatusUnauthorized)
	if _, err := authSvc.CreateUser(ctx, "lia@duasetapas.test", "Lia", auth.RoleCustomer, userPassword); err != nil {
		t.Fatal(err)
	}
	liaEmail := "lia@duasetapas.test"
	lia := login(liaEmail, "")
	if lia.AccessToken == "" || lia.TwoFactor != nil {
		t.Fatalf("cliente sem a verificação = %+v", lia)
	}
	if renewed := post("", "/api/auth/refresh", map[string]string{"refreshToken": lia.RefreshToken}, http.StatusOK); renewed.AccessToken == "" {
		t.Errorf("cliente não renovou: %+v", renewed)
	}

	// 6. O cliente ativa pelo e-mail (com a senha).
	post(lia.AccessToken, "/api/security/two-factor/start", map[string]string{"method": "email", "password": "errada-123456"}, http.StatusBadRequest)
	post(lia.AccessToken, "/api/security/two-factor/start", map[string]string{"method": "sms", "password": userPassword}, http.StatusBadRequest)
	post(lia.AccessToken, "/api/security/two-factor/start", map[string]string{"method": "email", "password": userPassword}, http.StatusOK)
	post(lia.AccessToken, "/api/security/two-factor/confirm", map[string]string{"code": "000000"}, http.StatusBadRequest)
	var confirmed struct {
		RecoveryCodes []string `json:"recoveryCodes"`
		Method        string   `json:"method"`
	}
	_ = json.Unmarshal(env.must(lia.AccessToken, http.MethodPost, "/api/security/two-factor/confirm",
		map[string]string{"code": mailer.last(t, liaEmail, twofactor.PurposeSetup)}, http.StatusOK), &confirmed)
	if confirmed.Method != "email" || len(confirmed.RecoveryCodes) != 10 {
		t.Fatalf("ativação por e-mail = %+v", confirmed)
	}

	// Login por e-mail: o código chega; reenviar espera 30 s; o novo vale.
	byEmail := login(liaEmail, "")
	if byEmail.TwoFactor == nil || byEmail.TwoFactor.Method != twofactor.MethodEmail {
		t.Fatalf("login do cliente por e-mail = %+v", byEmail)
	}
	firstCode := mailer.last(t, liaEmail, twofactor.PurposeLogin)
	post("", "/api/auth/2fa/resend", map[string]string{"challenge": byEmail.TwoFactor.Token}, http.StatusBadRequest)
	advance(31 * time.Second)
	post("", "/api/auth/2fa/resend", map[string]string{"challenge": byEmail.TwoFactor.Token}, http.StatusNoContent)
	newCode := mailer.last(t, liaEmail, twofactor.PurposeLogin)
	if newCode == firstCode {
		t.Skip("códigos sorteados iguais (1 em um milhão)")
	}
	post("", "/api/auth/2fa/verify", map[string]any{"challenge": byEmail.TwoFactor.Token, "code": firstCode}, http.StatusBadRequest)
	liaSession := post("", "/api/auth/2fa/verify", map[string]any{"challenge": byEmail.TwoFactor.Token, "code": newCode, "trustDevice": true}, http.StatusOK)
	if liaSession.AccessToken == "" || liaSession.DeviceToken == "" {
		t.Fatalf("login por e-mail = %+v", liaSession)
	}

	// A situação na tela de segurança: o método, os códigos e o aparelho.
	var status twofactor.Status
	_ = json.Unmarshal(env.must(liaSession.AccessToken, http.MethodGet, "/api/security/two-factor", nil, http.StatusOK), &status)
	if !status.Enabled || status.Method != "email" || status.Required || status.RecoveryCodesLeft != 10 ||
		len(status.Devices) != 1 || len(status.Methods) != 2 {
		t.Fatalf("situação = %+v", status)
	}

	// Troca para o app.
	enrollApp := post(liaSession.AccessToken, "/api/security/two-factor/start", map[string]string{"method": "totp", "password": userPassword}, http.StatusOK)
	_ = json.Unmarshal(env.must(liaSession.AccessToken, http.MethodPost, "/api/security/two-factor/confirm",
		map[string]string{"code": totpAt(t, enrollApp.Secret, clock())}, http.StatusOK), &confirmed)
	if confirmed.Method != "totp" {
		t.Fatalf("troca para o app = %+v", confirmed)
	}

	// Desativar pede a senha e o código (o mesmo código do app não vale de novo).
	post(liaSession.AccessToken, "/api/security/two-factor/disable",
		map[string]string{"password": userPassword, "code": totpAt(t, enrollApp.Secret, clock())}, http.StatusBadRequest)
	advance(30 * time.Second)
	post(liaSession.AccessToken, "/api/security/two-factor/disable",
		map[string]string{"password": "errada-123456", "code": totpAt(t, enrollApp.Secret, clock())}, http.StatusBadRequest)
	env.must(liaSession.AccessToken, http.MethodPost, "/api/security/two-factor/disable",
		map[string]string{"password": userPassword, "code": totpAt(t, enrollApp.Secret, clock())}, http.StatusNoContent)
	if back := login(liaEmail, liaSession.DeviceToken); back.AccessToken == "" {
		t.Errorf("desativada, ainda pediu código: %+v", back)
	}

	// 7. A equipe não desativa nem usa o e-mail.
	post(adminToken, "/api/security/two-factor/start", map[string]string{"method": "email", "password": userPassword}, http.StatusBadRequest)
	advance(30 * time.Second)
	if out := post(adminToken, "/api/security/two-factor/disable", map[string]string{"password": userPassword, "code": totpAt(t, secret, clock())}, http.StatusBadRequest); !strings.Contains(out.Error, "obrigatória para a equipe") {
		t.Errorf("equipe desativando = %+v", out)
	}

	// 8. O admin vê e redefine; o cliente não redefine ninguém.
	var team []struct {
		Email     string          `json:"email"`
		TwoFactor twofactor.Brief `json:"twoFactor"`
	}
	_ = json.Unmarshal(env.must(adminToken, http.MethodGet, "/api/users", nil, http.StatusOK), &team)
	for _, m := range team {
		if (m.Email == adminEmail) != m.TwoFactor.Enabled {
			t.Errorf("lista da equipe: %s = %+v", m.Email, m.TwoFactor)
		}
	}
	var adminID string
	if err := env.db.QueryRow(ctx, `SELECT id::text FROM users WHERE email = $1`, adminEmail).Scan(&adminID); err != nil {
		t.Fatal(err)
	}
	post(liaSession.AccessToken, "/api/users/"+adminID+"/two-factor/reset", nil, http.StatusForbidden)
	env.must(adminToken, http.MethodPost, "/api/users/"+adminID+"/two-factor/reset", nil, http.StatusNoContent)
	if reset := login(adminEmail, done.DeviceToken); reset.TwoFactor == nil || reset.TwoFactor.Kind != twofactor.KindSetup {
		t.Errorf("depois de redefinida, a equipe não reativa: %+v", reset)
	}
	var audited int
	if err := env.db.QueryRow(ctx, `SELECT COUNT(*) FROM audit_logs WHERE action IN ($1, $2, $3)`,
		audit.ActionTwoFactorEnabled, audit.ActionTwoFactorDisabled, audit.ActionTwoFactorReset).Scan(&audited); err != nil || audited != 5 {
		t.Errorf("auditoria = %d (%v)", audited, err)
	}

	// 9. A biometria já são dois fatores: dispensa o código (com a verificação ativa).
	liaUser, err := authSvc.FindByEmail(ctx, liaEmail)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tf.Start(ctx, liaUser, "email", userPassword); err != nil {
		t.Fatal(err)
	}
	if _, _, err := tf.Confirm(ctx, liaUser, mailer.last(t, liaEmail, twofactor.PurposeSetup)); err != nil {
		t.Fatal(err)
	}
	if out, err := tf.BeginLogin(ctx, liaUser, "", true); err != nil || !out.Pass {
		t.Errorf("biometria pediu código: %+v %v", out, err)
	}
}
