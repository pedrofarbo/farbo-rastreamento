// Package fiscal emite a nota fiscal de serviço (NFS-e) das faturas pagas,
// pela Notaas: a mensalidade (sem a parcela do rastreador, que é produto), o
// proporcional, a instalação e a desinstalação, e a fatura do mês da
// organização. A nota sai sozinha depois do pagamento, vai por e-mail ao
// cliente e aparece na ficha; estornada a fatura, a nota é cancelada. A
// Notaas cobra por nota: a rotina não reenvia o que ela recusou.
package fiscal

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/mail"
)

// As situações da nota.
const (
	StatusPending       = "pending"        // esperando a emissão
	StatusProcessing    = "processing"     // na prefeitura
	StatusIssued        = "issued"         // emitida
	StatusError         = "error"          // recusada (a central corrige e tenta de novo)
	StatusBlocked       = "blocked"        // o limite de notas do plano acabou: no mês seguinte
	StatusCancelPending = "cancel_pending" // a fatura foi estornada: cancelando
	StatusCancelled     = "cancelled"
)

// batch: quantas notas a rotina trata por vez (a Notaas aceita 30 por minuto).
const batch = 20

// ErrNotFound: a nota não existe.
var ErrNotFound = errors.New("nota fiscal não encontrada")

// Gateway é o que o serviço usa da Notaas. *Client implementa.
type Gateway interface {
	Emit(ctx context.Context, req EmitRequest) (string, error)
	Status(ctx context.Context, id string) (*Status, error)
	Cancel(ctx context.Context, id, reason string) error
	PDF(ctx context.Context, id string) (*PDF, error)
}

// Mailer manda a nota ao cliente.
type Mailer interface {
	FiscalIssued(ctx context.Context, d mail.FiscalDocument) error
}

// Document é a nota de uma fatura, como a ficha e o app mostram.
type Document struct {
	ID               uuid.UUID `json:"id"`
	InvoiceID        uuid.UUID `json:"invoiceId"`
	Status           string    `json:"status"`
	Number           string    `json:"number"`
	VerificationCode string    `json:"verificationCode"`
	AmountCents      int       `json:"amountCents"`
	// HasPDF: dá para baixar o DANFSE (pelo /pdf, que busca na Notaas).
	HasPDF      bool       `json:"hasPdf"`
	Error       string     `json:"error"`
	IssuedAt    *time.Time `json:"issuedAt"`
	CancelledAt *time.Time `json:"cancelledAt"`
	CreatedAt   time.Time  `json:"createdAt"`
}

type Service struct {
	db     *database.DB
	api    Gateway
	cfg    config.Fiscal
	mailer Mailer
	log    *slog.Logger
	now    func() time.Time
}

func NewService(db *database.DB, api Gateway, cfg config.Fiscal, mailer Mailer, log *slog.Logger) *Service {
	return &Service{db: db, api: api, cfg: cfg, mailer: mailer, log: log.With("component", "fiscal"), now: time.Now}
}

// SetClock troca o relógio (testes).
func (s *Service) SetClock(now func() time.Time) { s.now = now }

const documentColumns = `id, invoice_id, status, number, verification_code, amount_cents, provider_id <> '' AND status = 'issued',
	error, issued_at, cancelled_at, created_at`

func scanDocument(row database.Scanner) (*Document, error) {
	var d Document
	if err := row.Scan(&d.ID, &d.InvoiceID, &d.Status, &d.Number, &d.VerificationCode, &d.AmountCents, &d.HasPDF, &d.Error,
		&d.IssuedAt, &d.CancelledAt, &d.CreatedAt); err != nil {
		return nil, database.MapError(err)
	}
	return &d, nil
}

// ForCustomer são as notas do cliente, pela fatura.
func (s *Service) ForCustomer(ctx context.Context, customerID uuid.UUID) (map[string]*Document, error) {
	rows, err := s.db.Query(ctx, `SELECT `+documentColumns+` FROM fiscal_documents WHERE customer_id = $1
		ORDER BY created_at DESC LIMIT 500`, customerID)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	out := map[string]*Document{}
	for rows.Next() {
		d, err := scanDocument(rows)
		if err != nil {
			return nil, err
		}
		out[d.InvoiceID.String()] = d
	}
	return out, database.MapError(rows.Err())
}

// Get é uma nota; owner, quando dado, confere que é do cliente.
func (s *Service) Get(ctx context.Context, id uuid.UUID, owner *uuid.UUID) (*Document, string, error) {
	var providerID string
	var customer *uuid.UUID
	err := s.db.QueryRow(ctx, `SELECT provider_id, customer_id FROM fiscal_documents WHERE id = $1`, id).
		Scan(&providerID, &customer)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && owner != nil && (customer == nil || *customer != *owner)) {
		return nil, "", ErrNotFound
	}
	if err != nil {
		return nil, "", database.MapError(err)
	}
	d, err := scanDocument(s.db.QueryRow(ctx, `SELECT `+documentColumns+` FROM fiscal_documents WHERE id = $1`, id))
	return d, providerID, err
}

// PDF busca o DANFSE da nota emitida (e o número dela, para o nome do arquivo).
func (s *Service) PDF(ctx context.Context, id uuid.UUID, owner *uuid.UUID) (*PDF, string, error) {
	d, providerID, err := s.Get(ctx, id, owner)
	if err != nil {
		return nil, "", err
	}
	if !d.HasPDF || s.api == nil {
		return nil, "", ErrNotFound
	}
	pdf, err := s.api.PDF(ctx, providerID)
	return pdf, d.Number, err
}

// Retry: a central corrigiu o cadastro (ou o plano) e manda emitir de novo
// a nota recusada.
func (s *Service) Retry(ctx context.Context, id uuid.UUID) (*Document, error) {
	tag, err := s.db.Exec(ctx, `UPDATE fiscal_documents SET status = 'pending', provider_id = '', error = '',
		next_try_at = NOW(), updated_at = NOW() WHERE id = $1 AND status IN ('error', 'blocked')`, id)
	if err != nil {
		return nil, database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		if _, _, err := s.Get(ctx, id, nil); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("%w: só a nota recusada pode ser emitida de novo", ErrNotRetryable)
	}
	d, _, err := s.Get(ctx, id, nil)
	return d, err
}

// ErrNotRetryable: a nota não está recusada.
var ErrNotRetryable = errors.New("nota fiscal fora da situação")

// Touch: o webhook avisou que a nota mudou; a rotina consulta já.
func (s *Service) Touch(ctx context.Context, providerID string) error {
	_, err := s.db.Exec(ctx, `UPDATE fiscal_documents SET next_try_at = NOW(), updated_at = NOW()
		WHERE provider_id = $1 AND status IN ('processing', 'cancel_pending')`, providerID)
	return database.MapError(err)
}

// eligible são as faturas que dão NFS-e: a mensalidade, o proporcional, a
// instalação, a desinstalação e a fatura do mês da organização (o frete, o
// rastreador vendido e as avulsas da central, não).
const eligible = `(i.subscription_id IS NOT NULL OR i.description LIKE '% — proporcional de %'
	OR i.description LIKE 'Instalação do rastreador%' OR i.description LIKE 'Desinstalação do rastreador%'
	OR EXISTS (SELECT 1 FROM organization_invoices oi WHERE oi.invoice_id = i.id))`

// Work é a rotina (a cada minuto): marca desde quando vale, enfileira as
// faturas pagas, emite, acompanha, cancela as estornadas e manda o e-mail.
func (s *Service) Work(ctx context.Context) {
	if s.api == nil || !s.cfg.Enabled() {
		return
	}
	since, err := s.start(ctx)
	if err != nil {
		s.log.Error("falha ao marcar o início das notas fiscais", "err", err)
		return
	}
	if err := s.enqueue(ctx, since); err != nil {
		s.log.Error("falha ao enfileirar as notas fiscais", "err", err)
	}
	s.cancelRefunded(ctx)
	s.emitPending(ctx)
	s.followUp(ctx)
	s.sendMail(ctx)
}

// start marca, na primeira vez, desde quando as faturas pagas ganham nota.
func (s *Service) start(ctx context.Context) (time.Time, error) {
	var since time.Time
	err := s.db.QueryRow(ctx, `UPDATE fiscal_settings SET started_at = COALESCE(started_at, $1) RETURNING started_at`,
		s.now()).Scan(&since)
	return since, database.MapError(err)
}

// enqueue cria a nota das faturas pagas desde o início, pelo valor do
// serviço (a parcela do rastreador fica fora).
func (s *Service) enqueue(ctx context.Context, since time.Time) error {
	tag, err := s.db.Exec(ctx, `
		INSERT INTO fiscal_documents (invoice_id, customer_id, amount_cents, next_try_at)
		SELECT id, customer_id, service, $2 FROM (
			SELECT i.id, i.customer_id, i.amount_cents - COALESCE((SELECT sum(e.amount_cents) FROM equipment_installments e
				WHERE e.invoice_id = i.id), 0) AS service
			FROM invoices i
			WHERE i.status = 'PAID' AND i.paid_at >= $1 AND `+eligible+`
				AND NOT EXISTS (SELECT 1 FROM fiscal_documents f WHERE f.invoice_id = i.id)
			ORDER BY i.paid_at LIMIT 200) x
		WHERE service > 0
		ON CONFLICT (invoice_id) DO NOTHING`, since, s.now())
	if err != nil {
		return database.MapError(err)
	}
	if n := tag.RowsAffected(); n > 0 {
		s.log.Info("notas fiscais enfileiradas", "count", n)
	}
	return nil
}

// job é uma nota a emitir, com o tomador.
type job struct {
	id          uuid.UUID
	invoiceID   uuid.UUID
	attempts    int
	amountCents int
	description string
	due         time.Time
	name        string
	email       string
	phone       string
	document    string
	address     *Address
}

func (s *Service) emitPending(ctx context.Context) {
	rows, err := s.db.Query(ctx, `
		SELECT f.id, f.invoice_id, f.attempts, f.amount_cents, i.description, i.due_date,
			COALESCE(u.name, ''), COALESCE(u.email, ''), COALESCE(u.phone, ''), COALESCE(u.document, ''),
			a.street, a.number, a.complement, a.district, a.city, a.state, a.zip_code
		FROM fiscal_documents f JOIN invoices i ON i.id = f.invoice_id
		LEFT JOIN users u ON u.id = f.customer_id
		LEFT JOIN customer_addresses a ON a.customer_id = f.customer_id
		WHERE f.status = 'pending' AND f.next_try_at <= $1
		ORDER BY f.created_at LIMIT `+fmt.Sprint(batch), s.now())
	if err != nil {
		s.log.Error("falha ao listar as notas a emitir", "err", err)
		return
	}
	var jobs []job
	for rows.Next() {
		var j job
		var street, number, complement, district, city, state, zip *string
		if err := rows.Scan(&j.id, &j.invoiceID, &j.attempts, &j.amountCents, &j.description, &j.due, &j.name, &j.email,
			&j.phone, &j.document, &street, &number, &complement, &district, &city, &state, &zip); err != nil {
			rows.Close()
			s.log.Error("falha ao ler a nota a emitir", "err", err)
			return
		}
		if city != nil {
			j.address = &Address{Street: *street, Number: *number, Complement: *complement, District: *district, City: *city,
				State: *state, ZipCode: *zip}
		}
		jobs = append(jobs, j)
	}
	rows.Close()
	for _, j := range jobs {
		s.emit(ctx, j)
	}
}

// emit manda uma nota para a Notaas.
func (s *Service) emit(ctx context.Context, j job) {
	document := digits(j.document)
	taker := Taker{Name: strings.TrimSpace(j.name), Email: j.email, Phone: digits(j.phone), Address: j.address}
	switch len(document) {
	case 11:
		taker.CPF = document
	case 14:
		taker.CNPJ = document
	default:
		s.fail(ctx, j.id, "o cliente está sem CPF ou CNPJ: preencha o cadastro e emita de novo")
		return
	}
	if taker.Name == "" {
		s.fail(ctx, j.id, "o cliente está sem nome: preencha o cadastro e emita de novo")
		return
	}
	text := strings.TrimSpace(s.cfg.Description)
	if desc := strings.TrimSpace(j.description); desc != "" {
		text = strings.TrimSpace(text + " — " + desc)
	}
	id, err := s.api.Emit(ctx, EmitRequest{
		Reference: j.invoiceID.String(), Taker: taker, Code: s.cfg.ServiceCode, Text: text,
		Total: float64(j.amountCents) / 100, ISSRate: s.cfg.ISSRate, Month: j.due.Format("2006-01"),
	})
	if err != nil {
		s.emitFailed(ctx, j, err)
		return
	}
	if _, err := s.db.Exec(ctx, `UPDATE fiscal_documents SET status = 'processing', provider_id = $2, error = '',
		attempts = attempts + 1, next_try_at = $3, updated_at = NOW() WHERE id = $1`, j.id, id, s.now().Add(time.Minute)); err != nil {
		s.log.Error("nota fiscal enviada e não marcada (o webhook ou a consulta acertam)", "document", j.id, "notaas", id, "err", err)
		return
	}
	s.log.Info("nota fiscal enviada", "document", j.id, "invoice", j.invoiceID, "notaas", id)
}

// emitFailed decide o que fazer com a recusa: o limite do plano espera o
// mês seguinte; o problema temporário tenta de novo mais tarde; o resto
// (cadastro, configuração) espera a central.
func (s *Service) emitFailed(ctx context.Context, j job, err error) {
	var api *APIError
	switch {
	case errors.As(err, &api) && api.CreditLimit():
		now := s.now()
		next := time.Date(now.Year(), now.Month()+1, 1, 6, 0, 0, 0, now.Location())
		s.mark(ctx, j.id, StatusBlocked, "o limite de notas do plano da Notaas acabou neste mês: sai no mês que vem (ou mude o plano e emita de novo)", next)
		s.log.Warn("limite de notas fiscais do plano atingido", "document", j.id)
	case errors.As(err, &api) && !api.Temporary():
		s.fail(ctx, j.id, api.Message)
		s.log.Warn("nota fiscal recusada pela Notaas", "document", j.id, "status", api.Status, "code", api.Code, "err", api.Message)
	default:
		// Rede ou falha do lado deles: tenta de novo, cada vez mais espaçado.
		wait := time.Duration(min(j.attempts+1, 6)) * 5 * time.Minute
		if _, err := s.db.Exec(ctx, `UPDATE fiscal_documents SET attempts = attempts + 1, next_try_at = $2, error = $3,
			updated_at = NOW() WHERE id = $1`, j.id, s.now().Add(wait), "tentando de novo: "+trim(err.Error())); err != nil {
			s.log.Error("falha ao reagendar a nota fiscal", "document", j.id, "err", err)
		}
		s.log.Warn("nota fiscal não enviada (tenta de novo)", "document", j.id, "err", err)
	}
}

func (s *Service) fail(ctx context.Context, id uuid.UUID, reason string) {
	s.mark(ctx, id, StatusError, reason, s.now())
}

func (s *Service) mark(ctx context.Context, id uuid.UUID, status, reason string, next time.Time) {
	if _, err := s.db.Exec(ctx, `UPDATE fiscal_documents SET status = $2, error = $3, next_try_at = $4, updated_at = NOW()
		WHERE id = $1`, id, status, trim(reason), next); err != nil {
		s.log.Error("falha ao marcar a nota fiscal", "document", id, "status", status, "err", err)
	}
}

// followUp consulta as notas na prefeitura (e as em cancelamento).
func (s *Service) followUp(ctx context.Context) {
	rows, err := s.db.Query(ctx, `SELECT id, provider_id, status, attempts FROM fiscal_documents
		WHERE status IN ('processing', 'cancel_pending') AND provider_id <> '' AND next_try_at <= $1
		ORDER BY next_try_at LIMIT `+fmt.Sprint(batch), s.now())
	if err != nil {
		s.log.Error("falha ao listar as notas em andamento", "err", err)
		return
	}
	type pending struct {
		id         uuid.UUID
		providerID string
		status     string
		attempts   int
	}
	var list []pending
	for rows.Next() {
		var p pending
		if rows.Scan(&p.id, &p.providerID, &p.status, &p.attempts) == nil {
			list = append(list, p)
		}
	}
	rows.Close()
	for _, p := range list {
		st, err := s.api.Status(ctx, p.providerID)
		if err != nil {
			s.later(ctx, p.id, p.attempts)
			s.log.Warn("falha ao consultar a nota fiscal (tenta de novo)", "document", p.id, "err", err)
			continue
		}
		s.apply(ctx, p.id, p.status, p.attempts, st)
	}
}

// later reagenda a consulta, cada vez mais espaçada (até 30 minutos).
func (s *Service) later(ctx context.Context, id uuid.UUID, attempts int) {
	wait := time.Duration(min(attempts+1, 30)) * time.Minute
	if _, err := s.db.Exec(ctx, `UPDATE fiscal_documents SET attempts = attempts + 1, next_try_at = $2, updated_at = NOW()
		WHERE id = $1`, id, s.now().Add(wait)); err != nil {
		s.log.Error("falha ao reagendar a consulta da nota fiscal", "document", id, "err", err)
	}
}

// apply guarda o que a Notaas respondeu.
func (s *Service) apply(ctx context.Context, id uuid.UUID, current string, attempts int, st *Status) {
	var err error
	switch st.Status {
	case notaasIssued:
		if current == StatusCancelPending {
			// O cancelamento ainda está em análise.
			s.later(ctx, id, attempts)
			return
		}
		issued := s.now()
		if st.EmittedAt != nil {
			issued = *st.EmittedAt
		}
		_, err = s.db.Exec(ctx, `UPDATE fiscal_documents SET status = 'issued', number = $2, verification_code = $3,
			pdf_url = $4, xml_url = $5, error = '', issued_at = $6, updated_at = NOW() WHERE id = $1`,
			id, st.Number, st.VerificationCode, st.PDFURL, st.XMLURL, issued)
		s.log.Info("nota fiscal emitida", "document", id, "numero", st.Number)
	case notaasError:
		if current == StatusCancelPending {
			// O cancelamento foi recusado: a nota segue valendo.
			_, err = s.db.Exec(ctx, `UPDATE fiscal_documents SET status = 'issued', error = $2, updated_at = NOW()
				WHERE id = $1`, id, trim("o cancelamento foi recusado: "+st.Problem()))
			break
		}
		s.fail(ctx, id, cmp.Or(st.Problem(), "a prefeitura recusou a nota"))
	case notaasCancelled:
		_, err = s.db.Exec(ctx, `UPDATE fiscal_documents SET status = 'cancelled', cancelled_at = NOW(), error = '',
			updated_at = NOW() WHERE id = $1`, id)
	default: // queued, processing
		s.later(ctx, id, attempts)
	}
	if err != nil {
		s.log.Error("falha ao guardar a situação da nota fiscal", "document", id, "err", err)
	}
}

// cancelRefunded pede o cancelamento da nota de fatura estornada (que
// voltou a ficar em aberto ou foi cancelada); a que nem saiu, sai da fila.
func (s *Service) cancelRefunded(ctx context.Context) {
	if _, err := s.db.Exec(ctx, `UPDATE fiscal_documents f SET status = 'cancelled', cancelled_at = NOW(), updated_at = NOW()
		FROM invoices i WHERE i.id = f.invoice_id AND i.status <> 'PAID'
			AND f.status IN ('pending', 'error', 'blocked')`); err != nil {
		s.log.Error("falha ao tirar da fila as notas de faturas estornadas", "err", err)
	}
	rows, err := s.db.Query(ctx, `SELECT f.id, f.provider_id FROM fiscal_documents f JOIN invoices i ON i.id = f.invoice_id
		WHERE i.status <> 'PAID' AND f.status = 'issued' AND f.provider_id <> '' LIMIT `+fmt.Sprint(batch))
	if err != nil {
		s.log.Error("falha ao listar as notas de faturas estornadas", "err", err)
		return
	}
	type issued struct {
		id         uuid.UUID
		providerID string
	}
	var list []issued
	for rows.Next() {
		var d issued
		if rows.Scan(&d.id, &d.providerID) == nil {
			list = append(list, d)
		}
	}
	rows.Close()
	for _, d := range list {
		if err := s.api.Cancel(ctx, d.providerID, "Pagamento estornado ao cliente: o serviço não foi cobrado."); err != nil {
			s.log.Warn("falha ao pedir o cancelamento da nota fiscal (tenta de novo)", "document", d.id, "err", err)
			continue
		}
		if _, err := s.db.Exec(ctx, `UPDATE fiscal_documents SET status = 'cancel_pending', next_try_at = $2, updated_at = NOW()
			WHERE id = $1`, d.id, s.now().Add(time.Minute)); err != nil {
			s.log.Error("cancelamento pedido e não marcado", "document", d.id, "err", err)
		}
		s.log.Info("cancelamento da nota fiscal pedido (fatura estornada)", "document", d.id)
	}
}

// sendMail manda ao cliente a nota emitida (uma vez).
func (s *Service) sendMail(ctx context.Context) {
	if s.mailer == nil {
		return
	}
	rows, err := s.db.Query(ctx, `
		SELECT f.id, f.number, f.amount_cents, f.pdf_url, i.description, COALESCE(u.email, ''), COALESCE(u.name, '')
		FROM fiscal_documents f JOIN invoices i ON i.id = f.invoice_id JOIN users u ON u.id = f.customer_id
		WHERE f.status = 'issued' AND f.emailed_at IS NULL AND u.email <> ''
		ORDER BY f.issued_at LIMIT `+fmt.Sprint(batch))
	if err != nil {
		s.log.Error("falha ao listar as notas a mandar por e-mail", "err", err)
		return
	}
	var list []mail.FiscalDocument
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		var d mail.FiscalDocument
		if rows.Scan(&id, &d.Number, &d.AmountCents, &d.PDFURL, &d.Description, &d.To, &d.Name) == nil {
			list = append(list, d)
			ids = append(ids, id)
		}
	}
	rows.Close()
	for i, d := range list {
		if err := s.mailer.FiscalIssued(ctx, d); err != nil {
			s.log.Warn("nota fiscal não mandada por e-mail (tenta de novo)", "document", ids[i], "err", err)
			continue
		}
		if _, err := s.db.Exec(ctx, `UPDATE fiscal_documents SET emailed_at = NOW() WHERE id = $1`, ids[i]); err != nil {
			s.log.Error("nota fiscal mandada por e-mail e não marcada", "document", ids[i], "err", err)
		}
	}
}

func digits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func trim(s string) string {
	s = strings.TrimSpace(s)
	if len([]rune(s)) > 500 {
		return string([]rune(s)[:500])
	}
	return s
}
