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
)

// TestClaimTelemetry: a análise do rastreador na ocorrência de roubo. O
// veículo andou na hora, a energia foi cortada depois, o modo roubo foi
// ligado 15 minutos depois do horário declarado; a análise traz a posição
// da hora, os km depois e as conclusões. Só a equipe da organização vê.
// Precisa de FARBO_TEST_DATABASE_URL.
func TestClaimTelemetry(t *testing.T) {
	notifier := &recordingOrgNotifier{}
	env := newCredEnvWith(t, credEnvOptions{orgs: notifier, occurrences: notifier})
	ctx := context.Background()
	if _, err := env.auth.CreateUser(ctx, "admin@laudo.test", "Central", auth.RoleAdmin, userPassword); err != nil {
		t.Fatal(err)
	}
	central := env.tokenFor("admin@laudo.test")
	_ = json.Unmarshal(env.must(central, http.MethodPost, "/api/organizations", map[string]any{
		"kind": "association", "name": "Associação Laudo Certo", "document": "11.222.333/0001-81",
		"email": "contato@laudo.test", "priceCents": 3500, "dueDay": 10,
		"admin": map[string]string{"name": "Ivo Analista", "email": "ivo@laudo.test"},
	}, http.StatusCreated), &struct{}{})
	env.must("", http.MethodPost, "/api/auth/reset-password",
		map[string]string{"token": notifier.token("ivo@laudo.test"), "password": userPassword}, http.StatusNoContent)
	ivo := env.tokenFor("ivo@laudo.test")
	var bia, car struct {
		ID uuid.UUID `json:"id"`
	}
	_ = json.Unmarshal(env.must(ivo, http.MethodPost, "/api/org/associates", map[string]any{"name": "Bia Associada", "email": "bia@laudo.test"},
		http.StatusCreated), &bia)
	_ = json.Unmarshal(env.must(ivo, http.MethodPost, "/api/org/associates/"+bia.ID.String()+"/vehicles",
		map[string]any{"name": "Onix da Bia", "plate": "LAU1D23"}, http.StatusCreated), &car)
	if _, err := env.auth.SetPassword(ctx, bia.ID, userPassword); err != nil {
		t.Fatal(err)
	}
	biaToken := env.tokenFor("bia@laudo.test")

	when := time.Now().Add(-3 * time.Hour).Truncate(time.Second)
	var device uuid.UUID
	if err := env.db.QueryRow(ctx, `INSERT INTO devices (imei) VALUES ('869247061250001') RETURNING id`).Scan(&device); err != nil {
		t.Fatal(err)
	}
	if _, err := env.db.Exec(ctx, `UPDATE vehicles SET device_id = $1 WHERE id = $2`, device, car.ID); err != nil {
		t.Fatal(err)
	}
	// Andando a 50 km/h para o norte, de 10 min antes a 40 min depois.
	lat := -18.95
	for d := -10 * time.Minute; d <= 40*time.Minute; d += 30 * time.Second {
		if _, err := env.db.Exec(ctx, `INSERT INTO positions (device_id, gps_timestamp, latitude, longitude, speed_kmh, acc, gps_valid)
			VALUES ($1, $2, $3, -48.27, 50, TRUE, TRUE)`, device, when.Add(d), lat); err != nil {
			t.Fatal(err)
		}
		lat += 50.0 / 3600 * 30 / 111
	}
	if _, err := env.db.Exec(ctx, `INSERT INTO vehicle_events (device_id, type, timestamp) VALUES ($1, 'POWER_LOSS', $2)`,
		device, when.Add(25*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := env.db.Exec(ctx, `INSERT INTO theft_modes (vehicle_id, activated_at, expires_at) VALUES ($1, $2, $3)`,
		car.ID, when.Add(15*time.Minute), when.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}

	var claim struct {
		ID uuid.UUID `json:"id"`
	}
	_ = json.Unmarshal(env.must(biaToken, http.MethodPost, "/api/me/claims", map[string]any{
		"vehicleId": car.ID, "kind": "robbery", "occurredAt": when, "place": "Av. Rondon Pacheco", "description": "Fui abordada no semáforo.",
	}, http.StatusCreated), &claim)

	env.must(biaToken, http.MethodGet, "/api/org/claims/"+claim.ID.String()+"/telemetry", nil, http.StatusForbidden)
	var out struct {
		Available        bool       `json:"available"`
		KmAfter          float64    `json:"kmAfter"`
		TheftActivatedAt *time.Time `json:"theftActivatedAt"`
		PowerCuts        []string   `json:"powerCuts"`
		At               *struct {
			SpeedKmh float64 `json:"speedKmh"`
		} `json:"at"`
		Findings []struct {
			Level string `json:"level"`
			Text  string `json:"text"`
		} `json:"findings"`
	}
	_ = json.Unmarshal(env.must(ivo, http.MethodGet, "/api/org/claims/"+claim.ID.String()+"/telemetry", nil, http.StatusOK), &out)
	if !out.Available || out.At == nil || out.At.SpeedKmh != 50 || out.KmAfter < 30 || out.TheftActivatedAt == nil || len(out.PowerCuts) != 1 {
		t.Fatalf("telemetria = %+v", out)
	}
	var texts []string
	for _, f := range out.Findings {
		texts = append(texts, f.Level+": "+f.Text)
	}
	all := strings.Join(texts, "\n")
	for _, want := range []string{"ok: O rastreador mandou posição", "ok: Depois do horário declarado, o veículo andou",
		"ok: O modo roubo foi ligado", "15 min depois", "info: A energia do veículo foi cortada"} {
		if !strings.Contains(all, want) {
			t.Errorf("falta %q em\n%s", want, all)
		}
	}
}
