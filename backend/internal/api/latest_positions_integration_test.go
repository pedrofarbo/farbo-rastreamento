package api

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/devices"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/tracking"
)

// A lista de veículos busca só os rastreadores e as últimas posições dos
// veículos listados, pelo índice: vale a mais recente pelo relógio do
// aparelho (a posição guardada sem sinal que chega depois não volta o mapa),
// no empate a gravada por último, e quem não tem posição — ou não foi pedido
// — fica de fora. Precisa de FARBO_TEST_DATABASE_URL.
func TestLatestPositionsFor(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	devs := devices.NewRepository(db)
	positions := tracking.NewRepository(db)

	create := func(imei string) uuid.UUID {
		t.Helper()
		d, err := devs.Create(ctx, devices.Input{IMEI: imei, Protocol: "GT06"})
		if err != nil {
			t.Fatal(err)
		}
		return d.ID
	}
	insert := func(device uuid.UUID, at time.Time, lat float64) {
		t.Helper()
		if err := positions.Insert(ctx, &tracking.Position{DeviceID: device, GPSTimestamp: at, Latitude: lat, Longitude: -46.6}); err != nil {
			t.Fatal(err)
		}
	}
	moved, tied, silent, other := create("869000000000001"), create("869000000000002"),
		create("869000000000003"), create("869000000000004")
	now := time.Now().UTC().Truncate(time.Second)
	insert(moved, now, -23.2)
	insert(moved, now.Add(-10*time.Minute), -23.0) // descarregada depois de voltar o sinal
	insert(tied, now, -22.1)
	insert(tied, now, -22.2)
	insert(other, now, -21.0)

	latest, err := positions.LatestFor(ctx, []uuid.UUID{moved, tied, silent})
	if err != nil {
		t.Fatal(err)
	}
	if len(latest) != 2 || latest[moved] == nil || latest[moved].Latitude != -23.2 ||
		latest[tied] == nil || latest[tied].Latitude != -22.2 {
		t.Fatalf("últimas posições = %+v", latest)
	}
	if empty, err := positions.LatestFor(ctx, nil); err != nil || len(empty) != 0 {
		t.Fatalf("sem rastreadores = %+v %v", empty, err)
	}

	listed, err := devs.ListByIDs(ctx, []uuid.UUID{moved, silent})
	if err != nil || len(listed) != 2 {
		t.Fatalf("rastreadores pedidos = %d %v", len(listed), err)
	}
	for _, d := range listed {
		if d.ID != moved && d.ID != silent {
			t.Errorf("veio um rastreador não pedido: %s", d.IMEI)
		}
	}
	if none, err := devs.ListByIDs(ctx, nil); err != nil || len(none) != 0 {
		t.Fatalf("sem ids = %d %v", len(none), err)
	}
}
