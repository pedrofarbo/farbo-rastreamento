package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/commands"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/devices"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/fulfillment"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/leakcheck"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/protocols"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/protocols/gt06"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/smsmarket"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/smssetup"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/telemetry"
)

// fakeProvider imita a SMS Market: guarda os SMS, devolve a situação de entrega e
// as respostas que o teste mandar.
type fakeProvider struct {
	mu       sync.Mutex
	sent     []fakeSMS
	statuses map[string]string
	reasons  map[string]string
	inbox    []smsmarket.Reply
	fail     error
	inboxes  int
	balance  int
}

type fakeSMS struct{ to, body string }

// fakeCommander imita a conexão do rastreador: cada texto tem a sequência de
// desfechos, um por envio (o último se repete; sem nenhum, responde OK). Um
// desfecho sem SentAt é o rastreador fora do ar.
type fakeCommander struct {
	mu       sync.Mutex
	sent     []string
	outcomes map[string][]commands.Command
	byID     map[uuid.UUID]commands.Command
}

func (f *fakeCommander) Send(_ context.Context, req commands.Request) (*commands.Command, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if req.Type != protocols.CommandCustom || req.Device == nil {
		return nil, fmt.Errorf("pedido inesperado: %+v", req)
	}
	n := 0
	for _, text := range append(f.sent, req.RawOverride) {
		if text == req.RawOverride {
			n++
		}
	}
	f.sent = append(f.sent, req.RawOverride)
	out := commands.Command{Status: commands.StatusAcknowledged, Response: "OK"}
	if seq := f.outcomes[req.RawOverride]; len(seq) > 0 {
		out = seq[min(n, len(seq))-1]
	}
	out.ID, out.DeviceID = uuid.New(), req.Device.ID
	f.byID[out.ID] = out
	if out.SentAt == nil {
		return &out, nil // nem saiu: o envio já volta FAILED
	}
	sent := out
	sent.Status, sent.Response, sent.Error = commands.StatusSent, "", ""
	return &sent, nil
}

func (f *fakeCommander) Get(_ context.Context, id uuid.UUID) (*commands.Command, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.byID[id]
	return &out, nil
}

func (f *fakeCommander) texts() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.sent...)
}

func (f *fakeProvider) Send(_ context.Context, to, body string) (*smsmarket.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return nil, f.fail
	}
	f.sent = append(f.sent, fakeSMS{to, body})
	return &smsmarket.Message{ID: fmt.Sprint(len(f.sent)), Status: smsmarket.StatusQueued}, nil
}

func (f *fakeProvider) Fetch(_ context.Context, id string) (*smsmarket.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return &smsmarket.Message{ID: id, Status: f.statuses[id], ErrorMessage: f.reasons[id]}, nil
}

func (f *fakeProvider) Inbox(context.Context, time.Time) ([]smsmarket.Reply, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inboxes++
	return append([]smsmarket.Reply(nil), f.inbox...), nil
}

func (f *fakeProvider) Balance(context.Context) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.balance, nil
}

func (f *fakeProvider) reply(r smsmarket.Reply) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inbox = append(f.inbox, r)
}

func (f *fakeProvider) setStatus(id, status, reason string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statuses[id], f.reasons[id] = status, reason
}

func (f *fakeProvider) bodies() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.sent))
	for i, s := range f.sent {
		out[i] = s.body
	}
	return out
}

// A configuração na ativação, de ponta a ponta com a SMS Market e a conexão de
// mentira: por SMS só o APN e o servidor, um por vez (pela entrega, pela
// resposta do rastreador ou pelo tempo), a entrega e as respostas vêm das
// consultas à SMS Market (sem gravar resposta repetida), a senha do APN nunca
// aparece, e o pedido passa a "Configurado" quando o rastreador conecta. Aí o
// resto vai pela conexão, esperando a resposta de cada um; o que fica sem
// resposta vai por SMS. E as falhas: recusa da SMS Market, SMS não entregue,
// rastreador que não conecta. Precisa de FARBO_TEST_DATABASE_URL.
func TestSMSSetupEndToEnd(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{
		HTTP: config.HTTP{RateLimitRPS: 1000, RateLimitBurst: 1000},
		Auth: config.Auth{
			JWTSecret:      []byte("segredo-de-teste-integracao-0123456789abcdef"),
			AccessTokenTTL: time.Hour, RefreshTokenTTL: time.Hour, BcryptCost: bcrypt.MinCost,
		},
	}
	authSvc := auth.NewService(auth.NewRepository(db), cfg.Auth, nil, log)
	for _, role := range []string{auth.RoleAdmin, auth.RoleOperator, auth.RoleViewer} {
		if _, err := authSvc.CreateUser(ctx, role+"@sms.test", role, role, userPassword); err != nil {
			t.Fatal(err)
		}
	}
	devicesSvc := devices.NewService(devices.NewRepository(db), protocols.NewRegistry(gt06.New(false)))
	fulfillmentSvc := fulfillment.NewService(db, fulfillment.NewRepository(db), nil, nil, cfg.Shipping, "", log)
	fake := &fakeProvider{statuses: map[string]string{}, reasons: map[string]string{}}
	now := time.Now()
	clock := func() time.Time { return now }
	defaults := smssetup.Defaults{ServerHost: "farborastreadores.com.br", ServerPort: 5000, ReportSeconds: 30, ParkedSeconds: 3600}
	sms := smssetup.NewService(db, devicesSvc, fake, fulfillmentSvc, defaults, "SMS Market", log)
	sms.SetClock(clock)
	// Pela conexão: o intervalo encontra o rastreador fora do ar uma vez; a
	// consulta fica sem resposta duas vezes (e vai por SMS).
	sentAt := time.Now()
	conn := &fakeCommander{byID: map[uuid.UUID]commands.Command{}, outcomes: map[string][]commands.Command{
		"GMT,W,0,0#": {{Status: commands.StatusAcknowledged, Response: "GMT:W,0,0 OK", SentAt: &sentAt}},
		"TIMER,30,3600#": {
			{Status: commands.StatusFailed, Error: commands.ErrNotConnected.Error()},
			{Status: commands.StatusAcknowledged, Response: "TIMER:30,3600 OK", SentAt: &sentAt},
		},
		"PARAM#":                       {{Status: commands.StatusTimeout, SentAt: &sentAt}},
		"SZCS#GT06SEL=1#GT06IEXVOL=2#": {{Status: commands.StatusAcknowledged, Response: "SETOK", SentAt: &sentAt}},
	}}
	sms.SetCommander(conn)
	sms.RunOnlineInline()
	server := NewServer(Deps{
		Config: cfg, Log: log, Metrics: telemetry.NewMetrics(), DB: db, Auth: authSvc, Devices: devicesSvc,
		Audit: audit.NewService(audit.NewRepository(db), log), Fulfillment: fulfillmentSvc, SMSSetup: sms,
	})
	srv := httptest.NewServer(server.Handler())
	t.Cleanup(srv.Close)
	env := &credEnv{t: t, db: db, srv: srv}
	operator := env.login(auth.RoleOperator + "@sms.test")
	call := func(method, path string, body any, want int, out any) []byte {
		t.Helper()
		raw := env.must(operator, method, path, body, want)
		if found := leakcheck.Find(raw, "Senh4APN"); len(found) > 0 {
			t.Fatalf("%s %s mostra a senha do APN: %v", method, path, found)
		}
		if out != nil {
			if err := json.Unmarshal(raw, out); err != nil {
				t.Fatalf("%s %s: %v (%s)", method, path, err, raw)
			}
		}
		return raw
	}
	work := func(advance time.Duration) {
		now = now.Add(advance)
		sms.Work(ctx)
	}

	// Um pedido em configuração e o rastreador dele (com o chip).
	customer, err := authSvc.CreateUser(ctx, "cliente@sms.test", "Cliente", auth.RoleCustomer, userPassword)
	if err != nil {
		t.Fatal(err)
	}
	var vehicleID, orderID uuid.UUID
	if err := db.QueryRow(ctx, `INSERT INTO vehicles (name, plate, owner_id) VALUES ('Moto', 'SMS1A11', $1) RETURNING id`, customer.ID).Scan(&vehicleID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `
		INSERT INTO fulfillments (customer_id, vehicle_id, chip_status, tracker_status)
		VALUES ($1, $2, 'SEPARATED', 'CONFIGURING') RETURNING id`, customer.ID, vehicleID).Scan(&orderID); err != nil {
		t.Fatal(err)
	}
	newDevice := func(imei, phone string) *devices.Device {
		t.Helper()
		d, err := devicesSvc.Create(ctx, devices.Input{
			IMEI: imei, Model: "J16", PhoneNumber: phone, APN: "zap.vivo.com.br", APNUser: "vivo", APNPassword: "Senh4APN",
		})
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	j16 := newDevice("869999000000011", "(11) 99999-8888")

	// A prévia: os comandos redigidos; quem vê o painel sem operar, não.
	env.must(env.login(auth.RoleViewer+"@sms.test"), http.MethodGet, "/api/devices/"+j16.ID.String()+"/sms-setup", nil, http.StatusForbidden)
	var view struct {
		Enabled bool              `json:"enabled"`
		From    string            `json:"from"`
		Plan    smssetup.Plan     `json:"plan"`
		Session *smssetup.Session `json:"session"`
	}
	call(http.MethodGet, "/api/devices/"+j16.ID.String()+"/sms-setup", nil, http.StatusOK, &view)
	if !view.Enabled || view.From != "SMS Market" || view.Plan.Phone != "+5511999998888" ||
		len(view.Plan.Steps) != 5 || view.Plan.Steps[0].Text != "APN,zap.vivo.com.br,vivo,***#" || view.Plan.Steps[1].Via != "" ||
		view.Plan.Steps[2].Via != smssetup.ViaConnection || view.Plan.Steps[4].Text != "SZCS#GT06SEL=1#GT06IEXVOL=2#" ||
		view.Plan.Unlock.Text != "CMDLOCK,123456,0#" || view.Plan.Unlock.Via != "" ||
		view.Plan.Query.Text != "PARAM#" || view.Plan.Query.Via != smssetup.ViaConnection || len(view.Plan.Problems) != 0 || view.Session != nil {
		t.Fatalf("prévia = %+v", view)
	}
	semChip := newDevice("869999000000029", "")
	call(http.MethodGet, "/api/devices/"+semChip.ID.String()+"/sms-setup", nil, http.StatusOK, &view)
	if len(view.Plan.Problems) == 0 || !strings.Contains(view.Plan.Problems[0], "número do chip") {
		t.Errorf("sem chip = %+v", view.Plan)
	}
	env.must(operator, http.MethodPost, "/api/devices/"+semChip.ID.String()+"/sms-setup", map[string]any{}, http.StatusBadRequest)

	// Começa: o primeiro SMS sai na hora, com a senha de verdade só para a SMS Market.
	var session smssetup.Session
	call(http.MethodPost, "/api/devices/"+j16.ID.String()+"/sms-setup",
		map[string]any{"fulfillmentId": orderID, "query": true}, http.StatusCreated, &session)
	if session.Status != smssetup.StatusSending || len(session.Steps) != 6 || session.Steps[0].Status != "queued" ||
		session.Steps[1].Status != "pending" {
		t.Fatalf("começo = %+v", session)
	}
	if fake.sent[0].to != "+5511999998888" || fake.sent[0].body != "APN,zap.vivo.com.br,vivo,Senh4APN#" {
		t.Fatalf("primeiro SMS = %+v", fake.sent[0])
	}
	env.must(operator, http.MethodPost, "/api/devices/"+j16.ID.String()+"/sms-setup", map[string]any{}, http.StatusBadRequest)

	// Sem notícia, o próximo espera.
	work(5 * time.Second)
	if len(fake.bodies()) != 1 {
		t.Fatalf("mandou antes da hora: %v", fake.bodies())
	}
	// A consulta de entrega diz que chegou (RECEBIDA): o próximo sai.
	fake.setStatus("1", smsmarket.StatusDelivered, "")
	work(21 * time.Second)
	if b := fake.bodies(); len(b) != 2 || b[1] != "SERVER,1,farborastreadores.com.br,5000,0#" {
		t.Fatalf("depois da entrega: %v", b)
	}
	// Um status atrasado não volta para trás.
	if err := sms.UpdateStatus(ctx, "1", smsmarket.StatusSent, "", ""); err != nil {
		t.Fatal(err)
	}
	// A resposta do rastreador (lida na SMS Market, pelo número do chip) libera o
	// próximo na hora; a senha que ele ecoa fica redigida. O próximo é pela
	// conexão: nenhum SMS a mais, e passa a esperar o rastreador conectar.
	fake.reply(smsmarket.Reply{ID: "9001", SentID: "2", From: "5511999998888", Body: "APN:zap.vivo.com.br,vivo,Senh4APN OK"})
	work(time.Second)
	if b := fake.bodies(); len(b) != 2 {
		t.Fatalf("depois da resposta: %v", b)
	}
	call(http.MethodGet, "/api/devices/"+j16.ID.String()+"/sms-setup", nil, http.StatusOK, &view)
	if view.Session == nil || view.Session.Status != smssetup.StatusWaiting || view.Session.Steps[0].Status != "delivered" ||
		view.Session.Steps[2].Via != smssetup.ViaConnection || view.Session.Steps[2].Status != "pending" ||
		len(view.Session.Replies) != 1 || view.Session.Replies[0].Body != "APN:zap.vivo.com.br,vivo,*** OK" {
		t.Fatalf("esperando = %+v", view.Session)
	}
	if len(conn.texts()) != 0 {
		t.Fatalf("mandou pela conexão antes de ela existir: %v", conn.texts())
	}

	// O rastreador conectou: o pedido passa a "Configurado", com o aparelho no veículo.
	if err := devicesSvc.Touch(ctx, j16.ID, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	work(10 * time.Second)
	call(http.MethodGet, "/api/devices/"+j16.ID.String()+"/sms-setup", nil, http.StatusOK, &view)
	if view.Session.Status != smssetup.StatusDone || view.Session.ConnectedAt == nil || !strings.Contains(view.Session.Note, "Configurado") {
		t.Fatalf("conectou = %+v", view.Session)
	}
	// Pela conexão, na ordem: o fuso, o intervalo (de novo depois de o
	// rastreador voltar), a consulta (duas vezes sem resposta: vai por SMS) e
	// a voltagem por último.
	if got := strings.Join(conn.texts(), " "); got != "GMT,W,0,0# TIMER,30,3600# TIMER,30,3600# PARAM# PARAM# SZCS#GT06SEL=1#GT06IEXVOL=2#" {
		t.Fatalf("pela conexão: %s", got)
	}
	if b := fake.bodies(); len(b) != 3 || b[2] != "PARAM#" {
		t.Fatalf("SMS de reserva: %v", b)
	}
	steps := view.Session.Steps
	if view.Session.Online || steps[2].Status != smssetup.OnlineConfirmed || steps[2].Reply != "GMT:W,0,0 OK" ||
		steps[3].Status != smssetup.OnlineConfirmed || steps[4].Status != "queued" || !steps[4].Fallback ||
		steps[5].Status != smssetup.OnlineConfirmed || steps[5].Reply != "SETOK" || steps[5].SentAt == nil {
		t.Fatalf("passos pela conexão = %+v", steps)
	}
	// Na volta seguinte, nada sai de novo.
	work(10 * time.Second)
	if len(conn.texts()) != 6 || len(fake.bodies()) != 3 {
		t.Errorf("repetiu: %v %v", conn.texts(), fake.bodies())
	}
	var trackerStatus string
	var linked *uuid.UUID
	if err := db.QueryRow(ctx, `SELECT f.tracker_status, v.device_id FROM fulfillments f JOIN vehicles v ON v.id = f.vehicle_id WHERE f.id = $1`, orderID).
		Scan(&trackerStatus, &linked); err != nil {
		t.Fatal(err)
	}
	if trackerStatus != fulfillment.TrackerConfigured || linked == nil || *linked != j16.ID {
		t.Fatalf("pedido = %s, aparelho %v", trackerStatus, linked)
	}
	var automatic bool
	var note string
	if err := db.QueryRow(ctx, `SELECT automatic, note FROM fulfillment_events WHERE fulfillment_id = $1 AND status = 'CONFIGURED'`, orderID).
		Scan(&automatic, &note); err != nil || !automatic || !strings.Contains(note, "SMS") {
		t.Errorf("histórico: automático=%v nota=%q err=%v", automatic, note, err)
	}
	// A resposta lida de novo (a consulta traz as do dia) não é gravada outra
	// vez; a de um número que não é de rastreador é ignorada.
	fake.reply(smsmarket.Reply{ID: "9002", From: "5521988887777", Body: "oi"})
	work(time.Second)
	var replies, strangers int
	_ = db.QueryRow(ctx, `SELECT count(*) FILTER (WHERE device_id = $1), count(*) FILTER (WHERE phone LIKE '%21988887777')
		FROM sms_messages WHERE direction = 'IN'`, j16.ID).Scan(&replies, &strangers)
	if replies != 1 || strangers != 0 {
		t.Errorf("respostas gravadas = %d, de estranhos = %d", replies, strangers)
	}

	// Recusa da SMS Market: a configuração falha na hora, com o motivo.
	fake.fail = &smsmarket.APIError{HTTPStatus: http.StatusOK, Code: "300", Message: "SALDO INSUFICIENTE"}
	recusado := newDevice("869999000000037", "(11) 98888-0000")
	call(http.MethodPost, "/api/devices/"+recusado.ID.String()+"/sms-setup", map[string]any{}, http.StatusCreated, &session)
	if session.Status != smssetup.StatusFailed || !strings.Contains(session.Error, "SALDO INSUFICIENTE") || !strings.Contains(session.Error, "SMS Market") {
		t.Fatalf("recusa = %+v", session)
	}
	// Sem resposta da SMS Market: tenta de novo, e desiste na terceira.
	fake.fail = errors.New("SMS Market indisponível: timeout")
	instavel := newDevice("869999000000045", "(11) 97777-0000")
	call(http.MethodPost, "/api/devices/"+instavel.ID.String()+"/sms-setup", map[string]any{}, http.StatusCreated, &session)
	if session.Status != smssetup.StatusSending {
		t.Fatalf("primeira falha de rede = %+v", session)
	}
	work(10 * time.Second)
	work(10 * time.Second)
	call(http.MethodGet, "/api/devices/"+instavel.ID.String()+"/sms-setup", nil, http.StatusOK, &view)
	if view.Session.Status != smssetup.StatusFailed {
		t.Fatalf("rede caída = %+v", view.Session)
	}
	fake.fail = nil

	// SMS não entregue (o chip não recebe): falha com o motivo.
	naoEntregue := newDevice("869999000000052", "(11) 96666-0000")
	call(http.MethodPost, "/api/devices/"+naoEntregue.ID.String()+"/sms-setup", map[string]any{}, http.StatusCreated, &session)
	fake.setStatus(fmt.Sprint(len(fake.bodies())), smsmarket.StatusFailed, "o número está na lista de bloqueio da SMS Market")
	work(21 * time.Second)
	call(http.MethodGet, "/api/devices/"+naoEntregue.ID.String()+"/sms-setup", nil, http.StatusOK, &view)
	if view.Session.Status != smssetup.StatusFailed || !strings.Contains(view.Session.Error, "não chegou ao chip") ||
		!strings.Contains(view.Session.Error, "lista de bloqueio") {
		t.Fatalf("não entregue = %+v", view.Session)
	}

	// Não conectou no prazo; e a que é cancelada.
	mudo := newDevice("869999000000060", "(11) 95555-0000")
	call(http.MethodPost, "/api/devices/"+mudo.ID.String()+"/sms-setup", map[string]any{}, http.StatusCreated, &session)
	// Sem entrega nem resposta, o próximo sai pelo tempo.
	work(91 * time.Second)
	if last := fake.sent[len(fake.sent)-1]; last.to != "+5511955550000" || last.body != "SERVER,1,farborastreadores.com.br,5000,0#" {
		t.Fatalf("pelo tempo: %+v", last)
	}
	for range 4 {
		work(91 * time.Second)
	}
	work(21 * time.Minute)
	call(http.MethodGet, "/api/devices/"+mudo.ID.String()+"/sms-setup", nil, http.StatusOK, &view)
	if view.Session.Status != smssetup.StatusTimeout || !strings.Contains(view.Session.Error, "não conectou") {
		t.Fatalf("sem conexão = %+v", view.Session)
	}
	call(http.MethodPost, "/api/devices/"+mudo.ID.String()+"/sms-setup", map[string]any{"unlock": true}, http.StatusCreated, &session)
	if fake.sent[len(fake.sent)-1].body != "CMDLOCK,123456,0#" {
		t.Errorf("com o destravar, o primeiro é %q", fake.sent[len(fake.sent)-1].body)
	}
	call(http.MethodPost, "/api/sms-setup/"+session.ID.String()+"/cancel", nil, http.StatusOK, &session)
	if session.Status != smssetup.StatusCanceled {
		t.Errorf("cancelada = %+v", session)
	}
	env.must(operator, http.MethodPost, "/api/sms-setup/"+session.ID.String()+"/cancel", nil, http.StatusBadRequest)

	// O contador da página de rastreadores: o saldo vira ativações (2 SMS
	// cada) e o mês conta os SMS que saíram (sem os recusados). O saldo fica
	// guardado por um minuto; quem só vê o painel não vê o contador.
	fake.mu.Lock()
	fake.balance = 1003
	fake.mu.Unlock()
	var usage smssetup.Usage
	call(http.MethodGet, "/api/sms/usage", nil, http.StatusOK, &usage)
	var sent int
	_ = db.QueryRow(ctx, `SELECT count(*) FROM sms_messages WHERE direction = 'OUT' AND status <> 'failed'`).Scan(&sent)
	if !usage.Enabled || usage.Balance == nil || *usage.Balance != 1003 || usage.ActivationSMS != 2 || usage.Activations != 501 ||
		usage.SentLast30Days != sent || sent == 0 {
		t.Fatalf("contador = %+v (enviados %d)", usage, sent)
	}
	fake.mu.Lock()
	fake.balance = 8
	fake.mu.Unlock()
	call(http.MethodGet, "/api/sms/usage", nil, http.StatusOK, &usage)
	if *usage.Balance != 1003 {
		t.Errorf("leu de novo antes de um minuto: %d", *usage.Balance)
	}
	now = now.Add(61 * time.Second)
	call(http.MethodGet, "/api/sms/usage", nil, http.StatusOK, &usage)
	if *usage.Balance != 8 || usage.Activations != 4 {
		t.Errorf("depois de um minuto = %+v", usage)
	}
	env.must(env.login(auth.RoleViewer+"@sms.test"), http.MethodGet, "/api/sms/usage", nil, http.StatusForbidden)

	// Teste de ativação em massa:
	env.must(env.login(auth.RoleViewer+"@sms.test"), http.MethodPost, "/api/devices/bulk/sms-setup",
		map[string]any{"deviceIds": []string{j16.ID.String()}}, http.StatusForbidden)
	env.must(operator, http.MethodPost, "/api/devices/bulk/sms-setup",
		map[string]any{"deviceIds": []string{}}, http.StatusBadRequest)

	bulkDevice1 := newDevice("869999000000102", "(11) 94444-0000")
	bulkDevice2 := newDevice("869999000000110", "") // sem chip

	var bulkResp BulkSMSSetupResponse
	call(http.MethodPost, "/api/devices/bulk/sms-setup",
		map[string]any{"deviceIds": []string{bulkDevice1.ID.String(), bulkDevice2.ID.String()}},
		http.StatusOK, &bulkResp)
	if bulkResp.Total != 2 || bulkResp.Started != 1 || bulkResp.Failed != 1 || len(bulkResp.Results) != 2 {
		t.Fatalf("bulkResp inesperado: %+v", bulkResp)
	}
	if !bulkResp.Results[0].Success || bulkResp.Results[0].SessionID == nil || bulkResp.Results[0].Status != smssetup.StatusSending {
		t.Errorf("bulk item 1 falhou: %+v", bulkResp.Results[0])
	}
	if bulkResp.Results[1].Success || !strings.Contains(bulkResp.Results[1].Error, "número do chip") {
		t.Errorf("bulk item 2 devia ter falhado: %+v", bulkResp.Results[1])
	}
	// O lote só grava as sessões: nenhum SMS sai na requisição (com dezenas
	// de rastreadores, esperar a SMS Market de cada um passaria do tempo dela); o
	// primeiro sai na volta seguinte da rotina.
	fake.mu.Lock()
	beforeWork := len(fake.sent)
	fake.mu.Unlock()
	sms.Work(ctx)
	fake.mu.Lock()
	afterWork := fake.sent[beforeWork:]
	fake.mu.Unlock()
	if len(afterWork) != 1 || afterWork[0].to != "+5511944440000" {
		t.Errorf("depois do lote e da rotina, saiu %+v", afterWork)
	}
	// Cada rastreador do lote fica na auditoria (o que começou e o recusado).
	var audited int
	_ = db.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE action = 'SMS_SETUP' AND metadata->>'bulk' = 'true'`).Scan(&audited)
	if audited != 2 {
		t.Errorf("auditoria do lote: %d registros", audited)
	}
	// O contador passa a contar o SMS do lote.
	_ = db.QueryRow(ctx, `SELECT count(*) FROM sms_messages WHERE direction = 'OUT' AND status <> 'failed'`).Scan(&sent)

	// Sem a SMS Market configurada, nada sai (e o contador diz que está desligado).
	off := smssetup.NewService(db, devicesSvc, nil, fulfillmentSvc, defaults, "", log)
	if _, err := off.Start(ctx, mudo.ID, nil, smssetup.Options{}, nil); !errors.Is(err, smssetup.ErrDisabled) {
		t.Errorf("desligado: %v", err)
	}
	if u, err := off.Usage(ctx); err != nil || u.Enabled || u.Balance != nil || u.SentLast30Days != sent {
		t.Errorf("contador desligado = %+v %v", u, err)
	}

	// IMEI suspeito no cadastro (o caso de 13/10: um zero a mais): nenhum SMS
	// sai — o rastreador pegaria a configuração e seria recusado.
	suspeito, err := devicesSvc.Create(ctx, devices.Input{
		IMEI: "8658930088121639", IMEIConfirmed: true, Model: "J16", Protocol: "gt06", PhoneNumber: "(16) 99528-7689",
		APN: "zap.vivo.com.br", APNUser: "vivo", APNPassword: "Senh4APN",
	})
	if err != nil {
		t.Fatal(err)
	}
	before := len(fake.bodies())
	if plan, err := sms.Plan(ctx, suspeito.ID); err != nil || len(plan.Problems) != 1 ||
		!strings.Contains(plan.Problems[0], "O IMEI cadastrado (8658930088121639) tem 16 dígitos") {
		t.Errorf("plano com IMEI suspeito = %+v %v", plan, err)
	}
	if _, err := sms.Start(ctx, suspeito.ID, nil, smssetup.Options{}, nil); err == nil || !strings.Contains(err.Error(), "Corrija o IMEI") {
		t.Errorf("começar com IMEI suspeito = %v", err)
	}
	if len(fake.bodies()) != before {
		t.Error("mandou SMS com o IMEI suspeito")
	}

	// IMEI válido, mas de outro rastreador (dois números errados): o SMS sai,
	// o rastreador conecta com o IMEI de verdade e é recusado — a configuração
	// mostra qual IMEI está chegando.
	trocado := newDevice("865893088021631", "(16) 99528-7000")
	if err := devicesSvc.RecordUnknown(ctx, "865893088121639", "gt06", "187.32.97.125:58772"); err != nil {
		t.Fatal(err)
	}
	other, err := sms.Start(ctx, trocado.ID, nil, smssetup.Options{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if w := other.WrongIMEI; w == nil || w.Registered != "865893088021631" || w.Connecting != "865893088121639" || w.Attempts != 1 {
		t.Errorf("rastreador conectando com outro IMEI = %+v", w)
	}
}
