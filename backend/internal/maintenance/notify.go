package maintenance

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/mail"
)

// O aviso por e-mail: entre notifyFrom e notifyUntil (no fuso da central), a
// cada notifyEvery, as manutenções que ficaram perto ou venceram desde o
// último aviso vão, num e-mail por pessoa, para o titular e o Gestor que veem
// o veículo. Cada uma é avisada uma vez quando fica perto e outra quando
// vence (notified); registrar a manutenção recomeça.
const (
	notifyEvery = 30 * time.Minute
	notifyFrom  = 8
	notifyUntil = 20
)

// Mailer manda o aviso (mail.MaintenanceMailer).
type Mailer interface {
	Digest(ctx context.Context, d mail.MaintenanceDigest) error
}

// Notifier é a rotina do aviso.
type Notifier struct {
	svc       *Service
	mailer    Mailer
	suspended func(ctx context.Context, customerID uuid.UUID) (bool, error)
	log       *slog.Logger
}

func NewNotifier(svc *Service, mailer Mailer, suspended func(ctx context.Context, customerID uuid.UUID) (bool, error), log *slog.Logger) *Notifier {
	return &Notifier{svc: svc, mailer: mailer, suspended: suspended, log: log.With("component", "maintenance")}
}

// Run avisa a cada notifyEvery, no horário comercial.
func (n *Notifier) Run(ctx context.Context) {
	ticker := time.NewTicker(notifyEvery)
	defer ticker.Stop()
	for {
		if hour := n.svc.now().In(n.svc.loc).Hour(); hour >= notifyFrom && hour < notifyUntil {
			if sent, err := n.Round(ctx); err != nil && ctx.Err() == nil {
				n.log.Error("falha ao avisar das manutenções", "err", err)
			} else if sent > 0 {
				n.log.Info("avisos de manutenção enviados", "emails", sent)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// person é quem recebe o aviso de uma conta.
type person struct {
	email, name string
	restricted  bool
	groups      []uuid.UUID
}

// Round avisa das manutenções que pioraram e devolve quantos e-mails saíram.
func (n *Notifier) Round(ctx context.Context) (int, error) {
	items, err := n.svc.items(ctx, `v.owner_id IS NOT NULL AND (i.notified IS NULL OR i.notified = 'soon')`)
	if err != nil {
		return 0, err
	}
	byAccount := map[uuid.UUID][]*Item{}
	for _, it := range items {
		if it.Status != StatusSoon && it.Status != StatusDue {
			continue
		}
		if it.notified != nil && rank(*it.notified) >= rank(it.Status) {
			continue
		}
		byAccount[it.accountID] = append(byAccount[it.accountID], it)
	}

	sent := 0
	for account, list := range byAccount {
		if n.suspended != nil {
			if suspended, err := n.suspended(ctx, account); err != nil || suspended {
				continue
			}
		}
		// Reserva: duas instâncias não avisam a mesma manutenção.
		claimed := []*Item{}
		for _, it := range list {
			tag, err := n.svc.db.Exec(ctx, `
				UPDATE maintenance_items SET notified = $2
				WHERE id = $1 AND (notified IS NULL OR (notified = 'soon' AND $2 = 'due'))`, it.ID, it.Status)
			if err != nil {
				return sent, database.MapError(err)
			}
			if tag.RowsAffected() > 0 {
				claimed = append(claimed, it)
			}
		}
		if len(claimed) == 0 {
			continue
		}
		people, err := n.people(ctx, account)
		if err != nil {
			return sent, err
		}
		SortByUrgency(claimed)
		delivered := false
		for _, p := range people {
			var lines []mail.MaintenanceLine
			for _, it := range claimed {
				if p.restricted && (it.GroupID == nil || !slices.Contains(p.groups, *it.GroupID)) {
					continue
				}
				lines = append(lines, mail.MaintenanceLine{Vehicle: it.VehicleName, Plate: it.Plate, Item: it.Name,
					Due: it.Status == StatusDue, Detail: Describe(it)})
			}
			if len(lines) == 0 {
				continue
			}
			if err := n.mailer.Digest(ctx, mail.MaintenanceDigest{To: p.email, Name: p.name, Lines: lines}); err != nil {
				n.log.Warn("aviso de manutenção não saiu", "account", account, "err", err)
				continue
			}
			sent++
			delivered = true
		}
		if !delivered {
			// Ninguém recebeu (e-mail fora do ar): a próxima procura tenta de novo.
			for _, it := range claimed {
				if _, err := n.svc.db.Exec(ctx, `UPDATE maintenance_items SET notified = $2 WHERE id = $1`, it.ID, it.notified); err != nil {
					return sent, database.MapError(err)
				}
			}
		}
	}
	return sent, nil
}

// people são o titular e os Gestores da conta (com os grupos de quem está
// restrito).
func (n *Notifier) people(ctx context.Context, account uuid.UUID) ([]person, error) {
	rows, err := n.svc.db.Query(ctx, `
		SELECT u.email, u.name, FALSE, '{}'::uuid[] FROM users u WHERE u.id = $1 AND u.active
		UNION ALL
		SELECT u.email, u.name, m.restricted,
			COALESCE((SELECT array_agg(g.group_id) FROM account_member_groups g WHERE g.user_id = m.user_id), '{}')
		FROM account_members m JOIN users u ON u.id = m.user_id
		WHERE m.account_id = $1 AND m.role = 'manager' AND u.active`, account)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	var out []person
	for rows.Next() {
		var p person
		if err := rows.Scan(&p.email, &p.name, &p.restricted, &p.groups); err != nil {
			return nil, database.MapError(err)
		}
		out = append(out, p)
	}
	return out, database.MapError(rows.Err())
}

// Describe diz quanto falta (ou passou), por extenso: "venceu há 320 km",
// "faltam 12 dias".
func Describe(it *Item) string {
	var parts []string
	if it.RemainingKm != nil {
		km := int(math.Round(*it.RemainingKm))
		if km <= 0 {
			parts = append(parts, fmt.Sprintf("passou %s km", thousands(-km)))
		} else {
			parts = append(parts, fmt.Sprintf("faltam %s km", thousands(km)))
		}
	}
	if it.RemainingDays != nil {
		days := *it.RemainingDays
		switch {
		case days < 0:
			parts = append(parts, fmt.Sprintf("venceu há %d %s", -days, plural(-days, "dia", "dias")))
		case days == 0:
			parts = append(parts, "vence hoje")
		default:
			parts = append(parts, fmt.Sprintf("faltam %d %s", days, plural(days, "dia", "dias")))
		}
	}
	return strings.Join(parts, " · ")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// thousands: 48312 → "48.312".
func thousands(n int) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "." + s[i:]
	}
	return s
}
