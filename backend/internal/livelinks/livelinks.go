// Package livelinks é a localização ao vivo por um tempo: o cliente gera um
// link (sem login) que mostra onde o veículo está por algumas horas — para a
// família acompanhar uma viagem, para quem vai buscar a encomenda. O link
// morre sozinho no prazo ou quando o cliente encerra. Como no modo roubo,
// nada secreto fica no banco: o link é o id assinado.
package livelinks

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// Durations são os prazos que dá para escolher, em horas.
var Durations = []int{1, 2, 4, 8, 24}

const (
	// MaxActive é quantos links ativos um veículo pode ter ao mesmo tempo.
	MaxActive = 5
	// MaxLabelLength: para quem é o link ("Mãe", "Entregador").
	MaxLabelLength = 40
)

var (
	// ErrLinkEnded: o link não vale mais (encerrado, vencido ou inválido).
	ErrLinkEnded = errors.New("este link de localização não está mais ativo")
)

// InputError é um pedido recusado; a mensagem vai para a tela.
type InputError struct{ Message string }

func (e *InputError) Error() string { return e.Message }

// Link é um link de localização ao vivo.
type Link struct {
	ID            uuid.UUID  `json:"id"`
	VehicleID     uuid.UUID  `json:"vehicleId"`
	Label         string     `json:"label"`
	CreatedByName string     `json:"createdByName"`
	CreatedAt     time.Time  `json:"createdAt"`
	ExpiresAt     time.Time  `json:"expiresAt"`
	RevokedAt     *time.Time `json:"revokedAt"`
	Views         int        `json:"views"`
	LastViewedAt  *time.Time `json:"lastViewedAt"`
	// URL só vem para quem gerou (a lista do veículo).
	URL string `json:"url,omitempty"`
}

// Active diz se o link ainda vale agora.
func (l *Link) Active(now time.Time) bool { return l.RevokedAt == nil && now.Before(l.ExpiresAt) }

type Service struct {
	db     *database.DB
	appURL string
	key    []byte
	now    func() time.Time
}

func NewService(db *database.DB, appURL string, secret []byte) *Service {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte("farbo-live-link-v1"))
	return &Service{db: db, appURL: strings.TrimRight(appURL, "/"), key: mac.Sum(nil), now: time.Now}
}

// SetClock troca o relógio (testes).
func (s *Service) SetClock(now func() time.Time) { s.now = now }

var linkEncoding = base64.RawURLEncoding

// Token é a parte secreta do link: o id e a assinatura dele.
func (s *Service) Token(id uuid.UUID) string {
	mac := hmac.New(sha256.New, s.key)
	mac.Write(id[:])
	return linkEncoding.EncodeToString(append(id[:], mac.Sum(nil)[:16]...))
}

// PublicURL é o link que o cliente manda.
func (s *Service) PublicURL(id uuid.UUID) string { return s.appURL + "/ao-vivo/" + s.Token(id) }

const selectLink = `
	SELECT l.id, l.vehicle_id, l.label, COALESCE(u.name, ''), l.created_at, l.expires_at, l.revoked_at,
		l.views, l.last_viewed_at
	FROM live_links l LEFT JOIN users u ON u.id = l.created_by`

func scan(row database.Scanner) (*Link, error) {
	var l Link
	if err := row.Scan(&l.ID, &l.VehicleID, &l.Label, &l.CreatedByName, &l.CreatedAt, &l.ExpiresAt, &l.RevokedAt,
		&l.Views, &l.LastViewedAt); err != nil {
		return nil, database.MapError(err)
	}
	return &l, nil
}

// Active são os links ainda válidos do veículo, dos mais novos, com a URL.
func (s *Service) Active(ctx context.Context, vehicleID uuid.UUID) ([]*Link, error) {
	rows, err := s.db.Query(ctx, selectLink+`
		WHERE l.vehicle_id = $1 AND l.revoked_at IS NULL AND l.expires_at > $2
		ORDER BY l.created_at DESC`, vehicleID, s.now())
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	out := []*Link{}
	for rows.Next() {
		l, err := scan(rows)
		if err != nil {
			return nil, err
		}
		l.URL = s.PublicURL(l.ID)
		out = append(out, l)
	}
	return out, database.MapError(rows.Err())
}

// Create gera um link do veículo que vale por hours horas.
func (s *Service) Create(ctx context.Context, vehicleID, by uuid.UUID, hours int, label string) (*Link, error) {
	if !slices.Contains(Durations, hours) {
		return nil, &InputError{fmt.Sprintf("escolha por quanto tempo: %s horas", joinInts(Durations))}
	}
	label = strings.Join(strings.Fields(label), " ")
	if utf8.RuneCountInString(label) > MaxLabelLength {
		return nil, &InputError{fmt.Sprintf("o nome do link vai até %d caracteres", MaxLabelLength)}
	}
	now := s.now()
	var active int
	if err := s.db.QueryRow(ctx, `
		SELECT count(*) FROM live_links WHERE vehicle_id = $1 AND revoked_at IS NULL AND expires_at > $2`,
		vehicleID, now).Scan(&active); err != nil {
		return nil, database.MapError(err)
	}
	if active >= MaxActive {
		return nil, &InputError{fmt.Sprintf("este veículo já tem %d links ativos: encerre um para gerar outro", MaxActive)}
	}
	var id uuid.UUID
	if err := s.db.QueryRow(ctx, `
		INSERT INTO live_links (vehicle_id, created_by, label, created_at, expires_at) VALUES ($1, $2, $3, $4, $5)
		RETURNING id`, vehicleID, by, label, now, now.Add(time.Duration(hours)*time.Hour)).Scan(&id); err != nil {
		return nil, database.MapError(err)
	}
	l, err := scan(s.db.QueryRow(ctx, selectLink+` WHERE l.id = $1`, id))
	if err != nil {
		return nil, err
	}
	l.URL = s.PublicURL(l.ID)
	return l, nil
}

// Revoke encerra o link do veículo.
func (s *Service) Revoke(ctx context.Context, vehicleID, id uuid.UUID) error {
	tag, err := s.db.Exec(ctx, `
		UPDATE live_links SET revoked_at = $3 WHERE id = $1 AND vehicle_id = $2 AND revoked_at IS NULL`,
		id, vehicleID, s.now())
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return database.ErrNotFound
	}
	return nil
}

// ByToken é o link de uma URL pública, enquanto valer; conta a visita.
func (s *Service) ByToken(ctx context.Context, token string) (*Link, error) {
	token = strings.TrimSpace(token)
	raw, err := linkEncoding.DecodeString(token)
	if err != nil || len(raw) != 32 {
		return nil, ErrLinkEnded
	}
	id, err := uuid.FromBytes(raw[:16])
	if err != nil || !hmac.Equal([]byte(s.Token(id)), []byte(token)) {
		return nil, ErrLinkEnded
	}
	l, err := scan(s.db.QueryRow(ctx, selectLink+` WHERE l.id = $1`, id))
	if errors.Is(err, database.ErrNotFound) {
		return nil, ErrLinkEnded
	}
	if err != nil {
		return nil, err
	}
	now := s.now()
	if !l.Active(now) {
		return nil, ErrLinkEnded
	}
	// A visita conta uma vez por minuto (a página pergunta a cada 10 s).
	if _, err := s.db.Exec(ctx, `
		UPDATE live_links SET views = views + 1, last_viewed_at = $2
		WHERE id = $1 AND (last_viewed_at IS NULL OR last_viewed_at < $3)`, id, now, now.Add(-time.Minute)); err != nil {
		return nil, database.MapError(err)
	}
	return l, nil
}

func joinInts(list []int) string {
	parts := make([]string, len(list))
	for i, n := range list {
		parts[i] = fmt.Sprint(n)
	}
	last := parts[len(parts)-1]
	return strings.Join(parts[:len(parts)-1], ", ") + " ou " + last
}
