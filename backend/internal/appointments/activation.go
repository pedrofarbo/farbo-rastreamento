package appointments

import (
	"context"
	"errors"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

/*
 * A ativação pelo link do instalador: quando o rastreador está livre (sem
 * veículo) e nunca conectou, o instalador escolhe o chip do estoque e ativa
 * antes de instalar — o chip vai para o cadastro do rastreador e a
 * configuração sai por SMS (pacote smssetup). No agendamento, o instalador
 * escolhe também o rastreador, entre os livres; na instalação avulsa, ele é o
 * do link. O link guarda a ativação e quantas já fez.
 */

// MaxActivations é quantas ativações um link pode fazer: cada uma manda SMS
// (pagos), e errar o chip ou o rastreador mais que isso pede a central.
const MaxActivations = 3

// ErrTooManyActivations: o link já fez todas as ativações que podia.
var ErrTooManyActivations = &InputError{"este link já fez todas as ativações que podia: fale com a Farbo"}

// StockDevice é um rastreador livre para o instalador escolher: o IMEI, o
// modelo, o chip que já está nele (vazio: nenhum) e se já conectou alguma
// vez (ativado: é só instalar e testar o sinal).
type StockDevice struct {
	ID    uuid.UUID `json:"id"`
	IMEI  string    `json:"imei"`
	Model string    `json:"model"`
	ICCID string    `json:"iccid"`
	// ChipID: o chip do estoque que já está nele (nulo: nenhum, ou fora do
	// estoque) — a ativação pode usar ele mesmo.
	ChipID    *uuid.UUID `json:"chipId"`
	Activated bool       `json:"activated"`
}

// StockChip é um chip livre do estoque (o ICCID impresso nele e a operadora).
type StockChip struct {
	ID      uuid.UUID `json:"id"`
	ICCID   string    `json:"iccid"`
	Carrier string    `json:"carrier"`
}

// maxStock limita as listas do link.
const maxStock = 500

// deviceFree são os rastreadores que um link pode usar: sem veículo, sem
// reserva de outro link aberto — a ativação de outro agendamento ou a
// instalação avulsa dele — e do estoque certo: no link da central, fora dos
// kits dos representantes; no do representante ($3), só os do kit dele que
// já chegaram. $1 é o agendamento do link (nulo na avulsa) e $2, a
// instalação avulsa (nula no agendamento).
const deviceFree = `NOT EXISTS (SELECT 1 FROM vehicles v WHERE v.device_id = d.id)
	AND CASE WHEN $3::uuid IS NULL THEN
		NOT EXISTS (SELECT 1 FROM kit_shipment_devices k JOIN kit_shipments ks ON ks.id = k.shipment_id
			WHERE k.device_id = d.id AND ks.status <> 'canceled')
	ELSE
		EXISTS (SELECT 1 FROM kit_shipment_devices k JOIN kit_shipments ks ON ks.id = k.shipment_id
			WHERE k.device_id = d.id AND ks.affiliate_id = $3::uuid AND ks.status = 'delivered')
	END
	AND NOT EXISTS (SELECT 1 FROM installation_appointments a WHERE a.activation_device_id = d.id
		AND a.status IN ('scheduled', 'arrived') AND a.id IS DISTINCT FROM $1::uuid)
	AND NOT EXISTS (SELECT 1 FROM standalone_installs l WHERE l.device_id = d.id AND l.status = 'open'
		AND l.id IS DISTINCT FROM $2::uuid)`

// activated: o rastreador já conectou alguma vez.
const activated = `(d.last_seen_at IS NOT NULL OR d.status IN ('ONLINE', 'STALE'))`

// InstallStock são os rastreadores livres para o agendamento (os ativados e
// os que precisam de ativação) e os chips livres, os mais novos primeiro. No
// agendamento do representante, os rastreadores são os do kit dele (que já
// vêm com o chip): nenhum chip da central.
func (s *Service) InstallStock(ctx context.Context, appointmentID uuid.UUID) ([]StockDevice, []StockChip, error) {
	affiliate, err := s.affiliateOf(ctx, appointmentID)
	if err != nil {
		return nil, nil, err
	}
	rows, err := s.db.Query(ctx, `
		SELECT d.id, d.imei, concat_ws(' ', NULLIF(d.manufacturer, ''), NULLIF(d.model, '')), COALESCE(d.iccid, ''),
			(SELECT c.id FROM chips c WHERE c.iccid = d.iccid), `+activated+`
		FROM devices d WHERE `+deviceFree+`
		ORDER BY d.created_at DESC LIMIT `+strconv.Itoa(maxStock), appointmentID, nil, affiliate)
	if err != nil {
		return nil, nil, database.MapError(err)
	}
	devices := []StockDevice{}
	for rows.Next() {
		var d StockDevice
		if err := rows.Scan(&d.ID, &d.IMEI, &d.Model, &d.ICCID, &d.ChipID, &d.Activated); err != nil {
			rows.Close()
			return nil, nil, err
		}
		devices = append(devices, d)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, database.MapError(err)
	}
	if affiliate != nil {
		chips, err := s.kitChips(ctx, *affiliate)
		return devices, chips, err
	}
	chips, err := s.FreeChips(ctx)
	return devices, chips, err
}

// kitChips são os chips avulsos dos kits que o representante já recebeu,
// ainda fora de rastreador (os da central não são dele).
func (s *Service) kitChips(ctx context.Context, affiliateID uuid.UUID) ([]StockChip, error) {
	return s.chips(ctx, `
		SELECT c.id, c.iccid, c.carrier FROM chips c
		JOIN kit_shipment_chips kc ON kc.chip_id = c.id JOIN kit_shipments k ON k.id = kc.shipment_id
		WHERE k.affiliate_id = $1 AND k.status = 'delivered' AND NOT EXISTS (SELECT 1 FROM devices d WHERE d.iccid = c.iccid)
		ORDER BY c.iccid LIMIT `+strconv.Itoa(maxStock), affiliateID)
}

// ChipKitOwner é o representante em cujo kit o chip foi (nulo: em nenhum).
func (s *Service) ChipKitOwner(ctx context.Context, chipID uuid.UUID) (*uuid.UUID, error) {
	var owner uuid.UUID
	err := s.db.QueryRow(ctx, `SELECT k.affiliate_id FROM kit_shipment_chips kc JOIN kit_shipments k ON k.id = kc.shipment_id
		WHERE kc.chip_id = $1`, chipID).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, database.MapError(err)
	}
	return &owner, nil
}

// affiliateOf é o representante que agendou (nulo: a central).
func (s *Service) affiliateOf(ctx context.Context, appointmentID uuid.UUID) (*uuid.UUID, error) {
	var affiliate *uuid.UUID
	err := s.db.QueryRow(ctx, `SELECT affiliate_id FROM installation_appointments WHERE id = $1`, appointmentID).Scan(&affiliate)
	return affiliate, database.MapError(err)
}

// FreeChips são os chips do estoque da central: fora de rastreador e dos kits
// dos representantes (esses estão com eles).
func (s *Service) FreeChips(ctx context.Context) ([]StockChip, error) {
	return s.chips(ctx, `
		SELECT c.id, c.iccid, c.carrier FROM chips c
		WHERE NOT EXISTS (SELECT 1 FROM devices d WHERE d.iccid = c.iccid)
			AND NOT EXISTS (SELECT 1 FROM kit_shipment_chips kc WHERE kc.chip_id = c.id)
		ORDER BY c.created_at DESC LIMIT `+strconv.Itoa(maxStock))
}

// chips lê uma lista de chips (id, iccid, operadora).
func (s *Service) chips(ctx context.Context, query string, args ...any) ([]StockChip, error) {
	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	out := []StockChip{}
	for rows.Next() {
		var c StockChip
		if err := rows.Scan(&c.ID, &c.ICCID, &c.Carrier); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, database.MapError(rows.Err())
}

// ChipByICCID é o chip do estoque com o ICCID (nulo: não está no estoque).
func (s *Service) ChipByICCID(ctx context.Context, iccid string) (*StockChip, error) {
	var c StockChip
	err := s.db.QueryRow(ctx, `SELECT id, iccid, carrier FROM chips WHERE iccid = $1`, iccid).Scan(&c.ID, &c.ICCID, &c.Carrier)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, database.MapError(err)
	}
	return &c, nil
}

// DeviceFree diz se o agendamento pode usar o rastreador (sem veículo, fora
// dos kits e sem reserva de outro link aberto) e, se não, por quê.
func (s *Service) DeviceFree(ctx context.Context, appointmentID, deviceID uuid.UUID) error {
	affiliate, err := s.affiliateOf(ctx, appointmentID)
	if err != nil {
		return err
	}
	var free bool
	err = s.db.QueryRow(ctx, `SELECT `+deviceFree+` FROM devices d WHERE d.id = $4`, appointmentID, nil, affiliate, deviceID).Scan(&free)
	if errors.Is(err, pgx.ErrNoRows) {
		return invalid("este rastreador não está cadastrado na central")
	}
	if err != nil {
		return database.MapError(err)
	}
	if !free {
		if affiliate != nil {
			return invalid("este rastreador não está no seu kit, já está num veículo ou está reservado em outra instalação")
		}
		return invalid("este rastreador já está num veículo, no kit de um representante ou reservado em outra instalação")
	}
	return nil
}

// Activation é a ativação de um link: o rastreador, a sessão de SMS (nulos:
// nenhuma ainda) e quantas o link já fez.
type Activation struct {
	DeviceID  *uuid.UUID
	SessionID *uuid.UUID
	Count     int
}

// Left: quantas ativações o link ainda pode fazer.
func (a Activation) Left() int { return max(MaxActivations-a.Count, 0) }

// ActivationOf é a ativação do agendamento.
func (s *Service) ActivationOf(ctx context.Context, appointmentID uuid.UUID) (Activation, error) {
	var a Activation
	err := s.db.QueryRow(ctx, `SELECT activation_device_id, activation_session_id, activations
		FROM installation_appointments WHERE id = $1`, appointmentID).Scan(&a.DeviceID, &a.SessionID, &a.Count)
	return a, database.MapError(err)
}

// ReserveActivation conta mais uma ativação do agendamento (no teto, recusa)
// e reserva o rastreador para ele, antes de os SMS saírem.
func (s *Service) ReserveActivation(ctx context.Context, appointmentID, deviceID uuid.UUID) error {
	tag, err := s.db.Exec(ctx, `
		UPDATE installation_appointments SET activation_device_id = $2, activations = activations + 1, updated_at = NOW()
		WHERE id = $1 AND kind = 'install' AND status IN ('scheduled', 'arrived') AND activations < $3`,
		appointmentID, deviceID, MaxActivations)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrTooManyActivations
	}
	return nil
}

// RecordActivation guarda a sessão de SMS da ativação do agendamento.
func (s *Service) RecordActivation(ctx context.Context, appointmentID, sessionID uuid.UUID) error {
	_, err := s.db.Exec(ctx, `UPDATE installation_appointments SET activation_session_id = $2, updated_at = NOW() WHERE id = $1`,
		appointmentID, sessionID)
	return database.MapError(err)
}

// ReservedElsewhere diz se o rastreador está reservado pela ativação de
// outro agendamento aberto (não pode ser instalado neste).
func (s *Service) ReservedElsewhere(ctx context.Context, appointmentID, deviceID uuid.UUID) (bool, error) {
	var reserved bool
	err := s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM installation_appointments
		WHERE activation_device_id = $2 AND status IN ('scheduled', 'arrived') AND id <> $1)`, appointmentID, deviceID).
		Scan(&reserved)
	return reserved, database.MapError(err)
}

// StandaloneActivationOf é a ativação da instalação avulsa (o rastreador é o
// do link).
func (s *Service) StandaloneActivationOf(ctx context.Context, id uuid.UUID) (Activation, error) {
	var a Activation
	var device uuid.UUID
	err := s.db.QueryRow(ctx, `SELECT device_id, activation_session_id, activations FROM standalone_installs WHERE id = $1`, id).
		Scan(&device, &a.SessionID, &a.Count)
	a.DeviceID = &device
	return a, database.MapError(err)
}

// ReserveStandaloneActivation conta mais uma ativação da instalação avulsa
// (no teto, recusa).
func (s *Service) ReserveStandaloneActivation(ctx context.Context, id uuid.UUID) error {
	tag, err := s.db.Exec(ctx, `
		UPDATE standalone_installs SET activations = activations + 1, updated_at = NOW()
		WHERE id = $1 AND status = 'open' AND activations < $2`, id, MaxActivations)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrTooManyActivations
	}
	return nil
}

// RecordStandaloneActivation guarda a sessão de SMS da ativação da avulsa.
func (s *Service) RecordStandaloneActivation(ctx context.Context, id, sessionID uuid.UUID) error {
	_, err := s.db.Exec(ctx, `UPDATE standalone_installs SET activation_session_id = $2, updated_at = NOW() WHERE id = $1`,
		id, sessionID)
	return database.MapError(err)
}

// ReleaseActivation devolve a ativação contada quando os SMS nem saíram (o
// cadastro precisa de ajuste da central): ela não gastou nada.
func (s *Service) ReleaseActivation(ctx context.Context, appointmentID uuid.UUID) error {
	_, err := s.db.Exec(ctx, `UPDATE installation_appointments SET activations = GREATEST(activations - 1, 0), updated_at = NOW()
		WHERE id = $1`, appointmentID)
	return database.MapError(err)
}

// ReleaseStandaloneActivation é o ReleaseActivation da instalação avulsa.
func (s *Service) ReleaseStandaloneActivation(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.Exec(ctx, `UPDATE standalone_installs SET activations = GREATEST(activations - 1, 0), updated_at = NOW()
		WHERE id = $1`, id)
	return database.MapError(err)
}
