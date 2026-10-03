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
}

// WaitlistInput é o que a seção de pré-lançamento manda: o nome é opcional;
// e-mail e WhatsApp, obrigatórios.
type WaitlistInput struct {
	Name    string
	Email   string
	Phone   string
	Consent bool
}

func normalizeWaitlist(in WaitlistInput) (WaitlistInput, error) {
	in.Name = strings.Join(strings.Fields(in.Name), " ")
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	in.Phone = strings.TrimSpace(in.Phone)
	if utf8.RuneCountInString(in.Name) > 120 {
		return in, ValidationError{"nome longo demais"}
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
// atualizado, se veio): mandar de novo não duplica.
func (s *Service) JoinWaitlist(ctx context.Context, in WaitlistInput) (*WaitlistEntry, error) {
	in, err := normalizeWaitlist(in)
	if err != nil {
		return nil, err
	}
	var e WaitlistEntry
	err = s.repo.db.QueryRow(ctx, `
		INSERT INTO launch_waitlist (name, email, phone, consent_at) VALUES ($1, $2, $3, NOW())
		ON CONFLICT (lower(email)) DO UPDATE SET
			name = COALESCE(NULLIF(EXCLUDED.name, ''), launch_waitlist.name),
			phone = EXCLUDED.phone,
			consent_at = NOW(), updated_at = NOW()
		RETURNING id, name, email, phone, consent_at, created_at, updated_at`, in.Name, in.Email, in.Phone).
		Scan(&e.ID, &e.Name, &e.Email, &e.Phone, &e.ConsentAt, &e.CreatedAt, &e.UpdatedAt)
	if err != nil {
		return nil, database.MapError(err)
	}
	s.log.Info("inscrição na lista de lançamento", "entry", e.ID)
	return &e, nil
}

// Waitlist traz a lista inteira, as inscrições mais recentes primeiro.
func (r *Repository) Waitlist(ctx context.Context) ([]*WaitlistEntry, error) {
	rows, err := r.db.Query(ctx, `
		SELECT w.id, w.name, w.email, w.phone, w.consent_at, w.created_at, w.updated_at, u.id, c.customer_id IS NOT NULL
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
		if err := rows.Scan(&e.ID, &e.Name, &e.Email, &e.Phone, &e.ConsentAt, &e.CreatedAt, &e.UpdatedAt,
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
