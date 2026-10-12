package mail

import (
	"strings"
	"testing"
)

// O aviso de manutenção: o assunto conforme o que venceu e o que está perto,
// cada linha com o veículo e quanto falta, e o link para a manutenção.
func TestRenderMaintenance(t *testing.T) {
	d := MaintenanceDigest{To: "frota@cliente.test", Name: "Ana Souza", Lines: []MaintenanceLine{
		{Vehicle: "Van <01>", Plate: "NOR1A11", Item: "Troca de óleo", Due: true, Detail: "passou 320 km"},
		{Vehicle: "Carro 02", Item: "Revisão", Detail: "faltam 8 dias"},
	}}
	msg, err := renderMaintenance(d, "https://painel.farbo.test")
	if err != nil {
		t.Fatal(err)
	}
	if msg.Subject != "Manutenção: 1 vencida e 1 perto de vencer" {
		t.Errorf("assunto = %q", msg.Subject)
	}
	for _, want := range []string{"Olá, Ana!", "Troca de óleo · Van <01> (NOR1A11): passou 320 km", "Revisão · Carro 02: faltam 8 dias",
		"https://painel.farbo.test/manutencao"} {
		if !strings.Contains(msg.Text, want) {
			t.Errorf("texto sem %q:\n%s", want, msg.Text)
		}
	}
	if !strings.Contains(msg.HTML, "Van &lt;01&gt;") {
		t.Error("o HTML não escapou o nome do veículo")
	}
	one, _ := renderMaintenance(MaintenanceDigest{Lines: d.Lines[:1]}, "https://painel.farbo.test")
	if one.Subject != "Manutenção vencida: Troca de óleo · Van <01>" {
		t.Errorf("assunto de uma = %q", one.Subject)
	}
}
