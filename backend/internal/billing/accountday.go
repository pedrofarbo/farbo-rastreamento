package billing

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// O vencimento da conta: todas as assinaturas ativas de um cliente vencem no
// mesmo dia, para as mensalidades do mês (uma por veículo, numa frota) saírem
// juntas — um lembrete, um Pix. O veículo novo entra no dia da conta, e mudar
// o dia muda a conta inteira (ChangeAccountDueDay).

// AccountDueDay é o dia de vencimento da conta: o das assinaturas ativas do
// cliente. Numa conta de antes da regra, com dias diferentes, o mais comum
// (no empate, o da mais antiga). Zero sem nenhuma ativa.
func AccountDueDay(ctx context.Context, q database.Querier, customerID uuid.UUID) (int, error) {
	var day int
	err := q.QueryRow(ctx, `
		SELECT due_day FROM subscriptions WHERE customer_id = $1 AND status = 'ACTIVE'
		GROUP BY due_day ORDER BY count(*) DESC, min(created_at) LIMIT 1`, customerID).Scan(&day)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return day, database.MapError(err)
}

// AccountDueDay é o dia de vencimento da conta (zero sem assinatura ativa).
func (s *Service) AccountDueDay(ctx context.Context, customerID uuid.UUID) (int, error) {
	return AccountDueDay(ctx, s.repo.db, customerID)
}

// AlignDueDay põe a assinatura nova no dia da conta — dentro da transação
// do pedido, com o cliente travado, para dois pedidos juntos não saírem em
// dias diferentes. Sem um primeiro vencimento combinado, a primeira
// mensalidade é o próximo dia da conta; com ele, o mesmo mês no dia da conta
// (ou o mês seguinte, se esse dia já passou).
func (s *Service) AlignDueDay(ctx context.Context, q database.Querier, sub *Subscription, agreedFirst bool) error {
	day, err := AccountDueDay(ctx, q, sub.CustomerID)
	if err != nil || day == 0 || day == sub.DueDay {
		return err
	}
	today := s.Today()
	next := firstDueDate(today, day)
	if agreedFirst {
		if next = withDay(sub.NextDueDate, day); next.Before(today) {
			next = nextMonthly(next, day)
		}
	}
	sub.DueDay, sub.NextDueDate = day, next
	return nil
}
