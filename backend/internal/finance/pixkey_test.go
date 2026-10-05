package finance

import (
	"fmt"
	"strings"
	"testing"
)

// brCode monta um Pix copia-e-cola como os bancos geram (com o CRC certo).
func brCode(name, amount string) string {
	field := func(id, value string) string { return fmt.Sprintf("%s%02d%s", id, len(value), value) }
	account := field("00", "br.gov.bcb.pix") + field("01", "fornecedor@exemplo.com.br")
	payload := field("00", "01") + field("26", account) + field("52", "0000") + field("53", "986")
	if amount != "" {
		payload += field("54", amount)
	}
	payload += field("58", "BR") + field("59", name) + field("60", "SAO PAULO") + field("62", field("05", "***")) + "6304"
	return payload + fmt.Sprintf("%04X", crc16(payload))
}

func TestPixKeys(t *testing.T) {
	for key, want := range map[string]string{
		"fornecedor@exemplo.com.br":            "EMAIL",
		"123e4567-e89b-12d3-a456-426614174000": "RANDOM",
		"529.982.247-25":                       "CPF",
		"11.222.333/0001-81":                   "CNPJ",
		"11222333000181":                       "CNPJ",
		"+55 11 98888-7777":                    "PHONE",
		"(11) 98888-7777":                      "PHONE",
		"1133334444":                           "PHONE",
		"52998224725":                          "", // CPF ou celular: tem que dizer
		"":                                     "",
		"chave qualquer":                       "",
	} {
		if got := DetectKeyType(key); got != want {
			t.Errorf("DetectKeyType(%q) = %q, quero %q", key, got, want)
		}
	}
	for _, c := range []struct{ key, keyType, want string }{
		{"529.982.247-25", "CPF", "52998224725"},
		{"11.222.333/0001-81", "CNPJ", "11222333000181"},
		{"+55 (11) 98888-7777", "PHONE", "11988887777"},
		{"(11) 3333-4444", "PHONE", "1133334444"},
		{" Fornecedor@Exemplo.com.br ", "EMAIL", "fornecedor@exemplo.com.br"},
		{"123E4567-E89B-12D3-A456-426614174000", "RANDOM", "123e4567-e89b-12d3-a456-426614174000"},
	} {
		got, err := PixKeyFor(c.key, c.keyType)
		if err != nil || got != c.want {
			t.Errorf("PixKeyFor(%q, %s) = %q, %v; quero %q", c.key, c.keyType, got, err, c.want)
		}
	}
	for _, c := range []struct{ key, keyType string }{
		{"529.982.247-24", "CPF"}, // dígito errado
		{"11222333000180", "CNPJ"},
		{"98888-7777", "PHONE"}, // sem DDD
		{"sem-arroba", "EMAIL"},
		{"123", "RANDOM"},
		{"52998224725", "BR_CODE"},
	} {
		if _, err := PixKeyFor(c.key, c.keyType); err == nil {
			t.Errorf("PixKeyFor(%q, %s) aceitou", c.key, c.keyType)
		}
	}
	// No cadastro: 11 dígitos sem o tipo é recusado; com o tipo, aceito.
	if _, err := (SupplierInput{Name: "X", PixKey: "52998224725", Active: true}).Normalize(); err == nil {
		t.Error("aceitou 11 dígitos sem o tipo")
	}
	in, err := SupplierInput{Name: "X", PixKey: "52998224725", PixKeyType: "cpf", Active: true}.Normalize()
	if err != nil || in.PixKeyType != "CPF" {
		t.Errorf("com o tipo: %+v %v", in, err)
	}
	in, _ = SupplierInput{Name: "X", PixKeyType: "CPF", Active: true}.Normalize()
	if in.PixKeyType != "" {
		t.Errorf("sem chave ficou o tipo %q", in.PixKeyType)
	}
}

func TestParseBRCode(t *testing.T) {
	// O exemplo do manual do BR Code do Banco Central (CRC 1D3D).
	bcb := "00020126580014br.gov.bcb.pix0136123e4567-e12b-12d1-a456-4266554400005204000053039865802BR" +
		"5913Fulano de Tal6008BRASILIA62070503***63041D3D"
	if got, ok := ParseBRCode(bcb); !ok || got.Name != "Fulano de Tal" || got.AmountCents != 0 {
		t.Fatalf("exemplo do BCB = %+v %v", got, ok)
	}
	code := brCode("FORNECEDOR LTDA", "1234.50")
	got, ok := ParseBRCode(code)
	if !ok || got.Name != "FORNECEDOR LTDA" || got.City != "SAO PAULO" || got.AmountCents != 123450 {
		t.Fatalf("ParseBRCode = %+v %v", got, ok)
	}
	if got, ok := ParseBRCode(brCode("SEM VALOR", "")); !ok || got.AmountCents != 0 {
		t.Errorf("sem valor = %+v %v", got, ok)
	}
	// Minúsculas no CRC também valem.
	if _, ok := ParseBRCode(code[:len(code)-4] + strings.ToLower(code[len(code)-4:])); !ok {
		t.Error("CRC em minúsculas recusado")
	}
	for _, bad := range []string{
		code[:len(code)-1], // cortado
		strings.Replace(code, "1234.50", "1234.51", 1),           // mexido: o CRC não fecha
		"34191.79001 01043.510047 91020.150008 1 96610000045000", // boleto
		"",
	} {
		if _, ok := ParseBRCode(bad); ok {
			t.Errorf("aceitou %q", bad)
		}
	}
	if money(123456789) != "R$ 1.234.567,89" || money(5) != "R$ 0,05" {
		t.Errorf("money = %s / %s", money(123456789), money(5))
	}
}
