package devices

// CodeIMEISuspect acompanha a recusa do cadastro com IMEI suspeito: a tela
// mostra o aviso e deixa confirmar (IMEIConfirmed) se ele estiver certo.
const CodeIMEISuspect = "IMEI_SUSPECT"

// IMEIProblem diz o que está errado num IMEI de rastreador — o que o faria
// conectar no servidor e ser recusado como desconhecido (e perder os SMS da
// ativação): 16 dígitos (o IMEI tem 15) ou 15 com o dígito verificador que
// não bate. Vazio se está certo, ou se não tem cara de IMEI (o H02, por
// exemplo, usa um código de 10 dígitos).
func IMEIProblem(imei string) string {
	if !digitsOnly(imei) {
		return ""
	}
	switch len(imei) {
	case 16:
		return "tem 16 dígitos, e o IMEI tem 15 (provavelmente sobrou um número)"
	case 15:
		if !luhnValid(imei) {
			return "não confere: o último dígito (o verificador) não bate com os outros 14 " +
				"(provavelmente faltou, sobrou ou trocou um número)"
		}
	}
	return ""
}

// luhnValid confere o dígito verificador (Luhn), o último do IMEI.
func luhnValid(number string) bool {
	sum := 0
	for i := 0; i < len(number); i++ {
		d := int(number[len(number)-1-i] - '0')
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

func digitsOnly(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// SimilarIMEI diz se dois IMEIs diferentes são o mesmo com um erro de
// digitação: até 2 dígitos trocados, sobrando ou faltando. É o que liga um
// rastreador recusado como desconhecido ao cadastro que está errado.
func SimilarIMEI(a, b string) bool {
	if a == b || len(a) < 10 || len(b) < 10 {
		return false
	}
	return editDistance(a, b) <= 2
}

// editDistance é a distância de Levenshtein entre dois textos curtos.
func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}
