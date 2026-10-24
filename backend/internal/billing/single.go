package billing

import (
	"context"
	"fmt"
	"math"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// A fatura única do mês: tudo o que a conta paga vence no dia dela, e a tela
// mostra as linhas daquele dia como uma fatura só (um Pix, um lembrete). Na
// contratação nada é cobrado na hora: o proporcional dos dias até o
// vencimento, a mensalidade, o equipamento (ou a instalação, no comodato) e
// o frete entram na próxima fatura da conta.

// O rastreador da assinatura: vendido ou em comodato (emprestado).
const (
	EquipmentSale = "sale"
	EquipmentLoan = "loan"
)

// ChargeDate é o vencimento em que entra o que a conta contrata hoje: o
// próximo dia da conta depois de hoje; se a fatura desse dia já foi paga
// (toda ou em parte), a do mês seguinte — a fatura paga não ganha linha nova.
func ChargeDate(ctx context.Context, q database.Querier, customerID uuid.UUID, today Date, dueDay int) (Date, error) {
	due := firstDueDate(today.AddDays(1), dueDay)
	for i := 0; i < 3; i++ {
		var paid bool
		if err := q.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM invoices WHERE customer_id = $1 AND due_date = $2 AND status = 'PAID')`,
			customerID, due.Time).Scan(&paid); err != nil {
			return Date{}, database.MapError(err)
		}
		if !paid {
			break
		}
		due = nextMonthly(due, dueDay)
	}
	return due, nil
}

// ChargeDate é o vencimento em que entra o que a conta contrata hoje.
func (s *Service) ChargeDate(ctx context.Context, q database.Querier, customerID uuid.UUID, dueDay int) (Date, error) {
	return ChargeDate(ctx, q, customerID, s.Today(), dueDay)
}

// Prorata é a mensalidade pelos dias de from até to (mês comercial de 30
// dias) e quantos dias são.
func Prorata(monthlyCents int, from, to Date) (cents, days int) {
	days = int(math.Round(to.Sub(from.Time).Hours() / 24))
	if days <= 0 || monthlyCents <= 0 {
		return 0, max(days, 0)
	}
	return int(math.Round(float64(monthlyCents) * float64(days) / 30)), days
}

// ProrataDescription: "Plano Mensal — proporcional de 22/10 a 10/11 (19 dias)".
func ProrataDescription(planName string, from, to Date, days int) string {
	plural := "dias"
	if days == 1 {
		plural = "dia"
	}
	return fmt.Sprintf("%s — proporcional de %02d/%02d a %02d/%02d (%d %s)",
		planName, from.Day(), from.Month(), to.Day(), to.Month(), days, plural)
}
