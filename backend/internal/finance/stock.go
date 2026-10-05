package finance

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

const stockItemColumns = `id, name, kind, min_quantity, quantity, avg_cost_cents, active, created_at`

func scanStockItem(row database.Scanner) (*StockItem, error) {
	var x StockItem
	if err := row.Scan(&x.ID, &x.Name, &x.Kind, &x.MinQuantity, &x.Quantity, &x.AvgCostCents, &x.Active, &x.CreatedAt); err != nil {
		return nil, database.MapError(err)
	}
	x.ValueCents = int64(x.Quantity) * x.AvgCostCents
	x.Low = x.Active && x.MinQuantity > 0 && x.Quantity < x.MinQuantity
	return &x, nil
}

func (s *Service) StockItems(ctx context.Context) ([]*StockItem, error) {
	rows, err := s.db.Query(ctx, `SELECT `+stockItemColumns+` FROM stock_items ORDER BY active DESC, kind, lower(name)`)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	out := []*StockItem{}
	for rows.Next() {
		x, err := scanStockItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func (s *Service) SaveStockItem(ctx context.Context, id *uuid.UUID, in StockItemInput) (*StockItem, error) {
	in, err := in.Normalize()
	if err != nil {
		return nil, err
	}
	var item *StockItem
	if id == nil {
		item, err = scanStockItem(s.db.QueryRow(ctx, `
			INSERT INTO stock_items (name, kind, min_quantity, active) VALUES ($1, $2, $3, $4)
			RETURNING `+stockItemColumns, in.Name, in.Kind, in.MinQuantity, in.Active))
	} else {
		item, err = scanStockItem(s.db.QueryRow(ctx, `
			UPDATE stock_items SET name = $2, kind = $3, min_quantity = $4, active = $5, updated_at = NOW()
			WHERE id = $1 RETURNING `+stockItemColumns, *id, in.Name, in.Kind, in.MinQuantity, in.Active))
	}
	if errors.Is(err, database.ErrConflict) {
		return nil, invalid("Já existe um item com esse nome.")
	}
	return item, err
}

const movementColumns = `m.id, m.item_id, i.name, m.type, m.quantity, m.unit_cost_cents, m.occurred_on, m.supplier_id,
	COALESCE(sp.name, ''), m.notes, m.created_at`

const movementFrom = ` FROM stock_movements m
	JOIN stock_items i ON i.id = m.item_id
	LEFT JOIN suppliers sp ON sp.id = m.supplier_id`

func scanMovement(row database.Scanner) (*StockMovement, error) {
	var m StockMovement
	var day time.Time
	if err := row.Scan(&m.ID, &m.ItemID, &m.ItemName, &m.Type, &m.Quantity, &m.UnitCostCents, &day,
		&m.SupplierID, &m.SupplierName, &m.Notes, &m.CreatedAt); err != nil {
		return nil, database.MapError(err)
	}
	m.OccurredOn = billing.Date{Time: day}
	m.TotalCents = int64(m.Quantity) * m.UnitCostCents
	return &m, nil
}

// StockMovements lista os movimentos mais recentes (de um item, ou de todos).
func (s *Service) StockMovements(ctx context.Context, itemID *uuid.UUID, limit int) ([]*StockMovement, error) {
	if limit <= 0 || limit > maxListed {
		limit = 100
	}
	rows, err := s.db.Query(ctx, `SELECT `+movementColumns+movementFrom+`
		WHERE ($1::uuid IS NULL OR m.item_id = $1)
		ORDER BY m.occurred_on DESC, m.created_at DESC LIMIT $2`, itemID, limit)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	out := []*StockMovement{}
	for rows.Next() {
		m, err := scanMovement(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// MoveStock registra o movimento com o item travado: atualiza o saldo e,
// na entrada, o custo médio; nas saídas, grava o custo médio do momento (é
// ele que vai para o resultado do mês). Na entrada, pode lançar junto a
// conta a pagar da compra.
func (s *Service) MoveStock(ctx context.Context, in MovementInput, by *uuid.UUID) (*StockMovement, []*Entry, error) {
	in, err := in.Normalize(s.Today())
	if err != nil {
		return nil, nil, err
	}
	var payable *EntryInput
	if p := in.Payable; p != nil {
		normalized, err := EntryInput{
			Kind: KindPayable, CategoryID: p.CategoryID, SupplierID: in.SupplierID,
			AmountCents: int64(in.Quantity) * in.UnitCostCents, DueDate: p.DueDate, Installments: p.Installments,
			PaidOn: p.PaidOn, PaymentMethod: p.PaymentMethod, Notes: in.Notes,
			Description: "placeholder",
		}.Normalize()
		if err != nil {
			return nil, nil, err
		}
		if normalized.PaidOn != nil && s.Today().Before(*normalized.PaidOn) {
			return nil, nil, invalid("A data do pagamento não pode ser no futuro.")
		}
		payable = &normalized
	}

	var movementID uuid.UUID
	var entryIDs []uuid.UUID
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var name string
		var quantity int
		var avg int64
		if err := tx.QueryRow(ctx, `SELECT name, quantity, avg_cost_cents FROM stock_items WHERE id = $1 FOR UPDATE`,
			in.ItemID).Scan(&name, &quantity, &avg); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return invalid("Item não encontrado.")
			}
			return err
		}
		delta := in.signedQuantity()
		if quantity+delta < 0 {
			return invalid("Só há %d em estoque de %s.", quantity, name)
		}
		unitCost, newAvg := avg, avg
		if in.Type == MoveIn {
			unitCost, newAvg = in.UnitCostCents, AverageCost(quantity, avg, delta, in.UnitCostCents)
		}
		if in.SupplierID != nil {
			var ok bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM suppliers WHERE id = $1)`, *in.SupplierID).Scan(&ok); err != nil {
				return err
			}
			if !ok {
				return invalid("Fornecedor não encontrado.")
			}
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO stock_movements (item_id, type, quantity, unit_cost_cents, occurred_on, supplier_id, notes, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`,
			in.ItemID, in.Type, delta, unitCost, in.OccurredOn.Time, in.SupplierID, in.Notes, by).Scan(&movementID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE stock_items SET quantity = quantity + $2, avg_cost_cents = $3, updated_at = NOW() WHERE id = $1`,
			in.ItemID, delta, newAvg); err != nil {
			return err
		}
		if payable != nil {
			if err := checkRefs(ctx, tx, KindPayable, payable.CategoryID, in.SupplierID); err != nil {
				return err
			}
			payable.Description = fmt.Sprintf("Compra: %d × %s", in.Quantity, name)
			var err error
			entryIDs, err = insertEntries(ctx, tx, *payable, &movementID, by)
			return err
		}
		return nil
	})
	if err != nil {
		return nil, nil, mapErr(err)
	}
	movement, err := scanMovement(s.db.QueryRow(ctx, `SELECT `+movementColumns+movementFrom+` WHERE m.id = $1`, movementID))
	if err != nil {
		return nil, nil, err
	}
	entries := []*Entry{}
	if len(entryIDs) > 0 {
		if entries, err = s.entriesByID(ctx, entryIDs); err != nil {
			return nil, nil, err
		}
	}
	return movement, entries, nil
}

// InstalledTrackers: o que o sistema sabe dos rastreadores cadastrados —
// quantos estão num veículo e quantos ainda sem veículo —, para conferir com
// o estoque.
type InstalledTrackers struct {
	WithVehicle    int `json:"withVehicle"`
	WithoutVehicle int `json:"withoutVehicle"`
}

func (s *Service) InstalledTrackers(ctx context.Context) (*InstalledTrackers, error) {
	var out InstalledTrackers
	err := s.db.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE v.id IS NOT NULL), count(*) FILTER (WHERE v.id IS NULL)
		FROM devices d LEFT JOIN vehicles v ON v.device_id = d.id`).Scan(&out.WithVehicle, &out.WithoutVehicle)
	return &out, database.MapError(err)
}
