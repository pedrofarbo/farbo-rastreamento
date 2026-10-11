package mail

import (
	"context"
	"strings"
	"testing"
)

func TestAlertEmailEscapesAndShowsDetails(t *testing.T) {
	speed, limit, lat, lon := 127.4, 100.0, -23.55052, -46.63331
	msg, err := renderAlert(Alert{
		To: "ana@cliente.test", Name: "Ana Souza", Subject: "Alerta: Excesso de velocidade — x",
		Severity: SeverityWarning, Title: "Excesso de velocidade",
		Summary: "O veículo passou do limite.", Vehicle: `<img src=x onerror=alert(1)>`, Plate: "ABC1D23",
		When: "30/09/2026 às 02:14 (horário de Brasília)", Speed: &speed, Limit: &limit,
		Latitude: &lat, Longitude: &lon, Suppressed: 3, Cooldown: "30 minutos",
		ActionURL: "https://painel.test/veiculos/1", SettingsURL: "https://painel.test/alertas",
		AppURL: "https://painel.test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(msg.HTML, "<img src=x") || !strings.Contains(msg.HTML, "&lt;img src=x") {
		t.Error("nome do veículo precisa sair escapado no HTML")
	}
	for _, want := range []string{"Olá, Ana!", "127 km/h", "100 km/h", "-23.55052, -46.63331",
		"https://www.openstreetmap.org/?mlat=-23.550520&amp;mlon=-46.633310", "mais <strong>3</strong> vezes",
		"30 minutos", "https://painel.test/veiculos/1", "https://painel.test/alertas", "#b45309"} {
		if !strings.Contains(msg.HTML, want) {
			t.Errorf("HTML sem %q", want)
		}
	}
	for _, want := range []string{"Excesso de velocidade", "Velocidade: 127 km/h", "Limite: 100 km/h",
		"Mapa: https://www.openstreetmap.org/", "mais 3 vezes", "Escolha quais alertas receber"} {
		if !strings.Contains(msg.Text, want) {
			t.Errorf("texto sem %q", want)
		}
	}
	if msg.To != "ana@cliente.test" || msg.Subject == "" {
		t.Errorf("destinatário/assunto: %q %q", msg.To, msg.Subject)
	}
}

func TestAlertEmailForCentralHasNoSettingsLink(t *testing.T) {
	msg, err := renderAlert(Alert{To: "central@farbo.test", Severity: SeverityCritical, Title: "SOS",
		Summary: "x", Vehicle: "Carro", When: "agora", ActionURL: "https://painel.test/dashboard", AppURL: "https://painel.test"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(msg.HTML, "/alertas") || strings.Contains(msg.Text, "Escolha quais alertas") {
		t.Error("a central não tem tela de preferências: o e-mail não aponta para ela")
	}
	if !strings.Contains(msg.HTML, "Alerta de segurança") || !strings.Contains(msg.HTML, "#b3261e") {
		t.Error("alerta crítico usa a faixa vermelha de segurança")
	}
	if strings.Contains(msg.HTML, "Velocidade") || strings.Contains(msg.HTML, "repetiu") {
		t.Error("sem velocidade nem repetidos, essas linhas não aparecem")
	}
}

// O resumo dos alertas que passaram do teto por hora: quantos, por tipo e
// por veículo, com "e mais" e os links.
func TestAlertDigest(t *testing.T) {
	sender := &captureSender{}
	err := NewAlertMailer(sender).Digest(context.Background(), AlertDigest{
		To: "frota@x.test", Name: "Lia Souza", Count: 37, MaxPerHour: 20,
		Kinds:        []DigestLine{{"Excesso de velocidade", 21}, {"Ignição ligada (qualquer horário)", 16}},
		Vehicles:     []DigestLine{{"Van 0503", 4}, {"Caminhão 0012", 3}},
		MoreVehicles: 12,
		From:         "11/10/2026 às 08:02 (horário de Brasília)", Until: "11/10/2026 às 08:59 (horário de Brasília)",
		ActionURL: "https://farbo.test/dashboard", SettingsURL: "https://farbo.test/alertas",
	})
	if err != nil {
		t.Fatal(err)
	}
	m := sender.last
	if m.Subject != "Resumo: 37 alertas da última hora" {
		t.Errorf("assunto = %q", m.Subject)
	}
	for name, part := range map[string]string{"texto": m.Text, "HTML": m.HTML} {
		for _, want := range []string{"Olá, Lia!", "37 alertas ficaram", "até 20 alertas por hora", "Excesso de velocidade", "21",
			"Van 0503", "e mais 12 veículos", "08:02", "08:59", "https://farbo.test/dashboard", "https://farbo.test/alertas", "botão de pânico"} {
			if !strings.Contains(part, want) {
				t.Errorf("%s sem %q", name, want)
			}
		}
	}
}
