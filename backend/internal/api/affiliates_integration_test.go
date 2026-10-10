package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/affiliates"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/finance"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/leads"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/qrcode"
)

// O programa de afiliados de ponta a ponta: o admin cria os links; quem se
// cadastra por eles fica com o afiliado e, ao virar cliente, rende a
// comissão em cada mês pago (uma por veículo, não por cliente); o
// fechamento vira conta a pagar na Empresa, e pagar a conta paga as
// comissões na página do afiliado, que também dá o QR Code do link.
// Precisa de FARBO_TEST_DATABASE_URL.
func TestAffiliatesEndToEnd(t *testing.T) {
	env, _ := newLeadsEnv(t)
	ctx := context.Background()
	db := env.db

	env.must("", http.MethodGet, "/api/affiliates", nil, http.StatusUnauthorized)
	env.must(env.login(auth.RoleOperator+"@leads.test"), http.MethodGet, "/api/affiliates", nil, http.StatusForbidden)
	admin := env.login(auth.RoleAdmin + "@leads.test")
	call := func(method, path string, body any, want int, out any) {
		t.Helper()
		raw := env.must(admin, method, path, body, want)
		if out != nil {
			if err := json.Unmarshal(raw, out); err != nil {
				t.Fatalf("%s %s: %v (%s)", method, path, err, raw)
			}
		}
	}

	// O valor padrão é R$ 6.
	var settings affiliates.Settings
	call(http.MethodGet, "/api/affiliates/settings", nil, http.StatusOK, &settings)
	if settings.DefaultCommissionCents != 600 || settings.CategoryID == nil {
		t.Fatalf("configuração inicial = %+v", settings)
	}

	// Os links: o código sai do @; repetir o código é recusado.
	var joao, maria affiliates.Affiliate
	call(http.MethodPost, "/api/affiliates", map[string]any{"name": "João Motoca", "handle": "@joao.moto", "pixKey": "joao@pix.test", "active": true},
		http.StatusCreated, &joao)
	if joao.Code != "joao-moto" || joao.Handle != "joao.moto" || joao.CommissionCents != 600 || len(joao.ReportToken) < 20 {
		t.Fatalf("joão = %+v", joao)
	}
	env.must(admin, http.MethodPost, "/api/affiliates", map[string]any{"name": "Outro", "code": "Joao Moto", "active": true}, http.StatusBadRequest)
	env.must(admin, http.MethodPost, "/api/affiliates", map[string]any{"name": "Sem valor", "commissionCents": -1, "active": true}, http.StatusBadRequest)
	call(http.MethodPost, "/api/affiliates", map[string]any{"name": "Maria Influencer", "code": "maria", "commissionCents": 1000, "active": true},
		http.StatusCreated, &maria)

	// A tela do link mostra quem indica; código desconhecido, 404.
	var public affiliates.Public
	_ = json.Unmarshal(env.must("", http.MethodGet, "/api/public/affiliates/JOAO-MOTO", nil, http.StatusOK), &public)
	if public.Handle != "joao.moto" || public.Name != "João Motoca" {
		t.Errorf("público = %+v", public)
	}
	env.must("", http.MethodGet, "/api/public/affiliates/ninguem", nil, http.StatusNotFound)

	// Os cadastros pelos links. O afiliado é o primeiro que trouxe a pessoa.
	launch := func(email, ref string) {
		t.Helper()
		env.must("", http.MethodPost, "/api/public/launch",
			map[string]any{"email": email, "phone": "(11) 95555-4444", "consent": true, "ref": ref}, http.StatusCreated)
	}
	launch("ana@indicada.test", "joao-moto")
	launch("ANA@indicada.test", "maria")
	launch("sem@link.test", "")
	launch("link@quebrado.test", "nao-existe")
	env.must("", http.MethodPost, "/api/public/leads", map[string]any{
		"name": "Bia Lima", "email": "bia@indicada.test", "phone": "(11) 94444-3333", "plan": "mensal",
		"vehicleType": "moto", "vehicleCount": 1, "consent": true, "ref": "maria",
	}, http.StatusCreated)

	var waitlist []*leads.WaitlistEntry
	call(http.MethodGet, "/api/leads/waitlist", nil, http.StatusOK, &waitlist)
	referrer := map[string]string{}
	for _, e := range waitlist {
		referrer[e.Email] = e.Referrer
	}
	if referrer["ana@indicada.test"] != "@joao.moto" || referrer["sem@link.test"] != "" || referrer["link@quebrado.test"] != "" {
		t.Errorf("indicações na lista = %v", referrer)
	}
	// Todos viram pré-clientes; a Bia, pelo pré-cadastro, a Ana, pela lista.
	var leadList []*leads.Lead
	call(http.MethodGet, "/api/leads", nil, http.StatusOK, &leadList)
	byEmail := map[string]*leads.Lead{}
	for _, l := range leadList {
		byEmail[l.Email] = l
	}
	if l := byEmail["bia@indicada.test"]; l == nil || l.Referrer != "Maria Influencer" || l.AffiliateID == nil || l.Source != "landing" {
		t.Fatalf("pré-cliente bia = %+v", l)
	}
	if l := byEmail["ana@indicada.test"]; l == nil || l.Referrer != "@joao.moto" || l.Source != "indicacao" || !l.OnLaunchList {
		t.Fatalf("pré-cliente ana = %+v", l)
	}
	leadList = []*leads.Lead{byEmail["bia@indicada.test"]}

	// Os clientes: pelo e-mail na lista (Ana), pelo pré-cadastro (Bia), sem
	// indicação (Caio).
	type detail struct {
		ID        string               `json:"id"`
		Affiliate *affiliates.Referral `json:"affiliate"`
	}
	customer := func(name, email string, lead any) detail {
		t.Helper()
		var d detail
		call(http.MethodPost, "/api/customers", map[string]any{
			"name": name, "email": email, "password": "senha-do-cliente-123", "leadId": lead,
		}, http.StatusCreated, &d)
		return d
	}
	ana := customer("Ana", "ana@indicada.test", nil)
	bia := customer("Bia", "bia@indicada.test", leadList[0].ID)
	caio := customer("Caio", "caio@sem.test", nil)
	if ana.Affiliate == nil || ana.Affiliate.Handle != "joao.moto" || ana.Affiliate.Source != "waitlist" {
		t.Errorf("ana = %+v", ana.Affiliate)
	}
	if bia.Affiliate == nil || bia.Affiliate.Name != "Maria Influencer" || bia.Affiliate.Source != "lead" {
		t.Errorf("bia = %+v", bia.Affiliate)
	}
	if caio.Affiliate != nil {
		t.Errorf("caio = %+v", caio.Affiliate)
	}

	// As mensalidades. Ana tem dois veículos (duas comissões no mês: é por
	// veículo, não por cliente), uma mensalidade de antes da indicação e a
	// fatura do equipamento (que não rendem).
	brt, _ := time.LoadLocation("America/Sao_Paulo")
	now := time.Now().In(brt)
	cur := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	next, later, before := cur.AddDate(0, 1, 0), cur.AddDate(0, 2, 0), cur.AddDate(0, -1, 0)
	subscription := func(customerID string) string {
		t.Helper()
		var id string
		if err := db.QueryRow(ctx, `
			INSERT INTO subscriptions (customer_id, plan_name, price_cents, due_day, next_due_date)
			VALUES ($1, 'Plano Mensal', 6990, 10, $2) RETURNING id`, customerID, later).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	invoice := func(customerID string, sub any, due time.Time, paid bool) string {
		t.Helper()
		var id string
		if err := db.QueryRow(ctx, `
			INSERT INTO invoices (customer_id, subscription_id, description, amount_cents, due_date, status, paid_at, paid_via)
			VALUES ($1, $2, 'Mensalidade', 6990, $3,
				CASE WHEN $4 THEN 'PAID' ELSE 'OPEN' END, CASE WHEN $4 THEN NOW() END, CASE WHEN $4 THEN 'MANUAL' ELSE '' END)
			RETURNING id`, customerID, sub, due.AddDate(0, 0, 9), paid).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	anaCar, anaBike, biaBike, caioCar := subscription(ana.ID), subscription(ana.ID), subscription(bia.ID), subscription(caio.ID)
	invoice(ana.ID, anaCar, before, true)
	invoice(ana.ID, anaCar, cur, true)
	invoice(ana.ID, anaBike, cur, true)
	invoice(ana.ID, nil, cur, true) // o equipamento
	anaNext := invoice(ana.ID, anaCar, next, false)
	biaCur := invoice(bia.ID, biaBike, cur, true)
	invoice(caio.ID, caioCar, cur, true)

	byID := func() map[string]*affiliates.Affiliate {
		t.Helper()
		var list []*affiliates.Affiliate
		call(http.MethodGet, "/api/affiliates", nil, http.StatusOK, &list)
		out := map[string]*affiliates.Affiliate{}
		for _, a := range list {
			out[a.ID.String()] = a
		}
		return out
	}
	list := byID()
	if a := list[joao.ID.String()]; a.Signups != 1 || a.Customers != 1 || a.ActiveCustomers != 1 || a.ActiveVehicles != 2 ||
		a.PendingCents != 1200 {
		t.Errorf("joão = %+v", a)
	}
	if a := list[maria.ID.String()]; a.Signups != 1 || a.Customers != 1 || a.PendingCents != 1000 {
		t.Errorf("maria = %+v", a)
	}

	// O valor novo do João vale para o mês que ainda vai ser pago.
	call(http.MethodPatch, "/api/affiliates/"+joao.ID.String(), map[string]any{
		"name": "João Motoca", "handle": "joao.moto", "code": "joao-moto", "commissionCents": 800, "pixKey": "joao@pix.test", "active": true,
	}, http.StatusOK, nil)
	env.must(admin, http.MethodPost, "/api/invoices/"+anaNext+"/pay", nil, http.StatusOK)
	// A mensalidade da Bia foi estornada: a comissão (ainda aberta) sai.
	if _, err := db.Exec(ctx, `UPDATE invoices SET status = 'OPEN', paid_at = NULL, paid_via = '' WHERE id = $1`, biaCur); err != nil {
		t.Fatal(err)
	}
	list = byID()
	if a := list[joao.ID.String()]; a.PendingCents != 2000 {
		t.Errorf("joão depois do valor novo = %+v", a)
	}
	if a := list[maria.ID.String()]; a.PendingCents != 0 {
		t.Errorf("maria depois do estorno = %+v", a)
	}

	// O fechamento do mês: só o que é até ele.
	month := cur.Format("2006-01")
	var preview []affiliates.ClosingLine
	call(http.MethodGet, "/api/affiliates/closing?month="+month, nil, http.StatusOK, &preview)
	if len(preview) != 1 || preview[0].AffiliateID != joao.ID || preview[0].AmountCents != 1200 || preview[0].Commissions != 2 {
		t.Fatalf("prévia = %+v", preview)
	}
	env.must(admin, http.MethodGet, "/api/affiliates/closing?month=outubro", nil, http.StatusBadRequest)
	due := cur.AddDate(0, 1, 4).Format(time.DateOnly)
	var payouts []*affiliates.Payout
	call(http.MethodPost, "/api/affiliates/closing", map[string]any{"month": month, "dueDate": due}, http.StatusCreated, &payouts)
	if len(payouts) != 1 || payouts[0].AmountCents != 1200 || payouts[0].Status != "OPEN" || payouts[0].EntryID == nil {
		t.Fatalf("fechamento = %+v", payouts)
	}
	env.must(admin, http.MethodPost, "/api/affiliates/closing", map[string]any{"month": month, "dueDate": due}, http.StatusBadRequest)

	// A conta a pagar na Empresa, com o afiliado de fornecedor.
	var entries []finance.Entry
	call(http.MethodGet, "/api/finance/entries?kind=PAYABLE", nil, http.StatusOK, &entries)
	if len(entries) != 1 || entries[0].AmountCents != 1200 || entries[0].DueDate.Format(time.DateOnly) != due ||
		!strings.Contains(entries[0].Description, "João Motoca") || !strings.Contains(entries[0].Notes, "joao@pix.test") {
		t.Fatalf("conta a pagar = %+v", entries)
	}
	var suppliers []finance.Supplier
	call(http.MethodGet, "/api/finance/suppliers", nil, http.StatusOK, &suppliers)
	if len(suppliers) != 1 || suppliers[0].Name != "João Motoca" || suppliers[0].PixKey != "joao@pix.test" {
		t.Errorf("fornecedores = %+v", suppliers)
	}

	// A página do João: os números, sem dado de ninguém.
	report := func(token string, want int) (*affiliates.Report, string) {
		t.Helper()
		raw := env.must("", http.MethodGet, "/api/public/partner/"+token, nil, want)
		var r affiliates.Report
		_ = json.Unmarshal(raw, &r)
		return &r, string(raw)
	}
	r, raw := report(joao.ReportToken, http.StatusOK)
	if r.ToReceiveCents != 2000 || r.PaidCents != 0 || r.Customers != 1 || r.ActiveCustomers != 1 || r.ActiveVehicles != 2 ||
		r.Signups != 1 || len(r.Months) != 2 || r.Months[0].Status != "pending" || r.Months[0].AmountCents != 800 ||
		r.Months[0].Vehicles != 1 || r.Months[1].Status != "closed" || r.Months[1].Vehicles != 2 || r.Months[1].AmountCents != 1200 {
		t.Errorf("página do joão = %s", raw)
	}
	for _, secret := range []string{"ana@", "Ana", "joao@pix.test", ana.ID} {
		if strings.Contains(raw, secret) {
			t.Errorf("a página do afiliado mostra %q: %s", secret, raw)
		}
	}
	report("token-que-nao-existe-de-jeito-nenhum", http.StatusNotFound)

	// O QR Code do link de indicação, pela página do João, para o flyer.
	svg := env.must("", http.MethodGet, "/api/public/partner/"+joao.ReportToken+"/qr", nil, http.StatusOK)
	if want, _ := qrcode.SVG("https://farborastreadores.com.br/indicacao/joao-moto"); !bytes.Equal(svg, want) {
		t.Errorf("o QR (SVG) não é o do link de indicação do João")
	}
	png := env.must("", http.MethodGet, "/api/public/partner/"+joao.ReportToken+"/qr?format=png&download=1", nil, http.StatusOK)
	if want, _ := qrcode.PNG("https://farborastreadores.com.br/indicacao/joao-moto", 20); !bytes.Equal(png, want) {
		t.Errorf("o QR (PNG) não é o do link de indicação do João")
	}
	// Baixar vem como arquivo, com o nome; sem o download=1, para mostrar.
	for query, want := range map[string]string{
		"?format=png&download=1": `attachment; filename=qrcode-indicacao-joao-moto.png`,
		"?format=svg&download=1": `attachment; filename=qrcode-indicacao-joao-moto.svg`,
		"?format=png":            `inline; filename=qrcode-indicacao-joao-moto.png`,
	} {
		resp, err := http.Get(env.srv.URL + "/api/public/partner/" + joao.ReportToken + "/qr" + query)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if got := resp.Header.Get("Content-Disposition"); got != want {
			t.Errorf("QR %s: Content-Disposition = %q", query, got)
		}
	}
	env.must("", http.MethodGet, "/api/public/partner/token-que-nao-existe-de-jeito-nenhum/qr", nil, http.StatusNotFound)

	// Pagar a conta paga as comissões; a paga não se desfaz.
	env.must(admin, http.MethodPost, "/api/finance/entries/"+payouts[0].EntryID.String()+"/pay", map[string]any{}, http.StatusOK)
	r, raw = report(joao.ReportToken, http.StatusOK)
	if r.PaidCents != 1200 || r.ToReceiveCents != 800 || r.Months[1].Status != "paid" {
		t.Errorf("página depois de pagar = %s", raw)
	}
	env.must(admin, http.MethodDelete, "/api/affiliates/payouts/"+payouts[0].ID.String(), nil, http.StatusBadRequest)

	// O fechamento seguinte, desfeito: a conta sai e as comissões voltam.
	call(http.MethodPost, "/api/affiliates/closing", map[string]any{"month": next.Format("2006-01"), "dueDate": due}, http.StatusCreated, &payouts)
	if len(payouts) != 1 || payouts[0].AmountCents != 800 {
		t.Fatalf("segundo fechamento = %+v", payouts)
	}
	env.must(admin, http.MethodDelete, "/api/affiliates/payouts/"+payouts[0].ID.String(), nil, http.StatusNoContent)
	call(http.MethodGet, "/api/finance/entries?kind=PAYABLE&status=all", nil, http.StatusOK, &entries)
	if len(entries) != 1 || entries[0].Status != "PAID" {
		t.Errorf("a conta do fechamento desfeito ficou: %+v", entries)
	}
	call(http.MethodGet, "/api/affiliates/payouts", nil, http.StatusOK, &payouts)
	if len(payouts) != 1 || payouts[0].Status != "PAID" {
		t.Errorf("fechamentos = %+v", payouts)
	}
	if a := byID()[joao.ID.String()]; a.PendingCents != 800 || a.PaidCents != 1200 || a.OpenCents != 0 {
		t.Errorf("joão depois de desfazer = %+v", a)
	}

	// Link novo da página: o antigo para de abrir.
	var renewed affiliates.Affiliate
	call(http.MethodPost, "/api/affiliates/"+joao.ID.String()+"/report-token", nil, http.StatusOK, &renewed)
	report(joao.ReportToken, http.StatusNotFound)
	report(renewed.ReportToken, http.StatusOK)

	// Inativo: o link para de indicar e os meses novos não rendem.
	call(http.MethodPatch, "/api/affiliates/"+joao.ID.String(), map[string]any{
		"name": "João Motoca", "handle": "joao.moto", "code": "joao-moto", "active": false,
	}, http.StatusOK, nil)
	env.must("", http.MethodGet, "/api/public/affiliates/joao-moto", nil, http.StatusNotFound)
	// Pausado, sem QR (o link não indica mais).
	env.must("", http.MethodGet, "/api/public/partner/"+renewed.ReportToken+"/qr", nil, http.StatusNotFound)
	launch("depois@inativo.test", "joao-moto")
	call(http.MethodGet, "/api/leads/waitlist", nil, http.StatusOK, &waitlist)
	for _, e := range waitlist {
		if e.Email == "depois@inativo.test" && e.AffiliateID != nil {
			t.Errorf("link inativo indicou: %+v", e)
		}
	}
	invoice(ana.ID, anaCar, later, true)
	if a := byID()[joao.ID.String()]; a.PendingCents != 800 || a.CommissionCents != 800 {
		t.Errorf("joão inativo = %+v", a)
	}

	// O admin diz quem indicou o Caio (e tira); admin não é cliente.
	var set struct {
		Affiliate *affiliates.Referral `json:"affiliate"`
	}
	call(http.MethodPut, "/api/customers/"+caio.ID+"/affiliate", map[string]any{"affiliateId": maria.ID}, http.StatusOK, &set)
	if set.Affiliate == nil || set.Affiliate.Source != "admin" || set.Affiliate.AffiliateID != maria.ID {
		t.Errorf("caio indicado = %+v", set.Affiliate)
	}
	if a := byID()[maria.ID.String()]; a.Customers != 2 {
		t.Errorf("maria com o caio = %+v", a)
	}
	call(http.MethodPut, "/api/customers/"+caio.ID+"/affiliate", map[string]any{"affiliateId": nil}, http.StatusOK, &set)
	if set.Affiliate != nil {
		t.Errorf("caio sem indicação = %+v", set.Affiliate)
	}
	var adminID string
	if err := db.QueryRow(ctx, `SELECT id FROM users WHERE role = 'admin' LIMIT 1`).Scan(&adminID); err != nil {
		t.Fatal(err)
	}
	env.must(admin, http.MethodPut, "/api/customers/"+adminID+"/affiliate", map[string]any{"affiliateId": maria.ID}, http.StatusBadRequest)

	// O valor padrão novo, para todos.
	call(http.MethodPut, "/api/affiliates/settings", map[string]any{"defaultCommissionCents": 700, "applyToAll": true}, http.StatusOK, &settings)
	if settings.DefaultCommissionCents != 700 || byID()[maria.ID.String()].CommissionCents != 700 {
		t.Errorf("valor padrão = %+v", settings)
	}
}

func TestAffiliateInput(t *testing.T) {
	cents := 600
	in, err := affiliates.Input{Name: "  Fulano   da Silva ", Handle: " @Fulano.Moto ", CommissionCents: &cents}.Normalize()
	if err != nil || in.Name != "Fulano da Silva" || in.Handle != "Fulano.Moto" || in.Code != "fulano-moto" {
		t.Fatalf("%+v %v", in, err)
	}
	in, _ = affiliates.Input{Name: "Ação Rápida Motos"}.Normalize()
	if in.Code != "acao-rapida-motos" {
		t.Errorf("código = %q", in.Code)
	}
	for _, bad := range []affiliates.Input{
		{Name: ""},
		{Name: "X", Handle: "com espaço"},
		{Name: "!"},
		{Name: "Ok", Email: "não-é-email"},
	} {
		if _, err := bad.Normalize(); err == nil {
			t.Errorf("aceitou %+v", bad)
		}
	}
	if got := affiliates.Code(strings.Repeat("abc-", 20)); len(got) > 40 || strings.HasSuffix(got, "-") {
		t.Errorf("código longo = %q", got)
	}
}
