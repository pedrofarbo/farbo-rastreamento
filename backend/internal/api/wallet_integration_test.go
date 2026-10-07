package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/finance"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/fulfillment"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/melhorenvio"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/payments/abacatepay"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/topups"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/vehicles"
)

// fakeWallet é o Melhor Envios do teste: a carteira, a recarga e o começo
// da compra da etiqueta (cotação, carrinho e pagamento).
type fakeWallet struct {
	mu       sync.Mutex
	balances []string // um por consulta; o último se repete
	forbid   bool
	topUps   []map[string]string
	carts    int
	removed  int
	checkout string // vazio: pago
	// pixCode vai na resposta da recarga por Pix, dentro da resposta do meio
	// de pagamento (texto com JSON); vazio, a resposta não traz.
	pixCode string
}

func (f *fakeWallet) handler(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.URL.Path == "/api/v2/me/balance" && f.forbid:
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"message":"Invalid scope(s) provided."}`)
	case r.URL.Path == "/api/v2/me/balance" && r.Method == http.MethodGet:
		balance := f.balances[0]
		if len(f.balances) > 1 {
			f.balances = f.balances[1:]
		}
		_, _ = io.WriteString(w, `{"balance": `+balance+`, "reserved": 0, "debts": 0}`)
	case r.URL.Path == "/api/v2/me/balance" && r.Method == http.MethodPost:
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.topUps = append(f.topUps, body)
		link, digitable, response := `null`, `null`, `null`
		if body["slug"] == "pix" {
			link = `"https://melhorenvio.test/pix/PAY-1"`
			if f.pixCode != "" {
				inner, _ := json.Marshal(map[string]any{"transaction": map[string]string{"qrcode_original_path": f.pixCode}})
				quoted, _ := json.Marshal(string(inner))
				response = string(quoted)
			}
		} else {
			digitable = `"34191.79001 01043.510047 91020.150008 1 97950000005000"`
		}
		_, _ = io.WriteString(w, `{"payment": {"id": "pay-1", "protocol": "PAY-1", "value": 5000, "status": "pending",
			"redirect": "`+body["redirect_url"]+`", "link": `+link+`, "response": `+response+`}, "redirect": "`+body["redirect_url"]+`",
			"message": "", "digitable": `+digitable+`}`)
	case r.URL.Path == "/api/v2/me/shipment/calculate":
		_, _ = io.WriteString(w, `[{"id": 1, "name": "PAC", "price": "22.40", "delivery_time": 7, "company": {"name": "Correios"}}]`)
	case r.URL.Path == "/api/v2/me/cart" && r.Method == http.MethodPost:
		f.carts++
		_, _ = io.WriteString(w, `{"id": "ord-1", "protocol": "ORD-1", "price": "22.40", "status": "pending"}`)
	case strings.HasPrefix(r.URL.Path, "/api/v2/me/cart/") && r.Method == http.MethodDelete:
		f.removed++
	case r.URL.Path == "/api/v2/me/shipment/checkout":
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = io.WriteString(w, `{"message": "`+f.checkout+`"}`)
	default:
		http.NotFound(w, r)
	}
}

// A carteira do Melhor Envios no painel: o saldo, a recarga por Pix ou
// boleto (em nome da empresa) e a compra da etiqueta barrada sem saldo, sem
// sobrar nada no carrinho. Precisa de FARBO_TEST_DATABASE_URL.
func TestShippingWallet(t *testing.T) {
	field := func(id, value string) string { return fmt.Sprintf("%s%02d%s", id, len(value), value) }
	pixCode := func(amount string) string {
		payload := field("00", "01") + field("26", field("00", "br.gov.bcb.pix")+field("25", "pix.yapay.test/qr/abc")) +
			field("52", "0000") + field("53", "986") + field("54", amount) + field("58", "BR") +
			field("59", "YAPAY PAGAMENTOS") + field("60", "SAO PAULO") + field("62", field("05", "PAY1")) + "6304"
		return payload + crcHex(payload)
	}
	me := &fakeWallet{balances: []string{`"152.30"`}, pixCode: pixCode("50.00")}
	abacate := &fakePixSender{}
	abacate.respond = abacate.completes
	srv := httptest.NewServer(http.HandlerFunc(me.handler))
	t.Cleanup(srv.Close)

	var vehicleSvc *vehicles.Service
	env, _ := newLeadsEnv(t, func(d *Deps) {
		carrier := melhorenvio.New(melhorenvio.Config{BaseURL: srv.URL, StaticToken: "token", ContactEmail: "t@farbo.test"}, nil)
		d.Carrier = carrier
		d.Config.Mail.AppURL = "https://painel.farbo.test"
		d.Config.Company = config.Company{LegalName: "FARBO TECNOLOGIA LTDA", CNPJ: "49.757.084/0001-00"}
		shipping := config.Shipping{
			From: config.ShippingAddress{
				Name: "Farbo", Phone: "11999999999", CompanyDocument: "49757084000100", PostalCode: "01310100",
				Address: "Avenida Paulista", Number: "1000", District: "Bela Vista", City: "São Paulo", State: "SP",
			},
			WeightKg: 0.3, HeightCm: 4, WidthCm: 11, LengthCm: 16, InsuranceCents: 15000,
		}
		d.Fulfillment = fulfillment.NewService(d.DB, fulfillment.NewRepository(d.DB), carrier, nil, shipping, "Rastreador",
			slog.New(slog.NewTextHandler(io.Discard, nil)))
		d.Finance.SetPixSender(abacate, true)
		d.TopUps = topups.NewService(d.DB, carrier, d.Finance, d.Config.Company, d.Config.Mail.AppURL,
			slog.New(slog.NewTextHandler(io.Discard, nil)))
		vehicleSvc = d.Vehicles
	})
	ctx := context.Background()
	admin := env.login(auth.RoleAdmin + "@leads.test")
	operator := env.login(auth.RoleOperator + "@leads.test")

	// O saldo, em centavos, com o link do painel.
	var wallet struct {
		BalanceCents int    `json:"balanceCents"`
		PanelURL     string `json:"panelUrl"`
	}
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/integrations/melhorenvio/balance", nil, http.StatusOK), &wallet)
	if wallet.BalanceCents != 15230 || wallet.PanelURL != srv.URL+"/painel" {
		t.Errorf("saldo = %+v", wallet)
	}
	if status, _ := env.do(operator, http.MethodGet, "/api/integrations/melhorenvio/balance", nil); status != http.StatusForbidden {
		t.Errorf("operador viu o saldo: %d", status)
	}

	// Recarga: valida antes; o Pix vem com o link e volta para Pedidos.
	for _, bad := range []map[string]any{
		{"valueCents": 5000, "method": "cartao"}, {"valueCents": 50, "method": "pix"}, {"valueCents": 2_000_000, "method": "pix"},
	} {
		if status, body := env.do(admin, http.MethodPost, "/api/integrations/melhorenvio/balance", bad); status != http.StatusBadRequest {
			t.Errorf("recarga %v = %d %s", bad, status, body)
		}
	}
	var top struct {
		ID         string `json:"id"`
		Protocol   string `json:"protocol"`
		Link       string `json:"link"`
		Digitable  string `json:"digitable"`
		PixCode    string `json:"pixCode"`
		ValueCents int    `json:"valueCents"`
		Method     string `json:"method"`
	}
	_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/integrations/melhorenvio/balance",
		map[string]any{"valueCents": 5000, "method": "pix"}, http.StatusOK), &top)
	if top.Link != "https://melhorenvio.test/pix/PAY-1" || top.Protocol != "PAY-1" || top.ValueCents != 5000 || top.Method != "pix" ||
		top.PixCode != pixCode("50.00") || top.ID == "" {
		t.Errorf("Pix = %+v", top)
	}
	pixTop := top.ID
	pix := me.topUps[0]
	if pix["gateway"] != melhorenvio.TopUpGateway || pix["slug"] != "pix" || pix["value"] != "50.00" ||
		pix["redirect_url"] != "https://painel.farbo.test/pedidos?saldo=pago" || pix["cnpj"] != "" {
		t.Errorf("pedido do Pix = %v", pix)
	}
	// O boleto sai em nome da empresa; o link de volta não é o do pagamento.
	_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/integrations/melhorenvio/balance",
		map[string]any{"valueCents": 123456, "method": "boleto"}, http.StatusOK), &top)
	boleto := me.topUps[1]
	if boleto["value"] != "1234.56" || boleto["company_name"] != "FARBO TECNOLOGIA LTDA" || boleto["cnpj"] != "49757084000100" {
		t.Errorf("pedido do boleto = %v", boleto)
	}
	if top.Link != "" || !strings.HasPrefix(top.Digitable, "34191") || top.PixCode != "" {
		t.Errorf("boleto = %+v", top)
	}
	boletoTop := top.ID
	var audited int
	if err := env.db.QueryRow(ctx, `SELECT COUNT(*) FROM audit_logs WHERE action = $1`, audit.ActionShippingBalanceAdded).Scan(&audited); err != nil || audited != 2 {
		t.Errorf("auditoria = %d (%v)", audited, err)
	}

	// Pagar pela AbacatePay: a recarga vira uma conta a pagar (Frete e
	// envio, com o copia-e-cola) e sai pelo Pix dos fornecedores.
	var integration struct {
		PayWithAbacate bool `json:"payWithAbacate"`
	}
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/integrations/melhorenvio", nil, http.StatusOK), &integration)
	if !integration.PayWithAbacate {
		t.Errorf("integração sem a AbacatePay: %+v", integration)
	}
	entryOf := func(id string, code string, want int) (finance.Entry, string) {
		t.Helper()
		status, body := env.do(admin, http.MethodPost, "/api/integrations/melhorenvio/balance/"+id+"/entry", map[string]string{"pixCode": code})
		if status != want {
			t.Fatalf("conta da recarga %s = %d %s", id, status, body)
		}
		var e finance.Entry
		_ = json.Unmarshal(body, &e)
		return e, string(body)
	}
	bill, _ := entryOf(pixTop, "", http.StatusOK)
	if bill.Kind != finance.KindPayable || bill.Status != finance.StatusOpen || bill.AmountCents != 5000 ||
		bill.CategoryName != "Frete e envio" || bill.PaymentCode != pixCode("50.00") || !strings.Contains(bill.Description, "PAY-1") {
		t.Fatalf("conta = %+v", bill)
	}
	if again, _ := entryOf(pixTop, "", http.StatusOK); again.ID != bill.ID {
		t.Errorf("dois cliques abriram duas contas: %s e %s", bill.ID, again.ID)
	}
	if status, body := env.do(admin, http.MethodPost, "/api/finance/entries/"+bill.ID.String()+"/pix", nil); status != http.StatusOK {
		t.Fatalf("Pix pela AbacatePay = %d %s", status, body)
	}
	if sent := abacate.sent[len(abacate.sent)-1]; sent.KeyType != abacatepay.KeyBRCode || sent.Key != pixCode("50.00") || sent.AmountCents != 5000 {
		t.Errorf("Pix enviado = %+v", sent)
	}
	if _, body := entryOf(pixTop, "", http.StatusBadRequest); !strings.Contains(body, "já está paga") {
		t.Errorf("recarga paga = %s", body)
	}

	// Sem o copia-e-cola na resposta: cola-se o da página do Pix, conferido.
	me.mu.Lock()
	me.pixCode = ""
	me.mu.Unlock()
	_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/integrations/melhorenvio/balance",
		map[string]any{"valueCents": 3000, "method": "pix"}, http.StatusOK), &top)
	if top.PixCode != "" {
		t.Fatalf("copia-e-cola inventado: %+v", top)
	}
	for code, want := range map[string]string{
		"": "Cole o Pix copia-e-cola", "000201qualquer-coisa": "inválido", pixCode("50.00"): "R$ 50,00",
	} {
		if _, body := entryOf(top.ID, code, http.StatusBadRequest); !strings.Contains(body, want) {
			t.Errorf("copia-e-cola %q = %s", code, body)
		}
	}
	// Colado com quebras de linha e espaços: vale.
	code := pixCode("30.00")
	pasted, _ := entryOf(top.ID, " "+code[:40]+"\n"+code[40:]+" ", http.StatusOK)
	if pasted.PaymentCode != code || pasted.AmountCents != 3000 {
		t.Errorf("colado = %+v", pasted)
	}
	// Boleto não sai pela AbacatePay; quem não é admin não lança.
	if _, body := entryOf(boletoTop, code, http.StatusBadRequest); !strings.Contains(body, "Só a recarga por Pix") {
		t.Errorf("boleto = %s", body)
	}
	if status, _ := env.do(operator, http.MethodPost, "/api/integrations/melhorenvio/balance/"+top.ID+"/entry", map[string]string{}); status != http.StatusForbidden {
		t.Errorf("operador lançou: %d", status)
	}

	// Um pedido pronto para envio.
	authSvc := auth.NewService(auth.NewRepository(env.db), config.Auth{BcryptCost: 4}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	lia, err := authSvc.CreateUser(ctx, "lia@carteira.test", "Lia", auth.RoleCustomer, userPassword)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.db.Exec(ctx, `UPDATE users SET document = '52998224725', phone = '11987654321' WHERE id = $1`, lia.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := env.db.Exec(ctx, `INSERT INTO customer_addresses (customer_id, zip_code, street, number, complement, district, city, state)
		VALUES ($1, '20040002', 'Rua da Carioca', '10', '', 'Centro', 'Rio de Janeiro', 'RJ')`, lia.ID); err != nil {
		t.Fatal(err)
	}
	moto, err := vehicleSvc.Create(ctx, vehicles.Input{Name: "Moto da Lia", OwnerID: &lia.ID})
	if err != nil {
		t.Fatal(err)
	}
	order, err := fulfillment.Create(ctx, env.db, lia.ID, moto.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.db.Exec(ctx, `UPDATE fulfillments SET tracker_status = 'CONFIGURED' WHERE id = $1`, order); err != nil {
		t.Fatal(err)
	}
	buy := func() (int, map[string]any) {
		t.Helper()
		status, body := env.do(admin, http.MethodPost, "/api/fulfillments/"+order.String()+"/shipping/label", map[string]int{"serviceId": 1})
		var out map[string]any
		_ = json.Unmarshal(body, &out)
		return status, out
	}

	// Sem saldo: nem vai para o carrinho, e a tela sabe quanto falta.
	me.mu.Lock()
	me.balances = []string{`10`}
	me.mu.Unlock()
	status, out := buy()
	if status != http.StatusConflict || out["code"] != codeInsufficientBalance ||
		out["balanceCents"] != float64(1000) || out["priceCents"] != float64(2240) ||
		!strings.Contains(out["error"].(string), "R$ 22,40") {
		t.Fatalf("sem saldo = %d %v", status, out)
	}
	if me.carts != 0 {
		t.Errorf("foi ao carrinho sem saldo: %d", me.carts)
	}

	// O saldo mudou entre a conferência e o pagamento: a etiqueta sai do
	// carrinho e o aviso é o de saldo.
	me.mu.Lock()
	me.balances, me.checkout = []string{`100`, `"5.00"`}, "Saldo insuficiente"
	me.mu.Unlock()
	if status, out = buy(); status != http.StatusConflict || out["code"] != codeInsufficientBalance || out["balanceCents"] != float64(500) {
		t.Fatalf("pagamento recusado = %d %v", status, out)
	}
	if me.carts != 1 || me.removed != 1 {
		t.Errorf("carrinho = %d, removidas = %d", me.carts, me.removed)
	}

	// Sem permissão para ver o saldo: a compra não é barrada (quem decide é
	// o pagamento), e a consulta pede para conectar de novo.
	me.mu.Lock()
	me.forbid, me.checkout = true, "Documento inválido"
	me.mu.Unlock()
	if status, out = buy(); status != http.StatusBadGateway || !strings.Contains(out["error"].(string), "Documento inválido") {
		t.Errorf("sem permissão da carteira = %d %v", status, out)
	}
	if me.carts != 2 {
		t.Errorf("a compra foi barrada sem ler o saldo: %d", me.carts)
	}
	status, body := env.do(admin, http.MethodGet, "/api/integrations/melhorenvio/balance", nil)
	if status != http.StatusConflict || !strings.Contains(string(body), "BALANCE_FORBIDDEN") || !strings.Contains(string(body), "conecte de novo") {
		t.Errorf("saldo sem permissão = %d %s", status, body)
	}
}
