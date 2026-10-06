package fulfillment

import (
	"strings"
	"testing"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
)

func TestCanArrange(t *testing.T) {
	// O padrão: a Grande São Paulo.
	s := &Service{shipping: config.Shipping{ArrangeCities: strings.Split(config.GreaterSaoPaulo, ",")}}
	for _, tc := range []struct {
		city, state string
		want        bool
	}{
		{"São Paulo", "SP", true},
		{" SAO PAULO ", "sp", true},
		{"Guarulhos", "SP", true},
		{"Santo André", "SP", true},
		{"Mogi das Cruzes", "SP", true},
		{"Embu das Artes", "SP", true},
		{"Biritiba Mirim", "SP", true}, // sem o hífen
		{"Embu-Guaçu", "SP", true},
		{"Vargem Grande Paulista", "SP", true},
		{"São Paulo", "RJ", false},
		{"Campinas", "SP", false},
		{"Jundiaí", "SP", false},
		{"Santos", "SP", false},
		{"Embu", "SP", false},
		{"", "SP", false},
	} {
		if got := s.CanArrange(tc.city, tc.state); got != tc.want {
			t.Errorf("CanArrange(%q, %q) = %v", tc.city, tc.state, got)
		}
	}
	// Sem a UF, vale a cidade em qualquer estado.
	if !(&Service{shipping: config.Shipping{ArrangeCities: []string{"Guarulhos"}}}).CanArrange("guarulhos", "SP") {
		t.Error("cidade sem UF")
	}
	if _, err := s.Arrange("Rio de Janeiro", "rj"); err == nil || !strings.Contains(err.Error(), "não vale para Rio de Janeiro/RJ") {
		t.Errorf("combinou a entrega no Rio: %v", err)
	}
	if c, err := s.Arrange("Osasco", "SP"); err != nil || !c.Arranged || c.PriceCents != 0 || c.Name != ArrangedService {
		t.Errorf("Arrange em Osasco = %+v, %v", c, err)
	}
}
