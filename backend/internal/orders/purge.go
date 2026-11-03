package orders

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// A contratação pelo site que não foi paga: a conta nasce na contratação
// (para a pessoa ver e pagar o Pix), mas só vira cliente quando paga — até
// lá, checkout_pending esconde ela da central. Vencido o pagamento sem pagar,
// o cadastro sai: a conta, o veículo, o pedido e as faturas. O pré-cliente
// fica, com a anotação, para a central poder ligar.

const (
	// checkoutPayWindow: o Pix da contratação vale 24 horas; antes disso,
	// mesmo sem cobrança gerada, o cadastro fica.
	checkoutPayWindow = 24 * time.Hour
	// pixGrace: a folga depois de o Pix vencer (a confirmação atrasada).
	pixGrace = time.Hour
	// boletoGrace: o boleto pago no vencimento compensa em até 3 dias úteis.
	boletoGrace = 5 * 24 * time.Hour
)

// PurgeUnpaidCheckouts apaga os cadastros da contratação pelo site sem
// pagamento, quando nenhuma cobrança deles ainda pode ser paga. Devolve
// quantos saíram.
func (s *Service) PurgeUnpaidCheckouts(ctx context.Context, now time.Time) (int, error) {
	rows, err := s.db.Query(ctx, `
		SELECT u.id FROM users u
		WHERE u.checkout_at IS NOT NULL AND u.checkout_at < $1 AND checkout_pending(u.id)
			AND NOT EXISTS (
				SELECT 1 FROM payment_charges pc JOIN invoices i ON i.id = pc.invoice_id
				WHERE i.customer_id = u.id AND pc.status IN ('PENDING', 'CREATING')
					AND COALESCE(pc.expires_at, pc.created_at + INTERVAL '1 day') >
						CASE WHEN pc.method = 'BOLETO' THEN $2::timestamptz ELSE $3::timestamptz END)`,
		now.Add(-checkoutPayWindow-pixGrace), now.Add(-boletoGrace), now.Add(-pixGrace))
	if err != nil {
		return 0, database.MapError(err)
	}
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, database.MapError(err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, database.MapError(err)
	}
	purged := 0
	for _, id := range ids {
		if err := s.purgeCheckout(ctx, id, now); err != nil {
			s.log.Error("falha ao apagar a contratação pelo site sem pagamento", "customer", id, "err", err)
			continue
		}
		s.log.Info("contratação pelo site sem pagamento apagada", "customer", id)
		purged++
	}
	return purged, nil
}

// purgeCheckout apaga um cadastro (conferindo de novo que segue sem
// pagamento) e anota no pré-cliente.
func (s *Service) purgeCheckout(ctx context.Context, id uuid.UUID, now time.Time) error {
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var pending bool
		var created time.Time
		if err := tx.QueryRow(ctx, `SELECT checkout_pending(id), checkout_at FROM users WHERE id = $1 FOR UPDATE`, id).
			Scan(&pending, &created); err != nil {
			return database.MapError(err)
		}
		if !pending {
			return nil
		}
		brt := time.FixedZone("BRT", -3*60*60)
		note := "Contratou pelo site em " + created.In(brt).Format("02/01") + " e não pagou: o cadastro foi apagado em " +
			now.In(brt).Format("02/01") + "."
		if _, err := tx.Exec(ctx, `
			UPDATE leads SET status = 'NEW', notes = btrim(notes || E'\n' || $2), updated_at = NOW()
			WHERE customer_id = $1`, id, note); err != nil {
			return database.MapError(err)
		}
		// O veículo não sai junto com a conta (fica sem dono): sai antes.
		if _, err := tx.Exec(ctx, `DELETE FROM vehicles WHERE owner_id = $1`, id); err != nil {
			return database.MapError(err)
		}
		_, err := tx.Exec(ctx, `DELETE FROM users WHERE id = $1`, id)
		return database.MapError(err)
	})
}
