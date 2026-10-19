package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/devices"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/vehicles"
)

// O rastreio de bloqueio e desbloqueio: o bloqueio pelo app guarda de qual
// aparelho saiu e como foi confirmado; a lista diz quem pediu do ponto de
// vista de quem vê ("Você", a equipe da conta, quem tem acesso
// compartilhado, a central — sem o aparelho dela para o cliente — e o
// sistema). Precisa de FARBO_TEST_DATABASE_URL.
func TestEngineHistory(t *testing.T) {
	env := newCredEnvWith(t, credEnvOptions{stepUp: true, team: &recordingTeamNotifier{}, shares: &recordingShareNotifier{}})
	ctx := context.Background()
	authSvc := auth.NewService(auth.NewRepository(env.db), config.Auth{BcryptCost: bcrypt.MinCost}, nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	user := func(email, name, role string) *auth.User {
		t.Helper()
		u, err := authSvc.CreateUser(ctx, email, name, role, userPassword)
		if err != nil {
			t.Fatal(err)
		}
		return u
	}
	ana := user("ana@historico.test", "Ana", auth.RoleCustomer)
	bia := user("bia@historico.test", "Bia", auth.RoleCustomer)
	caio := user("caio@historico.test", "Caio", auth.RoleCustomer)
	user("davi@historico.test", "Davi", auth.RoleCustomer)
	admin := user("admin@historico.test", "Operador Farbo", auth.RoleAdmin)
	dev, err := env.devices.Create(ctx, devices.Input{IMEIConfirmed: true, IMEI: "869247061237019", Protocol: "gt06"})
	if err != nil {
		t.Fatal(err)
	}
	carro, err := env.vehicles.Create(ctx, vehicles.Input{Name: "Strada", DeviceID: &dev.ID, OwnerID: &ana.ID})
	if err != nil {
		t.Fatal(err)
	}
	// A Bia é da equipe da conta da Ana; o Caio tem acesso compartilhado.
	if _, err := env.db.Exec(ctx, `INSERT INTO account_members (user_id, account_id, role) VALUES ($1, $2, 'manager')`, bia.ID, ana.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := env.db.Exec(ctx, `INSERT INTO vehicle_shares (vehicle_id, owner_id, guest_id, can_block) VALUES ($1, $2, $3, TRUE)`,
		carro.ID, ana.ID, caio.ID); err != nil {
		t.Fatal(err)
	}
	if err := env.owners.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	// Os de antes, gravados direto (do mais velho ao mais novo).
	old := func(minutesAgo int, command string, by any, ua, confirmed, status string) {
		t.Helper()
		if _, err := env.db.Exec(ctx, `
			INSERT INTO device_commands (device_id, command, status, correlation_key, requested_by, requested_at, user_agent, confirmed_with)
			VALUES ($1, $2, $3, $4, $5, NOW() - make_interval(mins => $6), $7, $8)`,
			dev.ID, command, status, minutesAgo, by, minutesAgo, ua, confirmed); err != nil {
			t.Fatal(err)
		}
	}
	old(50, "ENGINE_CUT", nil, "", "", "ACKNOWLEDGED")
	old(40, "ENGINE_RESUME", admin.ID, "Mozilla/5.0 (Windows NT 10.0) Chrome/131.0", "", "ACKNOWLEDGED")
	old(30, "ENGINE_CUT", caio.ID, "Mozilla/5.0 (Linux; Android 14) Chrome/131.0", "biometric", "ACKNOWLEDGED")
	old(20, "ENGINE_RESUME", bia.ID, "Mozilla/5.0 (iPhone; CPU iPhone OS 18_7) Version/26.5.2 Safari/604.1", "password", "TIMEOUT")
	old(10, "REQUEST_POSITION", ana.ID, "", "", "ACKNOWLEDGED")

	// O bloqueio de agora, pelo app da Ana, confirmado com a senha.
	env.sender.connected["869247061237019"] = true
	anaToken := env.login("ana@historico.test")
	var grant struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(env.must(anaToken, http.MethodPost, "/api/step-up/password",
		map[string]string{"purpose": "engine_cut", "password": userPassword}, http.StatusOK), &grant)
	const safari = "Mozilla/5.0 (iPhone; CPU iPhone OS 18_7 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.6.1 Mobile/15E148 Safari/604.1"
	req, _ := http.NewRequest(http.MethodPost, env.srv.URL+"/api/vehicles/"+carro.ID.String()+"/commands/engine-cut", nil)
	req.Header.Set("Authorization", "Bearer "+anaToken)
	req.Header.Set("X-Step-Up-Token", grant.Token)
	req.Header.Set("User-Agent", safari)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("bloqueio: %d", res.StatusCode)
	}

	type item struct {
		Command       string    `json:"command"`
		Status        string    `json:"status"`
		Who           string    `json:"who"`
		WhoKind       string    `json:"whoKind"`
		UserAgent     string    `json:"userAgent"`
		ConfirmedWith string    `json:"confirmedWith"`
		RequestedAt   time.Time `json:"requestedAt"`
	}
	history := func(token string) []item {
		t.Helper()
		var out []item
		_ = json.Unmarshal(env.must(token, http.MethodGet, "/api/vehicles/"+carro.ID.String()+"/engine-history", nil, http.StatusOK), &out)
		return out
	}

	got := history(anaToken)
	if len(got) != 5 {
		t.Fatalf("rastreio da Ana (sem o pedido de posição) = %+v", got)
	}
	want := []struct{ command, who, kind, ua, confirmed string }{
		{"ENGINE_CUT", "Você", "you", safari, "password"},
		{"ENGINE_RESUME", "Bia", "account", "Mozilla/5.0 (iPhone; CPU iPhone OS 18_7) Version/26.5.2 Safari/604.1", "password"},
		{"ENGINE_CUT", "Caio", "shared", "Mozilla/5.0 (Linux; Android 14) Chrome/131.0", "biometric"},
		{"ENGINE_RESUME", "Central Farbo", "central", "", ""},
		{"ENGINE_CUT", "Sistema", "system", "", ""},
	}
	for i, w := range want {
		g := got[i]
		if g.Command != w.command || g.Who != w.who || g.WhoKind != w.kind || g.UserAgent != w.ua || g.ConfirmedWith != w.confirmed {
			t.Errorf("item %d = %+v, quer %+v", i, g, w)
		}
	}
	// A Bia (da equipe) vê o próprio como "Você" e o da Ana pelo nome.
	if b := history(env.login("bia@historico.test")); b[0].Who != "Ana" || b[0].WhoKind != "account" || b[1].Who != "Você" {
		t.Errorf("rastreio da Bia = %+v", b[:2])
	}
	// A central vê o nome de quem é da equipe dela e o aparelho.
	if a := history(env.login("admin@historico.test")); a[3].Who != "Operador Farbo" || a[3].UserAgent == "" {
		t.Errorf("rastreio na central = %+v", a[3])
	}
	// Outro cliente, e quem só acompanha o veículo, não veem.
	env.must(env.login("davi@historico.test"), http.MethodGet, "/api/vehicles/"+carro.ID.String()+"/engine-history", nil, http.StatusNotFound)
	env.must(env.login("caio@historico.test"), http.MethodGet, "/api/vehicles/"+carro.ID.String()+"/engine-history", nil, http.StatusNotFound)
}
