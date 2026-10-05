package leads

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// WaitlistEntry é quem pediu para ser avisado do lançamento.
type WaitlistEntry struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Phone     string    `json:"phone"`
	ConsentAt time.Time `json:"consentAt"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	// CustomerID: já é cliente (conta com este e-mail).
	CustomerID *uuid.UUID `json:"customerId"`
	// PromoClaimed: contratou com a promoção de pré-lançamento.
	PromoClaimed bool `json:"promoClaimed"`
	// Event: o evento em que se inscreveu (pelo QR Code); vazio: pela landing.
	Event string `json:"event"`
	// City: a cidade da instalação (a tela do evento pede).
	City string `json:"city"`
}

// WaitlistInput é o que a seção de pré-lançamento manda: o nome é opcional;
// e-mail e WhatsApp, obrigatórios.
type WaitlistInput struct {
	Name    string
	Email   string
	Phone   string
	Consent bool
	// Event: o evento da tela aberta pelo QR Code (vira EventSlug).
	Event string
	// City: a cidade da instalação (opcional; a tela do evento pede).
	City string
}

// maxEvent: o nome do evento no link do QR Code.
const maxEvent = 60

// EventSlug deixa o nome do evento no formato do link: minúsculo, sem
// acento, palavras separadas por hífen ("Encontro Insanos MC — Out/26" →
// "encontro-insanos-mc-out-26").
func EventSlug(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		if plain, ok := unaccent[r]; ok {
			r = plain
		}
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r)
			dash = false
		} else {
			dash = true
		}
		if b.Len() >= maxEvent {
			break
		}
	}
	return strings.TrimRight(b.String(), "-")
}

var unaccent = map[rune]rune{
	'á': 'a', 'à': 'a', 'â': 'a', 'ã': 'a', 'ä': 'a', 'é': 'e', 'è': 'e', 'ê': 'e', 'ë': 'e',
	'í': 'i', 'ì': 'i', 'î': 'i', 'ï': 'i', 'ó': 'o', 'ò': 'o', 'ô': 'o', 'õ': 'o', 'ö': 'o',
	'ú': 'u', 'ù': 'u', 'û': 'u', 'ü': 'u', 'ç': 'c', 'ñ': 'n',
}

func normalizeWaitlist(in WaitlistInput) (WaitlistInput, error) {
	in.Name = strings.Join(strings.Fields(in.Name), " ")
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	in.Phone = strings.TrimSpace(in.Phone)
	in.Event = EventSlug(in.Event)
	in.City = strings.Join(strings.Fields(in.City), " ")
	if utf8.RuneCountInString(in.Name) > 120 {
		return in, ValidationError{"nome longo demais"}
	}
	if utf8.RuneCountInString(in.City) > 120 {
		return in, ValidationError{"cidade longa demais"}
	}
	if !validEmail(in.Email) {
		return in, ValidationError{"informe um e-mail válido"}
	}
	if !validPhone(in.Phone) {
		return in, ValidationError{"informe o seu WhatsApp com DDD"}
	}
	if !in.Consent {
		return in, ValidationError{"é preciso aceitar receber o aviso do lançamento"}
	}
	return in, nil
}

// JoinWaitlist põe o e-mail na lista. Quem já está fica (com o nome
// atualizado, se veio): mandar de novo não duplica. O evento é o primeiro
// que trouxe a pessoa; a cidade, a última informada.
func (s *Service) JoinWaitlist(ctx context.Context, in WaitlistInput) (*WaitlistEntry, error) {
	in, err := normalizeWaitlist(in)
	if err != nil {
		return nil, err
	}
	var e WaitlistEntry
	err = s.repo.db.QueryRow(ctx, `
		INSERT INTO launch_waitlist (name, email, phone, event, city, consent_at) VALUES ($1, $2, $3, $4, $5, NOW())
		ON CONFLICT (lower(email)) DO UPDATE SET
			name = COALESCE(NULLIF(EXCLUDED.name, ''), launch_waitlist.name),
			phone = EXCLUDED.phone,
			event = COALESCE(NULLIF(launch_waitlist.event, ''), EXCLUDED.event),
			city = COALESCE(NULLIF(EXCLUDED.city, ''), launch_waitlist.city),
			consent_at = NOW(), updated_at = NOW()
		RETURNING id, name, email, phone, event, city, consent_at, created_at, updated_at`,
		in.Name, in.Email, in.Phone, in.Event, in.City).
		Scan(&e.ID, &e.Name, &e.Email, &e.Phone, &e.Event, &e.City, &e.ConsentAt, &e.CreatedAt, &e.UpdatedAt)
	if err != nil {
		return nil, database.MapError(err)
	}
	s.log.Info("inscrição na lista de lançamento", "entry", e.ID, "event", e.Event)
	return &e, nil
}

// Waitlist traz a lista inteira, as inscrições mais recentes primeiro.
func (r *Repository) Waitlist(ctx context.Context) ([]*WaitlistEntry, error) {
	rows, err := r.db.Query(ctx, `
		SELECT w.id, w.name, w.email, w.phone, w.event, w.city, w.consent_at, w.created_at, w.updated_at, u.id, c.customer_id IS NOT NULL
		FROM launch_waitlist w
		LEFT JOIN users u ON lower(u.email) = lower(w.email) AND u.role = 'customer'
		LEFT JOIN launch_promo_claims c ON c.customer_id = u.id
		ORDER BY w.created_at DESC`)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	out := []*WaitlistEntry{}
	for rows.Next() {
		var e WaitlistEntry
		if err := rows.Scan(&e.ID, &e.Name, &e.Email, &e.Phone, &e.Event, &e.City, &e.ConsentAt, &e.CreatedAt, &e.UpdatedAt,
			&e.CustomerID, &e.PromoClaimed); err != nil {
			return nil, database.MapError(err)
		}
		out = append(out, &e)
	}
	return out, database.MapError(rows.Err())
}

// RemoveFromWaitlist tira alguém da lista (pedido da pessoa, LGPD).
func (r *Repository) RemoveFromWaitlist(ctx context.Context, id uuid.UUID) error {
	tag, err := r.db.Exec(ctx, `DELETE FROM launch_waitlist WHERE id = $1`, id)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
