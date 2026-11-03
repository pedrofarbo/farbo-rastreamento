package affiliates

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// Os clientes do afiliado, na página dele: o nome, os veículos (a placa e o
// número do rastreador) e a situação do pagamento mês a mês. São dados dos
// clientes: só com a senha na página (o link sozinho não mostra).

// ErrCustomersNeedPassword: a página sem senha não mostra os clientes.
var ErrCustomersNeedPassword = errors.New("para ver os seus clientes, peça à Farbo para colocar senha na sua página")

// customerMonths: os meses mostrados, do atual para trás.
const customerMonths = 6

// Situação do pagamento de um cliente no mês.
const (
	PaymentPaid    = "paid"    // todas as faturas do mês pagas
	PaymentOpen    = "open"    // em aberto, ainda no prazo
	PaymentOverdue = "overdue" // alguma vencida sem pagar
	PaymentNone    = "none"    // sem fatura no mês
)

// PartnerCustomer é um cliente indicado pelo afiliado.
type PartnerCustomer struct {
	Name     string            `json:"name"`
	Since    time.Time         `json:"since"`
	Vehicles []PartnerVehicle  `json:"vehicles"`
	Months   []CustomerPayment `json:"months"`
}

// PartnerVehicle é um veículo do cliente: o nome, a placa e o número do
// rastreador (vazio: ainda sem rastreador).
type PartnerVehicle struct {
	Name  string `json:"name"`
	Plate string `json:"plate"`
	IMEI  string `json:"imei"`
}

// CustomerPayment é a situação do pagamento do cliente num mês (AAAA-MM).
type CustomerPayment struct {
	Month  string `json:"month"`
	Status string `json:"status"`
}

// PartnerCustomers são os clientes do afiliado da página (com a senha), com
// os veículos e os últimos meses de pagamento. A contratação pelo site ainda
// sem pagamento não entra.
func (s *Service) PartnerCustomers(ctx context.Context, token string) ([]PartnerCustomer, error) {
	if len(token) < 20 || len(token) > 64 {
		return nil, database.ErrNotFound
	}
	var affiliateID uuid.UUID
	var protected bool
	err := s.db.QueryRow(ctx, `SELECT id, report_password_hash <> '' FROM affiliates WHERE report_token = $1`, token).
		Scan(&affiliateID, &protected)
	if err != nil {
		return nil, database.MapError(err)
	}
	if !protected {
		return nil, ErrCustomersNeedPassword
	}

	local := s.now().In(s.loc)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
	first := time.Date(local.Year(), local.Month()-customerMonths+1, 1, 0, 0, 0, 0, time.UTC)
	rows, err := s.db.Query(ctx, `
		WITH cust AS (
			SELECT r.customer_id, u.name, r.created_at FROM affiliate_referrals r JOIN users u ON u.id = r.customer_id
			WHERE r.affiliate_id = $1 AND NOT checkout_pending(r.customer_id)
		), months AS (
			SELECT generate_series($2::date, $3::date, INTERVAL '1 month')::date AS m
		)
		SELECT c.customer_id, c.name, c.created_at, to_char(m.m, 'YYYY-MM'),
			count(i.id), count(i.id) FILTER (WHERE i.status = 'PAID'),
			count(i.id) FILTER (WHERE i.status = 'OPEN' AND i.due_date < $4::date)
		FROM cust c CROSS JOIN months m
		LEFT JOIN invoices i ON i.customer_id = c.customer_id AND i.status <> 'CANCELED'
			AND i.due_date >= m.m AND i.due_date < (m.m + INTERVAL '1 month')
		GROUP BY c.customer_id, c.name, c.created_at, m.m
		ORDER BY lower(c.name), c.customer_id, m.m DESC`,
		affiliateID, first, time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, time.UTC), today)
	if err != nil {
		return nil, database.MapError(err)
	}
	out := []PartnerCustomer{}
	index := map[uuid.UUID]int{}
	for rows.Next() {
		var id uuid.UUID
		var c PartnerCustomer
		var month string
		var total, paid, overdue int
		if err := rows.Scan(&id, &c.Name, &c.Since, &month, &total, &paid, &overdue); err != nil {
			rows.Close()
			return nil, database.MapError(err)
		}
		i, ok := index[id]
		if !ok {
			c.Vehicles, c.Months = []PartnerVehicle{}, []CustomerPayment{}
			out = append(out, c)
			i = len(out) - 1
			index[id] = i
		}
		status := PaymentOpen
		switch {
		case total == 0:
			status = PaymentNone
		case paid == total:
			status = PaymentPaid
		case overdue > 0:
			status = PaymentOverdue
		}
		out[i].Months = append(out[i].Months, CustomerPayment{Month: month, Status: status})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, database.MapError(err)
	}
	if len(out) == 0 {
		return out, nil
	}

	ids := make([]uuid.UUID, 0, len(index))
	for id := range index {
		ids = append(ids, id)
	}
	vrows, err := s.db.Query(ctx, `
		SELECT v.owner_id, v.name, COALESCE(v.plate, ''), COALESCE(d.imei, '')
		FROM vehicles v LEFT JOIN devices d ON d.id = v.device_id
		WHERE v.owner_id = ANY($1) ORDER BY lower(v.name)`, ids)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer vrows.Close()
	for vrows.Next() {
		var owner uuid.UUID
		var v PartnerVehicle
		if err := vrows.Scan(&owner, &v.Name, &v.Plate, &v.IMEI); err != nil {
			return nil, database.MapError(err)
		}
		if i, ok := index[owner]; ok {
			out[i].Vehicles = append(out[i].Vehicles, v)
		}
	}
	return out, database.MapError(vrows.Err())
}
