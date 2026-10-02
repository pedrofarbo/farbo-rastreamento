package fulfillment

import (
	"errors"
	"reflect"
	"testing"
)

func TestPlan(t *testing.T) {
	cases := []struct {
		name                         string
		track, target, chip, tracker string
		hasDevice, labeled           bool
		want                         []string
		refused                      bool
	}{
		{name: "chip avança", track: TrackChip, target: ChipShipped, chip: ChipRequested, tracker: TrackerAwaitingSupplier,
			want: []string{ChipShipped}},
		{name: "chip pula etapas (correção da central)", track: TrackChip, target: ChipSeparated, chip: ChipRequested,
			tracker: TrackerAwaitingSupplier, want: []string{ChipSeparated}},
		{name: "chip não volta depois da configuração", track: TrackChip, target: ChipAtBase, chip: ChipSeparated,
			tracker: TrackerConfiguring, refused: true},
		{name: "mesmo status recusado", track: TrackChip, target: ChipShipped, chip: ChipShipped,
			tracker: TrackerAwaitingSupplier, refused: true},
		{name: "rastreador na base sem chip: espera o chip", track: TrackTracker, target: TrackerAtBase,
			chip: ChipShipped, tracker: TrackerAwaitingSupplier, want: []string{TrackerAtBase, TrackerAwaitingChip}},
		{name: "rastreador na base com chip na base: não espera", track: TrackTracker, target: TrackerAtBase,
			chip: ChipAtBase, tracker: TrackerAwaitingSupplier, want: []string{TrackerAtBase}},
		{name: "configurar exige o chip separado", track: TrackTracker, target: TrackerConfiguring,
			chip: ChipAtBase, tracker: TrackerAwaitingChip, refused: true},
		{name: "configurar com o chip separado", track: TrackTracker, target: TrackerConfiguring,
			chip: ChipSeparated, tracker: TrackerAwaitingChip, want: []string{TrackerConfiguring}},
		{name: "configurado exige o aparelho", track: TrackTracker, target: TrackerConfigured,
			chip: ChipSeparated, tracker: TrackerConfiguring, refused: true},
		{name: "configurado com o aparelho", track: TrackTracker, target: TrackerConfigured,
			chip: ChipSeparated, tracker: TrackerConfiguring, hasDevice: true, want: []string{TrackerConfigured}},
		{name: "enviado só pela etiqueta", track: TrackTracker, target: TrackerShipped,
			chip: ChipSeparated, tracker: TrackerConfigured, hasDevice: true, refused: true},
		{name: "entregue em mãos, sem etiqueta", track: TrackTracker, target: TrackerDelivered,
			chip: ChipSeparated, tracker: TrackerConfigured, hasDevice: true, want: []string{TrackerDelivered}},
		{name: "entregue em mãos com etiqueta paga recusado", track: TrackTracker, target: TrackerDelivered,
			chip: ChipSeparated, tracker: TrackerConfigured, hasDevice: true, labeled: true, refused: true},
		{name: "entregue em mãos exige o aparelho", track: TrackTracker, target: TrackerDelivered,
			chip: ChipSeparated, tracker: TrackerConfigured, refused: true},
		{name: "entregue antes de configurado recusado", track: TrackTracker, target: TrackerDelivered,
			chip: ChipSeparated, tracker: TrackerConfiguring, hasDevice: true, refused: true},
		{name: "em trânsito sem etiqueta recusado", track: TrackTracker, target: TrackerInTransit,
			chip: ChipSeparated, tracker: TrackerConfigured, hasDevice: true, refused: true},
		{name: "entregue à mão depois do envio (reserva)", track: TrackTracker, target: TrackerDelivered,
			chip: ChipSeparated, tracker: TrackerInTransit, hasDevice: true, labeled: true, want: []string{TrackerDelivered}},
		{name: "enviado não volta para antes do envio", track: TrackTracker, target: TrackerConfigured,
			chip: ChipSeparated, tracker: TrackerShipped, hasDevice: true, labeled: true, refused: true},
		{name: "entregue pela transportadora não volta para antes do envio", track: TrackTracker, target: TrackerConfigured,
			chip: ChipSeparated, tracker: TrackerDelivered, hasDevice: true, labeled: true, refused: true},
		{name: "entregue em mãos pode ser desfeito", track: TrackTracker, target: TrackerConfigured,
			chip: ChipSeparated, tracker: TrackerDelivered, hasDevice: true, want: []string{TrackerConfigured}},
		{name: "entregue em mãos não vira em trânsito", track: TrackTracker, target: TrackerInTransit,
			chip: ChipSeparated, tracker: TrackerDelivered, hasDevice: true, refused: true},
		{name: "correção para trás antes do envio", track: TrackTracker, target: TrackerAtBase,
			chip: ChipSeparated, tracker: TrackerConfiguring, want: []string{TrackerAtBase}},
		{name: "aguardar chip com o chip já na base não faz sentido", track: TrackTracker, target: TrackerAwaitingChip,
			chip: ChipAtBase, tracker: TrackerAtBase, refused: true},
		{name: "status inexistente", track: TrackTracker, target: "LOST", chip: ChipRequested,
			tracker: TrackerAwaitingSupplier, refused: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := plan(tc.track, tc.target, tc.chip, tc.tracker, tc.hasDevice, tc.labeled)
			if tc.refused {
				var rule RuleError
				if !errors.As(err, &rule) {
					t.Fatalf("esperava recusa, veio %v %v", got, err)
				}
				return
			}
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("plan = %v, %v; quer %v", got, err, tc.want)
			}
		})
	}
}
