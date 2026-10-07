package mail

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestContractCopy(t *testing.T) {
	sender := &captureSender{}
	c := ContractCopy{
		Title: "Contrato de Prestação de Serviços de Rastreamento Veicular", Version: "1", EffectiveDate: "7 de outubro de 2026",
		SHA256: "abc123", Name: "Lia Martins", Document: "123.456.789-09", IP: "200.1.2.3",
		AcceptedAt: time.Date(2026, 10, 7, 15, 30, 0, 0, time.UTC),
		Intro:      []ContractBlock{{Text: "Termo de adesão."}},
		Sections:   []ContractSection{{Title: "6. Permanência mínima e multa", Blocks: []ContractBlock{{Items: []string{"3 meses", "1 mensalidade"}}}}},
	}
	if err := NewContractMailer(sender, "https://farbo.test").ContractAccepted(context.Background(), "lia@x.test", "Lia Martins", c); err != nil {
		t.Fatal(err)
	}
	m := sender.last
	if !strings.Contains(m.Subject, "Sua cópia do contrato") {
		t.Errorf("assunto %q", m.Subject)
	}
	for _, part := range []string{m.Text, m.HTML} {
		for _, want := range []string{"123.456.789-09", "07/10/2026 às 12:30", "200.1.2.3", "6. Permanência mínima e multa", "1 mensalidade", "https://farbo.test/contrato", "abc123"} {
			if !strings.Contains(part, want) {
				t.Errorf("falta %q em:\n%s", want, part)
			}
		}
	}
}
