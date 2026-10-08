package devices

import "testing"

// O ICCID do chip: só os dígitos, 19 ou 20, começando com 89 e com o dígito
// de verificação certo; a marcação da etiqueta ("SP") fica de fora.
func TestNormalizeICCID(t *testing.T) {
	for raw, want := range map[string]string{
		"":                           "",
		"89553202100093795330":       "89553202100093795330",
		"89553202100093795330SP":     "89553202100093795330",
		" 8955 3202 1000 9379 5330 ": "89553202100093795330",
		"8955-3202-1000-9379-533 sp": "8955320210009379533",
		"89553202100093795330 SP":    "89553202100093795330",
	} {
		got, err := NormalizeICCID(raw)
		if err != nil || got != want {
			t.Errorf("NormalizeICCID(%q) = %q, %v; quer %q", raw, got, err, want)
		}
	}
	for _, raw := range []string{
		"89553202100093795331",     // dígito de verificação errado
		"79553202100093795330",     // não começa com 89
		"8955320210009379",         // curto
		"895532021000937953301",    // longo
		"89553202100093795330ABCD", // letras demais
		"SP89553202100093795330",   // letras antes
		"8955320210O093795330",     // letra no meio
	} {
		if got, err := NormalizeICCID(raw); err == nil {
			t.Errorf("NormalizeICCID(%q) = %q, quer recusa", raw, got)
		}
	}
}
