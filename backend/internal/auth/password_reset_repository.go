package auth

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// ---------------------------------------------------------------------------
// Tokens de redefinição de senha
// ---------------------------------------------------------------------------

// HasRecentPasswordReset diz se o usuário já pediu redefinição nos últimos
// `within`. A conta é feita no relógio do banco, o mesmo que grava created_at.
func (r *Repository) HasRecentPasswordReset(ctx context.Context, userID uuid.UUID, within time.Duration) (bool, error) {
	var recent bool
	err := r.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM password_reset_tokens
			WHERE user_id = $1 AND created_at > NOW() - make_interval(secs => $2)
		)`, userID, within.Seconds()).Scan(&recent)
	return recent, database.MapError(err)
}

// CreatePasswordReset guarda o hash do token novo e apaga os pedidos
// anteriores ainda não usados: só o link mais recente continua valendo.
func (r *Repository) CreatePasswordReset(ctx context.Context, userID uuid.UUID, tokenHash string, expiresAt time.Time) error {
	return database.MapError(pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`DELETE FROM password_reset_tokens WHERE user_id = $1 AND used_at IS NULL`, userID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO password_reset_tokens (user_id, token_hash, expires_at)
			VALUES ($1, $2, $3)`, userID, tokenHash, expiresAt)
		return err
	}))
}

// PasswordResetUser devolve o dono de um token ainda utilizável, sem
// consumi-lo. Serve para a tela avisar logo de cara que o link expirou.
func (r *Repository) PasswordResetUser(ctx context.Context, tokenHash string) (uuid.UUID, error) {
	var userID uuid.UUID
	err := r.db.QueryRow(ctx, `
		SELECT t.user_id
		FROM password_reset_tokens t
		JOIN users u ON u.id = t.user_id
		WHERE t.token_hash = $1 AND t.used_at IS NULL AND t.expires_at > NOW() AND u.active`,
		tokenHash).Scan(&userID)
	return userID, database.MapError(err)
}

// ResetPassword troca a senha numa transação só: consome o token, grava o
// novo hash, descarta os outros pedidos pendentes e revoga todas as sessões
// (refresh tokens) do usuário. Token inválido, vencido, já usado ou de
// usuário desativado devolve database.ErrNotFound e não altera nada.
func (r *Repository) ResetPassword(ctx context.Context, tokenHash, passwordHash string) (uuid.UUID, error) {
	var userID uuid.UUID
	err := pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			UPDATE password_reset_tokens SET used_at = NOW()
			WHERE token_hash = $1 AND used_at IS NULL AND expires_at > NOW()
			RETURNING user_id`, tokenHash).Scan(&userID); err != nil {
			return err
		}

		tag, err := tx.Exec(ctx, `
			UPDATE users SET password_hash = $2, updated_at = NOW()
			WHERE id = $1 AND active`, userID, passwordHash)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			// Usuário desativado depois do pedido: desfaz tudo.
			return pgx.ErrNoRows
		}

		if _, err := tx.Exec(ctx,
			`DELETE FROM password_reset_tokens WHERE user_id = $1 AND used_at IS NULL`, userID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`UPDATE refresh_tokens SET revoked_at = NOW() WHERE user_id = $1 AND revoked_at IS NULL`, userID); err != nil {
			return err
		}
		// E na hora: o token de acesso de quem estava logado também cai.
		_, err = tx.Exec(ctx, `UPDATE users SET sessions_valid_after = date_trunc('second', NOW()) WHERE id = $1`, userID)
		return err
	})
	return userID, database.MapError(err)
}

// DeleteStalePasswordResets limpa pedidos vencidos há mais de um dia. A
// trilha de quem pediu e quem trocou fica na auditoria.
func (r *Repository) DeleteStalePasswordResets(ctx context.Context) (int64, error) {
	tag, err := r.db.Exec(ctx,
		`DELETE FROM password_reset_tokens WHERE expires_at < NOW() - INTERVAL '1 day'`)
	if err != nil {
		return 0, database.MapError(err)
	}
	return tag.RowsAffected(), nil
}
