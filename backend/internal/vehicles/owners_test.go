package vehicles

import (
	"testing"

	"github.com/google/uuid"
)

func TestOwnerIndexOwns(t *testing.T) {
	alice, bob := uuid.New(), uuid.New()
	aliceVehicle, aliceDevice := uuid.New(), uuid.New()
	centralVehicle := uuid.New()

	index := &OwnerIndex{
		byVehicle: map[uuid.UUID]uuid.UUID{aliceVehicle: alice},
		byDevice:  map[uuid.UUID]uuid.UUID{aliceDevice: alice},
	}

	cases := []struct {
		name     string
		customer uuid.UUID
		vehicle  *uuid.UUID
		device   *uuid.UUID
		want     bool
	}{
		{"dona pelo veículo", alice, &aliceVehicle, nil, true},
		{"dona pelo rastreador", alice, nil, &aliceDevice, true},
		{"outro cliente, mesmo veículo", bob, &aliceVehicle, nil, false},
		{"outro cliente, mesmo rastreador", bob, nil, &aliceDevice, false},
		{"veículo da central (sem dono)", alice, &centralVehicle, nil, false},
		{"mensagem sem veículo nem rastreador", alice, nil, nil, false},
		// O veículo manda: não adianta o rastreador ser da cliente se o
		// veículo informado não é.
		{"veículo alheio com rastreador próprio", alice, &centralVehicle, &aliceDevice, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := index.Owns(tc.customer, tc.vehicle, tc.device); got != tc.want {
				t.Fatalf("Owns = %v, esperado %v", got, tc.want)
			}
		})
	}
}

func TestOwnerIndexSharedWith(t *testing.T) {
	alice, bia, caio := uuid.New(), uuid.New(), uuid.New()
	vehicle, device, other := uuid.New(), uuid.New(), uuid.New()

	index := &OwnerIndex{
		byVehicle:       map[uuid.UUID]uuid.UUID{vehicle: alice},
		byDevice:        map[uuid.UUID]uuid.UUID{device: alice},
		guestsByVehicle: map[uuid.UUID]map[uuid.UUID]bool{vehicle: {bia: true}},
		guestsByDevice:  map[uuid.UUID]map[uuid.UUID]bool{device: {bia: true}},
	}

	cases := []struct {
		name    string
		user    uuid.UUID
		vehicle *uuid.UUID
		device  *uuid.UUID
		want    bool
	}{
		{"com acesso, pelo veículo", bia, &vehicle, nil, true},
		{"com acesso, pelo rastreador", bia, nil, &device, true},
		{"sem acesso", caio, &vehicle, nil, false},
		{"com acesso a outro veículo", bia, &other, &device, false},
		// O dono não "recebeu" acesso: o dele é o Owns.
		{"o dono", alice, &vehicle, nil, false},
		{"mensagem sem veículo nem rastreador", bia, nil, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := index.SharedWith(tc.user, tc.vehicle, tc.device); got != tc.want {
				t.Fatalf("SharedWith = %v, esperado %v", got, tc.want)
			}
		})
	}
	if index.Owns(bia, &vehicle, nil) {
		t.Error("quem tem acesso não vira dono")
	}
}
