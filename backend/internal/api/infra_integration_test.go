package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/infra"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/telemetry"
)

// O painel de infraestrutura de ponta a ponta: os avisos e erros do log vão
// para o banco (a informação comum, não), o resumo traz a máquina, o banco e
// os backups, os logs filtram por nível e busca, e só o admin vê. Precisa de
// FARBO_TEST_DATABASE_URL.
func TestInfraEndToEnd(t *testing.T) {
	db := integrationDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))

	capture := infra.NewCapture(slog.NewTextHandler(io.Discard, nil))
	capture.Persist(ctx, db)
	appLog := slog.New(capture).With("component", "pagamentos")
	appLog.Info("tudo certo")
	appLog.Error("falha ao gerar o Pix", "fatura", "f-1")
	appLog.Warn("o banco demorou a responder", "ms", 1200)
	// A gravação é em segundo plano: espera os dois chegarem.
	for deadline := time.Now().Add(3 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		var n int
		_ = db.QueryRow(ctx, `SELECT count(*) FROM system_logs`).Scan(&n)
		if n == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no banco: %d registros, quer 2", n)
		}
	}

	// Duas cópias do banco e arquivos que não contam.
	backups := t.TempDir()
	for name, age := range map[string]time.Duration{
		"tracker-20261002T030000Z.dump":         48 * time.Hour,
		"tracker-20261003T030000Z.dump":         24 * time.Hour,
		"tracker-20261004T030000Z.dump.partial": 0,
		"leia-me.txt":                           0,
	} {
		path := filepath.Join(backups, name)
		if err := os.WriteFile(path, []byte("cópia do banco"), 0o600); err != nil {
			t.Fatal(err)
		}
		at := time.Now().Add(-age)
		if err := os.Chtimes(path, at, at); err != nil {
			t.Fatal(err)
		}
	}
	monitor := infra.NewMonitor("/proc", "/")
	monitor.Sample(time.Now())
	svc := infra.NewService(db, monitor, capture, backups)

	cfg := &config.Config{
		HTTP: config.HTTP{RateLimitRPS: 1000, RateLimitBurst: 1000},
		Auth: config.Auth{
			JWTSecret:      []byte("segredo-de-teste-integracao-0123456789abcdef"),
			AccessTokenTTL: time.Hour, RefreshTokenTTL: time.Hour, BcryptCost: bcrypt.MinCost,
		},
	}
	authSvc := auth.NewService(auth.NewRepository(db), cfg.Auth, nil, quiet)
	for _, role := range []string{auth.RoleAdmin, auth.RoleOperator} {
		if _, err := authSvc.CreateUser(ctx, role+"@infra.test", role, role, userPassword); err != nil {
			t.Fatal(err)
		}
	}
	server := NewServer(Deps{
		Config: cfg, Log: quiet, Metrics: telemetry.NewMetrics(), DB: db, Auth: authSvc,
		Audit: audit.NewService(audit.NewRepository(db), quiet), Infra: svc,
	})
	srv := httptest.NewServer(server.Handler())
	t.Cleanup(srv.Close)
	env := &credEnv{t: t, db: db, srv: srv}

	env.must(env.login(auth.RoleOperator+"@infra.test"), http.MethodGet, "/api/infra/status", nil, http.StatusForbidden)
	env.must("", http.MethodGet, "/api/infra/logs", nil, http.StatusUnauthorized)
	admin := env.login(auth.RoleAdmin + "@infra.test")

	var status infra.Status
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/infra/status", nil, http.StatusOK), &status)
	if status.Host.MemTotal == 0 || status.Host.DiskTotal == 0 || status.Host.Cores == 0 || len(status.History) != 1 {
		t.Errorf("máquina = %+v, histórico %d", status.Host, len(status.History))
	}
	if !status.Database.OK || status.Database.SizeBytes == 0 || len(status.Database.Tables) == 0 || status.Database.MaxConnections == 0 {
		t.Errorf("banco = %+v", status.Database)
	}
	if status.Redis.Enabled {
		t.Errorf("redis sem configurar = %+v", status.Redis)
	}
	if !status.Backups.Available || status.Backups.Count != 2 || status.Backups.Latest == nil ||
		status.Backups.Latest.Name != "tracker-20261003T030000Z.dump" {
		t.Errorf("backups = %+v %+v", status.Backups, status.Backups.Latest)
	}
	if status.Logs.Errors24h != 1 || status.Logs.Warnings24h != 1 || status.Process.Goroutines == 0 {
		t.Errorf("logs = %+v, processo = %+v", status.Logs, status.Process)
	}

	logs := func(query string) infra.LogPage {
		t.Helper()
		var page infra.LogPage
		_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/infra/logs"+query, nil, http.StatusOK), &page)
		return page
	}
	if all := logs("?hours=24"); len(all.Entries) != 2 || len(all.Groups) != 2 {
		t.Fatalf("todos = %+v", all)
	}
	errs := logs("?level=ERROR")
	if len(errs.Entries) != 1 || errs.Entries[0].Component != "pagamentos" || errs.Entries[0].Attrs["fatura"] != "f-1" {
		t.Fatalf("erros = %+v", errs.Entries)
	}
	if found := logs("?q=pix"); len(found.Entries) != 1 || found.Entries[0].Message != "falha ao gerar o Pix" {
		t.Fatalf("busca = %+v", found.Entries)
	}

	// Os de mais de 30 dias saem na limpeza.
	if _, err := db.Exec(ctx, `INSERT INTO system_logs (logged_at, level, message) VALUES (NOW() - interval '40 days', 'ERROR', 'antigo')`); err != nil {
		t.Fatal(err)
	}
	if removed, err := svc.Cleanup(ctx); err != nil || removed != 1 {
		t.Fatalf("limpeza = %d %v", removed, err)
	}
}
