package finance

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// CustomerPaymentsLimit é quantos pagamentos a lista traz por vez (os mais
// recentes); os totais contam todos. Numa frota, um mês são centenas.
const CustomerPaymentsLimit = 500

// paidMethods são as formas de pagamento das faturas; a baixa da central (ou
// a antiga, sem forma) é MANUAL.
var paidMethods = map[string]bool{"PIX": true, "BOLETO": true, "CARD": true, "MANUAL": true}

// CustomerPayment é uma fatura de cliente paga: o que entrou no caixa.
type CustomerPayment struct {
	InvoiceID    uuid.UUID    `json:"invoiceId"`
	CustomerID   uuid.UUID    `json:"customerId"`
	CustomerName string       `json:"customerName"`
	Description  string       `json:"description"`
	AmountCents  int64        `json:"amountCents"`
	DueDate      billing.Date `json:"dueDate"`
	PaidAt       time.Time    `json:"paidAt"`
	// Method: PIX, BOLETO, CARD ou MANUAL.
	Method string `json:"method"`
}

// MethodSum é quanto entrou por uma forma de pagamento.
type MethodSum struct {
	Method string `json:"method"`
	Count  int    `json:"count"`
	Cents  int64  `json:"cents"`
}

// CustomerPayments são os pagamentos dos clientes de um mês, pelo dia em
// que entraram no caixa (em Brasília): o total é o mesmo das faturas dos
// clientes no fluxo de caixa e no resultado.
type CustomerPayments struct {
	Month    string            `json:"month"`
	Count    int               `json:"count"`
	Cents    int64             `json:"cents"`
	ByMethod []MethodSum       `json:"byMethod"`
	Payments []CustomerPayment `json:"payments"`
	// Truncated: há mais pagamentos do que a lista traz (busque para achar).
	Truncated bool `json:"truncated"`
}

// CustomerPaymentsFilter: o mês (AAAA-MM; vazio, o corrente), a busca (pelo
// cliente ou pela descrição) e a forma de pagamento.
type CustomerPaymentsFilter struct {
	Month  string
	Search string
	Method string
}

// customerPaymentsFrom é a base da lista: as faturas pagas no mês, no dia do
// caixa (o mesmo de paidFlows), com o cliente. $1 fuso, $2 e $3 o mês, $4 a
// busca (ILIKE), $5 a forma (vazia: todas).
const customerPaymentsFrom = `
	FROM invoices i LEFT JOIN users u ON u.id = i.customer_id
	WHERE i.status = 'PAID'
		AND (COALESCE(i.paid_at, i.updated_at) AT TIME ZONE $1)::date BETWEEN $2 AND $3
		AND ($4 = '' OR i.description ILIKE $4 OR u.name ILIKE $4 OR u.email ILIKE $4)
		AND ($5 = '' OR COALESCE(NULLIF(i.paid_via, ''), 'MANUAL') = $5)`

func (s *Service) CustomerPayments(ctx context.Context, f CustomerPaymentsFilter) (*CustomerPayments, error) {
	first := billing.NewDate(s.Today().Year(), s.Today().Month(), 1)
	if f.Month != "" {
		parsed, err := time.Parse("2006-01", f.Month)
		if err != nil {
			return nil, ValidationError{"mês inválido: use AAAA-MM"}
		}
		first = billing.NewDate(parsed.Year(), parsed.Month(), 1)
	}
	method := strings.ToUpper(strings.TrimSpace(f.Method))
	if method != "" && !paidMethods[method] {
		return nil, ValidationError{"forma de pagamento inválida"}
	}
	search := ""
	if q := strings.TrimSpace(f.Search); q != "" {
		search = "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(q) + "%"
	}
	last := AddMonths(first, 1).AddDays(-1)
	args := []any{tz, first.Time, last.Time, search, method}
	out := &CustomerPayments{Month: Month(first), ByMethod: []MethodSum{}, Payments: []CustomerPayment{}}

	rows, err := s.db.Query(ctx, `
		SELECT COALESCE(NULLIF(i.paid_via, ''), 'MANUAL'), count(*), COALESCE(sum(i.amount_cents), 0)::bigint`+
		customerPaymentsFrom+` GROUP BY 1 ORDER BY 3 DESC`, args...)
	if err != nil {
		return nil, database.MapError(err)
	}
	for rows.Next() {
		var m MethodSum
		if err := rows.Scan(&m.Method, &m.Count, &m.Cents); err != nil {
			rows.Close()
			return nil, err
		}
		out.ByMethod = append(out.ByMethod, m)
		out.Count += m.Count
		out.Cents += m.Cents
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, database.MapError(err)
	}

	rows, err = s.db.Query(ctx, `
		SELECT i.id, i.customer_id, COALESCE(u.name, ''), i.description, i.amount_cents, i.due_date,
			COALESCE(i.paid_at, i.updated_at), COALESCE(NULLIF(i.paid_via, ''), 'MANUAL')`+
		customerPaymentsFrom+` ORDER BY COALESCE(i.paid_at, i.updated_at) DESC, i.id LIMIT $6`,
		append(args, CustomerPaymentsLimit)...)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var p CustomerPayment
		if err := rows.Scan(&p.InvoiceID, &p.CustomerID, &p.CustomerName, &p.Description, &p.AmountCents,
			&p.DueDate.Time, &p.PaidAt, &p.Method); err != nil {
			return nil, err
		}
		out.Payments = append(out.Payments, p)
	}
	out.Truncated = out.Count > len(out.Payments)
	return out, database.MapError(rows.Err())
}
