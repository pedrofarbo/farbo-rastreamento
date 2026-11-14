package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/closing"
)

// O fechamento do mês: o que cada pagador (o cliente e a associação) teve
// faturado no mês, pago ou não; a central registra a nota emitida com o PDF,
// exporta a planilha, e o cliente e a associação baixam só as deles.
// Precisa de FARBO_TEST_DATABASE_URL.
func TestMonthlyClosing(t *testing.T) {
	env := newCredEnvWith(t, credEnvOptions{orgs: &recordingOrgNotifier{}})
	ctx := context.Background()
	user := func(email, name, role string) *auth.User {
		t.Helper()
		u, err := env.auth.CreateUser(ctx, email, name, role, userPassword)
		if err != nil {
			t.Fatal(err)
		}
		return u
	}
	user("admin@fecha.test", "Central", auth.RoleAdmin)
	ana := user("ana@fecha.test", "Ana", auth.RoleCustomer)
	user("beto@fecha.test", "Beto", auth.RoleCustomer)
	central := env.tokenFor("admin@fecha.test")

	// A associação, com o Administrador já com senha.
	var created struct {
		Organization struct {
			ID uuid.UUID `json:"id"`
		} `json:"organization"`
	}
	_ = json.Unmarshal(env.must(central, http.MethodPost, "/api/organizations", map[string]any{
		"kind": "association", "name": "Associação Fecha", "email": "financeiro@fecha.test", "priceCents": 5000, "dueDay": 10,
		"admin": map[string]string{"name": "Ari", "email": "ari@fecha.test"},
	}, http.StatusCreated), &created)
	orgID := created.Organization.ID
	ari, _ := env.auth.FindByEmail(ctx, "ari@fecha.test")
	if _, err := env.auth.SetPassword(ctx, ari.ID, userPassword); err != nil {
		t.Fatal(err)
	}

	invoice := func(customer uuid.UUID, due string, cents int, status string) {
		t.Helper()
		if _, err := env.db.Exec(ctx, `INSERT INTO invoices (customer_id, description, amount_cents, due_date, status)
			VALUES ($1, 'linha', $2, $3, $4)`, customer, cents, due, status); err != nil {
			t.Fatal(err)
		}
	}
	// Outubro: a Ana com duas linhas (uma paga) e uma cancelada; a associação
	// com uma. Novembro não entra.
	invoice(ana.ID, "2026-10-10", 6990, "PAID")
	invoice(ana.ID, "2026-10-10", 15000, "OPEN")
	invoice(ana.ID, "2026-10-10", 999, "CANCELED")
	invoice(orgID, "2026-10-10", 10000, "OPEN")
	invoice(ana.ID, "2026-11-10", 6990, "OPEN")

	var month closing.Month
	_ = json.Unmarshal(env.must(central, http.MethodGet, "/api/closing/?mes=2026-10", nil, http.StatusOK), &month)
	if len(month.Lines) != 2 || month.AmountCents != 6990+15000+10000 || month.PaidCents != 6990 || month.MissingNotes != 2 {
		t.Fatalf("fechamento = %+v", month)
	}
	byName := map[string]*closing.Line{}
	for _, l := range month.Lines {
		byName[l.Name] = l
	}
	if a := byName["Ana"]; a == nil || a.Invoices != 2 || a.AmountCents != 21990 || a.Kind != "customer" {
		t.Errorf("Ana = %+v", a)
	}
	if o := byName["Associação Fecha"]; o == nil || o.Kind != "organization" || o.AmountCents != 10000 {
		t.Errorf("associação = %+v", o)
	}

	// A nota da Ana, com o PDF.
	saveNote := func(customer uuid.UUID, number string, file []byte, want int) closing.Note {
		t.Helper()
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		for k, v := range map[string]string{"customerId": customer.String(), "month": "2026-10", "number": number,
			"issuedOn": "2026-11-01", "amountCents": "21990"} {
			_ = form.WriteField(k, v)
		}
		if file != nil {
			part, _ := form.CreateFormFile("file", "nota-"+number+".pdf")
			_, _ = part.Write(file)
		}
		_ = form.Close()
		req, _ := http.NewRequest(http.MethodPost, env.srv.URL+"/api/closing/notes", &body)
		req.Header.Set("Authorization", "Bearer "+central)
		req.Header.Set("Content-Type", form.FormDataContentType())
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		out, _ := io.ReadAll(res.Body)
		if res.StatusCode != want {
			t.Fatalf("nota %s: %d %s", number, res.StatusCode, out)
		}
		var n closing.Note
		_ = json.Unmarshal(out, &n)
		return n
	}
	pdf := []byte("%PDF-1.4\n% nota fiscal de teste\n%%EOF")
	saveNote(ana.ID, "", pdf, http.StatusBadRequest)
	saveNote(ana.ID, "123", []byte("isto não é nota"), http.StatusBadRequest)
	note := saveNote(ana.ID, "123", pdf, http.StatusOK)
	if !note.HasFile || note.Month != "2026-10" {
		t.Fatalf("nota = %+v", note)
	}
	// Trocar o número sem mandar o arquivo de novo mantém o PDF.
	if again := saveNote(ana.ID, "124", nil, http.StatusOK); again.ID != note.ID || !again.HasFile || again.Number != "124" {
		t.Fatalf("nota trocada = %+v", again)
	}
	orgNote := saveNote(orgID, "125", pdf, http.StatusOK)

	_ = json.Unmarshal(env.must(central, http.MethodGet, "/api/closing/?mes=2026-10", nil, http.StatusOK), &month)
	if month.Notes != 2 || month.MissingNotes != 0 {
		t.Errorf("depois das notas = %+v", month)
	}
	if csv := string(env.must(central, http.MethodGet, "/api/closing/export.csv?mes=2026-10", nil, http.StatusOK)); !strings.Contains(csv, "Ana;Cliente;") ||
		!strings.Contains(csv, ";124;01/11/2026;219,90") || !strings.Contains(csv, "Associação Fecha;Associação/seguradora;") {
		t.Errorf("planilha:\n%s", csv)
	}

	// A Ana baixa a dela; o Beto, não; a associação baixa a dela.
	anaToken := env.tokenFor("ana@fecha.test")
	var mine []closing.Note
	_ = json.Unmarshal(env.must(anaToken, http.MethodGet, "/api/me/fiscal-notes", nil, http.StatusOK), &mine)
	if len(mine) != 1 || mine[0].Number != "124" {
		t.Fatalf("notas da Ana = %+v", mine)
	}
	if got := env.must(anaToken, http.MethodGet, "/api/me/fiscal-notes/"+note.ID.String()+"/file", nil, http.StatusOK); !bytes.Equal(got, pdf) {
		t.Errorf("PDF da Ana = %q", got)
	}
	env.must(anaToken, http.MethodGet, "/api/me/fiscal-notes/"+orgNote.ID.String()+"/file", nil, http.StatusNotFound)
	env.must(env.tokenFor("beto@fecha.test"), http.MethodGet, "/api/me/fiscal-notes/"+note.ID.String()+"/file", nil, http.StatusNotFound)
	ariToken := env.tokenFor("ari@fecha.test")
	env.must(ariToken, http.MethodGet, "/api/org/fiscal-notes/"+orgNote.ID.String()+"/file", nil, http.StatusOK)
	env.must(ariToken, http.MethodGet, "/api/org/fiscal-notes/"+note.ID.String()+"/file", nil, http.StatusNotFound)
	// O fechamento é da central.
	env.must(anaToken, http.MethodGet, "/api/closing/?mes=2026-10", nil, http.StatusForbidden)

	// Tirar a nota.
	env.must(central, http.MethodDelete, "/api/closing/notes/"+note.ID.String(), nil, http.StatusNoContent)
	_ = json.Unmarshal(env.must(anaToken, http.MethodGet, "/api/me/fiscal-notes", nil, http.StatusOK), &mine)
	if len(mine) != 0 {
		t.Errorf("depois de tirar = %+v", mine)
	}
}

// Com a Notaas ligada, a fatura com a nota dela (a da contratação, paga na
// hora) não pede a nota registrada à mão; nem a que ainda vai ser paga, nem
// a que não dá nota de serviço (o frete, a avulsa). Só a paga antes de a
// Notaas começar fica por emitir. Precisa de FARBO_TEST_DATABASE_URL.
func TestMonthlyClosingWithNotaas(t *testing.T) {
	env := newCredEnv(t)
	ctx := context.Background()
	user := func(email, name, role string) *auth.User {
		t.Helper()
		u, err := env.auth.CreateUser(ctx, email, name, role, userPassword)
		if err != nil {
			t.Fatal(err)
		}
		return u
	}
	user("admin@notaas.test", "Central", auth.RoleAdmin)
	central := env.tokenFor("admin@notaas.test")
	cris := user("cris@notaas.test", "Cris", auth.RoleCustomer)
	davi := user("davi@notaas.test", "Davi", auth.RoleCustomer)
	eva := user("eva@notaas.test", "Eva", auth.RoleCustomer)
	fabio := user("fabio@notaas.test", "Fabio", auth.RoleCustomer)
	gil := user("gil@notaas.test", "Gil", auth.RoleCustomer)

	if _, err := env.db.Exec(ctx, `UPDATE fiscal_settings SET started_at = '2026-11-01 10:00:00+00'`); err != nil {
		t.Fatal(err)
	}
	invoice := func(customer uuid.UUID, description string, cents int, due, status, paidAt string) uuid.UUID {
		t.Helper()
		var id uuid.UUID
		if err := env.db.QueryRow(ctx, `INSERT INTO invoices (customer_id, description, amount_cents, due_date, status, paid_at)
			VALUES ($1, $2, $3, $4, $5, NULLIF($6, '')::timestamptz) RETURNING id`, customer, description, cents, due, status, paidAt).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	document := func(invoiceID, customer uuid.UUID, status, number string, cents int) {
		t.Helper()
		if _, err := env.db.Exec(ctx, `WITH f AS (
				INSERT INTO fiscal_documents (invoice_id, customer_id, status, number, amount_cents, issued_at)
				VALUES ($1, $2, $3, $4, $5, CASE WHEN $3 = 'issued' THEN NOW() END) RETURNING id, invoice_id, amount_cents)
			INSERT INTO fiscal_document_invoices (invoice_id, document_id, amount_cents) SELECT invoice_id, id, amount_cents FROM f`,
			invoiceID, customer, status, number, cents); err != nil {
			t.Fatal(err)
		}
	}
	// A Cris contratou e pagou na hora: o frete (sem nota), a instalação
	// (nota emitida) e o proporcional (nota na prefeitura).
	invoice(cris.ID, "Frete do rastreador: PAC", 2500, "2026-11-12", "PAID", "2026-11-12 15:00:00+00")
	document(invoice(cris.ID, "Instalação do rastreador (comodato): Gol", 15000, "2026-11-12", "PAID", "2026-11-12 15:00:00+00"),
		cris.ID, "issued", "1001", 15000)
	document(invoice(cris.ID, "Plano Mensal — proporcional de 12/11 a 10/12: Gol", 6000, "2026-11-12", "PAID", "2026-11-12 15:00:00+00"),
		cris.ID, "processing", "", 6000)
	// O Davi ainda não pagou: a nota sai quando pagar.
	invoice(davi.ID, "Instalação do rastreador (instaladores Farbo): Uno", 9000, "2026-11-20", "OPEN", "")
	// A Eva pagou antes de a Notaas começar: fica por emitir.
	invoice(eva.ID, "Desinstalação do rastreador: Fusca", 8000, "2026-11-05", "PAID", "2026-10-30 12:00:00+00")
	// A nota do Fabio foi recusada (a central manda de novo pela ficha).
	document(invoice(fabio.ID, "Instalação do rastreador (comodato): Kombi", 15000, "2026-11-10", "PAID", "2026-11-10 12:00:00+00"),
		fabio.ID, "error", "", 15000)
	// O Gil pagou uma avulsa: não dá nota de serviço.
	invoice(gil.ID, "Segunda via do chip", 3000, "2026-11-08", "PAID", "2026-11-08 12:00:00+00")

	closeMonth := func() (closing.Month, map[string]*closing.Line) {
		t.Helper()
		var month closing.Month
		_ = json.Unmarshal(env.must(central, http.MethodGet, "/api/closing/?mes=2026-11", nil, http.StatusOK), &month)
		byName := map[string]*closing.Line{}
		for _, l := range month.Lines {
			byName[l.Name] = l
		}
		return month, byName
	}

	// Sem a Notaas ligada, tudo o que não tem nota dela pede a nota à mão.
	if month, _ := closeMonth(); month.MissingNotes != 4 {
		t.Fatalf("sem a Notaas: faltam %d notas, esperava 4 (Cris, Davi, Eva e Gil): %+v", month.MissingNotes, month)
	}

	env.closing.SetAutoNotes(true)
	month, byName := closeMonth()
	if len(month.Lines) != 5 || month.MissingNotes != 1 || month.AutoNotes != 1 || month.Notes != 0 {
		t.Fatalf("com a Notaas = %+v", month)
	}
	if c := byName["Cris"]; c == nil || c.NeedsNote || c.Invoices != 3 || len(c.AutoNotes) != 2 || c.AutoNotes[0].Number != "1001" ||
		c.AutoNotes[0].Status != "issued" || c.AutoNotes[1].Status != "processing" || c.Upcoming != 0 {
		t.Errorf("Cris = %+v", c)
	}
	if d := byName["Davi"]; d == nil || d.NeedsNote || d.Upcoming != 1 || len(d.AutoNotes) != 0 {
		t.Errorf("Davi = %+v", d)
	}
	if e := byName["Eva"]; e == nil || !e.NeedsNote || len(e.AutoNotes) != 0 || e.Upcoming != 0 {
		t.Errorf("Eva = %+v", e)
	}
	if f := byName["Fabio"]; f == nil || f.NeedsNote || len(f.AutoNotes) != 1 || f.AutoNotes[0].Status != "error" {
		t.Errorf("Fabio = %+v", f)
	}
	if g := byName["Gil"]; g == nil || g.NeedsNote || g.Upcoming != 0 || len(g.AutoNotes) != 0 {
		t.Errorf("Gil = %+v", g)
	}
	csv := string(env.must(central, http.MethodGet, "/api/closing/export.csv?mes=2026-11", nil, http.StatusOK))
	for _, want := range []string{";;;;1001;150,00;Saindo pela Notaas", "Davi;Cliente;", ";Saindo pela Notaas", ";Por emitir",
		";Nota da Notaas recusada", ";Sem nota de serviço"} {
		if !strings.Contains(csv, want) {
			t.Errorf("planilha sem %q:\n%s", want, csv)
		}
	}

	// A rotina da Notaas ainda não começou: como desligada.
	if _, err := env.db.Exec(ctx, `UPDATE fiscal_settings SET started_at = NULL`); err != nil {
		t.Fatal(err)
	}
	if month, _ := closeMonth(); month.MissingNotes != 4 || month.AutoNotes != 1 {
		t.Errorf("antes de a rotina começar = %+v", month)
	}
}
