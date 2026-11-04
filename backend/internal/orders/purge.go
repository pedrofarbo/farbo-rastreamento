package orders

import (
	"context"
	"errors"
	"strings"
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

// payableCharges são as cobranças da conta (customer: a expressão SQL) que
// ainda podem ser pagas — o Pix até vencer (com a folga da confirmação), o
// boleto até compensar —, a que vence por último primeiro. $2 e $3 são os
// limites do boleto e do Pix (payLimits).
func payableCharges(customer string) string {
	return `
		SELECT pc.invoice_id, COALESCE(pc.expires_at, pc.created_at + INTERVAL '1 day') AS until
		FROM payment_charges pc JOIN invoices i ON i.id = pc.invoice_id
		WHERE i.customer_id = ` + customer + ` AND pc.status IN ('PENDING', 'CREATING')
			AND COALESCE(pc.expires_at, pc.created_at + INTERVAL '1 day') >
				CASE WHEN pc.method = 'BOLETO' THEN $2::timestamptz ELSE $3::timestamptz END
		ORDER BY until DESC`
}

// payLimits são os $2 e $3 de payableCharges.
func payLimits(now time.Time) (boleto, pix time.Time) {
	return now.Add(-boletoGrace), now.Add(-pixGrace)
}

// PurgeUnpaidCheckouts apaga os cadastros da contratação pelo site sem
// pagamento, quando nenhuma cobrança deles ainda pode ser paga. Devolve
// quantos saíram.
func (s *Service) PurgeUnpaidCheckouts(ctx context.Context, now time.Time) (int, error) {
	boleto, pix := payLimits(now)
	rows, err := s.db.Query(ctx, `
		SELECT u.id FROM users u
		WHERE u.checkout_at IS NOT NULL AND u.checkout_at < $1 AND checkout_pending(u.id)
			AND NOT EXISTS (`+payableCharges("u.id")+`)`,
		now.Add(-checkoutPayWindow-pixGrace), boleto, pix)
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
		removed, err := s.purgeCheckout(ctx, id, now)
		if err != nil {
			s.log.Error("falha ao apagar a contratação pelo site sem pagamento", "customer", id, "err", err)
			continue
		}
		if removed {
			s.log.Info("contratação pelo site sem pagamento apagada", "customer", id)
			purged++
		}
	}
	return purged, nil
}

// UnpaidCheckout é a contratação pelo site sem pagamento de quem contrata de
// novo com o mesmo e-mail. Released: nada dela podia mais ser pago e o
// cadastro saiu (a pessoa contrata de novo); senão, InvoiceID (uma fatura em
// aberto, para o link de pagamento) e até quando a cobrança vale.
type UnpaidCheckout struct {
	CustomerID   uuid.UUID
	Released     bool
	InvoiceID    uuid.UUID
	PayableUntil time.Time
}

// ReleaseUnpaidCheckout é para quem contrata pelo site com o e-mail de uma
// contratação que não foi paga. Nada que ainda possa ser pago, o cadastro
// antigo sai na hora: para a dona (owner: a senha conferiu), que está
// recomeçando, mesmo sem ter gerado o Pix; para quem não tem a senha, só
// depois do prazo de pagar da contratação (o critério da limpeza, sem
// esperar a hora dela). Senão, devolve o que pagar e até quando. nil: o
// e-mail não é de uma contratação sem pagamento (é de uma conta de verdade).
func (s *Service) ReleaseUnpaidCheckout(ctx context.Context, email string, owner uuid.UUID, now time.Time) (*UnpaidCheckout, error) {
	var out UnpaidCheckout
	var at time.Time
	err := s.db.QueryRow(ctx, `
		SELECT id, checkout_at FROM users WHERE lower(email) = lower($1) AND role = 'customer' AND checkout_at IS NOT NULL
			AND checkout_pending(id)`, strings.TrimSpace(email)).Scan(&out.CustomerID, &at)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, database.MapError(err)
	}
	if owner == out.CustomerID || at.Before(now.Add(-checkoutPayWindow-pixGrace)) {
		removed, err := s.purgeCheckout(ctx, out.CustomerID, now)
		if err != nil {
			return nil, err
		}
		if removed {
			s.log.Info("contratação pelo site sem pagamento apagada: a pessoa contratou de novo", "customer", out.CustomerID)
			out.Released = true
			return &out, nil
		}
	}
	// Ainda pode ser paga: o link vai para uma fatura em aberto (a da
	// cobrança pode ter sido cancelada, como o frete que saiu do pedido).
	boleto, pix := payLimits(now)
	var invoice uuid.UUID
	if err := s.db.QueryRow(ctx, payableCharges("$1")+` LIMIT 1`, out.CustomerID, boleto, pix).
		Scan(&invoice, &out.PayableUntil); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, database.MapError(err)
	}
	if out.PayableUntil.IsZero() {
		// Sem Pix gerado: o link de pagamento vale o prazo da contratação.
		out.PayableUntil = at.Add(checkoutPayWindow)
	}
	err = s.db.QueryRow(ctx, `SELECT id FROM invoices WHERE customer_id = $1 AND status = 'OPEN'
		ORDER BY due_date, created_at LIMIT 1`, out.CustomerID).Scan(&out.InvoiceID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, database.MapError(err)
	}
	return &out, nil
}

// purgeCheckout apaga um cadastro — conferindo de novo, com a conta travada,
// que segue sem pagamento e sem nada que ainda possa ser pago — e anota no
// pré-cliente. Devolve se apagou.
func (s *Service) purgeCheckout(ctx context.Context, id uuid.UUID, now time.Time) (bool, error) {
	removed := false
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var pending bool
		var created time.Time
		if err := tx.QueryRow(ctx, `SELECT checkout_pending(id), checkout_at FROM users WHERE id = $1 FOR UPDATE`, id).
			Scan(&pending, &created); err != nil {
			return database.MapError(err)
		}
		if !pending {
			return nil
		}
		boleto, pix := payLimits(now)
		var payable bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (`+payableCharges("$1")+`)`, id, boleto, pix).Scan(&payable); err != nil {
			return database.MapError(err)
		}
		if payable {
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
		if _, err := tx.Exec(ctx, `DELETE FROM users WHERE id = $1`, id); err != nil {
			return database.MapError(err)
		}
		removed = true
		return nil
	})
	return removed, err
}
