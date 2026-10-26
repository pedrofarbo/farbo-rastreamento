package devices

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// Chip é um chip M2M do estoque. Em uso é o que está num rastreador (pelo
// ICCID). A senha do APN nunca sai: só se está definida.
type Chip struct {
	ID             uuid.UUID `json:"id"`
	ICCID          string    `json:"iccid"`
	PhoneNumber    string    `json:"phoneNumber"`
	Carrier        string    `json:"carrier"`
	APN            string    `json:"apn"`
	APNUser        string    `json:"apnUser"`
	APNPasswordSet bool      `json:"apnPasswordSet"`
	Notes          string    `json:"notes"`
	// Device: o rastreador com este chip (nulo: livre no estoque).
	Device    *ChipDevice `json:"device"`
	CreatedAt time.Time   `json:"createdAt"`
	UpdatedAt time.Time   `json:"updatedAt"`

	apnPassword string
}

// ChipDevice é o rastreador em que o chip está.
type ChipDevice struct {
	ID   uuid.UUID `json:"id"`
	IMEI string    `json:"imei"`
}

// ChipInput é o cadastro de um chip. A senha do APN é só de escrita: vazia
// mantém a atual; ClearAPNPassword apaga.
type ChipInput struct {
	ICCID            string `json:"iccid"`
	PhoneNumber      string `json:"phoneNumber"`
	Carrier          string `json:"carrier"`
	APN              string `json:"apn"`
	APNUser          string `json:"apnUser"`
	APNPassword      string `json:"apnPassword"`
	ClearAPNPassword bool   `json:"clearApnPassword"`
	Notes            string `json:"notes"`
}

const chipColumns = `c.id, c.iccid, c.phone_number, c.carrier, c.apn, c.apn_user, c.apn_password, c.notes,
	d.id, COALESCE(d.imei, ''), c.created_at, c.updated_at`

const chipFrom = `FROM chips c LEFT JOIN devices d ON d.iccid = c.iccid`

func scanChip(row database.Scanner) (*Chip, error) {
	var c Chip
	var deviceID *uuid.UUID
	var imei string
	if err := row.Scan(&c.ID, &c.ICCID, &c.PhoneNumber, &c.Carrier, &c.APN, &c.APNUser, &c.apnPassword, &c.Notes,
		&deviceID, &imei, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, database.MapError(err)
	}
	c.APNPasswordSet = c.apnPassword != ""
	if deviceID != nil {
		c.Device = &ChipDevice{ID: *deviceID, IMEI: imei}
	}
	return &c, nil
}

// Chips lista o estoque, os mais novos primeiro.
func (s *Service) Chips(ctx context.Context) ([]*Chip, error) {
	rows, err := s.repo.db.Query(ctx, `SELECT `+chipColumns+` `+chipFrom+` ORDER BY c.created_at DESC, c.iccid`)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	out := []*Chip{}
	for rows.Next() {
		c, err := scanChip(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Service) Chip(ctx context.Context, id uuid.UUID) (*Chip, error) {
	return scanChip(s.repo.db.QueryRow(ctx, `SELECT `+chipColumns+` `+chipFrom+` WHERE c.id = $1`, id))
}

// normalizeChip confere o cadastro do chip: o ICCID (obrigatório, com o
// dígito verificador) e o tamanho dos textos.
func normalizeChip(in ChipInput) (ChipInput, error) {
	if in.ClearAPNPassword && in.APNPassword != "" {
		return in, invalid("informe a nova senha APN ou peça para apagá-la, não os dois")
	}
	iccid, err := NormalizeICCID(in.ICCID)
	if err != nil {
		return in, err
	}
	if iccid == "" {
		return in, invalid("informe o ICCID do chip: os 19 ou 20 números impressos nele")
	}
	in.ICCID = iccid
	for _, f := range []*string{&in.PhoneNumber, &in.Carrier, &in.APN, &in.APNUser, &in.Notes} {
		*f = strings.TrimSpace(*f)
	}
	if strings.ContainsAny(in.APN+in.APNUser+in.APNPassword, ",#") {
		return in, invalid("o APN, o usuário e a senha não podem ter vírgula nem #: eles vão no comando do rastreador")
	}
	if len(in.Carrier) > 40 || len(in.APN) > 100 || len(in.APNUser) > 100 || len(in.APNPassword) > 100 || len(in.Notes) > 500 {
		return in, invalid("algum campo do chip passou do tamanho")
	}
	return in, nil
}

// CreateChip cadastra um chip no estoque.
func (s *Service) CreateChip(ctx context.Context, in ChipInput) (*Chip, error) {
	in, err := normalizeChip(in)
	if err != nil {
		return nil, err
	}
	var id uuid.UUID
	err = s.repo.db.QueryRow(ctx, `
		INSERT INTO chips (iccid, phone_number, carrier, apn, apn_user, apn_password, notes)
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		in.ICCID, in.PhoneNumber, in.Carrier, in.APN, in.APNUser, in.APNPassword, in.Notes).Scan(&id)
	if errors.Is(database.MapError(err), database.ErrConflict) {
		return nil, invalid("o chip de ICCID %s já está cadastrado", in.ICCID)
	}
	if err != nil {
		return nil, database.MapError(err)
	}
	return s.Chip(ctx, id)
}

// UpdateChip corrige o cadastro do chip. Se ele está num rastreador, a linha,
// o APN e a senha vão junto para o rastreador (é para lá que vão os SMS e a
// configuração).
func (s *Service) UpdateChip(ctx context.Context, id uuid.UUID, in ChipInput) (*Chip, error) {
	in, err := normalizeChip(in)
	if err != nil {
		return nil, err
	}
	err = pgx.BeginFunc(ctx, s.repo.db, func(tx pgx.Tx) error {
		var oldICCID, password string
		if err := tx.QueryRow(ctx, `SELECT iccid, apn_password FROM chips WHERE id = $1 FOR UPDATE`, id).
			Scan(&oldICCID, &password); err != nil {
			return database.MapError(err)
		}
		switch {
		case in.ClearAPNPassword:
			password = ""
		case in.APNPassword != "":
			password = in.APNPassword
		}
		if _, err := tx.Exec(ctx, `
			UPDATE chips SET iccid = $2, phone_number = $3, carrier = $4, apn = $5, apn_user = $6, apn_password = $7,
				notes = $8, updated_at = NOW()
			WHERE id = $1`, id, in.ICCID, in.PhoneNumber, in.Carrier, in.APN, in.APNUser, password, in.Notes); err != nil {
			if errors.Is(database.MapError(err), database.ErrConflict) {
				return invalid("o chip de ICCID %s já está cadastrado", in.ICCID)
			}
			return database.MapError(err)
		}
		_, err := tx.Exec(ctx, `
			UPDATE devices SET iccid = $2, phone_number = $3, apn = $4, apn_user = $5, apn_password = $6, updated_at = NOW()
			WHERE iccid = $1`, oldICCID, in.ICCID, in.PhoneNumber, in.APN, in.APNUser, password)
		if errors.Is(database.MapError(err), database.ErrConflict) {
			return invalid("o ICCID %s já está em outro rastreador", in.ICCID)
		}
		return database.MapError(err)
	})
	if err != nil {
		var v ValidationError
		if errors.As(err, &v) {
			return nil, v
		}
		return nil, err
	}
	return s.Chip(ctx, id)
}

// DeleteChip tira do estoque um chip livre.
func (s *Service) DeleteChip(ctx context.Context, id uuid.UUID) error {
	c, err := s.Chip(ctx, id)
	if err != nil {
		return err
	}
	if c.Device != nil {
		return invalid("o chip está no rastreador de IMEI %s: tire-o do rastreador antes", c.Device.IMEI)
	}
	_, err = s.repo.db.Exec(ctx, `DELETE FROM chips WHERE id = $1`, id)
	return database.MapError(err)
}

// applyChip põe no cadastro do rastreador os dados do chip escolhido no
// estoque (ele precisa estar livre, ou já ser deste rastreador).
func (s *Service) applyChip(ctx context.Context, deviceID uuid.UUID, in Input) (Input, error) {
	if in.ChipID == nil {
		return in, nil
	}
	c, err := s.Chip(ctx, *in.ChipID)
	if errors.Is(err, database.ErrNotFound) {
		return in, invalid("o chip escolhido não está mais no estoque")
	}
	if err != nil {
		return in, err
	}
	if c.Device != nil && c.Device.ID != deviceID {
		return in, invalid("o chip %s já está no rastreador de IMEI %s", c.ICCID, c.Device.IMEI)
	}
	in.ICCID, in.PhoneNumber, in.APN, in.APNUser = c.ICCID, c.PhoneNumber, c.APN, c.APNUser
	in.APNPassword, in.ClearAPNPassword = c.apnPassword, c.apnPassword == ""
	return in, nil
}

// stockChip põe no estoque o chip digitado no cadastro do rastreador (um chip
// que já está lá só ganha o que estava em branco).
func (s *Service) stockChip(ctx context.Context, d *Device) error {
	if d.ICCID == "" {
		return nil
	}
	_, err := s.repo.db.Exec(ctx, `
		INSERT INTO chips (iccid, phone_number, apn, apn_user, apn_password) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (iccid) DO UPDATE SET
			phone_number = CASE WHEN chips.phone_number = '' THEN EXCLUDED.phone_number ELSE chips.phone_number END,
			apn          = CASE WHEN chips.apn = '' THEN EXCLUDED.apn ELSE chips.apn END,
			apn_user     = CASE WHEN chips.apn_user = '' THEN EXCLUDED.apn_user ELSE chips.apn_user END,
			apn_password = CASE WHEN chips.apn_password = '' THEN EXCLUDED.apn_password ELSE chips.apn_password END,
			updated_at   = NOW()`,
		d.ICCID, d.PhoneNumber, d.APN, d.APNUser, d.APNPassword)
	return database.MapError(err)
}
