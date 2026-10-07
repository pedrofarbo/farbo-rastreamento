package payments

import "strings"

// onlyDigits tira pontos, traços, barras e espaços.
func onlyDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// validTaxID aceita CPF ou CNPJ com os dígitos verificadores corretos. A
// AbacatePay recusa o Pix inteiro se o documento do pagador for inválido.
func validTaxID(doc string) bool {
	d := onlyDigits(doc)
	switch len(d) {
	case 11:
		return validCPF(d)
	case 14:
		return validCNPJ(d)
	}
	return false
}

func allSame(d string) bool {
	return strings.Count(d, d[:1]) == len(d)
}

func validCPF(d string) bool {
	if allSame(d) {
		return false
	}
	for _, size := range []int{9, 10} {
		sum := 0
		for i := 0; i < size; i++ {
			sum += int(d[i]-'0') * (size + 1 - i)
		}
		check := (sum * 10) % 11
		if check == 10 {
			check = 0
		}
		if check != int(d[size]-'0') {
			return false
		}
	}
	return true
}

func validCNPJ(d string) bool {
	if allSame(d) {
		return false
	}
	weights := []int{6, 5, 4, 3, 2, 9, 8, 7, 6, 5, 4, 3, 2}
	for _, size := range []int{12, 13} {
		sum := 0
		for i := 0; i < size; i++ {
			sum += int(d[i]-'0') * weights[len(weights)-size+i]
		}
		check := sum % 11
		if check < 2 {
			check = 0
		} else {
			check = 11 - check
		}
		if check != int(d[size]-'0') {
			return false
		}
	}
	return true
}

// ValidCPF confere os dígitos verificadores de um CPF (só os 11 dígitos).
func ValidCPF(d string) bool { return len(d) == 11 && onlyDigits(d) == d && validCPF(d) }

// ValidCNPJ confere os dígitos verificadores de um CNPJ (só os 14 dígitos).
func ValidCNPJ(d string) bool { return len(d) == 14 && onlyDigits(d) == d && validCNPJ(d) }

// Digits deixa só os números do CPF/CNPJ (sem pontos, traços e barras).
func Digits(doc string) string { return onlyDigits(doc) }

// ValidTaxID diz se é um CPF ou CNPJ com os dígitos verificadores corretos.
func ValidTaxID(doc string) bool { return validTaxID(doc) }

// FormatTaxID escreve o CPF (123.456.789-09) ou o CNPJ (12.345.678/0001-90).
func FormatTaxID(doc string) string {
	d := onlyDigits(doc)
	switch len(d) {
	case 11:
		return d[:3] + "." + d[3:6] + "." + d[6:9] + "-" + d[9:]
	case 14:
		return d[:2] + "." + d[2:5] + "." + d[5:8] + "/" + d[8:12] + "-" + d[12:]
	}
	return doc
}
