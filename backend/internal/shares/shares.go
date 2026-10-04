// Package shares guarda os acessos de terceiros: o cliente dá a outra pessoa,
// que entra com a própria conta, o acompanhamento de um veículo dele — a
// posição ao vivo — e, se quiser, o bloqueio do motor numa emergência (o
// celular roubado junto com o veículo, por exemplo). Desbloquear, ver o
// histórico, os eventos e as cercas continua só com o dono.
package shares

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/mail"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/push"
)

// MaxPerVehicle é quantas pessoas podem acompanhar um mesmo veículo.
const MaxPerVehicle = 5

// notifyTimeout limita cada e-mail ou notificação mandado em segundo plano.
const notifyTimeout = 30 * time.Second

// Share é um acesso. Para o dono, interessa quem recebeu (Guest*); para quem
// recebeu, de quem é o veículo (OwnerName). O e-mail do dono não sai daqui.
type Share struct {
	ID           uuid.UUID `json:"id"`
	VehicleID    uuid.UUID `json:"vehicleId"`
	VehicleName  string    `json:"vehicleName"`
	VehiclePlate string    `json:"vehiclePlate"`
	OwnerID      uuid.UUID `json:"-"`
	OwnerName    string    `json:"ownerName"`
	GuestID      uuid.UUID `json:"guestId"`
	GuestName    string    `json:"guestName"`
	GuestEmail   string    `json:"guestEmail"`
	// CanBlock: pode bloquear o motor (nunca desbloquear).
	CanBlock  bool      `json:"canBlock"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Input é o pedido do dono: para quem e o que ele pode.
type Input struct {
	VehicleID uuid.UUID
	Name      string
	Email     string
	CanBlock  bool
}

// InputError é um dado que o dono consegue corrigir.
type InputError struct{ Reason string }

func (e *InputError) Error() string { return e.Reason }

// ErrAlreadyShared: a pessoa já acompanha este veículo.
var ErrAlreadyShared = errors.New("essa pessoa já tem acesso a este veículo")

// Accounts é o que o cadastro precisa das contas (auth.Service).
type Accounts interface {
	GetUser(ctx context.Context, id uuid.UUID) (*auth.User, error)
	FindByEmail(ctx context.Context, email string) (*auth.User, error)
	Register(ctx context.Context, in auth.NewUser) (*auth.User, error)
	IssueInvite(ctx context.Context, user *auth.User) (string, time.Duration, error)
}

// Notifier manda os e-mails dos acessos (mail.ShareMailer).
type Notifier interface {
	GuestInvited(ctx context.Context, to string, n mail.ShareNotice, token string, ttl time.Duration) error
	GuestAdded(ctx context.Context, to string, n mail.ShareNotice) error
	OwnerShared(ctx context.Context, to string, n mail.ShareNotice) error
	GuestBlocked(ctx context.Context, to string, n mail.ShareNotice) error
}

// Pusher avisa no celular do dono (push.Service). Opcional.
type Pusher interface {
	Notify(ctx context.Context, userID uuid.UUID, n push.Notification) (int, error)
}

type Service struct {
	db       *database.DB
	accounts Accounts
	notifier Notifier
	pusher   Pusher
	log      *slog.Logger
	// runAsync manda os avisos fora da requisição; os testes trocam por uma
	// chamada síncrona.
	runAsync func(func())
}

func NewService(db *database.DB, accounts Accounts, notifier Notifier, log *slog.Logger) *Service {
	return &Service{
		db: db, accounts: accounts, notifier: notifier, log: log.With("component", "shares"),
		runAsync: func(f func()) { go f() },
	}
}

// SetPusher liga o aviso no celular do dono quando alguém bloqueia.
func (s *Service) SetPusher(p Pusher) { s.pusher = p }

// SetSync faz os avisos saírem na hora (testes).
func (s *Service) SetSync() { s.runAsync = func(f func()) { f() } }

// O acesso só vale enquanto quem o deu for o dono do veículo, e para conta
// ativa.
const selectShares = `
	SELECT s.id, s.vehicle_id, v.name, COALESCE(v.plate, ''), s.owner_id, o.name, s.guest_id, g.name, g.email,
	       s.can_block, s.created_at, s.updated_at
	FROM vehicle_shares s
	JOIN vehicles v ON v.id = s.vehicle_id AND v.owner_id = s.owner_id
	JOIN users o ON o.id = s.owner_id
	JOIN users g ON g.id = s.guest_id`

func scan(row database.Scanner) (*Share, error) {
	var sh Share
	err := row.Scan(&sh.ID, &sh.VehicleID, &sh.VehicleName, &sh.VehiclePlate, &sh.OwnerID, &sh.OwnerName,
		&sh.GuestID, &sh.GuestName, &sh.GuestEmail, &sh.CanBlock, &sh.CreatedAt, &sh.UpdatedAt)
	if err != nil {
		return nil, database.MapError(err)
	}
	return &sh, nil
}

func (s *Service) list(ctx context.Context, where string, args ...any) ([]*Share, error) {
	rows, err := s.db.Query(ctx, selectShares+" "+where, args...)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	out := []*Share{}
	for rows.Next() {
		sh, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sh)
	}
	return out, database.MapError(rows.Err())
}

// ByOwner: os acessos que o dono deu, de todos os veículos dele.
func (s *Service) ByOwner(ctx context.Context, ownerID uuid.UUID) ([]*Share, error) {
	return s.list(ctx, `WHERE s.owner_id = $1 ORDER BY v.name, g.name`, ownerID)
}

// ForGuest: os veículos que a pessoa acompanha.
func (s *Service) ForGuest(ctx context.Context, guestID uuid.UUID) ([]*Share, error) {
	return s.list(ctx, `WHERE s.guest_id = $1 AND g.active ORDER BY v.name`, guestID)
}

// Access é o acesso da pessoa a este veículo (database.ErrNotFound se não
// tiver).
func (s *Service) Access(ctx context.Context, guestID, vehicleID uuid.UUID) (*Share, error) {
	return scan(s.db.QueryRow(ctx, selectShares+` WHERE s.guest_id = $1 AND s.vehicle_id = $2 AND g.active`,
		guestID, vehicleID))
}

func (s *Service) get(ctx context.Context, id uuid.UUID) (*Share, error) {
	return scan(s.db.QueryRow(ctx, selectShares+` WHERE s.id = $1`, id))
}

// Create dá o acesso. Sem conta com o e-mail, ela é criada e a pessoa
// recebe o convite para criar a senha; com conta de cliente, só o aviso. O
// dono recebe um aviso de segurança (se não foi ele, remove e troca a
// senha). Devolve se a conta foi criada agora.
func (s *Service) Create(ctx context.Context, ownerID uuid.UUID, in Input) (*Share, bool, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	switch {
	case in.Name == "":
		return nil, false, &InputError{"informe o nome da pessoa"}
	case !strings.Contains(in.Email, "@") || strings.ContainsAny(in.Email, " \t\r\n<>"):
		return nil, false, &InputError{"informe um e-mail válido"}
	}

	var vehicleName string
	var shared int
	err := s.db.QueryRow(ctx, `
		SELECT v.name, (SELECT count(*) FROM vehicle_shares s WHERE s.vehicle_id = v.id AND s.owner_id = v.owner_id)
		FROM vehicles v WHERE v.id = $1 AND v.owner_id = $2`, in.VehicleID, ownerID).Scan(&vehicleName, &shared)
	if err != nil {
		return nil, false, database.MapError(err)
	}
	if shared >= MaxPerVehicle {
		return nil, false, &InputError{fmt.Sprintf("cada veículo pode ser acompanhado por até %d pessoas", MaxPerVehicle)}
	}

	owner, err := s.accounts.GetUser(ctx, ownerID)
	if err != nil {
		return nil, false, err
	}
	if strings.EqualFold(owner.Email, in.Email) {
		return nil, false, &InputError{"esse é o e-mail da sua conta: você já acompanha o veículo"}
	}

	guest, created, err := s.guestAccount(ctx, in)
	if err != nil {
		return nil, false, err
	}

	var id uuid.UUID
	err = s.db.QueryRow(ctx, `
		INSERT INTO vehicle_shares (vehicle_id, owner_id, guest_id, can_block) VALUES ($1, $2, $3, $4)
		RETURNING id`, in.VehicleID, ownerID, guest.ID, in.CanBlock).Scan(&id)
	if err != nil {
		if errors.Is(database.MapError(err), database.ErrConflict) {
			return nil, false, ErrAlreadyShared
		}
		return nil, false, database.MapError(err)
	}
	share, err := s.get(ctx, id)
	if err != nil {
		return nil, false, err
	}

	notice := noticeOf(share)
	if created {
		token, ttl, err := s.accounts.IssueInvite(ctx, guest)
		if err != nil {
			s.log.Error("acesso criado, mas o convite não foi gerado", "share", id, "err", err)
		} else {
			s.notify("convite do acesso", func(ctx context.Context) error {
				return s.notifier.GuestInvited(ctx, guest.Email, notice, token, ttl)
			})
		}
	} else {
		s.notify("aviso do acesso", func(ctx context.Context) error {
			return s.notifier.GuestAdded(ctx, guest.Email, notice)
		})
	}
	s.notify("aviso ao dono", func(ctx context.Context) error {
		return s.notifier.OwnerShared(ctx, owner.Email, notice)
	})
	return share, created, nil
}

// guestAccount acha a conta de cliente com o e-mail, ou cria uma (sem senha
// conhecida: a pessoa cria pelo convite).
func (s *Service) guestAccount(ctx context.Context, in Input) (*auth.User, bool, error) {
	for attempt := 0; attempt < 2; attempt++ {
		user, err := s.accounts.FindByEmail(ctx, in.Email)
		switch {
		case err == nil:
			if user.Role != auth.RoleCustomer {
				return nil, false, &InputError{"esse e-mail não pode receber acesso a veículos"}
			}
			if !user.Active {
				return nil, false, &InputError{"a conta com esse e-mail está desativada; fale com a central"}
			}
			return user, false, nil
		case !errors.Is(err, database.ErrNotFound):
			return nil, false, err
		}

		password, err := auth.RandomPassword()
		if err != nil {
			return nil, false, err
		}
		user, err = s.accounts.Register(ctx, auth.NewUser{
			Email: in.Email, Name: in.Name, Role: auth.RoleCustomer, Password: password,
		})
		if err == nil {
			return user, true, nil
		}
		// Criada ao mesmo tempo por outro pedido: na volta, ela já existe.
		if !errors.Is(err, database.ErrConflict) {
			return nil, false, &InputError{err.Error()}
		}
	}
	return nil, false, database.ErrConflict
}

// SetCanBlock muda a permissão de bloqueio. Ao ligar, o dono recebe o
// aviso de segurança de novo.
func (s *Service) SetCanBlock(ctx context.Context, ownerID, id uuid.UUID, canBlock bool) (before, after *Share, err error) {
	if before, err = s.get(ctx, id); err != nil {
		return nil, nil, err
	}
	if before.OwnerID != ownerID {
		return nil, nil, database.ErrNotFound
	}
	tag, err := s.db.Exec(ctx, `UPDATE vehicle_shares SET can_block = $3, updated_at = NOW() WHERE id = $1 AND owner_id = $2`,
		id, ownerID, canBlock)
	if err != nil {
		return nil, nil, database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return nil, nil, database.ErrNotFound
	}
	if after, err = s.get(ctx, id); err != nil {
		return nil, nil, err
	}
	if canBlock && !before.CanBlock {
		if owner, err := s.accounts.GetUser(ctx, ownerID); err == nil {
			notice := noticeOf(after)
			s.notify("aviso ao dono", func(ctx context.Context) error {
				return s.notifier.OwnerShared(ctx, owner.Email, notice)
			})
		}
	}
	return before, after, nil
}

// Remove tira o acesso. Vale para o dono e para quem recebeu (que pode
// deixar de acompanhar). O efeito é imediato: cada pedido confere o acesso.
func (s *Service) Remove(ctx context.Context, userID, id uuid.UUID) (*Share, error) {
	share, err := scan(s.db.QueryRow(ctx, `
		WITH gone AS (
			DELETE FROM vehicle_shares WHERE id = $1 AND (owner_id = $2 OR guest_id = $2)
			RETURNING id, vehicle_id, owner_id, guest_id, can_block, created_at, updated_at
		)
		SELECT gone.id, gone.vehicle_id, COALESCE(v.name, ''), COALESCE(v.plate, ''), gone.owner_id, o.name,
		       gone.guest_id, g.name, g.email, gone.can_block, gone.created_at, gone.updated_at
		FROM gone
		LEFT JOIN vehicles v ON v.id = gone.vehicle_id
		JOIN users o ON o.id = gone.owner_id
		JOIN users g ON g.id = gone.guest_id`, id, userID))
	if err != nil {
		return nil, err
	}
	return share, nil
}

// GuestBlocked avisa o dono (e-mail e celular) de que quem tem acesso
// bloqueou o motor.
func (s *Service) GuestBlocked(ctx context.Context, share *Share, at time.Time) {
	owner, err := s.accounts.GetUser(ctx, share.OwnerID)
	if err != nil {
		s.log.Error("bloqueio por terceiro sem aviso ao dono", "share", share.ID, "err", err)
		return
	}
	notice := noticeOf(share)
	notice.At = at
	s.notify("bloqueio por terceiro", func(ctx context.Context) error {
		return s.notifier.GuestBlocked(ctx, owner.Email, notice)
	})
	if s.pusher != nil {
		s.notify("bloqueio por terceiro (celular)", func(ctx context.Context) error {
			_, err := s.pusher.Notify(ctx, share.OwnerID, push.Notification{
				Title:    "Motor bloqueado por " + share.GuestName,
				Body:     fmt.Sprintf("%s pediu o bloqueio de %s pelo acesso que você deu.", share.GuestName, share.VehicleName),
				URL:      "/app/veiculos/" + share.VehicleID.String(),
				Tag:      "bloqueio-" + share.VehicleID.String(),
				Severity: "critical",
				Urgency:  "high",
			})
			return err
		})
	}
}

func (s *Service) notify(kind string, send func(ctx context.Context) error) {
	if s.notifier == nil {
		s.log.Error("aviso não enviado: nenhum Notifier configurado", "aviso", kind)
		return
	}
	s.runAsync(func() {
		ctx, cancel := context.WithTimeout(context.Background(), notifyTimeout)
		defer cancel()
		if err := send(ctx); err != nil {
			s.log.Error("falha ao enviar aviso", "aviso", kind, "err", err)
		}
	})
}

func noticeOf(sh *Share) mail.ShareNotice {
	return mail.ShareNotice{
		VehicleID: sh.VehicleID.String(), VehicleName: sh.VehicleName, VehiclePlate: sh.VehiclePlate,
		OwnerName: sh.OwnerName, GuestName: sh.GuestName, GuestEmail: sh.GuestEmail, CanBlock: sh.CanBlock,
	}
}
