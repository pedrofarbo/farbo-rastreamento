package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
)

// TestOrgTrackerHealth: a saúde dos rastreadores no painel da associação. O
// rastreador em dia não aparece; o sem sinal há mais de um dia, o há mais de
// três, o corte de energia seguido de silêncio (o rastreador desligado ou
// retirado), o corte que voltou e o que nunca mandou sinal aparecem, do mais
// grave. A equipe avisa o associado no app; a rotina avisa sozinha (das 9h às
// 20h, no máximo a cada 3 dias), e a associação desliga. Precisa de
// FARBO_TEST_DATABASE_URL.
func TestOrgTrackerHealth(t *testing.T) {
	notifier := &recordingOrgNotifier{}
	env := newCredEnvWith(t, credEnvOptions{orgs: notifier})
	pusher := &recordingPusher{}
	env.orgs.SetPusher(pusher)
	ctx := context.Background()
	// O relógio: hoje ao meio-dia no fuso da cobrança (a rotina só avisa de dia).
	local := time.Now().In(env.billing.Location())
	now := time.Date(local.Year(), local.Month(), local.Day(), 12, 0, 0, 0, env.billing.Location())
	env.billing.SetClock(func() time.Time { return now })

	if _, err := env.auth.CreateUser(ctx, "admin@saude.test", "Central", auth.RoleAdmin, userPassword); err != nil {
		t.Fatal(err)
	}
	central := env.tokenFor("admin@saude.test")
	var created struct {
		Organization struct {
			ID uuid.UUID `json:"id"`
		} `json:"organization"`
	}
	_ = json.Unmarshal(env.must(central, http.MethodPost, "/api/organizations", map[string]any{
		"kind": "association", "name": "Associação Estrada Boa", "document": "11.222.333/0001-81",
		"email": "contato@estrada.test", "priceCents": 3500, "dueDay": 10,
		"admin": map[string]string{"name": "Lia Gestora", "email": "lia@estrada.test"},
	}, http.StatusCreated), &created)
	env.must("", http.MethodPost, "/api/auth/reset-password",
		map[string]string{"token": notifier.token("lia@estrada.test"), "password": userPassword}, http.StatusNoContent)
	lia := env.tokenFor("lia@estrada.test")

	var bia struct {
		ID uuid.UUID `json:"id"`
	}
	_ = json.Unmarshal(env.must(lia, http.MethodPost, "/api/org/associates", map[string]any{"name": "Bia Associada", "email": "bia@estrada.test",
		"phone": "34988881234"}, http.StatusCreated), &bia)
	vehicle := func(i int, seen *time.Time, cut *time.Time) uuid.UUID {
		t.Helper()
		var v struct {
			ID uuid.UUID `json:"id"`
		}
		_ = json.Unmarshal(env.must(lia, http.MethodPost, "/api/org/associates/"+bia.ID.String()+"/vehicles",
			map[string]any{"name": fmt.Sprintf("Carro %d", i), "plate": fmt.Sprintf("EST%dA2%d", i, i)}, http.StatusCreated), &v)
		status := "OFFLINE"
		if seen != nil && now.Sub(*seen) < time.Hour {
			status = "ONLINE"
		}
		var device uuid.UUID
		if err := env.db.QueryRow(ctx, `INSERT INTO devices (imei, status, last_seen_at) VALUES ($1, $2, $3) RETURNING id`,
			fmt.Sprintf("86924706124000%d", i), status, seen).Scan(&device); err != nil {
			t.Fatal(err)
		}
		if _, err := env.db.Exec(ctx, `UPDATE vehicles SET device_id = $1 WHERE id = $2`, device, v.ID); err != nil {
			t.Fatal(err)
		}
		if cut != nil {
			if _, err := env.db.Exec(ctx, `INSERT INTO vehicle_events (device_id, type, timestamp, latitude, longitude, speed_kmh)
				VALUES ($1, 'POWER_LOSS', $2, -18.9, -48.2, 0)`, device, *cut); err != nil {
				t.Fatal(err)
			}
		}
		return v.ID
	}
	at := func(d time.Duration) *time.Time { v := now.Add(-d); return &v }
	ok := vehicle(1, at(time.Minute), nil)
	silent := vehicle(2, at(30*time.Hour), nil)
	long := vehicle(3, at(100*time.Hour), nil)
	removed := vehicle(4, at(4*time.Hour), at(5*time.Hour))
	cut := vehicle(5, at(time.Minute), at(26*time.Hour))
	never := vehicle(6, nil, nil)

	var health struct {
		Installed  int  `json:"installed"`
		Online     int  `json:"online"`
		Silent     int  `json:"silent"`
		SilentLong int  `json:"silentLong"`
		Never      int  `json:"never"`
		Removed    int  `json:"removed"`
		PowerCuts  int  `json:"powerCuts"`
		AutoNotice bool `json:"autoNotice"`
		Vehicles   []struct {
			VehicleID  uuid.UUID  `json:"vehicleId"`
			Issue      string     `json:"issue"`
			Phone      string     `json:"phone"`
			NotifiedAt *time.Time `json:"notifiedAt"`
		} `json:"vehicles"`
	}
	read := func() {
		t.Helper()
		_ = json.Unmarshal(env.must(lia, http.MethodGet, "/api/org/health", nil, http.StatusOK), &health)
	}
	read()
	if health.Installed != 6 || health.Online != 2 || health.Silent != 1 || health.SilentLong != 1 || health.Never != 1 ||
		health.Removed != 1 || health.PowerCuts != 2 || !health.AutoNotice {
		t.Fatalf("saúde = %+v", health)
	}
	order := []uuid.UUID{removed, long, silent, never, cut}
	issues := []string{"removed", "silent_long", "silent", "never", "power_cut"}
	if len(health.Vehicles) != 5 {
		t.Fatalf("veículos com problema = %+v", health.Vehicles)
	}
	for i, v := range health.Vehicles {
		if v.VehicleID != order[i] || v.Issue != issues[i] {
			t.Errorf("posição %d = %+v, quero %s %s", i, v, order[i], issues[i])
		}
	}

	// A equipe avisa: o sem sinal sim; o em dia e o corte que voltou, não.
	env.must(lia, http.MethodPost, "/api/org/health/vehicles/"+ok.String()+"/notify", nil, http.StatusBadRequest)
	env.must(lia, http.MethodPost, "/api/org/health/vehicles/"+cut.String()+"/notify", nil, http.StatusBadRequest)
	env.must(lia, http.MethodPost, "/api/org/health/vehicles/"+uuid.NewString()+"/notify", nil, http.StatusNotFound)
	var sent struct {
		Delivered int `json:"delivered"`
	}
	_ = json.Unmarshal(env.must(lia, http.MethodPost, "/api/org/health/vehicles/"+silent.String()+"/notify", nil, http.StatusOK), &sent)
	got := pusher.take()[bia.ID]
	if sent.Delivered != 1 || len(got) != 1 || got[0].Title != "Rastreador sem sinal: Carro 2" {
		t.Fatalf("aviso da equipe = %+v %+v", sent, got)
	}

	// A rotina: os outros três (o avisado agora espera 3 dias); de novo, nada.
	env.orgs.NotifyTrackerHealth(ctx)
	got = pusher.take()[bia.ID]
	if len(got) != 3 {
		t.Fatalf("avisos da rotina = %+v", got)
	}
	for _, n := range got {
		if n.Tag == "tracker-health:"+removed.String() && n.Body == "" {
			t.Errorf("aviso do corte sem texto: %+v", n)
		}
	}
	env.orgs.NotifyTrackerHealth(ctx)
	if got := pusher.take(); len(got) != 0 {
		t.Fatalf("a rotina avisou de novo: %+v", got)
	}
	read()
	for _, v := range health.Vehicles {
		if v.Issue != "power_cut" && v.NotifiedAt == nil {
			t.Errorf("sem o último aviso: %+v", v)
		}
	}

	// Desligado pela associação: a rotina não avisa, mesmo depois de 3 dias.
	env.must(lia, http.MethodPut, "/api/org/health/settings", map[string]any{"autoNotice": false}, http.StatusNoContent)
	now = now.Add(4 * 24 * time.Hour)
	env.orgs.NotifyTrackerHealth(ctx)
	if got := pusher.take(); len(got) != 0 {
		t.Fatalf("avisou com o aviso desligado: %+v", got)
	}
	// À noite, também não.
	env.must(lia, http.MethodPut, "/api/org/health/settings", map[string]any{"autoNotice": true}, http.StatusNoContent)
	now = time.Date(now.Year(), now.Month(), now.Day(), 22, 0, 0, 0, env.billing.Location())
	env.orgs.NotifyTrackerHealth(ctx)
	if got := pusher.take(); len(got) != 0 {
		t.Fatalf("avisou à noite: %+v", got)
	}
}
