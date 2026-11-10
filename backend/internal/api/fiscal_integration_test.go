package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/contract"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/fiscal"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/mail"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/orders"
)

// fakeNotaas faz o papel da Notaas: guarda o que foi emitido e responde a
// consulta com a situação que o teste define.
type fakeNotaas struct {
	mu       sync.Mutex
	emitted  []fiscal.EmitRequest
	status   map[string]*fiscal.Status
	refuse   *fiscal.APIError
	canceled []string
	tries    int // cada chamada da emissão, recusada ou não
}

func (f *fakeNotaas) Emit(_ context.Context, req fiscal.EmitRequest) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tries++
	if f.refuse != nil {
		return "", f.refuse
	}
	f.emitted = append(f.emitted, req)
	id := "inv_" + req.Reference[:8]
	f.status[id] = &fiscal.Status{ID: id, Status: "processing"}
	return id, nil
}

func (f *fakeNotaas) Status(_ context.Context, id string) (*fiscal.Status, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status[id], nil
}

func (f *fakeNotaas) Cancel(_ context.Context, id, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.canceled = append(f.canceled, id)
	return nil
}

func (f *fakeNotaas) PDF(_ context.Context, id string) (*fiscal.File, error) {
	return &fiscal.File{Body: []byte("%PDF-1.4 " + id)}, nil
}

func (f *fakeNotaas) XML(_ context.Context, id string) (*fiscal.File, error) {
	return &fiscal.File{Body: []byte(`<?xml version="1.0" encoding="utf-8"?><NFSe><nNFSe>` + id + `</nNFSe></NFSe>`)}, nil
}

type recordingFiscalMailer struct {
	mu        sync.Mutex
	sent      []mail.FiscalDocument
	credits   []mail.FiscalCredits
	creditsTo [][]string
}

func (m *recordingFiscalMailer) FiscalCreditsOver(_ context.Context, to []string, d mail.FiscalCredits) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.credits = append(m.credits, d)
	m.creditsTo = append(m.creditsTo, to)
	return nil
}

func (m *recordingFiscalMailer) FiscalIssued(_ context.Context, d mail.FiscalDocument) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, d)
	return nil
}

// A NFS-e das faturas pagas, pela Notaas: só as pagas depois de ligar; a
// mensalidade pelo valor cheio (com a parcela de um rastreador comprado
// antes), não o rastreador vendido nem o frete; o tomador com o CPF e o endereço; a nota emitida vai por e-mail
// uma vez e aparece na ficha e no app (o PDF só do dono); sem CPF, a nota
// fica recusada até a central emitir de novo; sem créditos na Notaas, a
// fila espera (um aviso à central, uma nota tentando a cada 15 minutos) e
// sai toda quando o crédito volta; a fatura estornada cancela a nota; o webhook
// assinado só faz consultar de novo. Precisa de FARBO_TEST_DATABASE_URL.
func TestFiscalDocuments(t *testing.T) {
	notaas := &fakeNotaas{status: map[string]*fiscal.Status{}}
	mailer := &recordingFiscalMailer{}
	var svc *fiscal.Service
	start := time.Now().Add(-time.Second)
	env, _ := newLeadsEnv(t, func(d *Deps) {
		d.Config.Catalog.SetupDueDays = 0
		d.Orders = orders.NewService(d.DB, d.Billing, d.Vehicles, d.Config.Catalog, d.Log)
		doc, err := contract.Render(contract.Params{Company: config.Company{Name: "Farbo Rastreadores"}, HistoryOptions: []int{7, 14, 30}})
		if err != nil {
			t.Fatal(err)
		}
		c := contract.NewService(d.DB, doc, &recordingContractMailer{}, d.Log)
		c.SetSync()
		d.Contract = c
		d.Config.Fiscal = config.Fiscal{NotaasAPIKey: "ntaas_teste", NotaasWebhookSecret: "segredo-do-webhook", ServiceCode: "010700",
			ISSRate: 2, ISSRateSet: true, Description: "Licença de uso do software de rastreamento veicular Farbo"}
		svc = fiscal.NewService(d.DB, notaas, d.Config.Fiscal, mailer, d.Log)
		svc.SetClock(func() time.Time { return start })
		d.Fiscal = svc
	})
	ctx := context.Background()
	admin := env.login(auth.RoleAdmin + "@leads.test")

	var o struct {
		ContractVersion string `json:"contractVersion"`
	}
	_ = json.Unmarshal(env.must("", http.MethodGet, "/api/public/checkout", nil, http.StatusOK), &o)
	address := map[string]any{"zipCode": "01305-000", "street": "Rua Augusta", "number": "500", "district": "Consolação",
		"city": "São Paulo", "state": "SP"}
	type placed struct {
		Subscription struct {
			ID uuid.UUID `json:"id"`
		} `json:"subscription"`
		Tokens struct {
			AccessToken string `json:"accessToken"`
			User        struct {
				ID uuid.UUID `json:"id"`
			} `json:"user"`
		} `json:"tokens"`
	}
	checkout := func(name, email, document string) placed {
		t.Helper()
		var p placed
		_ = json.Unmarshal(env.must("", http.MethodPost, "/api/public/checkout", map[string]any{
			"name": name, "email": email, "phone": "(11) 95555-0000", "document": document, "password": "senha-forte-1",
			"vehicle": map[string]any{"kind": "CAR", "brand": "Fiat", "model": "Uno", "plate": ""}, "address": address,
			"contractVersion": o.ContractVersion, "accept": true,
		}, http.StatusCreated), &p)
		return p
	}
	pay := func(customer uuid.UUID) {
		t.Helper()
		if _, err := env.db.Exec(ctx, `UPDATE invoices SET status = 'PAID', paid_at = NOW(), paid_via = 'PIX' WHERE customer_id = $1
			AND status = 'OPEN'`, customer); err != nil {
			t.Fatal(err)
		}
	}
	work := func() {
		t.Helper()
		svc.Work(ctx)
	}

	// Paga antes de ligar a emissão: sem nota retroativa.
	before := checkout("Bia Antes", "bia@fiscal.test", "390.533.447-05")
	if _, err := env.db.Exec(ctx, `UPDATE invoices SET status = 'PAID', paid_at = $2 WHERE customer_id = $1`,
		before.Tokens.User.ID, start.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}

	ana := checkout("Ana Souza", "ana@fiscal.test", "529.982.247-25")
	// A mensalidade traz R$ 10,00 da parcela de um rastreador comprado antes:
	// a nota sai pelo valor cheio do mês.
	var monthly uuid.UUID
	var monthlyCents int
	if err := env.db.QueryRow(ctx, `SELECT id, amount_cents FROM invoices WHERE subscription_id = $1`, ana.Subscription.ID).
		Scan(&monthly, &monthlyCents); err != nil {
		t.Fatal(err)
	}
	if _, err := env.db.Exec(ctx, `UPDATE invoices SET amount_cents = amount_cents + 1000, description = description || ' + parcela 1/10 do rastreador'
		WHERE id = $1`, monthly); err != nil {
		t.Fatal(err)
	}
	if _, err := env.db.Exec(ctx, `INSERT INTO equipment_installments (subscription_id, number, amount_cents, invoice_id)
		VALUES ($1, 1, 1000, $2)`, ana.Subscription.ID, monthly); err != nil {
		t.Fatal(err)
	}
	monthlyCents += 1000
	work() // marca o início
	pay(ana.Tokens.User.ID)
	work()

	if len(notaas.emitted) != 1 {
		t.Fatalf("emitidas = %+v", notaas.emitted)
	}
	e := notaas.emitted[0]
	if e.Reference != monthly.String() || e.Taker.CPF != "52998224725" || e.Taker.Name != "Ana Souza" || e.Taker.Address == nil ||
		e.Taker.Address.City != "São Paulo" || e.Total != float64(monthlyCents)/100 || e.ISSRate != 2 || e.Code != "010700" ||
		!strings.HasPrefix(e.Text, "Licença de uso do software de rastreamento veicular Farbo — Plano Mensal") {
		t.Errorf("emissão = %+v (taker %+v)", e, e.Taker)
	}
	// A nota é de serviço: sem a parcela do rastreador no texto (o valor é o do mês inteiro).
	if strings.Contains(e.Text, "parcela") || strings.Contains(e.Text, "do rastreador") {
		t.Errorf("a nota fala do rastreador: %q", e.Text)
	}

	// A prefeitura emitiu: a nota fica emitida e vai por e-mail uma vez.
	id := "inv_" + monthly.String()[:8]
	notaas.status[id] = &fiscal.Status{ID: id, Status: "issued", Number: "1234", VerificationCode: "3526",
		PDFURL: "https://documents.notaas.test/pdf/" + id + ".pdf"}
	svc.SetClock(func() time.Time { return time.Now().Add(time.Hour) })
	work()
	work()
	if len(mailer.sent) != 1 || mailer.sent[0].Number != "1234" || mailer.sent[0].To != "ana@fiscal.test" ||
		mailer.sent[0].AmountCents != monthlyCents {
		t.Fatalf("e-mails = %+v", mailer.sent)
	}

	// A ficha e o app mostram a nota; o PDF só para o dono.
	var detail struct {
		Fiscal map[string]fiscal.Document `json:"fiscal"`
	}
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/customers/"+ana.Tokens.User.ID.String(), nil, http.StatusOK), &detail)
	note, ok := detail.Fiscal[monthly.String()]
	if !ok || note.Status != fiscal.StatusIssued || note.Number != "1234" || !note.HasPDF || !note.HasXML || len(detail.Fiscal) != 1 {
		t.Fatalf("ficha = %+v", detail.Fiscal)
	}
	var mine map[string]fiscal.Document
	_ = json.Unmarshal(env.must(ana.Tokens.AccessToken, http.MethodGet, "/api/me/fiscal-documents", nil, http.StatusOK), &mine)
	if mine[monthly.String()].Number != "1234" {
		t.Errorf("app = %+v", mine)
	}
	status, out := env.do(ana.Tokens.AccessToken, http.MethodGet, "/api/me/fiscal-documents/"+note.ID.String()+"/pdf", nil)
	if status != http.StatusOK || !strings.HasPrefix(string(out), "%PDF") {
		t.Errorf("PDF da dona = %d %q", status, out)
	}
	status, _ = env.do(admin, http.MethodGet, "/api/fiscal-documents/"+note.ID.String()+"/pdf", nil)
	if status != http.StatusOK {
		t.Errorf("PDF pela central = %d", status)
	}
	// O XML, além do PDF: para a dona e para a central (com o nome do arquivo pelo número).
	xmlOf := func(token, path string) (int, string, http.Header) {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, env.srv.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(raw), resp.Header
	}
	code, xmlBody, header := xmlOf(ana.Tokens.AccessToken, "/api/me/fiscal-documents/"+note.ID.String()+"/xml")
	if code != http.StatusOK || !strings.HasPrefix(xmlBody, "<?xml") || header.Get("Content-Type") != "application/xml" ||
		header.Get("Content-Disposition") != `attachment; filename="nfse-1234.xml"` {
		t.Errorf("XML da dona = %d %q %v", code, xmlBody, header)
	}
	if code, _, _ := xmlOf(admin, "/api/fiscal-documents/"+note.ID.String()+"/xml"); code != http.StatusOK {
		t.Errorf("XML pela central = %d", code)
	}

	// Sem CPF no cadastro: recusada até a central emitir de novo.
	caio := checkout("Caio Sem Doc", "caio@fiscal.test", "111.444.777-35")
	if _, err := env.db.Exec(ctx, `UPDATE users SET document = '' WHERE id = $1`, caio.Tokens.User.ID); err != nil {
		t.Fatal(err)
	}
	pay(caio.Tokens.User.ID)
	work()
	var caioDetail struct {
		Fiscal map[string]fiscal.Document `json:"fiscal"`
	}
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/customers/"+caio.Tokens.User.ID.String(), nil, http.StatusOK), &caioDetail)
	if len(caioDetail.Fiscal) != 1 {
		t.Fatalf("notas do Caio = %+v", caioDetail.Fiscal)
	}
	var refused fiscal.Document
	for _, d := range caioDetail.Fiscal {
		refused = d
	}
	if refused.Status != fiscal.StatusError || !strings.Contains(refused.Error, "sem CPF ou CNPJ") {
		t.Fatalf("sem CPF = %+v", refused)
	}
	// Com o CPF, mas sem créditos na Notaas: espera os créditos.
	if _, err := env.db.Exec(ctx, `UPDATE users SET document = '11144477735' WHERE id = $1`, caio.Tokens.User.ID); err != nil {
		t.Fatal(err)
	}
	env.must(admin, http.MethodPost, "/api/fiscal-documents/"+refused.ID.String()+"/retry", nil, http.StatusOK)
	env.must(admin, http.MethodPost, "/api/fiscal-documents/"+note.ID.String()+"/retry", nil, http.StatusConflict)
	clock := time.Now().Add(time.Hour)
	svc.SetClock(func() time.Time { return clock })
	notaas.refuse = &fiscal.APIError{Status: http.StatusForbidden, Code: "CREDIT_LIMIT", Message: "Créditos esgotados"}
	work()
	statusOf := func(id uuid.UUID) (status, reason string) {
		t.Helper()
		_ = env.db.QueryRow(ctx, `SELECT status, error FROM fiscal_documents WHERE id = $1`, id).Scan(&status, &reason)
		return status, reason
	}
	if st, reason := statusOf(refused.ID); st != fiscal.StatusBlocked || !strings.Contains(reason, "coloque créditos") {
		t.Errorf("sem créditos = %s (%s)", st, reason)
	}
	if len(mailer.credits) != 1 || mailer.credits[0].Waiting != 1 ||
		!slices.Contains(mailer.creditsTo[0], auth.RoleAdmin+"@leads.test") {
		t.Errorf("aviso dos créditos = %+v para %v", mailer.credits, mailer.creditsTo)
	}
	tries := notaas.tries
	work() // antes dos 15 minutos: ninguém tenta
	if notaas.tries != tries {
		t.Errorf("tentou antes da hora: %d", notaas.tries-tries)
	}
	// Uma fatura paga enquanto isso: tenta, espera junto, sem outro aviso.
	duda := checkout("Duda Depois", "duda@fiscal.test", "935.411.347-80")
	pay(duda.Tokens.User.ID)
	work()
	var dudaNote uuid.UUID
	_ = env.db.QueryRow(ctx, `SELECT id FROM fiscal_documents WHERE customer_id = $1`, duda.Tokens.User.ID).Scan(&dudaNote)
	if st, _ := statusOf(dudaNote); st != fiscal.StatusBlocked || len(mailer.credits) != 1 {
		t.Errorf("paga sem créditos = %s, avisos %d", st, len(mailer.credits))
	}
	// Passados 15 minutos, só a mais antiga tenta de novo.
	tries = notaas.tries
	clock = clock.Add(16 * time.Minute)
	work()
	if notaas.tries != tries+1 || len(mailer.credits) != 1 {
		t.Errorf("nova tentativa: %d chamadas, %d avisos", notaas.tries-tries, len(mailer.credits))
	}
	// Com crédito de novo: a que tenta sai, e as outras saem atrás.
	notaas.refuse = nil
	clock = clock.Add(16 * time.Minute)
	work()
	work()
	for _, id := range []uuid.UUID{refused.ID, dudaNote} {
		if st, reason := statusOf(id); st != fiscal.StatusProcessing || reason != "" {
			t.Errorf("com crédito = %s (%s)", st, reason)
		}
	}

	// O estorno da mensalidade cancela a nota emitida.
	if _, err := env.db.Exec(ctx, `UPDATE invoices SET status = 'OPEN', paid_at = NULL WHERE id = $1`, monthly); err != nil {
		t.Fatal(err)
	}
	work()
	if len(notaas.canceled) != 1 || notaas.canceled[0] != id {
		t.Fatalf("cancelamentos = %v", notaas.canceled)
	}
	notaas.status[id] = &fiscal.Status{ID: id, Status: "cancelled"}
	// O webhook assinado só faz consultar de novo; sem assinatura, recusado.
	body := []byte(`{"event":"nfse.cancelled","data":{"invoiceId":"` + id + `"}}`)
	mac := hmac.New(sha256.New, []byte("segredo-do-webhook"))
	mac.Write(body)
	status, _ = env.doWith("", http.MethodPost, "/api/fiscal/notaas/webhook", json.RawMessage(body), map[string]string{
		"X-Notaas-Signature": "sha256=" + hex.EncodeToString(mac.Sum(nil)),
	})
	if status != http.StatusOK {
		t.Errorf("webhook assinado = %d", status)
	}
	if status, _ := env.do("", http.MethodPost, "/api/fiscal/notaas/webhook", json.RawMessage(body)); status != http.StatusUnauthorized {
		t.Errorf("webhook sem assinatura = %d", status)
	}
	work()
	var final string
	_ = env.db.QueryRow(ctx, `SELECT status FROM fiscal_documents WHERE id = $1`, note.ID).Scan(&final)
	if final != fiscal.StatusCancelled {
		t.Errorf("depois do estorno = %s", final)
	}
	// Cancelada, o XML (de emissão) continua para baixar; o PDF, não.
	var cancelled map[string]fiscal.Document
	_ = json.Unmarshal(env.must(ana.Tokens.AccessToken, http.MethodGet, "/api/me/fiscal-documents", nil, http.StatusOK), &cancelled)
	if d := cancelled[monthly.String()]; !d.HasXML || d.HasPDF {
		t.Errorf("cancelada = %+v", d)
	}
	if status, _ := env.do(ana.Tokens.AccessToken, http.MethodGet, "/api/me/fiscal-documents/"+note.ID.String()+"/xml", nil); status != http.StatusOK {
		t.Errorf("XML da cancelada = %d", status)
	}

	// A Bia pagou antes de ligar: nenhuma nota; o rastreador vendido e o frete nunca.
	var count, products int
	_ = env.db.QueryRow(ctx, `SELECT count(*) FROM fiscal_documents WHERE customer_id = $1`, before.Tokens.User.ID).Scan(&count)
	_ = env.db.QueryRow(ctx, `SELECT count(*) FROM fiscal_documents f JOIN invoices i ON i.id = f.invoice_id
		WHERE i.description LIKE 'Rastreador%' OR i.description LIKE 'Frete%'`).Scan(&products)
	if count != 0 || products != 0 {
		t.Errorf("notas indevidas: antes de ligar %d, produto ou frete %d", count, products)
	}
}
