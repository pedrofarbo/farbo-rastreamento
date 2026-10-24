package organizations

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// A fatura da organização é uma só por mês, no dia de vencimento dela, com o
// extrato: a mensalidade de cada veículo com rastreador (o mês que começa no
// vencimento), o proporcional dos dias de quem passou a contar no meio do
// mês, a instalação, o equipamento (na venda) e a desinstalação.

// Os itens do extrato.
const (
	ItemMonthly   = "monthly"
	ItemProrata   = "prorata"
	ItemInstall   = "install"
	ItemEquipment = "equipment"
	ItemUninstall = "uninstall"
)

// Item é uma linha do extrato da fatura.
type Item struct {
	Kind        string     `json:"kind"`
	VehicleID   *uuid.UUID `json:"vehicleId"`
	Name        string     `json:"name"`
	Plate       string     `json:"plate"`
	OwnerName   string     `json:"ownerName"`
	AmountCents int        `json:"amountCents"`
	// Days: os dias do proporcional.
	Days int `json:"days,omitempty"`
}

// Invoice é a fatura da organização, com o extrato (vazio na fatura avulsa
// lançada pela central).
type Invoice struct {
	*billing.Invoice
	// Vehicles: os veículos na mensalidade do mês.
	Vehicles   int    `json:"vehicles"`
	PriceCents int    `json:"priceCents"`
	Items      []Item `json:"items"`
}

// Invoices são as faturas da organização, com o extrato de cada mês.
func (s *Service) Invoices(ctx context.Context, orgID uuid.UUID) ([]*Invoice, error) {
	list, err := s.billing.ListInvoices(ctx, orgID)
	if err != nil {
		return nil, err
	}
	type ledger struct {
		vehicles, price int
		items           []Item
	}
	byInvoice := map[uuid.UUID]ledger{}
	rows, err := s.db.Query(ctx, `SELECT invoice_id, vehicles, price_cents, items FROM organization_invoices WHERE organization_id = $1`, orgID)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var l ledger
		if err := rows.Scan(&id, &l.vehicles, &l.price, &l.items); err != nil {
			return nil, err
		}
		byInvoice[id] = l
	}
	if err := rows.Err(); err != nil {
		return nil, database.MapError(err)
	}
	out := make([]*Invoice, 0, len(list))
	for _, inv := range list {
		l := byInvoice[inv.ID]
		if l.items == nil {
			l.items = []Item{}
		}
		out = append(out, &Invoice{Invoice: inv, Vehicles: l.vehicles, PriceCents: l.price, Items: l.items})
	}
	return out, nil
}

// SyncVehicles atualiza quem conta na cobrança de cada organização: o
// veículo de associado ativo com rastreador passa a contar no dia em que
// aparece; o que perde o rastreador (desinstalação), o associado ou a
// organização deixa de contar. A troca do rastreador no mesmo veículo não é
// desinstalação.
func (s *Service) SyncVehicles(ctx context.Context) error {
	today := s.billing.Today().Time
	return database.MapError(pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		// Troca do rastreador e nomes atualizados (o extrato mostra os de agora).
		if _, err := tx.Exec(ctx, `
			UPDATE organization_vehicles ov SET device_id = v.device_id, vehicle_name = v.name,
				plate = COALESCE(v.plate, ''), owner_name = a.name
			FROM vehicles v JOIN users a ON a.id = v.owner_id
			WHERE ov.removed_on IS NULL AND v.id = ov.vehicle_id AND a.organization_id = ov.organization_id
				AND a.active AND v.device_id IS NOT NULL`); err != nil {
			return err
		}
		// Quem deixou de contar; desinstalado se o veículo ficou sem rastreador.
		if _, err := tx.Exec(ctx, `
			UPDATE organization_vehicles ov SET removed_on = $1,
				uninstalled = NOT EXISTS (SELECT 1 FROM vehicles v WHERE v.id = ov.vehicle_id AND v.device_id IS NOT NULL)
			WHERE ov.removed_on IS NULL AND NOT EXISTS (
				SELECT 1 FROM vehicles v JOIN users a ON a.id = v.owner_id
				WHERE v.id = ov.vehicle_id AND a.organization_id = ov.organization_id AND a.active AND v.device_id IS NOT NULL)`,
			today); err != nil {
			return err
		}
		// O rastreador retirado depois de o veículo sair da conta (o associado
		// desligado, a retirada agendada pela central): a última passagem do
		// veículo pela organização cobra a desinstalação.
		if _, err := tx.Exec(ctx, `
			UPDATE organization_vehicles ov SET uninstalled = TRUE
			WHERE ov.removed_on IS NOT NULL AND NOT ov.uninstalled
				AND NOT EXISTS (SELECT 1 FROM organization_vehicles later
					WHERE later.vehicle_id = ov.vehicle_id AND later.created_at > ov.created_at)
				AND EXISTS (SELECT 1 FROM installation_appointments a
					WHERE a.vehicle_id = ov.vehicle_id AND a.kind = 'uninstall' AND a.status = 'done'
						AND a.completed_at >= ov.created_at)`); err != nil {
			return err
		}
		// Quem passou a contar. Quem volta sem ter tido o rastreador tirado
		// (o associado foi religado) não paga a instalação de novo.
		_, err := tx.Exec(ctx, `
			INSERT INTO organization_vehicles (organization_id, vehicle_id, device_id, vehicle_name, plate, owner_name,
				installed_on, install_billed, equipment_billed)
			SELECT a.organization_id, v.id, v.device_id, v.name, COALESCE(v.plate, ''), a.name, $1, paused.billed, paused.billed
			FROM vehicles v
			JOIN users a ON a.id = v.owner_id AND a.active AND a.organization_id IS NOT NULL
			CROSS JOIN LATERAL (SELECT EXISTS (
				SELECT 1 FROM organization_vehicles p
				WHERE p.vehicle_id = v.id AND p.organization_id = a.organization_id AND NOT p.uninstalled AND p.install_billed
			) AS billed) paused
			WHERE v.device_id IS NOT NULL
				AND NOT EXISTS (SELECT 1 FROM organization_vehicles ov WHERE ov.vehicle_id = v.id AND ov.removed_on IS NULL)`,
			today)
		return err
	}))
}

// ErrNothingToBill: nada a cobrar no vencimento (nenhum veículo instalado e
// nenhuma taxa).
var ErrNothingToBill = &InputError{"não há nada a cobrar: nenhum veículo de associado com rastreador instalado e nenhuma taxa pendente"}

// errAlreadyBilled: a fatura deste vencimento já existe.
var errAlreadyBilled = errors.New("fatura do vencimento já gerada")

// terms são os preços da organização na hora da fatura.
type terms struct {
	name   string
	price  int
	active bool
	Terms
}

// statement monta o extrato do vencimento due e, com mark, marca o que
// entrou como cobrado. Roda dentro da transação da fatura (ou da simulação).
func statement(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, t terms, due billing.Date, mark bool) ([]Item, int, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, vehicle_id, vehicle_name, plate, owner_name, installed_on, removed_on, uninstalled,
			prorata_billed, install_billed, equipment_billed, uninstall_billed
		FROM organization_vehicles
		WHERE organization_id = $1 AND (removed_on IS NULL
			OR NOT prorata_billed OR NOT install_billed OR (uninstalled AND NOT uninstall_billed))
		ORDER BY owner_name, vehicle_name, installed_on
		FOR UPDATE`, orgID)
	if err != nil {
		return nil, 0, err
	}
	type record struct {
		id                                       uuid.UUID
		vehicleID                                *uuid.UUID
		name, plate, owner                       string
		installed                                time.Time
		removed                                  *time.Time
		uninstalled, prorata, install, equipment bool
		uninstallBilled                          bool
	}
	var list []record
	for rows.Next() {
		var r record
		if err := rows.Scan(&r.id, &r.vehicleID, &r.name, &r.plate, &r.owner, &r.installed, &r.removed, &r.uninstalled,
			&r.prorata, &r.install, &r.equipment, &r.uninstallBilled); err != nil {
			rows.Close()
			return nil, 0, err
		}
		list = append(list, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	items := []Item{}
	monthly := 0
	add := func(r record, kind string, cents, days int) {
		if cents > 0 {
			items = append(items, Item{Kind: kind, VehicleID: r.vehicleID, Name: r.name, Plate: r.plate, OwnerName: r.owner,
				AmountCents: cents, Days: days})
		}
	}
	for _, r := range list {
		current := r.removed == nil
		if current {
			add(r, ItemMonthly, t.price, 0)
			monthly++
		}
		// O proporcional: de quando passou a contar até o vencimento (ou até
		// sair, se saiu antes de entrar numa fatura).
		if !r.prorata {
			until := due.Time
			if r.removed != nil && r.removed.Before(until) {
				until = *r.removed
			}
			days := min(max(int(math.Round(until.Sub(r.installed).Hours()/24)), 0), 45)
			add(r, ItemProrata, prorata(t.price, days), days)
		}
		if !r.install {
			add(r, ItemInstall, t.InstallFeeCents, 0)
		}
		if !r.equipment && t.EquipmentMode == EquipmentSale {
			add(r, ItemEquipment, t.EquipmentCents, 0)
		}
		if r.uninstalled && !r.uninstallBilled {
			add(r, ItemUninstall, t.UninstallFeeCents, 0)
		}
		if mark {
			if _, err := tx.Exec(ctx, `
				UPDATE organization_vehicles SET prorata_billed = TRUE, install_billed = TRUE, equipment_billed = TRUE,
					uninstall_billed = uninstall_billed OR uninstalled
				WHERE id = $1`, r.id); err != nil {
				return nil, 0, err
			}
		}
	}
	return items, monthly, nil
}

// prorata é a mensalidade pelos dias (mês comercial de 30 dias).
func prorata(price, days int) int {
	if days <= 0 || price <= 0 {
		return 0
	}
	return int(math.Round(float64(price) * float64(days) / 30))
}

func total(items []Item) int {
	sum := 0
	for _, it := range items {
		sum += it.AmountCents
	}
	return sum
}

func lockTerms(ctx context.Context, tx pgx.Tx, orgID uuid.UUID) (terms, error) {
	var t terms
	err := tx.QueryRow(ctx, `
		SELECT u.name, o.price_cents, u.active, o.equipment_mode, o.equipment_cents, o.install_fee_cents, o.uninstall_fee_cents
		FROM organizations o JOIN users u ON u.id = o.id
		WHERE o.id = $1 FOR UPDATE OF o`, orgID).Scan(&t.name, &t.price, &t.active,
		&t.EquipmentMode, &t.EquipmentCents, &t.InstallFeeCents, &t.UninstallFeeCents)
	return t, err
}

// generate lança a fatura de um vencimento, com o extrato.
func (s *Service) generate(ctx context.Context, orgID uuid.UUID, due billing.Date) (*Invoice, error) {
	var out *Invoice
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		t, err := lockTerms(ctx, tx, orgID)
		if err != nil {
			return err
		}
		if !t.active {
			return &InputError{"a organização está desativada"}
		}
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM organization_invoices WHERE organization_id = $1 AND due_date = $2)`,
			orgID, due.Time).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return errAlreadyBilled
		}
		items, monthly, err := statement(ctx, tx, orgID, t, due, true)
		if err != nil {
			return err
		}
		amount := total(items)
		if amount <= 0 {
			return ErrNothingToBill
		}
		description := fmt.Sprintf("Rastreamento — %s/%d", billing.MonthName(due.Month()), due.Year())
		if monthly > 0 {
			plural := "veículo"
			if monthly > 1 {
				plural = "veículos"
			}
			description += fmt.Sprintf(" · %d %s", monthly, plural)
		}
		// Sem o teto da fatura avulsa: a de uma organização grande passa dele.
		inv, err := billing.InsertInvoice(ctx, tx, &billing.Invoice{
			CustomerID: orgID, Description: description, AmountCents: amount, DueDate: due,
		})
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO organization_invoices (organization_id, due_date, invoice_id, vehicles, price_cents, items)
			VALUES ($1, $2, $3, $4, $5, $6)`, orgID, due.Time, inv.ID, monthly, t.price, items); err != nil {
			return err
		}
		out = &Invoice{Invoice: s.billing.Decorate(inv), Vehicles: monthly, PriceCents: t.price, Items: items}
		return nil
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return out, nil
}

// Estimate é a próxima fatura, pelo que está instalado hoje.
type Estimate struct {
	DueDate     billing.Date `json:"dueDate"`
	Vehicles    int          `json:"vehicles"`
	PriceCents  int          `json:"priceCents"`
	AmountCents int          `json:"amountCents"`
	Items       []Item       `json:"items"`
}

// errDryRun desfaz a simulação da próxima fatura.
var errDryRun = errors.New("simulação")

// NextEstimate simula a fatura do próximo vencimento ainda sem fatura, com o
// que conta hoje (nada é gravado).
func (s *Service) NextEstimate(ctx context.Context, orgID uuid.UUID) (*Estimate, error) {
	o, err := s.Get(ctx, orgID)
	if err != nil {
		return nil, err
	}
	if err := s.SyncVehicles(ctx); err != nil {
		return nil, err
	}
	due, err := s.nextOpenDue(ctx, o)
	if err != nil {
		return nil, err
	}
	out := &Estimate{DueDate: due, PriceCents: o.PriceCents}
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		t, err := lockTerms(ctx, tx, orgID)
		if err != nil {
			return err
		}
		if out.Items, out.Vehicles, err = statement(ctx, tx, orgID, t, due, false); err != nil {
			return err
		}
		out.AmountCents = total(out.Items)
		return errDryRun
	})
	if err != nil && !errors.Is(err, errDryRun) {
		return nil, database.MapError(err)
	}
	return out, nil
}

// nextOpenDue é o primeiro vencimento, a partir de hoje, ainda sem fatura.
func (s *Service) nextOpenDue(ctx context.Context, o *Organization) (billing.Date, error) {
	today := s.billing.Today()
	due := billing.NewDate(today.Year(), today.Month(), o.DueDay)
	if due.Before(today) {
		due = billing.NewDate(today.Year(), today.Month()+1, o.DueDay)
	}
	for i := 0; i < 24; i++ {
		var exists bool
		if err := s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM organization_invoices WHERE organization_id = $1 AND due_date = $2)`,
			o.ID, due.Time).Scan(&exists); err != nil {
			return billing.Date{}, database.MapError(err)
		}
		if !exists {
			break
		}
		due = billing.NewDate(due.Year(), due.Month()+1, o.DueDay)
	}
	return due, nil
}

// GenerateNow lança já a fatura do próximo vencimento sem fatura (a central,
// sem esperar a antecedência de sempre).
func (s *Service) GenerateNow(ctx context.Context, orgID uuid.UUID) (*Invoice, error) {
	o, err := s.Get(ctx, orgID)
	if err != nil {
		return nil, err
	}
	if err := s.SyncVehicles(ctx); err != nil {
		return nil, err
	}
	due, err := s.nextOpenDue(ctx, o)
	if err != nil {
		return nil, err
	}
	inv, err := s.generate(ctx, orgID, due)
	if errors.Is(err, errAlreadyBilled) {
		return nil, &InputError{"a fatura deste vencimento já foi gerada"}
	}
	return inv, err
}

// GenerateInvoices lança as faturas do mês das organizações, com a mesma
// antecedência das mensalidades dos clientes. O vencimento de antes da
// organização existir não é cobrado; sem nada a cobrar, não há fatura.
func (s *Service) GenerateInvoices(ctx context.Context) {
	if err := s.SyncVehicles(ctx); err != nil {
		s.log.Error("falha ao atualizar os veículos das organizações", "err", err)
		return
	}
	rows, err := s.db.Query(ctx, `
		SELECT o.id, o.due_day, o.created_at FROM organizations o JOIN users u ON u.id = o.id WHERE u.active`)
	if err != nil {
		s.log.Error("falha ao listar as organizações para as faturas", "err", err)
		return
	}
	type org struct {
		id      uuid.UUID
		dueDay  int
		created time.Time
	}
	var list []org
	for rows.Next() {
		var o org
		if err := rows.Scan(&o.id, &o.dueDay, &o.created); err != nil {
			rows.Close()
			s.log.Error("falha ao ler as organizações para as faturas", "err", err)
			return
		}
		list = append(list, o)
	}
	rows.Close()

	today := s.billing.Today()
	horizon := today.AddDays(s.billing.LeadDays())
	created := 0
	for _, o := range list {
		since := s.billing.DateOf(o.created)
		for _, due := range []billing.Date{
			billing.NewDate(today.Year(), today.Month(), o.dueDay),
			billing.NewDate(today.Year(), today.Month()+1, o.dueDay),
		} {
			if horizon.Before(due) || due.Before(since) {
				continue
			}
			_, err := s.generate(ctx, o.id, due)
			var input *InputError
			switch {
			case err == nil:
				created++
			case errors.Is(err, errAlreadyBilled), errors.As(err, &input):
			default:
				s.log.Error("falha ao gerar a fatura da organização", "organization", o.id, "due", due.String(), "err", err)
			}
		}
	}
	if created > 0 {
		s.log.Info("faturas das organizações geradas", "count", created)
	}
}
