package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/appointments"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/devices"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/tracking"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/vehicles"
)

// Sem GPS, o rastreador repete a coordenada guardada — no recém-instalado, a
// do teste na fábrica, na China (o caso do J16 em 28/10/2026). O mapa, a
// posição do veículo e a rota ignoram essas posições até o GPS pegar sinal, e
// o teste do instalador só passa com o GPS. Precisa de FARBO_TEST_DATABASE_URL.
func TestPositionsWithoutGPSFix(t *testing.T) {
	env := newCredEnvWith(t, credEnvOptions{installs: true})
	ctx := context.Background()
	admin, err := env.auth.CreateUser(ctx, "admin@gps.test", "Central", auth.RoleAdmin, userPassword)
	if err != nil {
		t.Fatal(err)
	}
	dev, err := env.devices.Create(ctx, devices.Input{IMEIConfirmed: true, IMEI: "865893088448008", Protocol: "gt06"})
	if err != nil {
		t.Fatal(err)
	}
	car, err := env.vehicles.Create(ctx, vehicles.Input{Name: "Corolla", Plate: "FBZ9A87", DeviceID: &dev.ID})
	if err != nil {
		t.Fatal(err)
	}
	tok := env.tokenFor("admin@gps.test")
	// O rastreador conectou agora (o servidor o viu), mas o GPS ainda não pegou sinal.
	if _, err := env.db.Exec(ctx, `UPDATE devices SET last_seen_at = NOW() WHERE id = $1`, dev.ID); err != nil {
		t.Fatal(err)
	}
	insert := func(lat, lon float64, valid bool, at time.Time) {
		t.Helper()
		if err := env.positions.Insert(ctx, &tracking.Position{DeviceID: dev.ID, GPSTimestamp: at, Latitude: lat, Longitude: lon,
			GPSValid: &valid, Protocol: "gt06", Source: tracking.SourceGPS}); err != nil {
			t.Fatal(err)
		}
		env.states.Update(ctx, dev.ID, func(st *tracking.State) { st.GPSValid = &valid })
	}
	type point struct {
		Latitude float64 `json:"latitude"`
	}
	current := func() (*point, string) {
		var out struct {
			Position *point `json:"position"`
			Message  string `json:"message"`
		}
		_ = json.Unmarshal(env.must(tok, http.MethodGet, "/api/vehicles/"+car.ID.String()+"/position", nil, http.StatusOK), &out)
		return out.Position, out.Message
	}
	onMap := func() *point {
		var list []struct {
			ID           uuid.UUID `json:"id"`
			LastPosition *point    `json:"lastPosition"`
		}
		_ = json.Unmarshal(env.must(tok, http.MethodGet, "/api/vehicles", nil, http.StatusOK), &list)
		for _, v := range list {
			if v.ID == car.ID {
				return v.LastPosition
			}
		}
		t.Fatalf("veículo fora da lista: %+v", list)
		return nil
	}
	period := "from=" + url.QueryEscape(time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)) +
		"&to=" + url.QueryEscape(time.Now().Add(time.Hour).UTC().Format(time.RFC3339))
	route := func(extra string) int {
		var out struct {
			Positions []point `json:"positions"`
		}
		_ = json.Unmarshal(env.must(tok, http.MethodGet, "/api/vehicles/"+car.ID.String()+"/positions?"+period+extra, nil,
			http.StatusOK), &out)
		return len(out.Positions)
	}
	link, err := env.installs.CreateStandalone(ctx, admin.ID, appointments.StandaloneInput{DeviceID: dev.ID, InstallerName: "Zé"})
	if err != nil {
		t.Fatal(err)
	}
	check := func() (ready, fix bool, message string) {
		var c struct {
			Ready   bool   `json:"ready"`
			GPSFix  bool   `json:"gpsFix"`
			Message string `json:"message"`
		}
		token := strings.TrimPrefix(link.URL, "https://painel.farbo.test/instalador/avulsa/")
		_ = json.Unmarshal(env.must("", http.MethodPost, "/api/public/installer-links/"+token+"/check", nil, http.StatusOK), &c)
		return c.Ready, c.GPSFix, c.Message
	}

	// Sem GPS: a posição da fábrica fica só no histórico bruto.
	insert(22.60194, 113.83198, false, time.Now().Add(-2*time.Minute))
	insert(22.60194, 113.83198, false, time.Now().Add(-time.Minute))
	if p, message := current(); p != nil || !strings.Contains(message, "GPS ainda não pegou sinal") {
		t.Fatalf("posição sem GPS = %+v %q", p, message)
	}
	if p := onMap(); p != nil {
		t.Fatalf("no mapa sem GPS = %+v", p)
	}
	if n := route(""); n != 0 {
		t.Fatalf("a rota passou pela posição sem GPS: %d pontos", n)
	}
	if n := route("&onlyValid=false"); n != 2 {
		t.Fatalf("o histórico completo = %d pontos", n)
	}
	if ready, fix, message := check(); ready || fix || !strings.Contains(message, "GPS ainda não pegou sinal") {
		t.Fatalf("teste do instalador sem GPS = %v %v %q", ready, fix, message)
	}

	// O GPS pegou sinal em São Paulo.
	insert(-23.5033, -46.86945, true, time.Now())
	if p, _ := current(); p == nil || p.Latitude != -23.5033 {
		t.Fatalf("posição com GPS = %+v", p)
	}
	if p := onMap(); p == nil || p.Latitude != -23.5033 {
		t.Fatalf("no mapa com GPS = %+v", p)
	}
	if n := route(""); n != 1 {
		t.Fatalf("a rota com GPS = %d pontos", n)
	}
	if ready, fix, _ := check(); !ready || !fix {
		t.Fatalf("teste do instalador com GPS = %v %v", ready, fix)
	}

	// Perdeu o sinal (garagem): o mapa fica na última posição com GPS.
	insert(-23.5033, -46.86945, false, time.Now().Add(time.Second))
	if p := onMap(); p == nil || p.Latitude != -23.5033 {
		t.Fatalf("no mapa depois de perder o sinal = %+v", p)
	}
}
