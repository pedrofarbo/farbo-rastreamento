// Package reportmail manda o relatório da frota por e-mail, toda semana (às
// segundas, com a semana anterior) ou todo mês (no dia 1º, com o mês
// anterior): os totais e os veículos que mais rodaram no corpo, e as
// planilhas do resumo e dos excessos anexas.
//
// Cada pessoa da conta agenda os seus, para si e, se puder mexer na conta,
// para mais alguns e-mails. O envio usa o acesso dela na hora: quem está
// restrito a grupos só recebe os veículos deles, e quem saiu da conta deixa
// de receber (o agendamento é apagado).
package reportmail

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	netmail "net/mail"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/mail"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/reports"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/vehicles"
)

// As frequências.
const (
	Weekly  = "weekly"
	Monthly = "monthly"
)

const (
	// MaxPerUser é quantos agendamentos cada pessoa pode ter.
	MaxPerUser = 5
	// MaxExtraRecipients é quantos e-mails a mais cada agendamento leva.
	MaxExtraRecipients = 5
	// sendHour: o relatório sai a partir desta hora (no fuso da central) do
	// primeiro dia depois do período.
	sendHour = 7
	// checkEvery é o ritmo da procura pelos que estão na hora.
	checkEvery = 15 * time.Minute
	// topVehicles é quantos veículos o corpo do e-mail lista.
	topVehicles = 10
)

// Schedule é um agendamento.
type Schedule struct {
	ID              uuid.UUID  `json:"id"`
	Frequency       string     `json:"frequency"`
	GroupID         *uuid.UUID `json:"groupId"`
	ExtraRecipients []string   `json:"extraRecipients"`
	LastSentAt      *time.Time `json:"lastSentAt"`
	CreatedAt       time.Time  `json:"createdAt"`

	accountID  uuid.UUID
	userID     uuid.UUID
	lastPeriod *time.Time
}

// Input é o pedido de um agendamento.
type Input struct {
	Frequency       string     `json:"frequency"`
	GroupID         *uuid.UUID `json:"groupId"`
	ExtraRecipients []string   `json:"extraRecipients"`
}

// Person é quem agenda: a conta que enxerga e o acesso dele.
type Person struct {
	UserID    uuid.UUID
	AccountID uuid.UUID
	// Restricted: só os veículos dos grupos em Groups.
	Restricted bool
	Groups     []uuid.UUID
	// CanAddRecipients: o titular e o Gestor mandam para mais e-mails; o
	// Visualizador, só para si.
	CanAddRecipients bool
}

// InputError é um pedido recusado; a mensagem vai para a tela.
type InputError struct{ Message string }

func (e *InputError) Error() string { return e.Message }

// Mailer manda o e-mail do relatório (mail.ReportMailer).
type Mailer interface {
	FleetReport(ctx context.Context, r mail.FleetReport) error
}

type Service struct {
	db        *database.DB
	reports   *reports.Service
	vehicles  *vehicles.Service
	mailer    Mailer
	suspended func(ctx context.Context, customerID uuid.UUID) (bool, error)
	log       *slog.Logger
	now       func() time.Time
}

func NewService(db *database.DB, reportsSvc *reports.Service, vehicleSvc *vehicles.Service, mailer Mailer,
	suspended func(ctx context.Context, customerID uuid.UUID) (bool, error), log *slog.Logger) *Service {
	return &Service{db: db, reports: reportsSvc, vehicles: vehicleSvc, mailer: mailer, suspended: suspended,
		log: log.With("component", "reportmail"), now: time.Now}
}

// SetClock troca o relógio (testes).
func (s *Service) SetClock(now func() time.Time) { s.now = now }

const columns = `id, account_id, user_id, frequency, group_id, extra_recipients, last_period, last_sent_at, created_at`

func scan(row database.Scanner) (*Schedule, error) {
	var sc Schedule
	if err := row.Scan(&sc.ID, &sc.accountID, &sc.userID, &sc.Frequency, &sc.GroupID, &sc.ExtraRecipients,
		&sc.lastPeriod, &sc.LastSentAt, &sc.CreatedAt); err != nil {
		return nil, database.MapError(err)
	}
	if sc.ExtraRecipients == nil {
		sc.ExtraRecipients = []string{}
	}
	return &sc, nil
}

func (s *Service) list(ctx context.Context, query string, args ...any) ([]*Schedule, error) {
	rows, err := s.db.Query(ctx, `SELECT `+columns+` FROM report_schedules `+query, args...)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	out := []*Schedule{}
	for rows.Next() {
		sc, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sc)
	}
	return out, database.MapError(rows.Err())
}

// List são os agendamentos da pessoa.
func (s *Service) List(ctx context.Context, userID uuid.UUID) ([]*Schedule, error) {
	return s.list(ctx, `WHERE user_id = $1 ORDER BY created_at`, userID)
}

// Create agenda um relatório. O primeiro sai na próxima procura, com o
// período que acabou de fechar.
func (s *Service) Create(ctx context.Context, p Person, in Input) (*Schedule, error) {
	if in.Frequency != Weekly && in.Frequency != Monthly {
		return nil, &InputError{"escolha a frequência: toda semana ou todo mês"}
	}
	if in.GroupID != nil {
		var owned bool
		if err := s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM vehicle_groups WHERE id = $1 AND owner_id = $2)`,
			*in.GroupID, p.AccountID).Scan(&owned); err != nil {
			return nil, database.MapError(err)
		}
		if !owned {
			return nil, &InputError{"o grupo escolhido não existe mais nesta conta"}
		}
	}
	if p.Restricted && (in.GroupID == nil || !slices.Contains(p.Groups, *in.GroupID)) {
		return nil, &InputError{"escolha um dos seus grupos"}
	}
	extras, err := normalizeEmails(in.ExtraRecipients)
	if err != nil {
		return nil, err
	}
	if len(extras) > 0 && !p.CanAddRecipients {
		return nil, &InputError{"seu acesso a esta conta é só para acompanhar: o relatório vai só para o seu e-mail"}
	}
	var count int
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM report_schedules WHERE user_id = $1`, p.UserID).Scan(&count); err != nil {
		return nil, database.MapError(err)
	}
	if count >= MaxPerUser {
		return nil, &InputError{fmt.Sprintf("até %d relatórios agendados por pessoa: apague um para criar outro", MaxPerUser)}
	}
	return scan(s.db.QueryRow(ctx, `
		INSERT INTO report_schedules (account_id, user_id, frequency, group_id, extra_recipients)
		VALUES ($1, $2, $3, $4, $5) RETURNING `+columns, p.AccountID, p.UserID, in.Frequency, in.GroupID, extras))
}

// Delete apaga um agendamento da pessoa.
func (s *Service) Delete(ctx context.Context, userID, id uuid.UUID) error {
	tag, err := s.db.Exec(ctx, `DELETE FROM report_schedules WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return database.ErrNotFound
	}
	return nil
}

func normalizeEmails(emails []string) ([]string, error) {
	out := []string{}
	for _, raw := range emails {
		email := strings.ToLower(strings.TrimSpace(raw))
		if email == "" {
			continue
		}
		if addr, err := netmail.ParseAddress(email); err != nil || addr.Address != email || len(email) > 254 {
			return nil, &InputError{"e-mail inválido: " + raw}
		}
		if !slices.Contains(out, email) {
			out = append(out, email)
		}
	}
	if len(out) > MaxExtraRecipients {
		return nil, &InputError{fmt.Sprintf("até %d e-mails a mais por relatório", MaxExtraRecipients)}
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Envio
// ---------------------------------------------------------------------------

// Period é o período que o relatório cobre: os dias de First a Last.
type Period struct {
	First, Last time.Time
}

// latest é o último período fechado da frequência e quando ele pode sair
// (sendHour do dia seguinte ao fim).
func latest(frequency string, now time.Time, loc *time.Location) (Period, time.Time) {
	local := now.In(loc)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	var next time.Time // o primeiro dia depois do período
	if frequency == Monthly {
		next = time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, loc)
		return Period{First: next.AddDate(0, -1, 0), Last: next.AddDate(0, 0, -1)}, next.Add(sendHour * time.Hour)
	}
	next = today.AddDate(0, 0, -((int(today.Weekday()) + 6) % 7)) // a segunda-feira desta semana
	return Period{First: next.AddDate(0, 0, -7), Last: next.AddDate(0, 0, -1)}, next.Add(sendHour * time.Hour)
}

// Run procura, a cada checkEvery, os relatórios na hora de sair.
func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(checkEvery)
	defer ticker.Stop()
	for {
		if n, err := s.Round(ctx); err != nil && ctx.Err() == nil {
			s.log.Error("falha ao mandar os relatórios agendados", "err", err)
		} else if n > 0 {
			s.log.Info("relatórios da frota enviados", "relatorios", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Round manda os relatórios cujo período fechou e devolve quantos saíram.
// Cada um é reservado antes (last_period): duas instâncias não mandam o
// mesmo; se o envio falhar, a reserva volta e a próxima procura tenta de
// novo.
func (s *Service) Round(ctx context.Context) (int, error) {
	now := s.now()
	loc := s.reports.Location()
	sent := 0
	for _, frequency := range []string{Weekly, Monthly} {
		period, sendAt := latest(frequency, now, loc)
		if now.Before(sendAt) {
			continue
		}
		due, err := s.list(ctx, `WHERE frequency = $1 AND (last_period IS NULL OR last_period < $2) ORDER BY created_at`,
			frequency, period.First)
		if err != nil {
			return sent, err
		}
		for _, sc := range due {
			tag, err := s.db.Exec(ctx, `
				UPDATE report_schedules SET last_period = $2 WHERE id = $1 AND (last_period IS NULL OR last_period < $2)`,
				sc.ID, period.First)
			if err != nil {
				return sent, database.MapError(err)
			}
			if tag.RowsAffected() == 0 {
				continue // outra instância pegou
			}
			delivered, err := s.send(ctx, sc, period)
			if err != nil {
				s.log.Warn("relatório agendado não saiu; tenta de novo na próxima procura", "schedule", sc.ID, "err", err)
				if _, err := s.db.Exec(ctx, `UPDATE report_schedules SET last_period = $2 WHERE id = $1 AND last_period = $3`,
					sc.ID, sc.lastPeriod, period.First); err != nil {
					return sent, database.MapError(err)
				}
				continue
			}
			if delivered {
				sent++
				if _, err := s.db.Exec(ctx, `UPDATE report_schedules SET last_sent_at = NOW() WHERE id = $1`, sc.ID); err != nil {
					return sent, database.MapError(err)
				}
			}
		}
	}
	return sent, nil
}

// recipient é quem agendou, como está hoje na conta.
type recipient struct {
	email, name      string
	restricted       bool
	groups           []uuid.UUID
	canAddRecipients bool
}

// whoIs carrega a pessoa do agendamento; nil se ela não é mais da conta.
func (s *Service) whoIs(ctx context.Context, sc *Schedule) (*recipient, error) {
	var r recipient
	var role *string
	err := s.db.QueryRow(ctx, `
		SELECT u.email, u.name, m.role, COALESCE(m.restricted, FALSE),
			COALESCE((SELECT array_agg(g.group_id) FROM account_member_groups g WHERE g.user_id = u.id), '{}')
		FROM users u
		LEFT JOIN account_members m ON m.user_id = u.id AND m.account_id = $2
		WHERE u.id = $1 AND u.active AND (u.id = $2 OR m.user_id IS NOT NULL)`, sc.userID, sc.accountID).
		Scan(&r.email, &r.name, &role, &r.restricted, &r.groups)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, database.MapError(err)
	}
	r.canAddRecipients = role == nil || *role == "manager"
	return &r, nil
}

// send monta e manda o relatório do período. Devolve false quando não havia
// o que mandar (quem agendou saiu da conta, conta suspensa, nenhum veículo).
func (s *Service) send(ctx context.Context, sc *Schedule, period Period) (bool, error) {
	who, err := s.whoIs(ctx, sc)
	if err != nil {
		return false, err
	}
	if who == nil {
		// Saiu da conta: o agendamento vai junto.
		_, err := s.db.Exec(ctx, `DELETE FROM report_schedules WHERE id = $1`, sc.ID)
		return false, database.MapError(err)
	}
	if s.suspended != nil {
		suspended, err := s.suspended(ctx, sc.accountID)
		if err != nil || suspended {
			return false, err
		}
	}

	list, err := s.reports.Vehicles(ctx, sc.accountID)
	if err != nil {
		return false, err
	}
	sees := func(v reports.Vehicle) bool {
		if sc.GroupID != nil && (v.GroupID == nil || *v.GroupID != *sc.GroupID) {
			return false
		}
		return !who.restricted || (v.GroupID != nil && slices.Contains(who.groups, *v.GroupID))
	}
	list = slices.DeleteFunc(list, func(v reports.Vehicle) bool { return !sees(v) })
	if len(list) == 0 {
		return false, nil
	}

	rows, err := s.reports.Summary(ctx, list, period.First, period.Last)
	if err != nil {
		return false, err
	}
	speeding, err := s.reports.Overspeeds(ctx, list, period.First, period.Last.AddDate(0, 0, 1))
	if err != nil {
		return false, err
	}
	report, err := s.build(ctx, sc, period, list, rows, speeding)
	if err != nil {
		return false, err
	}

	report.To, report.Name = who.email, who.name
	if err := s.mailer.FleetReport(ctx, report); err != nil {
		return false, err
	}
	// Os e-mails a mais: só enquanto quem agendou puder mandar para eles.
	if who.canAddRecipients {
		for _, extra := range sc.ExtraRecipients {
			copied := report
			copied.To, copied.Name, copied.ForwardedFrom = extra, "", who.name
			if err := s.mailer.FleetReport(ctx, copied); err != nil {
				s.log.Warn("relatório não chegou a um e-mail a mais", "schedule", sc.ID, "err", err)
			}
		}
	}
	return true, nil
}

// thousands: 48312 → "48.312".
func thousands(n int) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "." + s[i:]
	}
	return s
}

var months = [...]string{"janeiro", "fevereiro", "março", "abril", "maio", "junho", "julho", "agosto",
	"setembro", "outubro", "novembro", "dezembro"}

// build monta o e-mail: os totais, os que mais rodaram e as planilhas.
func (s *Service) build(ctx context.Context, sc *Schedule, period Period, list []reports.Vehicle,
	rows []reports.SummaryRow, speeding []reports.Overspeed) (mail.FleetReport, error) {
	sheet := reports.Sheet{Loc: s.reports.Location(), Groups: map[uuid.UUID]string{}}
	groups, err := s.vehicles.Groups(ctx, sc.accountID)
	if err != nil {
		return mail.FleetReport{}, err
	}
	for _, g := range groups {
		sheet.Groups[g.ID] = g.Name
	}
	labels, err := s.vehicles.Labels(ctx, sc.accountID)
	if err != nil {
		return mail.FleetReport{}, err
	}
	sheet.Label1, sheet.Label2 = labels.Label1, labels.Label2

	r := mail.FleetReport{Kind: "semanal", Vehicles: len(list), Overspeeds: len(speeding)}
	if sc.GroupID != nil {
		r.Group = sheet.Groups[*sc.GroupID]
	}
	stamp := period.First.Format("2006-01-02")
	if sc.Frequency == Monthly {
		r.Kind = "mensal"
		r.Period = months[period.First.Month()-1] + " de " + period.First.Format("2006")
		stamp = period.First.Format("2006-01")
	} else {
		r.Period = period.First.Format("02/01") + " a " + period.Last.Format("02/01/2006")
	}

	var km float64
	var moving, trips int
	for _, row := range rows {
		km += row.DistanceKm
		moving += row.MovingSeconds
		trips += row.Trips
		if row.ActiveDays > 0 {
			r.Active++
		}
	}
	r.Km, r.Trips = thousands(int(km+0.5)), thousands(trips)
	r.Moving = fmt.Sprintf("%s:%02d h", thousands(moving/3600), moving%3600/60)
	ranked := slices.Clone(rows)
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].DistanceKm > ranked[j].DistanceKm })
	for _, row := range ranked {
		if row.DistanceKm <= 0 {
			break
		}
		if len(r.Top) == topVehicles {
			r.More++
			continue
		}
		r.Top = append(r.Top, mail.FleetReportLine{Name: row.Name, Plate: row.Plate, Km: reports.Decimal(row.DistanceKm, 1),
			Trips: row.Trips, Overspeeds: row.Overspeeds})
	}

	attach := func(name string, rows [][]string) error {
		var buf bytes.Buffer
		if err := reports.WriteCSV(&buf, rows); err != nil {
			return err
		}
		r.Attachments = append(r.Attachments, mail.Attachment{
			Name: fmt.Sprintf("farbo-%s-%s.csv", name, stamp), ContentType: "text/csv; charset=utf-8", Data: buf.Bytes()})
		return nil
	}
	if err := attach("resumo", sheet.Summary(rows, list)); err != nil {
		return r, err
	}
	if len(speeding) > 0 {
		if err := attach("excessos-de-velocidade", sheet.Overspeeds(speeding, list)); err != nil {
			return r, err
		}
	}
	return r, nil
}
