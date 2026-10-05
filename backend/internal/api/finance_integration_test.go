package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/finance"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/mail"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/telemetry"
)

type fakeFinanceMailer struct {
	mu   sync.Mutex
	to   [][]string
	sent []mail.PayablesDue
	fail error
}

func (f *fakeFinanceMailer) PayablesDue(_ context.Context, to []string, d mail.PayablesDue) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	f.to, f.sent = append(f.to, to), append(f.sent, d)
	return nil
}

// A gestão da empresa de ponta a ponta, com o relógio em 15/10/2026: contas
// (parceladas, já pagas, com juros, reabertas, canceladas), receitas,
// anexos, recorrências, estoque com custo médio, e os números do caixa, da
// projeção e do resultado do mês conferidos centavo a centavo — com as
// faturas e as assinaturas dos clientes entrando sozinhas. Precisa de
// FARBO_TEST_DATABASE_URL.
func TestCompanyFinanceEndToEnd(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{
		HTTP: config.HTTP{RateLimitRPS: 1000, RateLimitBurst: 1000},
		Auth: config.Auth{
			JWTSecret:      []byte("segredo-de-teste-integracao-0123456789abcdef"),
			AccessTokenTTL: time.Hour, RefreshTokenTTL: time.Hour, BcryptCost: bcrypt.MinCost,
		},
	}
	authSvc := auth.NewService(auth.NewRepository(db), cfg.Auth, nil, log)
	for _, role := range []string{auth.RoleAdmin, auth.RoleOperator} {
		if _, err := authSvc.CreateUser(ctx, role+"@empresa.test", role, role, userPassword); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 10, 15, 15, 0, 0, 0, time.UTC) // 12h em Brasília
	mailer := &fakeFinanceMailer{}
	fs := finance.NewService(db, mailer, log)
	fs.SetClock(func() time.Time { return now })
	server := NewServer(Deps{
		Config: cfg, Log: log, Metrics: telemetry.NewMetrics(), DB: db, Auth: authSvc,
		Audit: audit.NewService(audit.NewRepository(db), log), Finance: fs,
	})
	srv := httptest.NewServer(server.Handler())
	t.Cleanup(srv.Close)
	env := &credEnv{t: t, db: db, srv: srv}

	env.must(env.login(auth.RoleOperator+"@empresa.test"), http.MethodGet, "/api/finance/overview", nil, http.StatusForbidden)
	env.must("", http.MethodGet, "/api/finance/entries?kind=PAYABLE", nil, http.StatusUnauthorized)
	admin := env.login(auth.RoleAdmin + "@empresa.test")
	call := func(method, path string, body any, want int, out any) {
		t.Helper()
		raw := env.must(admin, method, path, body, want)
		if out != nil {
			if err := json.Unmarshal(raw, out); err != nil {
				t.Fatalf("%s %s: %v (%s)", method, path, err, raw)
			}
		}
	}

	// As categorias iniciais.
	var categories []finance.Category
	call(http.MethodGet, "/api/finance/categories", nil, http.StatusOK, &categories)
	category := map[string]string{}
	for _, c := range categories {
		category[c.Name] = c.ID.String()
	}
	for _, name := range []string{"Impostos (DAS/Simples)", "Outras despesas", "Plano de dados dos chips",
		"Venda de equipamentos", "Compra de rastreadores e chips", "Aluguel e escritório", "Software e assinaturas"} {
		if category[name] == "" {
			t.Fatalf("categoria inicial %q não existe: %+v", name, categories)
		}
	}
	var vivo finance.Supplier
	call(http.MethodPost, "/api/finance/suppliers", map[string]any{"name": "Vivo Empresas", "document": "02.558.157/0001-62", "active": true},
		http.StatusCreated, &vivo)
	if vivo.Document != "02558157000162" {
		t.Errorf("documento = %q", vivo.Document)
	}
	call(http.MethodPut, "/api/finance/settings", map[string]any{"openingBalanceCents": 1000000, "openingDate": "2026-09-01"}, http.StatusOK, nil)

	create := func(body map[string]any, want int) []finance.Entry {
		t.Helper()
		var out []finance.Entry
		raw := env.must(admin, http.MethodPost, "/api/finance/entries", body, want)
		if want == http.StatusCreated {
			_ = json.Unmarshal(raw, &out)
		}
		return out
	}
	payable := func(desc, cat string, cents int, due string, extra map[string]any) []finance.Entry {
		t.Helper()
		body := map[string]any{"kind": "PAYABLE", "description": desc, "categoryId": category[cat], "amountCents": cents, "dueDate": due}
		for k, v := range extra {
			body[k] = v
		}
		return create(body, http.StatusCreated)
	}

	// Parcelado em 3: os centavos que sobram vão na primeira, e cada parcela
	// vence um mês depois (31/10 → 30/11 → 31/12).
	notebook := payable("Notebook", "Outras despesas", 100000, "2026-10-31", map[string]any{"installments": 3})
	if len(notebook) != 3 || notebook[0].AmountCents != 33334 || notebook[2].AmountCents != 33333 ||
		notebook[1].DueDate.String() != "2026-11-30" || notebook[2].DueDate.String() != "2026-12-31" ||
		*notebook[1].Installment != 2 || *notebook[1].Installments != 3 {
		t.Fatalf("parcelas = %+v", notebook)
	}
	das := payable("DAS de setembro", "Impostos (DAS/Simples)", 15000, "2026-10-10",
		map[string]any{"paidOn": "2026-10-10", "paymentMethod": "PIX"})[0]
	if das.Status != "PAID" || *das.PaidCents != 15000 {
		t.Fatalf("lançada já paga = %+v", das)
	}
	create(map[string]any{"kind": "PAYABLE", "description": "Errada", "categoryId": category["Venda de equipamentos"],
		"amountCents": 100, "dueDate": "2026-10-20"}, http.StatusBadRequest)
	plano := payable("Dados dos chips", "Plano de dados dos chips", 8000, "2026-10-15", map[string]any{"supplierId": vivo.ID})[0]
	contador := payable("Contador", "Outras despesas", 30000, "2026-10-18", nil)[0]
	internet := payable("Internet", "Outras despesas", 12000, "2026-10-05", nil)[0]
	if !internet.Overdue || plano.SupplierName != "Vivo Empresas" {
		t.Fatalf("vencida = %+v, fornecedor = %q", internet, plano.SupplierName)
	}

	// Baixa com juros, de novo não; reabre (volta a vencida) e paga outra vez.
	pay := func(id string, body map[string]any, want int) finance.Entry {
		t.Helper()
		var out finance.Entry
		call(http.MethodPost, "/api/finance/entries/"+id+"/pay", body, want, &out)
		return out
	}
	paid := pay(internet.ID.String(), map[string]any{"paidOn": "2026-10-14", "paidCents": 12500, "method": "BOLETO"}, http.StatusOK)
	if paid.Status != "PAID" || *paid.PaidCents != 12500 || paid.PaidOn.String() != "2026-10-14" || paid.Overdue {
		t.Fatalf("paga = %+v", paid)
	}
	pay(internet.ID.String(), map[string]any{}, http.StatusBadRequest)
	pay(contador.ID.String(), map[string]any{"paidOn": "2026-10-16"}, http.StatusBadRequest) // no futuro
	var reopened finance.Entry
	call(http.MethodPost, "/api/finance/entries/"+internet.ID.String()+"/reopen", nil, http.StatusOK, &reopened)
	if reopened.Status != "OPEN" || reopened.PaidOn != nil || !reopened.Overdue {
		t.Fatalf("reaberta = %+v", reopened)
	}
	pay(internet.ID.String(), map[string]any{"paidOn": "2026-10-14", "paidCents": 12500, "method": "BOLETO"}, http.StatusOK)

	var updated finance.Entry
	call(http.MethodPatch, "/api/finance/entries/"+contador.ID.String(), map[string]any{"description": "Contador (outubro)",
		"categoryId": category["Outras despesas"], "amountCents": 32000, "dueDate": "2026-10-18"}, http.StatusOK, &updated)
	if updated.AmountCents != 32000 || updated.Description != "Contador (outubro)" {
		t.Fatalf("editada = %+v", updated)
	}
	call(http.MethodPatch, "/api/finance/entries/"+das.ID.String(), map[string]any{"description": "x",
		"categoryId": category["Outras despesas"], "amountCents": 1, "dueDate": "2026-10-18"}, http.StatusBadRequest, nil)
	call(http.MethodPost, "/api/finance/entries/"+notebook[2].ID.String()+"/cancel", nil, http.StatusOK, nil)
	call(http.MethodDelete, "/api/finance/entries/"+notebook[2].ID.String(), nil, http.StatusNoContent, nil)
	call(http.MethodDelete, "/api/finance/entries/"+das.ID.String(), nil, http.StatusBadRequest, nil)

	sale := create(map[string]any{"kind": "RECEIVABLE", "description": "Venda de 2 rastreadores",
		"categoryId": category["Venda de equipamentos"], "amountCents": 40000, "dueDate": "2026-10-15"}, http.StatusCreated)[0]
	pay(sale.ID.String(), map[string]any{"method": "PIX"}, http.StatusOK) // hoje, pelo valor

	// Anexos: só PDF e imagem, e o download sai como anexo.
	upload := func(entryID, name string, data []byte) (int, finance.Attachment) {
		t.Helper()
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		part, _ := form.CreateFormFile("file", name)
		_, _ = part.Write(data)
		_ = form.Close()
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/finance/entries/"+entryID+"/attachments", &body)
		req.Header.Set("Content-Type", form.FormDataContentType())
		req.Header.Set("Authorization", "Bearer "+admin)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var a finance.Attachment
		_ = json.NewDecoder(res.Body).Decode(&a)
		return res.StatusCode, a
	}
	status, boleto := upload(contador.ID.String(), "boleto outubro.pdf", []byte("%PDF-1.4\nconteúdo do boleto"))
	if status != http.StatusCreated || boleto.ContentType != "application/pdf" || boleto.Filename != "boleto outubro.pdf" {
		t.Fatalf("anexo = %d %+v", status, boleto)
	}
	if status, _ := upload(contador.ID.String(), "pagina.html", []byte("<html><script>alert(1)</script>")); status != http.StatusBadRequest {
		t.Fatalf("HTML devia ser recusado, veio %d", status)
	}
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/finance/attachments/"+boleto.ID.String(), nil)
	req.Header.Set("Authorization", "Bearer "+admin)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	content, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "application/pdf" ||
		!strings.HasPrefix(res.Header.Get("Content-Disposition"), "attachment") ||
		res.Header.Get("X-Content-Type-Options") != "nosniff" || !bytes.HasPrefix(content, []byte("%PDF-")) {
		t.Fatalf("download = %d %v", res.StatusCode, res.Header)
	}
	var listed []finance.Entry
	call(http.MethodGet, "/api/finance/entries?kind=PAYABLE&q=contador", nil, http.StatusOK, &listed)
	if len(listed) != 1 || len(listed[0].Attachments) != 1 {
		t.Fatalf("busca com anexo = %+v", listed)
	}
	call(http.MethodDelete, "/api/finance/attachments/"+boleto.ID.String(), nil, http.StatusNoContent, nil)

	// Recorrências: gera o que vence nos próximos 35 dias; a que começou no
	// passado recupera os meses (vencidos).
	var aluguel, software finance.Recurrence
	call(http.MethodPost, "/api/finance/recurrences", map[string]any{"kind": "PAYABLE", "description": "Aluguel",
		"categoryId": category["Aluguel e escritório"], "amountCents": 200000, "firstDueDate": "2026-10-20"}, http.StatusCreated, &aluguel)
	if aluguel.NextDueDate.String() != "2026-11-20" || aluguel.DueDay != 20 {
		t.Fatalf("aluguel = %+v", aluguel)
	}
	call(http.MethodPost, "/api/finance/recurrences", map[string]any{"kind": "PAYABLE", "description": "Software",
		"categoryId": category["Software e assinaturas"], "amountCents": 5000, "firstDueDate": "2026-09-05"}, http.StatusCreated, &software)
	if software.NextDueDate.String() != "2026-12-05" {
		t.Fatalf("software = %+v", software)
	}
	call(http.MethodGet, "/api/finance/entries?kind=PAYABLE&q=software", nil, http.StatusOK, &listed)
	if len(listed) != 3 || listed[0].DueDate.String() != "2026-09-05" || !listed[0].Overdue {
		t.Fatalf("software gerado = %+v", listed)
	}
	call(http.MethodPatch, "/api/finance/recurrences/"+aluguel.ID.String(), map[string]any{"description": "Aluguel da sala",
		"categoryId": category["Aluguel e escritório"], "amountCents": 210000}, http.StatusOK, nil)
	call(http.MethodGet, "/api/finance/entries?kind=PAYABLE&q=aluguel", nil, http.StatusOK, &listed)
	if len(listed) != 1 || listed[0].AmountCents != 210000 || listed[0].Description != "Aluguel da sala" {
		t.Fatalf("aluguel alterado = %+v", listed)
	}
	call(http.MethodPost, "/api/finance/recurrences/"+aluguel.ID.String()+"/end", nil, http.StatusOK, nil)
	call(http.MethodGet, "/api/finance/entries?kind=PAYABLE&q=aluguel", nil, http.StatusOK, &listed)
	if len(listed) != 0 {
		t.Fatalf("encerrada, a conta que não venceu sai: %+v", listed)
	}

	// Estoque: compra com a conta parcelada, outra compra (custo médio),
	// saída pelo médio, saída maior que o saldo, perda e acerto.
	var item finance.StockItem
	call(http.MethodPost, "/api/finance/stock/items", map[string]any{"name": "Rastreador J16", "kind": "TRACKER",
		"minQuantity": 5, "active": true}, http.StatusCreated, &item)
	move := func(body map[string]any, want int) (finance.StockMovement, []finance.Entry) {
		t.Helper()
		body["itemId"] = item.ID
		var out struct {
			Movement finance.StockMovement `json:"movement"`
			Payables []finance.Entry       `json:"payables"`
		}
		call(http.MethodPost, "/api/finance/stock/movements", body, want, map[bool]any{true: &out, false: nil}[want == http.StatusCreated])
		return out.Movement, out.Payables
	}
	_, purchase := move(map[string]any{"type": "IN", "quantity": 10, "unitCostCents": 10000, "supplierId": vivo.ID,
		"payable": map[string]any{"categoryId": category["Compra de rastreadores e chips"], "dueDate": "2026-10-25", "installments": 2}},
		http.StatusCreated)
	if len(purchase) != 2 || purchase[0].AmountCents != 50000 || purchase[0].Description != "Compra: 10 × Rastreador J16" ||
		purchase[1].DueDate.String() != "2026-11-25" || purchase[0].StockMovementID == nil {
		t.Fatalf("conta da compra = %+v", purchase)
	}
	move(map[string]any{"type": "IN", "quantity": 10, "unitCostCents": 12000}, http.StatusCreated)
	out, _ := move(map[string]any{"type": "OUT", "quantity": 3, "notes": "instalações da semana"}, http.StatusCreated)
	if out.UnitCostCents != 11000 || out.Quantity != -3 || out.TotalCents != -33000 {
		t.Fatalf("saída pelo custo médio = %+v", out)
	}
	move(map[string]any{"type": "OUT", "quantity": 30}, http.StatusBadRequest)
	move(map[string]any{"type": "LOSS", "quantity": 1, "notes": "defeito"}, http.StatusCreated)
	move(map[string]any{"type": "ADJUST", "quantity": 1, "notes": "achado na contagem"}, http.StatusCreated)
	var stock struct {
		Items    []finance.StockItem       `json:"items"`
		Trackers finance.InstalledTrackers `json:"trackers"`
	}
	call(http.MethodGet, "/api/finance/stock/items", nil, http.StatusOK, &stock)
	if len(stock.Items) != 1 || stock.Items[0].Quantity != 17 || stock.Items[0].AvgCostCents != 11000 ||
		stock.Items[0].ValueCents != 187000 || stock.Items[0].Low {
		t.Fatalf("estoque = %+v", stock.Items)
	}
	call(http.MethodPatch, "/api/finance/stock/items/"+item.ID.String(), map[string]any{"name": "Rastreador J16", "kind": "TRACKER",
		"minQuantity": 20, "active": true}, http.StatusOK, nil)
	var movements []finance.StockMovement
	call(http.MethodGet, "/api/finance/stock/movements?item="+item.ID.String(), nil, http.StatusOK, &movements)
	if len(movements) != 5 {
		t.Fatalf("movimentos = %d", len(movements))
	}

	// As faturas e as assinaturas dos clientes entram sozinhas.
	var customerID string
	if err := db.QueryRow(ctx, `INSERT INTO users (email, name, role, password_hash) VALUES ('cliente@empresa.test', 'Cliente', 'customer', 'x') RETURNING id`).
		Scan(&customerID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `
		INSERT INTO invoices (customer_id, description, amount_cents, due_date, status, paid_at, paid_via) VALUES
			($1, 'Paga', 3490, '2026-10-10', 'PAID', '2026-10-12 15:00:00+00', 'MANUAL'),
			($1, 'A vencer', 3490, '2026-10-20', 'OPEN', NULL, ''),
			($1, 'Atrasada', 3490, '2026-10-01', 'OPEN', NULL, '')`, customerID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `
		INSERT INTO subscriptions (customer_id, plan_name, price_cents, due_day, next_due_date)
		VALUES ($1, 'Plano Mensal', 3490, 10, '2026-11-10')`, customerID); err != nil {
		t.Fatal(err)
	}

	// Caixa: entrou 3490 (fatura) + 40000 (venda); saiu 15000 (DAS) + 12500
	// (internet). Setembro começa no saldo inicial; agosto é antes dele.
	var flow finance.CashFlow
	call(http.MethodGet, "/api/finance/cashflow?months=3", nil, http.StatusOK, &flow)
	if len(flow.Months) != 3 || flow.Months[0].EndBalanceCents != nil || *flow.Months[1].EndBalanceCents != 1000000 {
		t.Fatalf("meses = %+v", flow.Months)
	}
	oct := flow.Months[2]
	if oct.InvoicesCents != 3490 || oct.OtherInCents != 40000 || oct.OutCents != 27500 || oct.NetCents != 15990 ||
		*oct.EndBalanceCents != 1015990 || flow.BalanceCents != 1015990 {
		t.Fatalf("outubro = %+v (saldo %d)", oct, flow.BalanceCents)
	}
	// Até 14/11: a receber 3490 + 3490 (faturas em aberto, com a atrasada) +
	// 3490 (mensalidade de 10/11); a pagar 33334 (notebook) + 8000 + 32000 +
	// 3 × 5000 (software) + 50000 (compra, 1ª parcela).
	p30, p60 := flow.Projections[0], flow.Projections[1]
	if p30.Days != 30 || p30.InCents != 10470 || p30.OutCents != 138334 || p30.BalanceCents != 1015990+10470-138334 {
		t.Errorf("projeção de 30 dias = %+v", p30)
	}
	// Até 14/12: mais a mensalidade de 10/12, a 2ª parcela do notebook e da
	// compra e o software de 05/12, que nem foi gerado ainda.
	if p60.InCents != 13960 || p60.OutCents != 138334+33333+50000+5000 {
		t.Errorf("projeção de 60 dias = %+v", p60)
	}

	// Resultado de outubro: receita 3490 + 40000; impostos 15000; custo do
	// equipamento 3 × 110,00 (a perda e o acerto se anulam); operacional
	// 12500. A compra para o estoque não é custo do mês.
	var dre []finance.DREMonth
	call(http.MethodGet, "/api/finance/dre?months=2", nil, http.StatusOK, &dre)
	if len(dre) != 2 || dre[1].Month != "2026-10" {
		t.Fatalf("meses do resultado = %+v", dre)
	}
	m := dre[1]
	if m.InvoicesCents != 3490 || m.RevenueCents != 43490 || m.TaxesCents != 15000 || m.NetRevenueCents != 28490 ||
		m.StockCostCents != 33000 || m.StockLossCents != 0 || m.GrossProfitCents != -4510 || m.OperatingCents != 12500 ||
		m.ResultCents != -17010 || m.InvestmentsCents != 0 {
		t.Fatalf("resultado de outubro = %+v", m)
	}

	// Visão geral e o número do menu.
	var overview finance.Overview
	call(http.MethodGet, "/api/finance/overview", nil, http.StatusOK, &overview)
	if overview.Overdue.Count != 2 || overview.Overdue.Cents != 10000 || overview.DueToday.Count != 1 ||
		overview.DueToday.Cents != 8000 || overview.DueWeek.Count != 1 || overview.DueWeek.Cents != 32000 ||
		overview.ReceivableOverdue.Count != 1 || overview.ReceivableOverdue.Cents != 3490 ||
		overview.BalanceCents != 1015990 || overview.MonthResult != -17010 || len(overview.LowStock) != 1 ||
		overview.StockValueCents != 187000 || len(overview.Upcoming) != 4 {
		t.Fatalf("visão geral = %+v", overview)
	}
	var alerts finance.Alerts
	call(http.MethodGet, "/api/finance/alerts", nil, http.StatusOK, &alerts)
	if alerts.Overdue != 2 || alerts.DueToday != 1 || alerts.LowStock != 1 {
		t.Fatalf("alertas = %+v", alerts)
	}

	// O resumo dos vencimentos: um por dia, para os administradores.
	fs.SendReminders(ctx)
	fs.SendReminders(ctx)
	if len(mailer.sent) != 1 || len(mailer.to[0]) != 1 || mailer.to[0][0] != auth.RoleAdmin+"@empresa.test" {
		t.Fatalf("avisos = %d para %v", len(mailer.sent), mailer.to)
	}
	d := mailer.sent[0]
	if len(d.Today) != 1 || d.Today[0].Description != "Dados dos chips" || d.Today[0].Supplier != "Vivo Empresas" ||
		len(d.Soon) != 1 || d.Soon[0].Amount != "R$ 320,00" || d.SoonDate != "18/10" ||
		d.OverdueCount != 2 || d.OverdueAmount != "R$ 100,00" {
		t.Fatalf("resumo = %+v", d)
	}
	// Antes das 8h, nada; com o envio falhando, tenta de novo depois.
	now = time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC) // 7h em Brasília
	fs.SendReminders(ctx)
	now = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC) // 9h; vence a 2ª parcela da compra em 25/10
	mailer.fail = errors.New("SMTP fora do ar")
	fs.SendReminders(ctx)
	var reserved int
	_ = db.QueryRow(ctx, `SELECT count(*) FROM finance_reminders WHERE day > '2026-10-15'`).Scan(&reserved)
	if reserved != 0 || len(mailer.sent) != 1 {
		t.Fatalf("antes das 8h ou com falha não reserva o dia: %d reservas, %d e-mails", reserved, len(mailer.sent))
	}
	mailer.fail = nil
	fs.SendReminders(ctx)
	if len(mailer.sent) != 2 || len(mailer.sent[1].Soon) != 1 || mailer.sent[1].Soon[0].Installment != "1/2" {
		t.Fatalf("depois da falha = %+v", mailer.sent)
	}
}
