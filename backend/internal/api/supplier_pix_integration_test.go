package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/finance"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/payments/abacatepay"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/stepup"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/telemetry"
)

// fakePixSender imita a AbacatePay nos envios: guarda o que foi pedido e
// responde o que o teste mandar.
type fakePixSender struct {
	mu      sync.Mutex
	sent    []abacatepay.TransferRequest
	respond func(req abacatepay.TransferRequest) (*abacatepay.Transfer, error)
	lookup  map[string]*abacatepay.Transfer
	delay   time.Duration
}

func (f *fakePixSender) SendPix(_ context.Context, req abacatepay.TransferRequest) (*abacatepay.Transfer, error) {
	time.Sleep(f.delay)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, req)
	return f.respond(req)
}

func (f *fakePixSender) GetTransfer(_ context.Context, id string) (*abacatepay.Transfer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if t, ok := f.lookup[id]; ok {
		return t, nil
	}
	return nil, &abacatepay.APIError{HTTPStatus: 400, Message: "Transaction not found"}
}

func (f *fakePixSender) Balance(context.Context) (*abacatepay.Balance, error) {
	return &abacatepay.Balance{Available: 509460}, nil
}

func (f *fakePixSender) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

// completes responde COMPLETE com a tarifa de R$ 0,80, como a AbacatePay.
func (f *fakePixSender) completes(req abacatepay.TransferRequest) (*abacatepay.Transfer, error) {
	t := &abacatepay.Transfer{
		ID: fmt.Sprintf("tran_%d", len(f.sent)), Status: abacatepay.TransferComplete, DevMode: true,
		ReceiptURL: "https://app.abacatepay.com/receipt/x", Amount: req.AmountCents, PlatformFee: 80, ExternalID: req.ExternalID,
	}
	if f.lookup == nil {
		f.lookup = map[string]*abacatepay.Transfer{}
	}
	f.lookup[t.ID] = t
	return t, nil
}

// Pagar fornecedores por Pix pela AbacatePay, de ponta a ponta com a
// AbacatePay de mentira: a confirmação da senha, a conta baixada e a tarifa
// lançada; recusa, falta de resposta e conferência à mão; dois cliques que
// não pagam duas vezes; o Pix copia-e-cola; e o envio que falha depois de
// enviado. Precisa de FARBO_TEST_DATABASE_URL.
func TestSupplierPixEndToEnd(t *testing.T) {
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
		if _, err := authSvc.CreateUser(ctx, role+"@pix.test", role, role, userPassword); err != nil {
			t.Fatal(err)
		}
	}
	fake := &fakePixSender{}
	fake.respond = fake.completes
	fs := finance.NewService(db, &fakeFinanceMailer{}, log)
	stepUpSvc := stepup.NewService(db, config.StepUp{RPID: "localhost", Origins: []string{"http://localhost"}}, authSvc)
	server := NewServer(Deps{
		Config: cfg, Log: log, Metrics: telemetry.NewMetrics(), DB: db, Auth: authSvc,
		Audit: audit.NewService(audit.NewRepository(db), log), Finance: fs, StepUp: stepUpSvc,
	})
	srv := httptest.NewServer(server.Handler())
	t.Cleanup(srv.Close)
	env := &credEnv{t: t, db: db, srv: srv}
	admin := env.login(auth.RoleAdmin + "@pix.test")
	call := func(method, path string, body any, want int, out any) {
		t.Helper()
		raw := env.must(admin, method, path, body, want)
		if out != nil {
			if err := json.Unmarshal(raw, out); err != nil {
				t.Fatalf("%s %s: %v (%s)", method, path, err, raw)
			}
		}
	}

	// Desligado: a tela sabe e o envio é recusado.
	var info finance.PixInfo
	call(http.MethodGet, "/api/finance/pix", nil, http.StatusOK, &info)
	if info.Enabled {
		t.Fatalf("ligado sem a AbacatePay: %+v", info)
	}
	fs.SetPixSender(fake, true)
	call(http.MethodGet, "/api/finance/pix", nil, http.StatusOK, &info)
	if !info.Enabled || !info.DevMode || info.AvailableCents == nil || *info.AvailableCents != 509460 {
		t.Fatalf("info = %+v", info)
	}
	env.must(env.login(auth.RoleOperator+"@pix.test"), http.MethodGet, "/api/finance/pix", nil, http.StatusForbidden)

	var categories []finance.Category
	call(http.MethodGet, "/api/finance/categories", nil, http.StatusOK, &categories)
	category := map[string]string{}
	for _, c := range categories {
		category[c.Name] = c.ID.String()
	}
	supplier := func(name, key, keyType string) finance.Supplier {
		t.Helper()
		var s finance.Supplier
		call(http.MethodPost, "/api/finance/suppliers", map[string]any{"name": name, "pixKey": key, "pixKeyType": keyType, "active": true},
			http.StatusCreated, &s)
		return s
	}
	env.must(admin, http.MethodPost, "/api/finance/suppliers", map[string]any{"name": "Ambíguo", "pixKey": "52998224725", "active": true}, http.StatusBadRequest)
	contador := supplier("Silva Contabilidade", "529.982.247-25", "CPF")
	if contador.PixKeyType != "CPF" {
		t.Errorf("fornecedor = %+v", contador)
	}
	semChave := supplier("Sem Chave", "", "")
	entry := func(description string, supplierID any, cents int64, code string) finance.Entry {
		t.Helper()
		var out []finance.Entry
		call(http.MethodPost, "/api/finance/entries", map[string]any{
			"kind": "PAYABLE", "description": description, "categoryId": category["Contabilidade"], "supplierId": supplierID,
			"amountCents": cents, "dueDate": time.Now().Format(time.DateOnly), "installments": 1, "paymentCode": code,
		}, http.StatusCreated, &out)
		return out[0]
	}
	type planView struct {
		Plan      *finance.PixPlan       `json:"plan"`
		Problem   string                 `json:"problem"`
		Transfers []*finance.PixTransfer `json:"transfers"`
	}
	plan := func(id string) planView {
		t.Helper()
		var p planView
		call(http.MethodGet, "/api/finance/entries/"+id+"/pix", nil, http.StatusOK, &p)
		return p
	}
	// O primeiro comprovante pela tela (a rota tem limite por IP); os outros
	// direto no serviço.
	var adminID uuid.UUID
	if err := db.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, auth.RoleAdmin+"@pix.test").Scan(&adminID); err != nil {
		t.Fatal(err)
	}
	grants := 0
	grant := func() string {
		t.Helper()
		grants++
		if grants == 1 {
			var g stepup.Grant
			call(http.MethodPost, "/api/step-up/password", map[string]any{"purpose": stepup.PurposeSupplierPix, "password": userPassword},
				http.StatusOK, &g)
			return g.Token
		}
		g, err := stepUpSvc.VerifyPassword(ctx, adminID, stepup.PurposeSupplierPix, userPassword)
		if err != nil {
			t.Fatal(err)
		}
		return g.Token
	}
	send := func(id, token string) (int, []byte) {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/finance/entries/"+id+"/pix", bytes.NewReader(nil))
		req.Header.Set("Authorization", "Bearer "+admin)
		if token != "" {
			req.Header.Set("X-Step-Up-Token", token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, body
	}
	get := func(id string) finance.Entry {
		t.Helper()
		var all []finance.Entry
		call(http.MethodGet, "/api/finance/entries?kind=PAYABLE&status=all", nil, http.StatusOK, &all)
		for _, e := range all {
			if e.ID.String() == id {
				return e
			}
		}
		t.Fatalf("conta %s sumiu", id)
		return finance.Entry{}
	}

	// O plano: a chave do fornecedor; sem chave, o motivo.
	honorarios := entry("Honorários de outubro", contador.ID, 45000, "")
	if p := plan(honorarios.ID.String()); p.Plan == nil || p.Plan.Key != "52998224725" || p.Plan.KeyType != "CPF" ||
		p.Plan.Source != "key" || p.Plan.AmountCents != 45000 {
		t.Fatalf("plano = %+v", p)
	}
	semPix := entry("Conta sem Pix", semChave.ID, 1000, "")
	if p := plan(semPix.ID.String()); p.Plan != nil || !strings.Contains(p.Problem, "não tem chave Pix") {
		t.Errorf("sem chave = %+v", p)
	}

	// Sem a confirmação da senha, não sai.
	if status, body := send(honorarios.ID.String(), ""); status != http.StatusForbidden || !strings.Contains(string(body), "confirme") {
		t.Fatalf("sem confirmação: %d %s", status, body)
	}
	if fake.count() != 0 {
		t.Fatal("enviou sem a confirmação")
	}
	// Com ela: sai, a conta é baixada e a tarifa entra nas contas.
	status, body := send(honorarios.ID.String(), grant())
	var sent finance.PixTransfer
	_ = json.Unmarshal(body, &sent)
	if status != http.StatusOK || sent.Status != finance.PixComplete || sent.ProviderID == "" || sent.FeeCents != 80 {
		t.Fatalf("envio: %d %s", status, body)
	}
	if req := fake.sent[0]; req.AmountCents != 45000 || req.Key != "52998224725" || req.KeyType != "CPF" ||
		!strings.HasPrefix(req.ExternalID, "farbo-pix-") || req.Description != "Honorários de outubro" {
		t.Errorf("pedido à AbacatePay = %+v", req)
	}
	paid := get(honorarios.ID.String())
	if paid.Status != finance.StatusPaid || paid.PaymentMethod != "PIX" || paid.PaidCents == nil || *paid.PaidCents != 45000 ||
		paid.Pix == nil || paid.Pix.Status != finance.PixComplete {
		t.Fatalf("conta paga = %+v", paid)
	}
	var fees []finance.Entry
	call(http.MethodGet, "/api/finance/entries?kind=PAYABLE&status=paid&q=Tarifa", nil, http.StatusOK, &fees)
	if len(fees) != 1 || fees[0].AmountCents != 80 || fees[0].CategoryName != "Taxas de pagamento" ||
		!strings.Contains(fees[0].Description, "Silva Contabilidade") {
		t.Fatalf("tarifa = %+v", fees)
	}
	// A conta paga por Pix não se reabre, não se exclui, não se paga de novo.
	env.must(admin, http.MethodPost, "/api/finance/entries/"+honorarios.ID.String()+"/reopen", nil, http.StatusBadRequest)
	env.must(admin, http.MethodDelete, "/api/finance/entries/"+honorarios.ID.String(), nil, http.StatusBadRequest)
	if status, _ := send(honorarios.ID.String(), grant()); status != http.StatusBadRequest || fake.count() != 1 {
		t.Errorf("segundo envio: %d (%d envios)", status, fake.count())
	}
	// O comprovante vale uma vez só.
	token := grant()
	sobra := entry("Material", contador.ID, 2000, "")
	if status, _ := send(sobra.ID.String(), token); status != http.StatusOK {
		t.Fatalf("material: %d", status)
	}
	if status, _ := send(semPix.ID.String(), token); status != http.StatusForbidden {
		t.Errorf("comprovante reaproveitado: %d", status)
	}

	// Recusa da AbacatePay: a mensagem dela, a conta em aberto, dá para tentar de novo.
	fake.respond = func(abacatepay.TransferRequest) (*abacatepay.Transfer, error) {
		return nil, &abacatepay.APIError{HTTPStatus: 400, Message: "Saldo insuficiente"}
	}
	aluguel := entry("Aluguel", contador.ID, 180000, "")
	if status, body := send(aluguel.ID.String(), grant()); status != http.StatusBadRequest || !strings.Contains(string(body), "Saldo insuficiente") {
		t.Fatalf("recusa: %d %s", status, body)
	}
	if e := get(aluguel.ID.String()); e.Status != finance.StatusOpen || e.Pix == nil || e.Pix.Status != finance.PixFailed {
		t.Fatalf("depois da recusa = %+v %+v", e, e.Pix)
	}
	fake.respond = fake.completes
	if status, _ := send(aluguel.ID.String(), grant()); status != http.StatusOK {
		t.Fatalf("de novo depois da recusa: %d", status)
	}

	// Sem resposta: fica a conferir; não se paga de novo nem à mão.
	fake.respond = func(abacatepay.TransferRequest) (*abacatepay.Transfer, error) {
		return nil, errors.New("AbacatePay indisponível: i/o timeout")
	}
	internet := entry("Internet", contador.ID, 12990, "")
	if status, body := send(internet.ID.String(), grant()); status != http.StatusBadRequest || !strings.Contains(string(body), "não respondeu") {
		t.Fatalf("sem resposta: %d %s", status, body)
	}
	unknown := get(internet.ID.String()).Pix
	if unknown == nil || unknown.Status != finance.PixUnknown {
		t.Fatalf("a conferir = %+v", unknown)
	}
	fake.respond = fake.completes
	before := fake.count()
	if status, _ := send(internet.ID.String(), grant()); status != http.StatusBadRequest || fake.count() != before {
		t.Errorf("reenviou sem conferir: %d", status)
	}
	env.must(admin, http.MethodPost, "/api/finance/entries/"+internet.ID.String()+"/pay", map[string]any{}, http.StatusBadRequest)
	env.must(admin, http.MethodPost, "/api/finance/entries/"+internet.ID.String()+"/cancel", nil, http.StatusBadRequest)
	env.must(admin, http.MethodDelete, "/api/finance/entries/"+internet.ID.String(), nil, http.StatusBadRequest)
	// Conferido: não saiu. Aí sim, tenta de novo.
	var resolved finance.PixTransfer
	call(http.MethodPost, "/api/finance/pix/"+unknown.ID.String()+"/resolve", map[string]any{"sent": false}, http.StatusOK, &resolved)
	if resolved.Status != finance.PixFailed || get(internet.ID.String()).Status != finance.StatusOpen {
		t.Fatalf("conferido (não saiu) = %+v", resolved)
	}
	env.must(admin, http.MethodPost, "/api/finance/pix/"+unknown.ID.String()+"/resolve", map[string]any{"sent": true}, http.StatusBadRequest)
	// Outro sem resposta, conferido como enviado (com o id de lá).
	fake.respond = func(req abacatepay.TransferRequest) (*abacatepay.Transfer, error) {
		_, _ = fake.completes(req) // saiu lá, mas a resposta não chegou
		return nil, errors.New("AbacatePay indisponível: connection reset")
	}
	if status, _ := send(internet.ID.String(), grant()); status != http.StatusBadRequest {
		t.Fatalf("segundo sem resposta: %d", status)
	}
	unknown = get(internet.ID.String()).Pix
	providerID := fmt.Sprintf("tran_%d", fake.count())
	fake.lookup[providerID].Amount = 99 // valor diferente: recusado
	env.must(admin, http.MethodPost, "/api/finance/pix/"+unknown.ID.String()+"/resolve", map[string]any{"sent": true, "providerId": providerID}, http.StatusBadRequest)
	fake.lookup[providerID].Amount = 12990
	call(http.MethodPost, "/api/finance/pix/"+unknown.ID.String()+"/resolve", map[string]any{"sent": true, "providerId": providerID}, http.StatusOK, &resolved)
	if e := get(internet.ID.String()); resolved.Status != finance.PixComplete || resolved.ProviderID != providerID || e.Status != finance.StatusPaid {
		t.Fatalf("conferido (saiu) = %+v %+v", resolved, e)
	}
	fake.respond = fake.completes

	// Dois cliques ao mesmo tempo: um Pix só.
	fake.delay = 300 * time.Millisecond
	chips := entry("Dados dos chips", contador.ID, 38000, "")
	before = fake.count()
	tokens := []string{grant(), grant()}
	var wg sync.WaitGroup
	statuses := make([]int, 2)
	for i := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			statuses[i], _ = send(chips.ID.String(), tokens[i])
		}()
	}
	wg.Wait()
	fake.delay = 0
	if fake.count()-before != 1 || !((statuses[0] == 200) != (statuses[1] == 200)) {
		t.Fatalf("dois cliques: %d envios, respostas %v", fake.count()-before, statuses)
	}

	// O Pix copia-e-cola da conta: com o valor dele; valor diferente, recusado.
	field := func(id, value string) string { return fmt.Sprintf("%s%02d%s", id, len(value), value) }
	brCode := func(amount string) string {
		payload := field("00", "01") + field("26", field("00", "br.gov.bcb.pix")+field("01", "loja@exemplo.com")) +
			field("52", "0000") + field("53", "986") + field("54", amount) + field("58", "BR") +
			field("59", "LOJA DE PECAS") + field("60", "SAO PAULO") + field("62", field("05", "***")) + "6304"
		return payload + crcHex(payload)
	}
	errado := entry("Peças (valor errado)", nil, 15000, brCode("149.90"))
	if p := plan(errado.ID.String()); p.Plan != nil || !strings.Contains(p.Problem, "R$ 149,90") {
		t.Errorf("valor diferente = %+v", p)
	}
	pecas := entry("Peças", nil, 14990, brCode("149.90"))
	if p := plan(pecas.ID.String()); p.Plan == nil || p.Plan.KeyType != "BR_CODE" || p.Plan.Recipient != "LOJA DE PECAS" {
		t.Fatalf("copia-e-cola = %+v", p)
	}
	if status, _ := send(pecas.ID.String(), grant()); status != http.StatusOK || fake.sent[len(fake.sent)-1].KeyType != "BR_CODE" {
		t.Fatalf("copia-e-cola: %d", status)
	}

	// Falhou depois de enviado (a conferência, mais tarde): a conta reabre e a tarifa sai.
	paidPix := get(pecas.ID.String()).Pix
	fake.lookup[paidPix.ProviderID].Status = abacatepay.TransferFailed
	fs.SetClock(func() time.Time { return time.Now().Add(20 * time.Minute) })
	fs.WatchPix(ctx)
	fs.SetClock(time.Now)
	if e := get(pecas.ID.String()); e.Status != finance.StatusOpen || e.Pix.Status != finance.PixFailed {
		t.Fatalf("falha depois = %+v %+v", e, e.Pix)
	}
	call(http.MethodGet, "/api/finance/entries?kind=PAYABLE&status=paid&q=Tarifa", nil, http.StatusOK, &fees)
	for _, f := range fees {
		if strings.Contains(f.Description, "LOJA") || strings.Contains(f.Description, "Peças") {
			t.Errorf("tarifa do envio falho ficou: %+v", f)
		}
	}
	// O webhook manda conferir: a falha do Pix do aluguel chega por ele.
	aluguelPix := get(aluguel.ID.String()).Pix
	fake.lookup[aluguelPix.ProviderID].Status = abacatepay.TransferFailed
	if err := fs.HandleTransferWebhook(ctx, []string{aluguelPix.ProviderID, "tran_de_outra_loja"}); err != nil {
		t.Fatal(err)
	}
	if e := get(aluguel.ID.String()); e.Status != finance.StatusOpen {
		t.Errorf("aluguel depois do webhook = %+v", e)
	}
	// A conta reaberta, sem Pix vivo, pode ser excluída (as tentativas saem junto).
	env.must(admin, http.MethodDelete, "/api/finance/entries/"+errado.ID.String(), nil, http.StatusNoContent)
	env.must(admin, http.MethodDelete, "/api/finance/entries/"+pecas.ID.String(), nil, http.StatusNoContent)
}

// crcHex é o CRC do Pix copia-e-cola (o mesmo do pacote finance).
func crcHex(s string) string {
	crc := uint16(0xFFFF)
	for i := 0; i < len(s); i++ {
		crc ^= uint16(s[i]) << 8
		for range 8 {
			if crc&0x8000 != 0 {
				crc = crc<<1 ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}
	return fmt.Sprintf("%04X", crc)
}
