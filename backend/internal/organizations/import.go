package organizations

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/payments"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/vehicles"
)

// A importação de associados por planilha: uma linha por veículo (ou por
// associado, sem veículo). As linhas com o mesmo e-mail são o mesmo
// associado, com vários veículos. Tudo é conferido antes de cadastrar
// qualquer coisa: com um problema, nada entra e a tela mostra a linha.

// MaxImportRows é o máximo de linhas de uma planilha.
const MaxImportRows = 500

// ImportRow é uma linha da planilha.
type ImportRow struct {
	// Line é a linha na planilha (para as mensagens); sem ela, a posição
	// mais 2 (o cabeçalho é a 1).
	Line     int             `json:"line"`
	Name     string          `json:"name"`
	Email    string          `json:"email"`
	Phone    string          `json:"phone"`
	Document string          `json:"document"`
	Vehicle  *vehicles.Input `json:"vehicle"`
}

// ImportProblem é um problema numa linha (Row é a posição em rows, de 0).
type ImportProblem struct {
	Row     int    `json:"row"`
	Message string `json:"message"`
}

// ImportResult é o que entrou (ou entraria, na conferência).
type ImportResult struct {
	DryRun     bool `json:"dryRun"`
	Associates int  `json:"associates"`
	Vehicles   int  `json:"vehicles"`
}

// importGroup é um associado da planilha, com os veículos dele.
type importGroup struct {
	first    int
	input    AssociateInput
	vehicles []importVehicle
}

type importVehicle struct {
	row   int
	input vehicles.Input
}

// ImportAssociates confere a planilha e, se não for só a conferência
// (dryRun), cadastra os associados (com o convite do app) e os veículos (na
// fila de instalação). Com problemas, devolve a lista e não cadastra nada.
func (s *Service) ImportAssociates(ctx context.Context, orgID uuid.UUID, rows []ImportRow, dryRun bool) (*ImportResult, []ImportProblem, error) {
	if len(rows) == 0 || len(rows) > MaxImportRows {
		return nil, nil, &InputError{fmt.Sprintf("a planilha vai de 1 a %d linhas", MaxImportRows)}
	}
	if _, err := s.active(ctx, s.db, orgID); err != nil {
		return nil, nil, err
	}
	line := func(i int) int {
		if rows[i].Line > 0 {
			return rows[i].Line
		}
		return i + 2
	}
	var problems []ImportProblem
	problem := func(row int, format string, args ...any) {
		problems = append(problems, ImportProblem{Row: row, Message: fmt.Sprintf(format, args...)})
	}

	groups := map[string]*importGroup{}
	order := []string{}
	plates := map[string]int{}
	for i, row := range rows {
		in := AssociateInput{Name: row.Name, Email: row.Email, Phone: row.Phone, Document: strings.TrimSpace(row.Document)}
		if err := cleanAssociate(&in); err != nil {
			problem(i, "falta o nome do associado (até 120 caracteres)")
			continue
		}
		if !validEmail(in.Email) {
			problem(i, "e-mail inválido: é por ele que o associado recebe o acesso ao app")
			continue
		}
		if in.Document != "" {
			if !payments.ValidTaxID(in.Document) {
				problem(i, "CPF (ou CNPJ) inválido: confira os números")
				continue
			}
			in.Document = payments.Digits(in.Document)
		}
		g, seen := groups[in.Email]
		if !seen {
			g = &importGroup{first: i, input: in}
			groups[in.Email] = g
			order = append(order, in.Email)
		} else if !strings.EqualFold(g.input.Name, in.Name) {
			problem(i, "o e-mail %s aparece na linha %d com outro nome", in.Email, line(g.first))
			continue
		}
		if row.Vehicle == nil {
			continue
		}
		v := *row.Vehicle
		v.Name, v.Plate = strings.TrimSpace(v.Name), strings.ToUpper(strings.TrimSpace(v.Plate))
		if v.Name == "" && v.Plate == "" {
			continue
		}
		if v.Name == "" {
			v.Name = v.Plate
		}
		v.OwnerID, v.DeviceID, v.GroupID = nil, nil, nil
		if err := vehicles.Validate(v); err != nil {
			problem(i, "%s", err.Error())
			continue
		}
		if v.Plate != "" {
			if other, dup := plates[v.Plate]; dup {
				problem(i, "a placa %s já está na linha %d", v.Plate, line(other))
				continue
			}
			plates[v.Plate] = i
		}
		g.vehicles = append(g.vehicles, importVehicle{row: i, input: v})
	}

	// O que já existe na Farbo: e-mails e placas.
	for _, email := range order {
		if _, err := s.accounts.FindByEmail(ctx, email); err == nil {
			problem(groups[email].first, "o e-mail %s já tem cadastro na Farbo (para incluir essa pessoa, fale com a central)", email)
		} else if !errors.Is(err, database.ErrNotFound) {
			return nil, nil, err
		}
	}
	if len(plates) > 0 {
		list := make([]string, 0, len(plates))
		for p := range plates {
			list = append(list, p)
		}
		taken, err := s.db.Query(ctx, `SELECT plate FROM vehicles WHERE plate = ANY($1)`, list)
		if err != nil {
			return nil, nil, database.MapError(err)
		}
		for taken.Next() {
			var p string
			if err := taken.Scan(&p); err != nil {
				taken.Close()
				return nil, nil, database.MapError(err)
			}
			problem(plates[p], "a placa %s já está cadastrada na Farbo", p)
		}
		taken.Close()
		if err := taken.Err(); err != nil {
			return nil, nil, database.MapError(err)
		}
	}

	result := &ImportResult{DryRun: dryRun, Associates: len(order)}
	for _, email := range order {
		result.Vehicles += len(groups[email].vehicles)
	}
	if len(problems) > 0 {
		return nil, sortProblems(problems), nil
	}
	if dryRun {
		return result, nil, nil
	}

	done := &ImportResult{}
	for _, email := range order {
		g := groups[email]
		a, err := s.CreateAssociate(ctx, orgID, g.input)
		if err != nil {
			return done, nil, stopped(line(g.first), err, done)
		}
		done.Associates++
		for _, v := range g.vehicles {
			if _, err := s.RegisterVehicle(ctx, orgID, a.ID, v.input); err != nil {
				return done, nil, stopped(line(v.row), err, done)
			}
			done.Vehicles++
		}
	}
	return done, nil, nil
}

// stopped é a importação interrompida no meio (algo mudou entre a
// conferência e o cadastro): diz a linha e o que já entrou.
func stopped(line int, err error, done *ImportResult) error {
	return &InputError{fmt.Sprintf("a importação parou na linha %d (%s); entraram %d associados e %d veículos antes dela",
		line, err.Error(), done.Associates, done.Vehicles)}
}

// sortProblems põe os problemas na ordem das linhas.
func sortProblems(list []ImportProblem) []ImportProblem {
	slices.SortStableFunc(list, func(a, b ImportProblem) int { return a.Row - b.Row })
	return list
}
