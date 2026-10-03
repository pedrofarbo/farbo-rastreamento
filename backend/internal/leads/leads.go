// Package leads guarda os pré-clientes: quem deixou o interesse na landing
// (nome, contato, plano, veículos) para a equipe entrar em contato e, se
// fechar, cadastrar o cliente.
package leads

import (
	"context"
	"errors"
	"log/slog"
	netmail "net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

const (
	StatusNew       = "NEW"
	StatusContacted = "CONTACTED"
	StatusConverted = "CONVERTED"
	StatusDiscarded = "DISCARDED"
)

// ErrNotFound: pré-cliente inexistente.
var ErrNotFound = database.ErrNotFound

// ValidationError é um dado recusado; a mensagem vai para a tela.
type ValidationError struct{ Message string }

func (e ValidationError) Error() string { return e.Message }

type Lead struct {
	ID           uuid.UUID  `json:"id"`
	Name         string     `json:"name"`
	Email        string     `json:"email"`
	Phone        string     `json:"phone"`
	City         string     `json:"city"`
	Plan         string     `json:"plan"`
	VehicleType  string     `json:"vehicleType"`
	VehicleCount int        `json:"vehicleCount"`
	Message      string     `json:"message"`
	Status       string     `json:"status"`
	Notes        string     `json:"notes"`
	CustomerID   *uuid.UUID `json:"customerId"`
	// OnLaunchList: o e-mail também está na lista de lançamento (e, com ela,
	// na promoção de pré-lançamento).
	OnLaunchList bool      `json:"onLaunchList"`
	Source       string    `json:"source"`
	ConsentAt    time.Time `json:"consentAt"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// Input é o que a landing manda.
type Input struct {
	Name         string
	Email        string
	Phone        string
	City         string
	Plan         string
	VehicleType  string
	VehicleCount int
	Message      string
	Consent      bool
	// JoinLaunch também inscreve o e-mail na lista de lançamento.
	JoinLaunch bool
}

// Normalize limpa e confere o cadastro de interesse.
func Normalize(in Input) (Input, error) {
	in.Name = strings.Join(strings.Fields(in.Name), " ")
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	in.Phone = strings.TrimSpace(in.Phone)
	in.City = strings.Join(strings.Fields(in.City), " ")
	in.Plan = strings.TrimSpace(in.Plan)
	in.VehicleType = strings.ToLower(strings.TrimSpace(in.VehicleType))
	in.Message = strings.TrimSpace(in.Message)

	switch n := utf8.RuneCountInString(in.Name); {
	case n < 2:
		return in, ValidationError{"informe o seu nome"}
	case n > 120:
		return in, ValidationError{"nome longo demais"}
	}
	if !validEmail(in.Email) {
		return in, ValidationError{"informe um e-mail válido"}
	}
	if !validPhone(in.Phone) {
		return in, ValidationError{"informe o seu WhatsApp com DDD"}
	}
	switch in.VehicleType {
	case "", "moto", "carro", "frota":
	default:
		return in, ValidationError{"tipo de veículo inválido"}
	}
	if in.VehicleCount == 0 {
		in.VehicleCount = 1
	}
	if in.VehicleCount < 1 || in.VehicleCount > 500 {
		return in, ValidationError{"quantidade de veículos inválida (1 a 500)"}
	}
	switch {
	case utf8.RuneCountInString(in.City) > 80:
		return in, ValidationError{"cidade longa demais"}
	case utf8.RuneCountInString(in.Plan) > 80:
		return in, ValidationError{"plano inválido"}
	case utf8.RuneCountInString(in.Message) > 1000:
		return in, ValidationError{"mensagem longa demais: até 1000 caracteres"}
	case !in.Consent:
		return in, ValidationError{"é preciso aceitar o contato da Farbo para enviar"}
	}
	return in, nil
}

// validPhone: celular ou fixo com DDD (10 ou 11 dígitos), com ou sem o 55.
func validPhone(phone string) bool {
	d := digits(phone)
	return len(d) >= 10 && len(d) <= 13 && len(phone) <= 30
}

// validEmail aceita só o endereço (sem nome na frente), com domínio de
// verdade (ana@exemplo, sem ponto, não passa).
func validEmail(email string) bool {
	addr, err := netmail.ParseAddress(email)
	return err == nil && addr.Address == email && len(email) <= 254 &&
		strings.Contains(email[strings.LastIndex(email, "@"):], ".")
}

func digits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// Repositório
// ---------------------------------------------------------------------------

type Repository struct{ db *database.DB }

func NewRepository(db *database.DB) *Repository { return &Repository{db: db} }

const leadColumns = `id, name, email, phone, city, plan, vehicle_type, vehicle_count, message, status, notes,
	customer_id, EXISTS (SELECT 1 FROM launch_waitlist w WHERE lower(w.email) = lower(leads.email)),
	source, consent_at, created_at, updated_at`

const selectLead = `SELECT ` + leadColumns + ` FROM leads`

func scan(row database.Scanner) (*Lead, error) {
	var l Lead
	if err := row.Scan(&l.ID, &l.Name, &l.Email, &l.Phone, &l.City, &l.Plan, &l.VehicleType, &l.VehicleCount,
		&l.Message, &l.Status, &l.Notes, &l.CustomerID, &l.OnLaunchList, &l.Source, &l.ConsentAt,
		&l.CreatedAt, &l.UpdatedAt); err != nil {
		return nil, database.MapError(err)
	}
	return &l, nil
}

// save grava o interesse. Quem já está na fila (novo ou em contato) com o
// mesmo e-mail é atualizado, em vez de virar outro pré-cliente: created diz
// qual dos dois aconteceu.
func (r *Repository) save(ctx context.Context, in Input) (*Lead, bool, error) {
	var lead *Lead
	created := false
	err := pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		var id uuid.UUID
		err := tx.QueryRow(ctx, `
			SELECT id FROM leads WHERE lower(email) = $1 AND status IN ('NEW', 'CONTACTED')
			ORDER BY created_at DESC LIMIT 1 FOR UPDATE`, in.Email).Scan(&id)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			created = true
			err = tx.QueryRow(ctx, `
				INSERT INTO leads (name, email, phone, city, plan, vehicle_type, vehicle_count, message, consent_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW()) RETURNING id`,
				in.Name, in.Email, in.Phone, in.City, in.Plan, in.VehicleType, in.VehicleCount, in.Message).Scan(&id)
		case err == nil:
			_, err = tx.Exec(ctx, `
				UPDATE leads SET name = $2, phone = COALESCE(NULLIF($3, ''), phone), city = COALESCE(NULLIF($4, ''), city),
					plan = $5, vehicle_type = $6, vehicle_count = $7, message = COALESCE(NULLIF($8, ''), message),
					consent_at = NOW(), updated_at = NOW()
				WHERE id = $1`,
				id, in.Name, in.Phone, in.City, in.Plan, in.VehicleType, in.VehicleCount, in.Message)
		}
		if err != nil {
			return err
		}
		lead, err = scan(tx.QueryRow(ctx, selectLead+` WHERE id = $1`, id))
		return err
	})
	return lead, created, database.MapError(err)
}

// List traz os pré-clientes, os mais recentes primeiro; status vazio traz
// todos.
func (r *Repository) List(ctx context.Context, status string) ([]*Lead, error) {
	rows, err := r.db.Query(ctx, selectLead+` WHERE $1 = '' OR status = $1 ORDER BY created_at DESC LIMIT 500`, status)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	out := []*Lead{}
	for rows.Next() {
		l, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, database.MapError(rows.Err())
}

func (r *Repository) Get(ctx context.Context, id uuid.UUID) (*Lead, error) {
	return scan(r.db.QueryRow(ctx, selectLead+` WHERE id = $1`, id))
}

// CountNew é o número no menu do painel.
func (r *Repository) CountNew(ctx context.Context) (int, error) {
	var n int
	err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM leads WHERE status = 'NEW'`).Scan(&n)
	return n, database.MapError(err)
}

// Update muda a situação e as anotações da equipe.
func (r *Repository) Update(ctx context.Context, id uuid.UUID, status, notes string) (*Lead, error) {
	switch status {
	case StatusNew, StatusContacted, StatusConverted, StatusDiscarded:
	default:
		return nil, ValidationError{"situação inválida"}
	}
	notes = strings.TrimSpace(notes)
	if utf8.RuneCountInString(notes) > 2000 {
		return nil, ValidationError{"anotações longas demais: até 2000 caracteres"}
	}
	return scan(r.db.QueryRow(ctx, `
		UPDATE leads SET status = $2, notes = $3, updated_at = NOW() WHERE id = $1
		RETURNING `+leadColumns, id, status, notes))
}

// MarkConverted liga o pré-cliente ao cliente cadastrado a partir dele.
func (r *Repository) MarkConverted(ctx context.Context, id, customerID uuid.UUID) error {
	tag, err := r.db.Exec(ctx, `
		UPDATE leads SET status = 'CONVERTED', customer_id = $2, updated_at = NOW() WHERE id = $1`, id, customerID)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ---------------------------------------------------------------------------
// Serviço
// ---------------------------------------------------------------------------

// Notifier avisa a equipe do pré-cliente novo.
type Notifier interface {
	NewLead(ctx context.Context, l *Lead) error
}

type Service struct {
	repo     *Repository
	notifier Notifier
	log      *slog.Logger
}

func NewService(repo *Repository, notifier Notifier, log *slog.Logger) *Service {
	return &Service{repo: repo, notifier: notifier, log: log.With("component", "leads")}
}

func (s *Service) Repo() *Repository { return s.repo }

// Submit grava o cadastro de interesse da landing. O aviso à equipe sai em
// segundo plano (o SMTP não segura quem está no formulário) e só para
// pré-cliente novo, não para quem mandou de novo.
func (s *Service) Submit(ctx context.Context, in Input) (*Lead, error) {
	in, err := Normalize(in)
	if err != nil {
		return nil, err
	}
	// Na lista primeiro: o pré-cliente gravado em seguida já sai com ela.
	if in.JoinLaunch {
		if _, err := s.JoinWaitlist(ctx, WaitlistInput{Name: in.Name, Email: in.Email, Phone: in.Phone, Consent: true}); err != nil {
			return nil, err
		}
	}
	lead, created, err := s.repo.save(ctx, in)
	if err != nil {
		return nil, err
	}
	s.log.Info("cadastro de interesse recebido", "lead", lead.ID, "novo", created)
	if created && s.notifier != nil {
		go func() {
			nctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
			defer cancel()
			if err := s.notifier.NewLead(nctx, lead); err != nil {
				s.log.Error("falha ao avisar a equipe do pré-cliente", "lead", lead.ID, "err", err)
			}
		}()
	}
	return lead, nil
}
