package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Sair dos outros aparelhos e desconectar todas as sessões: vale na hora,
// também para o token de acesso (15 minutos) que já estava no outro aparelho.
func TestSignOutOthersAndAll(t *testing.T) {
	ctx := context.Background()
	customer := &User{ID: uuid.New(), Email: "jair@cliente.test", Role: RoleCustomer, Active: true}
	store := newMemorySessionStore(customer)
	svc := serviceWithSecret(t, newSecret, store)

	status := func(access string) int {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer "+access)
		rec := httptest.NewRecorder()
		svc.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if p, ok := FromContext(r.Context()); !ok || p.SessionID == uuid.Nil {
				t.Errorf("o aparelho não veio no principal: %+v", p)
			}
			w.WriteHeader(http.StatusNoContent)
		})).ServeHTTP(rec, req)
		return rec.Code
	}
	sessionOf := func(access string) uuid.UUID {
		t.Helper()
		claims, err := svc.Parse(access)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := uuid.Parse(claims.SessionID)
		return id
	}

	// Dois aparelhos: o celular dele e um Safari que ele não reconhece.
	mine, err := svc.issue(ctx, customer, "iPhone · app")
	if err != nil {
		t.Fatal(err)
	}
	other, err := svc.issue(ctx, customer, "iPhone · Safari")
	if err != nil {
		t.Fatal(err)
	}
	if status(mine.AccessToken) != http.StatusNoContent || status(other.AccessToken) != http.StatusNoContent {
		t.Fatal("antes do corte, os dois entram")
	}
	list, err := svc.Sessions(ctx, customer.ID, sessionOf(mine.AccessToken))
	if err != nil || len(list) != 2 {
		t.Fatalf("sessões = %+v %v", list, err)
	}
	current := 0
	for _, s := range list {
		if s.Current {
			current++
			if s.UserAgent != "iPhone · app" {
				t.Errorf("o atual é %q", s.UserAgent)
			}
		}
	}
	if current != 1 {
		t.Errorf("atuais = %d", current)
	}

	// O corte é no segundo cheio: os tokens de antes precisam ser de um
	// segundo anterior.
	time.Sleep(1100 * time.Millisecond)
	fresh, revoked, err := svc.SignOutOthers(ctx, customer.ID, sessionOf(mine.AccessToken))
	if err != nil || revoked != 1 {
		t.Fatalf("sair dos outros: %d %v", revoked, err)
	}
	if status(other.AccessToken) != http.StatusUnauthorized {
		t.Error("o outro aparelho continuou entrando com o token de acesso")
	}
	if _, err := svc.Refresh(ctx, other.RefreshToken, ""); err == nil {
		t.Error("o outro aparelho renovou a sessão")
	}
	if status(fresh.AccessToken) != http.StatusNoContent || sessionOf(fresh.AccessToken) != sessionOf(mine.AccessToken) {
		t.Error("este aparelho saiu junto")
	}
	if renewed, err := svc.Refresh(ctx, mine.RefreshToken, "iPhone · app"); err != nil || status(renewed.AccessToken) != http.StatusNoContent {
		t.Errorf("este aparelho não renova: %v", err)
	} else {
		mine = renewed
	}

	// A central desconecta todas.
	time.Sleep(1100 * time.Millisecond)
	if n, err := svc.SignOutAll(ctx, customer.ID); err != nil || n != 1 {
		t.Fatalf("desconectar todas: %d %v", n, err)
	}
	if status(mine.AccessToken) != http.StatusUnauthorized {
		t.Error("depois de desconectar todas, o token ainda entra")
	}
	if list, _ := svc.Sessions(ctx, customer.ID, uuid.Nil); len(list) != 0 {
		t.Errorf("sobraram sessões: %+v", list)
	}
	// Entrar de novo funciona.
	again, err := svc.issue(ctx, customer, "iPhone · app")
	if err != nil || status(again.AccessToken) != http.StatusNoContent {
		t.Errorf("entrar de novo: %v", err)
	}
}
