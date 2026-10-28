package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/devices"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/tracking"
)

// A instalação avulsa de ponta a ponta: a central escolhe o rastreador e gera
// o link (um aberto por rastreador); o instalador abre sem login, testa o
// sinal (a central vê o resultado) e conclui só com o sinal — e o link
// expira. O link encerrado pela central também para de abrir. Precisa de
// FARBO_TEST_DATABASE_URL.
func TestStandaloneInstall(t *testing.T) {
	env := newCredEnvWith(t, credEnvOptions{installs: true})
	pusher := &recordingPusher{}
	env.installs.SetPusher(pusher)
	ctx := context.Background()
	op, err := env.auth.CreateUser(ctx, "op@avulsa.test", "Operadora", auth.RoleOperator, userPassword)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.auth.CreateUser(ctx, "cli@avulsa.test", "Cliente", auth.RoleCustomer, userPassword); err != nil {
		t.Fatal(err)
	}
	dev, err := env.devices.Create(ctx, devices.Input{IMEIConfirmed: true, IMEI: "869247061230333", Protocol: "gt06", Model: "TK910"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := env.devices.Create(ctx, devices.Input{IMEIConfirmed: true, IMEI: "869247061230444", Protocol: "gt06"})
	if err != nil {
		t.Fatal(err)
	}
	opTok := env.tokenFor("op@avulsa.test")
	env.must(env.tokenFor("cli@avulsa.test"), http.MethodGet, "/api/installer-links", nil, http.StatusForbidden)

	type link struct {
		ID         uuid.UUID  `json:"id"`
		IMEI       string     `json:"imei"`
		Status     string     `json:"status"`
		URL        string     `json:"url"`
		CheckedAt  *time.Time `json:"checkedAt"`
		CheckReady bool       `json:"checkReady"`
	}
	var l link
	env.must(opTok, http.MethodPost, "/api/installer-links", map[string]any{"deviceId": dev.ID}, http.StatusBadRequest)
	_ = json.Unmarshal(env.must(opTok, http.MethodPost, "/api/installer-links", map[string]any{
		"deviceId": dev.ID, "installerName": "Zé Instalador", "notes": "Moto do João",
	}, http.StatusCreated), &l)
	if l.IMEI != dev.IMEI || l.Status != "open" || !strings.HasPrefix(l.URL, "https://painel.farbo.test/instalador/avulsa/") {
		t.Fatalf("link = %+v", l)
	}
	// Um aberto por rastreador.
	env.must(opTok, http.MethodPost, "/api/installer-links", map[string]any{"deviceId": dev.ID, "installerName": "Outro"},
		http.StatusBadRequest)

	// O instalador, pelo link.
	token := strings.TrimPrefix(l.URL, "https://painel.farbo.test/instalador/avulsa/")
	base := "/api/public/installer-links/" + token
	var view struct {
		IMEI        string `json:"imei"`
		DeviceModel string `json:"deviceModel"`
		Notes       string `json:"notes"`
		Status      string `json:"status"`
	}
	_ = json.Unmarshal(env.must("", http.MethodGet, base, nil, http.StatusOK), &view)
	if view.IMEI != dev.IMEI || view.DeviceModel != "TK910" || view.Notes != "Moto do João" {
		t.Fatalf("link do instalador = %+v", view)
	}
	env.must("", http.MethodGet, "/api/public/installer-links/"+token[:len(token)-2]+"xx", nil, http.StatusGone)
	// O link do agendamento não abre a avulsa (e vice-versa): chaves diferentes.
	env.must("", http.MethodGet, "/api/public/install/"+token, nil, http.StatusGone)
	env.must("", http.MethodGet, "/api/public/installer-links/"+env.installs.Token(l.ID), nil, http.StatusGone)

	var c struct{ Ready bool }
	_ = json.Unmarshal(env.must("", http.MethodPost, base+"/check", nil, http.StatusOK), &c)
	if c.Ready {
		t.Fatalf("sem sinal = %+v", c)
	}
	var list []link
	_ = json.Unmarshal(env.must(opTok, http.MethodGet, "/api/installer-links", nil, http.StatusOK), &list)
	if len(list) != 1 || list[0].CheckedAt == nil || list[0].CheckReady {
		t.Fatalf("a central vê o teste sem sinal = %+v", list)
	}
	env.must("", http.MethodPost, base+"/complete", map[string]string{"note": "x"}, http.StatusConflict)
	if err := env.positions.Insert(ctx, &tracking.Position{DeviceID: dev.ID, GPSTimestamp: time.Now(), ReceivedAt: time.Now(),
		Latitude: -23.5, Longitude: -46.6}); err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(env.must("", http.MethodPost, base+"/check", nil, http.StatusOK), &c)
	if !c.Ready {
		t.Fatalf("com sinal = %+v", c)
	}
	_ = json.Unmarshal(env.must("", http.MethodPost, base+"/complete", map[string]string{"note": "Debaixo do banco"}, http.StatusOK), &view)
	if view.Status != "done" {
		t.Fatalf("concluída = %+v", view)
	}
	if got := pusher.take(); len(got[op.ID]) != 1 || got[op.ID][0].Title != "Instalação avulsa concluída" {
		t.Fatalf("aviso a quem gerou o link = %+v", got)
	}
	// Concluída: o link expira e some da central.
	env.must("", http.MethodGet, base, nil, http.StatusGone)
	env.must("", http.MethodPost, base+"/check", nil, http.StatusGone)
	env.must("", http.MethodPost, base+"/complete", nil, http.StatusGone)
	var done struct {
		Status        string `json:"status"`
		URL           string `json:"url"`
		InstallerNote string `json:"installerNote"`
	}
	var all []json.RawMessage
	_ = json.Unmarshal(env.must(opTok, http.MethodGet, "/api/installer-links", nil, http.StatusOK), &all)
	if len(all) != 1 {
		t.Fatalf("lista = %s", all)
	}
	_ = json.Unmarshal(all[0], &done)
	if done.Status != "done" || done.URL != "" || done.InstallerNote != "Debaixo do banco" {
		t.Fatalf("concluída na central = %+v", done)
	}
	env.must(opTok, http.MethodPost, "/api/installer-links/"+l.ID.String()+"/close", nil, http.StatusBadRequest)

	// Encerrado pela central: o link para na hora; dá para gerar outro.
	var second link
	_ = json.Unmarshal(env.must(opTok, http.MethodPost, "/api/installer-links", map[string]any{
		"deviceId": other.ID, "installerName": "Zé Instalador",
	}, http.StatusCreated), &second)
	secondBase := "/api/public/installer-links/" + strings.TrimPrefix(second.URL, "https://painel.farbo.test/instalador/avulsa/")
	env.must("", http.MethodGet, secondBase, nil, http.StatusOK)
	var closed link
	_ = json.Unmarshal(env.must(opTok, http.MethodPost, "/api/installer-links/"+second.ID.String()+"/close", nil, http.StatusOK), &closed)
	if closed.Status != "closed" || closed.URL != "" {
		t.Fatalf("encerrado = %+v", closed)
	}
	if out := env.must("", http.MethodGet, secondBase, nil, http.StatusGone); !strings.Contains(string(out), "encerrou") {
		t.Fatalf("link encerrado = %s", out)
	}
	env.must(opTok, http.MethodPost, "/api/installer-links", map[string]any{"deviceId": other.ID, "installerName": "Zé"},
		http.StatusCreated)
}
