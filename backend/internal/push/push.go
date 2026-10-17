// Package push cuida das notificações no celular do app do cliente: as
// chaves VAPID da central, as inscrições de cada aparelho e a entrega.
package push

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/webpush"
)

const (
	// maxPerUser: aparelhos inscritos por usuário; o mais antigo sai.
	maxPerUser = 10
	// testInterval: uma notificação de teste por minuto por usuário.
	testInterval = time.Minute
)

var (
	// ErrDisabled: push desligado (PUSH_ENABLED=false).
	ErrDisabled = errors.New("notificações no celular desligadas no servidor")
	// ErrInvalidSubscription: o que veio do navegador não é uma inscrição válida.
	ErrInvalidSubscription = errors.New("inscrição de notificação inválida")
	// ErrTestTooSoon: limite de uma notificação de teste por minuto.
	ErrTestTooSoon = errors.New("aguarde um minuto para enviar outra notificação de teste")
)

// Notification é o que aparece no celular. O service worker do app lê este
// JSON (frontend/src/app/sw.ts).
type Notification struct {
	Title    string `json:"title"`
	Body     string `json:"body"`
	URL      string `json:"url"`
	Tag      string `json:"tag,omitempty"`
	Severity string `json:"severity,omitempty"`
	// Urgency e TTL vão para o serviço de push, não para o aparelho.
	Urgency string        `json:"-"`
	TTL     time.Duration `json:"-"`
	Topic   string        `json:"-"`
}

// Device é um aparelho inscrito, como a tela mostra.
type Device struct {
	ID            int64      `json:"id"`
	UserAgent     string     `json:"userAgent"`
	CreatedAt     time.Time  `json:"createdAt"`
	LastSuccessAt *time.Time `json:"lastSuccessAt"`
	// Endpoint vai para a tela só para ela saber se o aparelho atual está na
	// lista; não é segredo (sem as chaves ninguém lê o que vai por ele).
	Endpoint string `json:"endpoint"`
}

// Sender é o que o serviço precisa do webpush.Client (dublê nos testes).
type Sender interface {
	Send(ctx context.Context, sub webpush.Subscription, msg webpush.Message) error
	AllowedEndpoint(endpoint string) error
}

type Service struct {
	db        *database.DB
	enabled   bool
	publicKey string
	sender    Sender
	log       *slog.Logger

	mu       sync.Mutex
	lastTest map[uuid.UUID]time.Time
	now      func() time.Time
}

// NewService carrega (ou cria) as chaves VAPID. Com push desligado devolve
// um serviço que só responde ErrDisabled.
func NewService(ctx context.Context, db *database.DB, cfg config.Push, secret []byte, log *slog.Logger) (*Service, error) {
	s := &Service{db: db, enabled: cfg.Enabled, log: log.With("component", "push"),
		lastTest: map[uuid.UUID]time.Time{}, now: time.Now}
	if !cfg.Enabled {
		return s, nil
	}
	var (
		vapid *webpush.VAPID
		err   error
	)
	if cfg.VAPIDPrivateKey != "" {
		vapid, err = webpush.ParseVAPID(cfg.VAPIDPrivateKey, cfg.VAPIDSubject)
	} else {
		vapid, err = s.storedVAPID(ctx, cfg.VAPIDSubject, secret)
	}
	if err != nil {
		return nil, err
	}
	s.publicKey = vapid.PublicKey()
	s.sender = webpush.NewClient(vapid, cfg.ExtraHosts)
	return s, nil
}

// storedVAPID usa o par guardado no banco; na primeira subida gera um. Se o
// JWT_SECRET mudou, a chave guardada não abre mais: gera outra e apaga as
// inscrições, que estavam presas à chave antiga.
func (s *Service) storedVAPID(ctx context.Context, subject string, secret []byte) (*webpush.VAPID, error) {
	gcm, err := sealer(secret)
	if err != nil {
		return nil, err
	}
	fresh, err := webpush.GenerateVAPID(subject)
	if err != nil {
		return nil, err
	}
	sealed, err := seal(gcm, fresh.PrivateKey())
	if err != nil {
		return nil, err
	}
	// Várias instâncias subindo juntas: só a primeira grava.
	if _, err := s.db.Exec(ctx, `
		INSERT INTO push_config (public_key, private_key_sealed) VALUES ($1, $2)
		ON CONFLICT (id) DO NOTHING`, fresh.PublicKey(), sealed); err != nil {
		return nil, database.MapError(err)
	}
	var stored string
	if err := s.db.QueryRow(ctx, `SELECT private_key_sealed FROM push_config`).Scan(&stored); err != nil {
		return nil, database.MapError(err)
	}
	if private, err := open(gcm, stored); err == nil {
		return webpush.ParseVAPID(private, subject)
	}

	s.log.Warn("chave VAPID guardada não abre (JWT_SECRET trocado): gerando outra; os aparelhos precisam ligar as notificações de novo")
	if err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE push_config SET public_key = $1, private_key_sealed = $2, created_at = NOW()`,
			fresh.PublicKey(), sealed); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `DELETE FROM push_subscriptions`)
		return err
	}); err != nil {
		return nil, database.MapError(err)
	}
	return fresh, nil
}

// Enabled diz se há push no servidor.
func (s *Service) Enabled() bool { return s.enabled }

// PublicKey é a applicationServerKey para o navegador se inscrever.
func (s *Service) PublicKey() string { return s.publicKey }

// Subscribe grava (ou move para este usuário) a inscrição de um aparelho.
func (s *Service) Subscribe(ctx context.Context, userID uuid.UUID, sub webpush.Subscription, userAgent string) error {
	if !s.enabled {
		return ErrDisabled
	}
	if err := validate(s.sender, sub); err != nil {
		return err
	}
	if len(userAgent) > 300 {
		userAgent = userAgent[:300]
	}
	return database.MapError(pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		// O mesmo navegador usado por outra conta passa a ser desta.
		if _, err := tx.Exec(ctx, `
			INSERT INTO push_subscriptions (user_id, endpoint, p256dh, auth, user_agent)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (endpoint) DO UPDATE SET user_id = EXCLUDED.user_id, p256dh = EXCLUDED.p256dh,
			    auth = EXCLUDED.auth, user_agent = EXCLUDED.user_agent, created_at = NOW()`,
			userID, sub.Endpoint, sub.P256dh, sub.Auth, userAgent); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			DELETE FROM push_subscriptions WHERE user_id = $1 AND id NOT IN (
			    SELECT id FROM push_subscriptions WHERE user_id = $1 ORDER BY created_at DESC LIMIT $2)`,
			userID, maxPerUser)
		return err
	}))
}

func validate(sender Sender, sub webpush.Subscription) error {
	if sender == nil || sender.AllowedEndpoint(sub.Endpoint) != nil || len(sub.Endpoint) > 2048 {
		return fmt.Errorf("%w: endereço de push não reconhecido", ErrInvalidSubscription)
	}
	key, err := decode(sub.P256dh)
	if err != nil {
		return fmt.Errorf("%w: chave p256dh", ErrInvalidSubscription)
	}
	if _, err := ecdh.P256().NewPublicKey(key); err != nil {
		return fmt.Errorf("%w: chave p256dh", ErrInvalidSubscription)
	}
	if auth, err := decode(sub.Auth); err != nil || len(auth) != 16 {
		return fmt.Errorf("%w: segredo auth", ErrInvalidSubscription)
	}
	return nil
}

// Unsubscribe apaga a inscrição de um aparelho do usuário.
func (s *Service) Unsubscribe(ctx context.Context, userID uuid.UUID, endpoint string) error {
	_, err := s.db.Exec(ctx, `DELETE FROM push_subscriptions WHERE user_id = $1 AND endpoint = $2`, userID, endpoint)
	return database.MapError(err)
}

// Devices lista os aparelhos inscritos do usuário.
func (s *Service) Devices(ctx context.Context, userID uuid.UUID) ([]Device, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, user_agent, created_at, last_success_at, endpoint
		FROM push_subscriptions WHERE user_id = $1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	out := []Device{}
	for rows.Next() {
		var d Device
		if err := rows.Scan(&d.ID, &d.UserAgent, &d.CreatedAt, &d.LastSuccessAt, &d.Endpoint); err != nil {
			return nil, database.MapError(err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// Notify guarda a notificação na área do app (mesmo com o push desligado ou
// sem aparelho inscrito), entrega a todos os aparelhos do usuário e devolve
// em quantos chegou ao serviço de push. Inscrição morta é apagada.
func (s *Service) Notify(ctx context.Context, userID uuid.UUID, n Notification) (int, error) {
	if err := s.record(ctx, userID, n); err != nil {
		s.log.Warn("falha ao guardar a notificação na área do app", "user", userID, "err", err)
	}
	return s.deliver(ctx, userID, n)
}

// deliver entrega a notificação aos aparelhos do usuário.
func (s *Service) deliver(ctx context.Context, userID uuid.UUID, n Notification) (int, error) {
	if !s.enabled {
		return 0, nil
	}
	payload, err := json.Marshal(n)
	if err != nil {
		return 0, err
	}
	rows, err := s.db.Query(ctx, `SELECT id, endpoint, p256dh, auth FROM push_subscriptions WHERE user_id = $1`, userID)
	if err != nil {
		return 0, database.MapError(err)
	}
	type target struct {
		id  int64
		sub webpush.Subscription
	}
	targets := []target{}
	for rows.Next() {
		var t target
		if err := rows.Scan(&t.id, &t.sub.Endpoint, &t.sub.P256dh, &t.sub.Auth); err != nil {
			rows.Close()
			return 0, database.MapError(err)
		}
		targets = append(targets, t)
	}
	rows.Close()

	delivered := 0
	var lastErr error
	for _, t := range targets {
		err := s.sender.Send(ctx, t.sub, webpush.Message{Payload: payload, TTL: n.TTL, Urgency: n.Urgency, Topic: n.Topic})
		switch {
		case errors.Is(err, webpush.ErrGone), errors.Is(err, webpush.ErrEndpoint):
			// Desinstalou, revogou ou expirou: não adianta insistir.
			if _, delErr := s.db.Exec(ctx, `DELETE FROM push_subscriptions WHERE id = $1`, t.id); delErr != nil {
				s.log.Warn("falha ao apagar inscrição de push morta", "err", delErr)
			}
		case err != nil:
			lastErr = err
			s.log.Warn("notificação não entregue ao serviço de push", "subscription", t.id, "err", err)
		default:
			delivered++
			if _, err := s.db.Exec(ctx, `UPDATE push_subscriptions SET last_success_at = NOW() WHERE id = $1`, t.id); err != nil {
				s.log.Warn("falha ao registrar entrega de push", "err", err)
			}
		}
	}
	if delivered == 0 && lastErr != nil {
		return 0, lastErr
	}
	return delivered, nil
}

// SendTest manda uma notificação de teste para os aparelhos do usuário.
func (s *Service) SendTest(ctx context.Context, userID uuid.UUID, url string) (int, error) {
	if !s.enabled {
		return 0, ErrDisabled
	}
	s.mu.Lock()
	if last, ok := s.lastTest[userID]; ok && s.now().Sub(last) < testInterval {
		s.mu.Unlock()
		return 0, ErrTestTooSoon
	}
	s.lastTest[userID] = s.now()
	s.mu.Unlock()
	// O teste vai só para o celular: não é um aviso para guardar.
	return s.deliver(ctx, userID, Notification{
		Title: "Notificações ligadas", Body: "É assim que os alertas dos seus veículos vão chegar neste celular.",
		URL: url, Tag: "teste", Severity: "info", Urgency: webpush.UrgencyNormal, TTL: 10 * time.Minute,
	})
}

// ---------------------------------------------------------------------------
// Chave privada guardada no banco
// ---------------------------------------------------------------------------

func sealer(secret []byte) (cipher.AEAD, error) {
	key := sha256.Sum256(append([]byte("farbo:push-vapid:"), secret...))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func seal(gcm cipher.AEAD, plain string) (string, error) {
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return "v1:" + base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(plain), nil)), nil
}

func open(gcm cipher.AEAD, sealed string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(sealed, "v1:"))
	if err != nil || len(raw) < gcm.NonceSize() {
		return "", errors.New("chave guardada ilegível")
	}
	plain, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
	if err != nil {
		return "", errors.New("chave guardada ilegível")
	}
	return string(plain), nil
}

func decode(v string) ([]byte, error) {
	v = strings.TrimRight(strings.TrimSpace(v), "=")
	v = strings.NewReplacer("+", "-", "/", "_").Replace(v)
	return base64.RawURLEncoding.DecodeString(v)
}
