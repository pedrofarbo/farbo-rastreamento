package maintenance

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// O vencimento anterior de um documento: o próximo menos o intervalo, sem
// passar dele na volta (fim de mês e 29 de fevereiro).
func TestDocumentStart(t *testing.T) {
	cases := []struct {
		due    string
		months int
		want   string
	}{
		{"2027-03-10", 12, "2026-03-10"},
		{"2027-03-31", 1, "2027-02-28"},
		{"2028-02-29", 12, "2027-02-28"},
		{"2031-05-20", 60, "2026-05-20"},
	}
	for _, c := range cases {
		due, _ := time.Parse(time.DateOnly, c.due)
		if got := documentStart(due, c.months).Format(time.DateOnly); got != c.want {
			t.Errorf("documentStart(%s, %d) = %s, quer %s", c.due, c.months, got, c.want)
		}
	}
}

// Renovar passa para o vencimento seguinte: adiantado ou atrasado, o
// calendário do documento fica o mesmo; muito atrasado, pula os ciclos.
func TestRenewedStart(t *testing.T) {
	cases := []struct {
		label, last, done, want string
	}{
		{"adiantado", "2026-02-10", "2027-01-20", "2027-02-10"},
		{"no dia", "2026-02-10", "2027-02-10", "2027-02-10"},
		{"atrasado", "2026-02-10", "2027-05-02", "2027-02-10"},
		{"dois anos atrasado", "2025-02-10", "2027-05-02", "2027-02-10"},
	}
	for _, c := range cases {
		if got := renewedStart(c.last, 12, c.done); got != c.want {
			t.Errorf("%s: renewedStart = %s, quer %s", c.label, got, c.want)
		}
	}
}

// Documento: só por data, avisa a 30 dias e diz a data do vencimento.
func TestEvaluateDocument(t *testing.T) {
	today := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		label  string
		last   string
		status string
		detail string
	}{
		{"em dia", "2026-01-10", StatusOK, "vence em 10/01/2027 (faltam 90 dias)"},
		{"perto (30 dias)", "2025-11-05", StatusSoon, "vence em 05/11/2026 (faltam 24 dias)"},
		{"vence hoje", "2025-10-12", StatusDue, "vence hoje"},
		{"vencido", "2025-10-01", StatusDue, "venceu há 11 dias"},
	}
	for _, c := range cases {
		it := Item{Kind: KindDocument, EveryMonths: ptr(12), LastDate: c.last}
		it.evaluate(Odometer{}, today)
		if it.Status != c.status || Describe(&it) != c.detail {
			t.Errorf("%s: %s %q (quer %s %q)", c.label, it.Status, Describe(&it), c.status, c.detail)
		}
	}
}

// O documento de ponta a ponta: criado pelo vencimento, sem km; renovado,
// vai para o vencimento seguinte; editado, troca o vencimento.
func TestDocumentEndToEnd(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	var owner, vehicle uuid.UUID
	if err := db.QueryRow(ctx, `INSERT INTO users (email, name, role, password_hash) VALUES ('doc@manutencao.test', 'Doc', 'customer', 'x') RETURNING id`).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `INSERT INTO vehicles (name, owner_id) VALUES ('Carro', $1) RETURNING id`, owner).Scan(&vehicle); err != nil {
		t.Fatal(err)
	}
	svc := NewService(db, "America/Sao_Paulo")
	loc, _ := time.LoadLocation("America/Sao_Paulo")
	svc.SetClock(func() time.Time { return time.Date(2026, 10, 12, 10, 0, 0, 0, loc) })

	var input *InputError
	bad := []ItemInput{
		{Kind: KindDocument, Name: "IPVA", EveryMonths: ptr(12)},
		{Kind: KindDocument, Name: "IPVA", EveryMonths: ptr(12), EveryKm: ptr(10000), DueDate: "2027-02-10"},
		{Kind: KindDocument, Name: "IPVA", DueDate: "2027-02-10"},
		{Kind: KindDocument, Name: "IPVA", EveryMonths: ptr(12), DueDate: "2040-02-10"},
		{Kind: "outro", Name: "IPVA", EveryMonths: ptr(12)},
	}
	for _, in := range bad {
		if _, err := svc.Create(ctx, []uuid.UUID{vehicle}, in); !errors.As(err, &input) {
			t.Errorf("%+v: %v", in, err)
		}
	}
	// O vencimento pode estar mais longe que o intervalo (a CNH que vence
	// daqui a 3 anos, renovada a cada 5).
	if _, err := svc.Create(ctx, []uuid.UUID{vehicle}, ItemInput{Kind: KindDocument, Name: "IPVA", EveryMonths: ptr(12), DueDate: "2026-11-05"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, []uuid.UUID{vehicle}, ItemInput{Kind: KindDocument, Name: "CNH", EveryMonths: ptr(60), DueDate: "2029-08-01"}); err != nil {
		t.Fatal(err)
	}
	item := func(name string) *Item {
		t.Helper()
		list, err := svc.Items(ctx, []uuid.UUID{vehicle})
		if err != nil {
			t.Fatal(err)
		}
		for _, it := range list {
			if it.Name == name {
				return it
			}
		}
		t.Fatalf("%s sumiu", name)
		return nil
	}
	ipva := item("IPVA")
	if ipva.Kind != KindDocument || *ipva.NextDate != "2026-11-05" || ipva.Status != StatusSoon || ipva.LastKm != nil {
		t.Errorf("IPVA = %+v", ipva)
	}
	if cnh := item("CNH"); *cnh.NextDate != "2029-08-01" || cnh.Status != StatusOK {
		t.Errorf("CNH = %+v", cnh)
	}
	// Pago adiantado: o próximo é o de 2027 (não um ano depois do pagamento).
	if _, err := svc.Done(ctx, DoneInput{ItemIDs: []uuid.UUID{ipva.ID}, Date: "2026-10-12"}, owner); err != nil {
		t.Fatal(err)
	}
	if ipva := item("IPVA"); *ipva.NextDate != "2027-11-05" || ipva.Status != StatusOK {
		t.Errorf("IPVA renovado = %+v", ipva)
	}
	// Editar troca o vencimento.
	if err := svc.Update(ctx, ipva.ID, ItemInput{Kind: KindDocument, Name: "IPVA 2027", EveryMonths: ptr(12), DueDate: "2027-03-15"}); err != nil {
		t.Fatal(err)
	}
	if ipva := item("IPVA 2027"); *ipva.NextDate != "2027-03-15" {
		t.Errorf("IPVA editado = %+v", ipva)
	}
}
