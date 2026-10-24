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
)

// A central sobe os associados por planilha dentro da associação; a
// associação pede a retirada do rastreador (ou desliga o associado) e o
// veículo entra na fila de desinstalação da central, que agenda; pelo link,
// o instalador tira a foto do rastreador retirado e conclui — o veículo fica
// sem rastreador e a desinstalação conta na fatura da associação. Precisa de
// FARBO_TEST_DATABASE_URL.
func TestUninstallAgenda(t *testing.T) {
	notifier := &recordingOrgNotifier{}
	env := newCredEnvWith(t, credEnvOptions{orgs: notifier, installs: true})
	pusher := &recordingPusher{}
	env.installs.SetPusher(pusher)
	ctx := context.Background()
	if _, err := env.auth.CreateUser(ctx, "admin@retirada.test", "Central", auth.RoleAdmin, userPassword); err != nil {
		t.Fatal(err)
	}
	central := env.tokenFor("admin@retirada.test")
	var created struct {
		Organization struct {
			ID uuid.UUID `json:"id"`
		} `json:"organization"`
	}
	_ = json.Unmarshal(env.must(central, http.MethodPost, "/api/organizations", map[string]any{
		"kind": "association", "name": "Associação Retirada", "document": "11.222.333/0001-81",
		"email": "contato@retirada.test", "priceCents": 3000, "dueDay": 10, "uninstallFeeCents": 4000,
		"admin": map[string]string{"name": "Gil Gestor", "email": "gil@retirada.test"},
	}, http.StatusCreated), &created)
	orgID := created.Organization.ID
	env.must("", http.MethodPost, "/api/auth/reset-password",
		map[string]string{"token": notifier.token("gil@retirada.test"), "password": userPassword}, http.StatusNoContent)
	gil := env.tokenFor("gil@retirada.test")

	// A central sobe a planilha (a associação não usa a rota da central).
	rows := []map[string]any{
		{"name": "Ana Associada", "email": "ana@retirada.test", "vehicle": map[string]any{"name": "Gol da Ana", "plate": "RET1A11"}},
		{"name": "Beto Associado", "email": "beto@retirada.test", "vehicle": map[string]any{"name": "Uno do Beto", "plate": "RET2B22"}},
	}
	importURL := "/api/organizations/" + orgID.String() + "/associates/import"
	env.must(gil, http.MethodPost, importURL, map[string]any{"rows": rows}, http.StatusForbidden)
	var dry struct {
		DryRun               bool
		Associates, Vehicles int
	}
	_ = json.Unmarshal(env.must(central, http.MethodPost, importURL, map[string]any{"rows": rows, "dryRun": true}, http.StatusOK), &dry)
	if !dry.DryRun || dry.Associates != 2 || dry.Vehicles != 2 {
		t.Fatalf("conferência = %+v", dry)
	}
	_ = json.Unmarshal(env.must(central, http.MethodPost, importURL, map[string]any{"rows": rows}, http.StatusCreated), &dry)
	if dry.DryRun || dry.Associates != 2 {
		t.Fatalf("importação = %+v", dry)
	}
	var associates []struct {
		ID   uuid.UUID `json:"id"`
		Name string    `json:"name"`
	}
	_ = json.Unmarshal(env.must(gil, http.MethodGet, "/api/org/associates", nil, http.StatusOK), &associates)
	if len(associates) != 2 {
		t.Fatalf("associados = %+v", associates)
	}

	// Os dois veículos com rastreador instalado (contam na fatura).
	vehicleOf := func(plate string) uuid.UUID {
		var id uuid.UUID
		if err := env.db.QueryRow(ctx, `SELECT id FROM vehicles WHERE plate = $1`, plate).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	gol, uno := vehicleOf("RET1A11"), vehicleOf("RET2B22")
	golDev, err := env.devices.Create(ctx, devices.Input{IMEIConfirmed: true, IMEI: "869247061230333", Protocol: "gt06"})
	if err != nil {
		t.Fatal(err)
	}
	unoDev, err := env.devices.Create(ctx, devices.Input{IMEIConfirmed: true, IMEI: "869247061230444", Protocol: "gt06"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.db.Exec(ctx, `UPDATE vehicles SET device_id = CASE id WHEN $1 THEN $2::uuid ELSE $4::uuid END WHERE id IN ($1, $3)`,
		gol, golDev.ID, uno, unoDev.ID); err != nil {
		t.Fatal(err)
	}
	if err := env.orgs.SyncVehicles(ctx); err != nil {
		t.Fatal(err)
	}

	type waiting struct {
		VehicleID uuid.UUID `json:"vehicleId"`
		IMEI      string    `json:"imei"`
		Reason    string    `json:"reason"`
		Note      string    `json:"note"`
	}
	awaiting := func() []waiting {
		var list []waiting
		_ = json.Unmarshal(env.must(central, http.MethodGet, "/api/installations/awaiting-uninstall", nil, http.StatusOK), &list)
		return list
	}
	if got := awaiting(); len(got) != 0 {
		t.Fatalf("ninguém pediu = %+v", got)
	}

	// A associação pede a retirada do Gol (e desiste, e pede de novo).
	env.must(gil, http.MethodPost, "/api/org/vehicles/"+gol.String()+"/uninstall", map[string]string{"note": "Vendeu o carro"}, http.StatusNoContent)
	if got := awaiting(); len(got) != 1 || got[0].VehicleID != gol || got[0].Reason != "requested" || got[0].IMEI != golDev.IMEI ||
		got[0].Note != "Vendeu o carro" {
		t.Fatalf("pedido = %+v", got)
	}
	env.must(gil, http.MethodDelete, "/api/org/vehicles/"+gol.String()+"/uninstall", nil, http.StatusNoContent)
	if got := awaiting(); len(got) != 0 {
		t.Fatalf("pedido cancelado = %+v", got)
	}
	env.must(gil, http.MethodPost, "/api/org/vehicles/"+gol.String()+"/uninstall", map[string]string{"note": "Vendeu o carro"}, http.StatusNoContent)
	// O veículo de fora da associação não aparece para ela.
	other, err := env.auth.CreateUser(ctx, "fora@retirada.test", "Fora", auth.RoleCustomer, userPassword)
	if err != nil {
		t.Fatal(err)
	}
	var outside uuid.UUID
	if err := env.db.QueryRow(ctx, `INSERT INTO vehicles (name, owner_id) VALUES ('Fora', $1) RETURNING id`, other.ID).Scan(&outside); err != nil {
		t.Fatal(err)
	}
	env.must(gil, http.MethodPost, "/api/org/vehicles/"+outside.String()+"/uninstall", map[string]string{}, http.StatusNotFound)

	// O associado desligado entra na fila sem pedido.
	beto := associates[0].ID
	if associates[0].Name != "Beto Associado" {
		beto = associates[1].ID
	}
	if _, err := env.db.Exec(ctx, `UPDATE users SET active = FALSE WHERE id = $1`, beto); err != nil {
		t.Fatal(err)
	}
	reasons := map[uuid.UUID]string{}
	for _, w := range awaiting() {
		reasons[w.VehicleID] = w.Reason
	}
	if len(reasons) != 2 || reasons[gol] != "requested" || reasons[uno] != "inactive" {
		t.Fatalf("fila = %+v", reasons)
	}

	// A central agenda a retirada do Gol (instalação, não: já tem rastreador).
	when := time.Now().Add(3 * time.Hour)
	env.must(central, http.MethodPost, "/api/installations", map[string]any{
		"vehicleId": gol, "installerName": "Zé", "scheduledFor": when, "address": "Rua A, 1",
	}, http.StatusBadRequest)
	var appt struct {
		ID   uuid.UUID `json:"id"`
		Kind string    `json:"kind"`
		IMEI string    `json:"imei"`
		URL  string    `json:"url"`
	}
	_ = json.Unmarshal(env.must(central, http.MethodPost, "/api/installations", map[string]any{
		"kind": "uninstall", "vehicleId": gol, "installerName": "Zé", "scheduledFor": when, "address": "Rua A, 1",
	}, http.StatusCreated), &appt)
	if appt.Kind != "uninstall" || appt.IMEI != golDev.IMEI {
		t.Fatalf("desinstalação agendada = %+v", appt)
	}
	ana := associates[0].ID
	if ana == beto {
		ana = associates[1].ID
	}
	if got := pusher.take(); len(got[ana]) != 1 || !strings.HasPrefix(got[ana][0].Title, "Desinstalação agendada") {
		t.Fatalf("aviso à associada = %+v", got)
	}
	var detail struct {
		Appointments []struct {
			Kind string `json:"kind"`
		} `json:"appointments"`
		UninstallRequests map[string]struct {
			Note string `json:"note"`
		} `json:"uninstallRequests"`
	}
	_ = json.Unmarshal(env.must(gil, http.MethodGet, "/api/org/associates/"+ana.String(), nil, http.StatusOK), &detail)
	if len(detail.Appointments) != 1 || detail.Appointments[0].Kind != "uninstall" || detail.UninstallRequests[gol.String()].Note != "Vendeu o carro" {
		t.Fatalf("ficha da associada = %+v", detail)
	}

	// O instalador, pelo link: sem teste do sinal; sem a foto, não conclui.
	base := "/api/public/install/" + strings.TrimPrefix(appt.URL, "https://painel.farbo.test/instalador/")
	var view struct {
		Kind   string `json:"kind"`
		IMEI   string `json:"imei"`
		Status string `json:"status"`
	}
	_ = json.Unmarshal(env.must("", http.MethodGet, base, nil, http.StatusOK), &view)
	if view.Kind != "uninstall" || view.IMEI != golDev.IMEI {
		t.Fatalf("link = %+v", view)
	}
	env.must("", http.MethodPost, base+"/check", map[string]string{"imei": golDev.IMEI}, http.StatusConflict)
	env.must("", http.MethodPost, base+"/complete", map[string]string{}, http.StatusBadRequest)
	if status, out := env.upload("", http.MethodPut, base+"/photos/tracker", jpegBytes, nil); status != http.StatusNoContent {
		t.Fatalf("foto: %d %s", status, out)
	}
	_ = json.Unmarshal(env.must("", http.MethodPost, base+"/complete", map[string]string{"note": "Retirado do painel"}, http.StatusOK), &view)
	if view.Status != "done" || view.IMEI != golDev.IMEI {
		t.Fatalf("concluída = %+v", view)
	}
	got, err := env.vehicles.Get(ctx, gol)
	if err != nil || got.DeviceID != nil {
		t.Fatalf("o rastreador continua no veículo: %+v %v", got, err)
	}
	if list := awaiting(); len(list) != 1 || list[0].VehicleID != uno {
		t.Fatalf("fila depois da retirada = %+v", list)
	}
	var install []struct {
		VehicleID uuid.UUID `json:"vehicleId"`
	}
	_ = json.Unmarshal(env.must(central, http.MethodGet, "/api/installations/awaiting", nil, http.StatusOK), &install)
	for _, w := range install {
		if w.VehicleID == gol {
			t.Fatal("o veículo desinstalado voltou para a fila de instalação")
		}
	}
	// A desinstalação conta na fatura da associação.
	if err := env.orgs.SyncVehicles(ctx); err != nil {
		t.Fatal(err)
	}
	uninstalledOf := func(vehicle uuid.UUID) bool {
		var uninstalled bool
		if err := env.db.QueryRow(ctx, `SELECT uninstalled FROM organization_vehicles WHERE vehicle_id = $1`, vehicle).
			Scan(&uninstalled); err != nil {
			t.Fatal(err)
		}
		return uninstalled
	}
	if !uninstalledOf(gol) {
		t.Fatal("a retirada do Gol não entrou na cobrança")
	}
	// O associado desligado: o veículo saiu da conta com o rastreador; a
	// retirada depois também cobra a desinstalação.
	if uninstalledOf(uno) {
		t.Fatal("desligar o associado não é desinstalação")
	}
	var unoAppt struct {
		URL string `json:"url"`
	}
	_ = json.Unmarshal(env.must(central, http.MethodPost, "/api/installations", map[string]any{
		"kind": "uninstall", "vehicleId": uno, "installerName": "Zé", "scheduledFor": when, "address": "Rua B, 2",
	}, http.StatusCreated), &unoAppt)
	unoBase := "/api/public/install/" + strings.TrimPrefix(unoAppt.URL, "https://painel.farbo.test/instalador/")
	if status, out := env.upload("", http.MethodPut, unoBase+"/photos/tracker", jpegBytes, nil); status != http.StatusNoContent {
		t.Fatalf("foto: %d %s", status, out)
	}
	env.must("", http.MethodPost, unoBase+"/complete", map[string]string{}, http.StatusOK)
	if err := env.orgs.SyncVehicles(ctx); err != nil {
		t.Fatal(err)
	}
	if !uninstalledOf(uno) {
		t.Fatal("a retirada do veículo do associado desligado não entrou na cobrança")
	}
	if list := awaiting(); len(list) != 0 {
		t.Fatalf("fila vazia = %+v", list)
	}
}
