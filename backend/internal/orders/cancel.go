package orders

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/fulfillment"
)

// CancelResult é o pedido cancelado: as cobranças em aberto que caíram e o
// que já tinha sido pago (a central devolve ao cliente, por fora).
type CancelResult struct {
	CustomerID       uuid.UUID   `json:"customerId"`
	SubscriptionID   *uuid.UUID  `json:"subscriptionId"`
	VehicleName      string      `json:"vehicleName"`
	CanceledInvoices int         `json:"canceledInvoices"`
	PaidCents        int         `json:"paidCents"`
	PaidInvoices     []uuid.UUID `json:"paidInvoices"`
}

// CancelOrder cancela o pedido de um veículo, antes de o rastreador sair para
// entrega: as cobranças do pedido em aberto (a 1ª mensalidade e as parcelas,
// e as avulsas — rastreador e frete, instalação, proporcional — que nasceram
// na mesma transação da assinatura) são canceladas; as pagas ficam e somam o
// que devolver. A assinatura é encerrada sem as cobranças do encerramento (o
// rastreador nem foi instalado), o aparelho já vinculado volta ao estoque e o
// veículo sai da conta — com ele, o acompanhamento e o agendamento.
func (s *Service) CancelOrder(ctx context.Context, fulfillmentID uuid.UUID) (*CancelResult, error) {
	var out CancelResult
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var vehicleID uuid.UUID
		var tracker, shippingOrder string
		err := tx.QueryRow(ctx, `
			SELECT f.customer_id, f.vehicle_id, f.subscription_id, f.tracker_status, COALESCE(f.shipping_order_id, ''), v.name
			FROM fulfillments f JOIN vehicles v ON v.id = f.vehicle_id
			WHERE f.id = $1 FOR UPDATE OF f`, fulfillmentID).
			Scan(&out.CustomerID, &vehicleID, &out.SubscriptionID, &tracker, &shippingOrder, &out.VehicleName)
		if err != nil {
			return database.MapError(err)
		}
		switch {
		case tracker == fulfillment.TrackerShipped || tracker == fulfillment.TrackerInTransit || tracker == fulfillment.TrackerDelivered:
			return ValidationError{"o rastreador já saiu para entrega: o pedido não pode mais ser cancelado por aqui"}
		case shippingOrder != "":
			return ValidationError{"há uma etiqueta do Melhor Envios neste pedido: cancele a etiqueta antes de cancelar o pedido"}
		}

		if out.SubscriptionID != nil {
			// As cobranças do pedido: as da assinatura e as avulsas criadas
			// junto com ela (NOW() é o mesmo dentro da transação do pedido).
			rows, err := tx.Query(ctx, `
				SELECT i.id, i.status, i.amount_cents
				FROM invoices i JOIN subscriptions s ON s.id = $1
				WHERE i.subscription_id = s.id
				   OR (i.subscription_id IS NULL AND i.customer_id = s.customer_id AND i.created_at = s.created_at)
				FOR UPDATE OF i`, *out.SubscriptionID)
			if err != nil {
				return database.MapError(err)
			}
			var open []uuid.UUID
			for rows.Next() {
				var id uuid.UUID
				var status string
				var cents int
				if err := rows.Scan(&id, &status, &cents); err != nil {
					rows.Close()
					return err
				}
				switch status {
				case "OPEN":
					open = append(open, id)
				case "PAID":
					out.PaidCents += cents
					out.PaidInvoices = append(out.PaidInvoices, id)
				}
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return err
			}
			if len(open) > 0 {
				if _, err := tx.Exec(ctx, `UPDATE invoices SET status = 'CANCELED', updated_at = NOW() WHERE id = ANY($1)`, open); err != nil {
					return database.MapError(err)
				}
			}
			out.CanceledInvoices = len(open)
			if _, err := tx.Exec(ctx, `
				UPDATE subscriptions SET status = 'CANCELED', canceled_at = NOW(), updated_at = NOW()
				WHERE id = $1 AND status = 'ACTIVE'`, *out.SubscriptionID); err != nil {
				return database.MapError(err)
			}
		}

		// O aparelho já vinculado (em configuração) volta ao estoque; o
		// veículo sai, e com ele o acompanhamento e o agendamento.
		if _, err := tx.Exec(ctx, `UPDATE vehicles SET device_id = NULL WHERE id = $1`, vehicleID); err != nil {
			return database.MapError(err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM vehicles WHERE id = $1`, vehicleID); err != nil {
			return database.MapError(err)
		}
		return nil
	})
	if err != nil {
		var rule ValidationError
		if errors.As(err, &rule) {
			return nil, rule
		}
		return nil, err
	}
	return &out, nil
}
