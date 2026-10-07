// Package adjustment é o reajuste anual da mensalidade pelo IPCA: em 1º de
// julho, o IPCA acumulado nos 12 meses até maio (o último publicado) corrige
// as assinaturas com pelo menos 12 meses na data do reajuste (Lei
// 10.192/2001) cujo cliente aceitou o contrato com a cláusula; o cliente é
// avisado por e-mail 30 dias antes, e o preço novo vale para as faturas que
// vencem a partir de 1º de agosto. Índice zero ou negativo mantém o preço. O
// admin pode cancelar o reajuste do ano antes de a primeira fatura com o
// preço novo ser gerada.
package adjustment

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/mail"
)

const (
	// StartMonth é o mês do reajuste; o aviso sai no mês anterior.
	StartMonth = time.August
	// NoticeDays é a antecedência mínima do aviso.
	NoticeDays = 30
	// ContractSince é a primeira versão do contrato com a cláusula de
	// reajuste: só é reajustado quem a aceitou.
	ContractSince = 2
	// lastNoticeDay: passado o fim de agosto sem aviso, o ano fica sem
	// reajuste (o contrato fala em agosto).
	lastNoticeMonth = time.August
)

// Situações do reajuste do ano.
const (
	StatusNotified = "NOTIFIED"
	StatusApplied  = "APPLIED"
	StatusCanceled = "CANCELED"
	StatusNoChange = "NO_CHANGE"
)

// Error é uma recusa para a tela.
type Error struct{ Message string }

func (e Error) Error() string { return e.Message }

// Mailer manda os avisos (mail.PriceAdjustmentMailer).
type Mailer interface {
	Notice(ctx context.Context, to, name string, n mail.PriceAdjustmentNotice) error
	Canceled(ctx context.Context, to, name string, n mail.PriceAdjustmentNotice) error
	Summary(ctx context.Context, to string, s mail.PriceAdjustmentSummary) error
}

type Config struct {
	Enabled  bool
	Location *time.Location
	// InvoiceLeadDays: a fatura sai esses dias antes do vencimento; o
	// cancelamento vale até a primeira fatura com o preço novo ser gerada.
	InvoiceLeadDays int
	// Os avisos saem das FromHour às ToHour (horário local).
	FromHour, ToHour int
}

type Service struct {
	db     *database.DB
	source Source
	mailer Mailer
	cfg    Config
	now    func() time.Time
	log    *slog.Logger
	async  func(func())
}

func NewService(db *database.DB, source Source, mailer Mailer, cfg Config, log *slog.Logger) *Service {
	if cfg.Location == nil {
		cfg.Location = time.UTC
	}
	if cfg.ToHour == 0 {
		cfg.FromHour, cfg.ToHour = 9, 20
	}
	return &Service{db: db, source: source, mailer: mailer, cfg: cfg, now: time.Now,
		log: log.With("component", "adjustment"), async: func(f func()) { go f() }}
}

// SetClock troca o relógio (testes).
func (s *Service) SetClock(now func() time.Time) { s.now = now }

// SetSync manda os e-mails na hora (testes).
func (s *Service) SetSync() { s.async = func(f func()) { f() } }

func (s *Service) today() billing.Date { return billing.DateIn(s.now(), s.cfg.Location) }

// Calendário do ano.
func noticeDate(year int) billing.Date { return billing.NewDate(year, StartMonth-1, 1) }
func startDate(year int) billing.Date  { return billing.NewDate(year, StartMonth, 1) }

// period é o acumulado do ano: de junho do ano anterior a maio.
func period(year int) (first, last billing.Date) {
	return billing.NewDate(year-1, StartMonth-2, 1), billing.NewDate(year, StartMonth-3, 1)
}

var monthNames = [...]string{"janeiro", "fevereiro", "março", "abril", "maio", "junho", "julho", "agosto",
	"setembro", "outubro", "novembro", "dezembro"}

// periodLabel: "junho de 2026 a maio de 2027".
func periodLabel(first, last billing.Date) string {
	return fmt.Sprintf("%s de %d a %s de %d", monthNames[first.Month()-1], first.Year(), monthNames[last.Month()-1], last.Year())
}

// cancelUntil é o último dia para cancelar: antes de a primeira fatura com o
// preço novo ser gerada.
func (s *Service) cancelUntil(effective billing.Date) billing.Date {
	return effective.AddDays(-s.cfg.InvoiceLeadDays - 1)
}

// ---------------------------------------------------------------------------
// O trabalho de hora em hora
// ---------------------------------------------------------------------------

// Work aplica os reajustes que começaram, reenvia os avisos pendentes e, em
// julho, calcula e avisa o reajuste do ano.
func (s *Service) Work(ctx context.Context) {
	if !s.cfg.Enabled {
		return
	}
	today := s.today()
	if err := s.apply(ctx, today); err != nil {
		s.log.Error("falha ao aplicar o reajuste", "err", err)
	}
	hour := s.now().In(s.cfg.Location).Hour()
	if hour < s.cfg.FromHour || hour >= s.cfg.ToHour {
		return
	}
	if err := s.announce(ctx, today); err != nil {
		if errors.Is(err, ErrNotPublished) {
			s.log.Info("reajuste aguardando o IPCA", "motivo", err)
		} else {
			s.log.Error("falha no reajuste anual", "err", err)
		}
	}
	s.notifyPending(ctx)
}

// announce calcula o reajuste do ano e agenda os preços novos (uma vez por
// ano: a chave é o ano).
func (s *Service) announce(ctx context.Context, today billing.Date) error {
	year := today.Year()
	lastDay := billing.NewDate(year, lastNoticeMonth+1, 1).AddDays(-1)
	if today.Before(noticeDate(year)) || lastDay.Before(today) {
		return nil
	}
	var exists bool
	if err := s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM price_adjustments WHERE year = $1)`, year).Scan(&exists); err != nil {
		return database.MapError(err)
	}
	if exists {
		return nil
	}
	first, last := period(year)
	months, err := s.source.MonthlyIPCA(ctx, first, last)
	if err != nil {
		return err
	}
	rate := Accumulate(months)
	// O aviso vem 30 dias antes: atrasado, o reajuste começa depois.
	effective := startDate(year)
	if late := today.AddDays(NoticeDays); effective.Before(late) {
		effective = late
	}
	status := StatusNotified
	if rate <= 0 {
		status = StatusNoChange
	}
	var count, diff int
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			INSERT INTO price_adjustments (year, period_start, period_end, rate_millionths, effective_from, status, notified_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7) ON CONFLICT (year) DO NOTHING`,
			year, first.Time, last.Time, rate, effective.Time, status, s.now())
		if err != nil || tag.RowsAffected() == 0 {
			return err // outra instância chegou antes
		}
		if status == StatusNoChange {
			return nil
		}
		// Quem tem 12 meses na data do reajuste, fora da promoção e com o
		// contrato que prevê o reajuste.
		cutoff := time.Date(effective.Year()-1, effective.Month(), effective.Day()+1, 0, 0, 0, 0, s.cfg.Location)
		rows, err := tx.Query(ctx, `
			SELECT s.id, s.customer_id, s.price_cents FROM subscriptions s
			WHERE s.status = 'ACTIVE' AND s.next_price_cents IS NULL AND s.created_at < $1
				AND (s.promo_until IS NULL OR s.promo_until <= $2)
				AND EXISTS (SELECT 1 FROM contract_acceptances a WHERE a.user_id = s.customer_id
					AND CASE WHEN a.version ~ '^[0-9]+$' THEN a.version::int ELSE 0 END >= $3)
			ORDER BY s.created_at
			FOR UPDATE OF s`, cutoff, effective.Time, ContractSince)
		if err != nil {
			return err
		}
		type sub struct {
			id, customer uuid.UUID
			price        int
		}
		var subs []sub
		for rows.Next() {
			var x sub
			if err := rows.Scan(&x.id, &x.customer, &x.price); err != nil {
				rows.Close()
				return err
			}
			subs = append(subs, x)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, x := range subs {
			next := Adjust(x.price, rate)
			if next == x.price {
				continue
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO price_adjustment_items (year, subscription_id, customer_id, old_price_cents, new_price_cents)
				VALUES ($1, $2, $3, $4, $5)`, year, x.id, x.customer, x.price, next); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE subscriptions SET next_price_cents = $2, next_price_from = $3, updated_at = NOW()
				WHERE id = $1`, x.id, next, effective.Time); err != nil {
				return err
			}
			count++
			diff += next - x.price
		}
		_, err = tx.Exec(ctx, `UPDATE price_adjustments SET subscriptions = $2 WHERE year = $1`, year, count)
		return err
	})
	if err != nil {
		return database.MapError(err)
	}
	s.log.Info("reajuste anual calculado", "ano", year, "ipca", Percent(rate), "assinaturas", count, "vale_de", effective)
	customers := 0
	if status == StatusNotified {
		customers = s.notifyYear(ctx, year)
	}
	summary := mail.PriceAdjustmentSummary{
		Year: year, Rate: Percent(rate), Period: periodLabel(first, last), EffectiveFrom: effective.Time,
		CancelUntil: s.cancelUntil(effective).Time, Customers: customers, Subscriptions: count, MonthlyDiffCents: diff,
		NoChange: status == StatusNoChange,
	}
	s.async(func() { s.sendSummary(context.WithoutCancel(ctx), summary) })
	return nil
}

// notifyPending reenvia os avisos que não saíram (e-mail fora do ar).
func (s *Service) notifyPending(ctx context.Context) {
	rows, err := s.db.Query(ctx, `SELECT DISTINCT i.year FROM price_adjustment_items i JOIN price_adjustments a ON a.year = i.year
		WHERE a.status = 'NOTIFIED' AND NOT i.notified AND a.effective_from > $1`, s.today().Time)
	if err != nil {
		s.log.Error("falha ao listar avisos de reajuste pendentes", "err", err)
		return
	}
	var years []int
	for rows.Next() {
		var y int
		if rows.Scan(&y) == nil {
			years = append(years, y)
		}
	}
	rows.Close()
	for _, y := range years {
		s.notifyYear(ctx, y)
	}
}

// customerNotice é o aviso de um cliente, montado das linhas do ano.
type customerNotice struct {
	email, name string
	subs        []uuid.UUID
	notice      mail.PriceAdjustmentNotice
}

func (s *Service) notices(ctx context.Context, year int, onlyPending, onlyNotified bool) ([]*customerNotice, error) {
	rows, err := s.db.Query(ctx, `
		SELECT u.id, u.email, u.name, i.subscription_id, i.old_price_cents, i.new_price_cents,
			COALESCE(v.name, ''), sb.plan_name, a.rate_millionths, a.period_start, a.period_end, a.effective_from
		FROM price_adjustment_items i
		JOIN price_adjustments a ON a.year = i.year
		JOIN users u ON u.id = i.customer_id
		JOIN subscriptions sb ON sb.id = i.subscription_id
		LEFT JOIN vehicles v ON v.id = sb.vehicle_id
		WHERE i.year = $1 AND ($2 = FALSE OR NOT i.notified) AND ($3 = FALSE OR i.notified)
		ORDER BY u.id, v.name`, year, onlyPending, onlyNotified)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	byCustomer := map[uuid.UUID]*customerNotice{}
	var order []*customerNotice
	for rows.Next() {
		var id, sub uuid.UUID
		var email, name, vehicle, plan string
		var oldCents, newCents, rate int
		var first, last, from time.Time
		if err := rows.Scan(&id, &email, &name, &sub, &oldCents, &newCents, &vehicle, &plan, &rate, &first, &last, &from); err != nil {
			return nil, err
		}
		n, ok := byCustomer[id]
		if !ok {
			n = &customerNotice{email: email, name: name, notice: mail.PriceAdjustmentNotice{
				Year: year, Rate: Percent(rate), Period: periodLabel(billing.Date{Time: first}, billing.Date{Time: last}),
				EffectiveFrom: from,
			}}
			byCustomer[id] = n
			order = append(order, n)
		}
		n.subs = append(n.subs, sub)
		n.notice.Lines = append(n.notice.Lines, mail.PriceAdjustmentLine{Vehicle: vehicle, Plan: plan, OldCents: oldCents, NewCents: newCents})
	}
	return order, rows.Err()
}

// notifyYear avisa cada cliente (um e-mail com todas as mensalidades dele)
// e marca o que saiu. Devolve quantos foram avisados agora.
func (s *Service) notifyYear(ctx context.Context, year int) int {
	list, err := s.notices(ctx, year, true, false)
	if err != nil {
		s.log.Error("falha ao montar os avisos de reajuste", "ano", year, "err", err)
		return 0
	}
	sent := 0
	for _, n := range list {
		if err := s.mailer.Notice(ctx, n.email, n.name, n.notice); err != nil {
			s.log.Warn("aviso de reajuste não enviado (tenta de novo)", "email", n.email, "err", err)
			continue
		}
		if _, err := s.db.Exec(ctx, `UPDATE price_adjustment_items SET notified = TRUE WHERE year = $1 AND subscription_id = ANY($2)`,
			year, n.subs); err != nil {
			s.log.Error("aviso de reajuste enviado e não marcado", "email", n.email, "err", err)
			continue
		}
		sent++
	}
	return sent
}

func (s *Service) sendSummary(ctx context.Context, summary mail.PriceAdjustmentSummary) {
	rows, err := s.db.Query(ctx, `SELECT email FROM users WHERE role = 'admin' AND active ORDER BY created_at`)
	if err != nil {
		s.log.Error("falha ao listar os admins para o resumo do reajuste", "err", err)
		return
	}
	var emails []string
	for rows.Next() {
		var e string
		if rows.Scan(&e) == nil {
			emails = append(emails, e)
		}
	}
	rows.Close()
	for _, e := range emails {
		if err := s.mailer.Summary(ctx, e, summary); err != nil {
			s.log.Warn("resumo do reajuste não enviado", "email", e, "err", err)
		}
	}
}

// apply põe em vigor os reajustes que já começaram: o preço agendado vira o
// preço da assinatura (as faturas desde a data já saíram com ele).
func (s *Service) apply(ctx context.Context, today billing.Date) error {
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			UPDATE subscriptions s SET price_cents = s.next_price_cents, next_price_cents = NULL, next_price_from = NULL,
				updated_at = NOW()
			FROM price_adjustment_items i JOIN price_adjustments a ON a.year = i.year
			WHERE i.subscription_id = s.id AND a.status = 'NOTIFIED' AND a.effective_from <= $1
				AND s.next_price_from = a.effective_from AND s.next_price_cents = i.new_price_cents`, today.Time); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE price_adjustments SET status = 'APPLIED', applied_at = NOW()
			WHERE status = 'NOTIFIED' AND effective_from <= $1`, today.Time)
		return err
	})
}

// ---------------------------------------------------------------------------
// Admin: a visão e o cancelamento
// ---------------------------------------------------------------------------

// Item é uma mensalidade reajustada.
type Item struct {
	SubscriptionID uuid.UUID `json:"subscriptionId"`
	CustomerID     uuid.UUID `json:"customerId"`
	CustomerName   string    `json:"customerName"`
	Vehicle        string    `json:"vehicle"`
	Plan           string    `json:"plan"`
	OldPriceCents  int       `json:"oldPriceCents"`
	NewPriceCents  int       `json:"newPriceCents"`
	Notified       bool      `json:"notified"`
}

// Adjustment é o reajuste de um ano.
type Adjustment struct {
	Year           int          `json:"year"`
	Period         string       `json:"period"`
	Rate           string       `json:"rate"`
	RateMillionths int          `json:"rateMillionths"`
	EffectiveFrom  billing.Date `json:"effectiveFrom"`
	Status         string       `json:"status"`
	Subscriptions  int          `json:"subscriptions"`
	NotifiedAt     time.Time    `json:"notifiedAt"`
	CanceledAt     *time.Time   `json:"canceledAt"`
	// CancelUntil: até quando dá para cancelar (CanCancel diz se ainda dá).
	CancelUntil      billing.Date `json:"cancelUntil"`
	CanCancel        bool         `json:"canCancel"`
	MonthlyDiffCents int          `json:"monthlyDiffCents"`
	Items            []Item       `json:"items"`
}

// Upcoming é o próximo reajuste ainda não calculado.
type Upcoming struct {
	Year       int          `json:"year"`
	NoticeDate billing.Date `json:"noticeDate"`
	StartDate  billing.Date `json:"startDate"`
	Period     string       `json:"period"`
}

// Overview é o que o painel mostra.
type Overview struct {
	Enabled     bool         `json:"enabled"`
	Upcoming    *Upcoming    `json:"upcoming"`
	Adjustments []Adjustment `json:"adjustments"`
}

// Overview: o próximo reajuste e os dos últimos anos.
func (s *Service) Overview(ctx context.Context) (*Overview, error) {
	today := s.today()
	out := &Overview{Enabled: s.cfg.Enabled, Adjustments: []Adjustment{}}
	rows, err := s.db.Query(ctx, `
		SELECT year, period_start, period_end, rate_millionths, effective_from, status, subscriptions, notified_at, canceled_at
		FROM price_adjustments ORDER BY year DESC LIMIT 5`)
	if err != nil {
		return nil, database.MapError(err)
	}
	for rows.Next() {
		var a Adjustment
		var first, last, from time.Time
		if err := rows.Scan(&a.Year, &first, &last, &a.RateMillionths, &from, &a.Status, &a.Subscriptions, &a.NotifiedAt, &a.CanceledAt); err != nil {
			rows.Close()
			return nil, err
		}
		a.Period = periodLabel(billing.Date{Time: first}, billing.Date{Time: last})
		a.Rate = Percent(a.RateMillionths)
		a.EffectiveFrom = billing.Date{Time: from}
		a.CancelUntil = s.cancelUntil(a.EffectiveFrom)
		a.CanCancel = a.Status == StatusNotified && !a.CancelUntil.Before(today)
		a.Items = []Item{}
		out.Adjustments = append(out.Adjustments, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out.Adjustments {
		a := &out.Adjustments[i]
		items, err := s.db.Query(ctx, `
			SELECT i.subscription_id, i.customer_id, u.name, COALESCE(v.name, ''), sb.plan_name,
				i.old_price_cents, i.new_price_cents, i.notified
			FROM price_adjustment_items i
			JOIN users u ON u.id = i.customer_id
			JOIN subscriptions sb ON sb.id = i.subscription_id
			LEFT JOIN vehicles v ON v.id = sb.vehicle_id
			WHERE i.year = $1 ORDER BY u.name, v.name`, a.Year)
		if err != nil {
			return nil, database.MapError(err)
		}
		for items.Next() {
			var it Item
			if err := items.Scan(&it.SubscriptionID, &it.CustomerID, &it.CustomerName, &it.Vehicle, &it.Plan,
				&it.OldPriceCents, &it.NewPriceCents, &it.Notified); err != nil {
				items.Close()
				return nil, err
			}
			a.MonthlyDiffCents += it.NewPriceCents - it.OldPriceCents
			a.Items = append(a.Items, it)
		}
		items.Close()
	}
	// O próximo: o deste ano, se ainda não saiu (até o fim de agosto); senão o
	// do ano que vem.
	year := today.Year()
	done := false
	for _, a := range out.Adjustments {
		done = done || a.Year == year
	}
	if done || billing.NewDate(year, lastNoticeMonth+1, 1).AddDays(-1).Before(today) {
		year++
	}
	first, last := period(year)
	out.Upcoming = &Upcoming{Year: year, NoticeDate: noticeDate(year), StartDate: startDate(year), Period: periodLabel(first, last)}
	return out, nil
}

// Cancel desiste do reajuste do ano: os preços agendados saem e os clientes
// avisados recebem o cancelamento. Só antes de a primeira fatura com o preço
// novo ser gerada.
func (s *Service) Cancel(ctx context.Context, year int, by uuid.UUID) error {
	today := s.today()
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var status string
		var from time.Time
		err := tx.QueryRow(ctx, `SELECT status, effective_from FROM price_adjustments WHERE year = $1 FOR UPDATE`, year).Scan(&status, &from)
		if errors.Is(err, pgx.ErrNoRows) {
			return database.ErrNotFound
		}
		if err != nil {
			return err
		}
		if status != StatusNotified {
			return Error{"Este reajuste não está agendado."}
		}
		if until := s.cancelUntil(billing.Date{Time: from}); until.Before(today) {
			return Error{fmt.Sprintf("Dava para cancelar até %s: as faturas com o preço novo já começaram a sair.",
				until.Format("02/01/2006"))}
		}
		if _, err := tx.Exec(ctx, `
			UPDATE subscriptions s SET next_price_cents = NULL, next_price_from = NULL, updated_at = NOW()
			FROM price_adjustment_items i
			WHERE i.year = $1 AND i.subscription_id = s.id AND s.next_price_from = $2`, year, from); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE price_adjustments SET status = 'CANCELED', canceled_at = NOW(), canceled_by = $2 WHERE year = $1`, year, by)
		return err
	})
	if err != nil {
		var e Error
		if errors.As(err, &e) {
			return e
		}
		return database.MapError(err)
	}
	s.log.Info("reajuste anual cancelado", "ano", year, "por", by)
	s.async(func() {
		ctx := context.WithoutCancel(ctx)
		list, err := s.notices(ctx, year, false, true)
		if err != nil {
			s.log.Error("falha ao montar os avisos de cancelamento", "ano", year, "err", err)
			return
		}
		for _, n := range list {
			if err := s.mailer.Canceled(ctx, n.email, n.name, n.notice); err != nil {
				s.log.Warn("aviso de cancelamento do reajuste não enviado", "email", n.email, "err", err)
			}
		}
	})
	return nil
}
