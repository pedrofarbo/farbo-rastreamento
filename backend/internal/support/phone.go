package support

import "strings"

// phoneKeys são as formas do número do contato para achar o cliente pelo
// telefone do cadastro (só dígitos): com e sem o 55 e, no Brasil, com e sem o
// nono dígito — o WhatsApp manda alguns celulares antigos sem ele
// (5511 8888-7777), e o cadastro pode ter qualquer uma das formas.
func phoneKeys(waID string) []string {
	d := digits(waID)
	if d == "" {
		return nil
	}
	keys := []string{d}
	if national, ok := strings.CutPrefix(d, "55"); ok && (len(national) == 10 || len(national) == 11) {
		ddd, local := national[:2], national[2:]
		switch {
		case len(local) == 8 && strings.ContainsRune("6789", rune(local[0])):
			keys = append(keys, "55"+ddd+"9"+local)
		case len(local) == 9 && local[0] == '9':
			keys = append(keys, "55"+ddd+local[1:])
		}
		for _, k := range keys[:len(keys):len(keys)] {
			keys = append(keys, k[2:])
		}
	}
	return keys
}

func digits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// displayPhone formata o número para o painel e os e-mails:
// 5511988887777 → +55 11 98888-7777.
func displayPhone(waID string) string {
	d := digits(waID)
	national, ok := strings.CutPrefix(d, "55")
	if !ok || (len(national) != 10 && len(national) != 11) {
		return "+" + d
	}
	local := national[2:]
	return "+55 " + national[:2] + " " + local[:len(local)-4] + "-" + local[len(local)-4:]
}
