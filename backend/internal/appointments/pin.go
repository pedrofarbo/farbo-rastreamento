package appointments

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"math/big"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

/*
 * A senha do link do instalador que não é representante: a central passa a
 * senha junto com o link (o link sozinho, repassado ou vazado, não abre a
 * tela — nem o estoque de rastreadores e chips que a ativação mostra). O
 * link que o representante agenda pela página dele abre sem senha. Errada
 * demais, o link trava até a central gerar outra.
 */

// MaxPinFailures: quantas senhas erradas o link aceita antes de travar.
const MaxPinFailures = 10

var (
	// ErrPinRequired: o link pede a senha.
	ErrPinRequired = errors.New("digite a senha que a Farbo passou junto com o link")
	// ErrPinWrong: a senha não confere.
	ErrPinWrong = errors.New("senha errada: confira com a Farbo")
	// ErrPinLocked: senha errada vezes demais; a central gera outra.
	ErrPinLocked = errors.New("a senha foi digitada errada vezes demais: peça uma senha nova à Farbo")
)

// newPin é uma senha de 6 números, ao acaso.
func newPin() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

// checkPin confere a senha do link na tabela (installation_appointments ou
// standalone_installs): sem senha guardada, abre; errada, conta a tentativa.
func (s *Service) checkPin(ctx context.Context, table string, id uuid.UUID, pin string) error {
	var stored *string
	var failures int
	if err := s.db.QueryRow(ctx, `SELECT access_pin, pin_failures FROM `+table+` WHERE id = $1`, id).
		Scan(&stored, &failures); err != nil {
		return database.MapError(err)
	}
	switch {
	case stored == nil:
		return nil
	case failures >= MaxPinFailures:
		return ErrPinLocked
	case pin == "":
		return ErrPinRequired
	case subtle.ConstantTimeCompare([]byte(pin), []byte(*stored)) == 1:
		return nil
	}
	if _, err := s.db.Exec(ctx, `UPDATE `+table+` SET pin_failures = pin_failures + 1 WHERE id = $1`, id); err != nil {
		return database.MapError(err)
	}
	if failures+1 >= MaxPinFailures {
		return ErrPinLocked
	}
	return ErrPinWrong
}

// CheckPin confere a senha do link do agendamento.
func (s *Service) CheckPin(ctx context.Context, id uuid.UUID, pin string) error {
	return s.checkPin(ctx, "installation_appointments", id, pin)
}

// CheckStandalonePin confere a senha do link da instalação avulsa.
func (s *Service) CheckStandalonePin(ctx context.Context, id uuid.UUID, pin string) error {
	return s.checkPin(ctx, "standalone_installs", id, pin)
}

// renewPin troca a senha do link aberto e zera as tentativas.
func (s *Service) renewPin(ctx context.Context, table, open string, id uuid.UUID) error {
	pin, err := newPin()
	if err != nil {
		return err
	}
	tag, err := s.db.Exec(ctx, `UPDATE `+table+` SET access_pin = $2, pin_failures = 0, updated_at = NOW()
		WHERE id = $1 AND `+open, id, pin)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return invalid("o link já foi encerrado: não há senha para trocar")
	}
	return nil
}

// RenewPin gera outra senha para o link do agendamento (a antiga para de
// valer; o link que abria sem senha passa a pedir).
func (s *Service) RenewPin(ctx context.Context, id uuid.UUID) (*Appointment, error) {
	if err := s.renewPin(ctx, "installation_appointments", `status IN ('scheduled', 'arrived')`, id); err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// RenewStandalonePin é o RenewPin da instalação avulsa.
func (s *Service) RenewStandalonePin(ctx context.Context, id uuid.UUID) (*Standalone, error) {
	if err := s.renewPin(ctx, "standalone_installs", `status = 'open'`, id); err != nil {
		return nil, err
	}
	return s.GetStandalone(ctx, id)
}
