package mail

import (
	"strings"
	"testing"
)

// O relatório da frota: o assunto com o período e o grupo, os totais e os
// que mais rodaram no texto e no HTML, a planilha anexa; a cópia para um
// e-mail a mais diz quem o incluiu.
func TestRenderFleetReport(t *testing.T) {
	r := FleetReport{
		To: "frota@cliente.test", Name: "Ana Souza", Kind: "semanal", Period: "05/10 a 11/10/2026", Group: "Norte",
		Vehicles: 12, Active: 10, Km: "1.234", Moving: "48:10 h", Trips: "87", Overspeeds: 3,
		Top:         []FleetReportLine{{Name: "Van <01>", Plate: "NOR1A11", Km: "321,5", Trips: 9, Overspeeds: 2}},
		More:        9,
		Attachments: []Attachment{{Name: "farbo-resumo-2026-10-05.csv", Data: []byte("x")}},
	}
	msg, err := renderFleetReport(r, "https://painel.farbo.test")
	if err != nil {
		t.Fatal(err)
	}
	if msg.Subject != "Relatório semanal da frota · 05/10 a 11/10/2026 · Norte" || len(msg.Attachments) != 1 {
		t.Errorf("assunto/anexos = %q, %d", msg.Subject, len(msg.Attachments))
	}
	for _, want := range []string{"Olá, Ana!", "10 de 12", "1.234", "Van <01> (NOR1A11): 321,5 km, 9 viagens, 2 excessos",
		"e mais 9 veículos na planilha", "https://painel.farbo.test/relatorios"} {
		if !strings.Contains(msg.Text, want) {
			t.Errorf("texto sem %q:\n%s", want, msg.Text)
		}
	}
	if !strings.Contains(msg.HTML, "Van &lt;01&gt;") || strings.Contains(msg.HTML, "Van <01>") {
		t.Error("o HTML não escapou o nome do veículo")
	}

	r.To, r.Name, r.ForwardedFrom = "socio@cliente.test", "", "Frota Ltda"
	copy, err := renderFleetReport(r, "https://painel.farbo.test")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(copy.Text, "Frota Ltda incluiu este e-mail") || !strings.Contains(copy.Text, "peça a Frota Ltda") {
		t.Errorf("cópia:\n%s", copy.Text)
	}
}
