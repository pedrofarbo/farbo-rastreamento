package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/commands"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/devices"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/tracking"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/vehicles"
)

// A checagem do corte (sem envio) respeita quem pode cortar e de qual veículo,
// e diz o motivo com um código. Precisa de FARBO_TEST_DATABASE_URL.
func TestEngineCutCheckEndToEnd(t *testing.T) {
	env := newCredEnvWith(t, credEnvOptions{snapshotsFor: func(positions *tracking.Repository) commands.TelemetryProvider {
		return tracking.NewSnapshotProvider(positions)
	}})
	ctx := context.Background()
	authSvc := auth.NewService(auth.NewRepository(env.db), config.Auth{BcryptCost: bcrypt.MinCost}, nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	user := func(email, role string) *auth.User {
		u, err := authSvc.CreateUser(ctx, email, email, role, userPassword)
		if err != nil {
			t.Fatal(err)
		}
		return u
	}
	ana := user("ana@corte.test", auth.RoleCustomer)
	user("bia@corte.test", auth.RoleCustomer)
	user("viewer@corte.test", auth.RoleViewer)

	dev, err := env.devices.Create(ctx, devices.Input{IMEI: "869247061238001", Protocol: "gt06"})
	if err != nil {
		t.Fatal(err)
	}
	carro, err := env.vehicles.Create(ctx, vehicles.Input{Name: "Carro da Ana", DeviceID: &dev.ID, OwnerID: &ana.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := env.owners.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	path := "/api/vehicles/" + carro.ID.String() + "/commands/engine-cut/check"
	anaToken := env.login("ana@corte.test")
	check := func() commands.EngineCutCheck {
		var out commands.EngineCutCheck
		if err := json.Unmarshal(env.must(anaToken, http.MethodGet, path, nil, http.StatusOK), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}

	if got := check(); got.Allowed || got.Code != commands.CutNoPosition {
		t.Fatalf("sem posição: %+v", got)
	}
	// Posição recebida há 42 min: antiga demais.
	if err := env.positions.Insert(ctx, &tracking.Position{DeviceID: dev.ID, GPSTimestamp: time.Now().UTC(),
		Latitude: -23.55, Longitude: -46.63, Protocol: "gt06", Source: "gps"}); err != nil {
		t.Fatal(err)
	}
	if _, err := env.db.Exec(ctx, `UPDATE positions SET received_at = NOW() - INTERVAL '42 minutes' WHERE device_id = $1`, dev.ID); err != nil {
		t.Fatal(err)
	}
	if got := check(); got.Code != commands.CutStalePosition || got.PositionAgeSeconds == nil || *got.PositionAgeSeconds < 2500 {
		t.Fatalf("posição antiga: %+v", got)
	}
	// Posição nova, parado: pode.
	if err := env.positions.Insert(ctx, &tracking.Position{DeviceID: dev.ID, GPSTimestamp: time.Now().UTC().Add(time.Second),
		Latitude: -23.55, Longitude: -46.63, Protocol: "gt06", Source: "gps"}); err != nil {
		t.Fatal(err)
	}
	if got := check(); !got.Allowed || got.Code != "" {
		t.Fatalf("posição nova e parado: %+v", got)
	}

	env.must(env.login("bia@corte.test"), http.MethodGet, path, nil, http.StatusNotFound)
	env.must(env.login("viewer@corte.test"), http.MethodGet, path, nil, http.StatusForbidden)
	env.must("", http.MethodGet, path, nil, http.StatusUnauthorized)
}
