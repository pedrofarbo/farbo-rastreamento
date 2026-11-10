package fiscal

import (
	"regexp"
	"strings"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
)

// installmentText é a parcela do rastreador vendido antes do rastreador
// incluso, na descrição da mensalidade (" + parcela 2/10 do rastreador").
var installmentText = regexp.MustCompile(`\s*\+ parcela \d+/\d+ do rastreador`)

// serviceText é a descrição da fatura como vai na NFS-e: só serviço. A Farbo
// não vende o rastreador, e a nota de serviço não fala dele — nem da parcela
// das assinaturas de antes do rastreador incluso (o valor da nota segue o do
// mês inteiro), nem do saldo no encerramento antecipado (vira o saldo das
// mensalidades do plano). A fatura do cliente continua com o texto dela.
func serviceText(description string) string {
	text := installmentText.ReplaceAllString(strings.TrimSpace(description), "")
	if rest, ok := strings.CutPrefix(text, billing.TrackerBalancePrefix); ok {
		text = "Encerramento antecipado do plano: saldo" + rest
	}
	return strings.TrimSpace(text)
}
