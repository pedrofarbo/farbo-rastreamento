// Package apikeys são as chaves da API de integração: a conta do cliente (ou
// a associação/seguradora) gera uma chave para o sistema dela ler os
// veículos, as posições e os eventos (/api/v1). A chave aparece uma vez só;
// aqui fica o hash.
package apikeys

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// MaxKeys: quantas chaves ativas uma conta pode ter.
const MaxKeys = 10

var (
	// ErrInvalid: a chave não existe, foi revogada ou está malformada.
	ErrInvalid = errors.New("chave da API inválida ou revogada")
	encoding   = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)
)

// InputError é um pedido recusado; a mensagem vai para a tela.
type InputError struct{ Message string }

func (e *InputError) Error() string { return e.Message }

// Key é uma chave (sem o segredo).
type Key struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
	RevokedAt  *time.Time `json:"revokedAt"`
}

type Service struct {
	db  *database.DB
	now func() time.Time
}

func NewService(db *database.DB) *Service { return &Service{db: db, now: time.Now} }

func random(n int) string {
	raw := make([]byte, n)
	_, _ = rand.Read(raw)
	return encoding.EncodeToString(raw)
}

func hash(key string) []byte {
	sum := sha256.Sum256([]byte(key))
	return sum[:]
}

// List são as chaves da conta, das mais novas.
func (s *Service) List(ctx context.Context, owner uuid.UUID) ([]Key, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, name, prefix, created_at, last_used_at, revoked_at FROM api_keys
		WHERE owner_id = $1 ORDER BY revoked_at IS NULL DESC, created_at DESC`, owner)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	out := []Key{}
	for rows.Next() {
		var k Key
		if err := rows.Scan(&k.ID, &k.Name, &k.Prefix, &k.CreatedAt, &k.LastUsedAt, &k.RevokedAt); err != nil {
			return nil, database.MapError(err)
		}
		out = append(out, k)
	}
	return out, database.MapError(rows.Err())
}

// Create gera a chave e devolve o segredo (a única vez que ele aparece):
// fk_<prefixo>_<segredo>.
func (s *Service) Create(ctx context.Context, owner, by uuid.UUID, name string) (*Key, string, error) {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" || utf8.RuneCountInString(name) > 60 {
		return nil, "", &InputError{"dê um nome à chave (até 60 caracteres): o sistema que vai usar"}
	}
	var active int
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM api_keys WHERE owner_id = $1 AND revoked_at IS NULL`, owner).
		Scan(&active); err != nil {
		return nil, "", database.MapError(err)
	}
	if active >= MaxKeys {
		return nil, "", &InputError{fmt.Sprintf("são até %d chaves ativas: revogue uma para gerar outra", MaxKeys)}
	}
	prefix := random(5)
	secret := "fk_" + prefix + "_" + random(20)
	var k Key
	if err := s.db.QueryRow(ctx, `
		INSERT INTO api_keys (owner_id, name, prefix, hash, created_by) VALUES ($1, $2, $3, $4, $5)
		RETURNING id, name, prefix, created_at, last_used_at, revoked_at`, owner, name, prefix, hash(secret), by).
		Scan(&k.ID, &k.Name, &k.Prefix, &k.CreatedAt, &k.LastUsedAt, &k.RevokedAt); err != nil {
		return nil, "", database.MapError(err)
	}
	return &k, secret, nil
}

// Revoke revoga a chave da conta (o sistema que a usa para na hora).
func (s *Service) Revoke(ctx context.Context, owner, id uuid.UUID) error {
	tag, err := s.db.Exec(ctx, `
		UPDATE api_keys SET revoked_at = NOW() WHERE id = $1 AND owner_id = $2 AND revoked_at IS NULL`, id, owner)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return database.ErrNotFound
	}
	return nil
}

// Authenticate é a conta da chave (ErrInvalid se não vale). O último uso é
// guardado no máximo uma vez por minuto.
func (s *Service) Authenticate(ctx context.Context, key string) (uuid.UUID, error) {
	key = strings.TrimSpace(key)
	parts := strings.Split(key, "_")
	if len(parts) != 3 || parts[0] != "fk" || parts[1] == "" {
		return uuid.Nil, ErrInvalid
	}
	var id, owner uuid.UUID
	var stored []byte
	var revoked *time.Time
	err := s.db.QueryRow(ctx, `SELECT id, owner_id, hash, revoked_at FROM api_keys WHERE prefix = $1`, parts[1]).
		Scan(&id, &owner, &stored, &revoked)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrInvalid
	}
	if err != nil {
		return uuid.Nil, database.MapError(err)
	}
	if revoked != nil || subtle.ConstantTimeCompare(stored, hash(key)) != 1 {
		return uuid.Nil, ErrInvalid
	}
	if _, err := s.db.Exec(ctx, `
		UPDATE api_keys SET last_used_at = $2 WHERE id = $1 AND (last_used_at IS NULL OR last_used_at < $3)`,
		id, s.now(), s.now().Add(-time.Minute)); err != nil {
		return uuid.Nil, database.MapError(err)
	}
	return owner, nil
}
