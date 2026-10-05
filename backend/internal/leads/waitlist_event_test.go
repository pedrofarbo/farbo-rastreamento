package leads

import (
	"strings"
	"testing"
)

func TestEventSlug(t *testing.T) {
	cases := map[string]string{
		"Encontro Insanos MC — Out/26": "encontro-insanos-mc-out-26",
		"  Feira de Motos São Paulo  ": "feira-de-motos-sao-paulo",
		"encontro-insanos-mc":          "encontro-insanos-mc",
		"Ação & Reação!!":              "acao-reacao",
		"":                             "",
		"---":                          "",
		"<script>alert(1)</script>":    "script-alert-1-script",
	}
	for in, want := range cases {
		if got := EventSlug(in); got != want {
			t.Errorf("EventSlug(%q) = %q, quer %q", in, got, want)
		}
	}
	if got := EventSlug(strings.Repeat("evento muito longo ", 10)); len(got) > maxEvent || strings.HasSuffix(got, "-") {
		t.Errorf("slug com %d letras", len(got))
	}
}
