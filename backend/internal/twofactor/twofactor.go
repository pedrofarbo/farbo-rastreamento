// Package twofactor é a verificação em duas etapas: depois da senha, o
// código do app autenticador (TOTP) ou do e-mail. Obrigatória para a equipe
// (só pelo app), opcional para o cliente (app ou e-mail). Cada pessoa tem
// códigos de recuperação e pode confiar num aparelho por 30 dias. Entrar com
// a biometria do aparelho já são dois fatores: dispensa o código.
package twofactor

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/qrcode"
)

// Métodos.
const (
	MethodTOTP  = "totp"
	MethodEmail = "email"
	// MethodRecovery e MethodTrusted só aparecem no resultado do login.
	MethodRecovery = "recovery"
	MethodTrusted  = "trusted_device"
)

// Tipos de desafio do login.
const (
	KindVerify = "verify"
	KindSetup  = "setup"
)

// Para que serve o código mandado por e-mail (muda o texto do e-mail).
const (
	PurposeLogin  = "login"
	PurposeSetup  = "setup"
	PurposeAction = "action"
)

// Avisos por e-mail de mudanças na verificação.
const (
	EventEnabledTOTP  = "enabled_totp"
	EventEnabledEmail = "enabled_email"
	EventDisabled     = "disabled"
	EventReset        = "reset"
	EventRecoveryUsed = "recovery_used"
)

const (
	challengeTTL = 10 * time.Minute
	setupTTL     = 30 * time.Minute
	codeTTL      = 10 * time.Minute
	maxAttempts  = 5
	resendEvery  = 30 * time.Second
	maxSends     = 5
	// TrustFor é quanto tempo um aparelho confiável dispensa o código.
	TrustFor      = 30 * 24 * time.Hour
	recoveryCount = 10
)

// Error é uma recusa para a tela, com a mensagem pronta.
type Error struct {
	Message string
	// Expired: o desafio acabou (expirou ou tentativas demais) — a tela
	// volta para a senha.
	Expired bool
}

func (e Error) Error() string { return e.Message }

func fail(format string, args ...any) error { return Error{Message: fmt.Sprintf(format, args...)} }

var errExpired = Error{Message: "A verificação expirou. Entre de novo com a senha.", Expired: true}

// ErrSetupRequired: alguém da equipe sem a verificação ativa (a sessão não
// renova; no login, ativa).
var ErrSetupRequired = errors.New("a equipe precisa ativar a verificação em duas etapas")

// Mailer manda os códigos e os avisos.
type Mailer interface {
	Code(ctx context.Context, to, name, code string, ttl time.Duration, purpose string) error
	Changed(ctx context.Context, to, name, event string, at time.Time) error
}

// Users é o que o serviço precisa das contas.
type Users interface {
	GetUser(ctx context.Context, id uuid.UUID) (*auth.User, error)
	PasswordMatches(ctx context.Context, id uuid.UUID, password string) (bool, error)
}

type Service struct {
	db     *database.DB
	users  Users
	mailer Mailer
	gcm    cipher.AEAD
	key    []byte
	now    func() time.Time
	log    *slog.Logger
	async  func(func())
}

// NewService: secret é o JWT_SECRET; dele saem a chave que cifra os
// segredos do app e a que embaralha os códigos guardados.
func NewService(db *database.DB, users Users, mailer Mailer, secret []byte, log *slog.Logger) (*Service, error) {
	sealKey := sha256.Sum256(append([]byte("farbo:two-factor:seal:"), secret...))
	block, err := aes.NewCipher(sealKey[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	hashKey := sha256.Sum256(append([]byte("farbo:two-factor:hash:"), secret...))
	return &Service{
		db: db, users: users, mailer: mailer, gcm: gcm, key: hashKey[:], now: time.Now,
		log: log.With("component", "twofactor"), async: func(f func()) { go f() },
	}, nil
}

// SetClock troca o relógio (testes).
func (s *Service) SetClock(now func() time.Time) { s.now = now }

// SetSync manda os avisos na hora (testes).
func (s *Service) SetSync() { s.async = func(f func()) { f() } }

// Required diz quem precisa da verificação: toda a equipe.
func Required(role string) bool {
	return role == auth.RoleAdmin || role == auth.RoleOperator || role == auth.RoleViewer
}

// ---------------------------------------------------------------------------
// Situação
// ---------------------------------------------------------------------------

// Device é um aparelho confiável.
type Device struct {
	ID         uuid.UUID `json:"id"`
	UserAgent  string    `json:"userAgent"`
	CreatedAt  time.Time `json:"createdAt"`
	LastUsedAt time.Time `json:"lastUsedAt"`
	ExpiresAt  time.Time `json:"expiresAt"`
}

// Status é a verificação de uma pessoa, para a tela de segurança.
type Status struct {
	Enabled   bool       `json:"enabled"`
	Method    string     `json:"method"`
	Required  bool       `json:"required"`
	EnabledAt *time.Time `json:"enabledAt"`
	// Methods: o que a pessoa pode usar (a equipe, só o app).
	Methods           []string `json:"methods"`
	RecoveryCodesLeft int      `json:"recoveryCodesLeft"`
	Devices           []Device `json:"devices"`
	EmailHint         string   `json:"emailHint"`
}

func methodsFor(role string) []string {
	if Required(role) {
		return []string{MethodTOTP}
	}
	return []string{MethodTOTP, MethodEmail}
}

// Brief é o resumo da verificação (as listas da equipe e a ficha do cliente).
type Brief struct {
	Enabled bool   `json:"enabled"`
	Method  string `json:"method"`
}

// Briefs resume a verificação de várias pessoas.
func (s *Service) Briefs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]Brief, error) {
	rows, err := s.db.Query(ctx, `SELECT user_id, method FROM user_two_factor WHERE user_id = ANY($1) AND enabled_at IS NOT NULL`, ids)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	out := map[uuid.UUID]Brief{}
	for rows.Next() {
		var id uuid.UUID
		var method string
		if err := rows.Scan(&id, &method); err != nil {
			return nil, err
		}
		out[id] = Brief{Enabled: true, Method: method}
	}
	return out, rows.Err()
}

// enabled devolve o método ativo ("" se não ativou).
func (s *Service) enabled(ctx context.Context, q database.Querier, userID uuid.UUID) (string, error) {
	var method string
	err := q.QueryRow(ctx, `SELECT method FROM user_two_factor WHERE user_id = $1 AND enabled_at IS NOT NULL`, userID).Scan(&method)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return method, err
}

// Status mostra a verificação de quem está conectado.
func (s *Service) Status(ctx context.Context, user *auth.User) (*Status, error) {
	out := &Status{Required: Required(user.Role), Methods: methodsFor(user.Role), EmailHint: EmailHint(user.Email), Devices: []Device{}}
	var enabledAt *time.Time
	err := s.db.QueryRow(ctx, `SELECT method, enabled_at FROM user_two_factor WHERE user_id = $1`, user.ID).Scan(&out.Method, &enabledAt)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, database.MapError(err)
	}
	out.Enabled, out.EnabledAt = enabledAt != nil, enabledAt
	if !out.Enabled {
		out.Method = ""
		return out, nil
	}
	if err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM two_factor_recovery_codes WHERE user_id = $1 AND used_at IS NULL`,
		user.ID).Scan(&out.RecoveryCodesLeft); err != nil {
		return nil, database.MapError(err)
	}
	rows, err := s.db.Query(ctx, `
		SELECT id, user_agent, created_at, last_used_at, expires_at FROM two_factor_devices
		WHERE user_id = $1 AND expires_at > $2 ORDER BY last_used_at DESC`, user.ID, s.now())
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var d Device
		if err := rows.Scan(&d.ID, &d.UserAgent, &d.CreatedAt, &d.LastUsedAt, &d.ExpiresAt); err != nil {
			return nil, err
		}
		out.Devices = append(out.Devices, d)
	}
	return out, rows.Err()
}

// Guard é a regra da sessão: alguém da equipe sem a verificação ativa não
// renova a sessão (entra de novo e ativa no login).
func (s *Service) Guard(ctx context.Context, user *auth.User) error {
	if !Required(user.Role) {
		return nil
	}
	method, err := s.enabled(ctx, s.db, user.ID)
	if err != nil {
		return err
	}
	if method == "" {
		return ErrSetupRequired
	}
	return nil
}

// ---------------------------------------------------------------------------
// Login
// ---------------------------------------------------------------------------

// Challenge é o que a tela recebe depois da senha: digitar o código
// (verify) ou ativar a verificação (setup, a equipe).
type Challenge struct {
	Token     string    `json:"challenge"`
	Kind      string    `json:"kind"`
	Method    string    `json:"method"`
	EmailHint string    `json:"emailHint"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// Outcome é a decisão depois da senha (ou da biometria).
type Outcome struct {
	// Pass: a sessão pode abrir. Via diz por quê (aparelho confiável,
	// biometria); vazio, sem verificação.
	Pass      bool
	Via       string
	Challenge *Challenge
}

// BeginLogin decide o que falta depois da senha. biometric: entrou com a
// biometria do aparelho, que já são dois fatores.
func (s *Service) BeginLogin(ctx context.Context, user *auth.User, deviceToken string, biometric bool) (*Outcome, error) {
	method, err := s.enabled(ctx, s.db, user.ID)
	if err != nil {
		return nil, database.MapError(err)
	}
	switch {
	case method != "" && biometric:
		return &Outcome{Pass: true, Via: "biometric"}, nil
	case method != "":
		if deviceToken != "" && s.trusted(ctx, user.ID, deviceToken) {
			return &Outcome{Pass: true, Via: MethodTrusted}, nil
		}
		ch, err := s.newChallenge(ctx, user, KindVerify, method)
		if err != nil {
			return nil, err
		}
		return &Outcome{Challenge: ch}, nil
	case Required(user.Role):
		ch, err := s.newChallenge(ctx, user, KindSetup, "")
		if err != nil {
			return nil, err
		}
		return &Outcome{Challenge: ch}, nil
	default:
		return &Outcome{Pass: true}, nil
	}
}

func (s *Service) newChallenge(ctx context.Context, user *auth.User, kind, method string) (*Challenge, error) {
	token, err := randomToken()
	if err != nil {
		return nil, err
	}
	ttl := challengeTTL
	if kind == KindSetup {
		ttl = setupTTL
	}
	ch := &Challenge{Token: token, Kind: kind, Method: method, EmailHint: EmailHint(user.Email), ExpiresAt: s.now().Add(ttl)}
	// Limpa os desafios velhos de vez em quando (cada login cria um).
	if _, err := s.db.Exec(ctx, `DELETE FROM two_factor_challenges WHERE expires_at < $1`, s.now().Add(-time.Hour)); err != nil {
		s.log.Warn("desafios velhos não apagados", "err", err)
	}
	var id uuid.UUID
	if err := s.db.QueryRow(ctx, `
		INSERT INTO two_factor_challenges (token_hash, user_id, kind, method, expires_at)
		VALUES ($1, $2, $3, $4, $5) RETURNING id`, hashToken(token), user.ID, kind, method, ch.ExpiresAt).Scan(&id); err != nil {
		return nil, database.MapError(err)
	}
	// O código por e-mail: no login de quem usa o e-mail e no começo da
	// ativação obrigatória (confirma que é o dono do e-mail).
	if method == MethodEmail || kind == KindSetup {
		purpose := PurposeLogin
		if kind == KindSetup {
			purpose = PurposeSetup
		}
		if err := s.sendChallengeCode(ctx, s.db, id, user, purpose); err != nil {
			return nil, err
		}
	}
	return ch, nil
}

// sendChallengeCode sorteia e manda o código do desafio.
func (s *Service) sendChallengeCode(ctx context.Context, q database.Querier, id uuid.UUID, user *auth.User, purpose string) error {
	code, err := randomDigits(6)
	if err != nil {
		return err
	}
	if _, err := q.Exec(ctx, `
		UPDATE two_factor_challenges SET code_hash = $2, code_sent_at = $3, sends = sends + 1 WHERE id = $1`,
		id, s.hashCode("challenge:"+id.String(), code), s.now()); err != nil {
		return database.MapError(err)
	}
	if err := s.mailer.Code(ctx, user.Email, user.Name, code, codeTTL, purpose); err != nil {
		s.log.Error("código da verificação não enviado", "user", user.ID, "err", err)
		return fail("Não deu para mandar o código por e-mail agora. Tente de novo em instantes.")
	}
	return nil
}

// challengeRow é o desafio travado na transação.
type challengeRow struct {
	id            uuid.UUID
	userID        uuid.UUID
	kind, method  string
	codeHash      string
	codeSentAt    *time.Time
	sends         int
	emailVerified bool
	pendingSecret string
	attempts      int
	expiresAt     time.Time
}

func (s *Service) lockChallenge(ctx context.Context, tx pgx.Tx, token, kind string) (*challengeRow, error) {
	var c challengeRow
	err := tx.QueryRow(ctx, `
		SELECT id, user_id, kind, method, code_hash, code_sent_at, sends, email_verified, pending_secret, attempts, expires_at
		FROM two_factor_challenges WHERE token_hash = $1 FOR UPDATE`, hashToken(strings.TrimSpace(token)),
	).Scan(&c.id, &c.userID, &c.kind, &c.method, &c.codeHash, &c.codeSentAt, &c.sends, &c.emailVerified,
		&c.pendingSecret, &c.attempts, &c.expiresAt)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (c.kind != kind || !s.now().Before(c.expiresAt) || c.attempts >= maxAttempts)) {
		return nil, errExpired
	}
	if err != nil {
		return nil, database.MapError(err)
	}
	return &c, nil
}

// wrongCode conta a tentativa errada (fora da transação que vai desfazer).
func (s *Service) wrongCode(ctx context.Context, id uuid.UUID, attempts int, userID uuid.UUID) error {
	if _, err := s.db.Exec(ctx, `UPDATE two_factor_challenges SET attempts = attempts + 1 WHERE id = $1`, id); err != nil {
		return database.MapError(err)
	}
	s.log.Warn("código da verificação errado", "user", userID, "tentativa", attempts+1)
	if left := maxAttempts - attempts - 1; left > 0 {
		return fail("Código incorreto. %s.", plural(left, "tentativa restante", "tentativas restantes"))
	}
	return errExpired
}

// Result é o login concluído.
type Result struct {
	UserID uuid.UUID
	// Used: totp, email ou recovery.
	Used string
	// DeviceToken: o aparelho confiável (quando pedido).
	DeviceToken string
	// RecoveryCodes: os códigos novos (na ativação obrigatória).
	RecoveryCodes []string
}

// errBadCode marca o código errado dentro da transação.
var errBadCode = errors.New("código errado")

// Verify confere o código do login: o do app, o do e-mail ou um de
// recuperação. trust: confiar neste aparelho por 30 dias.
func (s *Service) Verify(ctx context.Context, token, code string, trust bool, userAgent string) (*Result, error) {
	code = normalizeCode(code)
	var res *Result
	var ch *challengeRow
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var err error
		if ch, err = s.lockChallenge(ctx, tx, token, KindVerify); err != nil {
			return err
		}
		used, err := s.checkFactor(ctx, tx, ch.userID, code, ch)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM two_factor_challenges WHERE id = $1`, ch.id); err != nil {
			return err
		}
		res = &Result{UserID: ch.userID, Used: used}
		if trust {
			if res.DeviceToken, err = s.trustDevice(ctx, tx, ch.userID, userAgent); err != nil {
				return err
			}
		}
		return nil
	})
	if errors.Is(err, errBadCode) {
		return nil, s.wrongCode(ctx, ch.id, ch.attempts, ch.userID)
	}
	if err != nil {
		return nil, mapErr(err)
	}
	if res.Used == MethodRecovery {
		s.notify(res.UserID, EventRecoveryUsed)
	}
	return res, nil
}

// checkFactor confere o segundo fator de quem já ativou: o código do app
// (sem repetir um já usado), o do e-mail do desafio ou da ação, ou um de
// recuperação (que é gasto).
func (s *Service) checkFactor(ctx context.Context, tx pgx.Tx, userID uuid.UUID, code string, ch *challengeRow) (string, error) {
	var method, sealed, actionCode string
	var lastStep int64
	var actionExpires *time.Time
	if err := tx.QueryRow(ctx, `
		SELECT method, totp_secret, totp_last_step, action_code, action_expires FROM user_two_factor
		WHERE user_id = $1 AND enabled_at IS NOT NULL FOR UPDATE`, userID,
	).Scan(&method, &sealed, &lastStep, &actionCode, &actionExpires); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", errExpired
		}
		return "", err
	}
	if len(code) == 6 && isDigits(code) {
		switch method {
		case MethodTOTP:
			secret, err := s.open(sealed)
			if err != nil {
				s.log.Error("segredo do app ilegível (JWT_SECRET trocado?)", "user", userID, "err", err)
				return "", fail("Não deu para conferir o código do app. Use um código de recuperação.")
			}
			step := matchTOTP(secret, code, s.now())
			if step == 0 || step <= lastStep {
				return "", errBadCode
			}
			_, err = tx.Exec(ctx, `UPDATE user_two_factor SET totp_last_step = $2 WHERE user_id = $1`, userID, step)
			return MethodTOTP, err
		case MethodEmail:
			// O código do login (desafio) ou o da ação (desativar etc.).
			if ch != nil && ch.codeHash != "" && ch.codeSentAt != nil && s.now().Before(ch.codeSentAt.Add(codeTTL)) &&
				hmac.Equal([]byte(ch.codeHash), []byte(s.hashCode("challenge:"+ch.id.String(), code))) {
				return MethodEmail, nil
			}
			if ch == nil && actionCode != "" && actionExpires != nil && s.now().Before(*actionExpires) &&
				hmac.Equal([]byte(actionCode), []byte(s.hashCode("action:"+userID.String(), code))) {
				_, err := tx.Exec(ctx, `UPDATE user_two_factor SET action_code = '', action_expires = NULL WHERE user_id = $1`, userID)
				return MethodEmail, err
			}
			return "", errBadCode
		}
		return "", errBadCode
	}
	if len(code) == 8 {
		tag, err := tx.Exec(ctx, `
			UPDATE two_factor_recovery_codes SET used_at = $3
			WHERE user_id = $1 AND code_hash = $2 AND used_at IS NULL`, userID, s.hashCode("recovery:"+userID.String(), code), s.now())
		if err != nil {
			return "", err
		}
		if tag.RowsAffected() == 1 {
			return MethodRecovery, nil
		}
	}
	return "", errBadCode
}

// Resend manda outro código por e-mail (login por e-mail ou o começo da
// ativação obrigatória).
func (s *Service) Resend(ctx context.Context, token string) error {
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		ch, err := s.lockAny(ctx, tx, token)
		if err != nil {
			return err
		}
		if ch.method != MethodEmail && (ch.kind != KindSetup || ch.emailVerified) {
			return fail("Este código não vai por e-mail: abra o app autenticador.")
		}
		if ch.sends >= maxSends {
			return fail("Já mandamos códigos demais. Entre de novo com a senha.")
		}
		if ch.codeSentAt != nil && s.now().Before(ch.codeSentAt.Add(resendEvery)) {
			return fail("Aguarde alguns segundos para pedir outro código.")
		}
		user, err := s.users.GetUser(ctx, ch.userID)
		if err != nil {
			return err
		}
		purpose := PurposeLogin
		if ch.kind == KindSetup {
			purpose = PurposeSetup
		}
		return s.sendChallengeCode(ctx, tx, ch.id, user, purpose)
	})
}

func (s *Service) lockAny(ctx context.Context, tx pgx.Tx, token string) (*challengeRow, error) {
	var kind string
	if err := tx.QueryRow(ctx, `SELECT kind FROM two_factor_challenges WHERE token_hash = $1`,
		hashToken(strings.TrimSpace(token))).Scan(&kind); err != nil {
		return nil, errExpired
	}
	return s.lockChallenge(ctx, tx, token, kind)
}

// ---------------------------------------------------------------------------
// Ativação obrigatória da equipe (no login)
// ---------------------------------------------------------------------------

// Enrollment é o que a tela mostra para cadastrar no app.
type Enrollment struct {
	Secret     string `json:"secret"`
	OtpauthURL string `json:"otpauthUrl"`
	// QRCode: o SVG do otpauth (para escanear).
	QRCode string `json:"qrCode"`
}

func enrollmentFor(secret, email string) (*Enrollment, error) {
	link := otpauthURL(secret, email)
	svg, err := qrcode.SVG(link)
	if err != nil {
		return nil, err
	}
	return &Enrollment{Secret: secret, OtpauthURL: link, QRCode: string(svg)}, nil
}

// SetupEmail confere o código do e-mail (prova de que é o dono da conta, não
// só de quem sabe a senha) e devolve o segredo para o app.
func (s *Service) SetupEmail(ctx context.Context, token, code string) (*Enrollment, error) {
	code = normalizeCode(code)
	var out *Enrollment
	var ch *challengeRow
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var err error
		if ch, err = s.lockChallenge(ctx, tx, token, KindSetup); err != nil {
			return err
		}
		if !ch.emailVerified {
			if ch.codeHash == "" || ch.codeSentAt == nil || !s.now().Before(ch.codeSentAt.Add(codeTTL)) ||
				!hmac.Equal([]byte(ch.codeHash), []byte(s.hashCode("challenge:"+ch.id.String(), code))) {
				return errBadCode
			}
		}
		user, err := s.users.GetUser(ctx, ch.userID)
		if err != nil {
			return err
		}
		secret := ""
		if ch.pendingSecret != "" {
			secret, _ = s.open(ch.pendingSecret)
		}
		if secret == "" {
			if secret, err = newSecret(); err != nil {
				return err
			}
			sealed, err := s.seal(secret)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE two_factor_challenges SET email_verified = TRUE, pending_secret = $2 WHERE id = $1`,
				ch.id, sealed); err != nil {
				return err
			}
		}
		out, err = enrollmentFor(secret, user.Email)
		return err
	})
	if errors.Is(err, errBadCode) {
		return nil, s.wrongCode(ctx, ch.id, ch.attempts, ch.userID)
	}
	if err != nil {
		return nil, mapErr(err)
	}
	return out, nil
}

// SetupConfirm confere o primeiro código do app, ativa a verificação e
// entrega os códigos de recuperação.
func (s *Service) SetupConfirm(ctx context.Context, token, code string, trust bool, userAgent string) (*Result, error) {
	code = normalizeCode(code)
	var res *Result
	var ch *challengeRow
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var err error
		if ch, err = s.lockChallenge(ctx, tx, token, KindSetup); err != nil {
			return err
		}
		if !ch.emailVerified || ch.pendingSecret == "" {
			return fail("Confirme primeiro o código que mandamos para o seu e-mail.")
		}
		secret, err := s.open(ch.pendingSecret)
		if err != nil {
			return errExpired
		}
		step := matchTOTP(secret, code, s.now())
		if step == 0 {
			return errBadCode
		}
		if err := s.enable(ctx, tx, ch.userID, MethodTOTP, ch.pendingSecret, step); err != nil {
			return err
		}
		codes, err := s.newRecoveryCodes(ctx, tx, ch.userID)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM two_factor_challenges WHERE id = $1`, ch.id); err != nil {
			return err
		}
		res = &Result{UserID: ch.userID, Used: MethodTOTP, RecoveryCodes: codes}
		if trust {
			res.DeviceToken, err = s.trustDevice(ctx, tx, ch.userID, userAgent)
		}
		return err
	})
	if errors.Is(err, errBadCode) {
		return nil, s.wrongCode(ctx, ch.id, ch.attempts, ch.userID)
	}
	if err != nil {
		return nil, mapErr(err)
	}
	s.notify(res.UserID, EventEnabledTOTP)
	return res, nil
}

// enable grava o método ativo (o anterior, se havia, sai).
func (s *Service) enable(ctx context.Context, tx pgx.Tx, userID uuid.UUID, method, sealed string, step int64) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO user_two_factor (user_id, method, totp_secret, totp_last_step, enabled_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $5)
		ON CONFLICT (user_id) DO UPDATE SET method = EXCLUDED.method, totp_secret = EXCLUDED.totp_secret,
			totp_last_step = EXCLUDED.totp_last_step, enabled_at = EXCLUDED.enabled_at,
			pending_method = '', pending_secret = '', pending_code = '', pending_expires = NULL, pending_attempts = 0,
			action_code = '', action_expires = NULL, action_attempts = 0, updated_at = EXCLUDED.updated_at`,
		userID, method, sealed, step, s.now())
	return err
}

// ---------------------------------------------------------------------------
// Conta: ativar, trocar, desativar, códigos e aparelhos
// ---------------------------------------------------------------------------

func (s *Service) checkPassword(ctx context.Context, user *auth.User, password string) error {
	ok, err := s.users.PasswordMatches(ctx, user.ID, password)
	if err != nil {
		return err
	}
	if !ok {
		return fail("Senha incorreta.")
	}
	return nil
}

// Start começa a ativação (ou a troca de método) de quem está conectado,
// com a senha: o app recebe o QR Code; o e-mail, um código.
func (s *Service) Start(ctx context.Context, user *auth.User, method, password string) (*Enrollment, error) {
	allowed := false
	for _, m := range methodsFor(user.Role) {
		allowed = allowed || m == method
	}
	if !allowed {
		if Required(user.Role) {
			return nil, fail("A equipe usa o app autenticador.")
		}
		return nil, fail("Escolha o app autenticador ou o e-mail.")
	}
	if err := s.checkPassword(ctx, user, password); err != nil {
		return nil, err
	}
	sealed, code, out := "", "", (*Enrollment)(nil)
	if method == MethodTOTP {
		secret, err := newSecret()
		if err != nil {
			return nil, err
		}
		if sealed, err = s.seal(secret); err != nil {
			return nil, err
		}
		if out, err = enrollmentFor(secret, user.Email); err != nil {
			return nil, err
		}
	} else {
		var err error
		if code, err = randomDigits(6); err != nil {
			return nil, err
		}
	}
	codeHash := ""
	if code != "" {
		codeHash = s.hashCode("pending:"+user.ID.String(), code)
	}
	if _, err := s.db.Exec(ctx, `
		INSERT INTO user_two_factor (user_id, pending_method, pending_secret, pending_code, pending_expires, pending_attempts)
		VALUES ($1, $2, $3, $4, $5, 0)
		ON CONFLICT (user_id) DO UPDATE SET pending_method = EXCLUDED.pending_method,
			pending_secret = EXCLUDED.pending_secret, pending_code = EXCLUDED.pending_code,
			pending_expires = EXCLUDED.pending_expires, pending_attempts = 0, updated_at = NOW()`,
		user.ID, method, sealed, codeHash, s.now().Add(setupTTL)); err != nil {
		return nil, database.MapError(err)
	}
	if code != "" {
		if err := s.mailer.Code(ctx, user.Email, user.Name, code, codeTTL, PurposeSetup); err != nil {
			s.log.Error("código da ativação não enviado", "user", user.ID, "err", err)
			return nil, fail("Não deu para mandar o código por e-mail agora. Tente de novo em instantes.")
		}
		return &Enrollment{}, nil
	}
	return out, nil
}

// Confirm conclui a ativação com o código (do app ou do e-mail) e entrega
// os códigos de recuperação novos.
func (s *Service) Confirm(ctx context.Context, user *auth.User, code string) ([]string, string, error) {
	code = normalizeCode(code)
	var codes []string
	var method string
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var sealed, codeHash string
		var expires *time.Time
		var attempts int
		err := tx.QueryRow(ctx, `
			SELECT pending_method, pending_secret, pending_code, pending_expires, pending_attempts
			FROM user_two_factor WHERE user_id = $1 FOR UPDATE`, user.ID).Scan(&method, &sealed, &codeHash, &expires, &attempts)
		if errors.Is(err, pgx.ErrNoRows) || method == "" || expires == nil || !s.now().Before(*expires) || attempts >= maxAttempts {
			return fail("A ativação expirou. Comece de novo.")
		}
		if err != nil {
			return err
		}
		step := int64(0)
		ok := false
		if method == MethodTOTP {
			if secret, err := s.open(sealed); err == nil {
				step = matchTOTP(secret, code, s.now())
				ok = step > 0
			}
		} else {
			ok = codeHash != "" && hmac.Equal([]byte(codeHash), []byte(s.hashCode("pending:"+user.ID.String(), code)))
		}
		if !ok {
			return errBadCode
		}
		if method == MethodEmail {
			sealed = ""
		}
		if err := s.enable(ctx, tx, user.ID, method, sealed, step); err != nil {
			return err
		}
		codes, err = s.newRecoveryCodes(ctx, tx, user.ID)
		return err
	})
	if errors.Is(err, errBadCode) {
		var attempts int
		_ = s.db.QueryRow(ctx, `UPDATE user_two_factor SET pending_attempts = pending_attempts + 1 WHERE user_id = $1
			RETURNING pending_attempts`, user.ID).Scan(&attempts)
		if left := maxAttempts - attempts; left > 0 {
			return nil, "", fail("Código incorreto. %s.", plural(left, "tentativa restante", "tentativas restantes"))
		}
		return nil, "", fail("Código incorreto vezes demais. Comece de novo.")
	}
	if err != nil {
		return nil, "", mapErr(err)
	}
	event := EventEnabledTOTP
	if method == MethodEmail {
		event = EventEnabledEmail
	}
	s.notify(user.ID, event)
	return codes, method, nil
}

// SendActionCode manda o código por e-mail para confirmar uma mudança de
// quem usa o e-mail como segundo fator.
func (s *Service) SendActionCode(ctx context.Context, user *auth.User) error {
	method, err := s.enabled(ctx, s.db, user.ID)
	if err != nil {
		return database.MapError(err)
	}
	if method != MethodEmail {
		return fail("Use o código do app autenticador.")
	}
	var expires *time.Time
	if err := s.db.QueryRow(ctx, `SELECT action_expires FROM user_two_factor WHERE user_id = $1`, user.ID).Scan(&expires); err != nil {
		return database.MapError(err)
	}
	if expires != nil && s.now().Before(expires.Add(-codeTTL+resendEvery)) {
		return fail("Aguarde alguns segundos para pedir outro código.")
	}
	code, err := randomDigits(6)
	if err != nil {
		return err
	}
	if _, err := s.db.Exec(ctx, `UPDATE user_two_factor SET action_code = $2, action_expires = $3, action_attempts = 0
		WHERE user_id = $1`, user.ID, s.hashCode("action:"+user.ID.String(), code), s.now().Add(codeTTL)); err != nil {
		return database.MapError(err)
	}
	if err := s.mailer.Code(ctx, user.Email, user.Name, code, codeTTL, PurposeAction); err != nil {
		s.log.Error("código da mudança não enviado", "user", user.ID, "err", err)
		return fail("Não deu para mandar o código por e-mail agora. Tente de novo em instantes.")
	}
	return nil
}

// confirmChange confere a senha e o segundo fator de quem já ativou (para
// desativar ou gerar códigos novos).
func (s *Service) confirmChange(ctx context.Context, tx pgx.Tx, user *auth.User, password, code string) error {
	if err := s.checkPassword(ctx, user, password); err != nil {
		return err
	}
	_, err := s.checkFactor(ctx, tx, user.ID, normalizeCode(code), nil)
	if errors.Is(err, errBadCode) {
		return fail("Código incorreto.")
	}
	return err
}

// Disable desliga a verificação (só quem não é da equipe).
func (s *Service) Disable(ctx context.Context, user *auth.User, password, code string) error {
	if Required(user.Role) {
		return fail("A verificação em duas etapas é obrigatória para a equipe.")
	}
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if err := s.confirmChange(ctx, tx, user, password, code); err != nil {
			return err
		}
		return s.wipe(ctx, tx, user.ID)
	})
	if err != nil {
		return mapErr(err)
	}
	s.notify(user.ID, EventDisabled)
	return nil
}

// RegenerateCodes troca os códigos de recuperação (os antigos deixam de valer).
func (s *Service) RegenerateCodes(ctx context.Context, user *auth.User, password, code string) ([]string, error) {
	var codes []string
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if err := s.confirmChange(ctx, tx, user, password, code); err != nil {
			return err
		}
		var err error
		codes, err = s.newRecoveryCodes(ctx, tx, user.ID)
		return err
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return codes, nil
}

// Reset apaga a verificação de alguém (o admin, para quem perdeu o celular e
// os códigos). A equipe ativa de novo no próximo login.
func (s *Service) Reset(ctx context.Context, userID uuid.UUID) error {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if err := s.wipe(ctx, tx, userID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `DELETE FROM two_factor_challenges WHERE user_id = $1`, userID)
		return err
	})
	if err != nil {
		return database.MapError(err)
	}
	s.notify(userID, EventReset)
	return nil
}

func (s *Service) wipe(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	for _, q := range []string{
		`DELETE FROM user_two_factor WHERE user_id = $1`,
		`DELETE FROM two_factor_recovery_codes WHERE user_id = $1`,
		`DELETE FROM two_factor_devices WHERE user_id = $1`,
	} {
		if _, err := tx.Exec(ctx, q, userID); err != nil {
			return err
		}
	}
	return nil
}

// RevokeDevice tira a confiança de um aparelho (uuid.Nil: de todos).
func (s *Service) RevokeDevice(ctx context.Context, userID, id uuid.UUID) error {
	var err error
	if id == uuid.Nil {
		_, err = s.db.Exec(ctx, `DELETE FROM two_factor_devices WHERE user_id = $1`, userID)
	} else {
		var tag interface{ RowsAffected() int64 }
		tag, err = s.db.Exec(ctx, `DELETE FROM two_factor_devices WHERE user_id = $1 AND id = $2`, userID, id)
		if err == nil && tag.RowsAffected() == 0 {
			return database.ErrNotFound
		}
	}
	return database.MapError(err)
}

// ForgetDevices tira a confiança de todos os aparelhos (senha trocada).
func (s *Service) ForgetDevices(ctx context.Context, userID uuid.UUID) error {
	return s.RevokeDevice(ctx, userID, uuid.Nil)
}

// ---------------------------------------------------------------------------
// Códigos de recuperação e aparelhos confiáveis
// ---------------------------------------------------------------------------

// recoveryAlphabet: sem 0/o, 1/l/i (fáceis de confundir ao digitar).
const recoveryAlphabet = "23456789abcdefghjkmnpqrstuvwxyz"

func (s *Service) newRecoveryCodes(ctx context.Context, tx pgx.Tx, userID uuid.UUID) ([]string, error) {
	if _, err := tx.Exec(ctx, `DELETE FROM two_factor_recovery_codes WHERE user_id = $1`, userID); err != nil {
		return nil, err
	}
	codes := make([]string, 0, recoveryCount)
	for len(codes) < recoveryCount {
		raw, err := randomFrom(recoveryAlphabet, 8)
		if err != nil {
			return nil, err
		}
		tag, err := tx.Exec(ctx, `INSERT INTO two_factor_recovery_codes (user_id, code_hash) VALUES ($1, $2)
			ON CONFLICT DO NOTHING`, userID, s.hashCode("recovery:"+userID.String(), raw))
		if err != nil {
			return nil, err
		}
		if tag.RowsAffected() == 1 {
			codes = append(codes, raw[:4]+"-"+raw[4:])
		}
	}
	return codes, nil
}

func (s *Service) trustDevice(ctx context.Context, tx pgx.Tx, userID uuid.UUID, userAgent string) (string, error) {
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	if len(userAgent) > 300 {
		userAgent = userAgent[:300]
	}
	_, err = tx.Exec(ctx, `INSERT INTO two_factor_devices (user_id, token_hash, user_agent, created_at, last_used_at, expires_at)
		VALUES ($1, $2, $3, $4, $4, $5)`, userID, hashToken(token), userAgent, s.now(), s.now().Add(TrustFor))
	return token, err
}

// trusted diz se o aparelho é confiável (e marca o uso).
func (s *Service) trusted(ctx context.Context, userID uuid.UUID, token string) bool {
	tag, err := s.db.Exec(ctx, `UPDATE two_factor_devices SET last_used_at = $3
		WHERE user_id = $1 AND token_hash = $2 AND expires_at > $3`, userID, hashToken(strings.TrimSpace(token)), s.now())
	return err == nil && tag.RowsAffected() == 1
}

// ---------------------------------------------------------------------------
// Avisos, sigilo e utilitários
// ---------------------------------------------------------------------------

// notify avisa o dono da conta por e-mail (fora da requisição).
func (s *Service) notify(userID uuid.UUID, event string) {
	at := s.now()
	s.async(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		user, err := s.users.GetUser(ctx, userID)
		if err != nil {
			s.log.Warn("aviso da verificação sem usuário", "user", userID, "err", err)
			return
		}
		if err := s.mailer.Changed(ctx, user.Email, user.Name, event, at); err != nil {
			s.log.Warn("aviso da verificação não enviado", "user", userID, "event", event, "err", err)
		}
	})
}

func (s *Service) seal(plain string) (string, error) {
	nonce := make([]byte, s.gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return "v1:" + base64.StdEncoding.EncodeToString(s.gcm.Seal(nonce, nonce, []byte(plain), nil)), nil
}

func (s *Service) open(sealed string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(sealed, "v1:"))
	if err != nil || len(raw) < s.gcm.NonceSize() {
		return "", errors.New("segredo ilegível")
	}
	plain, err := s.gcm.Open(nil, raw[:s.gcm.NonceSize()], raw[s.gcm.NonceSize():], nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// hashCode embaralha um código curto com a chave do servidor: quem lê só o
// banco não testa os 10^6 códigos possíveis.
func (s *Service) hashCode(scope, code string) string {
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(scope + ":" + code))
	return hex.EncodeToString(mac.Sum(nil))
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func randomToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func randomFrom(alphabet string, n int) (string, error) {
	out := make([]byte, n)
	max := big.NewInt(int64(len(alphabet)))
	for i := range out {
		v, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		out[i] = alphabet[v.Int64()]
	}
	return string(out), nil
}

func randomDigits(n int) (string, error) { return randomFrom("0123456789", n) }

// normalizeCode tira espaços e hífens e põe em minúsculas ("ABCD-EFGH" e
// "123 456" valem).
func normalizeCode(code string) string {
	return strings.ToLower(strings.NewReplacer(" ", "", "-", "", "\t", "").Replace(strings.TrimSpace(code)))
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

// EmailHint mostra o e-mail sem entregá-lo: "a***@gmail.com".
func EmailHint(email string) string {
	at := strings.LastIndex(email, "@")
	if at < 1 {
		return ""
	}
	return email[:1] + "***" + email[at:]
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

func mapErr(err error) error {
	var e Error
	if errors.As(err, &e) {
		return e
	}
	return database.MapError(err)
}
