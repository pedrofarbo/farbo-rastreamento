package api

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/devices"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/mail"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/tracking"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/vehicles"
)

// stallRecorder guarda os avisos da instalação parada.
type stallRecorder struct {
	mu   sync.Mutex
	sent []stallSent
}

type stallSent struct {
	To string
	N  mail.StalledInstall
}

func (r *stallRecorder) InstallStalled(_ context.Context, to string, n mail.StalledInstall) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, stallSent{to, n})
	return nil
}

func (r *stallRecorder) take() []stallSent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.sent
	r.sent = nil
	return out
}

// O andamento da instalação pelo link e a instalação parada: a agenda mostra
// o rastreador e o teste do sinal guardados; passado o horário sem a chegada
// (late) e depois da chegada sem a conclusão (stalled), a instalação aparece
// nas paradas (de qualquer dia) e o e-mail sai uma vez por situação — à
// central, com a agenda, e ao representante que agendou, com o link. Remarcar
// zera. Precisa de FARBO_TEST_DATABASE_URL.
func TestInstallationProgressAndStall(t *testing.T) {
	env := newCredEnvWith(t, credEnvOptions{installs: true})
	rec := &stallRecorder{}
	env.installs.SetStallNotifier(rec, []string{"central@farbo.test"})
	ctx := context.Background()
	if _, err := env.auth.CreateUser(ctx, "op@parada.test", "Operadora", auth.RoleOperator, userPassword); err != nil {
		t.Fatal(err)
	}
	customer, err := env.auth.CreateUser(ctx, "duda@parada.test", "Duda Lima", auth.RoleCustomer, userPassword)
	if err != nil {
		t.Fatal(err)
	}
	car, err := env.vehicles.Create(ctx, vehicles.Input{Name: "Fiesta da Duda", Plate: "PRD1A23", OwnerID: &customer.ID})
	if err != nil {
		t.Fatal(err)
	}
	dev, err := env.devices.Create(ctx, devices.Input{IMEIConfirmed: true, IMEI: "860752061230249", Protocol: "gt06"})
	if err != nil {
		t.Fatal(err)
	}
	op := env.tokenFor("op@parada.test")
	tok := env.tokenFor("duda@parada.test")

	// Amanhã às 7h (Brasília): às 11h, a instalação está parada (das 8h às
	// 20h, o aviso sai).
	brt := time.FixedZone("BRT", -3*60*60)
	tomorrow := time.Now().In(brt).AddDate(0, 0, 1)
	when := time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 7, 0, 0, 0, brt)
	var appt struct {
		ID  uuid.UUID `json:"id"`
		URL string    `json:"url"`
		Pin string    `json:"pin"`
	}
	_ = json.Unmarshal(env.must(op, http.MethodPost, "/api/installations", map[string]any{
		"vehicleId": car.ID, "installerName": "Rafa Instalador", "scheduledFor": when, "address": "Rua das Flores, 10 - Centro",
	}, http.StatusCreated), &appt)

	type row struct {
		ID               uuid.UUID  `json:"id"`
		Status           string     `json:"status"`
		Stall            string     `json:"stall"`
		TrackerIMEI      string     `json:"trackerImei"`
		TrackerConnected bool       `json:"trackerConnected"`
		CheckedAt        *time.Time `json:"checkedAt"`
		CheckReady       bool       `json:"checkReady"`
		URL              string     `json:"url"`
	}
	stalled := func() []row {
		var out []row
		_ = json.Unmarshal(env.must(op, http.MethodGet, "/api/installations/stalled", nil, http.StatusOK), &out)
		return out
	}
	if got := stalled(); len(got) != 0 {
		t.Fatalf("parada antes da hora = %+v", got)
	}
	env.must(tok, http.MethodGet, "/api/installations/stalled", nil, http.StatusForbidden)
	// Antes da hora, nenhum aviso.
	env.installs.NotifyStalled(ctx)
	if got := rec.take(); len(got) != 0 {
		t.Fatalf("aviso antes da hora = %+v", got)
	}

	// Às 11h, o link não registrou a chegada: parada (late).
	env.installs.SetClock(func() time.Time { return when.Add(4 * time.Hour) })
	got := stalled()
	if len(got) != 1 || got[0].ID != appt.ID || got[0].Stall != "late" || got[0].URL == "" {
		t.Fatalf("parada sem a chegada = %+v", got)
	}
	env.installs.NotifyStalled(ctx)
	sent := rec.take()
	if len(sent) != 1 || sent[0].To != "central@farbo.test" || !sent[0].N.Late || sent[0].N.ForInstaller ||
		!strings.HasSuffix(sent[0].N.ActionURL, "/agenda") || !slices.Contains(sent[0].N.Missing, "registrar a chegada") {
		t.Fatalf("aviso da parada sem a chegada = %+v", sent)
	}
	// Uma vez por situação.
	env.installs.NotifyStalled(ctx)
	if again := rec.take(); len(again) != 0 {
		t.Fatalf("aviso repetido = %+v", again)
	}
	// À noite, nada sai (a parada nova espera a manhã).
	if _, err := env.db.Exec(ctx, `UPDATE installation_appointments SET stall_notice = '' WHERE id = $1`, appt.ID); err != nil {
		t.Fatal(err)
	}
	env.installs.SetClock(func() time.Time { return when.Add(15 * time.Hour) })
	env.installs.NotifyStalled(ctx)
	if night := rec.take(); len(night) != 0 {
		t.Fatalf("aviso às 22h = %+v", night)
	}
	// Remarcar zera o aviso.
	env.installs.SetClock(func() time.Time { return when.Add(4 * time.Hour) })
	env.installs.NotifyStalled(ctx)
	rec.take()
	_ = json.Unmarshal(env.must(op, http.MethodPatch, "/api/installations/"+appt.ID.String(), map[string]any{
		"vehicleId": car.ID, "installerName": "Rafa Instalador", "scheduledFor": when.Add(2 * time.Hour), "address": "Rua das Flores, 10 - Centro",
	}, http.StatusOK), &appt)
	var notice string
	if err := env.db.QueryRow(ctx, `SELECT stall_notice FROM installation_appointments WHERE id = $1`, appt.ID).Scan(&notice); err != nil || notice != "" {
		t.Fatalf("remarcada, o aviso = %q %v", notice, err)
	}

	// No local (o representante que agendou recebe o dele, com o link): a
	// chegada, o rastreador e o teste do sinal ficam no andamento.
	if _, err := env.db.Exec(ctx, `
		WITH a AS (INSERT INTO affiliates (name, code, report_token, commission_cents, email)
			VALUES ('Rafa Representante', 'rafa-parada', 'tok-rafa-parada', 0, 'rafa@rep.test') RETURNING id)
		UPDATE installation_appointments SET affiliate_id = (SELECT id FROM a) WHERE id = $1`, appt.ID); err != nil {
		t.Fatal(err)
	}
	token := strings.TrimPrefix(appt.URL, "https://painel.farbo.test/instalador/")
	base := "/api/public/install/" + token
	env.withPin(base, appt.Pin)
	env.must("", http.MethodPost, base+"/arrive", nil, http.StatusOK)
	env.must("", http.MethodPost, base+"/check", map[string]string{"imei": dev.IMEI}, http.StatusOK)
	got = stalled()
	if len(got) != 1 || got[0].Stall != "stalled" || got[0].TrackerIMEI != dev.IMEI || got[0].CheckedAt == nil || got[0].CheckReady {
		t.Fatalf("no local, sem sinal = %+v", got)
	}
	if err := env.positions.Insert(ctx, &tracking.Position{DeviceID: dev.ID, GPSTimestamp: time.Now(), ReceivedAt: time.Now(),
		Latitude: -18.9, Longitude: -48.2}); err != nil {
		t.Fatal(err)
	}
	env.must("", http.MethodPost, base+"/check", map[string]string{"imei": dev.IMEI}, http.StatusOK)
	got = stalled()
	if len(got) != 1 || !got[0].CheckReady {
		t.Fatalf("com sinal = %+v", got)
	}
	env.installs.NotifyStalled(ctx)
	sent = rec.take()
	if len(sent) != 2 {
		t.Fatalf("avisos da parada no local = %+v", sent)
	}
	var rep, central *mail.StalledInstall
	for i := range sent {
		switch sent[i].To {
		case "rafa@rep.test":
			rep = &sent[i].N
		case "central@farbo.test":
			central = &sent[i].N
		}
	}
	if rep == nil || central == nil || rep.Late || rep.ArrivedAt == nil || !rep.ForInstaller ||
		!strings.Contains(rep.ActionURL, "/instalador/") || central.ForInstaller {
		t.Fatalf("avisos = rep %+v central %+v", rep, central)
	}
	if !slices.Contains(rep.Done, "sinal testado") || !slices.Contains(rep.Missing, "foto do rastreador") ||
		!slices.Contains(rep.Missing, "concluir a instalação") {
		t.Fatalf("andamento no aviso = feito %v falta %v", rep.Done, rep.Missing)
	}

	// Concluída, sai das paradas.
	if status, out := env.upload("", http.MethodPut, base+"/photos/tracker", jpegBytes, nil); status != http.StatusNoContent {
		t.Fatalf("foto: %d %s", status, out)
	}
	env.must("", http.MethodPost, base+"/complete", map[string]string{"imei": dev.IMEI}, http.StatusOK)
	if got := stalled(); len(got) != 0 {
		t.Fatalf("concluída ainda parada = %+v", got)
	}
}
