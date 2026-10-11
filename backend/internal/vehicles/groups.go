package vehicles

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// Os limites da organização da frota: grupos por conta, o nome de um grupo,
// o nome de um campo personalizado e o valor de um campo.
const (
	MaxGroups       = 100
	MaxGroupName    = 60
	MaxLabelLength  = 40
	MaxCustomLength = 100
)

// Group é um grupo de veículos da conta, com quantos veículos tem.
type Group struct {
	ID       uuid.UUID `json:"id"`
	Name     string    `json:"name"`
	Vehicles int       `json:"vehicles"`
}

// FieldLabels são os nomes dos dois campos personalizados da conta (vazio:
// o campo ainda não tem nome).
type FieldLabels struct {
	Label1 string `json:"label1"`
	Label2 string `json:"label2"`
}

// ErrGroupNameTaken: a conta já tem um grupo com esse nome.
var ErrGroupNameTaken = ValidationError{Message: "já existe um grupo com esse nome"}

func groupName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", ValidationError{Message: "dê um nome ao grupo"}
	}
	if utf8.RuneCountInString(name) > MaxGroupName {
		return "", ValidationError{Message: fmt.Sprintf("o nome do grupo vai até %d caracteres", MaxGroupName)}
	}
	return name, nil
}

// Groups são os grupos da conta, por nome.
func (s *Service) Groups(ctx context.Context, owner uuid.UUID) ([]Group, error) {
	rows, err := s.repo.db.Query(ctx, `
		SELECT g.id, g.name, (SELECT count(*) FROM vehicles v WHERE v.group_id = g.id)
		FROM vehicle_groups g WHERE g.owner_id = $1 ORDER BY lower(g.name)`, owner)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	out := []Group{}
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.ID, &g.Name, &g.Vehicles); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// CreateGroup cria um grupo na conta.
func (s *Service) CreateGroup(ctx context.Context, owner uuid.UUID, name string) (*Group, error) {
	name, err := groupName(name)
	if err != nil {
		return nil, err
	}
	var count int
	if err := s.repo.db.QueryRow(ctx, `SELECT count(*) FROM vehicle_groups WHERE owner_id = $1`, owner).Scan(&count); err != nil {
		return nil, database.MapError(err)
	}
	if count >= MaxGroups {
		return nil, ValidationError{Message: fmt.Sprintf("até %d grupos por conta", MaxGroups)}
	}
	g := Group{Name: name}
	err = s.repo.db.QueryRow(ctx, `INSERT INTO vehicle_groups (owner_id, name) VALUES ($1, $2) RETURNING id`, owner, name).Scan(&g.ID)
	if err = database.MapError(err); errors.Is(err, database.ErrConflict) {
		return nil, ErrGroupNameTaken
	}
	return &g, err
}

// RenameGroup troca o nome de um grupo da conta.
func (s *Service) RenameGroup(ctx context.Context, owner, id uuid.UUID, name string) error {
	name, err := groupName(name)
	if err != nil {
		return err
	}
	tag, err := s.repo.db.Exec(ctx, `UPDATE vehicle_groups SET name = $3 WHERE id = $1 AND owner_id = $2`, id, owner, name)
	if err = database.MapError(err); errors.Is(err, database.ErrConflict) {
		return ErrGroupNameTaken
	}
	if err == nil && tag.RowsAffected() == 0 {
		return database.ErrNotFound
	}
	return err
}

// DeleteGroup apaga um grupo da conta; os veículos dele ficam sem grupo.
func (s *Service) DeleteGroup(ctx context.Context, owner, id uuid.UUID) error {
	tag, err := s.repo.db.Exec(ctx, `DELETE FROM vehicle_groups WHERE id = $1 AND owner_id = $2`, id, owner)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return database.ErrNotFound
	}
	return nil
}

// Labels são os nomes dos campos personalizados da conta.
func (s *Service) Labels(ctx context.Context, owner uuid.UUID) (FieldLabels, error) {
	var l FieldLabels
	err := s.repo.db.QueryRow(ctx, `SELECT label1, label2 FROM vehicle_field_labels WHERE customer_id = $1`, owner).
		Scan(&l.Label1, &l.Label2)
	if errors.Is(err, pgx.ErrNoRows) {
		return FieldLabels{}, nil
	}
	return l, database.MapError(err)
}

// SetLabels dá nome aos dois campos personalizados da conta.
func (s *Service) SetLabels(ctx context.Context, owner uuid.UUID, l FieldLabels) (FieldLabels, error) {
	l.Label1, l.Label2 = strings.TrimSpace(l.Label1), strings.TrimSpace(l.Label2)
	for _, label := range []string{l.Label1, l.Label2} {
		if utf8.RuneCountInString(label) > MaxLabelLength {
			return l, ValidationError{Message: fmt.Sprintf("o nome do campo vai até %d caracteres", MaxLabelLength)}
		}
	}
	_, err := s.repo.db.Exec(ctx, `
		INSERT INTO vehicle_field_labels (customer_id, label1, label2) VALUES ($1, $2, $3)
		ON CONFLICT (customer_id) DO UPDATE SET label1 = EXCLUDED.label1, label2 = EXCLUDED.label2, updated_at = NOW()`,
		owner, l.Label1, l.Label2)
	return l, database.MapError(err)
}
