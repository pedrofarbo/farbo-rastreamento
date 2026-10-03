package support

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/whatsapp"
)

// Quem responde a conversa.
const (
	ModeBot   = "BOT"
	ModeHuman = "HUMAN"
)

// Quem escreveu a mensagem.
const (
	AuthorContact = "CONTACT"
	AuthorBot     = "BOT"
	AuthorAgent   = "AGENT"
)

// Window é o prazo, desde a última mensagem do contato, em que o WhatsApp
// aceita resposta em texto livre. Depois, só modelo aprovado pela Meta.
const Window = 24 * time.Hour

// ErrNotFound: conversa inexistente.
var ErrNotFound = database.ErrNotFound

// errDuplicate: o webhook repetiu uma mensagem já gravada.
var errDuplicate = errors.New("mensagem já recebida")

type Conversation struct {
	ID          uuid.UUID `json:"id"`
	WaID        string    `json:"waId"`
	Phone       string    `json:"phone"`
	ContactName string    `json:"contactName"`
	// Cliente com este telefone no cadastro.
	CustomerID     *uuid.UUID `json:"customerId"`
	CustomerName   string     `json:"customerName"`
	Mode           string     `json:"mode"`
	HandoffReason  string     `json:"handoffReason"`
	NeedsAttention bool       `json:"needsAttention"`
	LastInboundAt  *time.Time `json:"lastInboundAt"`
	LastMessageAt  time.Time  `json:"lastMessageAt"`
	// WindowOpen: dá para responder com texto livre (até 24 h depois da
	// última mensagem do contato).
	WindowOpen bool      `json:"windowOpen"`
	CreatedAt  time.Time `json:"createdAt"`
	// LastMessage é a prévia da lista.
	LastMessage *Message `json:"lastMessage,omitempty"`
}

type Message struct {
	ID        int64      `json:"id"`
	Direction string     `json:"direction"`
	Author    string     `json:"author"`
	AgentID   *uuid.UUID `json:"agentId"`
	AgentName string     `json:"agentName"`
	Kind      string     `json:"kind"`
	Body      string     `json:"body"`
	Status    string     `json:"status"`
	Error     string     `json:"error"`
	CreatedAt time.Time  `json:"createdAt"`
	// Id na Meta: marcar como lida.
	WaMessageID string `json:"-"`
}

type Repository struct{ db *database.DB }

func NewRepository(db *database.DB) *Repository { return &Repository{db: db} }

const selectConversation = `
	SELECT c.id, c.wa_id, c.contact_name, c.customer_id, COALESCE(u.name, ''), c.mode, c.handoff_reason,
		c.needs_attention, c.last_inbound_at, c.last_message_at, c.created_at
	FROM whatsapp_conversations c
	LEFT JOIN users u ON u.id = c.customer_id`

func scanConversation(row database.Scanner) (*Conversation, error) {
	var c Conversation
	if err := row.Scan(&c.ID, &c.WaID, &c.ContactName, &c.CustomerID, &c.CustomerName, &c.Mode, &c.HandoffReason,
		&c.NeedsAttention, &c.LastInboundAt, &c.LastMessageAt, &c.CreatedAt); err != nil {
		return nil, database.MapError(err)
	}
	c.Phone = displayPhone(c.WaID)
	c.WindowOpen = c.LastInboundAt != nil && time.Since(*c.LastInboundAt) < Window
	return &c, nil
}

const selectMessage = `
	SELECT m.id, m.direction, m.author, m.agent_id, COALESCE(u.name, ''), m.kind, m.body, m.status, m.error,
		m.created_at, COALESCE(m.wa_message_id, '')
	FROM whatsapp_messages m
	LEFT JOIN users u ON u.id = m.agent_id`

func scanMessage(row database.Scanner) (*Message, error) {
	var m Message
	if err := row.Scan(&m.ID, &m.Direction, &m.Author, &m.AgentID, &m.AgentName, &m.Kind, &m.Body, &m.Status,
		&m.Error, &m.CreatedAt, &m.WaMessageID); err != nil {
		return nil, database.MapError(err)
	}
	return &m, nil
}

func (r *Repository) Get(ctx context.Context, id uuid.UUID) (*Conversation, error) {
	return scanConversation(r.db.QueryRow(ctx, selectConversation+` WHERE c.id = $1`, id))
}

// List traz as conversas mais recentes, com a última mensagem; com
// onlyAttention, só as que esperam a equipe.
func (r *Repository) List(ctx context.Context, onlyAttention bool, limit int) ([]*Conversation, error) {
	where := ""
	if onlyAttention {
		where = " WHERE c.needs_attention"
	}
	rows, err := r.db.Query(ctx, selectConversation+where+` ORDER BY c.last_message_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, database.MapError(err)
	}
	out := []*Conversation{}
	byID := map[uuid.UUID]*Conversation{}
	for rows.Next() {
		c, err := scanConversation(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, c)
		byID[c.ID] = c
	}
	rows.Close()
	if err := rows.Err(); err != nil || len(out) == 0 {
		return out, database.MapError(err)
	}
	ids := make([]uuid.UUID, 0, len(out))
	for _, c := range out {
		ids = append(ids, c.ID)
	}
	last, err := r.db.Query(ctx, `
		SELECT DISTINCT ON (m.conversation_id) m.conversation_id, m.id, m.direction, m.author, m.agent_id,
			COALESCE(u.name, ''), m.kind, m.body, m.status, m.error, m.created_at, COALESCE(m.wa_message_id, '')
		FROM whatsapp_messages m
		LEFT JOIN users u ON u.id = m.agent_id
		WHERE m.conversation_id = ANY($1)
		ORDER BY m.conversation_id, m.id DESC`, ids)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer last.Close()
	for last.Next() {
		var owner uuid.UUID
		var m Message
		if err := last.Scan(&owner, &m.ID, &m.Direction, &m.Author, &m.AgentID, &m.AgentName, &m.Kind, &m.Body,
			&m.Status, &m.Error, &m.CreatedAt, &m.WaMessageID); err != nil {
			return nil, database.MapError(err)
		}
		byID[owner].LastMessage = &m
	}
	return out, database.MapError(last.Err())
}

// CountAttention é o número no menu do painel.
func (r *Repository) CountAttention(ctx context.Context) (int, error) {
	var n int
	err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM whatsapp_conversations WHERE needs_attention`).Scan(&n)
	return n, database.MapError(err)
}

// Messages traz as últimas mensagens da conversa, da mais antiga para a
// mais nova.
func (r *Repository) Messages(ctx context.Context, conversationID uuid.UUID, limit int) ([]*Message, error) {
	rows, err := r.db.Query(ctx, `SELECT * FROM (`+selectMessage+`
		WHERE m.conversation_id = $1 ORDER BY m.id DESC LIMIT $2) recent ORDER BY id`, conversationID, limit)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	out := []*Message{}
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, database.MapError(rows.Err())
}

// FindCustomer acha o cliente pelo telefone do cadastro; com mais de um
// cliente no mesmo número, ninguém (não dá para saber quem é).
func (r *Repository) FindCustomer(ctx context.Context, waID string) (*uuid.UUID, error) {
	keys := phoneKeys(waID)
	if len(keys) == 0 {
		return nil, nil
	}
	rows, err := r.db.Query(ctx, `
		SELECT id FROM users
		WHERE role = 'customer' AND active AND regexp_replace(phone, '\D', '', 'g') = ANY($1)
		LIMIT 2`, keys)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	var found []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, database.MapError(err)
		}
		found = append(found, id)
	}
	if err := rows.Err(); err != nil || len(found) != 1 {
		return nil, database.MapError(err)
	}
	return &found[0], nil
}

// recordInbound grava a mensagem do contato (abrindo a conversa, se for a
// primeira) e devolve a conversa. Mensagem repetida pelo webhook: errDuplicate.
func (r *Repository) recordInbound(ctx context.Context, in whatsapp.Inbound, customerID *uuid.UUID) (*Conversation, error) {
	var conv *Conversation
	err := pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO whatsapp_conversations (wa_id, contact_name, customer_id, last_inbound_at, last_message_at)
			VALUES ($1, $2, $3, $4, NOW())
			ON CONFLICT (wa_id) DO UPDATE SET
				contact_name    = COALESCE(NULLIF(EXCLUDED.contact_name, ''), whatsapp_conversations.contact_name),
				customer_id     = COALESCE(whatsapp_conversations.customer_id, EXCLUDED.customer_id),
				last_inbound_at = GREATEST(whatsapp_conversations.last_inbound_at, EXCLUDED.last_inbound_at),
				last_message_at = NOW(),
				updated_at      = NOW()
			RETURNING id`, in.From, in.Name, customerID, in.At).Scan(&id); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `
			INSERT INTO whatsapp_messages (conversation_id, direction, author, wa_message_id, kind, body, created_at)
			VALUES ($1, 'IN', 'CONTACT', $2, $3, $4, $5)
			ON CONFLICT (wa_message_id) DO NOTHING`, id, in.ID, in.Kind, in.Body, in.At)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return errDuplicate
		}
		conv, err = scanConversation(tx.QueryRow(ctx, selectConversation+` WHERE c.id = $1`, id))
		return err
	})
	if errors.Is(err, errDuplicate) {
		return nil, errDuplicate
	}
	return conv, database.MapError(err)
}

// recordOutbound grava o que saiu. Resposta da equipe tira a conversa da
// fila de atenção.
func (r *Repository) recordOutbound(ctx context.Context, conversationID uuid.UUID, author string, agentID *uuid.UUID,
	waMessageID, body string) (*Message, error) {
	var m *Message
	err := pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		var id int64
		if err := tx.QueryRow(ctx, `
			INSERT INTO whatsapp_messages (conversation_id, direction, author, agent_id, wa_message_id, body, status)
			VALUES ($1, 'OUT', $2, $3, $4, $5, 'sent') RETURNING id`,
			conversationID, author, agentID, waMessageID, body).Scan(&id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE whatsapp_conversations SET last_message_at = NOW(), updated_at = NOW(),
				needs_attention = needs_attention AND $2 <> 'AGENT'
			WHERE id = $1`, conversationID, author); err != nil {
			return err
		}
		var err error
		m, err = scanMessage(tx.QueryRow(ctx, selectMessage+` WHERE m.id = $1`, id))
		return err
	})
	return m, database.MapError(err)
}

// setStatus aplica a notícia da Meta sobre uma mensagem enviada. Elas podem
// chegar fora de ordem: o status só avança (enviada → entregue → lida).
func (r *Repository) setStatus(ctx context.Context, waMessageID, status, errText string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE whatsapp_messages SET status = $2, error = $3
		WHERE wa_message_id = $1 AND direction = 'OUT'
			AND COALESCE(array_position(ARRAY['', 'sent', 'delivered', 'read', 'failed'], status), 0)
				< COALESCE(array_position(ARRAY['', 'sent', 'delivered', 'read', 'failed'], $2::text), 0)`,
		waMessageID, status, errText)
	return database.MapError(err)
}

// setMode muda quem responde. Para a equipe com attention, entra na fila.
func (r *Repository) setMode(ctx context.Context, id uuid.UUID, mode, reason string, attention bool) error {
	tag, err := r.db.Exec(ctx, `
		UPDATE whatsapp_conversations SET mode = $2, handoff_reason = $3, needs_attention = $4, updated_at = NOW()
		WHERE id = $1`, id, mode, reason, attention)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// flagAttention põe a conversa na fila da equipe (contato escreveu numa
// conversa que está com ela).
func (r *Repository) flagAttention(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.Exec(ctx, `UPDATE whatsapp_conversations SET needs_attention = TRUE WHERE id = $1`, id)
	return database.MapError(err)
}

// botRepliesSince conta as respostas da IA no período (limite diário).
func (r *Repository) botRepliesSince(ctx context.Context, conversationID uuid.UUID, since time.Time) (int, error) {
	var n int
	err := r.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM whatsapp_messages
		WHERE conversation_id = $1 AND author = 'BOT' AND created_at >= $2`, conversationID, since).Scan(&n)
	return n, database.MapError(err)
}
