package organizations

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/mail"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/push"
)

// Os comunicados da organização aos associados: a mensagem vai para a área
// de notificações do app de cada associado ativo (e o celular, se ele ligou
// as notificações) e, se pedido, por e-mail. Fica o histórico com quantos
// receberam.

const (
	MaxAnnouncementTitle = 80
	MaxAnnouncementBody  = 1000
	// MaxAnnouncementsPerDay: o teto por organização, para o comunicado não
	// virar propaganda que faz o associado desligar as notificações.
	MaxAnnouncementsPerDay = 5
	// announcementsListed: quantos o histórico mostra.
	announcementsListed = 50
)

// Pusher avisa no celular e guarda na área de notificações (push.Service).
type Pusher interface {
	Notify(ctx context.Context, userID uuid.UUID, n push.Notification) (int, error)
}

// SetPusher liga a entrega dos comunicados no app.
func (s *Service) SetPusher(p Pusher) { s.pusher = p }

// Announcement é um comunicado enviado.
type Announcement struct {
	ID            uuid.UUID  `json:"id"`
	Title         string     `json:"title"`
	Body          string     `json:"body"`
	Email         bool       `json:"email"`
	Recipients    int        `json:"recipients"`
	Delivered     int        `json:"delivered"`
	Emailed       int        `json:"emailed"`
	FinishedAt    *time.Time `json:"finishedAt"`
	CreatedByName string     `json:"createdByName"`
	CreatedAt     time.Time  `json:"createdAt"`
}

// AnnouncementInput é o comunicado pedido.
type AnnouncementInput struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Email bool   `json:"email"`
}

const selectAnnouncement = `
	SELECT a.id, a.title, a.body, a.email, a.recipients, a.delivered, a.emailed, a.finished_at,
		COALESCE(u.name, ''), a.created_at
	FROM org_announcements a LEFT JOIN users u ON u.id = a.created_by`

func scanAnnouncement(row database.Scanner) (*Announcement, error) {
	var a Announcement
	if err := row.Scan(&a.ID, &a.Title, &a.Body, &a.Email, &a.Recipients, &a.Delivered, &a.Emailed, &a.FinishedAt,
		&a.CreatedByName, &a.CreatedAt); err != nil {
		return nil, database.MapError(err)
	}
	return &a, nil
}

// Announcements são os comunicados da organização, dos mais novos.
func (s *Service) Announcements(ctx context.Context, orgID uuid.UUID) ([]*Announcement, error) {
	rows, err := s.db.Query(ctx, selectAnnouncement+`
		WHERE a.organization_id = $1 ORDER BY a.created_at DESC LIMIT $2`, orgID, announcementsListed)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	out := []*Announcement{}
	for rows.Next() {
		a, err := scanAnnouncement(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, database.MapError(rows.Err())
}

// recipient é um associado que recebe o comunicado.
type recipient struct {
	id          uuid.UUID
	name, email string
}

// Announce manda o comunicado aos associados ativos da organização. A
// entrega é fora da requisição; o histórico mostra quantos receberam.
func (s *Service) Announce(ctx context.Context, orgID, by uuid.UUID, in AnnouncementInput) (*Announcement, error) {
	in.Title = strings.Join(strings.Fields(in.Title), " ")
	in.Body = strings.TrimSpace(in.Body)
	switch {
	case in.Title == "" || utf8.RuneCountInString(in.Title) > MaxAnnouncementTitle:
		return nil, &InputError{fmt.Sprintf("dê um título ao comunicado (até %d caracteres)", MaxAnnouncementTitle)}
	case in.Body == "" || utf8.RuneCountInString(in.Body) > MaxAnnouncementBody:
		return nil, &InputError{fmt.Sprintf("escreva a mensagem (até %d caracteres)", MaxAnnouncementBody)}
	}
	org, err := s.Get(ctx, orgID)
	if err != nil {
		return nil, err
	}
	var today int
	if err := s.db.QueryRow(ctx, `
		SELECT count(*) FROM org_announcements WHERE organization_id = $1 AND created_at > NOW() - INTERVAL '1 day'`,
		orgID).Scan(&today); err != nil {
		return nil, database.MapError(err)
	}
	if today >= MaxAnnouncementsPerDay {
		return nil, &InputError{fmt.Sprintf("são até %d comunicados por dia: mande o próximo amanhã", MaxAnnouncementsPerDay)}
	}
	rows, err := s.db.Query(ctx, `
		SELECT id, name, email FROM users WHERE organization_id = $1 AND role = $2 AND active ORDER BY name`,
		orgID, auth.RoleCustomer)
	if err != nil {
		return nil, database.MapError(err)
	}
	var people []recipient
	for rows.Next() {
		var p recipient
		if err := rows.Scan(&p.id, &p.name, &p.email); err != nil {
			rows.Close()
			return nil, database.MapError(err)
		}
		people = append(people, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, database.MapError(err)
	}
	if len(people) == 0 {
		return nil, &InputError{"nenhum associado ativo para receber o comunicado"}
	}
	var id uuid.UUID
	if err := s.db.QueryRow(ctx, `
		INSERT INTO org_announcements (organization_id, title, body, email, recipients, created_by)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`, orgID, in.Title, in.Body, in.Email, len(people), by).Scan(&id); err != nil {
		return nil, database.MapError(err)
	}
	s.runAsync(func() { s.deliverAnnouncement(context.Background(), id, org.Name, in, people) })
	return scanAnnouncement(s.db.QueryRow(ctx, selectAnnouncement+` WHERE a.id = $1`, id))
}

// deliverAnnouncement entrega a cada associado e guarda quantos receberam.
func (s *Service) deliverAnnouncement(ctx context.Context, id uuid.UUID, orgName string, in AnnouncementInput, people []recipient) {
	delivered, emailed := 0, 0
	for _, p := range people {
		if s.pusher != nil {
			if _, err := s.pusher.Notify(ctx, p.id, push.Notification{
				Title: in.Title, Body: in.Body, URL: "/app/alertas", Tag: "announcement:" + id.String(),
				Severity: mail.SeverityInfo, TTL: 72 * time.Hour,
			}); err != nil {
				s.log.Warn("comunicado não chegou ao app", "announcement", id, "user", p.id, "err", err)
			} else {
				delivered++
			}
		}
		if in.Email && p.email != "" {
			if err := s.notifier.Announcement(ctx, p.email, mail.OrgAnnouncement{
				OrgName: orgName, MemberName: p.name, Title: in.Title, Body: in.Body,
			}); err != nil {
				s.log.Warn("comunicado não saiu por e-mail", "announcement", id, "user", p.id, "err", err)
			} else {
				emailed++
			}
		}
	}
	if _, err := s.db.Exec(ctx, `
		UPDATE org_announcements SET delivered = $2, emailed = $3, finished_at = NOW() WHERE id = $1`,
		id, delivered, emailed); err != nil {
		s.log.Error("falha ao guardar a entrega do comunicado", "announcement", id, "err", err)
	}
}
