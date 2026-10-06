package fulfillment

import (
	"testing"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
)

func TestCanArrange(t *testing.T) {
	s := &Service{shipping: config.Shipping{ArrangeCities: []string{"São Paulo/SP", "Guarulhos"}}}
	for _, tc := range []struct {
		city, state string
		want        bool
	}{
		{"São Paulo", "SP", true},
		{" SAO PAULO ", "sp", true},
		{"São Paulo", "RJ", false}, // São Paulo é a cidade de SP
		{"Santo André", "SP", false},
		{"Guarulhos", "SP", true}, // sem a UF, vale a cidade
		{"", "SP", false},
	} {
		if got := s.CanArrange(tc.city, tc.state); got != tc.want {
			t.Errorf("CanArrange(%q, %q) = %v", tc.city, tc.state, got)
		}
	}
	if _, err := s.Arrange("Rio de Janeiro", "RJ"); err == nil {
		t.Error("combinou a entrega no Rio")
	}
	if c, err := s.Arrange("São Paulo", "SP"); err != nil || !c.Arranged || c.PriceCents != 0 || c.Name != ArrangedService {
		t.Errorf("Arrange em SP = %+v, %v", c, err)
	}
}
