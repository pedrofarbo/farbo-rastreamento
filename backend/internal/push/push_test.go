package push

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/webpush"
)

func testDB(t *testing.T) *database.DB {
	t.Helper()
	dsn := os.Getenv("FARBO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("defina FARBO_TEST_DATABASE_URL (Postgres descartável) para rodar o teste com banco")
	}
	ctx := context.Background()
	schema := "test_push_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
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

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

// fakeSender aceita o serviço de push "push.test" e responde por endpoint.
type fakeSender struct {
	mu     sync.Mutex
	sent   []string
	status map[string]error
}

func (f *fakeSender) AllowedEndpoint(endpoint string) error {
	if strings.HasPrefix(endpoint, "https://push.test/") {
		return nil
	}
	return webpush.ErrEndpoint
}

func (f *fakeSender) Send(_ context.Context, sub webpush.Subscription, _ webpush.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, sub.Endpoint)
	return f.status[sub.Endpoint]
}

func subscription(t *testing.T, endpoint string) webpush.Subscription {
	t.Helper()
	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	_, _ = rand.Read(auth)
	return webpush.Subscription{Endpoint: endpoint,
		P256dh: base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()),
		Auth:   base64.RawURLEncoding.EncodeToString(auth)}
}

func newUser(t *testing.T, db *database.DB, email string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := db.QueryRow(context.Background(),
		`INSERT INTO users (email, name, role, password_hash) VALUES ($1, 'x', 'customer', 'x') RETURNING id`, email).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestVAPIDKeyIsStableAndRotatesWithSecret(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	cfg := config.Push{Enabled: true, VAPIDSubject: "mailto:central@farbo.test"}
	secret := []byte("segredo-jwt-de-teste-com-mais-de-32-bytes")

	first, err := NewService(ctx, db, cfg, secret, quiet)
	if err != nil {
		t.Fatal(err)
	}
	again, err := NewService(ctx, db, cfg, secret, quiet)
	if err != nil || again.PublicKey() != first.PublicKey() || first.PublicKey() == "" {
		t.Fatalf("reiniciar com o mesmo segredo mantém a chave: %q × %q (%v)", first.PublicKey(), again.PublicKey(), err)
	}
	var sealed string
	_ = db.QueryRow(ctx, `SELECT private_key_sealed FROM push_config`).Scan(&sealed)
	if !strings.HasPrefix(sealed, "v1:") || strings.Contains(sealed, first.PublicKey()) {
		t.Error("a chave privada fica cifrada no banco")
	}

	user := newUser(t, db, "ana@push.test")
	first.sender = &fakeSender{}
	if err := first.Subscribe(ctx, user, subscription(t, "https://push.test/a"), "Chrome"); err != nil {
		t.Fatal(err)
	}
	rotated, err := NewService(ctx, db, cfg, []byte("outro-segredo-jwt-trocado-com-32-bytes-ou-mais"), quiet)
	if err != nil {
		t.Fatal(err)
	}
	if rotated.PublicKey() == first.PublicKey() {
		t.Error("JWT_SECRET trocado: a chave guardada não abre e uma nova é gerada")
	}
	if devices, _ := rotated.Devices(ctx, user); len(devices) != 0 {
		t.Error("as inscrições presas à chave antiga são apagadas")
	}

	fixed, err := NewService(ctx, db, config.Push{Enabled: true, VAPIDPrivateKey: "yfWPiYE-n46HLnH0KqZOF1fJJU3MYrct3AELtAQ-oRw"}, secret, quiet)
	if err != nil || fixed.PublicKey() != "BP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27mlmlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A8" {
		t.Errorf("VAPID_PRIVATE_KEY informada é usada como está: %q %v", fixed.PublicKey(), err)
	}

	off, err := NewService(ctx, db, config.Push{Enabled: false}, secret, quiet)
	if err != nil || off.Enabled() {
		t.Fatal("PUSH_ENABLED=false")
	}
	if err := off.Subscribe(ctx, user, subscription(t, "https://push.test/b"), ""); !errors.Is(err, ErrDisabled) {
		t.Errorf("desligado recusa inscrição: %v", err)
	}
}

func TestSubscriptionsAndDelivery(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	svc, err := NewService(ctx, db, config.Push{Enabled: true, VAPIDSubject: "mailto:x@farbo.test"}, []byte("segredo-jwt-de-teste-com-mais-de-32-bytes"), quiet)
	if err != nil {
		t.Fatal(err)
	}
	sender := &fakeSender{status: map[string]error{}}
	svc.sender = sender
	ana, bia := newUser(t, db, "ana@push.test"), newUser(t, db, "bia@push.test")

	for name, sub := range map[string]webpush.Subscription{
		"endereço fora dos serviços de push": subscription(t, "http://127.0.0.1:8080/api/admin"),
		"p256dh que não é ponto":             {Endpoint: "https://push.test/x", P256dh: "AAAA", Auth: "BTBZMqHH6r4Tts7J_aSIgg"},
		"auth curto":                         {Endpoint: "https://push.test/x", P256dh: subscription(t, "x").P256dh, Auth: "AAAA"},
	} {
		if err := svc.Subscribe(ctx, ana, sub, ""); !errors.Is(err, ErrInvalidSubscription) {
			t.Errorf("%s: devia ser recusada, veio %v", name, err)
		}
	}

	shared := subscription(t, "https://push.test/celular-compartilhado")
	if err := svc.Subscribe(ctx, ana, shared, "Chrome no Android"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Subscribe(ctx, bia, shared, "Chrome no Android"); err != nil {
		t.Fatal(err)
	}
	if list, _ := svc.Devices(ctx, ana); len(list) != 0 {
		t.Error("o mesmo navegador usado por outra conta passa a ser dela")
	}
	if list, _ := svc.Devices(ctx, bia); len(list) != 1 || list[0].UserAgent != "Chrome no Android" {
		t.Errorf("aparelhos da Bia: %+v", list)
	}

	for i := range 12 {
		if err := svc.Subscribe(ctx, ana, subscription(t, fmt.Sprintf("https://push.test/ana-%02d", i)), ""); err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * time.Millisecond)
	}
	list, _ := svc.Devices(ctx, ana)
	if len(list) != 10 || list[0].Endpoint != "https://push.test/ana-11" {
		t.Errorf("no máximo 10 aparelhos, os mais novos: %d, primeiro %s", len(list), list[0].Endpoint)
	}

	sender.status["https://push.test/ana-11"] = webpush.ErrGone
	sender.status["https://push.test/ana-10"] = errors.New("503")
	delivered, err := svc.Notify(ctx, ana, Notification{Title: "SOS", Body: "x", URL: "/app/"})
	if err != nil || delivered != 8 {
		t.Fatalf("entregue a 8 dos 10 (1 morto, 1 com erro temporário): %d %v", delivered, err)
	}
	list, _ = svc.Devices(ctx, ana)
	if len(list) != 9 {
		t.Errorf("a inscrição morta é apagada; a com erro temporário fica: %d", len(list))
	}
	var withSuccess int
	_ = db.QueryRow(ctx, `SELECT count(*) FROM push_subscriptions WHERE user_id = $1 AND last_success_at IS NOT NULL`, ana).Scan(&withSuccess)
	if withSuccess != 8 {
		t.Errorf("última entrega registrada em 8, veio %d", withSuccess)
	}

	if err := svc.Unsubscribe(ctx, ana, "https://push.test/ana-09"); err != nil {
		t.Fatal(err)
	}
	if list, _ := svc.Devices(ctx, ana); len(list) != 8 {
		t.Errorf("desligar tira o aparelho: %d", len(list))
	}

	if n, err := svc.SendTest(ctx, bia, "/app/alertas"); err != nil || n != 1 {
		t.Fatalf("teste: %d %v", n, err)
	}
	if _, err := svc.SendTest(ctx, bia, "/app/alertas"); !errors.Is(err, ErrTestTooSoon) {
		t.Errorf("segundo teste no mesmo minuto: %v", err)
	}
	if n, err := svc.Notify(ctx, newUser(t, db, "sem@push.test"), Notification{Title: "x"}); err != nil || n != 0 {
		t.Errorf("usuário sem aparelhos: 0 entregas, sem erro: %d %v", n, err)
	}
}

// A área de notificações do app: o Notify guarda (até com o push desligado),
// o teste não; a lista vem da mais nova para a mais velha, com as não lidas;
// marcar uma ou todas como lidas só mexe nas da pessoa; as de mais de 90
// dias saem na limpeza.
func TestInbox(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	off, err := NewService(ctx, db, config.Push{Enabled: false}, []byte("segredo-jwt-de-teste-com-mais-de-32-bytes"), quiet)
	if err != nil {
		t.Fatal(err)
	}
	on, err := NewService(ctx, db, config.Push{Enabled: true, VAPIDSubject: "mailto:x@farbo.test"}, []byte("segredo-jwt-de-teste-com-mais-de-32-bytes"), quiet)
	if err != nil {
		t.Fatal(err)
	}
	on.sender = &fakeSender{status: map[string]error{}}
	ana, bia := newUser(t, db, "ana@inbox.test"), newUser(t, db, "bia@inbox.test")

	// Com o push desligado e sem aparelho: não entrega, mas guarda.
	if n, err := off.Notify(ctx, ana, Notification{Title: "Ignição ligada", Body: "Moto da Ana", URL: "/app/veiculos/1", Severity: "info"}); err != nil || n != 0 {
		t.Fatalf("push desligado: %d %v", n, err)
	}
	time.Sleep(2 * time.Millisecond)
	if _, err := on.Notify(ctx, ana, Notification{Title: "SOS", Body: "Moto da Ana", URL: "/app/veiculos/1", Severity: "critical"}); err != nil {
		t.Fatal(err)
	}
	if err := on.Subscribe(ctx, ana, subscription(t, "https://push.test/ana"), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := on.SendTest(ctx, ana, "/app/alertas"); err != nil {
		t.Fatal(err)
	}
	if _, err := on.Notify(ctx, bia, Notification{Title: "Fatura vence amanhã", URL: "/app/faturas", Severity: "warning"}); err != nil {
		t.Fatal(err)
	}

	inbox, err := on.Inbox(ctx, ana)
	if err != nil {
		t.Fatal(err)
	}
	if inbox.Unread != 2 || len(inbox.Items) != 2 || inbox.Items[0].Title != "SOS" || inbox.Items[0].Severity != "critical" ||
		inbox.Items[1].Title != "Ignição ligada" || inbox.Items[1].URL != "/app/veiculos/1" || inbox.Items[0].ReadAt != nil {
		t.Fatalf("a área da Ana (sem o teste) = %+v", inbox)
	}

	// A Bia não marca as da Ana; a Ana marca uma e depois todas.
	if err := on.MarkRead(ctx, bia, &inbox.Items[0].ID); err != nil {
		t.Fatal(err)
	}
	if again, _ := on.Inbox(ctx, ana); again.Unread != 2 {
		t.Errorf("a Bia marcou uma da Ana: %+v", again)
	}
	if err := on.MarkRead(ctx, ana, &inbox.Items[0].ID); err != nil {
		t.Fatal(err)
	}
	if again, _ := on.Inbox(ctx, ana); again.Unread != 1 || again.Items[0].ReadAt == nil || again.Items[1].ReadAt != nil {
		t.Errorf("depois de ler o SOS = %+v", again)
	}
	if err := on.MarkRead(ctx, ana, nil); err != nil {
		t.Fatal(err)
	}
	if again, _ := on.Inbox(ctx, ana); again.Unread != 0 {
		t.Errorf("depois de ler todas = %+v", again)
	}
	if other, _ := on.Inbox(ctx, bia); other.Unread != 1 || len(other.Items) != 1 {
		t.Errorf("a área da Bia = %+v", other)
	}

	// A limpeza: só as de mais de 90 dias.
	if _, err := db.Exec(ctx, `UPDATE user_notifications SET created_at = NOW() - interval '91 days' WHERE title = 'Ignição ligada'`); err != nil {
		t.Fatal(err)
	}
	if removed, err := on.PruneInbox(ctx); err != nil || removed != 1 {
		t.Fatalf("limpeza: %d %v", removed, err)
	}
	if again, _ := on.Inbox(ctx, ana); len(again.Items) != 1 || again.Items[0].Title != "SOS" {
		t.Errorf("depois da limpeza = %+v", again)
	}
}
