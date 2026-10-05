package finance

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
)

func TestAddMonths(t *testing.T) {
	cases := []struct {
		from   billing.Date
		months int
		want   string
	}{
		{billing.NewDate(2026, 1, 31), 1, "2026-02-28"},
		{billing.NewDate(2028, 1, 31), 1, "2028-02-29"}, // ano bissexto
		{billing.NewDate(2026, 1, 31), 2, "2026-03-31"},
		{billing.NewDate(2026, 10, 15), 3, "2027-01-15"},
		{billing.NewDate(2026, 12, 30), 2, "2027-02-28"},
		{billing.NewDate(2026, 5, 10), 0, "2026-05-10"},
	}
	for _, c := range cases {
		if got := AddMonths(c.from, c.months).String(); got != c.want {
			t.Errorf("AddMonths(%s, %d) = %s, quer %s", c.from, c.months, got, c.want)
		}
	}
}

func TestSplitInstallments(t *testing.T) {
	got := SplitInstallments(100000, 3)
	if len(got) != 3 || got[0] != 33334 || got[1] != 33333 || got[2] != 33333 {
		t.Fatalf("1000,00 em 3 = %v", got)
	}
	if got := SplitInstallments(5000, 1); len(got) != 1 || got[0] != 5000 {
		t.Fatalf("à vista = %v", got)
	}
	var sum int64
	for _, v := range SplitInstallments(99999, 7) {
		sum += v
	}
	if sum != 99999 {
		t.Fatalf("as parcelas somam %d", sum)
	}
}

func TestAverageCost(t *testing.T) {
	// Estoque vazio: o custo da compra.
	if got := AverageCost(0, 0, 10, 10000); got != 10000 {
		t.Errorf("primeira compra = %d", got)
	}
	// 10 a R$ 100 + 10 a R$ 120 = R$ 110.
	if got := AverageCost(10, 10000, 10, 12000); got != 11000 {
		t.Errorf("média = %d", got)
	}
	// Arredonda para o centavo mais próximo: (3×100 + 1×101) / 4 = 100,25.
	if got := AverageCost(3, 100, 1, 101); got != 100 {
		t.Errorf("arredondamento = %d", got)
	}
	if got := AverageCost(1, 100, 1, 101); got != 101 { // 100,5 → 101
		t.Errorf("meio centavo = %d", got)
	}
}

func TestAttachmentType(t *testing.T) {
	cases := map[string]string{
		"%PDF-1.7\n...":                    "application/pdf",
		"\x89PNG\r\n\x1a\n....":            "image/png",
		"\xFF\xD8\xFF\xE0..JFIF":           "image/jpeg",
		"<html><script>alert(1)</script>":  "",
		"%PD":                              "",
		"GIF89a":                           "",
		"PK\x03\x04 (zip, docx, xlsx...)":  "",
		"MZ\x90\x00 (programa do Windows)": "",
	}
	for data, want := range cases {
		if got := AttachmentType([]byte(data)); got != want {
			t.Errorf("AttachmentType(%q) = %q, quer %q", data, got, want)
		}
	}
}

func TestCleanFilename(t *testing.T) {
	cases := map[string]string{
		"boleto-outubro.pdf":           "boleto-outubro.pdf",
		`C:\Users\ana\nota fiscal.pdf`: "nota fiscal.pdf",
		"../../etc/passwd":             "passwd",
		"nota\"\x00\r\n.pdf":           "nota.pdf",
		"   ":                          "anexo",
	}
	for in, want := range cases {
		if got := CleanFilename(in); got != want {
			t.Errorf("CleanFilename(%q) = %q, quer %q", in, got, want)
		}
	}
}

func TestBRL(t *testing.T) {
	cases := map[int64]string{0: "R$ 0,00", 5: "R$ 0,05", 3490: "R$ 34,90", 123456: "R$ 1.234,56",
		100000000: "R$ 1.000.000,00", -2790: "-R$ 27,90"}
	for cents, want := range cases {
		if got := BRL(cents); got != want {
			t.Errorf("BRL(%d) = %q, quer %q", cents, got, want)
		}
	}
}

func message(err error) string {
	var v ValidationError
	if errors.As(err, &v) {
		return v.Message
	}
	return ""
}

func TestEntryValidation(t *testing.T) {
	cat := uuid.New()
	due := billing.NewDate(2026, 10, 20)
	ok := EntryInput{Kind: KindPayable, Description: " Aluguel ", CategoryID: cat, AmountCents: 150000, DueDate: due}
	got, err := ok.Normalize()
	if err != nil || got.Description != "Aluguel" || got.Installments != 1 {
		t.Fatalf("válido = %+v, %v", got, err)
	}
	paid := billing.NewDate(2026, 10, 1)
	for name, in := range map[string]EntryInput{
		"sem tipo":            {Description: "x", CategoryID: cat, AmountCents: 1, DueDate: due},
		"sem descrição":       {Kind: KindPayable, CategoryID: cat, AmountCents: 1, DueDate: due},
		"sem categoria":       {Kind: KindPayable, Description: "x", AmountCents: 1, DueDate: due},
		"valor zero":          {Kind: KindPayable, Description: "x", CategoryID: cat, DueDate: due},
		"sem vencimento":      {Kind: KindPayable, Description: "x", CategoryID: cat, AmountCents: 1},
		"parcelas demais":     {Kind: KindPayable, Description: "x", CategoryID: cat, AmountCents: 10000, DueDate: due, Installments: 61},
		"parcela de centavos": {Kind: KindPayable, Description: "x", CategoryID: cat, AmountCents: 2, DueDate: due, Installments: 3},
		"parcelado já pago":   {Kind: KindPayable, Description: "x", CategoryID: cat, AmountCents: 300, DueDate: due, Installments: 3, PaidOn: &paid},
		"forma inventada":     {Kind: KindPayable, Description: "x", CategoryID: cat, AmountCents: 1, DueDate: due, PaymentMethod: "CHEQUE"},
	} {
		if _, err := in.Normalize(); message(err) == "" {
			t.Errorf("%s: devia recusar, veio %v", name, err)
		}
	}
}

func TestPayValidation(t *testing.T) {
	today := billing.NewDate(2026, 10, 15)
	got, err := PayInput{}.Normalize(4990, today)
	if err != nil || got.PaidCents != 4990 || got.PaidOn != today {
		t.Fatalf("sem nada = baixa de hoje pelo valor: %+v %v", got, err)
	}
	if _, err := (PayInput{PaidOn: today.AddDays(1)}).Normalize(4990, today); message(err) == "" {
		t.Error("pagamento no futuro devia ser recusado")
	}
	if _, err := (PayInput{Method: "FIADO"}).Normalize(4990, today); message(err) == "" {
		t.Error("forma inventada devia ser recusada")
	}
}

func TestMovementValidation(t *testing.T) {
	today := billing.NewDate(2026, 10, 15)
	item := uuid.New()
	in, err := MovementInput{ItemID: item, Type: MoveOut, Quantity: 3, UnitCostCents: 999}.Normalize(today)
	if err != nil || in.UnitCostCents != 0 || in.OccurredOn != today || in.signedQuantity() != -3 {
		t.Fatalf("saída = %+v %v (sai pelo custo médio, hoje, negativa)", in, err)
	}
	if in, _ := (MovementInput{ItemID: item, Type: MoveAdjust, Quantity: -2}).Normalize(today); in.signedQuantity() != -2 {
		t.Errorf("acerto negativo = %d", in.signedQuantity())
	}
	for name, m := range map[string]MovementInput{
		"entrada sem custo": {ItemID: item, Type: MoveIn, Quantity: 1},
		"saída negativa":    {ItemID: item, Type: MoveOut, Quantity: -1},
		"quantidade zero":   {ItemID: item, Type: MoveLoss},
		"conta numa saída":  {ItemID: item, Type: MoveOut, Quantity: 1, Payable: &PurchasePayable{}},
		"no futuro":         {ItemID: item, Type: MoveLoss, Quantity: 1, OccurredOn: today.AddDays(1)},
		"tipo inventado":    {ItemID: item, Type: "SWAP", Quantity: 1},
		"sem item":          {Type: MoveLoss, Quantity: 1},
	} {
		if _, err := m.Normalize(today); message(err) == "" {
			t.Errorf("%s: devia recusar, veio %v", name, err)
		}
	}
}

func TestRecurrenceAndCategoryValidation(t *testing.T) {
	cat := uuid.New()
	if _, err := (RecurrenceInput{Kind: KindPayable, Description: "Aluguel", CategoryID: cat, AmountCents: 1,
		FirstDueDate: billing.NewDate(2026, 10, 30)}).Normalize(); message(err) == "" {
		t.Error("dia 30 devia ser recusado (não existe em fevereiro)")
	}
	end := billing.NewDate(2026, 9, 1)
	if _, err := (RecurrenceInput{Kind: KindPayable, Description: "Aluguel", CategoryID: cat, AmountCents: 1,
		FirstDueDate: billing.NewDate(2026, 10, 5), EndsOn: &end}).Normalize(); message(err) == "" {
		t.Error("fim antes do começo devia ser recusado")
	}
	if _, kind, err := (CategoryInput{Name: "Combustível", Group: GroupOperating}).Normalize(); err != nil || kind != CategoryExpense {
		t.Errorf("categoria de despesa = %s %v", kind, err)
	}
	if _, kind, _ := (CategoryInput{Name: "Aporte", Group: GroupCapitalIn}).Normalize(); kind != CategoryIncome {
		t.Errorf("aporte devia ser receita, veio %s", kind)
	}
	if _, _, err := (CategoryInput{Name: "X", Group: "LUCRO"}).Normalize(); message(err) == "" {
		t.Error("linha inventada devia ser recusada")
	}
	if _, err := (SupplierInput{Name: "Vivo", Document: "02.558.157/0001-62"}).Normalize(); err != nil {
		t.Errorf("CNPJ com máscara: %v", err)
	}
	if _, err := (SupplierInput{Name: "Vivo", Document: "123"}).Normalize(); message(err) == "" {
		t.Error("documento curto devia ser recusado")
	}
}
