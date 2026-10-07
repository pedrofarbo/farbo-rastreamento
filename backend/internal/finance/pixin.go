package finance

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/payments/abacatepay"
)

// A tarifa do Pix recebido. A AbacatePay desconta a tarifa (R$ 0,80 por
// transação) do valor que o cliente paga: a fatura entra cheia no caixa e a
// tarifa sai como despesa paga no mesmo dia — o saldo bate com o dela.

// RecordReceivedFees lança a tarifa de cada Pix recebido que ainda não tem a
// sua: pago, estornado ou em disputa (a tarifa foi cobrada). Uma vez por
// cobrança; o Pix de testes não tem tarifa.
func (s *Service) RecordReceivedFees(ctx context.Context) {
	rows, err := s.db.Query(ctx, `
		SELECT id FROM payment_charges
		WHERE status IN ('PAID', 'REFUNDED', 'UNDER_DISPUTE') AND NOT dev_mode AND paid_at IS NOT NULL
			AND fee_entry_id IS NULL
		ORDER BY paid_at LIMIT 100`)
	if err != nil {
		s.log.Error("falha ao listar as tarifas do Pix recebido", "err", err)
		return
	}
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		if err := s.recordReceivedFee(ctx, id); err != nil {
			s.log.Error("falha ao lançar a tarifa do Pix recebido", "charge", id, "err", err)
		}
	}
}

func (s *Service) recordReceivedFee(ctx context.Context, chargeID uuid.UUID) error {
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var fee int64
		var paidAt time.Time
		var provider, invoice, customer string
		err := tx.QueryRow(ctx, `
			SELECT c.platform_fee_cents, c.paid_at, c.provider_charge_id, i.description, COALESCE(u.name, '')
			FROM payment_charges c JOIN invoices i ON i.id = c.invoice_id LEFT JOIN users u ON u.id = i.customer_id
			WHERE c.id = $1 AND c.fee_entry_id IS NULL FOR UPDATE OF c`, chargeID).
			Scan(&fee, &paidAt, &provider, &invoice, &customer)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // outra instância lançou
		}
		if err != nil {
			return err
		}
		if fee <= 0 {
			fee = abacatepay.PixReceiveFeeCents
		}
		category, ok, err := feeCategoryID(ctx, tx)
		if err != nil {
			return err
		}
		if !ok {
			s.log.Warn("tarifa do Pix recebido sem categoria; não lançada", "charge", chargeID)
			return nil
		}
		day := paidAt.In(s.loc)
		description := "Tarifa do Pix recebido (AbacatePay): " + invoice
		if customer != "" {
			description += " · " + customer
		}
		var entry uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO finance_entries (kind, description, category_id, amount_cents, due_date, status, paid_on, paid_cents,
				payment_method, notes)
			VALUES ('PAYABLE', $1, $2, $3, $4::date, 'PAID', $4::date, $3, 'PIX', $5) RETURNING id`,
			truncateText(description, maxText), category, fee, day.Format("2006-01-02"), "Cobrança "+provider).Scan(&entry); err != nil {
			return database.MapError(err)
		}
		_, err = tx.Exec(ctx, `UPDATE payment_charges SET fee_entry_id = $2 WHERE id = $1`, chargeID, entry)
		return err
	})
}
