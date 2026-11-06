package mail

import (
	"strings"
	"testing"
)

// A nota fiscal emitida: o número no assunto, o valor, o que foi pago e o
// link do PDF; sem o PDF, o link das faturas.
func TestRenderFiscalIssued(t *testing.T) {
	d := FiscalDocument{To: "ana@cliente.test", Name: "Ana Souza", Number: "1234", AmountCents: 6990,
		Description: "Plano Mensal — novembro/2026", PDFURL: "https://documents.notaas.test/pdf/inv_1.pdf"}
	msg, err := renderFiscalIssued(d, "https://painel.farbo.test")
	if err != nil {
		t.Fatal(err)
	}
	if msg.Subject != "Nota fiscal nº 1234 da Farbo Rastreadores" {
		t.Errorf("assunto = %q", msg.Subject)
	}
	for _, want := range []string{"Olá, Ana!", "R$ 69,90", "Plano Mensal — novembro/2026", "NFS-e nº 1234",
		"https://documents.notaas.test/pdf/inv_1.pdf"} {
		if !strings.Contains(msg.Text, want) || !strings.Contains(msg.HTML, strings.ReplaceAll(want, "Olá, Ana!", "Ana")) {
			t.Errorf("sem %q:\n%s", want, msg.Text)
		}
	}
	d.PDFURL, d.Number = "", ""
	plain, _ := renderFiscalIssued(d, "https://painel.farbo.test")
	if plain.Subject != "Sua nota fiscal da Farbo Rastreadores" || !strings.Contains(plain.Text, "https://painel.farbo.test/faturas") {
		t.Errorf("sem PDF = %q\n%s", plain.Subject, plain.Text)
	}
}
