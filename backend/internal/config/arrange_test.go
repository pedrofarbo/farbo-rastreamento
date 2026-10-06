package config

import (
	"strings"
	"testing"
)

// A Grande São Paulo: os 39 municípios da região metropolitana, sem repetir.
func TestGreaterSaoPaulo(t *testing.T) {
	cities := strings.Split(GreaterSaoPaulo, ",")
	seen := map[string]bool{}
	for _, c := range cities {
		if !strings.HasSuffix(c, "/SP") || strings.TrimSpace(c) != c || seen[c] {
			t.Errorf("cidade %q", c)
		}
		seen[c] = true
	}
	if len(cities) != 39 {
		t.Errorf("%d cidades", len(cities))
	}
}
