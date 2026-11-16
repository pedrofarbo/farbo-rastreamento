package inspections

import (
	"testing"
	"time"
)

func TestRoute(t *testing.T) {
	cases := map[string]int{
		KindPeriodic + "/" + VehicleCar: 6, KindEntry + "/" + VehicleCar: 15, KindEntry + "/" + VehicleMotorcycle: 9,
		KindReplacement + "/" + VehicleCar: 15, KindClaim + "/" + VehicleMotorcycle: 8, "bobagem/" + VehicleCar: 6,
	}
	for key, want := range cases {
		kind, vehicle, _ := cut(key)
		if got := len(Route(kind, vehicle)); got != want {
			t.Errorf("%s: %d ângulos, quero %d", key, got, want)
		}
	}
	moto := Route(KindEntry, VehicleMotorcycle)
	for _, s := range moto {
		if s.ID == "chassis" && s.Hint != "Gravado no quadro, perto do guidão, legível" {
			t.Errorf("a moto tem a dica dela: %q", s.Hint)
		}
	}
	in := &Inspection{Slots: Route(KindClaim, VehicleCar)}
	for _, s := range in.Slots {
		if !s.Optional {
			in.Photos = append(in.Photos, Photo{Slot: s.ID})
		}
	}
	if !in.Complete() {
		t.Errorf("a outra avaria é opcional: faltam %+v", in.Missing())
	}
}

func cut(s string) (string, string, bool) {
	for i := range s {
		if s[i] == '/' {
			return s[:i], s[i+1:], true
		}
	}
	return s, "", false
}

func TestNormalizePlate(t *testing.T) {
	for raw, want := range map[string]string{"abc-1d23": "ABC1D23", " ABC 1234 ": "ABC1234", "ab.c1é23": "ABC123"} {
		if got := NormalizePlate(raw); got != want {
			t.Errorf("%q = %q, quero %q", raw, got, want)
		}
	}
	for plate, ok := range map[string]bool{"ABC1D23": true, "ABC1234": true, "AB12345": false, "ABC12D3": false} {
		if platePattern.MatchString(plate) != ok {
			t.Errorf("placa %s: %v", plate, !ok)
		}
	}
}

func TestTexts(t *testing.T) {
	for m, want := range map[float64]string{24: "20 m", 870: "870 m", 1234: "1,2 km", 35_400: "35 km"} {
		if got := distanceText(m); got != want {
			t.Errorf("distância %.0f = %q, quero %q", m, got, want)
		}
	}
	for d, want := range map[time.Duration]string{20 * time.Second: "1 min", 4 * time.Minute: "4 min",
		80 * time.Minute: "1 h 20 min", 2 * time.Hour: "2 h", 72 * time.Hour: "3 dias"} {
		if got := durationText(d); got != want {
			t.Errorf("duração %s = %q, quero %q", d, got, want)
		}
	}
	if d := distanceM(-18.9186, -48.2772, -18.96, -48.27); d < 4500 || d > 4800 {
		t.Errorf("distância = %.0f m", d)
	}
}

func TestPlaceAndTime(t *testing.T) {
	at := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
	pt := func(lat, lon float64) (*float64, *float64) { return &lat, &lon }
	photo := func(minute int, lat, lon float64, source string, kept bool) photoCheck {
		la, lo := pt(lat, lon)
		return photoCheck{Photo: Photo{Latitude: la, Longitude: lo, Source: source, TakenAt: at.Add(time.Duration(minute) * time.Minute)}, kept: kept}
	}
	codes := func(checks []Check) map[string]string {
		out := map[string]string{}
		for _, c := range checks {
			out[c.Code] = c.Level
		}
		return out
	}

	// Tudo certo: no mesmo lugar, em poucos minutos, pela câmera.
	got := codes(placeAndTime([]photoCheck{photo(0, -18.9186, -48.2772, SourceCamera, false), photo(3, -18.9187, -48.2771, SourceCamera, false)}))
	if got["same_place"] != CheckOK || got["same_time"] != CheckOK || got["live_camera"] != CheckOK || len(got) != 3 {
		t.Errorf("tudo certo = %v", got)
	}
	// Longe, ao longo de horas, escolhida do aparelho e sem localização.
	noLoc := photoCheck{Photo: Photo{Source: SourceFile, TakenAt: at}}
	got = codes(placeAndTime([]photoCheck{photo(0, -18.9186, -48.2772, SourceCamera, false), photo(180, -19.9, -43.9, SourceCamera, false), noLoc}))
	if got["spread_place"] != CheckWarn || got["spread_time"] != CheckWarn || got["file_source"] != CheckWarn || got["no_location"] != CheckWarn {
		t.Errorf("tudo errado = %v", got)
	}
	// As aproveitadas da devolvida não contam no lugar nem na hora.
	got = codes(placeAndTime([]photoCheck{photo(-3000, -19.9, -43.9, SourceCamera, true), photo(0, -18.9186, -48.2772, SourceCamera, false)}))
	if got["kept"] != CheckInfo || got["spread_place"] != "" || got["spread_time"] != "" {
		t.Errorf("refação = %v", got)
	}
}
