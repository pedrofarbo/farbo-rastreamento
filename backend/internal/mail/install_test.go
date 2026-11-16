package mail

import (
	"context"
	"strings"
	"testing"
	"time"
)

// O aviso da instalação parada: sem a chegada (o dia e a hora marcados) e no
// local sem concluir (a hora da chegada), com o que foi feito e o que falta;
// ao representante, o link do instalador.
func TestInstallStalled(t *testing.T) {
	brt := time.FixedZone("BRT", -3*60*60)
	s := &listSender{}
	m := NewInstallMailer(s, "https://painel.farbo.test/")
	when := time.Date(2026, 11, 12, 13, 0, 0, 0, brt)
	err := m.InstallStalled(context.Background(), "central@farbo.test", StalledInstall{
		Late: true, What: "Instalação", VehicleName: "Mercedes 16.30", Plate: "ABC1D23", CustomerName: "Ednei",
		InstallerName: "Rafael", ScheduledFor: when, Missing: []string{"registrar a chegada", "concluir a instalação"},
		ActionURL: "https://painel.farbo.test/agenda", ActionLabel: "Ver a agenda",
	})
	if err != nil {
		t.Fatal(err)
	}
	arrived := time.Date(2026, 11, 16, 14, 44, 0, 0, brt)
	err = m.InstallStalled(context.Background(), "rafa@rep.test", StalledInstall{
		What: "Instalação", VehicleName: "Ford New Fiesta", ScheduledFor: arrived.Add(16 * time.Minute), ArrivedAt: &arrived,
		Done: []string{"chegada às 14:44", "sinal testado"}, Missing: []string{"foto do rastreador", "concluir a instalação"},
		ForInstaller: true, ActionURL: "https://painel.farbo.test/instalador/tok", ActionLabel: "Abrir o link do instalador",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.sent) != 2 {
		t.Fatalf("enviados = %d", len(s.sent))
	}
	late, stalled := s.sent[0], s.sent[1]
	if late.Subject != "Instalação parada: Mercedes 16.30 · ABC1D23" || stalled.Subject != "Instalação parada: Ford New Fiesta" {
		t.Errorf("assuntos = %q / %q", late.Subject, stalled.Subject)
	}
	for _, want := range []string{"A instalação não começou pelo link", "marcada para qui. 12/11 às 13:00", "Falta: registrar a chegada, concluir a instalação.",
		"https://painel.farbo.test/agenda"} {
		if !strings.Contains(late.Text, want) || !strings.Contains(late.HTML, want) {
			t.Errorf("sem a chegada, falta %q:\n%s", want, late.Text)
		}
	}
	if strings.Contains(late.Text, "abra o link") {
		t.Errorf("a central recebeu o texto do instalador:\n%s", late.Text)
	}
	for _, want := range []string{"A instalação não foi concluída", "chegou no dia marcado às 14:44", "Já feito: chegada às 14:44, sinal testado.",
		"Falta: foto do rastreador, concluir a instalação.", "abra o link e termine os passos que faltam",
		"https://painel.farbo.test/instalador/tok"} {
		if !strings.Contains(stalled.Text, want) || !strings.Contains(stalled.HTML, want) {
			t.Errorf("no local, falta %q:\n%s", want, stalled.Text)
		}
	}
}
