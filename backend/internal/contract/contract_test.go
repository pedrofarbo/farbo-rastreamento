package contract

import (
	"strings"
	"testing"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
)

func render(t *testing.T, address string) *Document {
	t.Helper()
	return renderWith(t, address, 10)
}

func renderWith(t *testing.T, address string, maxInstallments int) *Document {
	t.Helper()
	doc, err := Render(Params{
		Company: config.Company{Name: "Farbo Rastreadores", LegalName: "FARBO TECNOLOGIA DE SISTEMAS E CLOUD LTDA",
			CNPJ: "49.757.084/0001-00", Address: address, Email: "contato@farborastreadores.com.br"},
		SuspendAfterDays: 10, HistoryOptions: []int{7, 14, 30}, MaxInstallments: maxInstallments,
	})
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

// O contrato tem o que foi pedido: permanência de 3 meses, multa de 1
// mensalidade, negativação com mais de 1 mês de atraso e o porquê (o plano
// M2M e o chip), o rastreador comprado ou em comodato e o CPF para as notas
// fiscais.
func TestContractText(t *testing.T) {
	doc := render(t, "")
	if doc.Title != "Contrato de Prestação de Serviços de Rastreamento Veicular" || len(doc.Sections) != 16 || doc.Version != Version {
		t.Fatalf("contrato = %q, %d cláusulas", doc.Title, len(doc.Sections))
	}
	for _, want := range []string{
		"FARBO TECNOLOGIA DE SISTEMAS E CLOUD LTDA", "CNPJ sob o nº 49.757.084/0001-00",
		"permanência mínima de 3 (três) meses", "multa equivalente a 1 (uma) mensalidade",
		"atraso de mais de 1 (um) mês", "órgãos de proteção ao crédito", "art. 43, § 2º",
		"plano de telecomunicações M2M", "geração, a habilitação e a entrega do chip",
		"o rastreador comprado pelo Cliente é de propriedade exclusiva dele", "NF-e", "NFS-e",
		"mais de 10 dias", "7, 14 ou 30 dias", "art. 49", "MP", "contato@farborastreadores.com.br",
		"exclusivamente o sistema de rastreamento remoto", "comandados pelo próprio Cliente",
		"serviços de segurança ou de vigilância", "central de monitoramento", "seguro veicular",
		"equipe tática ou de pronta-resposta de prontidão", "não garante a recuperação do veículo em caso de roubo ou furto",
		"prazo de arrependimento (cláusula 9)", "nova versão deste contrato (cláusula 14)",
		"parcelado em até 10 (dez) vezes sem juros, por Pix", "a 1ª fatura traz o frete", "a 1ª junto com a 1ª mensalidade",
		"Nada é cobrado no pedido", "o proporcional dos dias entre a contratação e o vencimento",
		"ativa até a mensalidade que traz a última parcela", "as parcelas restantes vencem de uma vez, numa fatura só",
		"não são multa: são o preço do rastreador",
		// O comodato.
		"comprado pelo Cliente ou emprestado a ele em comodato", "arts. 579 a 585 do Código Civil",
		"A taxa de instalação, quando o plano prevê, vem na 1ª fatura", "a taxa de desinstalação, quando o plano prevê",
		"permite a retirada do rastreador no dia e no endereço combinados, em até 30 (trinta) dias",
		"o preço de venda do mesmo rastreador na plataforma na data", "comprovado pelo boletim de ocorrência",
		"no comodato, a retirada não tem a taxa de desinstalação", "o rastreador em comodato é devolvido",
	} {
		if want == "MP" {
			want = "Medida Provisória nº 2.200-2/2001"
		}
		if !strings.Contains(doc.Text, want) {
			t.Errorf("falta %q", want)
		}
	}
	if strings.Contains(doc.Text, "{{") || strings.Contains(doc.Text, "com sede em") {
		t.Error("sobrou marcação ou endereço vazio")
	}
	// As cláusulas viram blocos: a de permanência é uma lista (com o
	// parcelamento, um item a mais; o de preço também).
	if s := doc.Sections[6]; s.Title != "7. Permanência mínima e multa" || len(s.Blocks) != 1 || s.Blocks[0].Kind != "ul" || len(s.Blocks[0].Items) != 5 {
		t.Errorf("cláusula 7 = %+v", s)
	}
	if s := doc.Sections[4]; len(s.Blocks) != 1 || len(s.Blocks[0].Items) != 7 {
		t.Errorf("cláusula 5 = %+v", s)
	}
	// Só à vista: o contrato não fala em parcelamento.
	cash := renderWith(t, "", 1)
	if strings.Contains(cash.Text, "parcela") || len(cash.Sections[6].Blocks[0].Items) != 4 || len(cash.Sections[4].Blocks[0].Items) != 6 {
		t.Errorf("contrato sem parcelamento:\n%s", cash.Text)
	}
	// Com o endereço, ele entra; o hash muda junto com o texto.
	withAddress := render(t, "Rua Exemplo, 100, São Paulo/SP")
	if !strings.Contains(withAddress.Text, "com sede em Rua Exemplo, 100, São Paulo/SP") || withAddress.SHA256 == doc.SHA256 {
		t.Error("endereço")
	}
	if render(t, "").SHA256 != doc.SHA256 {
		t.Error("o hash não é estável")
	}
}

// O termo do associado: quem paga é a organização (a Farbo não cobra o
// associado), o rastreador em comodato com a devolução, o que a organização
// vê e o aceite eletrônico; nada de mensalidade, multa ou negativação.
func TestAssociateTermText(t *testing.T) {
	doc, err := RenderAssociate(Params{
		Company:        config.Company{Name: "Farbo Rastreadores", LegalName: "FARBO TECNOLOGIA DE SISTEMAS E CLOUD LTDA", CNPJ: "49.757.084/0001-00", Email: "contato@farborastreadores.com.br"},
		HistoryOptions: []int{7, 14, 30}, SuspendAfterDays: 10, MaxInstallments: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Title != "Termo de Uso e Comodato do Associado" || doc.Version != AssociateVersion || !IsAssociateVersion(doc.Version) ||
		len(doc.Sections) != 11 {
		t.Fatalf("termo = %q %s, %d cláusulas", doc.Title, doc.Version, len(doc.Sections))
	}
	for _, want := range []string{
		"versão A1", "a {{Farbo}} não cobra nada do Associado", "a Organização, que cobra dele conforme as regras dela",
		"fica com o Associado em comodato", "arts. 579 a 585 do Código Civil", "em até 30 (trinta) dias",
		"comprovado pelo boletim de ocorrência", "bloquear e desbloquear o motor remotamente",
		"Medida Provisória nº 2.200-2/2001", "7, 14 ou 30 dias", "contato@farborastreadores.com.br",
	} {
		want = strings.ReplaceAll(want, "{{Farbo}}", "Farbo Rastreadores")
		if !strings.Contains(doc.Text, want) {
			t.Errorf("falta %q", want)
		}
	}
	for _, unwanted := range []string{"multa", "proteção ao crédito", "permanência mínima", "NFS-e", "{{"} {
		if strings.Contains(doc.Text, unwanted) {
			t.Errorf("o termo do associado não fala em %q", unwanted)
		}
	}
	if contract, _ := Render(Params{Company: config.Company{Name: "Farbo"}, HistoryOptions: []int{7}}); IsAssociateVersion(contract.Version) {
		t.Error("o contrato dos clientes não tem o prefixo A")
	}
}
