package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/alerts"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/devices"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/geofences"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/vehicles"
)

// A equipe restrita a grupos: a supervisora da região Norte vê, opera e
// recebe alertas só dos veículos do Norte; o veículo do Sul e o sem grupo
// respondem 404, como os de outra conta; o que é da conta inteira (faturas,
// grupos) fica de fora; nas cercas, mexe só nos veículos dela e os dos
// outros grupos continuam marcados. Tirar a restrição devolve a frota
// inteira; apagar o grupo dela não. Precisa de FARBO_TEST_DATABASE_URL.
func TestTeamRestrictedToGroups(t *testing.T) {
	notifier := &recordingTeamNotifier{}
	env := newCredEnvWith(t, credEnvOptions{stepUp: true, team: notifier, fleet: true})
	ctx := context.Background()
	authSvc := auth.NewService(auth.NewRepository(env.db), config.Auth{BcryptCost: bcrypt.MinCost}, nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	frota, err := authSvc.CreateUser(ctx, "frota@grupos-equipe.test", "Frota Ltda", auth.RoleCustomer, userPassword)
	if err != nil {
		t.Fatal(err)
	}
	norte, err := env.vehicles.CreateGroup(ctx, frota.ID, "Norte")
	if err != nil {
		t.Fatal(err)
	}
	sul, err := env.vehicles.CreateGroup(ctx, frota.ID, "Sul")
	if err != nil {
		t.Fatal(err)
	}
	newVehicle := func(imei, name string, group *vehicles.Group) *vehicles.Vehicle {
		dev, err := env.devices.Create(ctx, devices.Input{IMEI: imei, Protocol: "gt06"})
		if err != nil {
			t.Fatal(err)
		}
		in := vehicles.Input{Name: name, DeviceID: &dev.ID, OwnerID: &frota.ID}
		if group != nil {
			id := group.ID.String()
			in.GroupID = &id
		}
		v, err := env.vehicles.Create(ctx, in)
		if err != nil {
			t.Fatal(err)
		}
		env.sender.connected[imei] = true
		return v
	}
	vanNorte := newVehicle("869247061240001", "Van Norte", norte)
	vanSul := newVehicle("869247061240002", "Van Sul", sul)
	semGrupo := newVehicle("869247061240003", "Carro sem grupo", nil)
	owner := env.login("frota@grupos-equipe.test")

	expect := func(label, token, method, path string, body any, want int, code string) []byte {
		t.Helper()
		status, out := env.do(token, method, path, body)
		var parsed map[string]any
		_ = json.Unmarshal(out, &parsed)
		if status != want || (code != "" && parsed["code"] != code) {
			t.Fatalf("%s: %d %s (quer %d %s)", label, status, out, want, code)
		}
		return out
	}

	// A titular põe a supervisora só com o grupo Norte (grupo de outra conta
	// é recusado).
	grant, err := env.stepUp.VerifyPassword(ctx, frota.ID, "team_member", userPassword)
	if err != nil {
		t.Fatal(err)
	}
	status, out := env.doWith(owner, http.MethodPost, "/api/me/team", map[string]any{
		"name": "Su Supervisora", "email": "su@grupos-equipe.test", "role": "manager", "groups": []string{norte.ID.String()},
	}, map[string]string{"X-Step-Up-Token": grant.Token})
	var su struct {
		UserID     string      `json:"userId"`
		Restricted bool        `json:"restricted"`
		Groups     []uuid.UUID `json:"groups"`
	}
	_ = json.Unmarshal(out, &su)
	if status != http.StatusCreated || !su.Restricted || len(su.Groups) != 1 || su.Groups[0] != norte.ID {
		t.Fatalf("pôr na equipe: %d %s", status, out)
	}
	expect("grupo de outra conta", owner, http.MethodPatch, "/api/me/team/"+su.UserID,
		map[string]any{"groups": []string{uuid.NewString()}}, http.StatusBadRequest, "")
	var limit struct{ Limit, Used int }
	_ = json.Unmarshal(expect("limite", owner, http.MethodGet, "/api/me/team/limit", nil, http.StatusOK, ""), &limit)
	if limit.Limit != 30 || limit.Used != 1 {
		t.Errorf("limite da equipe = %+v", limit)
	}

	env.must("", http.MethodPost, "/api/auth/reset-password",
		map[string]string{"token": notifier.token("su@grupos-equipe.test"), "password": userPassword}, http.StatusNoContent)
	supervisor := env.login("su@grupos-equipe.test")
	var me struct {
		Membership *auth.Membership `json:"membership"`
	}
	_ = json.Unmarshal(expect("me", supervisor, http.MethodGet, "/api/auth/me", nil, http.StatusOK, ""), &me)
	if me.Membership == nil || !me.Membership.Restricted || len(me.Membership.Groups) != 1 {
		t.Fatalf("me = %+v", me.Membership)
	}

	// Só os veículos do Norte; o resto responde como se não existisse.
	var list []map[string]any
	_ = json.Unmarshal(expect("veículos", supervisor, http.MethodGet, "/api/vehicles", nil, http.StatusOK, ""), &list)
	if len(list) != 1 || list[0]["name"] != "Van Norte" {
		t.Fatalf("veículos da supervisora = %v", list)
	}
	path := func(v *vehicles.Vehicle) string { return "/api/vehicles/" + v.ID.String() }
	expect("posição do norte", supervisor, http.MethodPost, path(vanNorte)+"/commands/request-position", nil, http.StatusAccepted, "")
	expect("posição do sul", supervisor, http.MethodPost, path(vanSul)+"/commands/request-position", nil, http.StatusNotFound, "")
	expect("histórico sem grupo", supervisor, http.MethodGet, path(semGrupo)+"/positions", nil, http.StatusNotFound, "")
	expect("mover para o sul", supervisor, http.MethodPatch, path(vanNorte),
		map[string]any{"name": "Van Norte", "groupId": sul.ID.String()}, http.StatusForbidden, "RESTRICTED_ACCESS")
	expect("tirar do grupo", supervisor, http.MethodPatch, path(vanNorte),
		map[string]any{"name": "Van Norte", "groupId": ""}, http.StatusForbidden, "RESTRICTED_ACCESS")
	expect("renomear", supervisor, http.MethodPatch, path(vanNorte),
		map[string]any{"name": "Van Norte 01", "groupId": norte.ID.String()}, http.StatusOK, "")

	// O que é da conta inteira fica de fora; os grupos, só o dela.
	expect("faturas", supervisor, http.MethodGet, "/api/me/invoices", nil, http.StatusForbidden, "RESTRICTED_ACCESS")
	expect("criar grupo", supervisor, http.MethodPost, "/api/me/fleet/groups", map[string]any{"name": "Leste"}, http.StatusForbidden, "RESTRICTED_ACCESS")
	var fleet struct {
		Groups []vehicles.Group `json:"groups"`
	}
	_ = json.Unmarshal(expect("frota", supervisor, http.MethodGet, "/api/me/fleet", nil, http.StatusOK, ""), &fleet)
	if len(fleet.Groups) != 1 || fleet.Groups[0].Name != "Norte" {
		t.Errorf("grupos da supervisora = %+v", fleet.Groups)
	}
	var summary struct {
		Rows []map[string]any `json:"rows"`
	}
	_ = json.Unmarshal(expect("relatório", supervisor, http.MethodGet, "/api/reports/summary", nil, http.StatusOK, ""), &summary)
	if len(summary.Rows) != 1 || summary.Rows[0]["name"] != "Van Norte 01" {
		t.Errorf("relatório da supervisora = %+v", summary.Rows)
	}

	// Tempo real e alertas: só o Norte.
	if err := env.owners.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	suID := uuid.MustParse(su.UserID)
	if !env.owners.VisibleTo(suID, frota.ID, &vanNorte.ID, nil) || env.owners.VisibleTo(suID, frota.ID, &vanSul.ID, nil) ||
		env.owners.VisibleTo(suID, frota.ID, nil, semGrupo.DeviceID) || !env.owners.VisibleTo(frota.ID, frota.ID, &vanSul.ID, nil) {
		t.Error("o tempo real não respeita os grupos")
	}
	store := alerts.NewDBStore(env.db, nil)
	for v, want := range map[*vehicles.Vehicle]int{vanNorte: 1, vanSul: 0} {
		target, err := store.Target(ctx, *v.DeviceID)
		if err != nil {
			t.Fatal(err)
		}
		if len(target.Team) != want {
			t.Errorf("alertas de %s para a equipe: %d (quer %d)", v.Name, len(target.Team), want)
		}
	}

	// Cercas: a da titular com o Norte e o Sul aparece só com o Norte; a
	// edição dela mantém o Sul; apagar, não pode.
	var shared geofences.Geofence
	_ = json.Unmarshal(expect("cerca da titular", owner, http.MethodPost, "/api/geofences", map[string]any{
		"name": "Pátio", "latitude": -23.55, "longitude": -46.63, "radiusMeters": 300, "active": true,
		"vehicleIds": []string{vanNorte.ID.String(), vanSul.ID.String()},
	}, http.StatusCreated, ""), &shared)
	var fences []geofences.Geofence
	_ = json.Unmarshal(expect("cercas", supervisor, http.MethodGet, "/api/geofences", nil, http.StatusOK, ""), &fences)
	if len(fences) != 1 || len(fences[0].VehicleIDs) != 1 || fences[0].VehicleIDs[0] != vanNorte.ID {
		t.Fatalf("cercas da supervisora = %+v", fences)
	}
	edit := map[string]any{"name": "Pátio central", "latitude": -23.55, "longitude": -46.63, "radiusMeters": 400, "active": true,
		"vehicleIds": []string{vanNorte.ID.String()}}
	expect("editar a cerca", supervisor, http.MethodPatch, "/api/geofences/"+shared.ID.String(), edit, http.StatusOK, "")
	_ = json.Unmarshal(expect("cercas da titular", owner, http.MethodGet, "/api/geofences", nil, http.StatusOK, ""), &fences)
	if len(fences) != 1 || fences[0].Name != "Pátio central" || len(fences[0].VehicleIDs) != 2 {
		t.Fatalf("a edição tirou o Sul da cerca: %+v", fences)
	}
	edit["vehicleIds"] = []string{vanSul.ID.String()}
	expect("marcar o sul", supervisor, http.MethodPatch, "/api/geofences/"+shared.ID.String(), edit, http.StatusForbidden, "RESTRICTED_ACCESS")
	expect("apagar a cerca", supervisor, http.MethodDelete, "/api/geofences/"+shared.ID.String(), nil, http.StatusForbidden, "RESTRICTED_ACCESS")
	expect("cerca com o sul", supervisor, http.MethodPost, "/api/geofences", map[string]any{
		"name": "Base Sul", "latitude": -23.6, "longitude": -46.6, "radiusMeters": 300, "active": true,
		"vehicleIds": []string{vanSul.ID.String()},
	}, http.StatusForbidden, "RESTRICTED_ACCESS")

	// A manutenção: a titular cria em dois veículos; a supervisora vê e mexe
	// só na do Norte.
	expect("hodômetro", owner, http.MethodPut, path(vanNorte)+"/odometer", map[string]any{"km": 48000}, http.StatusOK, "")
	expect("manutenção da titular", owner, http.MethodPost, "/api/me/maintenance", map[string]any{
		"vehicleIds": []string{vanNorte.ID.String(), vanSul.ID.String()}, "name": "Troca de óleo", "everyKm": 10000, "everyMonths": 6,
	}, http.StatusCreated, "")
	var maint struct {
		Items []struct {
			ID        string   `json:"id"`
			VehicleID string   `json:"vehicleId"`
			LastKm    *float64 `json:"lastKm"`
			Status    string   `json:"status"`
		} `json:"items"`
	}
	_ = json.Unmarshal(expect("manutenção da titular", owner, http.MethodGet, "/api/me/maintenance", nil, http.StatusOK, ""), &maint)
	var sulItem string
	for _, it := range maint.Items {
		if it.VehicleID == vanSul.ID.String() {
			sulItem = it.ID
		}
	}
	if len(maint.Items) != 2 || sulItem == "" {
		t.Fatalf("manutenção da titular = %+v", maint)
	}
	_ = json.Unmarshal(expect("manutenção da supervisora", supervisor, http.MethodGet, "/api/me/maintenance", nil, http.StatusOK, ""), &maint)
	if len(maint.Items) != 1 || maint.Items[0].VehicleID != vanNorte.ID.String() || maint.Items[0].LastKm == nil || *maint.Items[0].LastKm != 48000 {
		t.Fatalf("manutenção da supervisora = %+v", maint)
	}
	expect("manutenção no sul", supervisor, http.MethodPost, "/api/me/maintenance",
		map[string]any{"vehicleIds": []string{vanSul.ID.String()}, "name": "Pneus", "everyKm": 40000}, http.StatusNotFound, "")
	expect("apagar a do sul", supervisor, http.MethodDelete, "/api/me/maintenance/"+sulItem, nil, http.StatusNotFound, "")
	expect("registrar a do sul", supervisor, http.MethodPost, "/api/me/maintenance/done",
		map[string]any{"itemIds": []string{maint.Items[0].ID, sulItem}}, http.StatusNotFound, "")
	expect("registrar a do norte", supervisor, http.MethodPost, "/api/me/maintenance/done",
		map[string]any{"itemIds": []string{maint.Items[0].ID}, "notes": "óleo 5W30"}, http.StatusOK, "")
	expect("hodômetro do sul", supervisor, http.MethodPut, path(vanSul)+"/odometer", map[string]any{"km": 1}, http.StatusNotFound, "")
	expect("horímetro do norte", supervisor, http.MethodPut, path(vanNorte)+"/engine-hours", map[string]any{"hours": 1200}, http.StatusOK, "")
	expect("horímetro do sul", supervisor, http.MethodPut, path(vanSul)+"/engine-hours", map[string]any{"hours": 1}, http.StatusNotFound, "")
	var detail struct {
		Odometer struct {
			Km    *float64 `json:"km"`
			Hours float64  `json:"hours"`
		} `json:"odometer"`
		Records []map[string]any `json:"records"`
	}
	_ = json.Unmarshal(expect("manutenção do veículo", supervisor, http.MethodGet, path(vanNorte)+"/maintenance", nil, http.StatusOK, ""), &detail)
	if detail.Odometer.Km == nil || *detail.Odometer.Km != 48000 || detail.Odometer.Hours != 1200 || len(detail.Records) != 1 ||
		detail.Records[0]["notes"] != "óleo 5W30" {
		t.Errorf("manutenção do veículo = %+v", detail)
	}

	// O relatório por e-mail: só de um grupo dela.
	expect("relatório da conta toda", supervisor, http.MethodPost, "/api/me/report-schedules",
		map[string]any{"frequency": "weekly"}, http.StatusBadRequest, "")
	expect("relatório do sul", supervisor, http.MethodPost, "/api/me/report-schedules",
		map[string]any{"frequency": "weekly", "groupId": sul.ID.String()}, http.StatusBadRequest, "")
	expect("relatório do norte", supervisor, http.MethodPost, "/api/me/report-schedules",
		map[string]any{"frequency": "monthly", "groupId": norte.ID.String(), "extraRecipients": []string{"regional@frota.test"}},
		http.StatusCreated, "")
	var schedules struct {
		Schedules        []map[string]any `json:"schedules"`
		CanAddRecipients bool             `json:"canAddRecipients"`
	}
	_ = json.Unmarshal(expect("relatórios agendados", supervisor, http.MethodGet, "/api/me/report-schedules", nil, http.StatusOK, ""), &schedules)
	if len(schedules.Schedules) != 1 || !schedules.CanAddRecipients {
		t.Fatalf("relatórios da supervisora = %+v", schedules)
	}
	_ = json.Unmarshal(expect("relatórios da titular", owner, http.MethodGet, "/api/me/report-schedules", nil, http.StatusOK, ""), &schedules)
	if len(schedules.Schedules) != 0 {
		t.Errorf("a titular vê os agendamentos da supervisora: %+v", schedules.Schedules)
	}

	// Apagar o grupo dela não devolve a frota inteira: fica sem veículo.
	expect("apagar o norte", owner, http.MethodDelete, "/api/me/fleet/groups/"+norte.ID.String(), nil, http.StatusNoContent, "")
	_ = json.Unmarshal(expect("veículos sem o grupo", supervisor, http.MethodGet, "/api/vehicles", nil, http.StatusOK, ""), &list)
	if len(list) != 0 {
		t.Fatalf("sem o grupo, ainda vê: %v", list)
	}
	// E o relatório do grupo apagado sai junto.
	_ = json.Unmarshal(expect("relatórios sem o grupo", supervisor, http.MethodGet, "/api/me/report-schedules", nil, http.StatusOK, ""), &schedules)
	if len(schedules.Schedules) != 0 {
		t.Errorf("o relatório do grupo apagado ficou: %+v", schedules.Schedules)
	}
	// Sem restrição (grupos vazio), a frota inteira.
	expect("tirar a restrição", owner, http.MethodPatch, "/api/me/team/"+su.UserID, map[string]any{"groups": []string{}}, http.StatusOK, "")
	_ = json.Unmarshal(expect("veículos sem restrição", supervisor, http.MethodGet, "/api/vehicles", nil, http.StatusOK, ""), &list)
	if len(list) != 3 {
		t.Fatalf("sem restrição vê %d veículos", len(list))
	}
	expect("faturas sem restrição", supervisor, http.MethodGet, "/api/me/invoices", nil, http.StatusOK, "")
}
