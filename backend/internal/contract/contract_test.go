package contract

import (
	"strings"
	"testing"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
)

func render(t *testing.T, address string) *Document {
	t.Helper()
	doc, err := Render(Params{
		Company: config.Company{Name: "Farbo Rastreadores", LegalName: "FARBO TECNOLOGIA DE SISTEMAS E CLOUD LTDA",
			CNPJ: "49.757.084/0001-00", Address: address, Email: "contato@farborastreadores.com.br"},
		SuspendAfterDays: 10, HistoryOptions: []int{7, 14, 30},
	})
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

// O contrato tem o que foi pedido: permanência de 3 meses, multa de 1
// mensalidade, negativação com mais de 1 mês de atraso e o porquê (o plano
// M2M e o chip), o rastreador incluso (a Farbo não o vende; o que falta da
// parte dele vence no encerramento antecipado) ou em comodato e o CPF para a
// nota de serviço.
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
		"A Farbo Rastreadores não vende o rastreador", "o Cliente não o compra nem o paga à parte",
		"passa a ser de propriedade dele desde a entrega", "NFS-e (nota fiscal de serviço eletrônica) das mensalidades",
		"mais de 10 dias", "7, 14 ou 30 dias", "art. 49", "MP", "contato@farborastreadores.com.br",
		"exclusivamente o sistema de rastreamento remoto", "comandados pelo próprio Cliente",
		"serviços de segurança ou de vigilância", "central de monitoramento", "seguro veicular",
		"equipe tática ou de pronta-resposta de prontidão", "não garante a recuperação do veículo em caso de roubo ou furto",
		"prazo de arrependimento (cláusula 9)", "nova versão deste contrato (cláusula 14)",
		"o único valor ligado a ele é o frete", "a mensalidade das primeiras mensalidades e a das seguintes",
		"Pessoa física (CPF): no pedido, paga-se a 1ª mensalidade, o frete e a taxa de instalação",
		"A parte do rastreador incluso somada ao plano não é reajustada",
		"por conta própria, com o instalador de preferência dele", "com os instaladores indicados pela Farbo Rastreadores",
		"a 2ª pelo menos um mês depois da 1ª", "Empresa (CNPJ): nada é cobrado no pedido",
		"o proporcional dos dias entre a contratação e o vencimento",
		"a parte do rastreador das mensalidades que faltam vence de uma vez, numa fatura só",
		"Ela não é multa: é o custo do rastreador", "pago aos poucos nas primeiras mensalidades",
		"Rastreador comprado antes da versão 9", "as parcelas restantes vencem de uma vez, numa fatura só",
		// O comodato.
		"o Cliente escolhe no pedido, quando a Farbo Rastreadores oferece o comodato", "arts. 579 a 585 do Código Civil",
		"a mensalidade do comodato pode ser diferente", "A taxa de instalação, quando houver, é cobrada junto com a 1ª mensalidade",
		"a taxa de desinstalação, quando houver",
		"permite a retirada do rastreador no dia e no endereço combinados, em até 30 (trinta) dias",
		"o valor de reposição do mesmo rastreador na data", "comprovado pelo boletim de ocorrência",
		"no comodato, a retirada não tem a taxa de desinstalação", "o rastreador em comodato é devolvido",
		// Os planos semestral e anual e a instalação com ou sem bloqueio (versão 10).
		"cobrada por mês ou, quando o pedido oferecer, por semestre ou por ano",
		"Nos planos semestral e anual (cláusula 5), ele já está no valor do período: nada é somado",
		"6 meses (semestral) ou 12 meses (anual)", "o 2º pelo menos 6 ou 12 meses depois do 1º",
		"o reajuste vale para o valor do período", "não há devolução proporcional",
		"o serviço continua até o último dia do período e a assinatura não renova",
		"o encerramento vale no fim do período (cláusula 7)",
		"Instalação com ou sem bloqueio", "Sem o relé, o bloqueio remoto não funciona",
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
	// Nada de vender o rastreador nem de nota de produto.
	for _, gone := range []string{"NF-e", "rastreador à vista", "preço de venda", "rastreador comprado pelo Cliente"} {
		if strings.Contains(doc.Text, gone) {
			t.Errorf("sobrou %q", gone)
		}
	}
	// As cláusulas viram blocos: a de permanência é uma lista, com o
	// rastreador incluso e o comprado antes; a de preço também.
	if s := doc.Sections[6]; s.Title != "7. Permanência mínima e multa" || len(s.Blocks) != 1 || s.Blocks[0].Kind != "ul" || len(s.Blocks[0].Items) != 7 {
		t.Errorf("cláusula 7 = %+v", s)
	}
	if s := doc.Sections[4]; len(s.Blocks) != 1 || len(s.Blocks[0].Items) != 9 {
		t.Errorf("cláusula 5 = %+v", s)
	}
	if s := doc.Sections[3]; s.Title != "4. O rastreador: incluso ou em comodato" {
		t.Errorf("cláusula 4 = %q", s.Title)
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
		HistoryOptions: []int{7, 14, 30}, SuspendAfterDays: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Title != "Termo de Uso e Comodato do Associado" || doc.Version != AssociateVersion || !IsAssociateVersion(doc.Version) ||
		len(doc.Sections) != 11 {
		t.Fatalf("termo = %q %s, %d cláusulas", doc.Title, doc.Version, len(doc.Sections))
	}
	for _, want := range []string{
		"versão " + AssociateVersion, "a {{Farbo}} não cobra nada do Associado", "a Organização, que cobra dele conforme as regras dela",
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
