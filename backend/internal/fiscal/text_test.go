package fiscal

import "testing"

// A NFS-e é de serviço: a descrição da fatura vai sem o rastreador.
func TestServiceText(t *testing.T) {
	cases := map[string]string{
		// A mensalidade de quem comprou o rastreador parcelado antes do incluso.
		"Especial Insanos MC — novembro/2026 + parcela 1/10 do rastreador":                        "Especial Insanos MC — novembro/2026",
		"Plano Mensal — dezembro/2026 · promoção de pré-lançamento + parcela 12/12 do rastreador": "Plano Mensal — dezembro/2026 · promoção de pré-lançamento",
		// O saldo do rastreador incluso no encerramento antecipado.
		"Rastreador incluso: saldo de 3 de 10 mensalidades (assinatura encerrada antes da 10ª): Gol · ABC1D23": "Encerramento antecipado do plano: saldo de 3 de 10 mensalidades (assinatura encerrada antes da 10ª): Gol · ABC1D23",
		// O resto passa como está.
		"Plano Mensal — novembro/2026":            "Plano Mensal — novembro/2026",
		"Instalação do rastreador: Gol · ABC1D23": "Instalação do rastreador: Gol · ABC1D23",
		"  Plano Anual — janeiro/2027  ":          "Plano Anual — janeiro/2027",
		"":                                        "",
	}
	for in, want := range cases {
		if got := serviceText(in); got != want {
			t.Errorf("serviceText(%q) = %q, quero %q", in, got, want)
		}
	}
}
