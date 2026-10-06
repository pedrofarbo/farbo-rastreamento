package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/devices"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/mail"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/push"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/tracking"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/vehicles"
)

// recordingTheftMailer guarda os e-mails do modo roubo ("tipo → para quem").
type recordingTheftMailer struct {
	mu   sync.Mutex
	sent []string
	urls []string
}

func (m *recordingTheftMailer) add(kind, to string, n mail.TheftNotice) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, kind+" → "+to)
	m.urls = append(m.urls, n.PublicURL)
	return nil
}

func (m *recordingTheftMailer) TheftActivated(_ context.Context, to, _ string, n mail.TheftNotice) error {
	return m.add("ativado por "+n.ActorName, to, n)
}

func (m *recordingTheftMailer) TheftEnded(_ context.Context, to, _ string, n mail.TheftNotice) error {
	kind := "desativado"
	if n.Recovered {
		kind = "recuperado"
	}
	return m.add(kind, to, n)
}

func (m *recordingTheftMailer) TheftReminder(_ context.Context, to, _ string, n mail.TheftNotice) error {
	return m.add("lembrete", to, n)
}

func (m *recordingTheftMailer) TheftExpired(_ context.Context, to, _ string, n mail.TheftNotice) error {
	return m.add("desligou sozinho", to, n)
}

// take devolve os e-mails, em ordem alfabética do destinatário dentro do lote.
func (m *recordingTheftMailer) take() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := m.sent
	m.sent, m.urls = nil, nil
	return out
}

type recordingPusher struct {
	mu   sync.Mutex
	sent map[uuid.UUID][]push.Notification
}

func (p *recordingPusher) Notify(_ context.Context, userID uuid.UUID, n push.Notification) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.sent == nil {
		p.sent = map[uuid.UUID][]push.Notification{}
	}
	p.sent[userID] = append(p.sent[userID], n)
	return 1, nil
}

func (p *recordingPusher) take() map[uuid.UUID][]push.Notification {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := p.sent
	p.sent = nil
	return out
}

func sameSet(got []string, want ...string) bool {
	if len(got) != len(want) {
		return false
	}
	left := map[string]int{}
	for _, g := range got {
		left[g]++
	}
	for _, w := range want {
		if left[w] == 0 {
			return false
		}
		left[w]--
	}
	return true
}

// O modo roubo de ponta a ponta, sem central: o dono (ou quem pode bloquear)
// liga; o rastreador recebe o intervalo curto (fora do ar, quando voltar);
// quem tem acesso é avisado; o link público mostra a posição sem login e sem
// nada do dono; só o dono desliga, com a confirmação; e ele desliga sozinho
// no prazo, com um lembrete por dia. Precisa de FARBO_TEST_DATABASE_URL.
func TestTheftModeEndToEnd(t *testing.T) {
	mailer := &recordingTheftMailer{}
	env := newCredEnvWith(t, credEnvOptions{stepUp: true, theft: mailer, shares: &recordingShareNotifier{}})
	pusher := &recordingPusher{}
	env.theft.SetPusher(pusher)
	clock := time.Now()
	env.theft.SetClock(func() time.Time { return clock })
	ctx := context.Background()
	authSvc := auth.NewService(auth.NewRepository(env.db), config.Auth{BcryptCost: bcrypt.MinCost}, nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	users := map[string]*auth.User{}
	for _, u := range []struct{ email, name, role string }{
		{"ana@roubo.test", "Ana Souza", auth.RoleCustomer}, {"caio@roubo.test", "Caio Lima", auth.RoleCustomer},
		{"dani@roubo.test", "Dani", auth.RoleCustomer}, {"zeca@roubo.test", "Zeca", auth.RoleCustomer},
	} {
		created, err := authSvc.CreateUser(ctx, u.email, u.name, u.role, userPassword)
		if err != nil {
			t.Fatal(err)
		}
		users[u.email] = created
	}
	const imei = "869247061239001"
	dev, err := env.devices.Create(ctx, devices.Input{IMEI: imei, Protocol: "gt06"})
	if err != nil {
		t.Fatal(err)
	}
	ana := users["ana@roubo.test"]
	moto, err := env.vehicles.Create(ctx, vehicles.Input{Name: "Moto da Ana", Plate: "ROU1B23", DeviceID: &dev.ID, OwnerID: &ana.ID})
	if err != nil {
		t.Fatal(err)
	}
	env.sender.setConnected(imei, true)
	// Caio pode bloquear o motor; Dani só acompanha.
	for email, canBlock := range map[string]bool{"caio@roubo.test": true, "dani@roubo.test": false} {
		if _, err := env.db.Exec(ctx, `INSERT INTO vehicle_shares (vehicle_id, owner_id, guest_id, can_block) VALUES ($1, $2, $3, $4)`,
			moto.ID, ana.ID, users[email].ID, canBlock); err != nil {
			t.Fatal(err)
		}
	}
	if err := env.owners.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	tokAna, tokCaio, tokDani, tokZeca := env.login("ana@roubo.test"), env.login("caio@roubo.test"),
		env.login("dani@roubo.test"), env.login("zeca@roubo.test")
	path := "/api/vehicles/" + moto.ID.String() + "/theft"

	type view struct {
		Mode *struct {
			ID              string `json:"id"`
			ActivatedByName string `json:"activatedByName"`
			BoostStatus     string `json:"boostStatus"`
			BoostCommand    string `json:"boostCommandStatus"`
		} `json:"mode"`
		PublicURL     string `json:"publicUrl"`
		CanActivate   bool   `json:"canActivate"`
		CanEnd        bool   `json:"canEnd"`
		ParkedSeconds int    `json:"parkedSeconds"`
		DurationHours int    `json:"durationHours"`
	}
	read := func(token string) view {
		t.Helper()
		var v view
		if err := json.Unmarshal(env.must(token, http.MethodGet, path, nil, http.StatusOK), &v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	timers := func() []string {
		t.Helper()
		var out []string
		for _, text := range env.sender.texts(t, imei) {
			if strings.HasPrefix(text, "TIMER") {
				out = append(out, text)
			}
		}
		return out
	}
	end := func(token string, outcome string, stepUp bool, want int) []byte {
		t.Helper()
		headers := map[string]string{}
		if stepUp {
			g, err := env.stepUp.VerifyPassword(ctx, ana.ID, "theft_end", userPassword)
			if err != nil {
				t.Fatal(err)
			}
			headers["X-Step-Up-Token"] = g.Token
		}
		status, body := env.doWith(token, http.MethodPost, path+"/end", map[string]string{"outcome": outcome}, headers)
		if status != want {
			t.Fatalf("encerrar (%s): %d %s (quer %d)", outcome, status, body, want)
		}
		return body
	}

	// Desligado: o dono pode ligar e desligar; quem pode bloquear só liga.
	if v := read(tokAna); v.Mode != nil || !v.CanActivate || !v.CanEnd || v.ParkedSeconds != 30 || v.DurationHours != 72 {
		t.Fatalf("dono, desligado = %+v", v)
	}
	if v := read(tokCaio); !v.CanActivate || v.CanEnd {
		t.Fatalf("caio = %+v", v)
	}
	// Quem só acompanha não liga; quem não tem acesso nem vê.
	env.must(tokDani, http.MethodPost, path, nil, http.StatusForbidden)
	env.must(tokZeca, http.MethodPost, path, nil, http.StatusNotFound)
	env.must(tokZeca, http.MethodGet, path, nil, http.StatusNotFound)

	// Caio liga (o celular da Ana foi junto com a moto): o rastreador recebe o
	// intervalo curto e todos são avisados — o push não vai para quem ligou.
	var on view
	if err := json.Unmarshal(env.must(tokCaio, http.MethodPost, path, nil, http.StatusCreated), &on); err != nil {
		t.Fatal(err)
	}
	if on.Mode == nil || on.Mode.BoostStatus != "SENT" || on.Mode.ActivatedByName != "Caio Lima" ||
		!strings.HasPrefix(on.PublicURL, "https://painel.farbo.test/localizar/") {
		t.Fatalf("ligado = %+v", on)
	}
	if got := timers(); len(got) != 1 || got[0] != "TIMER,10,30#" {
		t.Fatalf("comandos = %v", got)
	}
	if got := mailer.take(); !sameSet(got, "ativado por Caio Lima → ana@roubo.test",
		"ativado por Caio Lima → caio@roubo.test", "ativado por Caio Lima → dani@roubo.test") {
		t.Fatalf("e-mails = %v", got)
	}
	pushed := pusher.take()
	if len(pushed) != 2 || len(pushed[ana.ID]) != 1 || len(pushed[users["dani@roubo.test"].ID]) != 1 ||
		pushed[ana.ID][0].URL != "/app/veiculos/"+moto.ID.String() {
		t.Fatalf("push = %+v", pushed)
	}
	// De novo: o mesmo modo, sem novo comando nem aviso.
	var again view
	_ = json.Unmarshal(env.must(tokAna, http.MethodPost, path, nil, http.StatusOK), &again)
	if again.Mode == nil || again.Mode.ID != on.Mode.ID || len(timers()) != 1 || len(mailer.take()) != 0 {
		t.Fatalf("ligar de novo = %+v", again)
	}
	// A lista e o veículo mostram o modo roubo.
	var list []struct {
		ID    string          `json:"id"`
		Theft json.RawMessage `json:"theft"`
	}
	_ = json.Unmarshal(env.must(tokAna, http.MethodGet, "/api/vehicles", nil, http.StatusOK), &list)
	if len(list) != 1 || !strings.Contains(string(list[0].Theft), "since") {
		t.Fatalf("lista = %s", env.must(tokAna, http.MethodGet, "/api/vehicles", nil, http.StatusOK))
	}

	// O link público: sem login, a posição e o veículo — nada do dono.
	if err := env.positions.Insert(ctx, &tracking.Position{
		DeviceID: dev.ID, GPSTimestamp: clock.Add(-20 * time.Second), ReceivedAt: clock.Add(-18 * time.Second),
		Latitude: -23.55, Longitude: -46.63, SpeedKmh: 42, Protocol: "gt06", Source: "test",
	}); err != nil {
		t.Fatal(err)
	}
	token := on.PublicURL[strings.LastIndex(on.PublicURL, "/")+1:]
	public := env.must("", http.MethodGet, "/api/public/theft/"+token, nil, http.StatusOK)
	var pv struct {
		Vehicle struct {
			Name  string `json:"name"`
			Plate string `json:"plate"`
		} `json:"vehicle"`
		Position *struct {
			Latitude float64 `json:"latitude"`
			SpeedKmh float64 `json:"speedKmh"`
		} `json:"position"`
	}
	_ = json.Unmarshal(public, &pv)
	if pv.Vehicle.Name != "Moto da Ana" || pv.Vehicle.Plate != "ROU1B23" || pv.Position == nil ||
		pv.Position.Latitude != -23.55 || pv.Position.SpeedKmh != 42 {
		t.Fatalf("link público = %s", public)
	}
	for _, leak := range []string{"Souza", "ana@", "Caio", ana.ID.String()} {
		if strings.Contains(string(public), leak) {
			t.Errorf("o link público mostra %q: %s", leak, public)
		}
	}
	// Link adulterado ou inventado: nada.
	tampered := token[:len(token)-2] + "AA"
	if tampered == token {
		tampered = token[:len(token)-2] + "BB"
	}
	env.must("", http.MethodGet, "/api/public/theft/"+tampered, nil, http.StatusGone)
	env.must("", http.MethodGet, "/api/public/theft/nada", nil, http.StatusGone)

	// Desligar é só do dono, com a confirmação.
	end(tokCaio, "RECOVERED", false, http.StatusNotFound)
	end(tokAna, "RECOVERED", false, http.StatusForbidden)
	end(tokAna, "SUMIU", true, http.StatusBadRequest)
	end(tokAna, "RECOVERED", true, http.StatusOK)
	if got := timers(); len(got) != 2 || got[1] != "TIMER,10,3600#" {
		t.Fatalf("volta ao normal = %v", got)
	}
	if got := mailer.take(); !sameSet(got, "recuperado → ana@roubo.test", "recuperado → caio@roubo.test", "recuperado → dani@roubo.test") {
		t.Fatalf("e-mails do fim = %v", got)
	}
	if pushed := pusher.take(); len(pushed) != 2 || len(pushed[ana.ID]) != 0 {
		t.Fatalf("push do fim (não para quem desligou) = %+v", pushed)
	}
	env.must("", http.MethodGet, "/api/public/theft/"+token, nil, http.StatusGone)
	// Já desligado: nem pede a confirmação.
	end(tokAna, "CANCELLED", false, http.StatusConflict)
	if v := read(tokAna); v.Mode != nil || v.PublicURL != "" {
		t.Fatalf("desligado = %+v", v)
	}

	// Fora do ar: fica pendente (sem comando falho a cada volta da rotina) e
	// vai quando o rastreador voltar.
	env.sender.setConnected(imei, false)
	_ = json.Unmarshal(env.must(tokAna, http.MethodPost, path, nil, http.StatusCreated), &on)
	if on.Mode.BoostStatus != "PENDING" {
		t.Fatalf("fora do ar = %+v", on)
	}
	env.theft.Work(ctx)
	var failed int
	if err := env.db.QueryRow(ctx, `SELECT COUNT(*) FROM device_commands WHERE device_id = $1 AND status = 'FAILED'`, dev.ID).Scan(&failed); err != nil || failed != 0 {
		t.Fatalf("comandos falhos = %d (%v)", failed, err)
	}
	env.sender.setConnected(imei, true)
	env.theft.Work(ctx)
	if v := read(tokAna); v.Mode == nil || v.Mode.BoostStatus != "SENT" || len(timers()) != 3 {
		t.Fatalf("voltou = %+v %v", v, timers())
	}
	mailer.take()
	pusher.take()

	// Um dia depois: o lembrete, só para o dono, uma vez.
	clock = clock.Add(25 * time.Hour)
	env.theft.Work(ctx)
	env.theft.Work(ctx)
	if got := mailer.take(); len(got) != 1 || got[0] != "lembrete → ana@roubo.test" {
		t.Fatalf("lembrete = %v", got)
	}
	// Passou do prazo: desliga sozinho, volta o intervalo e avisa todos.
	clock = clock.Add(48 * time.Hour)
	env.theft.Work(ctx)
	if v := read(tokAna); v.Mode != nil {
		t.Fatalf("depois do prazo = %+v", v)
	}
	if got := timers(); len(got) != 4 || got[3] != "TIMER,10,3600#" {
		t.Fatalf("volta no prazo = %v", got)
	}
	if got := mailer.take(); !sameSet(got, "desligou sozinho → ana@roubo.test", "desligou sozinho → caio@roubo.test",
		"desligou sozinho → dani@roubo.test") {
		t.Fatalf("e-mails do prazo = %v", got)
	}
	var outcomes []string
	rows, err := env.db.Query(ctx, `SELECT outcome FROM theft_modes WHERE vehicle_id = $1 ORDER BY activated_at`, moto.ID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var o string
		_ = rows.Scan(&o)
		outcomes = append(outcomes, o)
	}
	rows.Close()
	if strings.Join(outcomes, ",") != "RECOVERED,EXPIRED" {
		t.Errorf("desfechos = %v", outcomes)
	}
}
