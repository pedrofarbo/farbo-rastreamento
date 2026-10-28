package appointments

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/mail"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/push"
)

/*
 * A instalação avulsa: a central escolhe o rastreador e manda o link ao
 * instalador, sem agendamento nem veículo. Pelo link (sem login: o id
 * assinado com outra chave), o instalador testa o sinal e conclui. O link
 * expira na conclusão ou quando a central encerra; um aberto por rastreador.
 */

// As situações da instalação avulsa.
const (
	StandaloneOpen   = "open"
	StandaloneDone   = "done"
	StandaloneClosed = "closed"
)

var (
	// ErrStandaloneDone: o instalador já concluiu (o link expirou).
	ErrStandaloneDone = errors.New("esta instalação já foi concluída: o link não vale mais")
	// ErrStandaloneClosed: a central encerrou o link.
	ErrStandaloneClosed = errors.New("a Farbo encerrou este link de instalação")
)

// Standalone é uma instalação avulsa.
type Standalone struct {
	ID          uuid.UUID `json:"id"`
	DeviceID    uuid.UUID `json:"deviceId"`
	IMEI        string    `json:"imei"`
	DeviceModel string    `json:"deviceModel"`
	// O veículo em que o rastreador está cadastrado (vazio se nenhum).
	VehicleName   string     `json:"vehicleName"`
	Plate         string     `json:"plate"`
	InstallerID   *uuid.UUID `json:"installerId"`
	InstallerName string     `json:"installerName"`
	Notes         string     `json:"notes"`
	Status        string     `json:"status"`
	// O último teste do sinal pelo link (nulo se ainda não testou).
	CheckedAt     *time.Time `json:"checkedAt"`
	CheckReady    bool       `json:"checkReady"`
	CompletedAt   *time.Time `json:"completedAt"`
	InstallerNote string     `json:"installerNote"`
	ClosedAt      *time.Time `json:"closedAt"`
	CreatedBy     *uuid.UUID `json:"-"`
	CreatedAt     time.Time  `json:"createdAt"`
	// URL: o link do instalador (só para a central, enquanto aberto).
	URL string `json:"url,omitempty"`
}

// StandaloneInput é o link pedido pela central.
type StandaloneInput struct {
	DeviceID      uuid.UUID  `json:"deviceId"`
	InstallerID   *uuid.UUID `json:"installerId"`
	InstallerName string     `json:"installerName"`
	Notes         string     `json:"notes"`
}

// StandalonePublicURL é o link do instalador na instalação avulsa.
func (s *Service) StandalonePublicURL(id uuid.UUID) string {
	return s.appURL + "/instalador/avulsa/" + sign(s.standaloneKey, id)
}

const selectStandalone = `
	SELECT l.id, l.device_id, d.imei, concat_ws(' ', NULLIF(d.manufacturer, ''), NULLIF(d.model, '')),
		COALESCE(v.name, ''), COALESCE(v.plate, ''), l.installer_id, COALESCE(NULLIF(l.installer_name, ''), i.name, ''),
		l.notes, l.status, l.checked_at, l.check_ready, l.completed_at, l.installer_note, l.closed_at, l.created_by,
		l.created_at
	FROM standalone_installs l
	JOIN devices d ON d.id = l.device_id
	LEFT JOIN vehicles v ON v.device_id = l.device_id
	LEFT JOIN installers i ON i.id = l.installer_id`

func scanStandalone(row database.Scanner) (*Standalone, error) {
	var l Standalone
	if err := row.Scan(&l.ID, &l.DeviceID, &l.IMEI, &l.DeviceModel, &l.VehicleName, &l.Plate, &l.InstallerID,
		&l.InstallerName, &l.Notes, &l.Status, &l.CheckedAt, &l.CheckReady, &l.CompletedAt, &l.InstallerNote,
		&l.ClosedAt, &l.CreatedBy, &l.CreatedAt); err != nil {
		return nil, database.MapError(err)
	}
	return &l, nil
}

// withURL põe o link nos abertos (o encerrado não vale mais).
func (s *Service) withURL(l *Standalone) *Standalone {
	if l.Status == StandaloneOpen {
		l.URL = s.StandalonePublicURL(l.ID)
	}
	return l
}

// Standalones são os links abertos e os encerrados nos últimos 30 dias:
// primeiro os abertos, depois os mais recentes.
func (s *Service) Standalones(ctx context.Context) ([]*Standalone, error) {
	rows, err := s.db.Query(ctx, selectStandalone+`
		WHERE l.status = 'open' OR l.updated_at > NOW() - INTERVAL '30 days'
		ORDER BY l.status = 'open' DESC, l.updated_at DESC LIMIT 500`)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	out := []*Standalone{}
	for rows.Next() {
		l, err := scanStandalone(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s.withURL(l))
	}
	return out, database.MapError(rows.Err())
}

// GetStandalone é a instalação avulsa (com o link, se aberta).
func (s *Service) GetStandalone(ctx context.Context, id uuid.UUID) (*Standalone, error) {
	l, err := scanStandalone(s.db.QueryRow(ctx, selectStandalone+` WHERE l.id = $1`, id))
	if err != nil {
		return nil, err
	}
	return s.withURL(l), nil
}

// CreateStandalone gera o link da instalação avulsa do rastreador.
func (s *Service) CreateStandalone(ctx context.Context, by uuid.UUID, in StandaloneInput) (*Standalone, error) {
	in.InstallerName = strings.Join(strings.Fields(in.InstallerName), " ")
	in.Notes = strings.TrimSpace(in.Notes)
	switch {
	case in.DeviceID == uuid.Nil:
		return nil, invalid("escolha o rastreador")
	case in.InstallerID == nil && in.InstallerName == "":
		return nil, invalid("escolha o instalador (ou escreva o nome)")
	case utf8.RuneCountInString(in.InstallerName) > 120 || utf8.RuneCountInString(in.Notes) > 1000:
		return nil, invalid("o nome do instalador vai até 120 caracteres e as observações até 1.000")
	}
	var exists bool
	if err := s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM devices WHERE id = $1)`, in.DeviceID).Scan(&exists); err != nil {
		return nil, database.MapError(err)
	}
	if !exists {
		return nil, invalid("rastreador não encontrado")
	}
	var id uuid.UUID
	err := s.db.QueryRow(ctx, `
		INSERT INTO standalone_installs (device_id, installer_id, installer_name, notes, created_by)
		VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		in.DeviceID, in.InstallerID, in.InstallerName, in.Notes, by).Scan(&id)
	if errors.Is(database.MapError(err), database.ErrConflict) {
		return nil, invalid("este rastreador já tem um link de instalação aberto: encerre aquele antes de gerar outro")
	}
	if err != nil {
		return nil, database.MapError(err)
	}
	return s.GetStandalone(ctx, id)
}

// CloseStandalone encerra o link (o instalador não abre mais).
func (s *Service) CloseStandalone(ctx context.Context, id, by uuid.UUID) (*Standalone, error) {
	tag, err := s.db.Exec(ctx, `
		UPDATE standalone_installs SET status = 'closed', closed_at = NOW(), closed_by = $2, updated_at = NOW()
		WHERE id = $1 AND status = 'open'`, id, by)
	if err != nil {
		return nil, database.MapError(err)
	}
	l, err := s.GetStandalone(ctx, id)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, invalid("o link já foi encerrado")
	}
	return l, nil
}

// StandaloneByToken é a instalação avulsa do link, só enquanto aberta.
func (s *Service) StandaloneByToken(ctx context.Context, token string) (*Standalone, error) {
	id, ok := verify(s.standaloneKey, token)
	if !ok {
		return nil, ErrLinkEnded
	}
	l, err := scanStandalone(s.db.QueryRow(ctx, selectStandalone+` WHERE l.id = $1`, id))
	if errors.Is(err, database.ErrNotFound) {
		return nil, ErrLinkEnded
	}
	if err != nil {
		return nil, err
	}
	switch l.Status {
	case StandaloneDone:
		return nil, ErrStandaloneDone
	case StandaloneClosed:
		return nil, ErrStandaloneClosed
	}
	return l, nil
}

// RecordStandaloneCheck guarda o resultado do teste do sinal (a central vê).
func (s *Service) RecordStandaloneCheck(ctx context.Context, id uuid.UUID, ready bool) error {
	_, err := s.db.Exec(ctx, `
		UPDATE standalone_installs SET checked_at = NOW(), check_ready = $2, updated_at = NOW()
		WHERE id = $1 AND status = 'open'`, id, ready)
	return database.MapError(err)
}

// CompleteStandalone conclui (o teste do sinal já passou) e o link expira;
// quem gerou o link é avisado no app.
func (s *Service) CompleteStandalone(ctx context.Context, id uuid.UUID, note string) (*Standalone, error) {
	note = strings.TrimSpace(note)
	if utf8.RuneCountInString(note) > 500 {
		return nil, invalid("a observação vai até 500 caracteres")
	}
	tag, err := s.db.Exec(ctx, `
		UPDATE standalone_installs SET status = 'done', completed_at = NOW(), installer_note = $2, checked_at = NOW(),
			check_ready = TRUE, updated_at = NOW()
		WHERE id = $1 AND status = 'open'`, id, note)
	if err != nil {
		return nil, database.MapError(err)
	}
	l, err := s.GetStandalone(ctx, id)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		// Concluído ou encerrado enquanto o instalador testava.
		if l.Status == StandaloneClosed {
			return nil, ErrStandaloneClosed
		}
		return nil, ErrStandaloneDone
	}
	if s.pusher != nil && l.CreatedBy != nil {
		by := *l.CreatedBy
		body := "Rastreador " + l.IMEI
		if l.InstallerName != "" {
			body += " instalado por " + l.InstallerName
		}
		s.runAsync(func() {
			if _, err := s.pusher.Notify(context.Background(), by, push.Notification{
				Title: "Instalação avulsa concluída", Body: body + ".", URL: "/prestadores", Tag: "standalone:" + l.ID.String(),
				Severity: mail.SeverityInfo,
			}); err != nil {
				s.log.Warn("aviso da instalação avulsa não chegou", "standalone", l.ID, "err", err)
			}
		})
	}
	return l, nil
}
