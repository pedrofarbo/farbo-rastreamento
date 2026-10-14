package devices

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

const (
	// unknownWriteEvery: as tentativas de um mesmo IMEI desconhecido vão ao
	// banco no máximo uma vez nesse intervalo (ele tenta a cada minuto, ou
	// mais).
	unknownWriteEvery = 30 * time.Second
	// unknownKeep: quanto tempo a tentativa fica guardada.
	unknownKeep = 30 * 24 * time.Hour
	// unknownMaxPending: teto de IMEIs na memória do intervalo (um scanner
	// mandando lixo não enche a memória).
	unknownMaxPending = 5000
	// UnknownWindow: até quando a tela mostra uma tentativa.
	UnknownWindow = 7 * 24 * time.Hour
)

// UnknownAttempt é um rastreador que tentou conectar sem estar cadastrado.
type UnknownAttempt struct {
	IMEI        string    `json:"imei"`
	Protocol    string    `json:"protocol"`
	RemoteAddr  string    `json:"remoteAddr"`
	FirstSeenAt time.Time `json:"firstSeenAt"`
	LastSeenAt  time.Time `json:"lastSeenAt"`
	Attempts    int       `json:"attempts"`
	// Suggestion é o cadastro de IMEI parecido: o provável erro de digitação.
	Suggestion *IMEISuggestion `json:"suggestion"`
}

// IMEISuggestion é o rastreador cadastrado com o IMEI parecido.
type IMEISuggestion struct {
	DeviceID uuid.UUID `json:"deviceId"`
	IMEI     string    `json:"imei"`
}

// pendingUnknown é o que ainda não foi ao banco de um IMEI desconhecido.
type pendingUnknown struct {
	written time.Time
	count   int
}

// RecordUnknown anota a tentativa de um rastreador não cadastrado. Vai ao
// banco no máximo a cada unknownWriteEvery por IMEI; uma falha só vai para
// quem chama registrar no log.
func (s *Service) RecordUnknown(ctx context.Context, imei, protocol, remote string) error {
	now := time.Now()
	s.unknownMu.Lock()
	if s.unknown == nil || len(s.unknown) >= unknownMaxPending {
		s.unknown = map[string]*pendingUnknown{}
	}
	p := s.unknown[imei]
	if p == nil {
		p = &pendingUnknown{}
		s.unknown[imei] = p
	}
	p.count++
	if !p.written.IsZero() && now.Sub(p.written) < unknownWriteEvery {
		s.unknownMu.Unlock()
		return nil
	}
	count := p.count
	p.written, p.count = now, 0
	prune := now.Sub(s.unknownPruned) > 24*time.Hour
	if prune {
		s.unknownPruned = now
	}
	s.unknownMu.Unlock()

	if prune {
		if _, err := s.repo.db.Exec(ctx, `DELETE FROM unknown_device_attempts WHERE last_seen_at < $1`, now.Add(-unknownKeep)); err != nil {
			return database.MapError(err)
		}
	}
	_, err := s.repo.db.Exec(ctx, `
		INSERT INTO unknown_device_attempts (imei, protocol, remote_addr, attempts) VALUES ($1, $2, $3, $4)
		ON CONFLICT (imei) DO UPDATE SET protocol = EXCLUDED.protocol, remote_addr = EXCLUDED.remote_addr,
			last_seen_at = NOW(), attempts = unknown_device_attempts.attempts + EXCLUDED.attempts`,
		imei, protocol, remote, count)
	return database.MapError(err)
}

// UnknownAttempts são as tentativas recentes de rastreadores que continuam
// sem cadastro (desde since), as mais recentes primeiro, cada uma com o
// cadastro de IMEI parecido, quando houver.
func (s *Service) UnknownAttempts(ctx context.Context, since time.Time) ([]*UnknownAttempt, error) {
	rows, err := s.repo.db.Query(ctx, `
		SELECT u.imei, u.protocol, u.remote_addr, u.first_seen_at, u.last_seen_at, u.attempts
		FROM unknown_device_attempts u
		WHERE u.last_seen_at >= $1 AND NOT EXISTS (SELECT 1 FROM devices d WHERE d.imei = u.imei)
		ORDER BY u.last_seen_at DESC LIMIT 100`, since)
	if err != nil {
		return nil, database.MapError(err)
	}
	out := []*UnknownAttempt{}
	for rows.Next() {
		a := &UnknownAttempt{}
		if err := rows.Scan(&a.IMEI, &a.Protocol, &a.RemoteAddr, &a.FirstSeenAt, &a.LastSeenAt, &a.Attempts); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, database.MapError(err)
	}
	if len(out) == 0 {
		return out, nil
	}

	// O cadastro mais parecido com cada um (o IMEI digitado errado).
	registered, err := s.repo.db.Query(ctx, `SELECT id, imei FROM devices`)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer registered.Close()
	best := make([]int, len(out))
	for i := range best {
		best[i] = 3
	}
	for registered.Next() {
		var id uuid.UUID
		var imei string
		if err := registered.Scan(&id, &imei); err != nil {
			return nil, err
		}
		for i, a := range out {
			if !SimilarIMEI(a.IMEI, imei) {
				continue
			}
			if d := editDistance(a.IMEI, imei); d < best[i] {
				best[i], a.Suggestion = d, &IMEISuggestion{DeviceID: id, IMEI: imei}
			}
		}
	}
	return out, database.MapError(registered.Err())
}

// UnknownLike é a tentativa recente (desde since) de um rastreador sem
// cadastro com o IMEI parecido com este — o rastreador deste cadastro
// conectando com o IMEI de verdade. Nil se não houve.
func (s *Service) UnknownLike(ctx context.Context, imei string, since time.Time) (*UnknownAttempt, error) {
	list, err := s.UnknownAttempts(ctx, since)
	if err != nil {
		return nil, err
	}
	for _, a := range list {
		if SimilarIMEI(a.IMEI, imei) {
			return a, nil
		}
	}
	return nil, nil
}
