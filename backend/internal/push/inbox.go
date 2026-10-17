package push

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// A área de notificações do app: o que o Notify manda fica guardado para a
// pessoa ver depois, lida ou não (o teste das notificações, não).

const (
	// InboxLimit: quantas a lista traz (as mais recentes).
	InboxLimit = 100
	// inboxKeep: por quanto tempo uma notificação fica guardada.
	inboxKeep = 90 * 24 * time.Hour
)

// Item é uma notificação na área do app.
type Item struct {
	ID        int64      `json:"id"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	URL       string     `json:"url"`
	Severity  string     `json:"severity"`
	ReadAt    *time.Time `json:"readAt"`
	CreatedAt time.Time  `json:"createdAt"`
}

// Inbox é a lista e quantas ainda não foram lidas.
type Inbox struct {
	Items  []Item `json:"items"`
	Unread int    `json:"unread"`
}

// record guarda a notificação na área do usuário.
func (s *Service) record(ctx context.Context, userID uuid.UUID, n Notification) error {
	_, err := s.db.Exec(ctx, `
		INSERT INTO user_notifications (user_id, title, body, url, severity) VALUES ($1, $2, $3, $4, $5)`,
		userID, n.Title, n.Body, n.URL, n.Severity)
	return database.MapError(err)
}

// Inbox traz as notificações mais recentes do usuário e quantas não lidas.
func (s *Service) Inbox(ctx context.Context, userID uuid.UUID) (*Inbox, error) {
	out := &Inbox{Items: []Item{}}
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM user_notifications WHERE user_id = $1 AND read_at IS NULL`,
		userID).Scan(&out.Unread); err != nil {
		return nil, database.MapError(err)
	}
	rows, err := s.db.Query(ctx, `
		SELECT id, title, body, url, severity, read_at, created_at FROM user_notifications
		WHERE user_id = $1 ORDER BY created_at DESC, id DESC LIMIT $2`, userID, InboxLimit)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var it Item
		if err := rows.Scan(&it.ID, &it.Title, &it.Body, &it.URL, &it.Severity, &it.ReadAt, &it.CreatedAt); err != nil {
			return nil, database.MapError(err)
		}
		out.Items = append(out.Items, it)
	}
	return out, database.MapError(rows.Err())
}

// MarkRead marca como lida uma notificação do usuário (id) ou todas (nil).
func (s *Service) MarkRead(ctx context.Context, userID uuid.UUID, id *int64) error {
	_, err := s.db.Exec(ctx, `
		UPDATE user_notifications SET read_at = NOW()
		WHERE user_id = $1 AND read_at IS NULL AND ($2::bigint IS NULL OR id = $2)`, userID, id)
	return database.MapError(err)
}

// PruneInbox apaga as notificações guardadas há mais de 90 dias.
func (s *Service) PruneInbox(ctx context.Context) (int64, error) {
	tag, err := s.db.Exec(ctx, `DELETE FROM user_notifications WHERE created_at < $1`, s.now().Add(-inboxKeep))
	if err != nil {
		return 0, database.MapError(err)
	}
	return tag.RowsAffected(), nil
}
