package finance

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// recurrenceLeadDays: as contas recorrentes aparecem com esse prazo antes do
// vencimento (dá tempo de agendar o pagamento e do aviso de 3 dias).
const recurrenceLeadDays = 35

// maxCatchUp limita quantos meses o gerador recupera de uma vez.
const maxCatchUp = 24

type Service struct {
	db     *database.DB
	loc    *time.Location
	now    func() time.Time
	log    *slog.Logger
	mailer Mailer
	// pix paga fornecedores pela AbacatePay (nil: desligado); pixDev: chave de testes.
	pix    PixSender
	pixDev bool
}

func NewService(db *database.DB, mailer Mailer, log *slog.Logger) *Service {
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		loc = time.FixedZone("BRT", -3*60*60)
	}
	return &Service{db: db, loc: loc, now: time.Now, log: log.With("component", "finance"), mailer: mailer}
}

// SetClock troca o relógio (testes).
func (s *Service) SetClock(now func() time.Time) { s.now = now }

// Today é a data de hoje em Brasília.
func (s *Service) Today() billing.Date { return billing.DateIn(s.now(), s.loc) }

func dateOrNil(t *time.Time) *billing.Date {
	if t == nil {
		return nil
	}
	return &billing.Date{Time: *t}
}

func timeOrNil(d *billing.Date) *time.Time {
	if d == nil {
		return nil
	}
	return &d.Time
}

// ---------------------------------------------------------------------------
// Categorias e fornecedores
// ---------------------------------------------------------------------------

func (s *Service) Categories(ctx context.Context) ([]Category, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, name, kind, dre_group, active FROM finance_categories ORDER BY kind DESC, active DESC, name`)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	out := []Category{}
	for rows.Next() {
		var c Category
		if err := rows.Scan(&c.ID, &c.Name, &c.Kind, &c.Group, &c.Active); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Service) SaveCategory(ctx context.Context, id *uuid.UUID, in CategoryInput) (*Category, error) {
	in, kind, err := in.Normalize()
	if err != nil {
		return nil, err
	}
	var c Category
	if id == nil {
		err = s.db.QueryRow(ctx, `
			INSERT INTO finance_categories (name, kind, dre_group, active) VALUES ($1, $2, $3, $4)
			RETURNING id, name, kind, dre_group, active`, in.Name, kind, in.Group, in.Active).
			Scan(&c.ID, &c.Name, &c.Kind, &c.Group, &c.Active)
	} else {
		// O tipo não muda: os lançamentos dela continuam do mesmo lado.
		err = s.db.QueryRow(ctx, `
			UPDATE finance_categories SET name = $2, dre_group = $3, active = $4, updated_at = NOW()
			WHERE id = $1 AND kind = $5
			RETURNING id, name, kind, dre_group, active`, *id, in.Name, in.Group, in.Active, kind).
			Scan(&c.ID, &c.Name, &c.Kind, &c.Group, &c.Active)
		if errors.Is(err, pgx.ErrNoRows) {
			if exists, _ := s.exists(ctx, `SELECT EXISTS (SELECT 1 FROM finance_categories WHERE id = $1)`, *id); exists {
				return nil, invalid("Uma categoria de despesa não vira de receita (nem o contrário): crie outra.")
			}
		}
	}
	if err != nil {
		if errors.Is(database.MapError(err), database.ErrConflict) {
			return nil, invalid("Já existe uma categoria com esse nome.")
		}
		return nil, database.MapError(err)
	}
	return &c, nil
}

func (s *Service) exists(ctx context.Context, query string, args ...any) (bool, error) {
	var ok bool
	err := s.db.QueryRow(ctx, query, args...).Scan(&ok)
	return ok, database.MapError(err)
}

const supplierColumns = `id, name, document, email, phone, pix_key, pix_key_type, notes, active, created_at`

func scanSupplier(row database.Scanner) (*Supplier, error) {
	var x Supplier
	if err := row.Scan(&x.ID, &x.Name, &x.Document, &x.Email, &x.Phone, &x.PixKey, &x.PixKeyType, &x.Notes, &x.Active, &x.CreatedAt); err != nil {
		return nil, database.MapError(err)
	}
	return &x, nil
}

func (s *Service) Suppliers(ctx context.Context) ([]*Supplier, error) {
	rows, err := s.db.Query(ctx, `SELECT `+supplierColumns+` FROM suppliers ORDER BY active DESC, lower(name)`)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	out := []*Supplier{}
	for rows.Next() {
		x, err := scanSupplier(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func (s *Service) SaveSupplier(ctx context.Context, id *uuid.UUID, in SupplierInput) (*Supplier, error) {
	in, err := in.Normalize()
	if err != nil {
		return nil, err
	}
	if id == nil {
		return scanSupplier(s.db.QueryRow(ctx, `
			INSERT INTO suppliers (name, document, email, phone, pix_key, pix_key_type, notes, active)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING `+supplierColumns,
			in.Name, in.Document, in.Email, in.Phone, in.PixKey, in.PixKeyType, in.Notes, in.Active))
	}
	return scanSupplier(s.db.QueryRow(ctx, `
		UPDATE suppliers SET name = $2, document = $3, email = $4, phone = $5, pix_key = $6, pix_key_type = $7,
			notes = $8, active = $9, updated_at = NOW()
		WHERE id = $1 RETURNING `+supplierColumns,
		*id, in.Name, in.Document, in.Email, in.Phone, in.PixKey, in.PixKeyType, in.Notes, in.Active))
}

// checkRefs confere a categoria (do lado certo) e o fornecedor.
func checkRefs(ctx context.Context, q database.Querier, kind string, categoryID uuid.UUID, supplierID *uuid.UUID) error {
	var categoryKind string
	err := q.QueryRow(ctx, `SELECT kind FROM finance_categories WHERE id = $1`, categoryID).Scan(&categoryKind)
	if errors.Is(err, pgx.ErrNoRows) {
		return invalid("Categoria não encontrada.")
	}
	if err != nil {
		return database.MapError(err)
	}
	if want := map[string]string{KindPayable: CategoryExpense, KindReceivable: CategoryIncome}[kind]; categoryKind != want {
		if kind == KindPayable {
			return invalid("Escolha uma categoria de despesa.")
		}
		return invalid("Escolha uma categoria de receita.")
	}
	if supplierID != nil {
		var ok bool
		if err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM suppliers WHERE id = $1)`, *supplierID).Scan(&ok); err != nil {
			return database.MapError(err)
		}
		if !ok {
			return invalid("Fornecedor não encontrado.")
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Lançamentos
// ---------------------------------------------------------------------------

const entryColumns = `e.id, e.kind, e.description, e.category_id, c.name, c.dre_group, e.supplier_id,
	COALESCE(sp.name, ''), e.amount_cents, e.due_date, e.status, e.paid_on, e.paid_cents, e.payment_method,
	e.payment_code, e.notes, e.recurrence_id, e.installment, e.installments, e.stock_movement_id,
	e.created_at, e.updated_at`

const entryFrom = ` FROM finance_entries e
	JOIN finance_categories c ON c.id = e.category_id
	LEFT JOIN suppliers sp ON sp.id = e.supplier_id`

func (s *Service) scanEntry(row database.Scanner) (*Entry, error) {
	var e Entry
	var due time.Time
	var paidOn *time.Time
	var installment, installments *int16
	err := row.Scan(&e.ID, &e.Kind, &e.Description, &e.CategoryID, &e.CategoryName, &e.Group, &e.SupplierID,
		&e.SupplierName, &e.AmountCents, &due, &e.Status, &paidOn, &e.PaidCents, &e.PaymentMethod,
		&e.PaymentCode, &e.Notes, &e.RecurrenceID, &installment, &installments, &e.StockMovementID,
		&e.CreatedAt, &e.UpdatedAt)
	if err != nil {
		return nil, database.MapError(err)
	}
	e.DueDate = billing.Date{Time: due}
	e.PaidOn = dateOrNil(paidOn)
	if installment != nil && installments != nil {
		a, b := int(*installment), int(*installments)
		e.Installment, e.Installments = &a, &b
	}
	e.Overdue = e.Status == StatusOpen && e.DueDate.Before(s.Today())
	e.Attachments = []Attachment{}
	return &e, nil
}

// EntryFilter escolhe os lançamentos da lista.
type EntryFilter struct {
	Kind string
	// Status: open (em aberto, com as vencidas), overdue, paid, canceled ou all.
	Status     string
	From, To   *billing.Date // pelo vencimento (ou pela data do pagamento, nas pagas)
	CategoryID *uuid.UUID
	SupplierID *uuid.UUID
	Search     string
}

const maxListed = 500

func (s *Service) Entries(ctx context.Context, f EntryFilter) ([]*Entry, error) {
	if f.Kind != KindPayable && f.Kind != KindReceivable {
		return nil, invalid("Tipo de lançamento inválido.")
	}
	where := []string{`e.kind = $1`}
	args := []any{f.Kind}
	arg := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	dateColumn := `e.due_date`
	order := `e.due_date, e.created_at`
	switch f.Status {
	case "", "open":
		where = append(where, `e.status = 'OPEN'`)
	case "overdue":
		where = append(where, `e.status = 'OPEN'`, `e.due_date < `+arg(s.Today().Time))
	case "paid":
		where = append(where, `e.status = 'PAID'`)
		dateColumn, order = `e.paid_on`, `e.paid_on DESC, e.created_at DESC`
	case "canceled":
		where = append(where, `e.status = 'CANCELED'`)
		order = `e.due_date DESC`
	case "all":
		order = `e.due_date DESC, e.created_at DESC`
	default:
		return nil, invalid("Situação inválida.")
	}
	if f.From != nil {
		where = append(where, dateColumn+` >= `+arg(f.From.Time))
	}
	if f.To != nil {
		where = append(where, dateColumn+` <= `+arg(f.To.Time))
	}
	if f.CategoryID != nil {
		where = append(where, `e.category_id = `+arg(*f.CategoryID))
	}
	if f.SupplierID != nil {
		where = append(where, `e.supplier_id = `+arg(*f.SupplierID))
	}
	if q := strings.TrimSpace(f.Search); q != "" {
		like := arg("%" + strings.ReplaceAll(strings.ReplaceAll(q, `\`, `\\`), "%", `\%`) + "%")
		where = append(where, `(e.description ILIKE `+like+` OR sp.name ILIKE `+like+` OR e.notes ILIKE `+like+`)`)
	}
	rows, err := s.db.Query(ctx, `SELECT `+entryColumns+entryFrom+` WHERE `+strings.Join(where, " AND ")+
		` ORDER BY `+order+` LIMIT `+strconv.Itoa(maxListed), args...)
	if err != nil {
		return nil, database.MapError(err)
	}
	out := []*Entry{}
	for rows.Next() {
		e, err := s.scanEntry(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, s.attach(ctx, out)
}

// attach preenche os anexos de cada lançamento (sem o conteúdo).
func (s *Service) attach(ctx context.Context, entries []*Entry) error {
	if len(entries) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(entries))
	byID := make(map[uuid.UUID]*Entry, len(entries))
	for i, e := range entries {
		ids[i], byID[e.ID] = e.ID, e
	}
	rows, err := s.db.Query(ctx, `
		SELECT id, entry_id, filename, content_type, size_bytes, created_at
		FROM finance_attachments WHERE entry_id = ANY($1) ORDER BY created_at`, ids)
	if err != nil {
		return database.MapError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var a Attachment
		if err := rows.Scan(&a.ID, &a.EntryID, &a.Filename, &a.ContentType, &a.SizeBytes, &a.CreatedAt); err != nil {
			return err
		}
		byID[a.EntryID].Attachments = append(byID[a.EntryID].Attachments, a)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	// O último envio por Pix de cada conta.
	pixRows, err := s.db.Query(ctx, `
		SELECT DISTINCT ON (entry_id) `+pixColumns+` FROM finance_pix_transfers
		WHERE entry_id = ANY($1) ORDER BY entry_id, created_at DESC`, ids)
	if err != nil {
		return database.MapError(err)
	}
	defer pixRows.Close()
	for pixRows.Next() {
		p, err := scanPix(pixRows)
		if err != nil {
			return err
		}
		byID[p.EntryID].Pix = p
	}
	return pixRows.Err()
}

func (s *Service) Entry(ctx context.Context, id uuid.UUID) (*Entry, error) {
	e, err := s.scanEntry(s.db.QueryRow(ctx, `SELECT `+entryColumns+entryFrom+` WHERE e.id = $1`, id))
	if err != nil {
		return nil, err
	}
	return e, s.attach(ctx, []*Entry{e})
}

// CreateEntries lança a conta (ou a receita); parcelada, uma por mês.
func (s *Service) CreateEntries(ctx context.Context, in EntryInput, by *uuid.UUID) ([]*Entry, error) {
	in, err := in.Normalize()
	if err != nil {
		return nil, err
	}
	if in.PaidOn != nil && s.Today().Before(*in.PaidOn) {
		return nil, invalid("A data do pagamento não pode ser no futuro.")
	}
	var ids []uuid.UUID
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if err := checkRefs(ctx, tx, in.Kind, in.CategoryID, in.SupplierID); err != nil {
			return err
		}
		ids, err = insertEntries(ctx, tx, in, nil, by)
		return err
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return s.entriesByID(ctx, ids)
}

// insertEntries grava as parcelas na transação (também usado pela compra do
// estoque).
func insertEntries(ctx context.Context, tx pgx.Tx, in EntryInput, movementID *uuid.UUID, by *uuid.UUID) ([]uuid.UUID, error) {
	amounts := SplitInstallments(in.AmountCents, in.Installments)
	ids := make([]uuid.UUID, 0, len(amounts))
	for i, amount := range amounts {
		var installment, installments *int
		if len(amounts) > 1 {
			a, b := i+1, len(amounts)
			installment, installments = &a, &b
		}
		status, paidCents := StatusOpen, (*int64)(nil)
		if in.PaidOn != nil {
			status, paidCents = StatusPaid, &amount
		}
		var id uuid.UUID
		err := tx.QueryRow(ctx, `
			INSERT INTO finance_entries (kind, description, category_id, supplier_id, amount_cents, due_date,
				status, paid_on, paid_cents, payment_method, payment_code, notes, installment, installments,
				stock_movement_id, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
			RETURNING id`,
			in.Kind, in.Description, in.CategoryID, in.SupplierID, amount, AddMonths(in.DueDate, i).Time,
			status, timeOrNil(in.PaidOn), paidCents, in.PaymentMethod, in.PaymentCode, in.Notes,
			installment, installments, movementID, by).Scan(&id)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func (s *Service) entriesByID(ctx context.Context, ids []uuid.UUID) ([]*Entry, error) {
	rows, err := s.db.Query(ctx, `SELECT `+entryColumns+entryFrom+` WHERE e.id = ANY($1) ORDER BY e.due_date`, ids)
	if err != nil {
		return nil, database.MapError(err)
	}
	out := []*Entry{}
	for rows.Next() {
		e, err := s.scanEntry(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, s.attach(ctx, out)
}

// UpdateEntry muda uma conta em aberto.
func (s *Service) UpdateEntry(ctx context.Context, id uuid.UUID, in EntryUpdate) (*Entry, error) {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var kind, status string
		if err := tx.QueryRow(ctx, `SELECT kind, status FROM finance_entries WHERE id = $1 FOR UPDATE`, id).
			Scan(&kind, &status); err != nil {
			return err
		}
		if status != StatusOpen {
			return ErrNotOpen
		}
		if err := noLivePix(ctx, tx, id); err != nil {
			return err
		}
		normalized, err := in.Normalize(kind)
		if err != nil {
			return err
		}
		if err := checkRefs(ctx, tx, kind, normalized.CategoryID, normalized.SupplierID); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			UPDATE finance_entries SET description = $2, category_id = $3, supplier_id = $4, amount_cents = $5,
				due_date = $6, payment_code = $7, notes = $8, updated_at = NOW()
			WHERE id = $1`,
			id, normalized.Description, normalized.CategoryID, normalized.SupplierID, normalized.AmountCents,
			normalized.DueDate.Time, normalized.PaymentCode, normalized.Notes)
		return err
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return s.Entry(ctx, id)
}

// Pay dá baixa numa conta em aberto.
func (s *Service) Pay(ctx context.Context, id uuid.UUID, in PayInput) (*Entry, error) {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var status string
		var amount int64
		if err := tx.QueryRow(ctx, `SELECT status, amount_cents FROM finance_entries WHERE id = $1 FOR UPDATE`, id).
			Scan(&status, &amount); err != nil {
			return err
		}
		if status != StatusOpen {
			return ErrNotOpen
		}
		if err := noLivePix(ctx, tx, id); err != nil {
			return err
		}
		paid, err := in.Normalize(amount, s.Today())
		if err != nil {
			return err
		}
		if paid.PaidCents == 0 {
			return invalid("Informe o valor pago.")
		}
		_, err = tx.Exec(ctx, `
			UPDATE finance_entries SET status = 'PAID', paid_on = $2, paid_cents = $3, payment_method = $4,
				updated_at = NOW()
			WHERE id = $1`, id, paid.PaidOn.Time, paid.PaidCents, paid.Method)
		return err
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return s.Entry(ctx, id)
}

// Reopen desfaz a baixa (ou o cancelamento). A conta paga por Pix pela
// AbacatePay não se reabre: o dinheiro saiu.
func (s *Service) Reopen(ctx context.Context, id uuid.UUID) (*Entry, error) {
	if err := noLivePix(ctx, s.db, id); err != nil {
		return nil, err
	}
	return s.setStatus(ctx, id, `
		UPDATE finance_entries SET status = 'OPEN', paid_on = NULL, paid_cents = NULL, payment_method = '',
			updated_at = NOW()
		WHERE id = $1 AND status <> 'OPEN'`)
}

// Cancel tira a conta das contas (sem apagar o registro).
func (s *Service) Cancel(ctx context.Context, id uuid.UUID) (*Entry, error) {
	if err := noLivePix(ctx, s.db, id); err != nil {
		return nil, err
	}
	return s.setStatus(ctx, id, `
		UPDATE finance_entries SET status = 'CANCELED', updated_at = NOW() WHERE id = $1 AND status = 'OPEN'`)
}

func (s *Service) setStatus(ctx context.Context, id uuid.UUID, query string) (*Entry, error) {
	tag, err := s.db.Exec(ctx, query, id)
	if err != nil {
		return nil, database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		if _, err := s.Entry(ctx, id); err != nil {
			return nil, err
		}
		return nil, invalid("A conta já está nessa situação.")
	}
	return s.Entry(ctx, id)
}

// DeleteEntry apaga um lançamento feito por engano (com os anexos). A conta
// paga precisa ser reaberta antes: o que já saiu do caixa não some sem querer.
func (s *Service) DeleteEntry(ctx context.Context, id uuid.UUID) error {
	if err := noLivePix(ctx, s.db, id); err != nil {
		return err
	}
	var tag pgconn.CommandTag
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		// As tentativas de Pix recusadas saem junto.
		if _, err := tx.Exec(ctx, `DELETE FROM finance_pix_transfers WHERE entry_id = $1 AND status = 'FAILED'`, id); err != nil {
			return err
		}
		var err error
		tag, err = tx.Exec(ctx, `DELETE FROM finance_entries WHERE id = $1 AND status <> 'PAID'`, id)
		return err
	})
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		if _, err := s.Entry(ctx, id); err != nil {
			return err
		}
		return invalid("Reabra a conta paga antes de excluir.")
	}
	return nil
}

// ---------------------------------------------------------------------------
// Anexos
// ---------------------------------------------------------------------------

func (s *Service) AddAttachment(ctx context.Context, entryID uuid.UUID, filename string, data []byte, by *uuid.UUID) (*Attachment, error) {
	if len(data) == 0 || len(data) > MaxAttachmentBytes {
		return nil, invalid("O arquivo precisa ter até %d MB.", MaxAttachmentBytes>>20)
	}
	contentType := AttachmentType(data)
	if contentType == "" {
		return nil, invalid("Envie um PDF ou uma imagem (PNG ou JPG).")
	}
	var a Attachment
	err := s.db.QueryRow(ctx, `
		INSERT INTO finance_attachments (entry_id, filename, content_type, size_bytes, data, created_by)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, entry_id, filename, content_type, size_bytes, created_at`,
		entryID, CleanFilename(filename), contentType, len(data), data, by).
		Scan(&a.ID, &a.EntryID, &a.Filename, &a.ContentType, &a.SizeBytes, &a.CreatedAt)
	if err != nil {
		return nil, database.MapError(err)
	}
	return &a, nil
}

// AttachmentFile devolve o arquivo para baixar.
func (s *Service) AttachmentFile(ctx context.Context, id uuid.UUID) (*Attachment, []byte, error) {
	var a Attachment
	var data []byte
	err := s.db.QueryRow(ctx, `
		SELECT id, entry_id, filename, content_type, size_bytes, created_at, data
		FROM finance_attachments WHERE id = $1`, id).
		Scan(&a.ID, &a.EntryID, &a.Filename, &a.ContentType, &a.SizeBytes, &a.CreatedAt, &data)
	if err != nil {
		return nil, nil, database.MapError(err)
	}
	return &a, data, nil
}

func (s *Service) DeleteAttachment(ctx context.Context, id uuid.UUID) error {
	tag, err := s.db.Exec(ctx, `DELETE FROM finance_attachments WHERE id = $1`, id)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return database.ErrNotFound
	}
	return nil
}

// ---------------------------------------------------------------------------
// Recorrências
// ---------------------------------------------------------------------------

const recurrenceColumns = `r.id, r.kind, r.description, r.category_id, c.name, r.supplier_id, COALESCE(sp.name, ''),
	r.amount_cents, r.due_day, r.next_due_date, r.ends_on, r.active, r.created_at`

const recurrenceFrom = ` FROM finance_recurrences r
	JOIN finance_categories c ON c.id = r.category_id
	LEFT JOIN suppliers sp ON sp.id = r.supplier_id`

func scanRecurrence(row database.Scanner) (*Recurrence, error) {
	var r Recurrence
	var next time.Time
	var ends *time.Time
	var dueDay int16
	if err := row.Scan(&r.ID, &r.Kind, &r.Description, &r.CategoryID, &r.CategoryName, &r.SupplierID,
		&r.SupplierName, &r.AmountCents, &dueDay, &next, &ends, &r.Active, &r.CreatedAt); err != nil {
		return nil, database.MapError(err)
	}
	r.DueDay, r.NextDueDate, r.EndsOn = int(dueDay), billing.Date{Time: next}, dateOrNil(ends)
	return &r, nil
}

func (s *Service) Recurrences(ctx context.Context) ([]*Recurrence, error) {
	rows, err := s.db.Query(ctx, `SELECT `+recurrenceColumns+recurrenceFrom+` ORDER BY r.active DESC, r.kind, r.due_day, r.description`)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	out := []*Recurrence{}
	for rows.Next() {
		r, err := scanRecurrence(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Service) recurrence(ctx context.Context, id uuid.UUID) (*Recurrence, error) {
	return scanRecurrence(s.db.QueryRow(ctx, `SELECT `+recurrenceColumns+recurrenceFrom+` WHERE r.id = $1`, id))
}

// CreateRecurrence cadastra a conta do mês e já gera as que vencem logo.
func (s *Service) CreateRecurrence(ctx context.Context, in RecurrenceInput) (*Recurrence, error) {
	in, err := in.Normalize()
	if err != nil {
		return nil, err
	}
	var id uuid.UUID
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if err := checkRefs(ctx, tx, in.Kind, in.CategoryID, in.SupplierID); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `
			INSERT INTO finance_recurrences (kind, description, category_id, supplier_id, amount_cents, due_day,
				next_due_date, ends_on)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`,
			in.Kind, in.Description, in.CategoryID, in.SupplierID, in.AmountCents, in.FirstDueDate.Day(),
			in.FirstDueDate.Time, timeOrNil(in.EndsOn)).Scan(&id)
	})
	if err != nil {
		return nil, mapErr(err)
	}
	s.GenerateRecurring(ctx)
	return s.recurrence(ctx, id)
}

// UpdateRecurrence muda o que vem pela frente: as próximas e as contas em
// aberto que ela já gerou e ainda não venceram.
func (s *Service) UpdateRecurrence(ctx context.Context, id uuid.UUID, in RecurrenceUpdate) (*Recurrence, error) {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var kind string
		var next time.Time
		if err := tx.QueryRow(ctx, `SELECT kind, next_due_date FROM finance_recurrences WHERE id = $1 FOR UPDATE`, id).
			Scan(&kind, &next); err != nil {
			return err
		}
		normalized, err := RecurrenceInput{
			Kind: kind, Description: in.Description, CategoryID: in.CategoryID, SupplierID: in.SupplierID,
			AmountCents: in.AmountCents, FirstDueDate: billing.NewDate(2000, 1, 1), EndsOn: in.EndsOn,
		}.Normalize()
		if err != nil {
			return err
		}
		if err := checkRefs(ctx, tx, kind, normalized.CategoryID, normalized.SupplierID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE finance_recurrences SET description = $2, category_id = $3, supplier_id = $4, amount_cents = $5,
				ends_on = $6, updated_at = NOW()
			WHERE id = $1`,
			id, normalized.Description, normalized.CategoryID, normalized.SupplierID, normalized.AmountCents,
			timeOrNil(in.EndsOn)); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			UPDATE finance_entries SET description = $2, category_id = $3, supplier_id = $4, amount_cents = $5,
				updated_at = NOW()
			WHERE recurrence_id = $1 AND status = 'OPEN' AND due_date >= $6`,
			id, normalized.Description, normalized.CategoryID, normalized.SupplierID, normalized.AmountCents,
			s.Today().Time)
		if err != nil {
			return err
		}
		// Com o fim antecipado, as que passaram dele saem.
		if in.EndsOn != nil {
			_, err = tx.Exec(ctx, `
				DELETE FROM finance_entries WHERE recurrence_id = $1 AND status = 'OPEN' AND due_date > $2`,
				id, in.EndsOn.Time)
		}
		return err
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return s.recurrence(ctx, id)
}

// EndRecurrence encerra: não gera mais, e as contas em aberto dela que ainda
// não venceram saem (as vencidas e as pagas ficam).
func (s *Service) EndRecurrence(ctx context.Context, id uuid.UUID) (*Recurrence, error) {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE finance_recurrences SET active = FALSE, updated_at = NOW() WHERE id = $1`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return database.ErrNotFound
		}
		_, err = tx.Exec(ctx, `
			DELETE FROM finance_entries WHERE recurrence_id = $1 AND status = 'OPEN' AND due_date >= $2`,
			id, s.Today().Time)
		return err
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return s.recurrence(ctx, id)
}

// GenerateRecurring cria as contas recorrentes que vencem nos próximos
// recurrenceLeadDays (como o gerador das faturas: idempotente e seguro com
// várias instâncias).
func (s *Service) GenerateRecurring(ctx context.Context) {
	created, err := s.generateRecurring(ctx, s.Today().AddDays(recurrenceLeadDays))
	if err != nil {
		s.log.Error("falha ao gerar as contas recorrentes", "err", err)
		return
	}
	if created > 0 {
		s.log.Info("contas recorrentes geradas", "count", created)
	}
}

func (s *Service) generateRecurring(ctx context.Context, horizon billing.Date) (int64, error) {
	type due struct {
		id, categoryID uuid.UUID
		kind, desc     string
		supplierID     *uuid.UUID
		amount         int64
		dueDay         int
		next           billing.Date
		endsOn         *billing.Date
	}
	var created int64
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id, kind, description, category_id, supplier_id, amount_cents, due_day, next_due_date, ends_on
			FROM finance_recurrences
			WHERE active AND next_due_date <= $1 AND (ends_on IS NULL OR next_due_date <= ends_on)
			ORDER BY next_due_date
			FOR UPDATE SKIP LOCKED`, horizon.Time)
		if err != nil {
			return err
		}
		var pending []due
		for rows.Next() {
			var d due
			var next time.Time
			var ends *time.Time
			var dueDay int16
			if err := rows.Scan(&d.id, &d.kind, &d.desc, &d.categoryID, &d.supplierID, &d.amount, &dueDay, &next, &ends); err != nil {
				rows.Close()
				return err
			}
			d.dueDay, d.next, d.endsOn = int(dueDay), billing.Date{Time: next}, dateOrNil(ends)
			pending = append(pending, d)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, d := range pending {
			next := d.next
			for i := 0; i < maxCatchUp && !horizon.Before(next) && (d.endsOn == nil || !d.endsOn.Before(next)); i++ {
				tag, err := tx.Exec(ctx, `
					INSERT INTO finance_entries (kind, description, category_id, supplier_id, amount_cents, due_date,
						recurrence_id)
					VALUES ($1, $2, $3, $4, $5, $6, $7)
					ON CONFLICT (recurrence_id, due_date) DO NOTHING`,
					d.kind, d.desc, d.categoryID, d.supplierID, d.amount, next.Time, d.id)
				if err != nil {
					return err
				}
				created += tag.RowsAffected()
				next = billing.NewDate(next.Year(), next.Month()+1, d.dueDay)
			}
			if _, err := tx.Exec(ctx, `UPDATE finance_recurrences SET next_due_date = $2 WHERE id = $1`, d.id, next.Time); err != nil {
				return err
			}
		}
		return nil
	})
	return created, database.MapError(err)
}

// ---------------------------------------------------------------------------
// Saldo inicial
// ---------------------------------------------------------------------------

// Settings é o ponto de partida do caixa.
type Settings struct {
	OpeningBalanceCents int64        `json:"openingBalanceCents"`
	OpeningDate         billing.Date `json:"openingDate"`
}

func (s *Service) Settings(ctx context.Context) (*Settings, error) {
	var out Settings
	var date time.Time
	if err := s.db.QueryRow(ctx, `SELECT opening_balance_cents, opening_date FROM finance_settings`).
		Scan(&out.OpeningBalanceCents, &date); err != nil {
		return nil, database.MapError(err)
	}
	out.OpeningDate = billing.Date{Time: date}
	return &out, nil
}

func (s *Service) SaveSettings(ctx context.Context, in Settings) (*Settings, error) {
	switch {
	case in.OpeningDate.IsZero():
		return nil, invalid("Informe a data do saldo inicial.")
	case s.Today().Before(in.OpeningDate):
		return nil, invalid("A data do saldo inicial não pode ser no futuro.")
	case in.OpeningBalanceCents > maxAmountCents*10 || in.OpeningBalanceCents < -maxAmountCents*10:
		return nil, invalid("Saldo inicial inválido.")
	}
	if _, err := s.db.Exec(ctx, `
		UPDATE finance_settings SET opening_balance_cents = $1, opening_date = $2, updated_at = NOW()`,
		in.OpeningBalanceCents, in.OpeningDate.Time); err != nil {
		return nil, database.MapError(err)
	}
	return s.Settings(ctx)
}

// mapErr deixa passar a mensagem de validação e traduz os erros do banco.
func mapErr(err error) error {
	var v ValidationError
	if errors.As(err, &v) {
		return v
	}
	return database.MapError(err)
}
