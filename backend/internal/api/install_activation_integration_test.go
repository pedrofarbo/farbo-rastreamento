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
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/vehicles"
)

// A ativação pelo link do instalador: no agendamento de um veículo sem
// rastreador, o link lista os rastreadores livres (sem os de veículo, os dos
// kits dos representantes e os reservados por outro link) e os chips livres;
// o rastreador que nunca conectou ganha o chip escolhido e a configuração por
// SMS (o já ativado é recusado, e o chip de outro rastreador também). O
// rastreador fica reservado para o link (some do estoque dos outros e não
// passa no teste deles); trocar o chip interrompe a ativação anterior; o
// link faz até 3. Na instalação avulsa, o rastreador é o do link: o
// instalador só escolhe o chip. Precisa de FARBO_TEST_DATABASE_URL.
func TestInstallerActivation(t *testing.T) {
	sms := &fakeProvider{statuses: map[string]string{}, reasons: map[string]string{}}
	env := newCredEnvWith(t, credEnvOptions{installs: true, sms: sms})
	ctx := context.Background()
	if _, err := env.auth.CreateUser(ctx, "op@ativa.test", "Operadora", auth.RoleOperator, userPassword); err != nil {
		t.Fatal(err)
	}
	customer, err := env.auth.CreateUser(ctx, "dono@ativa.test", "Dono Rui", auth.RoleCustomer, userPassword)
	if err != nil {
		t.Fatal(err)
	}
	car, err := env.vehicles.Create(ctx, vehicles.Input{Name: "Gol do Rui", Plate: "ATV1A11", OwnerID: &customer.ID})
	if err != nil {
		t.Fatal(err)
	}
	bike, err := env.vehicles.Create(ctx, vehicles.Input{Name: "Moto do Rui", Plate: "ATV2B22", OwnerID: &customer.ID})
	if err != nil {
		t.Fatal(err)
	}
	device := func(imei string) *devices.Device {
		t.Helper()
		d, err := env.devices.Create(ctx, devices.Input{IMEIConfirmed: true, IMEI: imei, Protocol: "gt06"})
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	fresh, seen, used, kit, alone := device("869247061240112"), device("869247061240229"), device("869247061240336"),
		device("869247061240443"), device("869247061240559")
	if _, err := env.db.Exec(ctx, `UPDATE devices SET last_seen_at = NOW() - INTERVAL '3 days' WHERE id = $1`, seen.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := env.vehicles.Create(ctx, vehicles.Input{Name: "Outro", DeviceID: &used.ID, OwnerID: &customer.ID}); err != nil {
		t.Fatal(err)
	}
	// O rastreador do kit de um representante é dele.
	if _, err := env.db.Exec(ctx, `
		WITH a AS (INSERT INTO affiliates (name, code, report_token, commission_cents) VALUES ('Rep', 'rep-ativa', 'tok-ativa', 600)
			RETURNING id),
		k AS (INSERT INTO kit_shipments (affiliate_id) SELECT id FROM a RETURNING id)
		INSERT INTO kit_shipment_devices (shipment_id, device_id) SELECT id, $1 FROM k`, kit.ID); err != nil {
		t.Fatal(err)
	}
	chip := func(iccid, phone string) uuid.UUID {
		t.Helper()
		c, err := env.devices.CreateChip(ctx, devices.ChipInput{ICCID: iccid, PhoneNumber: phone, Carrier: "Vivo", APN: "zap.vivo.com.br",
			APNUser: "vivo", APNPassword: "vivo"})
		if err != nil {
			t.Fatal(err)
		}
		return c.ID
	}
	chipA, chipB, chipC := chip("89551012345678901233", "11991110001"), chip("89551012345678901241", "11991110002"),
		chip("89551012345678901258", "11991110003")
	// O chip que já está num rastreador não é livre.
	busyChip := chip("89551012345678901266", "11991110004")
	if _, err := env.devices.AttachChip(ctx, used.ID, busyChip); err != nil {
		t.Fatal(err)
	}

	op := env.tokenFor("op@ativa.test")
	schedule := func(vehicle uuid.UUID) string {
		t.Helper()
		var appt struct {
			URL string `json:"url"`
			Pin string `json:"pin"`
		}
		_ = json.Unmarshal(env.must(op, http.MethodPost, "/api/installations", map[string]any{
			"vehicleId": vehicle, "installerName": "Zé", "scheduledFor": time.Now().Add(time.Hour), "address": "Rua A, 1",
		}, http.StatusCreated), &appt)
		path := "/api/public/install/" + strings.TrimPrefix(appt.URL, "https://painel.farbo.test/instalador/")
		env.withPin(path, appt.Pin)
		return path
	}
	base, other := schedule(car.ID), schedule(bike.ID)

	type stock struct {
		Devices []struct {
			ID        uuid.UUID `json:"id"`
			IMEI      string    `json:"imei"`
			Activated bool      `json:"activated"`
		} `json:"devices"`
		Chips []struct {
			ID    uuid.UUID `json:"id"`
			ICCID string    `json:"iccid"`
		} `json:"chips"`
	}
	stockOf := func(path string) (map[string]bool, map[uuid.UUID]bool) {
		t.Helper()
		var s stock
		_ = json.Unmarshal(env.must("", http.MethodGet, path+"/stock", nil, http.StatusOK), &s)
		devs, chips := map[string]bool{}, map[uuid.UUID]bool{}
		for _, d := range s.Devices {
			devs[d.IMEI] = d.Activated
		}
		for _, c := range s.Chips {
			chips[c.ID] = true
		}
		return devs, chips
	}
	devs, chips := stockOf(base)
	if activated, ok := devs[fresh.IMEI]; !ok || activated {
		t.Errorf("o novo, a ativar: %v", devs)
	}
	if activated, ok := devs[seen.IMEI]; !ok || !activated {
		t.Errorf("o que já conectou: %v", devs)
	}
	if _, ok := devs[used.IMEI]; ok {
		t.Errorf("o de outro veículo no estoque: %v", devs)
	}
	if _, ok := devs[kit.IMEI]; ok {
		t.Errorf("o do kit do representante no estoque: %v", devs)
	}
	if !chips[chipA] || !chips[chipB] || chips[busyChip] {
		t.Errorf("chips = %v", chips)
	}

	type activation struct {
		Enabled    bool `json:"enabled"`
		Left       int  `json:"left"`
		Activation *struct {
			IMEI   string `json:"imei"`
			ICCID  string `json:"iccid"`
			Status string `json:"status"`
			Steps  []struct {
				Label  string `json:"label"`
				Status string `json:"status"`
			} `json:"steps"`
		} `json:"activation"`
	}
	var act activation
	_ = json.Unmarshal(env.must("", http.MethodGet, base+"/activation", nil, http.StatusOK), &act)
	if !act.Enabled || act.Left != 3 || act.Activation != nil {
		t.Fatalf("antes = %+v", act)
	}
	activate := func(path string, body map[string]any, want int) []byte {
		t.Helper()
		return env.must("", http.MethodPost, path+"/activate", body, want)
	}
	// O que já conectou não precisa; o chip de outro rastreador, não; o de
	// outro veículo, não.
	if out := activate(base, map[string]any{"deviceId": seen.ID, "chipId": chipA}, http.StatusConflict); !strings.Contains(string(out), "já está ativado") {
		t.Errorf("ativado: %s", out)
	}
	if out := activate(base, map[string]any{"deviceId": fresh.ID, "chipId": busyChip}, http.StatusConflict); !strings.Contains(string(out), used.IMEI) {
		t.Errorf("chip de outro: %s", out)
	}
	activate(base, map[string]any{"deviceId": used.ID, "chipId": chipA}, http.StatusBadRequest)
	// Um chip sem o usuário e a senha do APN: a central precisa ajustar, e a
	// tentativa não conta.
	bare, err := env.devices.CreateChip(ctx, devices.ChipInput{ICCID: "89551012345678901274", PhoneNumber: "11991110005",
		Carrier: "Vivo", APN: "zap.vivo.com.br"})
	if err != nil {
		t.Fatal(err)
	}
	if out := activate(base, map[string]any{"deviceId": fresh.ID, "chipId": bare.ID}, http.StatusBadRequest); !strings.Contains(string(out), "Fale com a Farbo") {
		t.Errorf("chip sem a senha do APN: %s", out)
	}
	_ = json.Unmarshal(env.must("", http.MethodGet, base+"/activation", nil, http.StatusOK), &act)
	if act.Left != 3 {
		t.Errorf("a recusa contou como ativação: %+v", act)
	}

	// O novo com o chip A: o chip vai para o cadastro e a configuração sai
	// por SMS para a linha dele.
	_ = json.Unmarshal(activate(base, map[string]any{"deviceId": fresh.ID, "chipId": chipA}, http.StatusCreated), &act)
	if act.Left != 2 || act.Activation == nil || act.Activation.IMEI != fresh.IMEI || act.Activation.ICCID != "89551012345678901233" ||
		len(act.Activation.Steps) == 0 || act.Activation.Steps[0].Label == "" {
		t.Fatalf("ativação = %+v", act)
	}
	if raw := string(env.must("", http.MethodGet, base+"/activation", nil, http.StatusOK)); strings.Contains(raw, "zap.vivo") ||
		strings.Contains(raw, "farborastreadores.com.br") {
		t.Errorf("o link mostra os textos dos comandos: %s", raw)
	}
	got, err := env.devices.Get(ctx, fresh.ID)
	if err != nil || got.ICCID != "89551012345678901233" || got.PhoneNumber == "" || got.APN != "zap.vivo.com.br" {
		t.Fatalf("cadastro depois do chip = %+v (%v)", got, err)
	}
	if bodies := sms.bodies(); len(bodies) == 0 {
		t.Fatal("nenhum SMS saiu")
	}
	// A mesma de novo: já está andando.
	if out := activate(base, map[string]any{"deviceId": fresh.ID, "chipId": chipA}, http.StatusConflict); !strings.Contains(string(out), "em andamento") {
		t.Errorf("de novo: %s", out)
	}
	// Reservado: some do estoque do outro agendamento e não passa no teste dele.
	if devs, _ := stockOf(other); devs[fresh.IMEI] {
		t.Errorf("o reservado no estoque do outro: %v", devs)
	}
	if own, _ := stockOf(base); func() bool { _, ok := own[fresh.IMEI]; return !ok }() {
		t.Errorf("o reservado sumiu do próprio estoque: %v", own)
	}
	var check struct {
		OtherVehicle bool   `json:"otherVehicle"`
		Message      string `json:"message"`
	}
	_ = json.Unmarshal(env.must("", http.MethodPost, other+"/check", map[string]string{"imei": fresh.IMEI}, http.StatusOK), &check)
	if !check.OtherVehicle || !strings.Contains(check.Message, "outra instalação") {
		t.Errorf("teste do reservado no outro = %+v", check)
	}

	// Trocou o chip: a anterior para e começa outra (2 de 3); o chip A volta
	// ao estoque.
	var firstSession uuid.UUID
	_ = env.db.QueryRow(ctx, `SELECT activation_session_id FROM installation_appointments WHERE vehicle_id = $1`, car.ID).Scan(&firstSession)
	_ = json.Unmarshal(activate(base, map[string]any{"deviceId": fresh.ID, "chipId": chipB}, http.StatusCreated), &act)
	var firstStatus string
	_ = env.db.QueryRow(ctx, `SELECT status FROM sms_setup_sessions WHERE id = $1`, firstSession).Scan(&firstStatus)
	if act.Left != 1 || act.Activation.ICCID != "89551012345678901241" || firstStatus != "CANCELED" {
		t.Fatalf("troca do chip = %+v (anterior %s)", act, firstStatus)
	}
	if _, chips := stockOf(base); !chips[chipA] || chips[chipB] {
		t.Errorf("chips depois da troca = %v", chips)
	}
	// A terceira e o teto.
	activate(base, map[string]any{"deviceId": fresh.ID, "chipId": chipC}, http.StatusCreated)
	if out := activate(base, map[string]any{"deviceId": fresh.ID, "chipId": chipA}, http.StatusBadRequest); !strings.Contains(string(out), "todas as ativações") {
		t.Errorf("teto: %s", out)
	}

	// O rastreador conectou: a ativação fica pronta e ele aparece ativado.
	if _, err := env.db.Exec(ctx, `UPDATE sms_setup_sessions SET status = 'DONE', connected_at = NOW(), finished_at = NOW()
		WHERE device_id = $1 AND status IN ('SENDING', 'WAITING')`, fresh.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := env.db.Exec(ctx, `UPDATE devices SET last_seen_at = NOW(), status = 'ONLINE' WHERE id = $1`, fresh.ID); err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(env.must("", http.MethodGet, base+"/activation", nil, http.StatusOK), &act)
	if act.Activation == nil || act.Activation.Status != "DONE" || act.Left != 0 {
		t.Errorf("conectado = %+v", act)
	}
	if devs, _ := stockOf(base); !devs[fresh.IMEI] {
		t.Errorf("depois de conectar, ativado no estoque: %v", devs)
	}
	var withChip struct {
		Devices []struct {
			IMEI   string     `json:"imei"`
			ChipID *uuid.UUID `json:"chipId"`
		} `json:"devices"`
	}
	_ = json.Unmarshal(env.must("", http.MethodGet, base+"/stock", nil, http.StatusOK), &withChip)
	for _, d := range withChip.Devices {
		if d.IMEI == fresh.IMEI && (d.ChipID == nil || *d.ChipID != chipC) {
			t.Errorf("o chip do rastreador no estoque = %+v", d)
		}
	}

	// O veículo que recebeu o rastreador da central depois do agendamento
	// não ativa pelo link: é só instalar o dele.
	van, err := env.vehicles.Create(ctx, vehicles.Input{Name: "Van", Plate: "ATV3C33", OwnerID: &customer.ID})
	if err != nil {
		t.Fatal(err)
	}
	ready := schedule(van.ID)
	if _, err := env.db.Exec(ctx, `UPDATE vehicles SET device_id = $2 WHERE id = $1`, van.ID, seen.ID); err != nil {
		t.Fatal(err)
	}
	var view struct {
		VehicleIMEI string `json:"vehicleImei"`
	}
	_ = json.Unmarshal(env.must("", http.MethodGet, ready, nil, http.StatusOK), &view)
	if view.VehicleIMEI != seen.IMEI {
		t.Errorf("o rastreador do veículo no link = %+v", view)
	}
	env.must("", http.MethodGet, ready+"/stock", nil, http.StatusConflict)

	// A instalação avulsa: o rastreador é o do link; o instalador escolhe o chip.
	var link struct {
		URL string `json:"url"`
		Pin string `json:"pin"`
	}
	_ = json.Unmarshal(env.must(op, http.MethodPost, "/api/installer-links", map[string]any{
		"deviceId": alone.ID, "installerName": "Zé",
	}, http.StatusCreated), &link)
	avulsa := "/api/public/installer-links/" + link.URL[strings.LastIndex(link.URL, "/")+1:]
	env.withPin(avulsa, link.Pin)
	var standalone struct {
		NeedsActivation bool `json:"needsActivation"`
	}
	_ = json.Unmarshal(env.must("", http.MethodGet, avulsa, nil, http.StatusOK), &standalone)
	if !standalone.NeedsActivation {
		t.Fatalf("avulsa sem ativar = %+v", standalone)
	}
	if _, chips := stockOf(avulsa); !chips[chipA] {
		t.Errorf("chips da avulsa = %v", chips)
	}
	// O rastreador do link está reservado: some do estoque do agendamento.
	if devs, _ := stockOf(other); devs[alone.IMEI] {
		t.Errorf("o da avulsa no estoque do agendamento: %v", devs)
	}
	_ = json.Unmarshal(activate(avulsa, map[string]any{"chipId": chipA}, http.StatusCreated), &act)
	if act.Left != 2 || act.Activation == nil || act.Activation.IMEI != alone.IMEI || act.Activation.ICCID != "89551012345678901233" {
		t.Fatalf("ativação da avulsa = %+v", act)
	}
	// O chip que já está no rastreador do link vem primeiro (ativar de novo
	// com ele mesmo).
	var own stock
	_ = json.Unmarshal(env.must("", http.MethodGet, avulsa+"/stock", nil, http.StatusOK), &own)
	if len(own.Chips) == 0 || own.Chips[0].ID != chipA {
		t.Errorf("chips da avulsa depois da ativação = %+v", own.Chips)
	}
}
