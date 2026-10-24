package tracking

import "testing"

// O limite da bateria fraca: 12 V e 24 V; abaixo de 7 V é falta de energia.
func TestBatteryLowLimit(t *testing.T) {
	cases := map[float64]float64{0: 0, 6.9: 0, 11.2: 11.8, 12.6: 11.8, 14.2: 11.8, 23.1: 23.6, 27.8: 23.6}
	for volts, want := range cases {
		if got := BatteryLowLimit(volts); got != want {
			t.Errorf("BatteryLowLimit(%.1f) = %.1f, quer %.1f", volts, got, want)
		}
	}
}
