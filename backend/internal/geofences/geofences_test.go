package geofences

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestFencesWatchOnlyTheirVehicles(t *testing.T) {
	ana, bia := uuid.New(), uuid.New()
	carro, moto, alheio := uuid.New(), uuid.New(), uuid.New()
	central := &Geofence{ID: uuid.New(), Name: "Pátio", Latitude: -23.55, Longitude: -46.63, RadiusMeters: 500, Active: true}
	casa := &Geofence{ID: uuid.New(), Name: "Casa", Latitude: -23.55, Longitude: -46.63, RadiusMeters: 500, Active: true,
		OwnerID: &ana, VehicleIDs: []uuid.UUID{carro}}
	desligada := &Geofence{ID: uuid.New(), Name: "Desligada", Latitude: -23.55, Longitude: -46.63, RadiusMeters: 500,
		OwnerID: &ana, VehicleIDs: []uuid.UUID{carro}}

	s := &Service{cached: map[uuid.UUID]*Geofence{central.ID: central, casa.ID: casa}}
	_ = desligada // fora do cache: só as ativas são avaliadas

	inside := func(v Subject) []uuid.UUID { return s.Inside(v, -23.551, -46.631) }
	if got := inside(Subject{VehicleID: &carro, OwnerID: &ana}); !slices.Contains(got, casa.ID) || !slices.Contains(got, central.ID) {
		t.Errorf("o carro da Ana: a cerca dela e a da central, veio %v", got)
	}
	if got := inside(Subject{VehicleID: &moto, OwnerID: &ana}); !slices.Equal(got, []uuid.UUID{central.ID}) {
		t.Errorf("a moto da Ana não foi escolhida para Casa: só a central, veio %v", got)
	}
	// Veículo que mudou de dono deixa de ser vigiado pela cerca do antigo.
	if got := inside(Subject{VehicleID: &carro, OwnerID: &bia}); !slices.Equal(got, []uuid.UUID{central.ID}) {
		t.Errorf("carro agora da Bia: a cerca da Ana não vale, veio %v", got)
	}
	if got := inside(Subject{VehicleID: &alheio}); !slices.Equal(got, []uuid.UUID{central.ID}) {
		t.Errorf("veículo da central: só a cerca da central, veio %v", got)
	}
	if got := inside(Subject{}); !slices.Equal(got, []uuid.UUID{central.ID}) {
		t.Errorf("rastreador sem veículo: só a cerca da central, veio %v", got)
	}
	if got := s.Inside(Subject{VehicleID: &carro, OwnerID: &ana}, -23.60, -46.63); len(got) != 0 {
		t.Errorf("5 km dali: fora de todas, veio %v", got)
	}
	if !s.Applies(casa.ID, Subject{VehicleID: &carro, OwnerID: &ana}) || s.Applies(casa.ID, Subject{VehicleID: &moto, OwnerID: &ana}) ||
		s.Applies(desligada.ID, Subject{VehicleID: &carro, OwnerID: &ana}) {
		t.Error("Applies: só cerca ativa que vigia o veículo")
	}

	var nilService *Service
	if nilService.Inside(Subject{}, 0, 0) != nil || nilService.Applies(casa.ID, Subject{}) {
		t.Error("sem serviço de cercas, nada é avaliado")
	}
}

func TestNormalizeCustomerAndCentralFences(t *testing.T) {
	owner, car := uuid.New(), uuid.New()
	base := Input{Name: "  Casa  ", Latitude: -23.5, Longitude: -46.6, RadiusMeters: 200, VehicleIDs: []uuid.UUID{car, car}}

	got, err := normalize(base, &owner)
	if err != nil || got.Name != "Casa" || len(got.VehicleIDs) != 1 {
		t.Fatalf("nome aparado e veículos sem repetição: %+v, %v", got, err)
	}

	cases := map[string]struct {
		in    func(Input) Input
		owner *uuid.UUID
	}{
		"sem nome":                {func(in Input) Input { in.Name = " "; return in }, &owner},
		"nome comprido":           {func(in Input) Input { in.Name = strings.Repeat("a", 101); return in }, &owner},
		"latitude fora":           {func(in Input) Input { in.Latitude = 91; return in }, &owner},
		"raio pequeno do cliente": {func(in Input) Input { in.RadiusMeters = 30; return in }, &owner},
		"raio grande do cliente":  {func(in Input) Input { in.RadiusMeters = 60000; return in }, &owner},
		"cliente sem veículo":     {func(in Input) Input { in.VehicleIDs = nil; return in }, &owner},
		"central com veículo":     {func(in Input) Input { return in }, nil},
		"raio grande da central":  {func(in Input) Input { in.VehicleIDs = nil; in.RadiusMeters = 300000; return in }, nil},
	}
	for name, c := range cases {
		if _, err := normalize(c.in(base), c.owner); err == nil {
			t.Errorf("%s: devia recusar", name)
		}
	}

	central := base
	central.VehicleIDs, central.RadiusMeters = nil, 30
	if _, err := normalize(central, nil); err != nil {
		t.Errorf("a central aceita raio de 30 m e nenhum veículo: %v", err)
	}
}

// O limite de cercas cresce com a frota: 2 por veículo ativo, de 20 a 500.
func TestLimitGrowsWithTheFleet(t *testing.T) {
	for vehicles, want := range map[int]int{0: 20, 1: 20, 10: 20, 11: 22, 100: 200, 250: 500, 1000: 500} {
		if got := LimitFor(vehicles); got != want {
			t.Errorf("%d veículos: %d cercas, quer %d", vehicles, got, want)
		}
	}
}

// A área desenhada: dentro, fora (mesmo dentro do círculo que a cobre) e o
// centro e o raio calculados.
func TestPolygonFence(t *testing.T) {
	owner := uuid.New()
	// Um "L": o canto vazio fica dentro do círculo, mas fora da área.
	in, err := normalize(Input{Name: "Pátio", Shape: ShapePolygon, VehicleIDs: []uuid.UUID{uuid.New()}, Polygon: [][2]float64{
		{-23.5500, -46.6300}, {-23.5500, -46.6260}, {-23.5520, -46.6260}, {-23.5520, -46.6280},
		{-23.5540, -46.6280}, {-23.5540, -46.6300}, {-23.5500, -46.6300},
	}}, &owner)
	if err != nil {
		t.Fatal(err)
	}
	if len(in.Polygon) != 6 || in.RadiusMeters < 200 || in.RadiusMeters > 400 {
		t.Fatalf("polígono normalizado = %d pontos, raio %.0f", len(in.Polygon), in.RadiusMeters)
	}
	g := &Geofence{Shape: in.Shape, Latitude: in.Latitude, Longitude: in.Longitude, RadiusMeters: in.RadiusMeters, Polygon: in.Polygon}
	for _, tc := range []struct {
		lat, lon float64
		inside   bool
	}{
		{-23.5510, -46.6290, true},  // no braço de cima
		{-23.5530, -46.6290, true},  // no braço de baixo
		{-23.5530, -46.6270, false}, // no canto vazio do L
		{-23.5600, -46.6300, false}, // longe
	} {
		if got := g.Contains(tc.lat, tc.lon); got != tc.inside {
			t.Errorf("(%.4f, %.4f): dentro = %v, quer %v", tc.lat, tc.lon, got, tc.inside)
		}
	}
	for _, bad := range [][][2]float64{
		{{-23.55, -46.63}, {-23.551, -46.63}},
		make([][2]float64, MaxPolygonVertices+1),
		{{-23.55, -46.63}, {-23.55, -46.6299}, {-23.5501, -46.63}}, // pequena demais
	} {
		if _, err := normalize(Input{Name: "x", Shape: ShapePolygon, Polygon: bad, VehicleIDs: []uuid.UUID{uuid.New()}}, &owner); err == nil {
			t.Errorf("aceitou o polígono de %d pontos", len(bad))
		}
	}
}

// O horário dos avisos: sempre, só em dias, num intervalo e atravessando a
// meia-noite (a madrugada é do dia em que o intervalo começa).
func TestFenceSchedule(t *testing.T) {
	loc, _ := time.LoadLocation("America/Sao_Paulo")
	at := func(weekday time.Weekday, hour, minute int) time.Time {
		// 11/10/2026 é domingo.
		return time.Date(2026, 10, 11+int(weekday), hour, minute, 0, 0, loc)
	}
	minutes := func(h, m int) *int { v := h*60 + m; return &v }
	always := &Geofence{}
	weekdays := &Geofence{ScheduleDays: []int{1, 2, 3, 4, 5}}
	business := &Geofence{ScheduleDays: []int{1, 2, 3, 4, 5}, ScheduleStart: minutes(8, 0), ScheduleEnd: minutes(18, 0)}
	night := &Geofence{ScheduleDays: []int{5}, ScheduleStart: minutes(22, 0), ScheduleEnd: minutes(6, 0)}
	for _, tc := range []struct {
		name string
		g    *Geofence
		t    time.Time
		want bool
	}{
		{"sempre", always, at(time.Sunday, 3, 0), true},
		{"dia útil", weekdays, at(time.Monday, 3, 0), true},
		{"domingo fora", weekdays, at(time.Sunday, 12, 0), false},
		{"expediente", business, at(time.Tuesday, 8, 0), true},
		{"fim do expediente", business, at(time.Tuesday, 18, 0), false},
		{"antes do expediente", business, at(time.Tuesday, 7, 59), false},
		{"sexta à noite", night, at(time.Friday, 23, 0), true},
		{"madrugada de sábado é da sexta", night, at(time.Saturday, 5, 59), true},
		{"sábado de manhã", night, at(time.Saturday, 6, 0), false},
		{"madrugada de sexta é da quinta", night, at(time.Friday, 2, 0), false},
	} {
		if got := tc.g.NotifiesAt(tc.t, loc); got != tc.want {
			t.Errorf("%s: avisa = %v, quer %v", tc.name, got, tc.want)
		}
	}
	owner := uuid.New()
	base := Input{Name: "x", Latitude: -23.55, Longitude: -46.63, RadiusMeters: 200, VehicleIDs: []uuid.UUID{uuid.New()}}
	for _, bad := range []Input{
		{ScheduleDays: []int{7}},
		{ScheduleStart: minutes(8, 0)},
		{ScheduleStart: minutes(8, 0), ScheduleEnd: minutes(8, 0)},
	} {
		in := base
		in.ScheduleDays, in.ScheduleStart, in.ScheduleEnd = bad.ScheduleDays, bad.ScheduleStart, bad.ScheduleEnd
		if _, err := normalize(in, &owner); err == nil {
			t.Errorf("aceitou o horário %+v", bad)
		}
	}
	all := base
	all.ScheduleDays = []int{6, 0, 1, 2, 3, 4, 5, 5}
	if in, err := normalize(all, &owner); err != nil || len(in.ScheduleDays) != 0 || in.Shape != ShapeCircle {
		t.Errorf("os 7 dias viram sempre: %+v %v", in, err)
	}
}
