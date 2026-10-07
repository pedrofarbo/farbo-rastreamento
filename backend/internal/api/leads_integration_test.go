package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/addresses"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/affiliates"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/devices"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/events"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/finance"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/fulfillment"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/geofences"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/leads"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/orders"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/payments"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/protocols"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/protocols/gt06"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/retention"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/telemetry"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/tracking"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/vehicles"
	ws "github.com/pedrofarbo/farbo-rastreamento/backend/internal/websocket"
)

type recordingLeadNotifier struct {
	mu    sync.Mutex
	names []string
}

func (n *recordingLeadNotifier) NewLead(_ context.Context, l *leads.Lead) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.names = append(n.names, l.Name)
	return nil
}

func (n *recordingLeadNotifier) count() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.names)
}

// newLeadsEnv sobe a API com os pré-clientes e um admin e um operador
// (ver integrationDB).
// tweak ajusta as dependências antes de subir a API (ex.: o Melhor Envios
// de mentira dos testes de frete).
func newLeadsEnv(t *testing.T, tweak ...func(*Deps)) (*credEnv, *recordingLeadNotifier) {
	t.Helper()
	db := integrationDB(t)
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{
		HTTP: config.HTTP{RateLimitRPS: 1000, RateLimitBurst: 1000},
		Auth: config.Auth{
			JWTSecret:      []byte("segredo-de-teste-integracao-0123456789abcdef"),
			AccessTokenTTL: time.Hour, RefreshTokenTTL: time.Hour, BcryptCost: bcrypt.MinCost,
		},
		Tracking: config.Tracking{StaleAfter: 5 * time.Minute, OfflineAfter: time.Hour, HistoryRetentionDays: 30},
		Billing:  config.Billing{Timezone: "UTC", InvoiceLeadDays: 10},
		// Duas vagas só na promoção, para o teste chegar ao limite.
		Catalog: config.Catalog{
			PlanName: "Plano Mensal", PlanPriceCents: 6990, DefaultDueDay: 10,
			EquipmentName: "Rastreador J16 GT06", EquipmentPriceCents: 15000, SetupDueDays: 3, EquipmentMaxInstallments: 10,
			LaunchPromo: config.LaunchPromo{
				Enabled: true, EquipmentCents: 12000, MonthlyCents: 3490, Months: 12, Slots: 2,
				InsanosMonthlyCents: 2790, InsanosPlanName: "Especial Insanos MC",
			},
		},
	}
	authSvc := auth.NewService(auth.NewRepository(db), cfg.Auth, nil, log)
	for _, role := range []string{auth.RoleAdmin, auth.RoleOperator} {
		if _, err := authSvc.CreateUser(ctx, role+"@leads.test", role, role, userPassword); err != nil {
			t.Fatal(err)
		}
	}
	billingSvc, err := billing.NewService(billing.NewRepository(db), cfg.Billing, log)
	if err != nil {
		t.Fatal(err)
	}
	auditSvc := audit.NewService(audit.NewRepository(db), log)
	notifier := &recordingLeadNotifier{}
	hub := ws.NewHub(log, nil)
	metrics := telemetry.NewMetrics()
	devicesSvc := devices.NewService(devices.NewRepository(db), protocols.NewRegistry(gt06.New(false)))
	vehiclesSvc := vehicles.NewService(vehicles.NewRepository(db))
	positions := tracking.NewRepository(db)
	states := tracking.NewStateStore(tracking.NewStateRepository(db))
	fences := geofences.NewService(geofences.NewRepository(db))
	ingestor := tracking.NewIngestor(devicesSvc, vehiclesSvc, positions, states,
		events.NewService(events.NewRepository(db), hub, log), fences, nil, nil, hub, cfg.Tracking, metrics, log)
	deps := Deps{
		Config: cfg, Log: log, Metrics: metrics, DB: db, Auth: authSvc, Audit: auditSvc,
		Billing:   billingSvc,
		Payments:  payments.NewService(payments.NewRepository(db), billingSvc, nil, auditSvc, cfg.Payments, log),
		Devices:   devicesSvc,
		Vehicles:  vehiclesSvc,
		Owners:    vehicles.NewOwnerIndex(db, log),
		Positions: positions, States: states, Ingestor: ingestor, Geofences: fences,
		Orders:      orders.NewService(db, billingSvc, vehiclesSvc, cfg.Catalog, log),
		Addresses:   addresses.NewRepository(db),
		Fulfillment: fulfillment.NewService(db, fulfillment.NewRepository(db), nil, nil, cfg.Shipping, "", log),
		Retention:   retention.NewService(db, cfg.Tracking.HistoryRetentionDays, log),
		Leads:       leads.NewService(leads.NewRepository(db), notifier, log),
		Finance:     finance.NewService(db, &fakeFinanceMailer{}, log),
		Affiliates:  affiliates.NewService(db, log),
		WS:          ws.NewHandler(hub, nil), Hub: hub,
	}
	for _, f := range tweak {
		f(&deps)
	}
	server := NewServer(deps)
	srv := httptest.NewServer(server.Handler())
	t.Cleanup(srv.Close)
	env := &credEnv{t: t, db: db, srv: srv}
	return env, notifier
}

// Pré-clientes de ponta a ponta: a landing manda, a central vê, muda a
// situação e cadastra o cliente a partir dele. Precisa de
// FARBO_TEST_DATABASE_URL.
func TestLeadsEndToEnd(t *testing.T) {
	env, notifier := newLeadsEnv(t)

	form := map[string]any{
		"name": "Ana Souza", "email": "ana@exemplo.com.br", "phone": "(11) 98888-7777", "city": "Campinas",
		"plan": "Plano Mensal - R$ 69,90", "vehicleType": "moto", "vehicleCount": 2, "message": "Tenho duas motos",
		"consent": true, "website": "",
	}
	with := func(changes map[string]any) map[string]any {
		out := map[string]any{}
		for k, v := range form {
			out[k] = v
		}
		for k, v := range changes {
			out[k] = v
		}
		return out
	}

	// A landing aceita 5 envios seguidos por IP; o que vem depois espera.
	// Sem consentimento, recusado (as outras recusas estão no teste do pacote).
	env.must("", http.MethodPost, "/api/public/leads", with(map[string]any{"consent": false}), http.StatusBadRequest)
	// Isca preenchida: "ok" para o robô, nada gravado.
	env.must("", http.MethodPost, "/api/public/leads", with(map[string]any{"website": "http://spam"}), http.StatusCreated)

	// Envio de verdade e, depois, o mesmo e-mail de novo: um pré-cliente só,
	// atualizado, e um aviso só para a equipe.
	out := env.must("", http.MethodPost, "/api/public/leads", form, http.StatusCreated)
	if string(out) != "{\"status\":\"ok\"}\n" {
		t.Errorf("resposta pública devolve dados: %s", out)
	}
	env.must("", http.MethodPost, "/api/public/leads",
		with(map[string]any{"email": "ANA@exemplo.com.br", "vehicleCount": 3}), http.StatusCreated)

	// A central: só o admin vê.
	op := env.login(auth.RoleOperator + "@leads.test")
	env.must(op, http.MethodGet, "/api/leads", nil, http.StatusForbidden)
	admin := env.login(auth.RoleAdmin + "@leads.test")

	var list []leads.Lead
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/leads", nil, http.StatusOK), &list)
	if len(list) != 1 {
		t.Fatalf("pré-clientes = %d, quer 1 (o reenvio atualiza)", len(list))
	}
	lead := list[0]
	if lead.VehicleCount != 3 || lead.Status != leads.StatusNew || lead.City != "Campinas" || lead.Message != "Tenho duas motos" {
		t.Errorf("pré-cliente = %+v", lead)
	}
	deadline := time.Now().Add(2 * time.Second)
	for notifier.count() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if notifier.count() != 1 {
		t.Errorf("avisos à equipe = %d, quer 1", notifier.count())
	}

	var stats map[string]int
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/leads/stats", nil, http.StatusOK), &stats)
	if stats["new"] != 1 {
		t.Errorf("novos = %v", stats)
	}

	// Em contato, com anotação.
	var updated leads.Lead
	_ = json.Unmarshal(env.must(admin, http.MethodPatch, "/api/leads/"+lead.ID.String(),
		map[string]string{"status": leads.StatusContacted, "notes": "Liguei, retorna amanhã"}, http.StatusOK), &updated)
	if updated.Status != leads.StatusContacted || updated.Notes != "Liguei, retorna amanhã" {
		t.Errorf("atualizado = %+v", updated)
	}
	env.must(admin, http.MethodPatch, "/api/leads/"+lead.ID.String(), map[string]string{"status": "OUTRO"}, http.StatusBadRequest)

	// Fechou: o cliente é cadastrado a partir do pré-cliente.
	env.must(admin, http.MethodPost, "/api/customers", map[string]any{
		"name": lead.Name, "email": lead.Email, "phone": lead.Phone, "document": "",
		"password": "senha-do-cliente-123", "leadId": lead.ID,
	}, http.StatusCreated)
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/leads", nil, http.StatusOK), &list)
	if list[0].Status != leads.StatusConverted || list[0].CustomerID == nil {
		t.Errorf("depois do cadastro: %+v", list[0])
	}

	// Convertido, o mesmo e-mail na landing vira outro pré-cliente.
	env.must("", http.MethodPost, "/api/public/leads", form, http.StatusCreated)
	// Sexto envio seguido do mesmo IP: barrado.
	env.must("", http.MethodPost, "/api/public/leads", form, http.StatusTooManyRequests)
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/leads?status=NEW", nil, http.StatusOK), &list)
	if len(list) != 1 {
		t.Errorf("novos depois da conversão = %d, quer 1", len(list))
	}
}

// Lista de lançamento: a landing inscreve, o mesmo e-mail não duplica, a
// central vê e tira quem pediu para sair.
func TestLaunchWaitlistEndToEnd(t *testing.T) {
	env, notifier := newLeadsEnv(t)
	signup := map[string]any{"name": "Bruno", "email": "bruno@exemplo.com.br", "phone": "(11) 97777-6666", "consent": true, "website": ""}

	env.must("", http.MethodPost, "/api/public/launch",
		map[string]any{"email": "bruno@exemplo.com.br", "phone": "(11) 97777-6666", "consent": false}, http.StatusBadRequest)
	env.must("", http.MethodPost, "/api/public/launch",
		map[string]any{"email": "robo@spam.com", "phone": "(11) 90000-0000", "consent": true, "website": "x"}, http.StatusCreated)
	env.must("", http.MethodPost, "/api/public/launch", signup, http.StatusCreated)
	// De novo, com maiúsculas e sem nome: continua um só, com o nome de antes.
	env.must("", http.MethodPost, "/api/public/launch",
		map[string]any{"email": "BRUNO@exemplo.com.br", "phone": "(11) 97777-6666", "consent": true}, http.StatusCreated)

	admin := env.login(auth.RoleAdmin + "@leads.test")
	env.must(env.login(auth.RoleOperator+"@leads.test"), http.MethodGet, "/api/leads/waitlist", nil, http.StatusForbidden)

	var list []leads.WaitlistEntry
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/leads/waitlist", nil, http.StatusOK), &list)
	if len(list) != 1 || list[0].Name != "Bruno" || list[0].Email != "bruno@exemplo.com.br" || list[0].Phone != "(11) 97777-6666" {
		t.Fatalf("lista = %+v (a isca não entra; o reenvio não duplica)", list)
	}
	// A inscrição vira um pré-cliente (um só), na lista, sem avisar a equipe.
	var pre []leads.Lead
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/leads", nil, http.StatusOK), &pre)
	if len(pre) != 1 || pre[0].Email != "bruno@exemplo.com.br" || pre[0].Name != "Bruno" || pre[0].Status != leads.StatusNew ||
		pre[0].Source != "lancamento" || !pre[0].OnLaunchList || notifier.count() != 0 {
		t.Fatalf("pré-clientes = %+v, avisos = %d", pre, notifier.count())
	}
	lead := pre[0].ID.String()

	// Tirar da lista: perde a promoção, o pré-cliente fica.
	env.must(admin, http.MethodDelete, "/api/leads/"+lead+"/launch-list", nil, http.StatusNoContent)
	env.must(admin, http.MethodDelete, "/api/leads/"+lead+"/launch-list", nil, http.StatusBadRequest)
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/leads", nil, http.StatusOK), &pre)
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/leads/waitlist", nil, http.StatusOK), &list)
	if len(pre) != 1 || pre[0].OnLaunchList || len(list) != 0 {
		t.Fatalf("fora da lista: pré-clientes %+v, lista %+v", pre, list)
	}
	// Volta para a lista (inscreve de novo) e pede para apagar tudo (LGPD).
	env.must("", http.MethodPost, "/api/public/launch", signup, http.StatusCreated)
	env.must(admin, http.MethodDelete, "/api/leads/"+lead, nil, http.StatusNoContent)
	env.must(admin, http.MethodDelete, "/api/leads/"+lead, nil, http.StatusNotFound)
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/leads", nil, http.StatusOK), &pre)
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/leads/waitlist", nil, http.StatusOK), &list)
	if len(pre) != 0 || len(list) != 0 {
		t.Errorf("depois de apagar: pré-clientes %+v, lista %+v", pre, list)
	}
	// A rota antiga de tirar da lista continua valendo.
	env.must("", http.MethodPost, "/api/public/launch", signup, http.StatusCreated)
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/leads/waitlist", nil, http.StatusOK), &list)
	env.must(admin, http.MethodDelete, "/api/leads/waitlist/"+list[0].ID.String(), nil, http.StatusNoContent)
	env.must(admin, http.MethodDelete, "/api/leads/waitlist/"+list[0].ID.String(), nil, http.StatusNotFound)
	env.must(env.login(auth.RoleOperator+"@leads.test"), http.MethodDelete, "/api/leads/"+lead, nil, http.StatusForbidden)
}

// Cadastro no evento (a tela aberta pelo QR Code): muita gente pela mesma
// rede se inscreve sem esbarrar no limite, o evento fica gravado (e não some
// quando a pessoa se inscreve de novo pela landing) e o admin baixa o QR
// Code para imprimir.
func TestEventSignupEndToEnd(t *testing.T) {
	env, _ := newLeadsEnv(t)
	for i := range 20 {
		body := map[string]any{"name": fmt.Sprintf("Pessoa %d", i), "email": fmt.Sprintf("pessoa%d@evento.test", i),
			"phone": "(11) 98888-7777", "consent": true, "event": "Encontro Insanos MC — Out/26", "city": "  São   Paulo - SP "}
		env.must("", http.MethodPost, "/api/public/launch", body, http.StatusCreated)
	}
	// Folgado, mas com limite: a 21ª seguida espera.
	env.must("", http.MethodPost, "/api/public/launch", map[string]any{"email": "x@evento.test", "phone": "(11) 98888-7777",
		"consent": true}, http.StatusTooManyRequests)

	admin := env.login(auth.RoleAdmin + "@leads.test")
	var list []leads.WaitlistEntry
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/leads/waitlist", nil, http.StatusOK), &list)
	if len(list) != 20 || list[0].Event != "encontro-insanos-mc-out-26" || list[0].City != "São Paulo - SP" {
		t.Fatalf("inscrições do evento = %d, evento %q, cidade %q", len(list), list[0].Event, list[0].City)
	}
	// Inscrita de novo pela landing: o evento que a trouxe continua.
	if _, err := env.db.Exec(context.Background(), `DELETE FROM launch_waitlist WHERE email <> 'pessoa0@evento.test'`); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2100 * time.Millisecond) // o limite devolve uma vaga a cada 2 s
	env.must("", http.MethodPost, "/api/public/launch", map[string]any{"email": "pessoa0@evento.test", "phone": "(11) 98888-7777",
		"consent": true}, http.StatusCreated)
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/leads/waitlist", nil, http.StatusOK), &list)
	if len(list) != 1 || list[0].Event != "encontro-insanos-mc-out-26" || list[0].Name != "Pessoa 0" || list[0].City != "São Paulo - SP" {
		t.Fatalf("reinscrição pela landing = %+v", list)
	}

	// O QR Code do link do evento, para imprimir.
	link := url.QueryEscape("https://farborastreadores.com.br/evento/encontro-insanos-mc-out-26")
	get := func(token, query string) (*http.Response, []byte) {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, env.srv.URL+"/api/leads/qr?"+query, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		return res, body
	}
	res, body := get(admin, "text="+link+"&name=Encontro+Insanos+MC")
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "image/svg+xml" || !bytes.HasPrefix(body, []byte("<svg")) ||
		!strings.Contains(res.Header.Get("Content-Disposition"), "encontro-insanos-mc.svg") {
		t.Fatalf("SVG = %d %v %.40s", res.StatusCode, res.Header, body)
	}
	if res, body := get(admin, "text="+link+"&format=png"); res.StatusCode != http.StatusOK || !bytes.HasPrefix(body, []byte("\x89PNG")) {
		t.Fatalf("PNG = %d %v", res.StatusCode, res.Header)
	}
	if res, _ := get(admin, "text="); res.StatusCode != http.StatusBadRequest {
		t.Errorf("sem texto = %d", res.StatusCode)
	}
	if res, _ := get(env.login(auth.RoleOperator+"@leads.test"), "text="+link); res.StatusCode != http.StatusForbidden {
		t.Errorf("operador = %d", res.StatusCode)
	}
}

// Promoção de pré-lançamento: quem está na lista contrata o primeiro
// rastreador por R$ 120 e paga R$ 34,90 nos 12 primeiros meses (R$ 27,90 no
// plano do Insanos MC; depois, o plano); uma vaga por cliente, e o ambiente
// tem duas.
func TestLaunchPromoEndToEnd(t *testing.T) {
	env, _ := newLeadsEnv(t)
	ctx := context.Background()
	authSvc := auth.NewService(auth.NewRepository(env.db), config.Auth{BcryptCost: bcrypt.MinCost}, nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	ids := map[string]string{}
	for _, email := range []string{"ana@lista.test", "bia@lista.test", "caio@fora.test", "duda@lista.test"} {
		u, err := authSvc.CreateUser(ctx, email, email, auth.RoleCustomer, userPassword)
		if err != nil {
			t.Fatal(err)
		}
		ids[email] = u.ID.String()
	}
	for _, email := range []string{"ANA@lista.test", "bia@lista.test", "duda@lista.test"} {
		env.must("", http.MethodPost, "/api/public/launch",
			map[string]any{"email": email, "phone": "(11) 95555-4444", "consent": true}, http.StatusCreated)
	}
	admin := env.login(auth.RoleAdmin + "@leads.test")

	// Quem tem direito: na lista, sem ter usado, com vaga.
	var status orders.PromoStatus
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/customers/"+ids["ana@lista.test"]+"/launch-promo", nil, http.StatusOK), &status)
	if !status.Eligible || status.Offer.EquipmentCents != 12000 || status.Offer.MonthlyCents != 3490 || status.Offer.Months != 12 ||
		status.Offer.InsanosMonthlyCents != 2790 || status.Offer.InsanosPlanName != "Especial Insanos MC" {
		t.Fatalf("ana = %+v", status)
	}
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/customers/"+ids["caio@fora.test"]+"/launch-promo", nil, http.StatusOK), &status)
	if status.Eligible || status.Reason == "" {
		t.Errorf("caio (fora da lista) = %+v", status)
	}

	// O catálogo do cliente mostra a oferta só para quem tem direito.
	var catalog struct {
		LaunchPromo *orders.PromoOffer `json:"launchPromo"`
	}
	_ = json.Unmarshal(env.must(env.login("ana@lista.test"), http.MethodGet, "/api/catalog", nil, http.StatusOK), &catalog)
	if catalog.LaunchPromo == nil || catalog.LaunchPromo.MonthlyCents != 3490 {
		t.Errorf("catálogo da ana = %+v", catalog.LaunchPromo)
	}
	catalog.LaunchPromo = nil
	_ = json.Unmarshal(env.must(env.login("caio@fora.test"), http.MethodGet, "/api/catalog", nil, http.StatusOK), &catalog)
	if catalog.LaunchPromo != nil {
		t.Errorf("catálogo do caio com promoção: %+v", catalog.LaunchPromo)
	}

	orderPlan := func(customer, plate string, plan map[string]any, promo bool, want int) []byte {
		return env.must(admin, http.MethodPost, "/api/customers/"+ids[customer]+"/trackers", map[string]any{
			"vehicle":        map[string]any{"name": "Moto " + plate, "plate": plate},
			"equipmentCents": 15000,
			"plan":           plan,
			"launchPromo":    promo,
		}, want)
	}
	monthlyPlan := map[string]any{"planName": "Plano Mensal", "priceCents": 6990, "dueDay": 10}
	insanosPlan := map[string]any{"planName": "Especial Insanos MC", "priceCents": 3990, "dueDay": 10}
	order := func(customer, plate string, promo bool, want int) []byte {
		return orderPlan(customer, plate, monthlyPlan, promo, want)
	}

	// Duda já paga o plano do Insanos (veículo sem a promoção): o catálogo
	// dela mostra a mensalidade do Insanos; a central recebe as duas.
	orderPlan("duda@lista.test", "INS0A00", insanosPlan, false, http.StatusCreated)
	catalog.LaunchPromo = nil
	_ = json.Unmarshal(env.must(env.login("duda@lista.test"), http.MethodGet, "/api/catalog", nil, http.StatusOK), &catalog)
	if catalog.LaunchPromo == nil || catalog.LaunchPromo.MonthlyCents != 2790 {
		t.Errorf("catálogo da duda (Insanos) = %+v", catalog.LaunchPromo)
	}
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/customers/"+ids["duda@lista.test"]+"/launch-promo", nil, http.StatusOK), &status)
	if !status.Eligible || status.Offer.MonthlyCents != 3490 || status.Offer.InsanosMonthlyCents != 2790 {
		t.Errorf("duda (Insanos) = %+v", status)
	}

	// Ana contrata com a promoção.
	var result struct {
		Subscription struct {
			ID              string  `json:"id"`
			PriceCents      int     `json:"priceCents"`
			PromoPriceCents *int    `json:"promoPriceCents"`
			PromoUntil      *string `json:"promoUntil"`
			NextDueDate     string  `json:"nextDueDate"`
		} `json:"subscription"`
		SetupInvoice *struct {
			AmountCents int    `json:"amountCents"`
			Description string `json:"description"`
		} `json:"setupInvoice"`
	}
	_ = json.Unmarshal(order("ana@lista.test", "PRO1A11", true, http.StatusCreated), &result)
	sub := result.Subscription
	if result.SetupInvoice == nil || result.SetupInvoice.AmountCents != 12000 {
		t.Fatalf("fatura do equipamento = %+v", result.SetupInvoice)
	}
	first, _ := time.Parse(time.DateOnly, sub.NextDueDate)
	if sub.PriceCents != 6990 || sub.PromoPriceCents == nil || *sub.PromoPriceCents != 3490 || sub.PromoUntil == nil ||
		*sub.PromoUntil != first.AddDate(1, 0, 0).Format(time.DateOnly) {
		t.Fatalf("assinatura = %+v", sub)
	}

	// 13 mensalidades: as 12 primeiras com a promoção, a 13ª pelo plano.
	repo := billing.NewRepository(env.db)
	horizon := billing.Date{Time: first.AddDate(1, 0, 0)}
	for i := 0; i < 2; i++ {
		if _, err := repo.GenerateDue(ctx, horizon); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := env.db.Query(ctx, `SELECT amount_cents, description FROM invoices
		WHERE subscription_id = $1 ORDER BY due_date`, sub.ID)
	if err != nil {
		t.Fatal(err)
	}
	var amounts []int
	for rows.Next() {
		var cents int
		var description string
		if err := rows.Scan(&cents, &description); err != nil {
			t.Fatal(err)
		}
		if (cents == 3490) != strings.Contains(description, "promoção") {
			t.Errorf("descrição %q com valor %d", description, cents)
		}
		amounts = append(amounts, cents)
	}
	rows.Close()
	if len(amounts) != 13 || amounts[0] != 3490 || amounts[11] != 3490 || amounts[12] != 6990 {
		t.Fatalf("mensalidades = %v, quer 12 × 3490 e depois 6990", amounts)
	}

	// Segundo veículo da Ana: sem promoção (1 por cliente).
	out := order("ana@lista.test", "PRO2B22", true, http.StatusConflict)
	if !strings.Contains(string(out), "PROMO_UNAVAILABLE") {
		t.Errorf("segundo veículo com promoção: %s", out)
	}
	_ = json.Unmarshal(order("ana@lista.test", "PRO2B22", false, http.StatusCreated), &result)
	if result.SetupInvoice.AmountCents != 15000 || result.Subscription.PromoPriceCents != nil {
		t.Errorf("segundo veículo = %+v", result)
	}

	// Bia contrata no plano do Insanos: a promoção dela é a do Insanos, e
	// depois dos 12 meses vale o preço especial deles.
	_ = json.Unmarshal(orderPlan("bia@lista.test", "INS1B11", insanosPlan, true, http.StatusCreated), &result)
	if s := result.Subscription; s.PriceCents != 3990 || s.PromoPriceCents == nil || *s.PromoPriceCents != 2790 ||
		result.SetupInvoice == nil || result.SetupInvoice.AmountCents != 12000 {
		t.Fatalf("Insanos com promoção = %+v / %+v", s, result.SetupInvoice)
	}

	// Duda está na lista, mas as duas vagas já foram.
	out = orderPlan("duda@lista.test", "INS2C22", insanosPlan, true, http.StatusConflict)
	if !strings.Contains(string(out), "vagas") {
		t.Errorf("sem vaga: %s", out)
	}

	var usage orders.PromoUsage
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/leads/promo", nil, http.StatusOK), &usage)
	if usage.Used != 2 || usage.Slots != 2 || !usage.Enabled || usage.Offer.MonthlyCents != 3490 || usage.Offer.InsanosMonthlyCents != 2790 {
		t.Errorf("vagas = %+v", usage)
	}
	var list []leads.WaitlistEntry
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/leads/waitlist", nil, http.StatusOK), &list)
	claimed := map[string]bool{}
	for _, e := range list {
		if e.CustomerID == nil {
			t.Errorf("%s é cliente e veio sem customerId", e.Email)
		}
		claimed[e.Email] = e.PromoClaimed
	}
	if !claimed["ana@lista.test"] || !claimed["bia@lista.test"] || claimed["duda@lista.test"] {
		t.Errorf("quem usou a promoção = %v", claimed)
	}
}

// Pré-cadastro com "entrar também na lista de pré-lançamento": o e-mail vai
// para a lista (e para a promoção); desmarcado, não.
func TestLeadJoinsLaunchList(t *testing.T) {
	env, notifier := newLeadsEnv(t)
	lead := func(name, email, city string, join bool) {
		env.must("", http.MethodPost, "/api/public/leads", map[string]any{
			"name": name, "email": email, "phone": "(19) 98888-1111", "city": city, "plan": "Plano Mensal - R$ 69,90",
			"vehicleType": "moto", "vehicleCount": 1, "consent": true, "joinLaunch": join, "website": "",
		}, http.StatusCreated)
	}
	lead("Bruna", "bruna@exemplo.com.br", "Campinas - SP", true)
	lead("Carla", "carla@exemplo.com.br", "Santos", false)
	// Mandou de novo sem a cidade: a da lista fica.
	lead("Bruna", "bruna@exemplo.com.br", "", true)
	// Quem só estava na lista (evento) e depois fez o pré-cadastro: o mesmo
	// pré-cliente, e a equipe é avisada (uma vez).
	env.must("", http.MethodPost, "/api/public/launch",
		map[string]any{"name": "Davi", "email": "davi@exemplo.com.br", "phone": "(19) 97777-1111", "consent": true, "event": "feira-sp", "city": "Sorocaba"},
		http.StatusCreated)
	lead("Davi", "davi@exemplo.com.br", "", false)
	lead("Davi", "davi@exemplo.com.br", "", false)

	admin := env.login(auth.RoleAdmin + "@leads.test")
	var list []leads.WaitlistEntry
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/leads/waitlist", nil, http.StatusOK), &list)
	inList := map[string]leads.WaitlistEntry{}
	for _, e := range list {
		inList[e.Email] = e
	}
	if b := inList["bruna@exemplo.com.br"]; len(list) != 2 || b.Name != "Bruna" || b.Phone != "(19) 98888-1111" || b.City != "Campinas - SP" ||
		inList["davi@exemplo.com.br"].Event != "feira-sp" {
		t.Fatalf("lista de lançamento = %+v", list)
	}
	var pre []leads.Lead
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/leads", nil, http.StatusOK), &pre)
	onList := map[string]bool{}
	for _, l := range pre {
		onList[l.Email] = l.OnLaunchList
	}
	if len(pre) != 3 || !onList["bruna@exemplo.com.br"] || onList["carla@exemplo.com.br"] || !onList["davi@exemplo.com.br"] {
		t.Errorf("pré-clientes na lista = %v", onList)
	}
	for _, l := range pre {
		if l.Email == "davi@exemplo.com.br" && (l.Event != "feira-sp" || l.City != "Sorocaba" || l.Plan != "Plano Mensal - R$ 69,90" || l.Source != "landing") {
			t.Errorf("davi = %+v", l)
		}
	}
	// Avisos: Bruna, Carla e Davi (no pré-cadastro, não na lista nem no reenvio).
	deadline := time.Now().Add(2 * time.Second)
	for notifier.count() < 3 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	if notifier.count() != 3 {
		t.Errorf("avisos à equipe = %d, quer 3", notifier.count())
	}
}
