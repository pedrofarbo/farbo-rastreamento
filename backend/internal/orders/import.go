package orders

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/addresses"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/fulfillment"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/vehicles"
)

// MaxImportRows é o tamanho máximo de uma planilha importada.
const MaxImportRows = 1000

// ImportRow é uma linha da planilha: o veículo, o nome do grupo (criado se a
// conta ainda não tiver) e, se o rastreador já estiver com a central, o IMEI
// dele — o veículo já sai com o aparelho vinculado.
type ImportRow struct {
	Vehicle vehicles.Input `json:"vehicle"`
	Group   string         `json:"group"`
	IMEI    string         `json:"imei"`
}

// Import é a frota de uma vez, pela central: as linhas da planilha e, para
// todas, o mesmo plano e o mesmo valor de equipamento. O equipamento sai numa
// fatura só, com o valor somado.
type Import struct {
	Rows []ImportRow
	// EquipmentCents é o valor de cada aparelho; zero não cobra.
	EquipmentCents int
	// SetupDueDate é o vencimento da fatura do equipamento; nulo usa o prazo
	// do catálogo.
	SetupDueDate *billing.Date
	Plan         billing.SubscriptionInput
	// DryRun só confere a planilha: nada é gravado.
	DryRun bool
}

// ImportProblem é uma linha recusada. Line é a posição na lista (1 é a
// primeira linha de veículo); zero vale para a planilha inteira.
type ImportProblem struct {
	Line    int    `json:"line"`
	Message string `json:"message"`
}

// ImportProblems recusa a importação inteira: nada é gravado.
type ImportProblems []ImportProblem

func (p ImportProblems) Error() string {
	if len(p) == 1 {
		return p[0].Message
	}
	return fmt.Sprintf("%d problemas na planilha", len(p))
}

// ImportResult é o que a importação criou (ou criaria, na conferência).
type ImportResult struct {
	Vehicles int `json:"vehicles"`
	// Installed são os que já saem com o rastreador vinculado; os outros
	// entram no acompanhamento do envio.
	Installed int `json:"installed"`
	// NewGroups são os grupos que a planilha criou na conta.
	NewGroups []string `json:"newGroups"`
	// EquipmentCents é o total da fatura do equipamento.
	EquipmentCents int              `json:"equipmentCents"`
	SetupInvoice   *billing.Invoice `json:"setupInvoice"`
	DryRun         bool             `json:"dryRun"`
}

// importPlan é a planilha conferida, pronta para gravar.
type importPlan struct {
	devices   []*uuid.UUID
	groups    map[string]*uuid.UUID // nome em minúsculas → grupo (nulo: criar)
	newGroups []string
}

// ImportFleet cadastra a frota da planilha numa transação só: cada linha
// vira um veículo com a assinatura dele, e o equipamento de todas, uma
// fatura. Qualquer linha com problema recusa tudo (ImportProblems), com o
// motivo de cada uma.
func (s *Service) ImportFleet(ctx context.Context, customerID uuid.UUID, in Import) (*ImportResult, error) {
	if in.EquipmentCents < 0 {
		return nil, ValidationError{"o valor do equipamento não pode ser negativo"}
	}
	template, err := s.billing.NewSubscription(customerID, in.Plan)
	if err != nil {
		return nil, err
	}
	plan, err := s.checkImport(ctx, customerID, in.Rows)
	if err != nil {
		return nil, err
	}

	result := &ImportResult{Vehicles: len(in.Rows), NewGroups: plan.newGroups, DryRun: in.DryRun,
		EquipmentCents: in.EquipmentCents * len(in.Rows)}
	for _, d := range plan.devices {
		if d != nil {
			result.Installed++
		}
	}
	if in.DryRun {
		return result, nil
	}

	due := s.billing.Today().AddDays(s.catalog.SetupDueDays)
	if in.SetupDueDate != nil {
		due = *in.SetupDueDate
	}
	var setup *billing.Invoice
	if result.EquipmentCents > 0 {
		setup, err = s.billing.NewInvoice(customerID, billing.InvoiceInput{
			Description: fmt.Sprintf("%s × %d", s.catalog.EquipmentName, len(in.Rows)),
			AmountCents: result.EquipmentCents,
			DueDate:     due,
		})
		if err != nil {
			return nil, err
		}
	}

	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if err := lockCustomer(ctx, tx, customerID); err != nil {
			return err
		}
		// A conta vence num dia só: a frota inteira entra no dia dela.
		if err := s.billing.AlignDueDay(ctx, tx, template, in.Plan.FirstDueDate != nil); err != nil {
			return err
		}
		for _, name := range plan.newGroups {
			var id uuid.UUID
			if err := tx.QueryRow(ctx, `INSERT INTO vehicle_groups (owner_id, name) VALUES ($1, $2) RETURNING id`,
				customerID, name).Scan(&id); err != nil {
				return err
			}
			plan.groups[strings.ToLower(name)] = &id
		}
		address, err := addresses.GetWith(ctx, tx, customerID)
		if err != nil {
			return err
		}
		for i, row := range in.Rows {
			v := row.Vehicle
			v.OwnerID, v.DeviceID = &customerID, plan.devices[i]
			v.GroupID = nil
			if g := strings.TrimSpace(row.Group); g != "" {
				id := plan.groups[strings.ToLower(g)].String()
				v.GroupID = &id
			}
			vehicle, err := vehicles.InsertWith(ctx, tx, v)
			if err != nil {
				return fmt.Errorf("linha %d: %w", i+1, err)
			}
			sub := *template
			sub.VehicleID = &vehicle.ID
			if vehicle.DeviceID == nil {
				sub.DeliveryAddress = address
			}
			created, err := billing.InsertSubscription(ctx, tx, &sub)
			if err != nil {
				return fmt.Errorf("linha %d: %w", i+1, err)
			}
			// Sem aparelho vinculado, o rastreador entra no acompanhamento do
			// envio, como no pedido de um veículo.
			if vehicle.DeviceID == nil {
				if _, err := fulfillment.Create(ctx, tx, customerID, vehicle.ID, &created.ID); err != nil {
					return err
				}
			}
		}
		if setup != nil {
			if result.SetupInvoice, err = billing.InsertInvoice(ctx, tx, setup); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		if errors.Is(database.MapError(err), database.ErrConflict) {
			// Alguém cadastrou a mesma placa (ou o mesmo grupo) no meio do caminho.
			return nil, ImportProblems{{Message: "uma placa, um rastreador ou um grupo da planilha acabou de ser usado em outro cadastro; confira de novo"}}
		}
		return nil, database.MapError(err)
	}
	if result.SetupInvoice != nil {
		s.billing.Decorate(result.SetupInvoice)
	}
	s.billing.GenerateInvoices(ctx)
	s.log.Info("frota importada", "customer", customerID, "vehicles", result.Vehicles, "installed", result.Installed,
		"new_groups", len(result.NewGroups), "equipment_cents", result.EquipmentCents)
	return result, nil
}

// checkImport confere cada linha — os dados do veículo, placas e IMEIs
// repetidos na planilha ou já usados, os grupos — e junta todos os problemas
// de uma vez.
func (s *Service) checkImport(ctx context.Context, customerID uuid.UUID, rows []ImportRow) (*importPlan, error) {
	if len(rows) == 0 {
		return nil, ImportProblems{{Message: "a planilha não tem nenhum veículo"}}
	}
	if len(rows) > MaxImportRows {
		return nil, ImportProblems{{Message: fmt.Sprintf("a planilha tem %d veículos; o máximo é %d por vez", len(rows), MaxImportRows)}}
	}

	var problems ImportProblems
	add := func(line int, format string, args ...any) {
		problems = append(problems, ImportProblem{Line: line, Message: fmt.Sprintf(format, args...)})
	}
	plates := map[string]int{}
	imeis := map[string]int{}
	var plateList, imeiList []string
	for i := range rows {
		line, row := i+1, &rows[i]
		// O grupo vem pelo nome; o id é resolvido aqui.
		row.Vehicle.GroupID = nil
		row.IMEI = strings.TrimSpace(row.IMEI)
		row.Group = strings.TrimSpace(row.Group)
		if err := vehicles.Validate(row.Vehicle); err != nil {
			add(line, "%s", err.Error())
		}
		if utf8.RuneCountInString(row.Group) > vehicles.MaxGroupName {
			add(line, "o nome do grupo vai até %d caracteres", vehicles.MaxGroupName)
		}
		if plate := strings.ToUpper(strings.TrimSpace(row.Vehicle.Plate)); plate != "" {
			if first, ok := plates[plate]; ok {
				add(line, "a placa %s se repete (linha %d)", plate, first)
			} else {
				plates[plate] = line
				plateList = append(plateList, plate)
			}
		}
		if row.IMEI != "" {
			if first, ok := imeis[row.IMEI]; ok {
				add(line, "o IMEI %s se repete (linha %d)", row.IMEI, first)
			} else {
				imeis[row.IMEI] = line
				imeiList = append(imeiList, row.IMEI)
			}
		}
	}

	// Placas que já estão em outro veículo.
	if len(plateList) > 0 {
		taken, err := s.strings(ctx, `SELECT plate FROM vehicles WHERE plate = ANY($1)`, plateList)
		if err != nil {
			return nil, err
		}
		for _, plate := range taken {
			add(plates[plate], "a placa %s já está cadastrada", plate)
		}
	}

	// Os rastreadores: precisam estar cadastrados e livres.
	plan := &importPlan{devices: make([]*uuid.UUID, len(rows)), groups: map[string]*uuid.UUID{}}
	if len(imeiList) > 0 {
		found := map[string]*uuid.UUID{}
		dbRows, err := s.db.Query(ctx, `
			SELECT d.imei, d.id, EXISTS (SELECT 1 FROM vehicles v WHERE v.device_id = d.id)
			FROM devices d WHERE d.imei = ANY($1)`, imeiList)
		if err != nil {
			return nil, database.MapError(err)
		}
		for dbRows.Next() {
			var imei string
			var id uuid.UUID
			var linked bool
			if err := dbRows.Scan(&imei, &id, &linked); err != nil {
				dbRows.Close()
				return nil, database.MapError(err)
			}
			if linked {
				add(imeis[imei], "o rastreador %s já está em outro veículo", imei)
			}
			found[imei] = &id
		}
		dbRows.Close()
		if err := dbRows.Err(); err != nil {
			return nil, database.MapError(err)
		}
		for i, row := range rows {
			if row.IMEI == "" {
				continue
			}
			if id, ok := found[row.IMEI]; ok {
				plan.devices[i] = id
			} else if imeis[row.IMEI] == i+1 {
				add(i+1, "o rastreador %s não está cadastrado (cadastre-o em Rastreadores)", row.IMEI)
			}
		}
	}

	// Os grupos: os que a conta já tem e os que a planilha cria.
	existing, err := s.vehicles.Groups(ctx, customerID)
	if err != nil {
		return nil, err
	}
	for _, g := range existing {
		id := g.ID
		plan.groups[strings.ToLower(g.Name)] = &id
	}
	for _, row := range rows {
		key := strings.ToLower(row.Group)
		if key == "" {
			continue
		}
		if _, ok := plan.groups[key]; !ok {
			plan.groups[key] = nil
			plan.newGroups = append(plan.newGroups, row.Group)
		}
	}
	if len(existing)+len(plan.newGroups) > vehicles.MaxGroups {
		problems = append(problems, ImportProblem{Message: fmt.Sprintf(
			"a planilha cria %d grupos e a conta já tem %d: o máximo é %d", len(plan.newGroups), len(existing), vehicles.MaxGroups)})
	}

	if len(problems) > 0 {
		sortProblems(problems)
		return nil, problems
	}
	return plan, nil
}

func (s *Service) strings(ctx context.Context, query string, args ...any) ([]string, error) {
	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, database.MapError(err)
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// sortProblems põe os problemas na ordem das linhas (os da planilha inteira
// primeiro), mantendo a ordem de cada linha.
func sortProblems(p ImportProblems) {
	sort.SliceStable(p, func(i, j int) bool { return p[i].Line < p[j].Line })
}
