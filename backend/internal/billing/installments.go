package billing

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// O rastreador parcelado sem juros, por Pix: no pedido, só o frete; as
// parcelas vêm somadas às mensalidades da assinatura, uma por mês (a 1ª na
// 1ª mensalidade). Ela fica ativa até a última parcela (CommitmentUntil); se
// for encerrada antes, o saldo vira uma fatura só — ou é dispensado
// (arrependimento, pedido desfeito).

// MaxInstallments é o teto do banco; o catálogo diz quantas vezes oferece.
const MaxInstallments = 12

// SplitInstallments divide o valor em n parcelas: a 1ª leva os centavos que
// sobram da divisão (R$ 120,00 em 7x: 17,16 + 6 × 17,14).
func SplitInstallments(totalCents, n int) []int {
	if n < 1 {
		n = 1
	}
	base := totalCents / n
	out := make([]int, n)
	for i := range out {
		out[i] = base
	}
	out[0] = totalCents - base*(n-1)
	return out
}

// PlanInstallments marca a assinatura como de rastreador parcelado em n
// vezes: a parcela k vai na mensalidade k, e a permanência vai até o
// vencimento da que traz a última.
func (s *Subscription) PlanInstallments(n int) {
	last := s.NextDueDate
	for i := 1; i < n; i++ {
		last = nextMonthly(last, s.DueDay)
	}
	s.Installments, s.CommitmentUntil = n, &last
}

// InsertInstallments grava as parcelas da assinatura, todas por cobrar: a
// geração das mensalidades leva uma por mês.
func InsertInstallments(ctx context.Context, q database.Querier, subscriptionID uuid.UUID, amounts []int) error {
	for i, cents := range amounts {
		if _, err := q.Exec(ctx, `
			INSERT INTO equipment_installments (subscription_id, number, amount_cents)
			VALUES ($1, $2, $3)`, subscriptionID, i+1, cents); err != nil {
			return err
		}
	}
	return nil
}

// pendingInstallment é a próxima parcela ainda sem fatura.
type pendingInstallment struct {
	id     uuid.UUID
	number int
	cents  int
}

// nextPending trava e devolve a próxima parcela sem fatura (nil: nenhuma).
func nextPending(ctx context.Context, tx pgx.Tx, subscriptionID uuid.UUID) (*pendingInstallment, error) {
	var p pendingInstallment
	err := tx.QueryRow(ctx, `
		SELECT id, number, amount_cents FROM equipment_installments
		WHERE subscription_id = $1 AND invoice_id IS NULL AND waived_at IS NULL
		ORDER BY number LIMIT 1
		FOR UPDATE`, subscriptionID).Scan(&p.id, &p.number, &p.cents)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// installmentLabel: " + parcela 2/10 do rastreador".
func installmentLabel(number, total int) string {
	return fmt.Sprintf(" + parcela %d/%d do rastreador", number, total)
}

// releaseInstallments solta as parcelas de uma fatura cancelada: com a
// assinatura ativa, voltam para a próxima mensalidade; encerrada, ficam
// dispensadas (quem cancelou a fatura decidiu não cobrar).
func releaseInstallments(ctx context.Context, q database.Querier, invoiceID uuid.UUID) error {
	_, err := q.Exec(ctx, `
		UPDATE equipment_installments ei
		SET invoice_id = NULL, waived_at = CASE WHEN s.status = 'ACTIVE' THEN NULL ELSE NOW() END
		FROM subscriptions s
		WHERE s.id = ei.subscription_id AND ei.invoice_id = $1`, invoiceID)
	return err
}

// installmentSummary é o andamento das parcelas de uma assinatura.
type installmentSummary struct {
	total, regular, paid, due int
}

// fillInstallments preenche o andamento das parcelas das assinaturas
// parceladas (uma consulta para todas).
func (r *Repository) fillInstallments(ctx context.Context, subs ...*Subscription) error {
	ids := make([]uuid.UUID, 0, len(subs))
	for _, s := range subs {
		if s != nil && s.Installments > 0 {
			ids = append(ids, s.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := r.db.Query(ctx, `
		SELECT ei.subscription_id,
			sum(ei.amount_cents),
			min(ei.amount_cents),
			count(*) FILTER (WHERE i.status = 'PAID'),
			COALESCE(sum(ei.amount_cents) FILTER (WHERE ei.waived_at IS NULL AND i.status IS DISTINCT FROM 'PAID'), 0)
		FROM equipment_installments ei
		LEFT JOIN invoices i ON i.id = ei.invoice_id
		WHERE ei.subscription_id = ANY($1)
		GROUP BY ei.subscription_id`, ids)
	if err != nil {
		return database.MapError(err)
	}
	defer rows.Close()
	got := map[uuid.UUID]installmentSummary{}
	for rows.Next() {
		var id uuid.UUID
		var sum installmentSummary
		if err := rows.Scan(&id, &sum.total, &sum.regular, &sum.paid, &sum.due); err != nil {
			return err
		}
		got[id] = sum
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, s := range subs {
		if sum, ok := got[s.ID]; ok && s.Installments > 0 {
			s.EquipmentCents, s.InstallmentCents, s.InstallmentsPaid, s.InstallmentsDueCents = sum.total, sum.regular, sum.paid, sum.due
		}
	}
	return nil
}

// Os destinos do saldo do rastreador quando a assinatura parcelada é
// encerrada antes da última parcela.
const (
	// BalanceCharge: as parcelas que faltam vencem numa fatura só.
	BalanceCharge = "CHARGE"
	// BalanceWaive: não são cobradas (arrependimento, pedido desfeito).
	BalanceWaive = "WAIVE"
)

// ErrBalanceChoice: a assinatura tem parcelas do rastreador por pagar e o
// encerramento precisa dizer o que fazer com elas.
var ErrBalanceChoice = errors.New("o rastreador desta assinatura tem parcelas por pagar: escolha se o saldo será cobrado numa fatura ou dispensado")

// settleBalance resolve as parcelas que ficaram sem fatura no encerramento
// (as das mensalidades futuras canceladas voltam para cá): cobradas numa
// fatura avulsa, que vence em dueDays, ou dispensadas. As que estão em fatura
// vencida continuam nela (são dívida, como a mensalidade).
func settleBalance(ctx context.Context, tx pgx.Tx, sub *Subscription, choice string, today Date, dueDays int) (*Invoice, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, number, amount_cents FROM equipment_installments
		WHERE subscription_id = $1 AND invoice_id IS NULL AND waived_at IS NULL
		ORDER BY number
		FOR UPDATE`, sub.ID)
	if err != nil {
		return nil, err
	}
	var pending []pendingInstallment
	for rows.Next() {
		var p pendingInstallment
		if err := rows.Scan(&p.id, &p.number, &p.cents); err != nil {
			rows.Close()
			return nil, err
		}
		pending = append(pending, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil || len(pending) == 0 {
		return nil, err
	}
	ids := make([]uuid.UUID, len(pending))
	total := 0
	for i, p := range pending {
		ids[i], total = p.id, total+p.cents
	}
	if choice == BalanceWaive {
		_, err := tx.Exec(ctx, `UPDATE equipment_installments SET waived_at = $2 WHERE id = ANY($1)`, ids, time.Now())
		return nil, err
	}
	numbers := fmt.Sprintf("parcela %d", pending[0].number)
	if len(pending) > 1 {
		numbers = fmt.Sprintf("parcelas %d a %d", pending[0].number, pending[len(pending)-1].number)
	}
	inv, err := InsertInvoice(ctx, tx, &Invoice{
		CustomerID: sub.CustomerID,
		Description: fmt.Sprintf("Saldo do rastreador: %s de %d (assinatura encerrada antes da última parcela)",
			numbers, sub.Installments),
		AmountCents: total,
		DueDate:     today.AddDays(dueDays),
	})
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE equipment_installments SET invoice_id = $2 WHERE id = ANY($1)`, ids, inv.ID); err != nil {
		return nil, err
	}
	return inv, nil
}

// hasUnpaidInstallments diz se a assinatura tem parcela do rastreador ainda
// não paga (nem dispensada).
func hasUnpaidInstallments(ctx context.Context, q database.Querier, subscriptionID uuid.UUID) (bool, error) {
	var unpaid bool
	err := q.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM equipment_installments ei LEFT JOIN invoices i ON i.id = ei.invoice_id
			WHERE ei.subscription_id = $1 AND ei.waived_at IS NULL AND i.status IS DISTINCT FROM 'PAID'
		)`, subscriptionID).Scan(&unpaid)
	return unpaid, err
}
