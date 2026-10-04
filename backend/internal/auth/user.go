// Package auth trata autenticação (JWT + refresh token) e autorização (RBAC).
package auth

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// Perfis de acesso (RBAC, §27).
const (
	RoleAdmin    = "admin"    // tudo, inclusive cadastro e usuários
	RoleOperator = "operator" // opera o painel e envia comandos
	RoleViewer   = "viewer"   // somente leitura
	// RoleCustomer é o cliente final: enxerga apenas os próprios veículos e
	// faturas. Os outros três perfis são a equipe da central.
	RoleCustomer = "customer"
)

func ValidRole(role string) bool {
	return TeamRole(role) || role == RoleCustomer
}

// TeamRole: os perfis da equipe da central. O cliente tem cadastro próprio
// (em Clientes), com contato, endereço e assinaturas.
func TeamRole(role string) bool {
	switch role {
	case RoleAdmin, RoleOperator, RoleViewer:
		return true
	}
	return false
}

var (
	// ErrNotTeam: o usuário é cliente, não da equipe.
	ErrNotTeam = errors.New("o usuário não é da equipe")
	// ErrLastAdmin: a mudança deixaria o painel sem administrador ativo.
	ErrLastAdmin = errors.New("o painel precisa de pelo menos um administrador ativo")
	// ErrSelfChange: ninguém muda o próprio perfil nem se desativa (outro
	// administrador faz isso).
	ErrSelfChange = errors.New("você não pode mudar o próprio perfil nem se desativar")
)

type User struct {
	ID    uuid.UUID `json:"id"`
	Email string    `json:"email"`
	Name  string    `json:"name"`
	Role  string    `json:"role"`

	// Contato e documento (CPF/CNPJ) do cliente; vazios para a equipe.
	Phone    string `json:"phone"`
	Document string `json:"document"`

	// PasswordHash nunca sai em JSON.
	PasswordHash string `json:"-"`

	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

const userColumns = `id, email, name, role, phone, document, password_hash, active, created_at, updated_at`

type Repository struct{ db *database.DB }

func NewRepository(db *database.DB) *Repository { return &Repository{db: db} }

func scanUser(row database.Scanner) (*User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.Phone, &u.Document, &u.PasswordHash,
		&u.Active, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, database.MapError(err)
	}
	return &u, nil
}

func (r *Repository) Create(ctx context.Context, u *User) error {
	return database.MapError(r.db.QueryRow(ctx, `
		INSERT INTO users (email, name, role, phone, document, password_hash)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at, updated_at`,
		strings.TrimSpace(u.Email), u.Name, u.Role, u.Phone, u.Document, u.PasswordHash,
	).Scan(&u.ID, &u.CreatedAt, &u.UpdatedAt))
}

func (r *Repository) GetByEmail(ctx context.Context, email string) (*User, error) {
	return scanUser(r.db.QueryRow(ctx,
		`SELECT `+userColumns+` FROM users WHERE lower(email) = lower($1)`, strings.TrimSpace(email)))
}

func (r *Repository) GetByID(ctx context.Context, id uuid.UUID) (*User, error) {
	return scanUser(r.db.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id = $1`, id))
}

// ListTeam lista a equipe da central (sem os clientes).
func (r *Repository) ListTeam(ctx context.Context) ([]*User, error) {
	rows, err := r.db.Query(ctx, `SELECT `+userColumns+` FROM users WHERE role <> $1 ORDER BY created_at`, RoleCustomer)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()

	out := []*User{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// UpdateProfile altera nome, contato e situação. Desativar também encerra
// as sessões abertas, para o acesso cair na hora e não só no próximo login.
func (r *Repository) UpdateProfile(ctx context.Context, id uuid.UUID, p Profile) (*User, error) {
	user, err := scanUser(r.db.QueryRow(ctx, `
		UPDATE users SET name = $2, phone = $3, document = $4, active = $5, updated_at = NOW()
		WHERE id = $1
		RETURNING `+userColumns, id, p.Name, p.Phone, p.Document, p.Active))
	if err != nil {
		return nil, err
	}
	if !user.Active {
		if err := r.RevokeAllForUser(ctx, id); err != nil {
			return nil, err
		}
	}
	return user, nil
}

// Member são os dados de alguém da equipe que um administrador altera.
type Member struct {
	Name   string
	Role   string
	Active bool
}

// UpdateMember altera nome, perfil e situação de alguém da equipe, a pedido
// de actor, e devolve como estava antes e como ficou.
//
// Roda numa transação que trava os administradores ativos (sempre na mesma
// ordem): duas mudanças ao mesmo tempo não deixam o painel sem nenhum. Quem
// muda de perfil ou é desativado perde as sessões abertas — o perfil vai no
// token de acesso, e o novo vale a partir do próximo login.
func (r *Repository) UpdateMember(ctx context.Context, actor, id uuid.UUID, m Member) (before, after *User, err error) {
	err = pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id FROM users WHERE role = $1 AND active ORDER BY id FOR UPDATE`, RoleAdmin)
		if err != nil {
			return err
		}
		admins := 0
		for rows.Next() {
			admins++
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}

		if before, err = scanUser(tx.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id = $1 FOR UPDATE`, id)); err != nil {
			return err
		}
		switch {
		case !TeamRole(before.Role):
			return ErrNotTeam
		case id == actor && (m.Role != before.Role || !m.Active):
			return ErrSelfChange
		case before.Role == RoleAdmin && before.Active && !(m.Role == RoleAdmin && m.Active) && admins <= 1:
			return ErrLastAdmin
		}

		if after, err = scanUser(tx.QueryRow(ctx, `
			UPDATE users SET name = $2, role = $3, active = $4, updated_at = NOW()
			WHERE id = $1
			RETURNING `+userColumns, id, m.Name, m.Role, m.Active)); err != nil {
			return err
		}
		if after.Role != before.Role || !after.Active {
			_, err = tx.Exec(ctx,
				`UPDATE refresh_tokens SET revoked_at = NOW() WHERE user_id = $1 AND revoked_at IS NULL`, id)
		}
		return err
	})
	if err != nil {
		return nil, nil, database.MapError(err)
	}
	return before, after, nil
}

func (r *Repository) Count(ctx context.Context) (int, error) {
	var n int
	err := r.db.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&n)
	return n, database.MapError(err)
}

// ---------------------------------------------------------------------------
// Refresh tokens
// ---------------------------------------------------------------------------

// StoreRefreshToken guarda apenas o hash: o valor em claro fica só no cliente.
// keyID identifica o JWT_SECRET em uso (ver signingKeyID).
func (r *Repository) StoreRefreshToken(ctx context.Context, userID uuid.UUID, tokenHash, keyID string, expiresAt time.Time, userAgent string) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO refresh_tokens (user_id, token_hash, key_id, expires_at, user_agent)
		VALUES ($1, $2, $3, $4, $5)`, userID, tokenHash, keyID, expiresAt, userAgent)
	return database.MapError(err)
}

type refreshRecord struct {
	ID     uuid.UUID
	UserID uuid.UUID
}

// ConsumeRefreshToken valida e revoga o token numa única operação, de modo que
// um refresh token só possa ser usado uma vez (rotação). Token emitido com
// outro JWT_SECRET (keyID diferente) não vale.
func (r *Repository) ConsumeRefreshToken(ctx context.Context, tokenHash, keyID string) (*refreshRecord, error) {
	var rec refreshRecord
	err := r.db.QueryRow(ctx, `
		UPDATE refresh_tokens SET revoked_at = NOW()
		WHERE token_hash = $1 AND key_id = $2 AND revoked_at IS NULL AND expires_at > NOW()
		RETURNING id, user_id`, tokenHash, keyID).Scan(&rec.ID, &rec.UserID)
	if err != nil {
		return nil, database.MapError(err)
	}
	return &rec, nil
}

func (r *Repository) RevokeRefreshToken(ctx context.Context, tokenHash string) error {
	_, err := r.db.Exec(ctx,
		`UPDATE refresh_tokens SET revoked_at = NOW() WHERE token_hash = $1 AND revoked_at IS NULL`,
		tokenHash)
	return database.MapError(err)
}

func (r *Repository) RevokeAllForUser(ctx context.Context, userID uuid.UUID) error {
	_, err := r.db.Exec(ctx,
		`UPDATE refresh_tokens SET revoked_at = NOW() WHERE user_id = $1 AND revoked_at IS NULL`,
		userID)
	return database.MapError(err)
}

// RevokeRefreshTokensNotSignedBy revoga as sessões abertas com outro
// JWT_SECRET (ou de antes de a chave ser gravada, com key_id vazio).
func (r *Repository) RevokeRefreshTokensNotSignedBy(ctx context.Context, keyID string) (int64, error) {
	tag, err := r.db.Exec(ctx,
		`UPDATE refresh_tokens SET revoked_at = NOW() WHERE revoked_at IS NULL AND key_id <> $1`, keyID)
	if err != nil {
		return 0, database.MapError(err)
	}
	return tag.RowsAffected(), nil
}

// DeleteExpiredRefreshTokens limpa a tabela periodicamente.
func (r *Repository) DeleteExpiredRefreshTokens(ctx context.Context) (int64, error) {
	tag, err := r.db.Exec(ctx,
		`DELETE FROM refresh_tokens WHERE expires_at < NOW() - INTERVAL '30 days'`)
	if err != nil {
		return 0, database.MapError(err)
	}
	return tag.RowsAffected(), nil
}
