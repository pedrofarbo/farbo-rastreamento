package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/devices"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/tracking"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/vehicles"
)

// A localização ao vivo por um tempo: o dono gera o link (com a
// confirmação), quem recebe vê a posição sem login e sem nada do dono além
// do primeiro nome; o link morre no prazo, quando o dono encerra ou quando a
// conta é suspensa. Precisa de FARBO_TEST_DATABASE_URL.
func TestLiveLinks(t *testing.T) {
	env := newCredEnvWith(t, credEnvOptions{stepUp: true, liveLinks: true, suspendAfterDays: 10})
	ctx := context.Background()
	clock := time.Now()
	env.liveLinks.SetClock(func() time.Time { return clock })
	ana, err := env.auth.CreateUser(ctx, "ana@aovivo.test", "Ana Souza", "customer", userPassword)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.auth.CreateUser(ctx, "zeca@aovivo.test", "Zeca", "customer", userPassword); err != nil {
		t.Fatal(err)
	}
	dev, err := env.devices.Create(ctx, devices.Input{IMEIConfirmed: true, IMEI: "869247061239777", Protocol: "gt06"})
	if err != nil {
		t.Fatal(err)
	}
	car, err := env.vehicles.Create(ctx, vehicles.Input{Name: "Carro da Ana", Plate: "VIV1A23", DeviceID: &dev.ID, OwnerID: &ana.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := env.owners.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if err := env.positions.Insert(ctx, &tracking.Position{DeviceID: dev.ID, GPSTimestamp: clock.Add(-time.Minute),
		ReceivedAt: clock.Add(-time.Minute), Latitude: -23.55, Longitude: -46.63, SpeedKmh: 42}); err != nil {
		t.Fatal(err)
	}
	tokAna, tokZeca := env.login("ana@aovivo.test"), env.login("zeca@aovivo.test")
	path := "/api/vehicles/" + car.ID.String() + "/live-links"
	grant := func() map[string]string {
		g, err := env.stepUp.VerifyPassword(ctx, ana.ID, "vehicle_share", userPassword)
		if err != nil {
			t.Fatal(err)
		}
		return map[string]string{stepUpHeader: g.Token}
	}

	// Sem a confirmação, não gera; prazo fora da lista, também não.
	env.must(tokAna, http.MethodPost, path, map[string]any{"hours": 2}, http.StatusForbidden)
	if status, _ := env.doWith(tokAna, http.MethodPost, path, map[string]any{"hours": 3}, grant()); status != http.StatusUnprocessableEntity {
		t.Fatalf("3 horas: %d", status)
	}
	// Quem não é dono nem vê.
	env.must(tokZeca, http.MethodGet, path, nil, http.StatusNotFound)

	status, out := env.doWith(tokAna, http.MethodPost, path, map[string]any{"hours": 2, "label": "  Mãe  "}, grant())
	if status != http.StatusCreated {
		t.Fatalf("gerar: %d %s", status, out)
	}
	var link struct {
		ID        string    `json:"id"`
		URL       string    `json:"url"`
		Label     string    `json:"label"`
		ExpiresAt time.Time `json:"expiresAt"`
	}
	_ = json.Unmarshal(out, &link)
	if !strings.HasPrefix(link.URL, "https://painel.farbo.test/ao-vivo/") || link.Label != "Mãe" ||
		link.ExpiresAt.Sub(clock).Round(time.Minute) != 2*time.Hour {
		t.Fatalf("link = %+v", link)
	}
	token := strings.TrimPrefix(link.URL, "https://painel.farbo.test/ao-vivo/")
	public := "/api/public/live/" + token

	// Sem login: o veículo, a posição e o primeiro nome de quem mandou.
	body := string(env.must("", http.MethodGet, public, nil, http.StatusOK))
	if !strings.Contains(body, `"sharedBy":"Ana"`) || !strings.Contains(body, `"latitude":-23.55`) ||
		strings.Contains(body, "ana@aovivo.test") || strings.Contains(body, "Souza") {
		t.Fatalf("link público = %s", body)
	}
	// Token mexido: não vale.
	env.must("", http.MethodGet, "/api/public/live/"+token[:len(token)-2]+"xx", nil, http.StatusGone)

	var list struct {
		Links []struct {
			ID    string `json:"id"`
			Views int    `json:"views"`
		} `json:"links"`
		Durations []int `json:"durations"`
	}
	_ = json.Unmarshal(env.must(tokAna, http.MethodGet, path, nil, http.StatusOK), &list)
	if len(list.Links) != 1 || list.Links[0].Views != 1 || len(list.Durations) == 0 {
		t.Fatalf("lista = %+v", list)
	}

	// Conta suspensa: o link para junto.
	if _, err := env.db.Exec(ctx, `
		INSERT INTO invoices (customer_id, description, amount_cents, due_date, status)
		VALUES ($1, 'Plano', 6990, CURRENT_DATE - 40, 'OPEN')`, ana.ID); err != nil {
		t.Fatal(err)
	}
	env.must("", http.MethodGet, public, nil, http.StatusGone)
	if _, err := env.db.Exec(ctx, `DELETE FROM invoices WHERE customer_id = $1`, ana.ID); err != nil {
		t.Fatal(err)
	}
	env.must("", http.MethodGet, public, nil, http.StatusOK)

	// O prazo acaba: o link morre e sai da lista.
	clock = clock.Add(2*time.Hour + time.Second)
	env.must("", http.MethodGet, public, nil, http.StatusGone)
	_ = json.Unmarshal(env.must(tokAna, http.MethodGet, path, nil, http.StatusOK), &list)
	if len(list.Links) != 0 {
		t.Fatalf("vencido na lista = %+v", list)
	}

	// Encerrar antes do prazo.
	_, out = env.doWith(tokAna, http.MethodPost, path, map[string]any{"hours": 1}, grant())
	_ = json.Unmarshal(out, &link)
	token = strings.TrimPrefix(link.URL, "https://painel.farbo.test/ao-vivo/")
	env.must("", http.MethodGet, "/api/public/live/"+token, nil, http.StatusOK)
	env.must(tokZeca, http.MethodDelete, path+"/"+link.ID, nil, http.StatusNotFound)
	env.must(tokAna, http.MethodDelete, path+"/"+link.ID, nil, http.StatusNoContent)
	env.must("", http.MethodGet, "/api/public/live/"+token, nil, http.StatusGone)
	env.must(tokAna, http.MethodDelete, path+"/"+link.ID, nil, http.StatusNotFound)
}
