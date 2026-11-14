package fiscal

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// Uma nota por pagamento: as faturas do mesmo cliente pagas juntas, do mesmo
// mês (a competência da nota), saem numa NFS-e só. Juntas quer dizer pela
// mesma cobrança (o Pix, o boleto ou o cartão de várias, como o "pagar
// tudo") ou, sem cobrança, pelas baixas da central feitas em sequência.

// manualQuiet: as baixas da central do mesmo cliente e mês esperam este tempo
// sem nenhuma nova para sair numa nota só (quem dá baixa nas faturas de cada
// veículo, uma por uma, faz isso em poucos minutos).
const manualQuiet = 10 * time.Minute

// groupsPerRun: quantos pagamentos a rotina enfileira por vez (cada um com
// todas as faturas dele, mesmo as centenas de uma frota).
const groupsPerRun = 50

// paymentGroup é a chave do pagamento da fatura: o cliente, o mês e a
// cobrança que pagou (a baixa da central, sem cobrança, junta as do mês; a
// paga sem cobrança e sem baixa, de antes, fica sozinha).
const paymentGroup = `COALESCE(i.customer_id::text, '') || ':' || to_char(i.due_date, 'YYYY-MM') || ':' || CASE
	WHEN i.paid_charge_id IS NOT NULL THEN 'c' || i.paid_charge_id::text
	WHEN i.paid_via = 'MANUAL' THEN 'm'
	ELSE 'i' || i.id::text END`

// noteItem é uma fatura na nota: o texto de serviço dela e, na mensalidade
// (uma assinatura, um veículo), a placa ou o nome do veículo.
type noteItem struct {
	text    string
	monthly bool
	vehicle string
}

// maxPlates: quantas placas a nota lista em cada linha; o resto vira "e mais N".
const maxPlates = 20

// maxNoteText: o texto do serviço da nota sem passar do limite da NFS-e (a
// descrição do serviço vai até 2.000 caracteres, com o começo da configuração).
const maxNoteText = 1500

// noteText é o texto do serviço da nota de várias faturas: as mensalidades
// iguais numa linha, com a quantidade de veículos e as placas ("Plano Mensal
// — novembro/2026 · 3 veículos (ABC1D23, DEF4G56, GHI7J89)"); as outras
// faturas, cada uma com o texto dela ("× 2" quando se repete). Uma fatura só:
// o texto dela, como sempre.
func noteText(items []noteItem) string {
	if len(items) == 1 {
		return items[0].text
	}
	type line struct {
		text     string
		monthly  bool
		count    int
		vehicles []string
	}
	var lines []*line
	byKey := map[string]*line{}
	for _, it := range items {
		key := fmt.Sprintf("%t|%s", it.monthly, it.text)
		l := byKey[key]
		if l == nil {
			l = &line{text: it.text, monthly: it.monthly}
			byKey[key] = l
			lines = append(lines, l)
		}
		l.count++
		if it.vehicle != "" {
			l.vehicles = append(l.vehicles, it.vehicle)
		}
	}
	render := func(plates bool) string {
		parts := make([]string, 0, len(lines))
		for _, l := range lines {
			switch {
			case l.monthly:
				parts = append(parts, strings.TrimSpace(l.text+" · "+vehiclesText(l.count, l.vehicles, plates)))
			case l.count > 1:
				parts = append(parts, fmt.Sprintf("%s × %d", l.text, l.count))
			default:
				parts = append(parts, l.text)
			}
		}
		return strings.Join(parts, "; ")
	}
	// Numa frota grande, sem as placas; se nem assim couber, cortado.
	text := render(true)
	if len([]rune(text)) > maxNoteText {
		text = render(false)
	}
	if r := []rune(text); len(r) > maxNoteText {
		text = strings.TrimSpace(string(r[:maxNoteText-1])) + "…"
	}
	return text
}

// vehiclesText: "1 veículo (ABC1D23)", "3 veículos (ABC1D23, DEF4G56,
// GHI7J89)", "25 veículos (20 placas e mais 5)" ou, sem as placas, só a conta.
func vehiclesText(count int, vehicles []string, plates bool) string {
	text := fmt.Sprintf("%d veículo", count)
	if count != 1 {
		text += "s"
	}
	if !plates || len(vehicles) == 0 {
		return text
	}
	shown := vehicles[:min(len(vehicles), maxPlates)]
	list := strings.Join(shown, ", ")
	if rest := count - len(shown); rest > 0 {
		list += fmt.Sprintf(" e mais %d", rest)
	}
	return text + " (" + list + ")"
}

// enqueue cria as notas das faturas pagas desde o início, uma por pagamento,
// pelo valor cheio das faturas: a Farbo só emite nota de serviço, e a
// mensalidade inteira é serviço (com a parte do rastreador incluso e, nas
// antigas, a parcela do comprado). Uma fatura entra em uma nota só.
func (s *Service) enqueue(ctx context.Context, since time.Time) error {
	now := s.now()
	rows, err := s.db.Query(ctx, `
		WITH c AS (
			SELECT i.id, i.customer_id, i.amount_cents, i.description, i.due_date, i.paid_at, i.created_at,
				i.subscription_id IS NOT NULL AS monthly,
				COALESCE(NULLIF(v.plate, ''), v.name, '') AS vehicle,
				i.paid_charge_id IS NULL AND i.paid_via = 'MANUAL' AS manual,
				`+paymentGroup+` AS grp
			FROM invoices i
			LEFT JOIN subscriptions s ON s.id = i.subscription_id
			LEFT JOIN vehicles v ON v.id = s.vehicle_id
			WHERE i.status = 'PAID' AND i.paid_at >= $1 AND i.amount_cents > 0 AND `+Eligible+`
				AND NOT EXISTS (SELECT 1 FROM fiscal_document_invoices fi WHERE fi.invoice_id = i.id)
		), g AS (
			SELECT grp FROM c GROUP BY grp
			-- As baixas da central esperam a última do mês ficar para trás.
			HAVING NOT bool_or(manual) OR max(paid_at) <= $2
			ORDER BY min(paid_at), grp
			LIMIT $3
		)
		SELECT c.grp, c.id, c.customer_id, c.amount_cents, c.description, c.monthly, c.vehicle
		FROM c JOIN g USING (grp)
		ORDER BY c.grp, c.due_date, c.created_at, c.id`, since, now.Add(-manualQuiet), groupsPerRun)
	if err != nil {
		return database.MapError(err)
	}
	type group struct {
		customer *uuid.UUID
		ids      []uuid.UUID
		amounts  []int32
		total    int
		items    []noteItem
	}
	var order []string
	groups := map[string]*group{}
	for rows.Next() {
		var key, description, vehicle string
		var id uuid.UUID
		var customer *uuid.UUID
		var cents int
		var monthly bool
		if err := rows.Scan(&key, &id, &customer, &cents, &description, &monthly, &vehicle); err != nil {
			rows.Close()
			return database.MapError(err)
		}
		g := groups[key]
		if g == nil {
			g = &group{customer: customer}
			groups[key] = g
			order = append(order, key)
		}
		g.ids = append(g.ids, id)
		g.amounts = append(g.amounts, int32(cents))
		g.total += cents
		g.items = append(g.items, noteItem{text: serviceText(description), monthly: monthly, vehicle: vehicle})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return database.MapError(err)
	}

	created, invoices := 0, 0
	for _, key := range order {
		g := groups[key]
		err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
			var doc uuid.UUID
			// A primeira fatura (a de vencimento mais antigo) é a referência na Notaas.
			if err := tx.QueryRow(ctx, `INSERT INTO fiscal_documents (invoice_id, customer_id, amount_cents, description, next_try_at)
				VALUES ($1, $2, $3, $4, $5) RETURNING id`, g.ids[0], g.customer, g.total, noteText(g.items), now).Scan(&doc); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `INSERT INTO fiscal_document_invoices (invoice_id, document_id, amount_cents)
				SELECT u.id, $2, u.cents FROM unnest($1::uuid[], $3::int[]) AS u (id, cents)`, g.ids, doc, g.amounts)
			return err
		})
		if err != nil {
			// Outra rotina pegou alguma das faturas no meio: nada entra, e a
			// próxima volta vê o que sobrou.
			s.log.Error("falha ao enfileirar a nota fiscal do pagamento", "invoices", len(g.ids), "err", database.MapError(err))
			continue
		}
		created++
		invoices += len(g.ids)
	}
	if created > 0 {
		s.log.Info("notas fiscais enfileiradas", "count", created, "invoices", invoices)
	}
	return nil
}
