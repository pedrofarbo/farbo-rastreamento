package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/affiliates"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/vehicles"
)

// O cliente que veio por um representante segue as condições dele em toda
// contratação, não só pelo app: no Novo veículo da central (a tabela pelo
// tipo, a instalação com bloqueio no preço dele e o anual dele, mesmo com
// outro valor mandado), no catálogo que a central vê, na frota por planilha
// (cada veículo pelo tipo) e na reativação; com o representante pausado, o
// semestral e o anual seguem; o plano que a central define para a conta
// vale acima dele. Precisa de FARBO_TEST_DATABASE_URL.
func TestRepresentativeConditionsEverywhere(t *testing.T) {
	env, _ := newLeadsEnv(t)
	ctx := context.Background()
	admin := env.login(auth.RoleAdmin + "@leads.test")
	table := vehicles.KindPrices{MotorcycleCents: 3990, CarCents: 4990, TruckCents: 5990}
	var rui affiliates.Affiliate
	_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/affiliates", map[string]any{
		"name": "Rui Representante", "code": "rui", "active": true, "planPrices": table,
		"installPrices": map[string]any{"mode": "custom", "installCents": 9000, "uninstallCents": 5000, "blockingCents": 16000},
		"recurrence":    map[string]any{"enabled": true, "semiannualCents": 25000, "annualCents": 45000},
	}, http.StatusCreated), &rui)

	var bia struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/customers", map[string]any{
		"name": "Bia", "email": "bia@rui.test", "phone": "(11) 97777-0000", "document": "", "password": userPassword,
	}, http.StatusCreated), &bia)
	env.must(env.login("bia@rui.test"), http.MethodPut, "/api/me/address", map[string]any{
		"zipCode": "01305-000", "street": "Rua Augusta", "number": "500", "district": "Consolação", "city": "São Paulo", "state": "SP",
	}, http.StatusOK)
	if _, err := env.db.Exec(ctx, `INSERT INTO affiliate_referrals (customer_id, affiliate_id, source) VALUES ($1, $2, 'admin')`,
		bia.ID, rui.ID); err != nil {
		t.Fatal(err)
	}

	type catalogView struct {
		PlanPriceCents int                  `json:"planPriceCents"`
		PlanByKind     *vehicles.KindPrices `json:"planByKind"`
		Representative string               `json:"representative"`
		Periods        []periodOffer        `json:"periods"`
		Install        *struct {
			PriceCents    int `json:"priceCents"`
			BlockingCents int `json:"blockingCents"`
		} `json:"install"`
	}
	catalog := func() catalogView {
		t.Helper()
		var c catalogView
		_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/customers/"+bia.ID+"/catalog", nil, http.StatusOK), &c)
		return c
	}
	// A central vê o que a Bia veria: as condições do Rui.
	if c := catalog(); c.Representative != "Rui Representante" || c.PlanByKind == nil || *c.PlanByKind != table ||
		len(c.Periods) != 2 || c.Install == nil || c.Install.PriceCents != 9000 || c.Install.BlockingCents != 16000 {
		t.Fatalf("catálogo da Bia na central = %+v", c)
	}

	type placed struct {
		Subscription   billing.Subscription `json:"subscription"`
		InstallInvoice *billing.Invoice     `json:"installInvoice"`
	}
	order := func(body map[string]any) placed {
		t.Helper()
		var p placed
		_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/customers/"+bia.ID+"/trackers", body, http.StatusCreated), &p)
		return p
	}
	// A moto pela central, com outro valor mandado: sai na tabela do Rui, com
	// a parte do rastreador somada e a instalação com bloqueio no preço dele.
	moto := order(map[string]any{
		"vehicle": map[string]any{"name": "Moto da Bia", "plate": "BIA1A11", "kind": vehicles.KindMotorcycle},
		"plan":    map[string]any{"planName": "Plano Qualquer", "priceCents": 1000, "dueDay": 10},
		"tracker": map[string]any{"cents": 0, "months": 0}, "farboInstall": true, "installBlocking": true,
	})
	if s := moto.Subscription; s.PriceCents != 3990 || s.PlanName != "Plano Mensal" || !s.TrackerAdded || s.TrackerCents == 0 ||
		moto.InstallInvoice == nil || moto.InstallInvoice.AmountCents != 16000 {
		t.Fatalf("moto da Bia pela central = %+v / %+v", s, moto.InstallInvoice)
	}
	var farbo, blocking bool
	_ = env.db.QueryRow(ctx, `SELECT farbo_install, install_blocking FROM subscriptions WHERE id = $1`, moto.Subscription.ID).
		Scan(&farbo, &blocking)
	if !farbo || !blocking {
		t.Fatalf("moto da Bia: Farbo instala %v, bloqueio %v", farbo, blocking)
	}
	// O carro no anual do Rui.
	carro := order(map[string]any{
		"vehicle": map[string]any{"name": "Carro da Bia", "plate": "BIA2B22", "kind": vehicles.KindCar},
		"plan":    map[string]any{"planName": "Plano Qualquer", "priceCents": 1000, "dueDay": 10}, "periodMonths": 12,
	})
	if s := carro.Subscription; s.PriceCents != 45000 || s.PeriodMonths != 12 || s.TrackerCents != 0 {
		t.Fatalf("carro da Bia no anual pela central = %+v", s)
	}

	// A frota por planilha: cada veículo pela tabela do tipo dele.
	var imported struct {
		Vehicles int `json:"vehicles"`
	}
	_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/customers/"+bia.ID+"/trackers/import", map[string]any{
		"rows": []map[string]any{
			{"vehicle": map[string]any{"name": "Caminhão 1", "plate": "BIA3C33", "kind": vehicles.KindTruck}},
			{"vehicle": map[string]any{"name": "Moto 2", "plate": "BIA4D44", "kind": vehicles.KindMotorcycle}},
		},
		"plan": map[string]any{"planName": "Plano Qualquer", "priceCents": 1000, "dueDay": 10}, "tracker": map[string]any{"cents": 0, "months": 0},
	}, http.StatusCreated), &imported)
	priceOf := func(plate string) (int, bool) {
		t.Helper()
		var cents int
		var added bool
		if err := env.db.QueryRow(ctx, `SELECT s.price_cents, s.tracker_added FROM subscriptions s JOIN vehicles v ON v.id = s.vehicle_id
			WHERE v.plate = $1 AND s.status = 'ACTIVE'`, plate).Scan(&cents, &added); err != nil {
			t.Fatal(err)
		}
		return cents, added
	}
	if cents, added := priceOf("BIA3C33"); imported.Vehicles != 2 || cents != 5990 || !added {
		t.Errorf("caminhão da planilha = %d (parte somada %v)", cents, added)
	}
	if cents, _ := priceOf("BIA4D44"); cents != 3990 {
		t.Errorf("moto da planilha = %d", cents)
	}

	// A reativação: a mensalidade do Rui para o tipo do veículo.
	if _, err := env.db.Exec(ctx, `UPDATE subscriptions SET status = 'CANCELED' WHERE id = $1`, moto.Subscription.ID); err != nil {
		t.Fatal(err)
	}
	var reactivated billing.Subscription
	_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/customers/"+bia.ID+"/vehicles/"+moto.Subscription.VehicleID.String()+"/subscription",
		map[string]any{"planName": "Plano Qualquer", "priceCents": 1000, "dueDay": 10}, http.StatusCreated), &reactivated)
	if reactivated.PriceCents != 3990 || reactivated.PlanName != "Plano Mensal" {
		t.Errorf("reativação da moto da Bia = %+v", reactivated)
	}

	// O Rui pausado: o semestral e o anual seguem com quem ele já indicou.
	env.must(admin, http.MethodPatch, "/api/affiliates/"+rui.ID.String(), map[string]any{
		"name": "Rui Representante", "code": "rui", "active": false, "planPrices": table,
		"installPrices": map[string]any{"mode": "custom", "installCents": 9000, "uninstallCents": 5000, "blockingCents": 16000},
		"recurrence":    map[string]any{"enabled": true, "semiannualCents": 25000, "annualCents": 45000},
	}, http.StatusOK)
	if c := catalog(); len(c.Periods) != 2 || c.PlanByKind == nil {
		t.Errorf("catálogo da Bia com o Rui pausado = %+v", c)
	}

	// O plano da conta, definido pela central, vale acima do representante.
	env.must(admin, http.MethodPut, "/api/customers/"+bia.ID+"/plan",
		map[string]any{"planName": "Plano Frota", "priceCents": 4500, "dueDay": 10}, http.StatusOK)
	if c := catalog(); c.Representative != "" || c.PlanByKind != nil {
		t.Errorf("catálogo da Bia com o plano da conta = %+v", c)
	}
	van := order(map[string]any{
		"vehicle": map[string]any{"name": "Van da Bia", "plate": "BIA5E55", "kind": vehicles.KindCar},
		"plan":    map[string]any{"planName": "Plano Frota", "priceCents": 4500, "dueDay": 10}, "tracker": map[string]any{"cents": 0, "months": 0},
	})
	if van.Subscription.PriceCents != 4500 || van.Subscription.PlanName != "Plano Frota" {
		t.Errorf("van da Bia com o plano da conta = %+v", van.Subscription)
	}
}
