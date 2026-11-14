package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/closing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/fiscal"
)

// TestFiscalDocumentsByPayment: uma NFS-e por pagamento. A associação paga
// de uma vez (o "pagar tudo") a mensalidade dos 3 veículos de novembro, a
// instalação, o frete e a mensalidade atrasada de outubro: sai uma nota de
// novembro com a quantidade de veículos e as placas (sem o frete) e outra de
// outubro (cada mês, a competência dele). A nota aparece sob cada fatura que
// cobre, vai por e-mail uma vez, conta como uma no fechamento e é cancelada
// no estorno. As baixas da central em sequência esperam 10 minutos sem outra
// e saem numa nota só. Precisa de FARBO_TEST_DATABASE_URL.
func TestFiscalDocumentsByPayment(t *testing.T) {
	notaas := &fakeNotaas{status: map[string]*fiscal.Status{}}
	mailer := &recordingFiscalMailer{}
	var svc *fiscal.Service
	var closings *closing.Service
	clock := time.Now().Add(-time.Second)
	env, _ := newLeadsEnv(t, func(d *Deps) {
		d.Config.Fiscal = config.Fiscal{NotaasAPIKey: "ntaas_teste", ServiceCode: "010700", ISSRate: 2, ISSRateSet: true,
			Description: "Licença de uso do software de rastreamento veicular Farbo"}
		svc = fiscal.NewService(d.DB, notaas, d.Config.Fiscal, mailer, d.Log)
		svc.SetClock(func() time.Time { return clock })
		d.Fiscal = svc
		closings = closing.NewService(d.DB)
		closings.SetAutoNotes(true)
		d.Closing = closings
	})
	ctx := context.Background()
	admin := env.login(auth.RoleAdmin + "@leads.test")
	id := func(query string, args ...any) uuid.UUID {
		t.Helper()
		var out uuid.UUID
		if err := env.db.QueryRow(ctx, query, args...).Scan(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := env.db.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	customer := func(name, email, document string) uuid.UUID {
		return id(`INSERT INTO users (email, name, role, password_hash, document, phone)
			VALUES ($1, $2, 'customer', 'x', $3, '(34) 98888-1234') RETURNING id`, email, name, document)
	}
	// A mensalidade de um veículo (uma assinatura); seq ordena as faturas.
	seq := 0
	monthly := func(owner uuid.UUID, vehicle, plate, description, due string) uuid.UUID {
		t.Helper()
		seq++
		v := id(`INSERT INTO vehicles (name, plate, owner_id) VALUES ($1, NULLIF($2, ''), $3) RETURNING id`, vehicle, plate, owner)
		sub := id(`INSERT INTO subscriptions (customer_id, plan_name, price_cents, due_day, next_due_date, vehicle_id)
			VALUES ($1, 'Plano Mensal', 5990, 10, '2026-12-10', $2) RETURNING id`, owner, v)
		return id(`INSERT INTO invoices (customer_id, subscription_id, description, amount_cents, due_date, created_at)
			VALUES ($1, $2, $3, 5990, $4, NOW() + make_interval(secs => $5)) RETURNING id`, owner, sub, description, due, seq)
	}
	single := func(owner uuid.UUID, description string, cents int, due string) uuid.UUID {
		t.Helper()
		seq++
		return id(`INSERT INTO invoices (customer_id, description, amount_cents, due_date, created_at)
			VALUES ($1, $2, $3, $4, NOW() + make_interval(secs => $5)) RETURNING id`, owner, description, cents, due, seq)
	}
	work := func() {
		t.Helper()
		svc.Work(ctx)
	}
	work() // marca o início

	moto := customer("Associação Moto Clube", "contato@motoclube.test", "11.222.333/0001-81")
	nov := []uuid.UUID{
		monthly(moto, "Fazer 250", "ABC1D23", "Plano Mensal — novembro/2026", "2026-11-10"),
		monthly(moto, "Gol", "DEF4G56", "Plano Mensal — novembro/2026", "2026-11-10"),
		monthly(moto, "Honda CG 160", "", "Plano Mensal — novembro/2026", "2026-11-10"),
		single(moto, "Instalação do rastreador (comodato): Gol · DEF4G56", 15000, "2026-11-10"),
	}
	freight := single(moto, "Frete do rastreador: PAC", 2500, "2026-11-10")
	oct := monthly(moto, "Biz", "JKL1M23", "Plano Mensal — outubro/2026", "2026-10-10")
	all := append(append([]uuid.UUID{}, nov...), freight, oct)

	// O "pagar tudo": um Pix só, que quita as 6 de uma vez.
	charge := id(`INSERT INTO payment_charges (invoice_id, provider, provider_charge_id, amount_cents, status, paid_at)
		VALUES ($1, 'abacatepay', 'pix_char_grupo', 50460, 'PAID', NOW()) RETURNING id`, oct)
	exec(`UPDATE invoices SET status = 'PAID', paid_at = NOW(), paid_via = 'PIX', paid_charge_id = $2 WHERE id = ANY($1)`, all, charge)
	work()

	if len(notaas.emitted) != 2 {
		t.Fatalf("emitidas = %+v", notaas.emitted)
	}
	byMonth := map[string]fiscal.EmitRequest{}
	for _, e := range notaas.emitted {
		byMonth[e.Month] = e
	}
	novNote, octNote := byMonth["2026-11"], byMonth["2026-10"]
	want := "Licença de uso do software de rastreamento veicular Farbo — Plano Mensal — novembro/2026 · 3 veículos " +
		"(ABC1D23, DEF4G56, Honda CG 160); Instalação do rastreador (comodato): Gol · DEF4G56"
	if novNote.Reference != nov[0].String() || novNote.Text != want || novNote.Total != float64(3*5990+15000)/100 ||
		novNote.Taker.CNPJ != "11222333000181" {
		t.Errorf("nota de novembro = %+v\n  texto %q", novNote, novNote.Text)
	}
	if octNote.Reference != oct.String() || octNote.Text != "Licença de uso do software de rastreamento veicular Farbo — Plano Mensal — outubro/2026" ||
		octNote.Total != 59.90 {
		t.Errorf("nota de outubro = %+v", octNote)
	}
	work() // nada de novo na fila
	if len(notaas.emitted) != 2 {
		t.Fatalf("emitiu de novo: %d", len(notaas.emitted))
	}

	// A prefeitura emitiu as duas: um e-mail de cada, com o texto da nota.
	novID, octID := "inv_"+nov[0].String()[:8], "inv_"+oct.String()[:8]
	notaas.status[novID] = &fiscal.Status{ID: novID, Status: "issued", Number: "2001"}
	notaas.status[octID] = &fiscal.Status{ID: octID, Status: "issued", Number: "2002"}
	clock = time.Now().Add(time.Hour)
	work()
	work()
	if len(mailer.sent) != 2 {
		t.Fatalf("e-mails = %+v", mailer.sent)
	}
	for _, m := range mailer.sent {
		if m.Number == "2001" && (m.AmountCents != 3*5990+15000 || m.Description != want[len("Licença de uso do software de rastreamento veicular Farbo — "):]) {
			t.Errorf("e-mail da nota de novembro = %+v", m)
		}
	}

	// A ficha: a mesma nota sob as 4 faturas de novembro; a de outubro só na dela; o frete sem nota.
	var detail struct {
		Fiscal map[string]fiscal.Document `json:"fiscal"`
	}
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/customers/"+moto.String(), nil, http.StatusOK), &detail)
	if len(detail.Fiscal) != 5 {
		t.Fatalf("notas na ficha = %+v", detail.Fiscal)
	}
	first := detail.Fiscal[nov[0].String()]
	for _, inv := range nov {
		d := detail.Fiscal[inv.String()]
		if d.ID != first.ID || d.Number != "2001" || d.Invoices != 4 || d.AmountCents != 3*5990+15000 {
			t.Errorf("nota da fatura %s = %+v", inv, d)
		}
	}
	if d := detail.Fiscal[oct.String()]; d.Number != "2002" || d.Invoices != 1 {
		t.Errorf("nota de outubro na ficha = %+v", d)
	}
	if _, ok := detail.Fiscal[freight.String()]; ok {
		t.Error("o frete ganhou nota")
	}

	// O fechamento de novembro: uma nota da Notaas, nada por emitir.
	var month closing.Month
	_ = json.Unmarshal(env.must(admin, http.MethodGet, "/api/closing/?mes=2026-11", nil, http.StatusOK), &month)
	if len(month.Lines) != 1 || len(month.Lines[0].AutoNotes) != 1 || month.Lines[0].NeedsNote || month.Lines[0].Upcoming != 0 ||
		month.AutoNotes != 1 {
		t.Errorf("fechamento = %+v", month)
	}

	// As baixas da central, uma por veículo: esperam 10 minutos sem outra e saem numa nota só.
	bruno := customer("Bruno Frota", "bruno@frota.test", "529.982.247-25")
	manual := []uuid.UUID{
		monthly(bruno, "Strada", "MNO1P23", "Plano Mensal — novembro/2026", "2026-11-10"),
		monthly(bruno, "Saveiro", "QRS4T56", "Plano Mensal — novembro/2026", "2026-11-10"),
	}
	clock = time.Now()
	env.must(admin, http.MethodPost, "/api/invoices/"+manual[0].String()+"/pay", nil, http.StatusOK)
	work()
	env.must(admin, http.MethodPost, "/api/invoices/"+manual[1].String()+"/pay", nil, http.StatusOK)
	work()
	if len(notaas.emitted) != 2 {
		t.Fatalf("a baixa saiu antes dos 10 minutos: %+v", notaas.emitted[2:])
	}
	clock = time.Now().Add(11 * time.Minute)
	work()
	if len(notaas.emitted) != 3 {
		t.Fatalf("emitidas depois das baixas = %d", len(notaas.emitted))
	}
	if e := notaas.emitted[2]; e.Reference != manual[0].String() || e.Total != 2*59.90 ||
		e.Text != "Licença de uso do software de rastreamento veicular Farbo — Plano Mensal — novembro/2026 · 2 veículos (MNO1P23, QRS4T56)" {
		t.Errorf("nota das baixas = %+v", e)
	}

	// O estorno do Pix reabre as 6 faturas: as duas notas são canceladas.
	exec(`UPDATE invoices SET status = 'OPEN', paid_at = NULL, paid_via = '', paid_charge_id = NULL WHERE id = ANY($1)`, all)
	work()
	if len(notaas.canceled) != 2 {
		t.Fatalf("cancelamentos = %v", notaas.canceled)
	}
	for _, c := range notaas.canceled {
		if c != novID && c != octID {
			t.Errorf("cancelou %s", c)
		}
	}
	// Pago de novo, nenhuma das faturas volta à fila: cada uma entra em uma nota só.
	exec(`UPDATE invoices SET status = 'PAID', paid_at = NOW(), paid_via = 'PIX', paid_charge_id = $2 WHERE id = ANY($1)`, all, charge)
	work()
	if len(notaas.emitted) != 3 {
		t.Errorf("emitiu de novo depois do estorno: %+v", notaas.emitted[3:])
	}
}
