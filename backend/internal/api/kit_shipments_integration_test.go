package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/affiliates"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/kits"
)

// Os kits dos representantes de ponta a ponta: a central monta a remessa com
// os rastreadores livres (o do veículo e o de outra remessa, não), envia com
// o código de rastreio; o representante acompanha pela página, confirma que
// recebeu e vê o estoque, que baixa quando ele instala (o cliente só com a
// senha na página); ele pede mais kits, a central atende ou cancela (os
// rastreadores voltam a ficar livres). Precisa de FARBO_TEST_DATABASE_URL.
func TestKitShipments(t *testing.T) {
	env, _ := newLeadsEnv(t, func(d *Deps) { d.Kits = kits.NewService(d.DB) })
	ctx := context.Background()
	admin := env.login(auth.RoleAdmin + "@leads.test")
	op := env.login(auth.RoleOperator + "@leads.test")

	var rui, ana affiliates.Affiliate
	_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/affiliates", map[string]any{
		"name": "Rui Representante", "code": "rui", "active": true, "handDelivery": true,
	}, http.StatusCreated), &rui)
	_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/affiliates", map[string]any{
		"name": "Ana Afiliada", "code": "ana", "active": true,
	}, http.StatusCreated), &ana)

	device := func(imei string) uuid.UUID {
		t.Helper()
		var id uuid.UUID
		if err := env.db.QueryRow(ctx, `INSERT INTO devices (imei, iccid) VALUES ($1, $2) RETURNING id`, imei, "8955"+imei).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	d1, d2, d3 := device("869247061900101"), device("869247061900102"), device("869247061900103")
	inCar := device("869247061900199")
	var customer uuid.UUID
	if err := env.db.QueryRow(ctx, `INSERT INTO users (email, name, role, password_hash) VALUES ('cliente@kits.test', 'Carla Cliente', 'customer', 'x')
		RETURNING id`).Scan(&customer); err != nil {
		t.Fatal(err)
	}
	if _, err := env.db.Exec(ctx, `INSERT INTO vehicles (name, plate, owner_id, device_id) VALUES ('Carro antigo', 'OLD1A11', $1, $2)`,
		customer, inCar); err != nil {
		t.Fatal(err)
	}

	available := func() map[uuid.UUID]bool {
		t.Helper()
		var list []kits.Device
		_ = json.Unmarshal(env.must(op, http.MethodGet, "/api/kit-shipments/available-devices", nil, http.StatusOK), &list)
		out := map[uuid.UUID]bool{}
		for _, d := range list {
			out[d.ID] = true
		}
		return out
	}
	if a := available(); !a[d1] || !a[d2] || !a[d3] || a[inCar] {
		t.Fatalf("livres = %v", a)
	}

	// A central monta e envia.
	var k kits.Shipment
	_ = json.Unmarshal(env.must(op, http.MethodPost, "/api/kit-shipments", map[string]any{
		"affiliateId": rui.ID, "deviceIds": []uuid.UUID{d1, d2}, "notes": "caixa azul",
	}, http.StatusCreated), &k)
	if k.Status != kits.StatusPreparing || len(k.Devices) != 2 || k.AffiliateName != "Rui Representante" {
		t.Fatalf("remessa = %+v", k)
	}
	env.must(op, http.MethodPost, "/api/kit-shipments", map[string]any{"affiliateId": ana.ID, "deviceIds": []uuid.UUID{d2}},
		http.StatusBadRequest)
	env.must(op, http.MethodPost, "/api/kit-shipments", map[string]any{"affiliateId": ana.ID, "deviceIds": []uuid.UUID{inCar}},
		http.StatusBadRequest)
	if a := available(); a[d1] || a[d2] || !a[d3] {
		t.Errorf("livres depois da remessa = %v", a)
	}
	env.must(op, http.MethodPost, "/api/kit-shipments/"+k.ID.String()+"/ship", map[string]any{"carrier": "", "trackingCode": "X"},
		http.StatusBadRequest)
	_ = json.Unmarshal(env.must(op, http.MethodPost, "/api/kit-shipments/"+k.ID.String()+"/ship",
		map[string]any{"carrier": "Correios SEDEX", "trackingCode": "qb123456789br"}, http.StatusOK), &k)
	if k.Status != kits.StatusShipped || k.TrackingCode != "QB123456789BR" || k.ShippedAt == nil {
		t.Fatalf("enviada = %+v", k)
	}

	// O representante acompanha e confirma que recebeu.
	page := "/api/public/partner/" + rui.ReportToken
	partner := func() kits.PartnerKits {
		t.Helper()
		var p kits.PartnerKits
		_ = json.Unmarshal(env.must("", http.MethodGet, page+"/kits", nil, http.StatusOK), &p)
		return p
	}
	if p := partner(); len(p.Shipments) != 1 || p.Shipments[0].TrackingCode != "QB123456789BR" || len(p.Stock) != 0 {
		t.Fatalf("página antes de receber = %+v", p)
	}
	// Outro afiliado não confirma a remessa do Rui.
	env.must("", http.MethodPost, "/api/public/partner/"+ana.ReportToken+"/kits/"+k.ID.String()+"/received", nil, http.StatusNotFound)
	env.must("", http.MethodPost, page+"/kits/"+k.ID.String()+"/received", nil, http.StatusOK)
	if p := partner(); len(p.Stock) != 2 || p.Installed != 0 || p.Shipments[0].Status != kits.StatusDelivered {
		t.Fatalf("página depois de receber = %+v", p)
	}

	// Ele instala um no carro de um cliente: o estoque baixa (o cliente só com senha).
	if _, err := env.db.Exec(ctx, `INSERT INTO vehicles (name, plate, owner_id, device_id) VALUES ('Moto da Carla', 'CAR2B22', $1, $2)`,
		customer, d1); err != nil {
		t.Fatal(err)
	}
	p := partner()
	if len(p.Stock) != 1 || p.Installed != 1 {
		t.Fatalf("estoque depois de instalar = %+v", p)
	}
	for _, d := range p.Shipments[0].Devices {
		if d.ID == d1 && (!d.Installed || d.Plate != "CAR2B22" || d.CustomerName != "") {
			t.Errorf("instalado sem senha = %+v", d)
		}
	}

	// Ele pede mais; a central atende com o que tem e depois cancela.
	var req kits.Shipment
	_ = json.Unmarshal(env.must("", http.MethodPost, page+"/kits/request", map[string]any{"quantity": 3, "note": "para a semana que vem"},
		http.StatusCreated), &req)
	if req.Status != kits.StatusRequested || req.RequestedQuantity == nil || *req.RequestedQuantity != 3 {
		t.Fatalf("pedido = %+v", req)
	}
	env.must("", http.MethodPost, page+"/kits/request", map[string]any{"quantity": 0}, http.StatusBadRequest)
	// Um pedido por vez.
	env.must("", http.MethodPost, page+"/kits/request", map[string]any{"quantity": 1}, http.StatusBadRequest)
	var list []kits.Shipment
	_ = json.Unmarshal(env.must(op, http.MethodGet, "/api/kit-shipments?status=requested", nil, http.StatusOK), &list)
	if len(list) != 1 || list[0].ID != req.ID {
		t.Fatalf("pedidos na central = %+v", list)
	}
	_ = json.Unmarshal(env.must(op, http.MethodPost, "/api/kit-shipments/"+req.ID.String()+"/prepare",
		map[string]any{"deviceIds": []uuid.UUID{d3}}, http.StatusOK), &req)
	if req.Status != kits.StatusPreparing || len(req.Devices) != 1 {
		t.Fatalf("pedido preparado = %+v", req)
	}
	_ = json.Unmarshal(env.must(op, http.MethodPost, "/api/kit-shipments/"+req.ID.String()+"/cancel", nil, http.StatusOK), &req)
	if req.Status != kits.StatusCanceled || len(req.Devices) != 0 || !available()[d3] {
		t.Fatalf("cancelado = %+v (livre %v)", req, available()[d3])
	}
	// A entregue não se cancela.
	env.must(op, http.MethodPost, "/api/kit-shipments/"+k.ID.String()+"/cancel", nil, http.StatusBadRequest)
	// Ele pede de novo e desiste: só o pedido dele, e só enquanto espera a central.
	_ = json.Unmarshal(env.must("", http.MethodPost, page+"/kits/request", map[string]any{"quantity": 2}, http.StatusCreated), &req)
	env.must("", http.MethodPost, "/api/public/partner/"+ana.ReportToken+"/kits/"+req.ID.String()+"/cancel", nil, http.StatusNotFound)
	_ = json.Unmarshal(env.must("", http.MethodPost, page+"/kits/"+req.ID.String()+"/cancel", nil, http.StatusOK), &req)
	if req.Status != kits.StatusCanceled {
		t.Errorf("pedido desistido = %+v", req)
	}
	env.must("", http.MethodPost, page+"/kits/"+k.ID.String()+"/cancel", nil, http.StatusBadRequest)

	// A página do Rui mostra a seção dos kits; a da Ana (sem nada disso), não.
	var report struct {
		Kits bool `json:"kits"`
	}
	_ = json.Unmarshal(env.must("", http.MethodGet, page, nil, http.StatusOK), &report)
	if !report.Kits {
		t.Error("página do Rui sem os kits")
	}
	_ = json.Unmarshal(env.must("", http.MethodGet, "/api/public/partner/"+ana.ReportToken, nil, http.StatusOK), &report)
	if report.Kits {
		t.Error("página da Ana com os kits")
	}

	// Com a senha na página, o cliente do rastreador instalado aparece.
	env.must(admin, http.MethodPatch, "/api/affiliates/"+rui.ID.String(), map[string]any{
		"name": "Rui Representante", "code": "rui", "active": true, "handDelivery": true, "reportPassword": "senha-do-rui-1",
	}, http.StatusOK)
	if status, _ := env.do("", http.MethodGet, page+"/kits", nil); status != http.StatusUnauthorized {
		t.Fatalf("sem a sessão = %d", status)
	}
	_, raw := env.do("", http.MethodPost, page+"/session", map[string]any{"password": "senha-do-rui-1"})
	var login struct {
		Session string `json:"session"`
	}
	_ = json.Unmarshal(raw, &login)
	status, raw := env.doWith("", http.MethodGet, page+"/kits", nil, map[string]string{partnerSessionHeader: login.Session})
	if status != http.StatusOK {
		t.Fatalf("com a sessão = %d %s", status, raw)
	}
	_ = json.Unmarshal(raw, &p)
	names := map[uuid.UUID]string{}
	for _, k := range p.Shipments {
		for _, d := range k.Devices {
			names[d.ID] = d.CustomerName
		}
	}
	if names[d1] != "Carla Cliente" || names[d2] != "" {
		t.Errorf("clientes com a senha = %v", names)
	}
}
