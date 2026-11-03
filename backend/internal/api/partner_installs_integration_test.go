package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/affiliates"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/appointments"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/contract"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/orders"
)

// A gestão da instalação pelo representante: ligar pede a senha da página;
// pela página (com a sessão), ele vê o cliente que indicou esperando a
// instalação (com o nome e o endereço), agenda (ele é o instalador, no
// endereço do cliente), remarca e cancela; não mexe em veículo de quem ele não
// indicou; a central vê na fila que o cliente é dele; e o rastreador do
// comodato encerrado entra na fila da retirada. Precisa de
// FARBO_TEST_DATABASE_URL.
func TestPartnerManagesInstalls(t *testing.T) {
	env, _ := newLeadsEnv(t, func(d *Deps) {
		d.Config.Catalog.SetupDueDays = 0
		d.Orders = orders.NewService(d.DB, d.Billing, d.Vehicles, d.Config.Catalog, d.Log)
		doc, err := contract.Render(contract.Params{Company: config.Company{Name: "Farbo Rastreadores"}, HistoryOptions: []int{7, 14, 30}})
		if err != nil {
			t.Fatal(err)
		}
		svc := contract.NewService(d.DB, doc, &recordingContractMailer{}, d.Log)
		svc.SetSync()
		d.Contract = svc
		installs := appointments.NewService(d.DB, "https://painel.farbo.test", d.Config.Auth.JWTSecret, d.Log)
		installs.SetSync()
		d.Appointments = installs
	})
	ctx := context.Background()
	admin := env.login(auth.RoleAdmin + "@leads.test")

	// Ligar a gestão pede a senha da página (ela mostra os dados dos clientes).
	env.must(admin, http.MethodPost, "/api/affiliates", map[string]any{"name": "Sem senha", "active": true, "manageInstalls": true},
		http.StatusBadRequest)
	var rui affiliates.Affiliate
	_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/affiliates", map[string]any{
		"name": "Rui Representante", "code": "rui", "active": true, "manageInstalls": true, "reportPassword": "senha-do-rui-1",
		"installPrices": map[string]any{"mode": "direct", "installCents": 12000, "uninstallCents": 8000},
	}, http.StatusCreated), &rui)
	if !rui.ManageInstalls {
		t.Fatalf("representante = %+v", rui)
	}
	// Tirar a senha com a gestão ligada não passa.
	env.must(admin, http.MethodPatch, "/api/affiliates/"+rui.ID.String(), map[string]any{
		"name": "Rui Representante", "code": "rui", "active": true, "manageInstalls": true, "reportPassword": "",
		"installPrices": map[string]any{"mode": "direct", "installCents": 12000, "uninstallCents": 8000},
	}, http.StatusBadRequest)

	// A cliente do Rui contrata o comodato pelo link; outro cliente, sem link.
	var o struct {
		ContractVersion string `json:"contractVersion"`
	}
	_ = json.Unmarshal(env.must("", http.MethodGet, "/api/public/checkout", nil, http.StatusOK), &o)
	address := map[string]any{"zipCode": "01305-000", "street": "Rua Augusta", "number": "500", "district": "Consolação",
		"city": "São Paulo", "state": "SP"}
	contractWith := func(ref, name, email, document string) billing.Subscription {
		t.Helper()
		var r struct {
			Subscription billing.Subscription `json:"subscription"`
		}
		_ = json.Unmarshal(env.must("", http.MethodPost, "/api/public/checkout", map[string]any{
			"name": name, "email": email, "phone": "(11) 95555-0000", "document": document, "password": "senha-forte-1",
			"vehicle": map[string]any{"kind": "CAR", "brand": "Fiat", "model": "Uno", "plate": ""}, "address": address,
			"contractVersion": o.ContractVersion, "accept": true, "ref": ref, "equipmentMode": "loan",
		}, http.StatusCreated), &r)
		return r.Subscription
	}
	ana := contractWith("rui", "Ana Indicada", "ana@rui.test", "529.982.247-25")
	other := contractWith("", "Bia Sem Link", "bia@site.test", "390.533.447-05")
	vehicleOf := func(sub billing.Subscription) uuid.UUID {
		t.Helper()
		var id uuid.UUID
		if err := env.db.QueryRow(ctx, `SELECT vehicle_id FROM subscriptions WHERE id = $1`, sub.ID).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	anaCar, biaCar := vehicleOf(ana), vehicleOf(other)

	page := "/api/public/partner/" + rui.ReportToken
	if status, _ := env.do("", http.MethodGet, page+"/installations", nil); status != http.StatusUnauthorized {
		t.Fatalf("sem a senha = %d", status)
	}
	_, out := env.do("", http.MethodPost, page+"/session", map[string]any{"password": "senha-do-rui-1"})
	var login struct {
		Session string `json:"session"`
	}
	_ = json.Unmarshal(out, &login)
	headers := map[string]string{partnerSessionHeader: login.Session}
	call := func(method, path string, body any, want int) []byte {
		t.Helper()
		status, out := env.doWith("", method, page+path, body, headers)
		if status != want {
			t.Fatalf("%s %s = %d %s", method, path, status, out)
		}
		return out
	}
	queueNow := func() appointments.PartnerQueue {
		t.Helper()
		var q appointments.PartnerQueue
		_ = json.Unmarshal(call(http.MethodGet, "/installations", nil, http.StatusOK), &q)
		return q
	}
	q := queueNow()
	if len(q.AwaitingInstall) != 1 || q.AwaitingInstall[0].VehicleID != anaCar || q.AwaitingInstall[0].CustomerName != "Ana Indicada" ||
		!strings.Contains(q.AwaitingInstall[0].Address, "Rua Augusta 500") || len(q.Appointments) != 0 {
		t.Fatalf("fila do representante = %+v", q)
	}

	// Agenda: ele é o instalador, no endereço da cliente.
	when := time.Now().Add(48 * time.Hour).Truncate(time.Minute)
	var a appointments.Appointment
	_ = json.Unmarshal(call(http.MethodPost, "/installations", map[string]any{
		"kind": "install", "vehicleId": anaCar, "scheduledFor": when, "notes": "portão azul",
	}, http.StatusCreated), &a)
	if a.InstallerName != "Rui Representante" || !strings.Contains(a.Address, "Rua Augusta 500") || a.URL == "" || a.Notes != "portão azul" {
		t.Fatalf("agendamento = %+v", a)
	}
	if q := queueNow(); len(q.AwaitingInstall) != 0 || len(q.Appointments) != 1 || q.Appointments[0].URL == "" {
		t.Fatalf("fila depois de agendar = %+v", q)
	}
	// Veículo de quem ele não indicou: não.
	call(http.MethodPost, "/installations", map[string]any{"kind": "install", "vehicleId": biaCar, "scheduledFor": when},
		http.StatusNotFound)
	// Remarca e cancela.
	later := when.Add(2 * time.Hour)
	_ = json.Unmarshal(call(http.MethodPatch, "/installations/"+a.ID.String(), map[string]any{"scheduledFor": later, "notes": "ligar antes"},
		http.StatusOK), &a)
	if !a.ScheduledFor.Equal(later) || a.Notes != "ligar antes" || a.InstallerName != "Rui Representante" {
		t.Errorf("remarcado = %+v", a)
	}
	_ = json.Unmarshal(call(http.MethodPost, "/installations/"+a.ID.String()+"/cancel", nil, http.StatusOK), &a)
	if a.Status != appointments.StatusCanceled {
		t.Errorf("cancelado = %+v", a)
	}

	// A central vê na fila que a cliente é do representante.
	var awaiting []struct {
		VehicleID      uuid.UUID `json:"vehicleId"`
		Representative string    `json:"representative"`
	}
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/installations/awaiting", nil, http.StatusOK), &awaiting)
	seen := map[uuid.UUID]string{}
	for _, w := range awaiting {
		seen[w.VehicleID] = w.Representative
	}
	if seen[anaCar] != "Rui Representante" || seen[biaCar] != "" {
		t.Errorf("fila da central = %+v", awaiting)
	}

	// O comodato encerrado com o rastreador no carro: a retirada é dele.
	var device uuid.UUID
	if err := env.db.QueryRow(ctx, `INSERT INTO devices (imei) VALUES ('869247061909999') RETURNING id`).Scan(&device); err != nil {
		t.Fatal(err)
	}
	if _, err := env.db.Exec(ctx, `UPDATE vehicles SET device_id = $2 WHERE id = $1`, anaCar, device); err != nil {
		t.Fatal(err)
	}
	if _, err := env.db.Exec(ctx, `UPDATE subscriptions SET status = 'CANCELED', updated_at = NOW() WHERE id = $1`, ana.ID); err != nil {
		t.Fatal(err)
	}
	q = queueNow()
	if len(q.AwaitingUninstall) != 1 || q.AwaitingUninstall[0].VehicleID != anaCar || q.AwaitingUninstall[0].IMEI != "869247061909999" {
		t.Fatalf("retirada = %+v", q.AwaitingUninstall)
	}
	call(http.MethodPost, "/installations", map[string]any{"kind": "uninstall", "vehicleId": anaCar, "scheduledFor": when},
		http.StatusCreated)

	// Sem a gestão ligada, a página não tem as instalações.
	env.must(admin, http.MethodPatch, "/api/affiliates/"+rui.ID.String(), map[string]any{
		"name": "Rui Representante", "code": "rui", "active": true, "manageInstalls": false,
	}, http.StatusOK)
	call(http.MethodGet, "/installations", nil, http.StatusNotFound)
}
