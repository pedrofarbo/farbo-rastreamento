package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/alerts"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/devices"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/mail"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/vehicles"
)

// recordingTeamNotifier guarda os convites da equipe (e o link de cada um).
type recordingTeamNotifier struct {
	mu      sync.Mutex
	invited map[string]string // e-mail → token do convite
	added   []string
}

func (n *recordingTeamNotifier) MemberInvited(_ context.Context, to string, notice mail.TeamNotice, token string, _ time.Duration) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.invited == nil {
		n.invited = map[string]string{}
	}
	n.invited[to] = token
	return nil
}

func (n *recordingTeamNotifier) MemberAdded(_ context.Context, to string, _ mail.TeamNotice) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.added = append(n.added, to)
	return nil
}

func (n *recordingTeamNotifier) token(email string) string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.invited[email]
}

// A conta empresa de ponta a ponta: o titular põe na equipe um Gestor e um
// Visualizador (com a confirmação extra), cada um entra com o próprio login
// e enxerga a conta do titular — o Gestor opera, o Visualizador só
// acompanha, e nenhum dos dois mexe na conta —; trocar o papel e tirar da
// equipe valem na hora; a central vê a equipe na ficha do cliente; os
// alertas chegam à equipe. Precisa de FARBO_TEST_DATABASE_URL.
func TestAccountTeamEndToEnd(t *testing.T) {
	notifier := &recordingTeamNotifier{}
	env := newCredEnvWith(t, credEnvOptions{stepUp: true, team: notifier, shares: &recordingShareNotifier{}})
	ctx := context.Background()
	authSvc := auth.NewService(auth.NewRepository(env.db), config.Auth{BcryptCost: bcrypt.MinCost}, nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	users := map[string]*auth.User{}
	for email, role := range map[string]string{
		"frota@team.test": auth.RoleCustomer, "outro@team.test": auth.RoleCustomer, "admin@team.test": auth.RoleAdmin,
	} {
		u, err := authSvc.CreateUser(ctx, email, map[string]string{"frota@team.test": "Frota Ltda"}[email], role, userPassword)
		if err != nil {
			t.Fatal(err)
		}
		users[email] = u
	}
	newVehicle := func(imei, name string, owner *auth.User) *vehicles.Vehicle {
		dev, err := env.devices.Create(ctx, devices.Input{IMEIConfirmed: true, IMEI: imei, Protocol: "gt06"})
		if err != nil {
			t.Fatal(err)
		}
		v, err := env.vehicles.Create(ctx, vehicles.Input{Name: name, DeviceID: &dev.ID, OwnerID: &owner.ID})
		if err != nil {
			t.Fatal(err)
		}
		env.sender.connected[imei] = true
		return v
	}
	van := newVehicle("869247061239001", "Van 01", users["frota@team.test"])
	newVehicle("869247061239002", "Carro do outro", users["outro@team.test"])
	if err := env.owners.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	owner, admin := env.login("frota@team.test"), env.login("admin@team.test")
	vanPath := "/api/vehicles/" + van.ID.String()

	call := func(token, method, path, stepUp string, body any) (int, []byte) {
		t.Helper()
		var reader io.Reader
		if body != nil {
			raw, _ := json.Marshal(body)
			reader = bytes.NewReader(raw)
		}
		req, _ := http.NewRequest(method, env.srv.URL+path, reader)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		if stepUp != "" {
			req.Header.Set("X-Step-Up-Token", stepUp)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		out, _ := io.ReadAll(res.Body)
		return res.StatusCode, out
	}
	expect := func(label string, token, method, path string, body any, want int, code string) []byte {
		t.Helper()
		status, out := call(token, method, path, "", body)
		var parsed map[string]any
		_ = json.Unmarshal(out, &parsed)
		if status != want || (code != "" && parsed["code"] != code) {
			t.Fatalf("%s: %d %s (quer %d %s)", label, status, out, want, code)
		}
		return out
	}
	grant := func() string {
		t.Helper()
		g, err := env.stepUp.VerifyPassword(ctx, users["frota@team.test"].ID, "team_member", userPassword)
		if err != nil {
			t.Fatal(err)
		}
		return g.Token
	}
	type member struct {
		UserID string `json:"userId"`
		Name   string `json:"name"`
		Email  string `json:"email"`
		Role   string `json:"role"`
		Active bool   `json:"active"`
	}
	add := func(name, email, role string, want int) member {
		t.Helper()
		status, out := call(owner, http.MethodPost, "/api/me/team", grant(), map[string]any{"name": name, "email": email, "role": role})
		if status != want {
			t.Fatalf("pôr %s na equipe: %d %s", email, status, out)
		}
		var m member
		_ = json.Unmarshal(out, &m)
		return m
	}
	// Quem foi convidado cria a senha pelo link e entra.
	join := func(email string) string {
		t.Helper()
		token := notifier.token(email)
		if token == "" {
			t.Fatalf("%s não recebeu o convite", email)
		}
		env.must("", http.MethodPost, "/api/auth/reset-password",
			map[string]string{"token": token, "password": userPassword}, http.StatusNoContent)
		return env.login(email)
	}

	// Pôr alguém na equipe pede a confirmação extra.
	status, out := call(owner, http.MethodPost, "/api/me/team", "", map[string]any{"name": "Gi", "email": "gi@team.test", "role": "manager"})
	if status != http.StatusForbidden {
		t.Fatalf("sem confirmação: %d %s", status, out)
	}
	gi := add("Gi Gestora", "GI@team.test", auth.MemberManager, http.StatusCreated)
	vi := add("Vi Visualizador", "vi@team.test", auth.MemberViewer, http.StatusCreated)
	if gi.Email != "gi@team.test" || gi.Role != "manager" || vi.Role != "viewer" {
		t.Fatalf("equipe = %+v %+v", gi, vi)
	}
	// Recusas: o próprio e-mail, um cliente com veículos, quem já é da
	// equipe e um papel que não existe.
	add("Eu", "frota@team.test", auth.MemberViewer, http.StatusBadRequest)
	add("Outro", "outro@team.test", auth.MemberViewer, http.StatusBadRequest)
	add("Gi de novo", "gi@team.test", auth.MemberViewer, http.StatusBadRequest)
	add("Chefe", "chefe@team.test", "admin", http.StatusBadRequest)
	manager, viewer := join("gi@team.test"), join("vi@team.test")

	// Quem é da equipe sabe de que conta é, e com que papel.
	var me struct {
		Membership *auth.Membership `json:"membership"`
	}
	_ = json.Unmarshal(expect("me do gestor", manager, http.MethodGet, "/api/auth/me", nil, http.StatusOK, ""), &me)
	if me.Membership == nil || me.Membership.AccountID != users["frota@team.test"].ID || me.Membership.Role != "manager" ||
		me.Membership.OwnerName != "Frota Ltda" {
		t.Fatalf("me do gestor = %+v", me.Membership)
	}

	// Os dois enxergam os veículos da conta (e só os dela).
	for label, token := range map[string]string{"gestor": manager, "visualizador": viewer} {
		var list []map[string]any
		_ = json.Unmarshal(expect("veículos do "+label, token, http.MethodGet, "/api/vehicles", nil, http.StatusOK, ""), &list)
		if len(list) != 1 || list[0]["name"] != "Van 01" {
			t.Fatalf("veículos do %s = %v", label, list)
		}
		expect("posições do "+label, token, http.MethodGet, vanPath+"/positions", nil, http.StatusOK, "")
		expect("contrato do "+label, token, http.MethodGet, "/api/me/contract", nil, http.StatusNotFound, "")
		// A conta é do titular: equipe, cadastro, contrato e pedidos.
		expect("equipe pelo "+label, token, http.MethodGet, "/api/me/team", nil, http.StatusForbidden, "ACCOUNT_ROLE")
		expect("cadastro pelo "+label, token, http.MethodPut, "/api/me/profile",
			map[string]any{"name": "Outro nome", "document": "52998224725"}, http.StatusForbidden, "ACCOUNT_ROLE")
		expect("pedido pelo "+label, token, http.MethodPost, "/api/me/trackers", map[string]any{}, http.StatusForbidden, "ACCOUNT_ROLE")
		expect("compartilhar pelo "+label, token, http.MethodPost, "/api/me/shares", map[string]any{}, http.StatusForbidden, "ACCOUNT_ROLE")
		expect("aceite pelo "+label, token, http.MethodPost, "/api/me/contract/accept", map[string]any{}, http.StatusForbidden, "ACCOUNT_ROLE")
	}

	// O Gestor opera: comandos, edição do veículo e faturas.
	expect("posição pelo gestor", manager, http.MethodPost, vanPath+"/commands/request-position", nil, http.StatusAccepted, "")
	expect("editar pelo gestor", manager, http.MethodPatch, vanPath, map[string]any{"name": "Van 01 (entregas)"}, http.StatusOK, "")
	expect("faturas do gestor", manager, http.MethodGet, "/api/me/invoices", nil, http.StatusOK, "")

	// O Visualizador só acompanha.
	expect("posição pelo visualizador", viewer, http.MethodPost, vanPath+"/commands/request-position", nil, http.StatusForbidden, "ACCOUNT_ROLE")
	expect("corte pelo visualizador", viewer, http.MethodPost, vanPath+"/commands/engine-cut", nil, http.StatusForbidden, "ACCOUNT_ROLE")
	expect("editar pelo visualizador", viewer, http.MethodPatch, vanPath, map[string]any{"name": "Minha van"}, http.StatusForbidden, "ACCOUNT_ROLE")
	expect("faturas do visualizador", viewer, http.MethodGet, "/api/me/invoices", nil, http.StatusForbidden, "ACCOUNT_ROLE")
	expect("cerca pelo visualizador", viewer, http.MethodPost, "/api/geofences", map[string]any{}, http.StatusForbidden, "ACCOUNT_ROLE")
	expect("roubo pelo visualizador", viewer, http.MethodPost, vanPath+"/theft", nil, http.StatusForbidden, "ACCOUNT_ROLE")

	// O titular vê a equipe e troca o papel: vale na próxima requisição.
	var team []member
	_ = json.Unmarshal(expect("equipe", owner, http.MethodGet, "/api/me/team", nil, http.StatusOK, ""), &team)
	if len(team) != 2 || team[0].Email != "gi@team.test" || team[1].Email != "vi@team.test" {
		t.Fatalf("equipe = %+v", team)
	}
	expect("papel inválido", owner, http.MethodPatch, "/api/me/team/"+vi.UserID, map[string]any{"role": "admin"}, http.StatusBadRequest, "")
	expect("promover", owner, http.MethodPatch, "/api/me/team/"+vi.UserID, map[string]any{"role": "manager"}, http.StatusOK, "")
	expect("posição pelo promovido", viewer, http.MethodPost, vanPath+"/commands/request-position", nil, http.StatusAccepted, "")

	// A central vê a equipe na ficha do cliente; quem é da equipe não
	// aparece como cliente.
	var customers []map[string]any
	_ = json.Unmarshal(expect("clientes", admin, http.MethodGet, "/api/customers", nil, http.StatusOK, ""), &customers)
	for _, c := range customers {
		if c["email"] == "gi@team.test" || c["email"] == "vi@team.test" {
			t.Errorf("quem é da equipe apareceu como cliente: %v", c)
		}
	}
	_ = json.Unmarshal(expect("equipe na central", admin, http.MethodGet,
		"/api/customers/"+users["frota@team.test"].ID.String()+"/team", nil, http.StatusOK, ""), &team)
	if len(team) != 2 {
		t.Fatalf("equipe na central = %+v", team)
	}

	// Os alertas dos veículos da conta chegam à equipe.
	target, err := alerts.NewDBStore(env.db, nil).Target(ctx, *van.DeviceID)
	if err != nil {
		t.Fatal(err)
	}
	if target.Owner == nil || len(target.Team) != 2 || target.Team[0].Email != "gi@team.test" {
		t.Fatalf("alvo dos alertas = %+v", target)
	}

	// Tirar da equipe vale na hora: a Gi volta a enxergar só a própria
	// conta (vazia) e não entra mais.
	expect("tirar", owner, http.MethodDelete, "/api/me/team/"+gi.UserID, nil, http.StatusNoContent, "")
	var after []map[string]any
	_ = json.Unmarshal(expect("veículos depois de sair", manager, http.MethodGet, "/api/vehicles", nil, http.StatusOK, ""), &after)
	if len(after) != 0 {
		t.Fatalf("depois de sair ainda vê: %v", after)
	}
	expect("comando depois de sair", manager, http.MethodPost, vanPath+"/commands/request-position", nil, http.StatusNotFound, "")
	if status, _ := env.do("", http.MethodPost, "/api/auth/login", map[string]string{"email": "gi@team.test", "password": userPassword}); status == http.StatusOK {
		t.Fatal("quem saiu da equipe ainda entra")
	}
	expect("tirar de novo", owner, http.MethodDelete, "/api/me/team/"+gi.UserID, nil, http.StatusNotFound, "")

	// De volta: a conta é reativada e ela recebe um convite novo.
	notifier.invited["gi@team.test"] = ""
	add("Gi Gestora", "gi@team.test", auth.MemberViewer, http.StatusCreated)
	var active bool
	if err := env.db.QueryRow(ctx, `SELECT active FROM users WHERE email = 'gi@team.test'`).Scan(&active); err != nil || !active {
		t.Fatalf("de volta à equipe, ativa = %v (%v)", active, err)
	}
	if notifier.token("gi@team.test") == "" {
		t.Fatal("de volta à equipe sem convite novo")
	}
}
