package mail

import (
	"context"
	"strings"
	"testing"
)

// listSender guarda todas as mensagens enviadas.
type listSender struct{ sent []Message }

func (s *listSender) Send(_ context.Context, m Message) error {
	s.sent = append(s.sent, m)
	return nil
}

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

// O aviso de que os créditos da Notaas acabaram: quantas notas esperam, o
// que fazer e o link do painel da Notaas, para cada administrador.
func TestFiscalCreditsOver(t *testing.T) {
	msg, err := renderFiscalCredits(FiscalCredits{Waiting: 3}, "https://painel.farbo.test")
	if err != nil {
		t.Fatal(err)
	}
	if msg.Subject != "Os créditos da Notaas acabaram: notas fiscais paradas" {
		t.Errorf("assunto = %q", msg.Subject)
	}
	for _, want := range []string{"3 notas estão esperando", "Coloque créditos na Notaas", "em até 15 minutos", NotaasURL} {
		if !strings.Contains(msg.Text, want) || !strings.Contains(msg.HTML, want) {
			t.Errorf("sem %q:\n%s", want, msg.Text)
		}
	}
	one, _ := renderFiscalCredits(FiscalCredits{Waiting: 1}, "https://painel.farbo.test")
	if !strings.Contains(one.Text, "1 nota está esperando") {
		t.Errorf("uma nota:\n%s", one.Text)
	}

	sender := &listSender{}
	if err := NewFiscalMailer(sender, "https://painel.farbo.test").FiscalCreditsOver(context.Background(),
		[]string{"central@farbo.test", "pedro@farbo.test"}, FiscalCredits{Waiting: 2}); err != nil {
		t.Fatal(err)
	}
	if len(sender.sent) != 2 || sender.sent[0].To != "central@farbo.test" || sender.sent[1].To != "pedro@farbo.test" {
		t.Errorf("enviados = %+v", sender.sent)
	}
}
