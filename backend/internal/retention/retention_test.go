package retention

import (
	"io"
	"log/slog"
	"testing"
)

func TestEffective(t *testing.T) {
	s := NewService(nil, 30, slog.New(slog.NewTextHandler(io.Discard, nil)))
	seven, fourteen := 7, 14
	cases := []struct {
		name              string
		vehicle, customer *int
		want              int
	}{
		{"sem nada: padrão da central", nil, nil, 30},
		{"do cliente", nil, &fourteen, 14},
		{"exceção do veículo vence o cliente", &seven, &fourteen, 7},
		{"só o veículo", &seven, nil, 7},
	}
	for _, tc := range cases {
		if got := s.Effective(tc.vehicle, tc.customer); got != tc.want {
			t.Errorf("%s: %d, quer %d", tc.name, got, tc.want)
		}
	}
}

func TestValid(t *testing.T) {
	for _, d := range []int{7, 14, 30, 90, 180, 365} {
		if !Valid(d) {
			t.Errorf("%d devia valer", d)
		}
	}
	for _, d := range []int{0, 1, 15, 31, 60, 366, -7} {
		if Valid(d) {
			t.Errorf("%d não devia valer", d)
		}
	}
	bad := 10
	if check(&bad) != ErrInvalid || check(nil) != nil {
		t.Error("check")
	}
}
