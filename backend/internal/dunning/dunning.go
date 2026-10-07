// Package dunning é a régua de cobrança: os lembretes de fatura por e-mail e
// no celular — quando a fatura é gerada, 3 dias antes, no dia, 3 dias depois
// e perto da suspensão —, cada um com o link que abre o Pix da fatura sem
// login. Cada etapa sai uma vez por fatura e para assim que ela é paga; a
// central pode mandar um lembrete a mais quando quiser.
package dunning

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"log/slog"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/mail"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/push"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/webpush"
)

// As etapas da régua.
const (
	KindIssued         = mail.ReminderIssued         // a fatura foi gerada
	KindDueSoon        = mail.ReminderDueSoon        // vence em até 3 dias
	KindDueToday       = mail.ReminderDueToday       // vence hoje
	KindOverdue        = mail.ReminderOverdue        // venceu há 3 dias
	KindSuspensionSoon = mail.ReminderSuspensionSoon // o acesso é suspenso em 2 dias
	KindManual         = mail.ReminderManual         // pedido pela central
)

// ErrNotOpen: a fatura não está em aberto (paga ou cancelada).
var ErrNotOpen = errors.New("a fatura não está em aberto")

// Stage é a etapa da régua para uma fatura que vence daqui a -daysLate dias
// (ou venceu há daysLate); vazio, nenhuma. O acesso é suspenso quando o
// atraso passa de suspendAfter dias (0: nunca). Como as etapas só avançam,
// quem perdeu uma (o servidor fora do ar, a fatura criada já perto do
// vencimento) recebe só a do momento.
func Stage(daysLate, suspendAfter int) string {
	switch {
	case daysLate <= -4:
		return KindIssued
	case daysLate < 0:
		return KindDueSoon
	case daysLate == 0:
		return KindDueToday
	case suspendAfter > 0 && daysLate > suspendAfter:
		return "" // já suspenso: o aviso está no painel e no app
	case suspendAfter > 0 && daysLate >= suspendAfter-1:
		return KindSuspensionSoon
	case daysLate >= 3:
		return KindOverdue
	}
	return ""
}

// Config é a régua.
type Config struct {
	// SuspendAfterDays: o atraso que suspende o acesso (BILLING_SUSPEND_AFTER_DAYS).
	SuspendAfterDays int
	// Location é o fuso da cobrança; os lembretes saem entre FromHour e
	// ToHour (hora local), não de madrugada.
	Location *time.Location
	FromHour int
	ToHour   int
	// AppURL monta o link de pagamento; LinkSecret o assina.
	AppURL     string
	LinkSecret []byte
}

// Mailer manda o e-mail (mail.InvoiceMailer).
type Mailer interface {
	Reminder(ctx context.Context, to, name string, r mail.InvoiceReminder) error
}

// Pusher avisa no celular (push.Service). Opcional.
type Pusher interface {
	Notify(ctx context.Context, userID uuid.UUID, n push.Notification) (int, error)
}

// Invoices lê as faturas (billing.Service).
type Invoices interface {
	GetInvoice(ctx context.Context, id uuid.UUID) (*billing.Invoice, error)
	Today() billing.Date
}

// Reminder é um lembrete que saiu.
type Reminder struct {
	InvoiceID uuid.UUID  `json:"invoiceId"`
	Kind      string     `json:"kind"`
	SentBy    *uuid.UUID `json:"-"`
	Emailed   bool       `json:"emailed"`
	Pushed    int        `json:"pushed"`
	CreatedAt time.Time  `json:"createdAt"`
}

type Service struct {
	db       *database.DB
	invoices Invoices
	mailer   Mailer
	pusher   Pusher
	cfg      Config
	key      []byte
	now      func() time.Time
	log      *slog.Logger
}

func NewService(db *database.DB, invoices Invoices, mailer Mailer, cfg Config, log *slog.Logger) *Service {
	if cfg.Location == nil {
		cfg.Location = time.UTC
	}
	if cfg.ToHour <= cfg.FromHour {
		cfg.FromHour, cfg.ToHour = 9, 20
	}
	cfg.AppURL = strings.TrimRight(cfg.AppURL, "/")
	mac := hmac.New(sha256.New, cfg.LinkSecret)
	mac.Write([]byte("farbo-invoice-link-v1"))
	return &Service{db: db, invoices: invoices, mailer: mailer, cfg: cfg, key: mac.Sum(nil), now: time.Now,
		log: log.With("component", "dunning")}
}

// SetPusher liga o lembrete no celular.
func (s *Service) SetPusher(p Pusher) { s.pusher = p }

// SetClock troca o relógio (testes).
func (s *Service) SetClock(now func() time.Time) { s.now = now }

// ---------------------------------------------------------------------------
// O link de pagamento (sem login)
// ---------------------------------------------------------------------------

var linkEncoding = base64.RawURLEncoding

// Token é a parte secreta do link: o id da fatura e a assinatura. Só paga
// aquela fatura; nada secreto fica no banco.
func (s *Service) Token(invoiceID uuid.UUID) string {
	mac := hmac.New(sha256.New, s.key)
	mac.Write(invoiceID[:])
	return linkEncoding.EncodeToString(append(invoiceID[:], mac.Sum(nil)[:16]...))
}

// PayURL é o link que abre o Pix da fatura.
func (s *Service) PayURL(invoiceID uuid.UUID) string {
	return s.cfg.AppURL + "/pagar/" + s.Token(invoiceID)
}

// InvoiceID confere o link e devolve a fatura dele.
func (s *Service) InvoiceID(token string) (uuid.UUID, bool) {
	token = strings.TrimSpace(token)
	raw, err := linkEncoding.DecodeString(token)
	if err != nil || len(raw) != 32 {
		return uuid.Nil, false
	}
	id, err := uuid.FromBytes(raw[:16])
	if err != nil || !hmac.Equal([]byte(s.Token(id)), []byte(token)) {
		return uuid.Nil, false
	}
	return id, true
}

// ---------------------------------------------------------------------------
// A rotina
// ---------------------------------------------------------------------------

// claimed é uma fatura cujo lembrete de hoje acabou de ser reservado.
type claimed struct {
	reminderID int64
	invoice    *billing.Invoice
	daysLate   int
}

// Work manda os lembretes do dia (fora do horário, não faz nada). Cada
// etapa é reservada no banco antes de sair: duas instâncias, ou duas voltas
// da rotina, não mandam o mesmo lembrete duas vezes.
func (s *Service) Work(ctx context.Context) int {
	local := s.now().In(s.cfg.Location)
	if h := local.Hour(); h < s.cfg.FromHour || h >= s.cfg.ToHour {
		return 0
	}
	today := s.invoices.Today()
	oldest := today.AddDays(-(s.cfg.SuspendAfterDays + 1))
	if s.cfg.SuspendAfterDays <= 0 {
		oldest = today.AddDays(-60)
	}
	rows, err := s.db.Query(ctx, `
		SELECT i.id FROM invoices i JOIN users u ON u.id = i.customer_id
		WHERE i.status = 'OPEN' AND i.amount_cents > 0 AND u.active AND i.due_date >= $1
		ORDER BY i.due_date`, oldest.Time)
	if err != nil {
		s.log.Error("falha ao listar as faturas da régua", "err", err)
		return 0
	}
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			s.log.Error("falha ao listar as faturas da régua", "err", err)
			return 0
		}
		ids = append(ids, id)
	}
	rows.Close()

	// Por cliente e etapa: duas faturas que vencem juntas viram um lembrete só.
	type group struct {
		customerID uuid.UUID
		kind       string
	}
	batches := map[group][]claimed{}
	var order []group
	for _, id := range ids {
		inv, err := s.invoices.GetInvoice(ctx, id)
		if err != nil || inv.Status != billing.InvoiceOpen {
			continue
		}
		late := daysBetween(inv.DueDate, today)
		kind := Stage(late, s.cfg.SuspendAfterDays)
		if kind == "" {
			continue
		}
		reminderID, ok, err := s.claim(ctx, inv.ID, kind, nil)
		if err != nil {
			s.log.Error("falha ao reservar o lembrete", "invoice", inv.ID, "kind", kind, "err", err)
			continue
		}
		if !ok {
			continue
		}
		g := group{inv.CustomerID, kind}
		if _, seen := batches[g]; !seen {
			order = append(order, g)
		}
		batches[g] = append(batches[g], claimed{reminderID: reminderID, invoice: inv, daysLate: late})
	}
	sent := 0
	for _, g := range order {
		s.deliver(ctx, g.customerID, g.kind, batches[g])
		sent += len(batches[g])
	}
	if sent > 0 {
		s.log.Info("lembretes de fatura enviados", "invoices", sent, "messages", len(order))
	}
	return sent
}

// SendNow manda um lembrete da fatura agora (a central pediu).
func (s *Service) SendNow(ctx context.Context, invoiceID uuid.UUID, actor *uuid.UUID) (*Reminder, error) {
	inv, err := s.invoices.GetInvoice(ctx, invoiceID)
	if err != nil {
		return nil, err
	}
	if inv.Status != billing.InvoiceOpen {
		return nil, ErrNotOpen
	}
	reminderID, _, err := s.claim(ctx, inv.ID, KindManual, actor)
	if err != nil {
		return nil, err
	}
	late := daysBetween(inv.DueDate, s.invoices.Today())
	s.deliver(ctx, inv.CustomerID, KindManual, []claimed{{reminderID: reminderID, invoice: inv, daysLate: late}})
	last, err := s.LastByInvoice(ctx, []uuid.UUID{inv.ID})
	if err != nil {
		return nil, err
	}
	r := last[inv.ID]
	return &r, nil
}

// claim reserva o lembrete; false se a etapa já tinha saído.
func (s *Service) claim(ctx context.Context, invoiceID uuid.UUID, kind string, actor *uuid.UUID) (int64, bool, error) {
	var id int64
	err := s.db.QueryRow(ctx, `
		INSERT INTO invoice_reminders (invoice_id, kind, sent_by) VALUES ($1, $2, $3)
		ON CONFLICT (invoice_id, kind) WHERE kind <> 'MANUAL' DO NOTHING
		RETURNING id`, invoiceID, kind, actor).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	return id, err == nil, database.MapError(err)
}

// deliver manda o e-mail e o push de um grupo de faturas do mesmo cliente.
func (s *Service) deliver(ctx context.Context, customerID uuid.UUID, kind string, items []claimed) {
	var name, email string
	if err := s.db.QueryRow(ctx, `SELECT name, email FROM users WHERE id = $1`, customerID).Scan(&name, &email); err != nil {
		s.log.Error("falha ao ler o cliente do lembrete", "customer", customerID, "err", err)
		return
	}
	sort.Slice(items, func(i, j int) bool { return items[i].invoice.DueDate.Before(items[j].invoice.DueDate) })
	r := mail.InvoiceReminder{Kind: kind}
	for _, it := range items {
		r.Invoices = append(r.Invoices, mail.InvoiceLine{
			Description: it.invoice.Description, AmountCents: it.invoice.AmountCents,
			DueDate: it.invoice.DueDate.Format("02/01/2006"), PayURL: s.PayURL(it.invoice.ID),
		})
		if it.daysLate > r.DaysLate {
			r.DaysLate = it.daysLate
		}
		if -it.daysLate > r.DaysToDue {
			r.DaysToDue = -it.daysLate
		}
	}
	if r.DaysToDue > 0 && r.DaysLate == 0 {
		// A que vence primeiro é a que importa.
		r.DaysToDue = -items[0].daysLate
	}
	if s.cfg.SuspendAfterDays > 0 && r.DaysLate > 0 {
		r.SuspendIn = s.cfg.SuspendAfterDays + 1 - r.DaysLate
		r.SuspendOn = s.invoices.Today().AddDays(r.SuspendIn).Format("02/01/2006")
	}

	emailed := false
	if strings.TrimSpace(email) != "" {
		if err := s.mailer.Reminder(ctx, email, name, r); err != nil {
			s.log.Warn("falha no e-mail do lembrete de fatura", "customer", customerID, "kind", kind, "err", err)
		} else {
			emailed = true
		}
	}
	pushed := 0
	if s.pusher != nil {
		title, body := r.PushText()
		url := "/app/faturas"
		if len(items) == 1 {
			url += "?pagar=" + items[0].invoice.ID.String()
		}
		n := push.Notification{
			Title: title, Body: body, URL: url, Tag: "fatura:" + kind, Severity: mail.SeverityWarning,
			Urgency: webpush.UrgencyNormal, TTL: 24 * time.Hour,
		}
		if kind == KindSuspensionSoon || kind == KindOverdue {
			n.Severity = mail.SeverityCritical
		}
		if count, err := s.pusher.Notify(ctx, customerID, n); err != nil {
			s.log.Warn("falha no push do lembrete de fatura", "customer", customerID, "kind", kind, "err", err)
		} else {
			pushed = count
		}
	}
	for _, it := range items {
		if _, err := s.db.Exec(ctx, `UPDATE invoice_reminders SET emailed = $2, pushed = $3 WHERE id = $1`,
			it.reminderID, emailed, pushed); err != nil {
			s.log.Error("falha ao gravar o lembrete", "invoice", it.invoice.ID, "err", err)
		}
	}
}

// LastByInvoice: o último lembrete de cada fatura (a ficha do cliente).
func (s *Service) LastByInvoice(ctx context.Context, invoiceIDs []uuid.UUID) (map[uuid.UUID]Reminder, error) {
	out := map[uuid.UUID]Reminder{}
	if len(invoiceIDs) == 0 {
		return out, nil
	}
	rows, err := s.db.Query(ctx, `
		SELECT DISTINCT ON (invoice_id) invoice_id, kind, sent_by, emailed, pushed, created_at
		FROM invoice_reminders WHERE invoice_id = ANY($1)
		ORDER BY invoice_id, created_at DESC, id DESC`, invoiceIDs)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var r Reminder
		if err := rows.Scan(&r.InvoiceID, &r.Kind, &r.SentBy, &r.Emailed, &r.Pushed, &r.CreatedAt); err != nil {
			return nil, database.MapError(err)
		}
		out[r.InvoiceID] = r
	}
	return out, database.MapError(rows.Err())
}

// daysBetween: quantos dias do vencimento até hoje (negativo: ainda vai vencer).
func daysBetween(due, today billing.Date) int {
	return int(math.Round(today.Time.Sub(due.Time).Hours() / 24))
}
