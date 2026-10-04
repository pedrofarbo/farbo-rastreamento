package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/devices"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/mail"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/shares"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/vehicles"
	ws "github.com/pedrofarbo/farbo-rastreamento/backend/internal/websocket"
)

// recordingShareNotifier guarda os avisos dos acessos ("tipo → para quem").
type recordingShareNotifier struct {
	mu   sync.Mutex
	sent []string
}

func (n *recordingShareNotifier) add(kind, to string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.sent = append(n.sent, kind+" → "+to)
}

func (n *recordingShareNotifier) GuestInvited(_ context.Context, to string, _ mail.ShareNotice, token string, _ time.Duration) error {
	if token == "" {
		n.add("convite SEM LINK", to)
		return nil
	}
	n.add("convite", to)
	return nil
}

func (n *recordingShareNotifier) GuestAdded(_ context.Context, to string, _ mail.ShareNotice) error {
	n.add("aviso", to)
	return nil
}

func (n *recordingShareNotifier) OwnerShared(_ context.Context, to string, _ mail.ShareNotice) error {
	n.add("segurança", to)
	return nil
}

func (n *recordingShareNotifier) GuestBlocked(_ context.Context, to string, notice mail.ShareNotice) error {
	n.add("bloqueio por "+notice.GuestName, to)
	return nil
}

func (n *recordingShareNotifier) take() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := n.sent
	n.sent = nil
	return out
}

// Acessos de terceiros de ponta a ponta: o dono dá o acesso (com a
// confirmação), quem recebe vê só a posição ao vivo e, se liberado, bloqueia
// (nunca desbloqueia); o dono é avisado e tira o acesso quando quiser.
// Precisa de FARBO_TEST_DATABASE_URL.
func TestVehicleSharesEndToEnd(t *testing.T) {
	notifier := &recordingShareNotifier{}
	env := newCredEnvWith(t, credEnvOptions{stepUp: true, shares: notifier})
	ctx := context.Background()
	authSvc := auth.NewService(auth.NewRepository(env.db), config.Auth{BcryptCost: bcrypt.MinCost}, nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	users := map[string]*auth.User{}
	for email, role := range map[string]string{
		"ana@shares.test": auth.RoleCustomer, "caio@shares.test": auth.RoleCustomer,
		"dani@shares.test": auth.RoleCustomer, "zeca@shares.test": auth.RoleCustomer,
		"admin@shares.test": auth.RoleAdmin,
	} {
		name := map[string]string{"ana@shares.test": "Ana Souza", "caio@shares.test": "Caio", "dani@shares.test": "Dani"}[email]
		u, err := authSvc.CreateUser(ctx, email, name, role, userPassword)
		if err != nil {
			t.Fatal(err)
		}
		users[email] = u
	}
	newVehicle := func(imei, name string, owner *auth.User) *vehicles.Vehicle {
		dev, err := env.devices.Create(ctx, devices.Input{IMEI: imei, Protocol: "gt06"})
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
	carro := newVehicle("869247061238001", "Carro da Ana", users["ana@shares.test"])
	moto := newVehicle("869247061238002", "Moto do Zeca", users["zeca@shares.test"])
	if err := env.owners.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	ana, caio, dani := env.login("ana@shares.test"), env.login("caio@shares.test"), env.login("dani@shares.test")
	vehiclePath := "/api/vehicles/" + carro.ID.String()

	// O comprovante sai direto do serviço: pela rota, o limite de senhas por
	// endereço barraria o teste (a rota tem o próprio teste).
	tokenUser := map[string]*auth.User{ana: users["ana@shares.test"], caio: users["caio@shares.test"], dani: users["dani@shares.test"]}
	grant := func(token, purpose string) string {
		t.Helper()
		g, err := env.stepUp.VerifyPassword(ctx, tokenUser[token].ID, purpose, userPassword)
		if err != nil {
			t.Fatalf("comprovante %s: %v", purpose, err)
		}
		return g.Token
	}
	call := func(token, method, path, stepUp string, body any) (int, map[string]any) {
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
		var out map[string]any
		_ = json.NewDecoder(res.Body).Decode(&out)
		return res.StatusCode, out
	}
	expect := func(label string, status int, out map[string]any, want int, code string) {
		t.Helper()
		if status != want || (code != "" && out["code"] != code) {
			t.Fatalf("%s: %d %v (quer %d %s)", label, status, out, want, code)
		}
	}
	share := func(email string, canBlock bool, want int) map[string]any {
		t.Helper()
		status, out := call(ana, http.MethodPost, "/api/me/shares", grant(ana, "vehicle_share"),
			map[string]any{"vehicleId": carro.ID, "name": "Pessoa " + email, "email": email, "canBlock": canBlock})
		expect("compartilhar com "+email, status, out, want, "")
		return out
	}

	// Dar acesso pede a confirmação extra.
	status, out := call(ana, http.MethodPost, "/api/me/shares", "",
		map[string]any{"vehicleId": carro.ID, "name": "Bia", "email": "bia@shares.test", "canBlock": false})
	expect("sem confirmação", status, out, http.StatusForbidden, "STEP_UP_REQUIRED")

	// Sem conta: a conta é criada e vai o convite; o dono recebe o aviso.
	if got := share("Bia@Shares.test", false, http.StatusCreated); got["guestEmail"] != "bia@shares.test" || got["canBlock"] != false {
		t.Fatalf("bia = %v", got)
	}
	if got := notifier.take(); len(got) != 2 || got[0] != "convite → bia@shares.test" || got[1] != "segurança → ana@shares.test" {
		t.Fatalf("avisos = %v", got)
	}
	if bia, err := authSvc.FindByEmail(ctx, "bia@shares.test"); err != nil || bia.Role != auth.RoleCustomer {
		t.Fatalf("conta da bia: %v %v", bia, err)
	}
	// Com conta de cliente: só o aviso.
	caioShare := share("caio@shares.test", true, http.StatusCreated)
	daniShare := share("dani@shares.test", false, http.StatusCreated)
	if got := notifier.take(); len(got) != 4 || got[0] != "aviso → caio@shares.test" || got[2] != "aviso → dani@shares.test" {
		t.Fatalf("avisos = %v", got)
	}

	// O que não pode: veículo dos outros (antes de gastar a confirmação), o
	// próprio e-mail, e-mail da equipe, a mesma pessoa de novo, sem nome.
	status, out = call(ana, http.MethodPost, "/api/me/shares", "",
		map[string]any{"vehicleId": moto.ID, "name": "X", "email": "x@shares.test"})
	expect("veículo alheio", status, out, http.StatusNotFound, "")
	share("ana@shares.test", false, http.StatusBadRequest)
	share("admin@shares.test", false, http.StatusBadRequest)
	share("caio@shares.test", false, http.StatusConflict)
	status, out = call(ana, http.MethodPost, "/api/me/shares", grant(ana, "vehicle_share"),
		map[string]any{"vehicleId": carro.ID, "name": " ", "email": "y@shares.test"})
	expect("sem nome", status, out, http.StatusBadRequest, "")

	var list []shares.Share
	_ = json.Unmarshal(env.must(ana, http.MethodGet, "/api/me/shares", nil, http.StatusOK), &list)
	if len(list) != 3 {
		t.Fatalf("acessos da ana = %+v", list)
	}

	// Quem recebeu vê o veículo, marcado como compartilhado, e a posição ao vivo.
	var vlist []struct {
		ID     uuid.UUID `json:"id"`
		Shared *struct {
			OwnerName string `json:"ownerName"`
			CanBlock  bool   `json:"canBlock"`
		} `json:"shared"`
	}
	_ = json.Unmarshal(env.must(caio, http.MethodGet, "/api/vehicles", nil, http.StatusOK), &vlist)
	if len(vlist) != 1 || vlist[0].ID != carro.ID || vlist[0].Shared == nil || vlist[0].Shared.OwnerName != "Ana Souza" ||
		!vlist[0].Shared.CanBlock {
		t.Fatalf("veículos do caio = %+v", vlist)
	}
	env.must(caio, http.MethodGet, vehiclePath, nil, http.StatusOK)
	env.must(caio, http.MethodGet, vehiclePath+"/position", nil, http.StatusOK)
	// O dono não vê o veículo como compartilhado.
	vlist = nil
	_ = json.Unmarshal(env.must(ana, http.MethodGet, "/api/vehicles", nil, http.StatusOK), &vlist)
	if len(vlist) != 1 || vlist[0].Shared != nil {
		t.Fatalf("veículos da ana = %+v", vlist)
	}

	// O resto continua só do dono: histórico, eventos, comandos, edição,
	// desbloqueio e status.
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, vehiclePath + "/positions"},
		{http.MethodGet, vehiclePath + "/events"},
		{http.MethodGet, vehiclePath + "/commands"},
		{http.MethodPatch, vehiclePath},
		{http.MethodPost, vehiclePath + "/commands/engine-resume"},
		{http.MethodPost, vehiclePath + "/commands/request-status"},
	} {
		status, out := call(caio, c.method, c.path, "", map[string]any{"name": "Roubado"})
		expect("caio "+c.method+" "+c.path, status, out, http.StatusNotFound, "")
	}
	// Outro veículo da ana, sem acesso: nada.
	env.must(caio, http.MethodGet, "/api/vehicles/"+moto.ID.String(), nil, http.StatusNotFound)

	// Pedir a posição agora, sim.
	env.must(caio, http.MethodPost, vehiclePath+"/commands/request-position", nil, http.StatusAccepted)

	// Sem o bloqueio liberado, nem a checagem do corte.
	status, out = call(dani, http.MethodGet, vehiclePath+"/commands/engine-cut/check", "", nil)
	expect("dani checagem", status, out, http.StatusForbidden, "")
	status, out = call(dani, http.MethodPost, vehiclePath+"/commands/engine-cut", grant(dani, "engine_cut"), nil)
	expect("dani corte", status, out, http.StatusForbidden, "")

	// Com o bloqueio liberado: pede a confirmação dele, bloqueia e o dono é avisado.
	env.must(caio, http.MethodGet, vehiclePath+"/commands/engine-cut/check", nil, http.StatusOK)
	status, out = call(caio, http.MethodPost, vehiclePath+"/commands/engine-cut", "", nil)
	expect("caio corte sem confirmação", status, out, http.StatusForbidden, "STEP_UP_REQUIRED")
	status, out = call(caio, http.MethodPost, vehiclePath+"/commands/engine-cut", grant(caio, "engine_cut"), nil)
	expect("caio corte", status, out, http.StatusAccepted, "")
	if got := notifier.take(); len(got) != 1 || got[0] != "bloqueio por Caio → ana@shares.test" {
		t.Fatalf("aviso do bloqueio = %v", got)
	}
	var cuts int
	if err := env.db.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE action = $1 AND user_id = $2`,
		audit.ActionSharedEngineCut, users["caio@shares.test"].ID).Scan(&cuts); err != nil || cuts != 1 {
		t.Errorf("auditoria do bloqueio por terceiro = %d %v", cuts, err)
	}

	// Desbloquear é do dono, e com a confirmação dele.
	resume := vehiclePath + "/commands/engine-resume"
	status, out = call(ana, http.MethodPost, resume, "", nil)
	expect("ana desbloqueio sem confirmação", status, out, http.StatusForbidden, "STEP_UP_REQUIRED")
	status, out = call(ana, http.MethodPost, resume, grant(ana, "engine_cut"), nil)
	expect("ana desbloqueio com a confirmação de outra ação", status, out, http.StatusForbidden, "STEP_UP_REQUIRED")
	status, out = call(ana, http.MethodPost, resume, grant(ana, "engine_resume"), nil)
	expect("ana desbloqueio", status, out, http.StatusAccepted, "")

	// Liberar o bloqueio pede a confirmação; tirar, não.
	daniPath := "/api/me/shares/" + daniShare["id"].(string)
	status, out = call(ana, http.MethodPatch, daniPath, "", map[string]any{"canBlock": true})
	expect("liberar sem confirmação", status, out, http.StatusForbidden, "STEP_UP_REQUIRED")
	status, out = call(ana, http.MethodPatch, daniPath, grant(ana, "vehicle_share"), map[string]any{"canBlock": true})
	expect("liberar", status, out, http.StatusOK, "")
	status, out = call(ana, http.MethodPatch, daniPath, "", map[string]any{"canBlock": false})
	expect("tirar", status, out, http.StatusOK, "")
	// Só o dono muda.
	status, out = call(dani, http.MethodPatch, daniPath, "", map[string]any{"canBlock": false})
	expect("dani muda o próprio acesso", status, out, http.StatusNotFound, "")

	// O tempo real: quem recebeu ganha a posição e o motor, não os eventos.
	caioReq := httptest.NewRequest(http.MethodGet, "/ws", nil).WithContext(auth.WithPrincipal(ctx,
		&auth.Principal{UserID: users["caio@shares.test"].ID, Role: auth.RoleCustomer}))
	scope := (&Server{Deps: Deps{Owners: env.owners}}).websocketScope(caioReq)
	msg := func(kind string, vehicle uuid.UUID) ws.Message { return ws.Message{Type: kind, VehicleID: &vehicle} }
	if !scope(msg(ws.TypePositionUpdated, carro.ID)) || !scope(msg(ws.TypeEngineStatusChanged, carro.ID)) {
		t.Error("caio sem a posição ao vivo do carro")
	}
	if scope(msg(ws.TypeVehicleEvent, carro.ID)) || scope(msg(ws.TypePositionUpdated, moto.ID)) {
		t.Error("caio recebendo eventos, ou veículo sem acesso")
	}

	// O dono tira o acesso: vale na hora, inclusive no tempo real.
	env.must(ana, http.MethodDelete, "/api/me/shares/"+caioShare["id"].(string), nil, http.StatusNoContent)
	env.must(caio, http.MethodGet, vehiclePath, nil, http.StatusNotFound)
	if scope(msg(ws.TypePositionUpdated, carro.ID)) {
		t.Error("o tempo real continua depois de tirar o acesso")
	}
	vlist = nil
	_ = json.Unmarshal(env.must(caio, http.MethodGet, "/api/vehicles", nil, http.StatusOK), &vlist)
	if len(vlist) != 0 {
		t.Errorf("veículos do caio depois = %+v", vlist)
	}
	// Quem recebeu pode deixar de acompanhar; ninguém mais mexe no acesso.
	env.must(caio, http.MethodDelete, daniPath, nil, http.StatusNotFound)
	env.must(dani, http.MethodDelete, daniPath, nil, http.StatusNoContent)
	_ = json.Unmarshal(env.must(ana, http.MethodGet, "/api/me/shares", nil, http.StatusOK), &list)
	if len(list) != 1 || list[0].GuestEmail != "bia@shares.test" {
		t.Errorf("acessos da ana no fim = %+v", list)
	}
}
