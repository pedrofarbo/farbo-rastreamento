package payments

import (
	"strings"
	"testing"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/addresses"
)

// TestCardPayerMissing: o que falta para o cartão pela Pagar.me, na mesma
// conferência da cobrança (pagarmeCustomer e o endereço de cobrança).
func TestCardPayerMissing(t *testing.T) {
	address := &addresses.Address{ZipCode: "01001000", Street: "Praça da Sé", Number: "100", District: "Sé", City: "São Paulo", State: "SP"}
	full := Payer{Name: "Ana", Email: "ana@x.test", Document: "529.982.247-25", Phone: "(11) 98888-1234", Address: address}
	cases := []struct {
		name string
		edit func(p *Payer)
		want string
	}{
		{"completo", func(*Payer) {}, ""},
		{"celular com o 55", func(p *Payer) { p.Phone = "+55 11 98888-1234" }, ""},
		{"CNPJ", func(p *Payer) { p.Document = "11.222.333/0001-81" }, ""},
		{"sem nome", func(p *Payer) { p.Name = " " }, "nome"},
		{"e-mail sem arroba", func(p *Payer) { p.Email = "ana" }, "e-mail"},
		{"CPF inválido", func(p *Payer) { p.Document = "123.456.789-00" }, "CPF ou CNPJ válido"},
		{"celular curto", func(p *Payer) { p.Phone = "98888-1234" }, "celular com DDD"},
		// 12 dígitos sem o 55 do país: a Pagar.me não aceita (o Pix aceitaria).
		{"celular de 12 dígitos", func(p *Payer) { p.Phone = "123456789012" }, "celular com DDD"},
		{"sem endereço", func(p *Payer) { p.Address = nil }, "endereço completo (CEP, rua, número, cidade e UF)"},
		{"endereço sem número", func(p *Payer) {
			a := *address
			a.Number = ""
			p.Address = &a
		}, "endereço completo (CEP, rua, número, cidade e UF)"},
		{"vazio", func(p *Payer) { *p = Payer{} }, "nome|e-mail|CPF ou CNPJ válido|celular com DDD|endereço completo (CEP, rua, número, cidade e UF)"},
	}
	for _, c := range cases {
		p := full
		c.edit(&p)
		got := CardPayerMissing(&p)
		if strings.Join(got, "|") != c.want {
			t.Errorf("%s: falta %v, esperado %q", c.name, got, c.want)
		}
		// O que não falta nada a cobrança aceita (e vice-versa).
		if ok := pagarmeCustomer(&p) != nil && pagarmeCustomer(&p).Address != nil; ok != (len(got) == 0) {
			t.Errorf("%s: a conferência (%v) e a cobrança (%v) discordam", c.name, got, ok)
		}
	}
	if got := CardPayerMissing(nil); len(got) != 5 {
		t.Errorf("sem pagador = %v", got)
	}
}
