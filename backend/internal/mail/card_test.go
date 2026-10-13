package mail

import (
	"strings"
	"testing"
)

// O aviso do débito recusado: com nova tentativa amanhã ou a última, o cartão
// pela bandeira e o final, e o link das faturas.
func TestRenderCardDeclined(t *testing.T) {
	d := CardDeclined{To: "ana@cliente.test", Name: "Ana Souza", Brand: "VISA", Last4: "4242", AmountCents: 13970,
		DueDate: "13/10/2026", Retry: true}
	msg, err := renderCardDeclined(d, "https://painel.farbo.test")
	if err != nil {
		t.Fatal(err)
	}
	if msg.Subject != "Não conseguimos cobrar a fatura no seu cartão" {
		t.Errorf("assunto = %q", msg.Subject)
	}
	for _, want := range []string{"Olá, Ana!", "R$ 139,70", "13/10/2026", "VISA final 4242", "tentar de novo amanhã",
		"https://painel.farbo.test/faturas"} {
		if !strings.Contains(msg.Text, want) || !strings.Contains(msg.HTML, strings.ReplaceAll(want, "Olá, Ana!", "Ana")) {
			t.Errorf("sem %q:\n%s", want, msg.Text)
		}
	}
	d.Retry = false
	last, _ := renderCardDeclined(d, "https://painel.farbo.test")
	if last.Subject != "Pague a fatura por Pix ou boleto: o cartão foi recusado" || !strings.Contains(last.Text, "última tentativa") {
		t.Errorf("última = %q\n%s", last.Subject, last.Text)
	}
}
