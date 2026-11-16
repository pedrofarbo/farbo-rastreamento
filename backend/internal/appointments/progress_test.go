package appointments

import (
	"slices"
	"testing"
	"time"
)

func TestStallOf(t *testing.T) {
	at := time.Date(2026, 11, 16, 14, 0, 0, 0, brt)
	arrived := at.Add(30 * time.Minute)
	cases := []struct {
		name string
		a    Appointment
		now  time.Time
		want string
	}{
		{"agendada, antes da hora", Appointment{Status: StatusScheduled, ScheduledFor: at}, at.Add(-time.Hour), ""},
		{"agendada, 2h depois", Appointment{Status: StatusScheduled, ScheduledFor: at}, at.Add(2 * time.Hour), ""},
		{"agendada, 4h depois", Appointment{Status: StatusScheduled, ScheduledFor: at}, at.Add(4 * time.Hour), StallLate},
		{"no local há 1h", Appointment{Status: StatusArrived, ScheduledFor: at, ArrivedAt: &arrived}, arrived.Add(time.Hour), ""},
		{"no local há 4h", Appointment{Status: StatusArrived, ScheduledFor: at, ArrivedAt: &arrived}, arrived.Add(4 * time.Hour), StallStalled},
		{"concluída", Appointment{Status: StatusDone, ScheduledFor: at, ArrivedAt: &arrived}, at.AddDate(0, 0, 3), ""},
		{"cancelada", Appointment{Status: StatusCanceled, ScheduledFor: at}, at.AddDate(0, 0, 3), ""},
	}
	for _, c := range cases {
		if got := stallOf(&c.a, c.now); got != c.want {
			t.Errorf("%s: %q, queria %q", c.name, got, c.want)
		}
	}
}

func TestProgress(t *testing.T) {
	arrived := time.Date(2026, 11, 16, 17, 44, 0, 0, time.UTC)
	checked := arrived.Add(time.Hour)
	done, missing := Progress(&Appointment{Kind: KindInstall, Status: StatusScheduled})
	if len(done) != 0 || !slices.Equal(missing, []string{"registrar a chegada", "escolher (e ativar) o rastreador", "teste do sinal",
		"foto do rastreador", "concluir a instalação"}) {
		t.Fatalf("agendada: feito %v falta %v", done, missing)
	}
	done, missing = Progress(&Appointment{Kind: KindInstall, Status: StatusArrived, ArrivedAt: &arrived,
		TrackerIMEI: "860752061230249", TrackerConnected: true, CheckedAt: &checked})
	if !slices.Equal(done, []string{"chegada às 14:44", "rastreador de IMEI final 230249 conectado"}) ||
		!slices.Equal(missing, []string{"teste do sinal (o último não passou)", "foto do rastreador", "concluir a instalação"}) {
		t.Fatalf("no local, sem sinal: feito %v falta %v", done, missing)
	}
	done, missing = Progress(&Appointment{Kind: KindInstall, Status: StatusArrived, ArrivedAt: &arrived,
		TrackerIMEI: "860752061230249", CheckedAt: &checked, CheckReady: true, Photos: []string{"tracker"}})
	if !slices.Equal(done, []string{"chegada às 14:44", "rastreador de IMEI final 230249", "sinal testado", "foto do rastreador"}) ||
		!slices.Equal(missing, []string{"concluir a instalação"}) {
		t.Fatalf("só falta concluir: feito %v falta %v", done, missing)
	}
	done, missing = Progress(&Appointment{Kind: KindUninstall, Status: StatusArrived, ArrivedAt: &arrived})
	if !slices.Equal(done, []string{"chegada às 14:44"}) ||
		!slices.Equal(missing, []string{"foto do rastreador retirado", "concluir a retirada"}) {
		t.Fatalf("retirada: feito %v falta %v", done, missing)
	}
}
