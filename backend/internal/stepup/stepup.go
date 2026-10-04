// Package stepup é a confirmação extra antes de ações sensíveis, como o
// cliente desligar o motor pelo app: a biometria do aparelho (WebAuthn: Face
// ID, digital) ou, se ela falhar, a senha da conta.
//
// A confirmação vira um comprovante de uso único, válido por poucos minutos e
// para uma ação só. Quem exige é o servidor: sem o comprovante, a ação é
// recusada mesmo que alguém chame a API direto com um token roubado.
package stepup

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// As ações que pedem a confirmação extra.
const (
	// PurposeEngineCut é desligar o motor.
	PurposeEngineCut = "engine_cut"
	// PurposeEngineResume é religar o motor: com o celular roubado e o app
	// aberto, quem está com ele não desbloqueia o veículo.
	PurposeEngineResume = "engine_resume"
	// PurposeVehicleShare é dar a alguém acesso a um veículo (ou o bloqueio):
	// quem pega o celular por um instante não se cadastra para rastrear.
	PurposeVehicleShare = "vehicle_share"
)

// Métodos de confirmação (no comprovante e na auditoria).
const (
	MethodBiometric = "biometric"
	MethodPassword  = "password"
)

const (
	purposeRegister = "register"
	purposeLogin    = "login"
	// O tempo do aparelho mostrar o Face ID e o cliente olhar para ele.
	challengeTTL = 5 * time.Minute
	// GrantTTL cobre a confirmação e a espera da posição nova antes do corte
	// (até 1 minuto), com folga.
	GrantTTL              = 3 * time.Minute
	maxCredentialsPerUser = 10
	// Senha errada: no máximo 5 tentativas a cada 15 minutos por usuário.
	maxPasswordFailures = 5
	failureWindow       = 15 * time.Minute
	maxNameLength       = 60
)

var purposes = []string{PurposeEngineCut, PurposeEngineResume, PurposeVehicleShare}

var (
	ErrWrongPassword      = errors.New("senha incorreta")
	ErrTooManyAttempts    = errors.New("muitas tentativas com a senha errada: aguarde alguns minutos")
	ErrNoGrant            = errors.New("confirmação ausente, expirada ou já usada")
	ErrUnknownPurpose     = errors.New("ação desconhecida")
	ErrUnknownCredential  = errors.New("biometria não cadastrada nesta conta")
	ErrAlreadyRegistered  = errors.New("esta biometria já está cadastrada")
	ErrTooManyCredentials = fmt.Errorf("limite de %d aparelhos com biometria: remova um para cadastrar outro", maxCredentialsPerUser)
)

// Passwords confere a senha de um usuário logado (auth.Service).
type Passwords interface {
	PasswordMatches(ctx context.Context, id uuid.UUID, password string) (bool, error)
}

// Credential é um aparelho com biometria cadastrada.
type Credential struct {
	ID           int64      `json:"id"`
	CredentialID string     `json:"credentialId"`
	Name         string     `json:"name"`
	CreatedAt    time.Time  `json:"createdAt"`
	LastUsedAt   *time.Time `json:"lastUsedAt"`
}

// Grant é o comprovante da confirmação.
type Grant struct {
	Token     string    `json:"token"`
	Method    string    `json:"method"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type rpInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type userInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
}

type credParam struct {
	Type string `json:"type"`
	Alg  int    `json:"alg"`
}

// CreationOptions alimenta o navegador.credentials.create (base64url).
type CreationOptions struct {
	ChallengeID        string      `json:"challengeId"`
	Challenge          string      `json:"challenge"`
	RP                 rpInfo      `json:"rp"`
	User               userInfo    `json:"user"`
	PubKeyCredParams   []credParam `json:"pubKeyCredParams"`
	TimeoutMS          int         `json:"timeout"`
	ExcludeCredentials []string    `json:"excludeCredentials"`
}

// RequestOptions alimenta o navigator.credentials.get (base64url).
type RequestOptions struct {
	ChallengeID      string   `json:"challengeId"`
	Challenge        string   `json:"challenge"`
	RPID             string   `json:"rpId"`
	TimeoutMS        int      `json:"timeout"`
	AllowCredentials []string `json:"allowCredentials"`
}

type Service struct {
	db        *database.DB
	rp        relyingParty
	rpName    string
	passwords Passwords

	mu       sync.Mutex
	failures map[uuid.UUID][]time.Time
	now      func() time.Time
}

func NewService(db *database.DB, cfg config.StepUp, passwords Passwords) *Service {
	return &Service{
		db: db, rp: relyingParty{id: cfg.RPID, origins: cfg.Origins}, rpName: "Farbo Rastreadores",
		passwords: passwords, failures: map[uuid.UUID][]time.Time{}, now: time.Now,
	}
}

// RPID é o domínio das credenciais (o app confere se pode usar a biometria).
func (s *Service) RPID() string { return s.rp.id }

func knownPurpose(purpose string) error {
	if !slices.Contains(purposes, purpose) {
		return ErrUnknownPurpose
	}
	return nil
}

// --- Senha ---------------------------------------------------------------------

// checkPassword confere a senha com teto de tentativas por usuário.
func (s *Service) checkPassword(ctx context.Context, userID uuid.UUID, password string) error {
	s.mu.Lock()
	recent := s.recentFailures(userID)
	s.mu.Unlock()
	if len(recent) >= maxPasswordFailures {
		return ErrTooManyAttempts
	}
	ok, err := s.passwords.PasswordMatches(ctx, userID, password)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !ok {
		s.failures[userID] = append(s.recentFailures(userID), s.now())
		return ErrWrongPassword
	}
	delete(s.failures, userID)
	return nil
}

// recentFailures devolve (e guarda) só as falhas dentro da janela. Chame com
// o mutex.
func (s *Service) recentFailures(userID uuid.UUID) []time.Time {
	cutoff := s.now().Add(-failureWindow)
	kept := slices.DeleteFunc(s.failures[userID], func(at time.Time) bool { return at.Before(cutoff) })
	if len(kept) == 0 {
		delete(s.failures, userID)
		return nil
	}
	s.failures[userID] = kept
	return kept
}

// VerifyPassword confirma a ação com a senha da conta.
func (s *Service) VerifyPassword(ctx context.Context, userID uuid.UUID, purpose, password string) (*Grant, error) {
	if err := knownPurpose(purpose); err != nil {
		return nil, err
	}
	if err := s.checkPassword(ctx, userID, password); err != nil {
		return nil, err
	}
	return s.issueGrant(ctx, userID, purpose, MethodPassword)
}

// --- Cadastro da biometria -----------------------------------------------------

// RegistrationOptions começa o cadastro da biometria deste aparelho. Exige a
// senha: com só o token de acesso (roubado), ninguém cadastra a própria
// "biometria" na conta de outro.
func (s *Service) RegistrationOptions(ctx context.Context, userID uuid.UUID, email, name, password string) (*CreationOptions, error) {
	if err := s.checkPassword(ctx, userID, password); err != nil {
		return nil, err
	}
	creds, err := s.credentials(ctx, userID)
	if err != nil {
		return nil, err
	}
	if len(creds) >= maxCredentialsPerUser {
		return nil, ErrTooManyCredentials
	}
	id, challenge, err := s.newChallenge(ctx, userID, purposeRegister)
	if err != nil {
		return nil, err
	}
	exclude := make([]string, 0, len(creds))
	for _, c := range creds {
		exclude = append(exclude, c.CredentialID)
	}
	return &CreationOptions{
		ChallengeID: id.String(),
		Challenge:   b64(challenge),
		RP:          rpInfo{ID: s.rp.id, Name: s.rpName},
		User:        userInfo{ID: b64(userID[:]), Name: email, DisplayName: name},
		// ES256 primeiro (iPhone, Android); RS256 para o Windows Hello.
		PubKeyCredParams:   []credParam{{"public-key", AlgES256}, {"public-key", AlgRS256}},
		TimeoutMS:          int(time.Minute / time.Millisecond),
		ExcludeCredentials: exclude,
	}, nil
}

// Register guarda a biometria deste aparelho.
func (s *Service) Register(ctx context.Context, userID uuid.UUID, in RegistrationResponse) (*Credential, error) {
	challenge, err := s.takeChallenge(ctx, in.ChallengeID, userID, purposeRegister)
	if err != nil {
		return nil, err
	}
	credentialID, err := decode("credentialId", in.CredentialID)
	if err != nil {
		return nil, err
	}
	cdRaw, err := decode("clientDataJSON", in.ClientDataJSON)
	if err != nil {
		return nil, err
	}
	adRaw, err := decode("authenticatorData", in.AuthenticatorData)
	if err != nil {
		return nil, err
	}
	spki, err := decode("publicKey", in.PublicKey)
	if err != nil {
		return nil, err
	}
	if err := s.rp.checkClientData(cdRaw, "webauthn.create", challenge); err != nil {
		return nil, err
	}
	ad, err := s.rp.parseAuthData(adRaw, true)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(ad.credentialID, credentialID) {
		return nil, refuse("id da credencial diferente do autenticador")
	}
	if _, err := parsePublicKey(spki, in.Algorithm); err != nil {
		return nil, err
	}

	var c Credential
	err = s.db.QueryRow(ctx, `
		INSERT INTO webauthn_credentials (user_id, credential_id, public_key, algorithm, sign_count, name)
		SELECT $1, $2, $3, $4, $5, $6
		WHERE (SELECT count(*) FROM webauthn_credentials WHERE user_id = $1) < $7
		RETURNING id, created_at`,
		userID, credentialID, spki, in.Algorithm, int64(ad.signCount), cleanName(in.Name), maxCredentialsPerUser,
	).Scan(&c.ID, &c.CreatedAt)
	switch err := database.MapError(err); {
	case errors.Is(err, database.ErrConflict):
		return nil, ErrAlreadyRegistered
	case errors.Is(err, database.ErrNotFound):
		return nil, ErrTooManyCredentials
	case err != nil:
		return nil, err
	}
	c.CredentialID, c.Name = b64(credentialID), cleanName(in.Name)
	return &c, nil
}

func cleanName(name string) string {
	name = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, name))
	if name == "" {
		return "Aparelho"
	}
	if utf8.RuneCountInString(name) > maxNameLength {
		name = string([]rune(name)[:maxNameLength])
	}
	return name
}

// Credentials lista os aparelhos com biometria do usuário.
func (s *Service) Credentials(ctx context.Context, userID uuid.UUID) ([]Credential, error) {
	return s.credentials(ctx, userID)
}

func (s *Service) credentials(ctx context.Context, userID uuid.UUID) ([]Credential, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, credential_id, name, created_at, last_used_at
		FROM webauthn_credentials WHERE user_id = $1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	out := []Credential{}
	for rows.Next() {
		var (
			c   Credential
			raw []byte
		)
		if err := rows.Scan(&c.ID, &raw, &c.Name, &c.CreatedAt, &c.LastUsedAt); err != nil {
			return nil, database.MapError(err)
		}
		c.CredentialID = b64(raw)
		out = append(out, c)
	}
	return out, rows.Err()
}

// DeleteCredential remove a biometria de um aparelho.
func (s *Service) DeleteCredential(ctx context.Context, userID uuid.UUID, id int64) error {
	tag, err := s.db.Exec(ctx, `DELETE FROM webauthn_credentials WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return database.ErrNotFound
	}
	return nil
}

// --- Confirmação com a biometria ---------------------------------------------

// AssertionOptions começa a confirmação de uma ação com a biometria.
func (s *Service) AssertionOptions(ctx context.Context, userID uuid.UUID, purpose string) (*RequestOptions, error) {
	if err := knownPurpose(purpose); err != nil {
		return nil, err
	}
	creds, err := s.credentials(ctx, userID)
	if err != nil {
		return nil, err
	}
	if len(creds) == 0 {
		return nil, ErrUnknownCredential
	}
	id, challenge, err := s.newChallenge(ctx, userID, "assert:"+purpose)
	if err != nil {
		return nil, err
	}
	allow := make([]string, 0, len(creds))
	for _, c := range creds {
		allow = append(allow, c.CredentialID)
	}
	return &RequestOptions{
		ChallengeID: id.String(), Challenge: b64(challenge), RPID: s.rp.id,
		TimeoutMS: int(time.Minute / time.Millisecond), AllowCredentials: allow,
	}, nil
}

// VerifyAssertion confere a biometria e emite o comprovante.
func (s *Service) VerifyAssertion(ctx context.Context, userID uuid.UUID, purpose string, in AssertionResponse) (*Grant, error) {
	if err := knownPurpose(purpose); err != nil {
		return nil, err
	}
	if err := s.verifyAssertion(ctx, userID, "assert:"+purpose, in); err != nil {
		return nil, err
	}
	return s.issueGrant(ctx, userID, purpose, MethodBiometric)
}

// --- Entrar com a biometria ----------------------------------------------------

// ownerOf é o dono de uma credencial (o login ainda não sabe quem é).
func (s *Service) ownerOf(ctx context.Context, credentialID string) (uuid.UUID, error) {
	raw, err := decode("credentialId", credentialID)
	if err != nil {
		return uuid.Nil, err
	}
	var userID uuid.UUID
	err = s.db.QueryRow(ctx, `SELECT user_id FROM webauthn_credentials WHERE credential_id = $1`, raw).Scan(&userID)
	if errors.Is(database.MapError(err), database.ErrNotFound) {
		return uuid.Nil, ErrUnknownCredential
	}
	return userID, database.MapError(err)
}

// LoginOptions começa o login com a biometria deste aparelho: o app guarda
// qual credencial é dele e pede o desafio por ela.
func (s *Service) LoginOptions(ctx context.Context, credentialID string) (*RequestOptions, error) {
	userID, err := s.ownerOf(ctx, credentialID)
	if err != nil {
		return nil, err
	}
	id, challenge, err := s.newChallenge(ctx, userID, purposeLogin)
	if err != nil {
		return nil, err
	}
	return &RequestOptions{
		ChallengeID: id.String(), Challenge: b64(challenge), RPID: s.rp.id,
		TimeoutMS: int(time.Minute / time.Millisecond), AllowCredentials: []string{credentialID},
	}, nil
}

// VerifyLogin confere a biometria do login e diz de quem é a conta.
func (s *Service) VerifyLogin(ctx context.Context, in AssertionResponse) (uuid.UUID, error) {
	userID, err := s.ownerOf(ctx, in.CredentialID)
	if err != nil {
		return uuid.Nil, err
	}
	if err := s.verifyAssertion(ctx, userID, purposeLogin, in); err != nil {
		return uuid.Nil, err
	}
	return userID, nil
}

// verifyAssertion confere uma resposta do navigator.credentials.get: o
// desafio (de uso único, do usuário e da finalidade), a origem, o domínio, a
// verificação do usuário, a assinatura e o contador.
func (s *Service) verifyAssertion(ctx context.Context, userID uuid.UUID, challengePurpose string, in AssertionResponse) error {
	challenge, err := s.takeChallenge(ctx, in.ChallengeID, userID, challengePurpose)
	if err != nil {
		return err
	}
	credentialID, err := decode("credentialId", in.CredentialID)
	if err != nil {
		return err
	}
	cdRaw, err := decode("clientDataJSON", in.ClientDataJSON)
	if err != nil {
		return err
	}
	adRaw, err := decode("authenticatorData", in.AuthenticatorData)
	if err != nil {
		return err
	}
	signature, err := decode("signature", in.Signature)
	if err != nil {
		return err
	}

	var (
		id        int64
		spki      []byte
		alg       int
		signCount int64
	)
	err = s.db.QueryRow(ctx, `
		SELECT id, public_key, algorithm, sign_count FROM webauthn_credentials
		WHERE credential_id = $1 AND user_id = $2`, credentialID, userID,
	).Scan(&id, &spki, &alg, &signCount)
	if errors.Is(database.MapError(err), database.ErrNotFound) {
		return ErrUnknownCredential
	}
	if err != nil {
		return database.MapError(err)
	}

	if err := s.rp.checkClientData(cdRaw, "webauthn.get", challenge); err != nil {
		return err
	}
	ad, err := s.rp.parseAuthData(adRaw, false)
	if err != nil {
		return err
	}
	key, err := parsePublicKey(spki, alg)
	if err != nil {
		return err
	}
	if err := verifySignature(key, adRaw, cdRaw, signature); err != nil {
		return err
	}
	// Contador: quem usa (a maioria dos Android e chaves físicas) só sobe;
	// voltar ou repetir é sinal de credencial copiada. O iPhone manda sempre 0.
	if (ad.signCount != 0 || signCount != 0) && int64(ad.signCount) <= signCount {
		return refuse("contador do autenticador não avançou")
	}
	_, err = s.db.Exec(ctx, `
		UPDATE webauthn_credentials SET sign_count = $2, last_used_at = NOW() WHERE id = $1`,
		id, int64(ad.signCount))
	return database.MapError(err)
}

// --- Comprovante -------------------------------------------------------------

func (s *Service) issueGrant(ctx context.Context, userID uuid.UUID, purpose, method string) (*Grant, error) {
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		return nil, err
	}
	hash := sha256.Sum256(token)
	g := &Grant{Token: b64(token), Method: method}
	err := s.db.QueryRow(ctx, `
		INSERT INTO step_up_grants (token_hash, user_id, purpose, method, expires_at)
		VALUES ($1, $2, $3, $4, NOW() + make_interval(secs => $5))
		RETURNING expires_at`,
		hash[:], userID, purpose, method, GrantTTL.Seconds(),
	).Scan(&g.ExpiresAt)
	if err != nil {
		return nil, database.MapError(err)
	}
	// Faxina oportunista do que já expirou há tempo.
	_, _ = s.db.Exec(ctx, `DELETE FROM step_up_grants WHERE expires_at < NOW() - INTERVAL '1 day'`)
	return g, nil
}

// Consume gasta o comprovante: vale uma vez, para a ação e o usuário dele,
// dentro do prazo.
func (s *Service) Consume(ctx context.Context, userID uuid.UUID, purpose, token string) (string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != 32 {
		return "", ErrNoGrant
	}
	hash := sha256.Sum256(raw)
	var method string
	err = s.db.QueryRow(ctx, `
		UPDATE step_up_grants SET used_at = NOW()
		WHERE token_hash = $1 AND user_id = $2 AND purpose = $3 AND used_at IS NULL AND expires_at > NOW()
		RETURNING method`, hash[:], userID, purpose).Scan(&method)
	if errors.Is(database.MapError(err), database.ErrNotFound) {
		return "", ErrNoGrant
	}
	if err != nil {
		return "", database.MapError(err)
	}
	return method, nil
}

// --- Desafios ------------------------------------------------------------------

func (s *Service) newChallenge(ctx context.Context, userID uuid.UUID, purpose string) (uuid.UUID, []byte, error) {
	challenge := make([]byte, 32)
	if _, err := rand.Read(challenge); err != nil {
		return uuid.Nil, nil, err
	}
	var id uuid.UUID
	err := s.db.QueryRow(ctx, `
		INSERT INTO step_up_challenges (user_id, purpose, challenge, expires_at)
		VALUES ($1, $2, $3, NOW() + make_interval(secs => $4)) RETURNING id`,
		userID, purpose, challenge, challengeTTL.Seconds()).Scan(&id)
	if err != nil {
		return uuid.Nil, nil, database.MapError(err)
	}
	_, _ = s.db.Exec(ctx, `DELETE FROM step_up_challenges WHERE expires_at < NOW() - INTERVAL '1 day'`)
	return id, challenge, nil
}

// takeChallenge gasta o desafio (uso único, do usuário e da ação certos).
func (s *Service) takeChallenge(ctx context.Context, rawID string, userID uuid.UUID, purpose string) ([]byte, error) {
	id, err := uuid.Parse(rawID)
	if err != nil {
		return nil, refuse("desafio inválido")
	}
	var challenge []byte
	err = s.db.QueryRow(ctx, `
		UPDATE step_up_challenges SET used_at = NOW()
		WHERE id = $1 AND user_id = $2 AND purpose = $3 AND used_at IS NULL AND expires_at > NOW()
		RETURNING challenge`, id, userID, purpose).Scan(&challenge)
	if errors.Is(database.MapError(err), database.ErrNotFound) {
		return nil, refuse("desafio expirado ou já usado")
	}
	if err != nil {
		return nil, database.MapError(err)
	}
	return challenge, nil
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
