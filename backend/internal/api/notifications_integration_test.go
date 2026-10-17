package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/push"
)

// A área de notificações pelo app: a pessoa vê só as dela, marca uma como
// lida (abrindo) e depois todas. Precisa de FARBO_TEST_DATABASE_URL.
func TestNotificationsInbox(t *testing.T) {
	var pushSvc *push.Service
	env, _ := newLeadsEnv(t, func(d *Deps) {
		var err error
		if pushSvc, err = push.NewService(context.Background(), d.DB, config.Push{Enabled: false},
			[]byte("segredo-jwt-de-teste-com-mais-de-32-bytes"), d.Log); err != nil {
			t.Fatal(err)
		}
		d.Push = pushSvc
	})
	ctx := context.Background()
	authSvc := auth.NewService(auth.NewRepository(env.db), config.Auth{BcryptCost: bcrypt.MinCost}, nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	ana, err := authSvc.CreateUser(ctx, "ana@notif.test", "Ana", auth.RoleCustomer, userPassword)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authSvc.CreateUser(ctx, "bia@notif.test", "Bia", auth.RoleCustomer, userPassword); err != nil {
		t.Fatal(err)
	}
	for _, title := range []string{"Ignição ligada", "Bateria desconectada"} {
		if _, err := pushSvc.Notify(ctx, ana.ID, push.Notification{Title: title, Body: "Moto da Ana", URL: "/app/veiculos/1", Severity: "warning"}); err != nil {
			t.Fatal(err)
		}
	}
	token := env.login("ana@notif.test")
	inbox := func(token string) push.Inbox {
		t.Helper()
		var out push.Inbox
		_ = json.Unmarshal(env.must(token, http.MethodGet, "/api/me/notifications", nil, http.StatusOK), &out)
		return out
	}
	got := inbox(token)
	if got.Unread != 2 || len(got.Items) != 2 || got.Items[0].Title != "Bateria desconectada" {
		t.Fatalf("a área da Ana = %+v", got)
	}
	if other := inbox(env.login("bia@notif.test")); other.Unread != 0 || len(other.Items) != 0 {
		t.Errorf("a Bia vê = %+v", other)
	}
	env.must(token, http.MethodPost, "/api/me/notifications/read", map[string]any{"id": got.Items[0].ID}, http.StatusNoContent)
	if got = inbox(token); got.Unread != 1 || got.Items[0].ReadAt == nil {
		t.Errorf("depois de abrir uma = %+v", got)
	}
	env.must(token, http.MethodPost, "/api/me/notifications/read", nil, http.StatusNoContent)
	if got = inbox(token); got.Unread != 0 {
		t.Errorf("depois de ler todas = %+v", got)
	}
	env.must("", http.MethodGet, "/api/me/notifications", nil, http.StatusUnauthorized)
}
