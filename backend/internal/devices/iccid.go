package devices

import (
	"regexp"
	"strings"
)

// iccidPattern: os dígitos do chip (com espaços ou traços entre eles) e, no
// fim, até 3 letras — a marcação da etiqueta, como o "SP" dos chips da Algar.
var iccidPattern = regexp.MustCompile(`^([0-9][0-9 .-]*[0-9])\s*[A-Za-z]{0,3}$`)

// NormalizeICCID deixa o ICCID só com os dígitos e o confere: 19 ou 20
// dígitos, começando com 89 (telecomunicações), com o último dígito de
// verificação (Luhn) batendo — pega o erro de digitação. Vazio é vazio.
func NormalizeICCID(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	m := iccidPattern.FindStringSubmatch(raw)
	if m == nil {
		return "", invalid("ICCID inválido: use os números impressos no chip (ex.: 89553202100093795330)")
	}
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, m[1])
	switch {
	case len(digits) < 19 || len(digits) > 20:
		return "", invalid("ICCID inválido: são 19 ou 20 dígitos (este tem %d)", len(digits))
	case !strings.HasPrefix(digits, "89"):
		return "", invalid("ICCID inválido: começa com 89")
	case !luhn(digits):
		return "", invalid("ICCID inválido: o último dígito não confere; verifique se algum número foi digitado errado")
	}
	return digits, nil
}

// luhn confere o dígito de verificação (o último).
func luhn(digits string) bool {
	sum := 0
	for i := 0; i < len(digits); i++ {
		d := int(digits[len(digits)-1-i] - '0')
		if i%2 == 1 {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
	}
	return sum%10 == 0
}
