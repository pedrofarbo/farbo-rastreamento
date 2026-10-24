package busroutes

import (
	"testing"
	"time"
)

// A viagem: os pontos passados (na ordem, com um pulado), o atraso, a
// previsão dos próximos, o desvio do caminho e o fim.
func TestProgress(t *testing.T) {
	// Uma linha reta para o norte: 4 pontos a cada ~1,1 km, 5 min um do outro.
	route := &Route{DeviationM: 300}
	for i := range 4 {
		route.Stops = append(route.Stops, Stop{Seq: i + 1, Name: "P", Latitude: -23.60 + float64(i)*0.01, Longitude: -46.60,
			RadiusM: 100, OffsetMin: i * 5})
	}
	dep := time.Date(2026, 10, 24, 8, 0, 0, 0, time.UTC)
	trip := &Trip{DepartsAt: dep, VehicleName: "Ônibus 12"}
	at := func(min int) time.Time { return dep.Add(time.Duration(min) * time.Minute) }
	pt := func(min int, lat, lon float64) Point { return Point{At: at(min), Latitude: lat, Longitude: lon} }

	// Antes de sair.
	if p := Progress(route, trip, nil, at(-5)); p.Status != TripScheduled || *p.NextSeq != 1 {
		t.Fatalf("antes = %+v", p)
	}
	// Saiu 2 min atrasado; o ponto 2 passou entre posições (pulado); no 3, 4 min atrasado.
	points := []Point{pt(2, -23.60, -46.60), pt(9, -23.595, -46.60), pt(14, -23.58, -46.60)}
	p := Progress(route, trip, points, at(15))
	if p.Status != TripRunning || p.Stops[0].DelayMin == nil || *p.Stops[0].DelayMin != 2 || p.Stops[1].ArrivedAt != nil ||
		*p.Stops[2].DelayMin != 4 || *p.DelayMin != 4 || *p.NextSeq != 4 || p.OffRoute {
		t.Fatalf("andando = %+v", p)
	}
	if !p.Stops[3].ETA.Equal(at(19)) {
		t.Fatalf("previsão do ponto 4 = %v", p.Stops[3].ETA)
	}
	// Saiu do caminho (1 km a leste).
	off := append(points, pt(15, -23.575, -46.59))
	if p := Progress(route, trip, off, at(16)); !p.OffRoute {
		t.Fatalf("fora do caminho = %+v", p)
	}
	// Chegou ao último: terminou.
	done := append(points, pt(16, -23.57, -46.60))
	if p := Progress(route, trip, done, at(17)); p.Status != TripFinished || p.NextSeq != nil {
		t.Fatalf("terminou = %+v", p)
	}
	// Sem sair: o atraso conta da saída prevista.
	if p := Progress(route, trip, nil, at(7)); p.Status != TripRunning || *p.DelayMin != 7 {
		t.Fatalf("parado = %+v", p)
	}
	// Cancelada.
	trip.Canceled = true
	if p := Progress(route, trip, points, at(15)); p.Status != TripCanceled {
		t.Fatalf("cancelada = %+v", p)
	}
}
