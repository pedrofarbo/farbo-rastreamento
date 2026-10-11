package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/alerts"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/reports"
)

// Os relatórios da frota, das posições ao CSV: o cálculo em segundo plano
// resume cada dia; o resumo, os km por dia, as viagens e os excessos saem
// para o cliente (só os da conta dele) e para a central (?cliente=). Precisa
// de FARBO_TEST_DATABASE_URL.
func TestFleetReports(t *testing.T) {
	env, _ := newLeadsEnv(t)
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	authSvc := auth.NewService(auth.NewRepository(env.db), config.Auth{BcryptCost: bcrypt.MinCost}, nil, log)
	frota, err := authSvc.CreateUser(ctx, "frota@relatorio.test", "Frota Ltda", auth.RoleCustomer, userPassword)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authSvc.CreateUser(ctx, "outro@relatorio.test", "Outro", auth.RoleCustomer, userPassword); err != nil {
		t.Fatal(err)
	}
	customer, other, admin := env.login("frota@relatorio.test"), env.login("outro@relatorio.test"), env.login(auth.RoleAdmin+"@leads.test")

	loc, _ := time.LoadLocation("America/Sao_Paulo")
	now := time.Now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	day1, day2 := today.AddDate(0, 0, -3), today.AddDate(0, 0, -2)

	var device, vehicle uuid.UUID
	if err := env.db.QueryRow(ctx, `INSERT INTO devices (imei) VALUES ('869247061777001') RETURNING id`).Scan(&device); err != nil {
		t.Fatal(err)
	}
	if err := env.db.QueryRow(ctx, `
		INSERT INTO vehicles (name, plate, device_id, owner_id, created_at) VALUES ('Van 01', 'REL1A11', $1, $2, $3) RETURNING id`,
		device, frota.ID, today.AddDate(0, 0, -10)).Scan(&vehicle); err != nil {
		t.Fatal(err)
	}
	lat := -23.55
	point := func(at time.Time, meters, speed float64, acc bool) {
		t.Helper()
		lat += meters / 111_320
		if _, err := env.db.Exec(ctx, `
			INSERT INTO positions (device_id, gps_timestamp, received_at, latitude, longitude, speed_kmh, acc, gps_valid)
			VALUES ($1, $2, $3, $4, -46.63, $5, $6, true)`, device, at, time.Now().Add(-time.Hour), lat, speed, acc); err != nil {
			t.Fatal(err)
		}
	}
	// Uma viagem de 6 km às 8h (com 2 min acima de 80 km/h) e a chegada,
	// ligada parada 5 min.
	trip := func(start time.Time, overspeed bool) {
		at := start
		for range 5 {
			point(at, 0, 0, false)
			at = at.Add(time.Minute)
		}
		for i := range 20 {
			speed := 36.0
			if overspeed && i >= 6 && i <= 9 {
				speed = 95
			}
			at = at.Add(30 * time.Second)
			point(at, 300, speed, true)
		}
		for range 6 {
			at = at.Add(time.Minute)
			point(at, 0, 0, true)
		}
	}
	trip(day1.Add(8*time.Hour), true)
	trip(day2.Add(8*time.Hour), false)
	event := func(typ string, at time.Time, speed float64) {
		t.Helper()
		if _, err := env.db.Exec(ctx, `
			INSERT INTO vehicle_events (device_id, type, timestamp, latitude, longitude, speed_kmh, metadata)
			VALUES ($1, $2, $3, -23.54, -46.63, $4, '{"limitKmh": 80}')`, device, typ, at, speed); err != nil {
			t.Fatal(err)
		}
	}
	event("OVERSPEED", day1.Add(8*time.Hour+8*time.Minute+30*time.Second), 90)
	event("OVERSPEED_END", day1.Add(8*time.Hour+10*time.Minute+30*time.Second), 40)

	if n, err := reports.NewAggregator(env.db, "America/Sao_Paulo", log).Round(ctx); err != nil || n != 2 {
		t.Fatalf("cálculo: %d dias, %v", n, err)
	}

	period := "de=" + day1.Format(time.DateOnly) + "&ate=" + day2.Format(time.DateOnly)
	var summary struct {
		Rows []reports.SummaryRow `json:"rows"`
	}
	_ = json.Unmarshal(env.must(customer, http.MethodGet, "/api/reports/summary?"+period, nil, http.StatusOK), &summary)
	if len(summary.Rows) != 1 {
		t.Fatalf("resumo = %+v", summary)
	}
	s := summary.Rows[0]
	if s.Name != "Van 01" || s.DistanceKm < 11.9 || s.DistanceKm > 12.1 || s.Trips != 2 || s.Overspeeds != 1 ||
		s.ActiveDays != 2 || s.MaxSpeedKmh != 95 || s.IdleSeconds != 2*5*60 {
		t.Errorf("resumo da Van = %+v", s)
	}

	var daily struct {
		Rows []reports.DailyRow `json:"rows"`
	}
	_ = json.Unmarshal(env.must(customer, http.MethodGet, "/api/reports/daily?"+period, nil, http.StatusOK), &daily)
	if len(daily.Rows) != 2 || daily.Rows[0].Day != day1.Format(time.DateOnly) || daily.Rows[0].Overspeeds != 1 || daily.Rows[1].Overspeeds != 0 {
		t.Errorf("km por dia = %+v", daily.Rows)
	}

	var trips struct {
		Trips []reports.Trip `json:"trips"`
	}
	_ = json.Unmarshal(env.must(customer, http.MethodGet, "/api/reports/trips?veiculo="+vehicle.String()+"&"+period, nil, http.StatusOK), &trips)
	if len(trips.Trips) != 2 || !trips.Trips[0].Start.Equal(day1.Add(8*time.Hour+4*time.Minute)) {
		t.Errorf("viagens = %+v", trips.Trips)
	}
	env.must(customer, http.MethodGet, "/api/reports/trips?"+period, nil, http.StatusBadRequest)

	var speeding struct {
		Rows []reports.Overspeed `json:"rows"`
	}
	_ = json.Unmarshal(env.must(customer, http.MethodGet, "/api/reports/overspeed?"+period, nil, http.StatusOK), &speeding)
	if len(speeding.Rows) != 1 || speeding.Rows[0].MaxSpeedKmh != 95 || speeding.Rows[0].End == nil ||
		speeding.Rows[0].LimitKmh == nil || *speeding.Rows[0].LimitKmh != 80 {
		t.Errorf("excessos = %+v", speeding.Rows)
	}

	// A planilha: BOM, ponto e vírgula, vírgula decimal.
	_, raw := env.do(customer, http.MethodGet, "/api/reports/summary?formato=csv&"+period, nil)
	if !bytes.HasPrefix(raw, []byte{0xEF, 0xBB, 0xBF}) || !strings.Contains(string(raw), "Veículo;Placa;Grupo;Km") ||
		!strings.Contains(string(raw), "Van 01;REL1A11;;12,0;") {
		t.Errorf("CSV do resumo:\n%s", raw)
	}

	// Cada conta vê só os seus; a central escolhe o cliente; o período tem teto.
	_ = json.Unmarshal(env.must(other, http.MethodGet, "/api/reports/summary?"+period, nil, http.StatusOK), &summary)
	if len(summary.Rows) != 0 {
		t.Errorf("outro cliente vê %+v", summary.Rows)
	}
	env.must(other, http.MethodGet, "/api/reports/trips?veiculo="+vehicle.String()+"&"+period, nil, http.StatusNotFound)
	env.must(admin, http.MethodGet, "/api/reports/summary?"+period, nil, http.StatusBadRequest)
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/reports/summary?cliente="+frota.ID.String()+"&"+period, nil, http.StatusOK), &summary)
	if len(summary.Rows) != 1 || summary.Rows[0].Trips != 2 {
		t.Errorf("a central vê %+v", summary.Rows)
	}
	env.must(customer, http.MethodGet, "/api/reports/summary?de="+today.AddDate(0, 0, -40).Format(time.DateOnly), nil, http.StatusBadRequest)

	// Uma rodada sem posições novas não refaz nada.
	if n, err := reports.NewAggregator(env.db, "America/Sao_Paulo", log).Round(ctx); err != nil || n != 0 {
		t.Errorf("segunda rodada: %d dias, %v", n, err)
	}
}

// Os e-mails a mais dos alertas pela API: salvar, conferir e mostrar.
func TestAlertExtraRecipientsAPI(t *testing.T) {
	env, _ := newLeadsEnv(t, func(d *Deps) { d.AlertStore = alerts.NewDBStore(d.DB, d.Billing.IsSuspended) })
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	authSvc := auth.NewService(auth.NewRepository(env.db), config.Auth{BcryptCost: bcrypt.MinCost}, nil, log)
	if _, err := authSvc.CreateUser(ctx, "dono@extras.test", "Dono", auth.RoleCustomer, userPassword); err != nil {
		t.Fatal(err)
	}
	token := env.login("dono@extras.test")
	var resp struct {
		ExtraRecipients    []string `json:"extraRecipients"`
		MaxExtraRecipients int      `json:"maxExtraRecipients"`
		CanEditRecipients  bool     `json:"canEditRecipients"`
	}
	_ = json.Unmarshal(env.must(token, http.MethodGet, "/api/me/alerts", nil, http.StatusOK), &resp)
	if len(resp.ExtraRecipients) != 0 || resp.MaxExtraRecipients != 5 || !resp.CanEditRecipients {
		t.Fatalf("antes = %+v", resp)
	}
	_ = json.Unmarshal(env.must(token, http.MethodPut, "/api/me/alerts/recipients",
		map[string]any{"emails": []string{"Socio@Frota.test", "ops@frota.test"}}, http.StatusOK), &resp)
	if len(resp.ExtraRecipients) != 2 || resp.ExtraRecipients[0] != "socio@frota.test" && resp.ExtraRecipients[1] != "socio@frota.test" {
		t.Errorf("depois = %+v", resp)
	}
	env.must(token, http.MethodPut, "/api/me/alerts/recipients", map[string]any{"emails": []string{"sem-arroba"}}, http.StatusBadRequest)
}
