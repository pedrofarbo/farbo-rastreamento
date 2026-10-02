package stepup

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

const (
	testRP     = "farborastreadores.com.br"
	testOrigin = "https://app.farborastreadores.com.br"
)

// softAuthenticator faz o papel do Face ID: uma chave que assina como o
// autenticador de plataforma do aparelho.
type softAuthenticator struct {
	t      *testing.T
	ec     *ecdsa.PrivateKey
	rsa    *rsa.PrivateKey
	credID []byte
	count  uint32
	rpID   string
}

func newSoftAuthenticator(t *testing.T, alg int) *softAuthenticator {
	a := &softAuthenticator{t: t, credID: randomBytes(t, 16), rpID: testRP}
	var err error
	if alg == AlgRS256 {
		a.rsa, err = rsa.GenerateKey(rand.Reader, 2048)
	} else {
		a.ec, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	}
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func randomBytes(t *testing.T, n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

func (a *softAuthenticator) alg() int {
	if a.rsa != nil {
		return AlgRS256
	}
	return AlgES256
}

func (a *softAuthenticator) spki() []byte {
	var pub any
	if a.rsa != nil {
		pub = &a.rsa.PublicKey
	} else {
		pub = &a.ec.PublicKey
	}
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		a.t.Fatal(err)
	}
	return der
}

func (a *softAuthenticator) authData(flags byte, attested bool) []byte {
	h := sha256.Sum256([]byte(a.rpID))
	b := append(h[:], flags)
	b = binary.BigEndian.AppendUint32(b, a.count)
	if attested {
		b = append(b, make([]byte, 16)...) // AAGUID
		b = binary.BigEndian.AppendUint16(b, uint16(len(a.credID)))
		b = append(b, a.credID...)
		b = append(b, 0xa0) // chave COSE (o servidor usa a SPKI do navegador)
	}
	return b
}

func (a *softAuthenticator) sign(authData, clientDataJSON []byte) []byte {
	h := sha256.Sum256(clientDataJSON)
	digest := sha256.Sum256(append(append([]byte{}, authData...), h[:]...))
	if a.rsa != nil {
		sig, err := rsa.SignPKCS1v15(rand.Reader, a.rsa, crypto.SHA256, digest[:])
		if err != nil {
			a.t.Fatal(err)
		}
		return sig
	}
	sig, err := ecdsa.SignASN1(rand.Reader, a.ec, digest[:])
	if err != nil {
		a.t.Fatal(err)
	}
	return sig
}

func clientDataJSON(typ, challenge, origin string) []byte {
	raw, _ := json.Marshal(map[string]any{"type": typ, "challenge": challenge, "origin": origin, "crossOrigin": false})
	return raw
}

func (a *softAuthenticator) register(options *CreationOptions, origin string) RegistrationResponse {
	cd := clientDataJSON("webauthn.create", options.Challenge, origin)
	return RegistrationResponse{
		ChallengeID: options.ChallengeID, CredentialID: b64(a.credID), ClientDataJSON: b64(cd),
		AuthenticatorData: b64(a.authData(flagUserPresent|flagUserVerified|flagAttested, true)),
		PublicKey:         b64(a.spki()), Algorithm: a.alg(), Name: "iPhone",
	}
}

func (a *softAuthenticator) assert(options *RequestOptions, origin string, flags byte) AssertionResponse {
	cd := clientDataJSON("webauthn.get", options.Challenge, origin)
	ad := a.authData(flags, false)
	return AssertionResponse{
		ChallengeID: options.ChallengeID, CredentialID: b64(a.credID), ClientDataJSON: b64(cd),
		AuthenticatorData: b64(ad), Signature: b64(a.sign(ad, cd)),
	}
}

// --- Sem banco: as conferências do WebAuthn --------------------------------

func TestWebAuthnChecks(t *testing.T) {
	rp := relyingParty{id: testRP, origins: []string{testOrigin}}
	challenge := randomBytes(t, 32)
	ok := clientDataJSON("webauthn.get", b64(challenge), testOrigin)
	if err := rp.checkClientData(ok, "webauthn.get", challenge); err != nil {
		t.Fatalf("pedido válido recusado: %v", err)
	}
	for name, raw := range map[string][]byte{
		"tipo de cadastro no lugar de uso": clientDataJSON("webauthn.create", b64(challenge), testOrigin),
		"outro desafio":                    clientDataJSON("webauthn.get", b64(randomBytes(t, 32)), testOrigin),
		"site falso":                       clientDataJSON("webauthn.get", b64(challenge), "https://farbo-rastreadores.com"),
		"http":                             clientDataJSON("webauthn.get", b64(challenge), "http://app.farborastreadores.com.br"),
		"lixo":                             []byte("{"),
	} {
		if err := rp.checkClientData(raw, "webauthn.get", challenge); !errors.Is(err, ErrVerification) {
			t.Errorf("%s: devia recusar, veio %v", name, err)
		}
	}
	cross, _ := json.Marshal(map[string]any{"type": "webauthn.get", "challenge": b64(challenge), "origin": testOrigin, "crossOrigin": true})
	if err := rp.checkClientData(cross, "webauthn.get", challenge); err == nil {
		t.Error("dentro de iframe de outro site: devia recusar")
	}

	a := newSoftAuthenticator(t, AlgES256)
	if _, err := rp.parseAuthData(a.authData(flagUserPresent|flagUserVerified, false), false); err != nil {
		t.Fatalf("authData válido recusado: %v", err)
	}
	if _, err := rp.parseAuthData(a.authData(flagUserPresent, false), false); err == nil {
		t.Error("sem verificação do usuário (biometria): devia recusar")
	}
	a.rpID = "outro.com.br"
	if _, err := rp.parseAuthData(a.authData(flagUserPresent|flagUserVerified, false), false); err == nil {
		t.Error("credencial de outro domínio: devia recusar")
	}
	if _, err := rp.parseAuthData([]byte{1, 2, 3}, false); err == nil {
		t.Error("authData curto: devia recusar")
	}
}

func TestSignaturesES256AndRS256(t *testing.T) {
	for _, alg := range []int{AlgES256, AlgRS256} {
		a := newSoftAuthenticator(t, alg)
		key, err := parsePublicKey(a.spki(), alg)
		if err != nil {
			t.Fatalf("alg %d: %v", alg, err)
		}
		ad := a.authData(flagUserPresent|flagUserVerified, false)
		cd := clientDataJSON("webauthn.get", "x", testOrigin)
		sig := a.sign(ad, cd)
		if err := verifySignature(key, ad, cd, sig); err != nil {
			t.Fatalf("alg %d: assinatura válida recusada: %v", alg, err)
		}
		tampered := append([]byte{}, cd...)
		tampered[len(tampered)-2] ^= 1
		if err := verifySignature(key, ad, tampered, sig); err == nil {
			t.Errorf("alg %d: dados alterados passaram", alg)
		}
		if _, err := parsePublicKey(a.spki(), -alg); err == nil {
			t.Errorf("alg %d: chave com algoritmo trocado devia ser recusada", alg)
		}
	}
	p384, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	der, _ := x509.MarshalPKIXPublicKey(&p384.PublicKey)
	if _, err := parsePublicKey(der, AlgES256); err == nil {
		t.Error("curva fora do ES256 devia ser recusada")
	}
}

// --- Com banco: cadastro, uso, comprovante ----------------------------------

func stepUpDB(t *testing.T) *database.DB {
	t.Helper()
	dsn := os.Getenv("FARBO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("defina FARBO_TEST_DATABASE_URL (Postgres descartável) para rodar o teste com banco")
	}
	ctx := context.Background()
	schema := "test_stepup_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		_ = admin.Close(context.Background())
	})
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	db := &database.DB{Pool: pool}
	if err := db.Migrate(ctx, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatal(err)
	}
	return db
}

type fixedPasswords map[uuid.UUID]string

func (f fixedPasswords) PasswordMatches(_ context.Context, id uuid.UUID, password string) (bool, error) {
	return f[id] == password, nil
}

func TestStepUpEndToEnd(t *testing.T) {
	db := stepUpDB(t)
	ctx := context.Background()
	var ana, bia uuid.UUID
	for _, u := range []struct {
		email string
		id    *uuid.UUID
	}{{"ana@stepup.test", &ana}, {"bia@stepup.test", &bia}} {
		if err := db.QueryRow(ctx, `INSERT INTO users (email, name, role, password_hash) VALUES ($1, 'x', 'customer', 'x') RETURNING id`,
			u.email).Scan(u.id); err != nil {
			t.Fatal(err)
		}
	}
	svc := NewService(db, config.StepUp{RPID: testRP, Origins: []string{testOrigin, "https://painel.farborastreadores.com.br"}},
		fixedPasswords{ana: "senha-da-ana", bia: "senha-da-bia"})

	// --- Cadastro da biometria: exige a senha -------------------------------
	if _, err := svc.RegistrationOptions(ctx, ana, "ana@stepup.test", "Ana", "errada"); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("cadastro sem a senha certa: %v", err)
	}
	face := newSoftAuthenticator(t, AlgES256)
	options, err := svc.RegistrationOptions(ctx, ana, "ana@stepup.test", "Ana", "senha-da-ana")
	if err != nil || options.RP.ID != testRP || len(options.PubKeyCredParams) != 2 {
		t.Fatalf("opções de cadastro: %+v %v", options, err)
	}
	reg := face.register(options, testOrigin)
	cred, err := svc.Register(ctx, ana, reg)
	if err != nil || cred.Name != "iPhone" {
		t.Fatalf("cadastro: %+v %v", cred, err)
	}
	if _, err := svc.Register(ctx, ana, reg); err == nil {
		t.Fatal("o mesmo desafio não pode ser usado duas vezes")
	}
	options, _ = svc.RegistrationOptions(ctx, ana, "ana@stepup.test", "Ana", "senha-da-ana")
	if len(options.ExcludeCredentials) != 1 {
		t.Error("o cadastro seguinte exclui a biometria já cadastrada")
	}
	if _, err := svc.Register(ctx, bia, face.register(options, testOrigin)); err == nil {
		t.Error("desafio da Ana não serve para a Bia")
	}

	// --- Confirmação com a biometria -----------------------------------------
	grantWith := func(user uuid.UUID, a *softAuthenticator, origin string, flags byte) (*Grant, error) {
		req, err := svc.AssertionOptions(ctx, user, PurposeEngineCut)
		if err != nil {
			return nil, err
		}
		return svc.VerifyAssertion(ctx, user, PurposeEngineCut, a.assert(req, origin, flags))
	}
	uv := byte(flagUserPresent | flagUserVerified)
	grant, err := grantWith(ana, face, testOrigin, uv)
	if err != nil || grant.Method != MethodBiometric || time.Until(grant.ExpiresAt) > GrantTTL+time.Minute {
		t.Fatalf("biometria válida: %+v %v", grant, err)
	}
	if _, err := grantWith(ana, face, "https://painel.farborastreadores.com.br", uv); err != nil {
		t.Errorf("do painel (mesmo domínio) também vale: %v", err)
	}
	for name, try := range map[string]func() error{
		"site falso": func() error { _, err := grantWith(ana, face, "https://farbo-falso.com", uv); return err },
		"sem verificar o usuário": func() error {
			_, err := grantWith(ana, face, testOrigin, flagUserPresent)
			return err
		},
		"outra chave com o mesmo id": func() error {
			other := newSoftAuthenticator(t, AlgES256)
			other.credID = face.credID
			_, err := grantWith(ana, other, testOrigin, uv)
			return err
		},
		"Bia usando a biometria da Ana": func() error { _, err := grantWith(bia, face, testOrigin, uv); return err },
	} {
		if err := try(); err == nil {
			t.Errorf("%s: devia recusar", name)
		}
	}
	req, _ := svc.AssertionOptions(ctx, ana, PurposeEngineCut)
	assertion := face.assert(req, testOrigin, uv)
	if _, err := svc.VerifyAssertion(ctx, ana, PurposeEngineCut, assertion); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.VerifyAssertion(ctx, ana, PurposeEngineCut, assertion); err == nil {
		t.Error("a mesma resposta não pode ser repetida")
	}

	// Contador: quem usa precisa subir; voltar é credencial copiada.
	android := newSoftAuthenticator(t, AlgRS256)
	android.count = 5
	opts, _ := svc.RegistrationOptions(ctx, ana, "ana@stepup.test", "Ana", "senha-da-ana")
	if _, err := svc.Register(ctx, ana, android.register(opts, testOrigin)); err != nil {
		t.Fatal(err)
	}
	android.count = 6
	if _, err := grantWith(ana, android, testOrigin, uv); err != nil {
		t.Fatalf("contador subiu: %v", err)
	}
	if _, err := grantWith(ana, android, testOrigin, uv); err == nil {
		t.Error("contador repetido: devia recusar")
	}

	// --- Comprovante: uma vez, da ação e do usuário dele ------------------------
	grant, _ = grantWith(ana, face, testOrigin, uv)
	if _, err := svc.Consume(ctx, bia, PurposeEngineCut, grant.Token); !errors.Is(err, ErrNoGrant) {
		t.Error("comprovante da Ana não vale para a Bia")
	}
	if method, err := svc.Consume(ctx, ana, PurposeEngineCut, grant.Token); err != nil || method != MethodBiometric {
		t.Fatalf("comprovante válido: %q %v", method, err)
	}
	if _, err := svc.Consume(ctx, ana, PurposeEngineCut, grant.Token); !errors.Is(err, ErrNoGrant) {
		t.Error("comprovante já usado")
	}
	if _, err := svc.Consume(ctx, ana, PurposeEngineCut, ""); !errors.Is(err, ErrNoGrant) {
		t.Error("sem comprovante")
	}
	grant, _ = grantWith(ana, face, testOrigin, uv)
	if _, err := db.Exec(ctx, `UPDATE step_up_grants SET expires_at = NOW() - INTERVAL '1 second'`); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Consume(ctx, ana, PurposeEngineCut, grant.Token); !errors.Is(err, ErrNoGrant) {
		t.Error("comprovante vencido")
	}

	// --- Senha: o plano B, com teto de tentativas ------------------------------------
	if g, err := svc.VerifyPassword(ctx, bia, PurposeEngineCut, "senha-da-bia"); err != nil || g.Method != MethodPassword {
		t.Fatalf("senha certa: %+v %v", g, err)
	}
	if _, err := svc.VerifyPassword(ctx, bia, "outra_acao", "senha-da-bia"); !errors.Is(err, ErrUnknownPurpose) {
		t.Error("ação desconhecida")
	}
	if _, err := svc.AssertionOptions(ctx, bia, PurposeEngineCut); !errors.Is(err, ErrUnknownCredential) {
		t.Error("sem biometria cadastrada: o app vai direto para a senha")
	}
	for range maxPasswordFailures {
		if _, err := svc.VerifyPassword(ctx, bia, PurposeEngineCut, "chute"); !errors.Is(err, ErrWrongPassword) {
			t.Fatalf("senha errada: %v", err)
		}
	}
	if _, err := svc.VerifyPassword(ctx, bia, PurposeEngineCut, "senha-da-bia"); !errors.Is(err, ErrTooManyAttempts) {
		t.Error("depois de 5 erros, nem a senha certa passa por um tempo")
	}
	svc.now = func() time.Time { return time.Now().Add(failureWindow + time.Minute) }
	if _, err := svc.VerifyPassword(ctx, bia, PurposeEngineCut, "senha-da-bia"); err != nil {
		t.Errorf("passada a janela, volta a aceitar: %v", err)
	}

	// --- Entrar com a biometria (login do app) ----------------------------------------
	login, err := svc.LoginOptions(ctx, b64(face.credID))
	if err != nil || len(login.AllowCredentials) != 1 || login.RPID != testRP {
		t.Fatalf("opções de login: %+v %v", login, err)
	}
	who, err := svc.VerifyLogin(ctx, face.assert(login, testOrigin, uv))
	if err != nil || who != ana {
		t.Fatalf("login com a biometria da Ana: %v %v", who, err)
	}
	loginAgain := face.assert(login, testOrigin, uv)
	if _, err := svc.VerifyLogin(ctx, loginAgain); err == nil {
		t.Error("o desafio do login vale uma vez só")
	}
	if _, err := svc.LoginOptions(ctx, b64(randomBytes(t, 16))); !errors.Is(err, ErrUnknownCredential) {
		t.Errorf("credencial desconhecida: %v", err)
	}
	// O desafio de confirmação (bloqueio) não serve para entrar, e vice-versa.
	cutChallenge, _ := svc.AssertionOptions(ctx, ana, PurposeEngineCut)
	if _, err := svc.VerifyLogin(ctx, face.assert(cutChallenge, testOrigin, uv)); err == nil {
		t.Error("desafio do bloqueio não abre sessão")
	}
	login, _ = svc.LoginOptions(ctx, b64(face.credID))
	if _, err := svc.VerifyAssertion(ctx, ana, PurposeEngineCut, face.assert(login, testOrigin, uv)); err == nil {
		t.Error("desafio do login não confirma o bloqueio")
	}
	login, _ = svc.LoginOptions(ctx, b64(face.credID))
	if _, err := svc.VerifyLogin(ctx, face.assert(login, testOrigin, flagUserPresent)); err == nil {
		t.Error("login sem verificar o usuário (rosto, digital): recusado")
	}

	// --- Remover a biometria ---------------------------------------------------------
	creds, _ := svc.Credentials(ctx, ana)
	if len(creds) != 2 {
		t.Fatalf("a Ana tem 2 aparelhos: %+v", creds)
	}
	if err := svc.DeleteCredential(ctx, bia, creds[0].ID); err == nil {
		t.Error("a Bia não remove a biometria da Ana")
	}
	if err := svc.DeleteCredential(ctx, ana, creds[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := grantWith(ana, face, testOrigin, uv); err == nil {
		t.Error("biometria removida não confirma mais")
	}
	if _, err := svc.LoginOptions(ctx, b64(face.credID)); !errors.Is(err, ErrUnknownCredential) {
		t.Error("biometria removida não entra mais")
	}
}
