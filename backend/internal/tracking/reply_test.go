package tracking

import "testing"

func TestFailedResponse(t *testing.T) {
	for response, failed := range map[string]bool{
		// O J16 aceitou os dois parâmetros: a lista de ERROR: está vazia.
		"SETOK: GT06SEL=1*GT06IEXVOL=2 ERROR:": false,
		// Aceitou um e recusou o outro.
		"SETOK: GT06SEL=1 ERROR: GT06IEXVOL": true,
		// O estado: "Fail positioning" é o GPS sem sinal, não o comando.
		"Battery:3.94V;GPRS:Online;GSM Signal Level:20;ACC:OFF;GPS:Fail positioning;GPS Signal Level:0,0,0,0,0,0;Defense:ON;": false,
		"Battery:3.99V;GPRS:Online;GSM Signal Level:15;ACC:OFF;GPS:OFF;Defense:ON;":                                           false,
		"<10-25 18:45>http://maps.google.com/maps?q=-23.492195,-46.843112":                                                    false,
		"TIMER ACC ON:10s,ACC OFF:3600s": false,
		// As negativas de verdade continuam negativas.
		"Command error":   true,
		"Password error!": true,
		"Not support":     true,
		"Cut fail":        true,
		"Invalid param":   true,
	} {
		if got := failedResponse(response); got != failed {
			t.Errorf("failedResponse(%q) = %v, quer %v", response, got, failed)
		}
	}
}
