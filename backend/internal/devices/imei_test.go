package devices

import (
	"strings"
	"testing"
)

// O caso real de 13/10/2026: o rastreador 865893088121639 cadastrado como
// 8658930088121639 (um zero a mais) pegou a configuração por SMS, conectou e
// foi recusado como desconhecido — e os SMS se perderam.
func TestIMEIProblem(t *testing.T) {
	for imei, want := range map[string]string{
		"865893088121639":  "",                // o IMEI de verdade (dígito verificador certo)
		"8658930088121639": "tem 16 dígitos",  // o cadastro errado
		"865893008812163":  "o último dígito", // 15, com o verificador que não bate
		"869247061230011":  "o último dígito", // os IMEIs inventados dos testes
		"1234567890":       "",                // o código de 10 dígitos do H02
		"86589308812163":   "",                // 14: não tem cara de IMEI, não opina
		"86589308812163a":  "",                // não numérico: quem recusa é o ValidateIMEI
	} {
		got := IMEIProblem(imei)
		if (want == "") != (got == "") || !strings.Contains(got, want) {
			t.Errorf("IMEIProblem(%s) = %q, quer %q", imei, got, want)
		}
	}
}

func TestSimilarIMEI(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"865893088121639", "8658930088121639", true}, // um dígito a mais
		{"865893088121639", "86589308812163", true},   // um a menos
		{"865893088121639", "865893088112639", true},  // dois trocados de lugar
		{"865893088121639", "865893088121639", false}, // o mesmo não é "parecido"
		{"865893088121639", "865893077777639", false}, // longe demais
		{"865893088121639", "123456789", false},       // curto demais
	}
	for _, c := range cases {
		if got := SimilarIMEI(c.a, c.b); got != c.want {
			t.Errorf("SimilarIMEI(%s, %s) = %v, quer %v", c.a, c.b, got, c.want)
		}
	}
}
