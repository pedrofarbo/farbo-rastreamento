package geofences

import (
	"slices"
	"strings"
	"testing"

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
