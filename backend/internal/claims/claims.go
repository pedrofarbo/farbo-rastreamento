// Package claims são as ocorrências: o associado de uma associação ou
// seguradora abre pelo app (batida, roubo, furto, vidro...), com as fotos e
// o boletim de ocorrência; a equipe da organização acompanha, pede o que
// falta e muda a situação. Cada passo fica no histórico, e a mudança de
// situação avisa o associado no app.
package claims

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/finance"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/mail"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/push"
)

// Os tipos de ocorrência.
var Kinds = []string{"collision", "theft", "robbery", "glass", "natural", "other"}

// KindLabels: como a tela e o e-mail escrevem cada tipo.
var KindLabels = map[string]string{
	"collision": "Colisão", "theft": "Furto", "robbery": "Roubo", "glass": "Vidros",
	"natural": "Fenômeno da natureza", "other": "Outro",
}

// As situações.
const (
	StatusOpen      = "open"
	StatusAnalyzing = "analyzing"
	StatusApproved  = "approved"
	StatusDenied    = "denied"
	StatusClosed    = "closed"
)

// Statuses na ordem da tela.
var Statuses = []string{StatusOpen, StatusAnalyzing, StatusApproved, StatusDenied, StatusClosed}

// StatusLabels: como a tela e o aviso escrevem cada situação.
var StatusLabels = map[string]string{
	StatusOpen: "Aberta", StatusAnalyzing: "Em análise", StatusApproved: "Aprovada", StatusDenied: "Negada",
	StatusClosed: "Encerrada",
}

// Os tipos de arquivo.
const (
	FilePhoto        = "photo"
	FilePoliceReport = "police_report"
	FileDocument     = "document"
)

const (
	MaxDescription = 2000
	MaxNote        = 1000
	MaxPlace       = 200
	// MaxFileBytes: uma foto do celular (o app reduz antes) ou o PDF do B.O.
	MaxFileBytes = 6 << 20
	// MaxFiles por ocorrência.
	MaxFiles = 30
)

// InputError é um pedido recusado; a mensagem vai para a tela.
type InputError struct{ Message string }

func (e *InputError) Error() string { return e.Message }

func invalid(format string, args ...any) error { return &InputError{fmt.Sprintf(format, args...)} }

// Claim é uma ocorrência.
type Claim struct {
	ID             uuid.UUID  `json:"id"`
	OrganizationID uuid.UUID  `json:"organizationId"`
	AssociateID    uuid.UUID  `json:"associateId"`
	AssociateName  string     `json:"associateName"`
	VehicleID      *uuid.UUID `json:"vehicleId"`
	VehicleName    string     `json:"vehicleName"`
	Plate          string     `json:"plate"`
	Kind           string     `json:"kind"`
	OccurredAt     time.Time  `json:"occurredAt"`
	Place          string     `json:"place"`
	Description    string     `json:"description"`
	PoliceReport   string     `json:"policeReport"`
	Status         string     `json:"status"`
	Photos         int        `json:"photos"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
	// Na ocorrência aberta (Get): os arquivos e o histórico.
	Files   []File   `json:"files,omitempty"`
	Updates []Update `json:"updates,omitempty"`
}

// File é um arquivo da ocorrência (sem o conteúdo).
type File struct {
	ID          uuid.UUID `json:"id"`
	Kind        string    `json:"kind"`
	Filename    string    `json:"filename"`
	ContentType string    `json:"contentType"`
	SizeBytes   int       `json:"sizeBytes"`
	CreatedAt   time.Time `json:"createdAt"`
}

// Update é um passo do histórico.
type Update struct {
	ID         int64     `json:"id"`
	AuthorName string    `json:"authorName"`
	AuthorKind string    `json:"authorKind"`
	Status     string    `json:"status"`
	Note       string    `json:"note"`
	CreatedAt  time.Time `json:"createdAt"`
}

// Input é a ocorrência que o associado abre.
type Input struct {
	VehicleID    *uuid.UUID `json:"vehicleId"`
	Kind         string     `json:"kind"`
	OccurredAt   time.Time  `json:"occurredAt"`
	Place        string     `json:"place"`
	Description  string     `json:"description"`
	PoliceReport string     `json:"policeReport"`
}

// Notifier avisa a equipe da organização (mail.OrgMailer).
type Notifier interface {
	ClaimOpened(ctx context.Context, to, name string, n mail.ClaimNotice) error
}

// Pusher avisa o associado no app (push.Service).
type Pusher interface {
	Notify(ctx context.Context, userID uuid.UUID, n push.Notification) (int, error)
}

type Service struct {
	db       *database.DB
	notifier Notifier
	pusher   Pusher
	now      func() time.Time
	runAsync func(func())
	log      *slog.Logger
}

func NewService(db *database.DB, notifier Notifier, log *slog.Logger) *Service {
	return &Service{db: db, notifier: notifier, now: time.Now, runAsync: func(f func()) { go f() },
		log: log.With("component", "claims")}
}

// SetPusher liga o aviso no app.
func (s *Service) SetPusher(p Pusher) { s.pusher = p }

// SetSync faz os avisos saírem dentro da chamada (testes).
func (s *Service) SetSync() { s.runAsync = func(f func()) { f() } }

const selectClaim = `
	SELECT c.id, c.organization_id, c.associate_id, a.name, c.vehicle_id, COALESCE(v.name, ''), COALESCE(v.plate, ''),
		c.kind, c.occurred_at, c.place, c.description, c.police_report, c.status,
		(SELECT count(*) FROM claim_files f WHERE f.claim_id = c.id AND f.kind = 'photo'), c.created_at, c.updated_at
	FROM claims c JOIN users a ON a.id = c.associate_id LEFT JOIN vehicles v ON v.id = c.vehicle_id`

func scan(row database.Scanner) (*Claim, error) {
	var c Claim
	if err := row.Scan(&c.ID, &c.OrganizationID, &c.AssociateID, &c.AssociateName, &c.VehicleID, &c.VehicleName,
		&c.Plate, &c.Kind, &c.OccurredAt, &c.Place, &c.Description, &c.PoliceReport, &c.Status, &c.Photos,
		&c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, database.MapError(err)
	}
	return &c, nil
}

// Filter escolhe as ocorrências: as de uma organização ou as de um associado.
type Filter struct {
	OrganizationID *uuid.UUID
	AssociateID    *uuid.UUID
	Status         string
}

// MaxListed: quantas a lista traz (as mais recentes).
const MaxListed = 500

// List são as ocorrências, das mais recentes.
func (s *Service) List(ctx context.Context, f Filter) ([]*Claim, error) {
	if f.Status != "" && !slices.Contains(Statuses, f.Status) {
		return nil, invalid("situação inválida")
	}
	rows, err := s.db.Query(ctx, selectClaim+`
		WHERE ($1::uuid IS NULL OR c.organization_id = $1) AND ($2::uuid IS NULL OR c.associate_id = $2)
			AND ($3 = '' OR c.status = $3)
		ORDER BY c.created_at DESC LIMIT $4`, f.OrganizationID, f.AssociateID, f.Status, MaxListed)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	out := []*Claim{}
	for rows.Next() {
		c, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, database.MapError(rows.Err())
}

// Get é a ocorrência com os arquivos e o histórico.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (*Claim, error) {
	c, err := scan(s.db.QueryRow(ctx, selectClaim+` WHERE c.id = $1`, id))
	if err != nil {
		return nil, err
	}
	files, err := s.db.Query(ctx, `
		SELECT id, kind, filename, content_type, size_bytes, created_at FROM claim_files
		WHERE claim_id = $1 ORDER BY created_at`, id)
	if err != nil {
		return nil, database.MapError(err)
	}
	c.Files = []File{}
	for files.Next() {
		var f File
		if err := files.Scan(&f.ID, &f.Kind, &f.Filename, &f.ContentType, &f.SizeBytes, &f.CreatedAt); err != nil {
			files.Close()
			return nil, database.MapError(err)
		}
		c.Files = append(c.Files, f)
	}
	files.Close()
	updates, err := s.db.Query(ctx, `
		SELECT u.id, COALESCE(a.name, ''), u.author_kind, u.status, u.note, u.created_at FROM claim_updates u
		LEFT JOIN users a ON a.id = u.author_id WHERE u.claim_id = $1 ORDER BY u.created_at, u.id`, id)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer updates.Close()
	c.Updates = []Update{}
	for updates.Next() {
		var u Update
		if err := updates.Scan(&u.ID, &u.AuthorName, &u.AuthorKind, &u.Status, &u.Note, &u.CreatedAt); err != nil {
			return nil, database.MapError(err)
		}
		c.Updates = append(c.Updates, u)
	}
	return c, database.MapError(updates.Err())
}

// Open abre a ocorrência do associado (o veículo, se dito, é dele) e avisa
// a equipe da organização.
func (s *Service) Open(ctx context.Context, orgID, associateID uuid.UUID, in Input) (*Claim, error) {
	in.Place = strings.Join(strings.Fields(in.Place), " ")
	in.Description = strings.TrimSpace(in.Description)
	in.PoliceReport = strings.Join(strings.Fields(in.PoliceReport), " ")
	now := s.now()
	switch {
	case !slices.Contains(Kinds, in.Kind):
		return nil, invalid("escolha o tipo da ocorrência")
	case in.OccurredAt.IsZero() || in.OccurredAt.After(now.Add(5*time.Minute)) || in.OccurredAt.Before(now.AddDate(-1, 0, 0)):
		return nil, invalid("informe quando aconteceu (até um ano atrás)")
	case in.Description == "" || utf8.RuneCountInString(in.Description) > MaxDescription:
		return nil, invalid("conte o que aconteceu (até %d caracteres)", MaxDescription)
	case utf8.RuneCountInString(in.Place) > MaxPlace:
		return nil, invalid("o local vai até %d caracteres", MaxPlace)
	case utf8.RuneCountInString(in.PoliceReport) > 60:
		return nil, invalid("o número do boletim de ocorrência vai até 60 caracteres")
	}
	if in.VehicleID != nil {
		var owner *uuid.UUID
		if err := s.db.QueryRow(ctx, `SELECT owner_id FROM vehicles WHERE id = $1`, *in.VehicleID).Scan(&owner); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, invalid("veículo não encontrado")
			}
			return nil, database.MapError(err)
		}
		if owner == nil || *owner != associateID {
			return nil, invalid("veículo não encontrado")
		}
	}
	var id uuid.UUID
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			INSERT INTO claims (organization_id, associate_id, vehicle_id, kind, occurred_at, place, description,
				police_report, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $2) RETURNING id`,
			orgID, associateID, in.VehicleID, in.Kind, in.OccurredAt, in.Place, in.Description, in.PoliceReport).Scan(&id); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO claim_updates (claim_id, author_id, author_kind, status) VALUES ($1, $2, 'associate', 'open')`,
			id, associateID)
		return err
	})
	if err != nil {
		return nil, database.MapError(err)
	}
	c, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	s.runAsync(func() { s.notifyOpened(context.Background(), c) })
	return c, nil
}

// notifyOpened avisa por e-mail o Administrador e o Operador da organização.
func (s *Service) notifyOpened(ctx context.Context, c *Claim) {
	if s.notifier == nil {
		return
	}
	rows, err := s.db.Query(ctx, `
		SELECT u.email, u.name, COALESCE(o.name, '') FROM organization_members m
		JOIN users u ON u.id = m.user_id JOIN users o ON o.id = m.organization_id
		WHERE m.organization_id = $1 AND m.role IN ('admin', 'operator') AND u.active`, c.OrganizationID)
	if err != nil {
		s.log.Error("falha ao listar quem avisar da ocorrência", "claim", c.ID, "err", err)
		return
	}
	type person struct{ email, name, org string }
	var people []person
	for rows.Next() {
		var p person
		if err := rows.Scan(&p.email, &p.name, &p.org); err != nil {
			rows.Close()
			s.log.Error("falha ao listar quem avisar da ocorrência", "claim", c.ID, "err", err)
			return
		}
		people = append(people, p)
	}
	rows.Close()
	for _, p := range people {
		n := mail.ClaimNotice{
			ClaimID: c.ID.String(), OrgName: p.org, AssociateName: c.AssociateName, VehicleName: c.VehicleName,
			Plate: c.Plate, KindLabel: KindLabels[c.Kind], OccurredAt: c.OccurredAt, Description: c.Description,
		}
		if err := s.notifier.ClaimOpened(ctx, p.email, p.name, n); err != nil {
			s.log.Warn("aviso da ocorrência não saiu", "claim", c.ID, "err", err)
		}
	}
}

// AddFile anexa uma foto ou documento (com a ocorrência ainda aberta ou em
// análise). O tipo vem do conteúdo: fotos JPEG ou PNG; o B.O. e os
// documentos também em PDF.
func (s *Service) AddFile(ctx context.Context, claimID, by uuid.UUID, kind, filename string, data []byte) (*File, error) {
	if kind != FilePhoto && kind != FilePoliceReport && kind != FileDocument {
		return nil, invalid("tipo de arquivo inválido")
	}
	if len(data) == 0 || len(data) > MaxFileBytes {
		return nil, invalid("o arquivo vai até %d MB", MaxFileBytes>>20)
	}
	contentType := finance.AttachmentType(data)
	switch {
	case contentType == "":
		return nil, invalid("envie uma foto (JPEG ou PNG) ou um PDF")
	case kind == FilePhoto && contentType == "application/pdf":
		return nil, invalid("a foto precisa ser JPEG ou PNG")
	}
	var f File
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var status string
		var count int
		if err := tx.QueryRow(ctx, `
			SELECT c.status, (SELECT count(*) FROM claim_files f WHERE f.claim_id = c.id) FROM claims c
			WHERE c.id = $1 FOR UPDATE`, claimID).Scan(&status, &count); err != nil {
			return err
		}
		if status != StatusOpen && status != StatusAnalyzing {
			return invalid("a ocorrência já foi %s: não dá mais para anexar", strings.ToLower(StatusLabels[status]))
		}
		if count >= MaxFiles {
			return invalid("são até %d arquivos por ocorrência", MaxFiles)
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO claim_files (claim_id, kind, filename, content_type, size_bytes, data, uploaded_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id, kind, filename, content_type, size_bytes, created_at`,
			claimID, kind, finance.CleanFilename(filename), contentType, len(data), data, by).
			Scan(&f.ID, &f.Kind, &f.Filename, &f.ContentType, &f.SizeBytes, &f.CreatedAt); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE claims SET updated_at = NOW() WHERE id = $1`, claimID)
		return err
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return &f, nil
}

// FileData é o arquivo com o conteúdo.
func (s *Service) FileData(ctx context.Context, claimID, fileID uuid.UUID) (*File, []byte, error) {
	var f File
	var data []byte
	err := s.db.QueryRow(ctx, `
		SELECT id, kind, filename, content_type, size_bytes, created_at, data FROM claim_files
		WHERE id = $1 AND claim_id = $2`, fileID, claimID).
		Scan(&f.ID, &f.Kind, &f.Filename, &f.ContentType, &f.SizeBytes, &f.CreatedAt, &data)
	if err != nil {
		return nil, nil, database.MapError(err)
	}
	return &f, data, nil
}

// AddNote põe uma nota no histórico (authorKind: associate ou organization).
func (s *Service) AddNote(ctx context.Context, claimID, by uuid.UUID, authorKind, note string) error {
	note = strings.TrimSpace(note)
	if note == "" || utf8.RuneCountInString(note) > MaxNote {
		return invalid("escreva a nota (até %d caracteres)", MaxNote)
	}
	return s.update(ctx, claimID, by, authorKind, "", note)
}

// SetStatus muda a situação (a equipe da organização), com uma nota
// opcional, e avisa o associado no app.
func (s *Service) SetStatus(ctx context.Context, claimID, by uuid.UUID, status, note string) (*Claim, error) {
	note = strings.TrimSpace(note)
	switch {
	case !slices.Contains(Statuses, status):
		return nil, invalid("situação inválida")
	case utf8.RuneCountInString(note) > MaxNote:
		return nil, invalid("a nota vai até %d caracteres", MaxNote)
	}
	if err := s.update(ctx, claimID, by, "organization", status, note); err != nil {
		return nil, err
	}
	c, err := s.Get(ctx, claimID)
	if err != nil {
		return nil, err
	}
	if s.pusher != nil {
		s.runAsync(func() {
			body := note
			if body == "" {
				body = KindLabels[c.Kind] + " de " + c.OccurredAt.In(brt).Format("02/01/2006") + ": toque para ver."
			}
			if _, err := s.pusher.Notify(context.Background(), c.AssociateID, push.Notification{
				Title: "Ocorrência: " + strings.ToLower(StatusLabels[status]), Body: body,
				URL: "/app/ocorrencias/" + c.ID.String(), Tag: "claim:" + c.ID.String(), Severity: mail.SeverityInfo,
			}); err != nil {
				s.log.Warn("aviso da ocorrência não chegou ao associado", "claim", c.ID, "err", err)
			}
		})
	}
	return c, nil
}

var brt = time.FixedZone("BRT", -3*60*60)

func (s *Service) update(ctx context.Context, claimID, by uuid.UUID, authorKind, status, note string) error {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var current string
		if err := tx.QueryRow(ctx, `SELECT status FROM claims WHERE id = $1 FOR UPDATE`, claimID).Scan(&current); err != nil {
			return err
		}
		if status == "" && current == StatusClosed {
			return invalid("a ocorrência foi encerrada")
		}
		if status != "" && status == current && note == "" {
			return invalid("a ocorrência já está %s", strings.ToLower(StatusLabels[status]))
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO claim_updates (claim_id, author_id, author_kind, status, note) VALUES ($1, $2, $3, $4, $5)`,
			claimID, by, authorKind, status, note); err != nil {
			return err
		}
		if status != "" {
			_, err := tx.Exec(ctx, `UPDATE claims SET status = $2, updated_at = NOW() WHERE id = $1`, claimID, status)
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE claims SET updated_at = NOW() WHERE id = $1`, claimID)
		return err
	})
	return mapErr(err)
}

func mapErr(err error) error {
	var input *InputError
	if errors.As(err, &input) {
		return input
	}
	return database.MapError(err)
}
