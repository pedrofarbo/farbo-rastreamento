package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/analytics"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/telemetry"
	"github.com/pedrofarbo/farbo-rastreamento/backend/migrations"
)

// Visitas da landing de ponta a ponta: a página manda as visitas e os
// eventos, robôs e lixo ficam de fora, cada visitante conta uma vez por dia
// (o código muda no dia seguinte e o sal de ontem some) e o admin vê o
// resumo. Precisa de FARBO_TEST_DATABASE_URL.
func TestLandingAnalyticsEndToEnd(t *testing.T) {
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
	for _, role := range []string{auth.RoleAdmin, auth.RoleOperator} {
		if _, err := authSvc.CreateUser(ctx, role+"@visitas.test", role, role, userPassword); err != nil {
			t.Fatal(err)
		}
	}
	day := time.Date(2026, 10, 4, 15, 0, 0, 0, time.UTC)
	svc := analytics.NewService(db, log)
	svc.SetClock(func() time.Time { return day })
	server := NewServer(Deps{
		Config: cfg, Log: log, Metrics: telemetry.NewMetrics(), DB: db, Auth: authSvc,
		Audit: audit.NewService(audit.NewRepository(db), log), Analytics: svc,
	})
	srv := httptest.NewServer(server.Handler())
	t.Cleanup(srv.Close)
	env := &credEnv{t: t, db: db, srv: srv}

	const (
		iphone  = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1"
		windows = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36 Edg/126.0.0.0"
		bot     = "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)"
	)
	send := func(ua string, hit map[string]any, want int) {
		t.Helper()
		body, _ := json.Marshal(hit)
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/public/analytics", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", ua)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != want {
			t.Fatalf("%v: %d, quer %d", hit, res.StatusCode, want)
		}
	}
	event := func(ua, name, label string) {
		t.Helper()
		send(ua, map[string]any{"name": name, "label": label, "path": "/"}, http.StatusNoContent)
	}

	// Celular, vindo do Instagram com campanha: viu as seções, clicou e se cadastrou.
	send(iphone, map[string]any{"name": "pageview", "path": "/", "referrer": "https://www.instagram.com/",
		"utmSource": "Instagram", "utmCampaign": "lancamento", "screenWidth": 390}, http.StatusNoContent)
	event(iphone, "section_view", "beneficios")
	event(iphone, "section_view", "planos")
	event(iphone, "cta_click", "hero-pre-lancamento")
	event(iphone, "lead_open", "Plano Mensal - R$ 69,90")
	event(iphone, "lead_submit", "Plano Mensal - R$ 69,90")
	// Voltou depois direto: no dia, continua vindo do Instagram.
	event(iphone, "pageview", "")
	// Computador, direto: voltou à página e entrou na lista.
	event(windows, "pageview", "")
	event(windows, "section_view", "beneficios")
	event(windows, "pageview", "")
	event(windows, "waitlist_submit", "")
	// Robô e evento desconhecido: aceitos (204), mas não contam. Corpo errado: 400.
	event(bot, "pageview", "")
	event(windows, "inventado", "x")
	send(windows, map[string]any{"name": "pageview", "ip": "1.2.3.4"}, http.StatusBadRequest)

	// Nada de IP ou navegador gravado: só o código do visitante.
	var stored int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM analytics_events WHERE visitor LIKE '%127.0.0.1%' OR length(visitor) <> 24`).Scan(&stored); err != nil || stored != 0 {
		t.Fatalf("visitante com dado pessoal: %d %v", stored, err)
	}

	admin := env.login(auth.RoleAdmin + "@visitas.test")
	env.must(env.login(auth.RoleOperator+"@visitas.test"), http.MethodGet, "/api/analytics/landing", nil, http.StatusForbidden)
	env.must("", http.MethodGet, "/api/analytics/landing", nil, http.StatusUnauthorized)

	summaryOf := func(origin string) analytics.Summary {
		t.Helper()
		var s analytics.Summary
		_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/analytics/landing?days=7&origem="+origin, nil, http.StatusOK), &s)
		return s
	}
	summary := func() analytics.Summary { return summaryOf("") }
	s := summary()
	if s.Visitors != 2 || s.Pageviews != 4 || s.Leads != 1 || s.LeadOpens != 1 || s.Waitlist != 1 || s.ActiveNow != 2 {
		t.Fatalf("totais = %+v", s)
	}
	if len(s.Days) != 7 || s.Days[6].Day != "2026-10-04" || s.Days[6].Visitors != 2 || s.Days[0].Visitors != 0 {
		t.Fatalf("dias = %+v", s.Days)
	}
	keys := func(list []analytics.Count) map[string]int {
		m := map[string]int{}
		for _, c := range list {
			m[c.Key] = c.Visitors
		}
		return m
	}
	// O funil de cada origem: o celular veio do Instagram e se cadastrou; o
	// computador veio direto e entrou na lista.
	byChannel := map[string]analytics.Origin{}
	for _, o := range s.Origins {
		byChannel[o.Channel] = o
	}
	if o := byChannel["instagram"]; o.Visitors != 1 || o.LeadOpens != 1 || o.Leads != 1 || o.Converted != 1 || o.Clicked != 1 {
		t.Errorf("origem instagram = %+v", o)
	}
	if o := byChannel["direto"]; o.Visitors != 1 || o.Waitlist != 1 || o.Leads != 0 || o.Converted != 1 || o.Clicked != 0 {
		t.Errorf("origem direto = %+v", o)
	}
	if len(s.Origins) != 2 || s.Channel != "" {
		t.Errorf("origens = %+v (filtro %q)", s.Origins, s.Channel)
	}
	// Filtrado por origem: o resumo inteiro é só de quem veio por ela, e a
	// lista das origens continua completa.
	insta := summaryOf("Instagram")
	if insta.Channel != "instagram" || insta.Visitors != 1 || insta.Pageviews != 2 || insta.Leads != 1 || insta.Waitlist != 0 ||
		len(insta.Origins) != 2 || keys(insta.Clicks)["hero-pre-lancamento"] != 1 || keys(insta.Sections)["planos"] != 1 ||
		keys(insta.Devices)["mobile"] != 1 || keys(insta.Devices)["desktop"] != 0 {
		t.Errorf("só Instagram = %+v", insta)
	}
	if direct := summaryOf("direto"); direct.Visitors != 1 || direct.Waitlist != 1 || direct.Leads != 0 || len(direct.Clicks) != 0 ||
		direct.Days[6].Visitors != 1 {
		t.Errorf("só direto = %+v", direct)
	}
	if none := summaryOf("google"); none.Visitors != 0 || none.Leads != 0 || len(none.Sections) != 0 {
		t.Errorf("origem sem visitas = %+v", none)
	}
	if c := keys(s.Campaigns); len(c) != 1 || c["instagram · lancamento"] != 1 {
		t.Errorf("campanhas = %+v", s.Campaigns)
	}
	if d := keys(s.Devices); d["mobile"] != 1 || d["desktop"] != 1 {
		t.Errorf("aparelhos = %+v", s.Devices)
	}
	if sec := keys(s.Sections); sec["beneficios"] != 2 || sec["planos"] != 1 {
		t.Errorf("seções = %+v", s.Sections)
	}
	if c := keys(s.Clicks); c["hero-pre-lancamento"] != 1 {
		t.Errorf("cliques = %+v", s.Clicks)
	}

	// No dia seguinte, o mesmo celular é outro código: conta de novo, e o sal
	// de ontem não existe mais.
	day = day.Add(24 * time.Hour)
	event(iphone, "pageview", "")
	if s := summary(); s.Visitors != 3 || s.Days[6].Day != "2026-10-05" || s.Days[6].Visitors != 1 {
		t.Fatalf("dia seguinte = %d visitantes, %+v", s.Visitors, s.Days)
	}
	var salts int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM analytics_salts WHERE day < '2026-10-05'`).Scan(&salts); err != nil || salts != 0 {
		t.Fatalf("sal de ontem ainda guardado: %d %v", salts, err)
	}
}

// A migração que classificou as visitas antigas segue as mesmas regras de
// analytics.Channel. Precisa de FARBO_TEST_DATABASE_URL.
func TestChannelMigrationMatchesCode(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	body, err := migrations.FS.ReadFile("0027_analytics_channel.sql")
	if err != nil {
		t.Fatal(err)
	}
	update := string(body[bytes.Index(body, []byte("UPDATE analytics_events")):])

	sources := []string{"", "instagram", "instagram_bio", "ig", "insta", "facebook", "fb", "meta", "whatsapp", "wa", "zap",
		"tiktok", "youtube", "yt", "google", "googleads", "bing", "panfleto", "qrcode"}
	browsers := []string{"", "Instagram", "Facebook", "TikTok", "Chrome"}
	hosts := []string{"", "instagram.com", "l.instagram.com", "facebook.com", "m.facebook.com", "fb.me", "wa.me",
		"web.whatsapp.com", "tiktok.com", "youtube.com", "youtu.be", "google.com", "google.com.br", "news.google.com",
		"bing.com", "duckduckgo.com", "search.yahoo.com", "exemplo.com.br", "notgoogle.com", "googleusercontent.com"}
	if _, err := db.Exec(ctx, `
		INSERT INTO analytics_events (day, visitor, name, referrer_host, utm_source, browser)
		SELECT '2026-10-01', 'teste', 'pageview', h, u, b
		FROM unnest($1::text[]) h, unnest($2::text[]) u, unnest($3::text[]) b`, hosts, sources, browsers); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, update); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query(ctx, `SELECT referrer_host, utm_source, browser, channel FROM analytics_events`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var host, source, browser, channel string
		if err := rows.Scan(&host, &source, &browser, &channel); err != nil {
			t.Fatal(err)
		}
		if want := analytics.Channel(host, source, browser); channel != want {
			t.Errorf("migração deu %q para (%q, %q, %q); o código dá %q", channel, host, source, browser, want)
		}
		n++
	}
	if n != len(hosts)*len(sources)*len(browsers) {
		t.Fatalf("conferidas %d visitas", n)
	}
}
