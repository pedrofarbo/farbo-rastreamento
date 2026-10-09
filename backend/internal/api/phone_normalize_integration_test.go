package api

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/phone"
	"github.com/pedrofarbo/farbo-rastreamento/backend/migrations"
)

// O telefone do cliente num formato só, "(11) 9-8888-7777": a migration
// acerta os que já estavam gravados (dá o mesmo que phone.Format) e toda
// gravação nova sai normalizada — o cadastro, a ficha e o Meus dados.
// Precisa de FARBO_TEST_DATABASE_URL.
func TestPhonesNormalized(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()

	// Os formatos que havia em produção, gravados crus (sem passar pelo cadastro).
	raws := []string{"(11) 98888-7777", "11988887777", "+55 11 98888-7777", "(11) 8888-7777", "(11) 3333-4444", "551133334444", "ramal 12", ""}
	ids := make([]string, len(raws))
	for i, raw := range raws {
		if err := db.QueryRow(ctx, `INSERT INTO users (email, name, role, phone, password_hash) VALUES ($1, 'x', 'customer', $2, 'x') RETURNING id`,
			"cru"+string(rune('a'+i))+"@telefone.test", raw).Scan(&ids[i]); err != nil {
			t.Fatal(err)
		}
	}
	sql, err := migrations.FS.ReadFile("0050_normalize_phones.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, string(sql)); err != nil {
		t.Fatal(err)
	}
	for i, raw := range raws {
		var got string
		_ = db.QueryRow(ctx, `SELECT phone FROM users WHERE id = $1`, ids[i]).Scan(&got)
		if want := phone.Format(raw); got != want {
			t.Errorf("migration(%q) = %q, phone.Format dá %q", raw, got, want)
		}
	}

	// Gravações novas: o cadastro, a ficha (perfil) e o Meus dados (contato).
	authSvc := auth.NewService(auth.NewRepository(db), config.Auth{BcryptCost: bcrypt.MinCost}, nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	created, err := authSvc.Register(ctx, auth.NewUser{
		Email: "novo@telefone.test", Name: "Novo", Role: auth.RoleCustomer, Phone: "+55 (34) 99999-0000", Password: userPassword,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Phone != "(34) 9-9999-0000" {
		t.Errorf("cadastro = %q", created.Phone)
	}
	updated, err := authSvc.UpdateProfile(ctx, created.ID, auth.Profile{Name: "Novo", Phone: "34988887777", Active: true})
	if err != nil || updated.Phone != "(34) 9-8888-7777" {
		t.Errorf("ficha = %q %v", updated.Phone, err)
	}
	contact, err := authSvc.UpdateContact(ctx, created.ID, "Novo", "(34) 97777-6666", "")
	if err != nil || contact.Phone != "(34) 9-7777-6666" {
		t.Errorf("meus dados = %q %v", contact.Phone, err)
	}
}
