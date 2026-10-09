package phone

import "testing"

// Os formatos que chegam (com e sem máscara, com o 55, celular antigo) viram
// um só; o que não é telefone brasileiro fica como veio.
func TestFormat(t *testing.T) {
	for raw, want := range map[string]string{
		"(11) 98888-7777":   "(11) 9-8888-7777",
		"11988887777":       "(11) 9-8888-7777",
		"+55 11 98888-7777": "(11) 9-8888-7777",
		"5511988887777":     "(11) 9-8888-7777",
		"(11) 9-8888-7777":  "(11) 9-8888-7777",
		" 34 9 9999-0000 ":  "(34) 9-9999-0000",
		"(11) 8888-7777":    "(11) 9-8888-7777", // celular de antes do 9
		"(11) 3333-4444":    "(11) 3333-4444",   // fixo
		"551133334444":      "(11) 3333-4444",
		"":                  "",
		"ramal 12":          "ramal 12",
		"+1 231 790 3105":   "+1 231 790 3105",
	} {
		if got := Format(raw); got != want {
			t.Errorf("Format(%q) = %q, quer %q", raw, got, want)
		}
	}
}
