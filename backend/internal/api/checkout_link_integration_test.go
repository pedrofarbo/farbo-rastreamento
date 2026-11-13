package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/contract"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/orders"
)

// O link de pagamento: a central gera um link com uma oferta fechada; quem
// abre /contratar?link= vê só ela (também com a contratação pelo site
// fechada) e contrata nela, mesmo mandando outras escolhas; o representante
// guardado no aparelho não muda a oferta, mas a conta fica com ele; o cupom
// não vai junto; o link de um uso só
// recusa o segundo; o vencido e o desconhecido dizem o motivo; o próximo
// veículo do cliente sai no preço normal; o link usado não sai (só
// desligado). Precisa de FARBO_TEST_DATABASE_URL.
func TestCheckoutLinks(t *testing.T) {
	var bs *billing.Service
	env, _ := newLeadsEnv(t, func(d *Deps) {
		bs = d.Billing
		d.Config.Catalog.SetupDueDays = 0
		d.Orders = orders.NewService(d.DB, d.Billing, d.Vehicles, d.Config.Catalog, d.Log)
		doc, err := contract.Render(contract.Params{Company: config.Company{Name: "Farbo Rastreadores"}, HistoryOptions: []int{7, 14, 30}})
		if err != nil {
			t.Fatal(err)
		}
		svc := contract.NewService(d.DB, doc, &recordingContractMailer{}, d.Log)
		svc.SetSync()
		d.Contract = svc
	})
	ctx := context.Background()
	clock := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	bs.SetClock(func() time.Time { return clock })
	admin := env.login(auth.RoleAdmin + "@leads.test")
	env.must(admin, http.MethodPost, "/api/affiliates", map[string]any{"name": "Rita Representante", "code": "rita", "active": true},
		http.StatusCreated)
	var catalog struct {
		PlanPriceCents int `json:"planPriceCents"`
	}
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/catalog", nil, http.StatusOK), &catalog)
	// A contratação pelo site fechada: o link vale assim mesmo.
	if _, err := env.db.Exec(ctx, `UPDATE order_settings SET checkout_enabled = FALSE`); err != nil {
		t.Fatal(err)
	}

	// A central: os limites.
	for _, bad := range []map[string]any{
		{"label": " ", "equipmentMode": "sale", "priceCents": 4990, "active": true},
		{"label": "X", "equipmentMode": "buy", "priceCents": 4990, "active": true},
		{"label": "X", "equipmentMode": "sale", "periodMonths": 3, "priceCents": 4990, "active": true},
		{"label": "X", "equipmentMode": "sale", "priceCents": 50, "active": true},
		{"label": "X", "equipmentMode": "loan", "priceCents": 4990, "installCents": -1, "active": true},
		{"label": "X", "equipmentMode": "sale", "priceCents": 4990, "maxUses": 0, "active": true},
	} {
		env.must(admin, http.MethodPost, "/api/checkout-links", bad, http.StatusBadRequest)
	}
	create := func(in map[string]any) orders.CheckoutLink {
		t.Helper()
		var l orders.CheckoutLink
		_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/checkout-links", in, http.StatusCreated), &l)
		return l
	}
	// O comodato anual, com bloqueio, de um uso só.
	frota := create(map[string]any{"label": " Frota João ", "equipmentMode": "loan", "periodMonths": 12, "priceCents": 60000,
		"installCents": 0, "uninstallCents": 8500, "installBlocking": true, "maxUses": 1, "active": true})
	if len(frota.Token) != 14 || frota.Label != "Frota João" || !frota.FarboInstall || frota.Uses != 0 || !frota.InstallBlocking {
		t.Fatalf("link do comodato = %+v", frota)
	}
	// O incluso mensal por conta própria: a taxa e o bloqueio mandados não ficam.
	barato := create(map[string]any{"label": "Indicação do Zé", "equipmentMode": "sale", "priceCents": 4990,
		"farboInstall": false, "installCents": 5000, "uninstallCents": 3000, "installBlocking": true, "active": true})
	if barato.FarboInstall || barato.InstallCents != 0 || barato.UninstallCents != 0 || barato.InstallBlocking || barato.PeriodMonths != 1 {
		t.Fatalf("link do incluso = %+v", barato)
	}
	vencido := create(map[string]any{"label": "Feira", "equipmentMode": "sale", "priceCents": 5990, "farboInstall": true,
		"installCents": 12000, "validUntil": "2026-11-10", "active": true})
	if frota.Token == barato.Token || barato.Token == vencido.Token {
		t.Fatal("dois links com o mesmo código")
	}

	type offerView struct {
		Enabled         bool                      `json:"enabled"`
		PlanCents       int                       `json:"planCents"`
		ContractVersion string                    `json:"contractVersion"`
		Coupons         bool                      `json:"coupons"`
		EquipmentModes  string                    `json:"equipmentModes"`
		Loan            *orders.LoanOffer         `json:"loan"`
		Install         *orders.InstallService    `json:"install"`
		Tracker         orders.Tracker            `json:"tracker"`
		Periods         []periodOffer             `json:"periods"`
		Link            *orders.CheckoutLinkOffer `json:"link"`
		LinkProblem     string                    `json:"linkProblem"`
	}
	offer := func(query string) offerView {
		t.Helper()
		var o offerView
		_ = json.Unmarshal(env.must("", http.MethodGet, "/api/public/checkout?"+query, nil, http.StatusOK), &o)
		return o
	}
	if o := offer(""); o.Enabled || o.Link != nil {
		t.Fatalf("o site fechado = %+v", o)
	}
	o := offer("link=" + strings.ToUpper(frota.Token) + "&ref=rita")
	if !o.Enabled || o.Link == nil || o.Link.PeriodMonths != 12 || o.EquipmentModes != "loan" || o.Coupons || o.LinkProblem != "" ||
		len(o.Periods) != 1 || o.Periods[0].Months != 12 || o.Periods[0].Cents != 60000 || o.Periods[0].LoanCents != 60000 ||
		o.Loan == nil || o.Loan.SurchargeCents != 0 || o.Loan.UninstallFeeCents != 8500 || o.Loan.InstallFeeCents != 0 ||
		o.Install == nil || o.Install.PriceCents != 0 {
		t.Fatalf("oferta do link do comodato = %+v (link %+v)", o, o.Link)
	}
	if o := offer("link=" + barato.Token); o.PlanCents != 4990 || o.EquipmentModes != "sale" || o.Install != nil || o.Loan != nil ||
		len(o.Periods) != 0 || o.Tracker.Added {
		t.Fatalf("oferta do link do incluso = %+v", o)
	}
	if o := offer("link=" + vencido.Token); o.Link != nil || !strings.Contains(o.LinkProblem, "venceu em 10/11/2026") {
		t.Fatalf("oferta do link vencido = %+v", o)
	}
	if o := offer("link=naoexiste"); o.Link != nil || !strings.Contains(o.LinkProblem, "não encontrado") {
		t.Fatalf("oferta do link desconhecido = %+v", o)
	}

	address := map[string]any{"zipCode": "01305-000", "street": "Rua Augusta", "number": "500", "district": "Consolação",
		"city": "São Paulo", "state": "SP"}
	type checkoutResult struct {
		Subscription   billing.Subscription `json:"subscription"`
		FirstInvoice   *billing.Invoice     `json:"firstInvoice"`
		InstallInvoice *billing.Invoice     `json:"installInvoice"`
		Tokens         *struct {
			AccessToken string `json:"accessToken"`
		} `json:"tokens"`
	}
	// As escolhas mandadas (o incluso, o mensal, a instalação da Farbo, o
	// representante do aparelho) não contam: vale a oferta do link.
	body := func(email, document, link, coupon string) map[string]any {
		return map[string]any{
			"name": "Gil Souza", "email": email, "phone": "(11) 95555-0000", "document": document, "password": "senha-forte-1",
			"vehicle": map[string]any{"kind": "CAR", "brand": "Fiat", "model": "Uno", "plate": ""}, "address": address,
			"contractVersion": o.ContractVersion, "accept": true, "equipmentMode": "sale", "periodMonths": 1, "farboInstall": true,
			"link": link, "coupon": coupon, "ref": "rita",
		}
	}
	refused := func(email, document, link, coupon, want string) {
		t.Helper()
		status, out := env.do("", http.MethodPost, "/api/public/checkout", body(email, document, link, coupon))
		if status != http.StatusBadRequest || !strings.Contains(string(out), want) {
			t.Fatalf("contratação pelo link %s: %d %s", link, status, out)
		}
		var n int
		_ = env.db.QueryRow(ctx, `SELECT count(*) FROM users WHERE email = $1`, email).Scan(&n)
		if n != 0 {
			t.Fatalf("a contratação recusada deixou a conta %s", email)
		}
	}
	refused("cupom@link.test", "321.654.987-91", frota.Token, "BEMVINDO", "não vale junto com cupom")
	refused("vencido@link.test", "321.654.987-91", vencido.Token, "", "venceu em 10/11/2026")
	// Sem o link, com o site fechado: recusada.
	refused("site@link.test", "321.654.987-91", "", "", "fechada")

	var joao checkoutResult
	_ = json.Unmarshal(env.must("", http.MethodPost, "/api/public/checkout", body("joao@link.test", "321.654.987-91", frota.Token, ""),
		http.StatusCreated), &joao)
	sub := joao.Subscription
	if sub.PeriodMonths != 12 || sub.PriceCents != 60000 || sub.PlanName != orders.PeriodPlanName(12) ||
		sub.EquipmentMode != billing.EquipmentLoan || sub.UninstallFeeCents != 8500 || sub.TrackerCents != 0 ||
		joao.InstallInvoice != nil || joao.FirstInvoice == nil || joao.FirstInvoice.AmountCents != 60000 {
		t.Fatalf("contratação pelo link do comodato = %+v / %+v / %+v", sub, joao.FirstInvoice, joao.InstallInvoice)
	}
	var farbo, blocking bool
	var uses, referrals int
	_ = env.db.QueryRow(ctx, `SELECT farbo_install, install_blocking FROM subscriptions WHERE id = $1`, sub.ID).Scan(&farbo, &blocking)
	_ = env.db.QueryRow(ctx, `SELECT count(*) FROM checkout_link_uses WHERE link_id = $1 AND subscription_id = $2`, frota.ID, sub.ID).Scan(&uses)
	_ = env.db.QueryRow(ctx, `SELECT count(*) FROM affiliate_referrals WHERE customer_id = $1`, sub.CustomerID).Scan(&referrals)
	if !farbo || !blocking || uses != 1 || referrals != 1 {
		t.Fatalf("comodato pelo link: Farbo instala %v, bloqueio %v, usos %d, representante %d", farbo, blocking, uses, referrals)
	}
	// O link de um uso só: o segundo é recusado, e a oferta diz o motivo.
	refused("outro@link.test", "159.753.486-25", frota.Token, "", "já foi usado")
	if o := offer("link=" + frota.Token); o.Link != nil || !strings.Contains(o.LinkProblem, "já foi usado") {
		t.Fatalf("oferta do link usado = %+v", o)
	}

	// O incluso mensal por conta própria, sem representante: o valor do link,
	// a parte do rastreador já dentro dele.
	zeBody := body("ze@link.test", "159.753.486-25", barato.Token, "")
	delete(zeBody, "ref")
	var ze checkoutResult
	_ = json.Unmarshal(env.must("", http.MethodPost, "/api/public/checkout", zeBody, http.StatusCreated), &ze)
	if s := ze.Subscription; s.PriceCents != 4990 || s.PeriodMonths > 1 || s.EquipmentMode != billing.EquipmentSale || s.TrackerAdded ||
		ze.InstallInvoice != nil || ze.FirstInvoice == nil || ze.FirstInvoice.AmountCents != 4990 {
		t.Fatalf("contratação pelo link do incluso = %+v / %+v / %+v", s, ze.FirstInvoice, ze.InstallInvoice)
	}
	_ = env.db.QueryRow(ctx, `SELECT farbo_install FROM subscriptions WHERE id = $1`, ze.Subscription.ID).Scan(&farbo)
	if farbo {
		t.Fatal("o link por conta própria saiu com a instalação da Farbo")
	}
	// O próximo veículo do Zé: o preço normal, não o do link.
	if _, err := env.db.Exec(ctx, `UPDATE invoices SET status = 'PAID', paid_at = NOW(), paid_via = 'MANUAL'
		WHERE customer_id = $1`, ze.Subscription.CustomerID); err != nil {
		t.Fatal(err)
	}
	var zeCatalog struct {
		PlanPriceCents int `json:"planPriceCents"`
	}
	_ = json.Unmarshal(env.must(ze.Tokens.AccessToken, http.MethodGet, "/api/catalog", nil, http.StatusOK), &zeCatalog)
	if zeCatalog.PlanPriceCents != catalog.PlanPriceCents {
		t.Fatalf("mensalidade do veículo novo de quem contratou pelo link = %d (a normal é %d)", zeCatalog.PlanPriceCents, catalog.PlanPriceCents)
	}

	// Mais usos: o link volta a valer para a próxima contratação.
	frotaEdit := map[string]any{"label": "Frota João", "equipmentMode": "loan", "periodMonths": 12, "priceCents": 60000,
		"uninstallCents": 8500, "installBlocking": true, "maxUses": 2, "active": true}
	var edited orders.CheckoutLink
	_ = json.Unmarshal(env.must(admin, http.MethodPut, "/api/checkout-links/"+frota.ID.String(), frotaEdit, http.StatusOK), &edited)
	if edited.Token != frota.Token || edited.Uses != 1 || edited.MaxUses == nil || *edited.MaxUses != 2 {
		t.Fatalf("link editado = %+v", edited)
	}
	if o := offer("link=" + frota.Token); o.Link == nil {
		t.Fatalf("o link com mais usos = %+v", o)
	}
	frotaEdit["active"] = false
	env.must(admin, http.MethodPut, "/api/checkout-links/"+frota.ID.String(), frotaEdit, http.StatusOK)
	refused("desligado@link.test", "258.147.369-09", frota.Token, "", "não está mais valendo")

	// A lista com os usos; o usado não sai, o sem uso sai; o cliente não vê.
	var list []orders.CheckoutLink
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/checkout-links", nil, http.StatusOK), &list)
	usesOf := map[string]int{}
	for _, l := range list {
		usesOf[l.Label] = l.Uses
	}
	if len(list) != 3 || usesOf["Frota João"] != 1 || usesOf["Indicação do Zé"] != 1 || usesOf["Feira"] != 0 {
		t.Fatalf("lista dos links = %+v", list)
	}
	env.must(admin, http.MethodDelete, "/api/checkout-links/"+frota.ID.String(), nil, http.StatusConflict)
	env.must(admin, http.MethodDelete, "/api/checkout-links/"+vencido.ID.String(), nil, http.StatusNoContent)
	env.must(ze.Tokens.AccessToken, http.MethodGet, "/api/checkout-links", nil, http.StatusForbidden)
}
