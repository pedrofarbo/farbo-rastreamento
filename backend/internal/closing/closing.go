// Package closing é o fechamento do mês: para cada pagador — o cliente ou a
// associação/seguradora — o que foi faturado no mês (as faturas que vencem
// nele, pagas ou não: competência) e a nota fiscal dele. A nota é emitida
// fora (no emissor da empresa); aqui a central registra o número, a data, o
// valor e o arquivo (PDF ou XML), e o cliente e a organização baixam na tela
// de faturas.
package closing

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/finance"
)

// MaxFileBytes: o PDF ou o XML de uma nota cabem com folga.
const MaxFileBytes = 5 << 20

// InputError é um dado recusado; a mensagem vai para a tela.
type InputError struct{ Message string }

func (e *InputError) Error() string { return e.Message }

// Note é a nota fiscal registrada de um pagador num mês.
type Note struct {
	ID          uuid.UUID    `json:"id"`
	CustomerID  uuid.UUID    `json:"customerId"`
	Month       string       `json:"month"` // AAAA-MM
	Number      string       `json:"number"`
	IssuedOn    billing.Date `json:"issuedOn"`
	AmountCents int          `json:"amountCents"`
	Filename    string       `json:"filename"`
	HasFile     bool         `json:"hasFile"`
	CreatedAt   time.Time    `json:"createdAt"`
	UpdatedAt   time.Time    `json:"updatedAt"`
}

// Line é um pagador no fechamento do mês.
type Line struct {
	PayerID uuid.UUID `json:"payerId"`
	// Kind: customer (cliente) ou organization (associação/seguradora).
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	Document string `json:"document"`
	Email    string `json:"email"`
	// Invoices: as linhas de fatura do mês; AmountCents, o faturado (sem as
	// canceladas); PaidCents, o que delas já foi pago.
	Invoices    int   `json:"invoices"`
	AmountCents int   `json:"amountCents"`
	PaidCents   int   `json:"paidCents"`
	Note        *Note `json:"note"`
}

// Month é o fechamento de um mês.
type Month struct {
	Month string  `json:"month"`
	Lines []*Line `json:"lines"`
	// Os totais: o faturado, o pago e quantas notas faltam.
	AmountCents  int `json:"amountCents"`
	PaidCents    int `json:"paidCents"`
	Notes        int `json:"notes"`
	MissingNotes int `json:"missingNotes"`
}

type Service struct{ db *database.DB }

func NewService(db *database.DB) *Service { return &Service{db: db} }

// ParseMonth lê "AAAA-MM" e devolve o primeiro dia do mês.
func ParseMonth(raw string) (time.Time, error) {
	t, err := time.Parse("2006-01", strings.TrimSpace(raw))
	if err != nil {
		return time.Time{}, &InputError{"mês inválido (use AAAA-MM)"}
	}
	return t, nil
}

const noteColumns = `id, customer_id, to_char(month, 'YYYY-MM'), number, issued_on, amount_cents, filename,
	data IS NOT NULL, created_at, updated_at`

func scanNote(row database.Scanner) (*Note, error) {
	var n Note
	var issued time.Time
	if err := row.Scan(&n.ID, &n.CustomerID, &n.Month, &n.Number, &issued, &n.AmountCents, &n.Filename,
		&n.HasFile, &n.CreatedAt, &n.UpdatedAt); err != nil {
		return nil, database.MapError(err)
	}
	n.IssuedOn = billing.Date{Time: issued}
	return &n, nil
}

// Close é o fechamento do mês: cada pagador com o que foi faturado e a nota.
func (s *Service) Close(ctx context.Context, month time.Time) (*Month, error) {
	end := month.AddDate(0, 1, 0)
	rows, err := s.db.Query(ctx, `
		SELECT u.id, u.role, u.name, u.document, u.email, count(*),
			COALESCE(sum(i.amount_cents), 0),
			COALESCE(sum(i.amount_cents) FILTER (WHERE i.status = 'PAID'), 0)
		FROM invoices i JOIN users u ON u.id = i.customer_id
		WHERE i.status <> 'CANCELED' AND i.due_date >= $1 AND i.due_date < $2
		GROUP BY u.id
		ORDER BY lower(u.name), u.id`, month, end)
	if err != nil {
		return nil, database.MapError(err)
	}
	out := &Month{Month: month.Format("2006-01"), Lines: []*Line{}}
	byPayer := map[uuid.UUID]*Line{}
	for rows.Next() {
		var l Line
		var role string
		if err := rows.Scan(&l.PayerID, &role, &l.Name, &l.Document, &l.Email, &l.Invoices, &l.AmountCents, &l.PaidCents); err != nil {
			rows.Close()
			return nil, database.MapError(err)
		}
		l.Kind = "customer"
		if role == "organization" {
			l.Kind = "organization"
		}
		out.Lines = append(out.Lines, &l)
		byPayer[l.PayerID] = &l
		out.AmountCents += l.AmountCents
		out.PaidCents += l.PaidCents
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, database.MapError(err)
	}

	notes, err := s.db.Query(ctx, `SELECT `+noteColumns+` FROM fiscal_notes WHERE month = $1`, month)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer notes.Close()
	for notes.Next() {
		n, err := scanNote(notes)
		if err != nil {
			return nil, err
		}
		if l := byPayer[n.CustomerID]; l != nil {
			l.Note = n
		}
	}
	if err := notes.Err(); err != nil {
		return nil, database.MapError(err)
	}
	for _, l := range out.Lines {
		if l.Note != nil {
			out.Notes++
		} else if l.AmountCents > 0 {
			out.MissingNotes++
		}
	}
	return out, nil
}

// NoteInput é a nota emitida (o arquivo é opcional).
type NoteInput struct {
	CustomerID  uuid.UUID
	Month       time.Time
	Number      string
	IssuedOn    billing.Date
	AmountCents int
	Filename    string
	Data        []byte
}

// fileType é o tipo do arquivo da nota: PDF, imagem ou o XML da NFS-e.
func fileType(data []byte) string {
	if t := finance.AttachmentType(data); t != "" {
		return t
	}
	head := bytes.TrimSpace(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")))
	if bytes.HasPrefix(head, []byte("<?xml")) || bytes.HasPrefix(head, []byte("<")) {
		return "application/xml"
	}
	return ""
}

// SaveNote registra (ou troca) a nota do pagador no mês. Sem arquivo novo,
// mantém o que já tinha.
func (s *Service) SaveNote(ctx context.Context, in NoteInput, by *uuid.UUID) (*Note, error) {
	in.Number = strings.TrimSpace(in.Number)
	switch {
	case in.Number == "" || len(in.Number) > 60:
		return nil, &InputError{"informe o número da nota (até 60 caracteres)"}
	case in.IssuedOn.IsZero():
		return nil, &InputError{"informe a data de emissão"}
	case in.AmountCents < 0 || in.AmountCents > 100_000_000_00:
		return nil, &InputError{"valor da nota fora da faixa"}
	case len(in.Data) > MaxFileBytes:
		return nil, &InputError{fmt.Sprintf("o arquivo precisa ter até %d MB", MaxFileBytes>>20)}
	}
	var contentType string
	var data []byte
	if len(in.Data) > 0 {
		if contentType = fileType(in.Data); contentType == "" {
			return nil, &InputError{"envie o PDF ou o XML da nota (ou uma imagem PNG ou JPG)"}
		}
		data = in.Data
	}
	var exists bool
	if err := s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE id = $1 AND role IN ('customer', 'organization'))`,
		in.CustomerID).Scan(&exists); err != nil {
		return nil, database.MapError(err)
	}
	if !exists {
		return nil, database.ErrNotFound
	}
	return scanNote(s.db.QueryRow(ctx, `
		INSERT INTO fiscal_notes (customer_id, month, number, issued_on, amount_cents, filename, content_type, size_bytes, data, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (customer_id, month) DO UPDATE SET number = EXCLUDED.number, issued_on = EXCLUDED.issued_on,
			amount_cents = EXCLUDED.amount_cents,
			filename = CASE WHEN EXCLUDED.data IS NULL THEN fiscal_notes.filename ELSE EXCLUDED.filename END,
			content_type = CASE WHEN EXCLUDED.data IS NULL THEN fiscal_notes.content_type ELSE EXCLUDED.content_type END,
			size_bytes = CASE WHEN EXCLUDED.data IS NULL THEN fiscal_notes.size_bytes ELSE EXCLUDED.size_bytes END,
			data = COALESCE(EXCLUDED.data, fiscal_notes.data),
			updated_at = NOW()
		RETURNING `+noteColumns,
		in.CustomerID, in.Month, in.Number, in.IssuedOn.Time, in.AmountCents, finance.CleanFilename(in.Filename),
		contentType, len(data), data, by))
}

// DeleteNote tira a nota registrada (emitida errada, cancelada).
func (s *Service) DeleteNote(ctx context.Context, id uuid.UUID) (*Note, error) {
	return scanNote(s.db.QueryRow(ctx, `DELETE FROM fiscal_notes WHERE id = $1 RETURNING `+noteColumns, id))
}

// Notes são as notas de um pagador, da mais nova à mais antiga.
func (s *Service) Notes(ctx context.Context, customerID uuid.UUID) ([]*Note, error) {
	rows, err := s.db.Query(ctx, `SELECT `+noteColumns+` FROM fiscal_notes WHERE customer_id = $1 ORDER BY month DESC`, customerID)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	out := []*Note{}
	for rows.Next() {
		n, err := scanNote(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, database.MapError(rows.Err())
}

// File é o arquivo da nota para baixar.
type File struct {
	Note        *Note
	ContentType string
	Data        []byte
}

// NoteFile devolve o arquivo da nota; com owner, só se for dele (de outro:
// não encontrado).
func (s *Service) NoteFile(ctx context.Context, id uuid.UUID, owner *uuid.UUID) (*File, error) {
	var f File
	var data []byte
	var contentType string
	n := &Note{}
	var issued time.Time
	err := s.db.QueryRow(ctx, `SELECT `+noteColumns+`, content_type, data FROM fiscal_notes WHERE id = $1`, id).
		Scan(&n.ID, &n.CustomerID, &n.Month, &n.Number, &issued, &n.AmountCents, &n.Filename, &n.HasFile,
			&n.CreatedAt, &n.UpdatedAt, &contentType, &data)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (data == nil || (owner != nil && *owner != n.CustomerID))) {
		return nil, database.ErrNotFound
	}
	if err != nil {
		return nil, database.MapError(err)
	}
	n.IssuedOn = billing.Date{Time: issued}
	f.Note, f.ContentType, f.Data = n, contentType, data
	return &f, nil
}
