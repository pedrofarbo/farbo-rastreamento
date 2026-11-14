package fiscal

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

// A nota de várias faturas: as mensalidades iguais numa linha, com a
// quantidade de veículos e as placas; as outras faturas com o texto delas.
func TestNoteText(t *testing.T) {
	monthly := func(text, plate string) noteItem { return noteItem{text: text, monthly: true, vehicle: plate} }
	other := func(text string) noteItem { return noteItem{text: text} }
	cases := []struct {
		name  string
		items []noteItem
		want  string
	}{
		{"uma fatura: o texto dela, como sempre",
			[]noteItem{monthly("Plano Mensal — novembro/2026", "ABC1D23")},
			"Plano Mensal — novembro/2026"},
		{"a frota no mesmo plano",
			[]noteItem{monthly("Plano Mensal — novembro/2026", "ABC1D23"), monthly("Plano Mensal — novembro/2026", "DEF4G56"),
				monthly("Plano Mensal — novembro/2026", "Honda CG 160")},
			"Plano Mensal — novembro/2026 · 3 veículos (ABC1D23, DEF4G56, Honda CG 160)"},
		{"planos diferentes e a instalação",
			[]noteItem{monthly("Plano Mensal — novembro/2026", "ABC1D23"), monthly("Plano Mensal — novembro/2026 · cupom FROTA10", "DEF4G56"),
				other("Instalação do rastreador (comodato): Gol · DEF4G56"), monthly("Plano Mensal — novembro/2026", "GHI7J89")},
			"Plano Mensal — novembro/2026 · 2 veículos (ABC1D23, GHI7J89); Plano Mensal — novembro/2026 · cupom FROTA10 · 1 veículo (DEF4G56); " +
				"Instalação do rastreador (comodato): Gol · DEF4G56"},
		{"as outras iguais contam vezes",
			[]noteItem{other("Instalação do rastreador (comodato) × 3"), other("Desinstalação do rastreador (comodato)"),
				other("Desinstalação do rastreador (comodato)")},
			"Instalação do rastreador (comodato) × 3; Desinstalação do rastreador (comodato) × 2"},
		{"a assinatura sem veículo conta, sem a placa",
			[]noteItem{monthly("Plano Mensal — novembro/2026", "ABC1D23"), monthly("Plano Mensal — novembro/2026", "")},
			"Plano Mensal — novembro/2026 · 2 veículos (ABC1D23 e mais 1)"},
	}
	for _, c := range cases {
		if got := noteText(c.items); got != c.want {
			t.Errorf("%s:\n  got  %q\n  want %q", c.name, got, c.want)
		}
	}

	// Mais de 20 placas: as 20 primeiras e "e mais N".
	var fleet []noteItem
	for i := range 25 {
		fleet = append(fleet, monthly("Plano Mensal — novembro/2026", fmt.Sprintf("AAA%04d", i)))
	}
	got := noteText(fleet)
	if !strings.HasPrefix(got, "Plano Mensal — novembro/2026 · 25 veículos (AAA0000, AAA0001,") ||
		!strings.HasSuffix(got, "AAA0019 e mais 5)") {
		t.Errorf("frota de 25 = %q", got)
	}

	// Uma frota de planos variados que não cabe com as placas: sem elas; e
	// nunca passa do limite da NFS-e.
	fleet = nil
	for i := range 300 {
		fleet = append(fleet, monthly(fmt.Sprintf("Plano Especial %d — novembro/2026", i%40), fmt.Sprintf("AAA%04d", i)))
	}
	got = noteText(fleet)
	if strings.Contains(got, "AAA0") || utf8.RuneCountInString(got) > maxNoteText {
		t.Errorf("frota grande (%d caracteres) = %q", utf8.RuneCountInString(got), got)
	}
	if !strings.Contains(got, "Plano Especial 0 — novembro/2026 · 8 veículos; Plano Especial 1 — novembro/2026 · 8 veículos") {
		t.Errorf("frota grande sem as placas = %q", got)
	}
	for i := range 300 {
		fleet[i].text = fmt.Sprintf("Plano Especial de número %d da associação — novembro/2026", i)
	}
	if got = noteText(fleet); utf8.RuneCountInString(got) > maxNoteText || !strings.HasSuffix(got, "…") {
		t.Errorf("texto cortado (%d caracteres) = %q", utf8.RuneCountInString(got), got)
	}
}
