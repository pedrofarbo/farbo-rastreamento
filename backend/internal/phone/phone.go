// Package phone deixa os telefones brasileiros num formato só: celular
// "(11) 9-8888-7777" e fixo "(11) 3333-4444".
package phone

import "strings"

// Format normaliza o telefone: tira o 55 do país, põe o 9 no celular antigo
// (10 dígitos começando com 6 a 9, de antes de 2016) e aplica a máscara. O
// que não é telefone brasileiro (outro tamanho) volta como veio, sem os
// espaços das pontas — não se perde o que foi digitado.
func Format(raw string) string {
	raw = strings.TrimSpace(raw)
	d := Digits(raw)
	if (len(d) == 12 || len(d) == 13) && strings.HasPrefix(d, "55") {
		d = d[2:]
	}
	if len(d) == 10 && d[2] >= '6' && d[2] <= '9' {
		d = d[:2] + "9" + d[2:]
	}
	switch {
	case len(d) == 11 && d[2] == '9':
		return "(" + d[:2] + ") 9-" + d[3:7] + "-" + d[7:]
	case len(d) == 10:
		return "(" + d[:2] + ") " + d[2:6] + "-" + d[6:]
	}
	return raw
}

// Digits deixa só os números.
func Digits(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, s)
}
