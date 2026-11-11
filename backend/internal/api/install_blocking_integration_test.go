package api

import (
	"context"
	"encoding/json"
	"net/http"
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

// A instalação com o relé do bloqueio: o representante tem o preço dela ao
// lado da sem bloqueio; a oferta pelo link mostra os dois; o cliente escolhe
// no pedido (no incluso com a instalação contratada e no comodato), e a
// escolha fica na assinatura, que a fila da central, a página do
// representante e o link do instalador mostram. Com preços próprios, a Farbo
// cobra a com bloqueio; pelo link sem a escolha, o pedido com bloqueio é
// recusado; por conta própria, a escolha não vale. O representante muda (ou
// tira) o preço pela página dele. Precisa de FARBO_TEST_DATABASE_URL.
func TestInstallBlocking(t *testing.T) {
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

	// O preço com bloqueio fora da faixa não passa.
	env.must(admin, http.MethodPost, "/api/affiliates", map[string]any{
		"name": "Errado", "active": true,
		"installPrices": map[string]any{"mode": "direct", "installCents": 100, "uninstallCents": 100, "blockingCents": -1},
	}, http.StatusBadRequest)
	create := func(body map[string]any) affiliates.Affiliate {
		t.Helper()
		var a affiliates.Affiliate
		_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/affiliates", body, http.StatusCreated), &a)
		return a
	}
	carlos := create(map[string]any{
		"name": "Carlos Representante", "code": "carlos", "active": true, "manageInstalls": true,
		"reportPassword": "senha-do-carlos-1",
		"installPrices":  map[string]any{"mode": "direct", "installCents": 10000, "uninstallCents": 8500, "blockingCents": 18000},
	})
	if p := carlos.InstallPrices; p == nil || p.BlockingCents == nil || *p.BlockingCents != 18000 || p.InstallCents != 10000 {
		t.Fatalf("representante = %+v", carlos.InstallPrices)
	}
	create(map[string]any{
		"name": "Loja Com Bloqueio", "code": "loja-b", "active": true,
		"installPrices": map[string]any{"mode": "custom", "installCents": 9000, "uninstallCents": 5000, "blockingCents": 16000},
	})
	create(map[string]any{
		"name": "Loja Sem Bloqueio", "code": "loja-s", "active": true,
		"installPrices": map[string]any{"mode": "custom", "installCents": 9000, "uninstallCents": 5000},
	})

	type offerView struct {
		Install *struct {
			PriceCents    int `json:"priceCents"`
			BlockingCents int `json:"blockingCents"`
		} `json:"install"`
		ContractVersion string `json:"contractVersion"`
	}
	offer := func(query string) offerView {
		t.Helper()
		var o offerView
		_ = json.Unmarshal(env.must("", http.MethodGet, "/api/public/checkout"+query, nil, http.StatusOK), &o)
		return o
	}
	o := offer("?ref=carlos")
	if o.Install == nil || o.Install.PriceCents != 10000 || o.Install.BlockingCents != 18000 {
		t.Fatalf("oferta do representante = %+v", o.Install)
	}
	if s := offer("?ref=loja-s"); s.Install == nil || s.Install.BlockingCents != 0 {
		t.Fatalf("oferta sem a escolha = %+v", s.Install)
	}
	if s := offer(""); s.Install == nil || s.Install.BlockingCents != 0 {
		t.Fatalf("oferta do site = %+v", s.Install)
	}

	address := map[string]any{"zipCode": "01305-000", "street": "Rua Augusta", "number": "500", "district": "Consolação",
		"city": "São Paulo", "state": "SP"}
	type result struct {
		Subscription   billing.Subscription `json:"subscription"`
		InstallInvoice *billing.Invoice     `json:"installInvoice"`
	}
	body := func(ref, email, document string, extra map[string]any) map[string]any {
		b := map[string]any{
			"name": "Gil Souza", "email": email, "phone": "(11) 95555-0000", "document": document, "password": "senha-forte-1",
			"vehicle": map[string]any{"kind": "CAR", "brand": "Fiat", "model": "Uno", "plate": ""}, "address": address,
			"contractVersion": o.ContractVersion, "accept": true, "ref": ref,
		}
		for k, v := range extra {
			b[k] = v
		}
		return b
	}
	contractWith := func(ref, email, document string, extra map[string]any) result {
		t.Helper()
		var r result
		_ = json.Unmarshal(env.must("", http.MethodPost, "/api/public/checkout", body(ref, email, document, extra), http.StatusCreated), &r)
		return r
	}
	blocking := func(sub billing.Subscription) bool {
		t.Helper()
		var on bool
		if err := env.db.QueryRow(ctx, `SELECT install_blocking FROM subscriptions WHERE id = $1`, sub.ID).Scan(&on); err != nil {
			t.Fatal(err)
		}
		return on
	}

	// Pelo representante, o incluso com a instalação com bloqueio: paga direto
	// a ele (sem fatura), e a escolha fica na assinatura.
	withRelay := contractWith("carlos", "ana@carlos.test", "529.982.247-25", map[string]any{"farboInstall": true, "installBlocking": true})
	if withRelay.InstallInvoice != nil || !blocking(withRelay.Subscription) {
		t.Errorf("incluso com bloqueio pelo representante = %+v (instalação %+v)", withRelay.Subscription, withRelay.InstallInvoice)
	}
	// Sem bloqueio, e por conta própria (a escolha não vale): sem a marca.
	plain := contractWith("carlos", "bia@carlos.test", "390.533.447-05", map[string]any{"farboInstall": true})
	self := contractWith("carlos", "caio@carlos.test", "111.444.777-35", map[string]any{"installBlocking": true})
	if blocking(plain.Subscription) || blocking(self.Subscription) {
		t.Errorf("sem bloqueio = %v, por conta própria = %v", blocking(plain.Subscription), blocking(self.Subscription))
	}
	// Com preços próprios, a Farbo cobra a com bloqueio — no comodato e no
	// incluso com a instalação contratada.
	loan := contractWith("loja-b", "dani@loja.test", "714.602.380-01", map[string]any{"equipmentMode": "loan", "installBlocking": true})
	if loan.InstallInvoice == nil || loan.InstallInvoice.AmountCents != 16000 || !blocking(loan.Subscription) {
		t.Errorf("comodato com bloqueio pela loja = %+v (instalação %+v)", loan.Subscription, loan.InstallInvoice)
	}
	sale := contractWith("loja-b", "edu@loja.test", "275.484.389-23", map[string]any{"farboInstall": true, "installBlocking": true})
	if sale.InstallInvoice == nil || sale.InstallInvoice.AmountCents != 16000 || !blocking(sale.Subscription) {
		t.Errorf("incluso com bloqueio pela loja = %+v (instalação %+v)", sale.Subscription, sale.InstallInvoice)
	}
	nope := contractWith("loja-b", "fabi@loja.test", "123.456.789-09", map[string]any{"equipmentMode": "loan"})
	if nope.InstallInvoice == nil || nope.InstallInvoice.AmountCents != 9000 || blocking(nope.Subscription) {
		t.Errorf("comodato sem bloqueio pela loja = %+v (instalação %+v)", nope.Subscription, nope.InstallInvoice)
	}
	// Pelo link sem a escolha (e pelo site), o pedido com bloqueio é recusado.
	env.must("", http.MethodPost, "/api/public/checkout", body("loja-s", "gabi@loja.test", "987.654.321-00",
		map[string]any{"equipmentMode": "loan", "installBlocking": true}), http.StatusBadRequest)
	env.must("", http.MethodPost, "/api/public/checkout", body("", "hugo@site.test", "987.654.321-00",
		map[string]any{"farboInstall": true, "installBlocking": true}), http.StatusBadRequest)

	// Pagas, entram na fila: a central e o representante veem o com bloqueio.
	if _, err := env.db.Exec(ctx, `UPDATE invoices SET status = 'PAID', paid_at = NOW() WHERE customer_id IN
		(SELECT customer_id FROM subscriptions WHERE id = ANY($1))`, []uuid.UUID{withRelay.Subscription.ID, plain.Subscription.ID}); err != nil {
		t.Fatal(err)
	}
	vehicleOf := func(sub billing.Subscription) uuid.UUID {
		t.Helper()
		var id uuid.UUID
		if err := env.db.QueryRow(ctx, `SELECT vehicle_id FROM subscriptions WHERE id = $1`, sub.ID).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	relayCar, plainCar := vehicleOf(withRelay.Subscription), vehicleOf(plain.Subscription)
	var awaiting []appointments.Awaiting
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/installations/awaiting", nil, http.StatusOK), &awaiting)
	seen := map[uuid.UUID]bool{}
	for _, w := range awaiting {
		if w.VehicleID == relayCar || w.VehicleID == plainCar {
			seen[w.VehicleID] = w.Blocking
		}
	}
	if len(seen) != 2 || !seen[relayCar] || seen[plainCar] {
		t.Fatalf("fila da central = %+v", seen)
	}

	page := "/api/public/partner/" + carlos.ReportToken
	_, out := env.do("", http.MethodPost, page+"/session", map[string]any{"password": "senha-do-carlos-1"})
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
	var q appointments.PartnerQueue
	_ = json.Unmarshal(call(http.MethodGet, "/installations", nil, http.StatusOK), &q)
	queued := map[uuid.UUID]bool{}
	for _, v := range q.AwaitingInstall {
		queued[v.VehicleID] = v.Blocking
	}
	if len(queued) != 2 || !queued[relayCar] || queued[plainCar] {
		t.Fatalf("fila do representante = %+v", queued)
	}
	// Agendada, o agendamento e o link do instalador mostram.
	var a appointments.Appointment
	_ = json.Unmarshal(call(http.MethodPost, "/installations", map[string]any{
		"kind": "install", "vehicleId": relayCar, "scheduledFor": time.Now().Add(48 * time.Hour).Truncate(time.Minute),
	}, http.StatusCreated), &a)
	if !a.Blocking {
		t.Errorf("agendamento = %+v", a)
	}
	var view struct {
		Blocking bool `json:"blocking"`
	}
	token := a.URL[len("https://painel.farbo.test/instalador/"):]
	_ = json.Unmarshal(env.must("", http.MethodGet, "/api/public/install/"+token, nil, http.StatusOK), &view)
	if !view.Blocking {
		t.Errorf("link do instalador = %+v", view)
	}

	// Pela página, ele muda o preço com bloqueio; sem ele, o link não oferece.
	var mine orders.InstallPrices
	_ = json.Unmarshal(call(http.MethodPut, "/install-prices", map[string]any{"installCents": 11000, "uninstallCents": 8500,
		"blockingCents": 20000}, http.StatusOK), &mine)
	if mine.BlockingCents == nil || *mine.BlockingCents != 20000 || mine.InstallCents != 11000 {
		t.Errorf("valores do representante = %+v", mine)
	}
	if o := offer("?ref=carlos"); o.Install == nil || o.Install.BlockingCents != 20000 || o.Install.PriceCents != 11000 {
		t.Errorf("oferta com os valores dele = %+v", o.Install)
	}
	call(http.MethodPut, "/install-prices", map[string]any{"installCents": 11000, "uninstallCents": 8500}, http.StatusOK)
	if o := offer("?ref=carlos"); o.Install == nil || o.Install.BlockingCents != 0 {
		t.Errorf("oferta sem a escolha = %+v", o.Install)
	}
	// A central edita: o preço com bloqueio volta.
	var edited affiliates.Affiliate
	_ = json.Unmarshal(env.must(admin, http.MethodPatch, "/api/affiliates/"+carlos.ID.String(), map[string]any{
		"name": "Carlos Representante", "code": "carlos", "active": true, "manageInstalls": true,
		"installPrices": map[string]any{"mode": "direct", "installCents": 10000, "uninstallCents": 8500, "blockingCents": 18000},
	}, http.StatusOK), &edited)
	if p := edited.InstallPrices; p == nil || p.BlockingCents == nil || *p.BlockingCents != 18000 {
		t.Errorf("edição = %+v", edited.InstallPrices)
	}
}
