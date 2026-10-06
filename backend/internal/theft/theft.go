// Package theft é o modo roubo. Sem central de monitoramento, quem age é o
// cliente: ele (ou quem ele autorizou a bloquear o motor) avisa que o veículo
// foi roubado; o rastreador passa a mandar a posição com mais frequência,
// inclusive parado; quem tem acesso ao veículo é avisado; e um link público,
// sem login, mostra a posição ao vivo para a polícia. Encerra quando o dono
// marca o veículo como recuperado (ou desativa) ou quando o prazo acaba — a
// bateria do veículo não aguenta o intervalo curto para sempre.
package theft

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/commands"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/devices"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/mail"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/protocols"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/protocols/gt06"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/push"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/webpush"
)

// Situação dos comandos ao rastreador.
const (
	StatusPending = "PENDING" // ainda não chegou (fora do ar: vai quando ele voltar)
	StatusSent    = "SENT"    // entregue ao rastreador
	StatusSkipped = "SKIPPED" // o modelo não muda o intervalo (ou não precisou)
)

// Como o modo roubo terminou.
const (
	OutcomeRecovered = "RECOVERED" // o dono recuperou o veículo
	OutcomeCancelled = "CANCELLED" // o dono desativou (alarme falso, por exemplo)
	OutcomeExpired   = "EXPIRED"   // o prazo acabou
)

var (
	// ErrNotFound: veículo inexistente.
	ErrNotFound = database.ErrNotFound
	// ErrNotActive: o modo roubo não está ligado neste veículo.
	ErrNotActive = errors.New("o modo roubo não está ligado neste veículo")
	// ErrLinkEnded: o link público não vale mais (encerrado ou inválido).
	ErrLinkEnded = errors.New("este link de localização não está mais ativo")
)

// RuleError é um pedido recusado pelas regras; a mensagem vai para a tela.
type RuleError struct{ Message string }

func (e RuleError) Error() string { return e.Message }

// Config são os tempos e os intervalos do modo roubo.
type Config struct {
	// Duration: quanto tempo fica ligado antes de desligar sozinho.
	Duration time.Duration
	// Reminder: de quanto em quanto tempo o dono é lembrado.
	Reminder time.Duration
	// ParkedSeconds: a posição com o veículo parado, no modo roubo.
	ParkedSeconds int
	// ReportSeconds e NormalParkedSeconds: o intervalo de sempre (em
	// movimento, o do cadastro do rastreador tem precedência).
	ReportSeconds       int
	NormalParkedSeconds int
	// AppURL monta o link público; LinkSecret o assina.
	AppURL     string
	LinkSecret []byte
}

// Mode é um modo roubo (ligado ou já encerrado).
type Mode struct {
	ID              uuid.UUID  `json:"id"`
	VehicleID       uuid.UUID  `json:"vehicleId"`
	ActivatedBy     *uuid.UUID `json:"-"`
	ActivatedByName string     `json:"activatedByName"`
	ActivatedAt     time.Time  `json:"activatedAt"`
	ExpiresAt       time.Time  `json:"expiresAt"`
	RemindedAt      *time.Time `json:"-"`
	// BoostStatus: o intervalo curto chegou ao rastreador? BoostCommandStatus
	// é o do comando (SENT, ACKNOWLEDGED...), quando foi pela conexão dele.
	BoostStatus        string     `json:"boostStatus"`
	BoostCommandID     *uuid.UUID `json:"-"`
	BoostCommandStatus string     `json:"boostCommandStatus"`
	BoostSMSAt         *time.Time `json:"boostSmsAt"`
	EndedAt            *time.Time `json:"endedAt"`
	EndedBy            *uuid.UUID `json:"-"`
	EndedByName        string     `json:"-"`
	Outcome            string     `json:"outcome"`
	RestoreStatus      string     `json:"restoreStatus"`
}

// Active diz se o modo ainda vale agora.
func (m *Mode) Active(now time.Time) bool { return m.EndedAt == nil && now.Before(m.ExpiresAt) }

// Brief é o modo roubo ligado, na lista de veículos.
type Brief struct {
	Since     time.Time `json:"since"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// Commander manda comandos ao rastreador (commands.Service).
type Commander interface {
	Send(ctx context.Context, req commands.Request) (*commands.Command, error)
}

// Devices lê o rastreador (devices.Service).
type Devices interface {
	Get(ctx context.Context, id uuid.UUID) (*devices.Device, error)
}

// Texter manda um comando por SMS ao chip (smssetup.Service). Opcional.
type Texter interface {
	Enabled() bool
	SendText(ctx context.Context, dev *devices.Device, text string) error
}

// Pusher avisa no celular (push.Service). Opcional.
type Pusher interface {
	Notify(ctx context.Context, userID uuid.UUID, n push.Notification) (int, error)
}

// Mailer manda os e-mails (mail.TheftMailer).
type Mailer interface {
	TheftActivated(ctx context.Context, to, name string, n mail.TheftNotice) error
	TheftEnded(ctx context.Context, to, name string, n mail.TheftNotice) error
	TheftReminder(ctx context.Context, to, name string, n mail.TheftNotice) error
	TheftExpired(ctx context.Context, to, name string, n mail.TheftNotice) error
}

type Service struct {
	db       *database.DB
	devices  Devices
	commands Commander
	online   func(imei string) bool
	texter   Texter
	pusher   Pusher
	mailer   Mailer
	cfg      Config
	key      []byte
	now      func() time.Time
	runAsync func(func())
	log      *slog.Logger
}

func NewService(db *database.DB, devs Devices, cmds Commander, online func(imei string) bool, mailer Mailer,
	cfg Config, log *slog.Logger) *Service {
	mac := hmac.New(sha256.New, cfg.LinkSecret)
	mac.Write([]byte("farbo-theft-link-v1"))
	cfg.AppURL = strings.TrimRight(cfg.AppURL, "/")
	return &Service{
		db: db, devices: devs, commands: cmds, online: online, mailer: mailer, cfg: cfg, key: mac.Sum(nil),
		now: time.Now, runAsync: func(f func()) { go f() }, log: log.With("component", "theft"),
	}
}

// SetTexter liga o SMS de reserva (rastreador fora do ar).
func (s *Service) SetTexter(t Texter) { s.texter = t }

// SetPusher liga o aviso no celular.
func (s *Service) SetPusher(p Pusher) { s.pusher = p }

// SetClock troca o relógio (testes).
func (s *Service) SetClock(now func() time.Time) { s.now = now }

// SetSync faz os avisos saírem na hora (testes).
func (s *Service) SetSync() { s.runAsync = func(f func()) { f() } }

// Settings é o que a tela explica: o intervalo parado e o prazo.
func (s *Service) Settings() (parkedSeconds int, duration time.Duration) {
	return s.cfg.ParkedSeconds, s.cfg.Duration
}

// ---------------------------------------------------------------------------
// Leitura
// ---------------------------------------------------------------------------

const selectMode = `
	SELECT t.id, t.vehicle_id, t.activated_by, COALESCE(a.name, ''), t.activated_at, t.expires_at, t.reminded_at,
		t.boost_status, t.boost_command_id, COALESCE(c.status, ''), t.boost_sms_at,
		t.ended_at, t.ended_by, COALESCE(e.name, ''), COALESCE(t.outcome, ''), COALESCE(t.restore_status, '')
	FROM theft_modes t
	LEFT JOIN users a ON a.id = t.activated_by
	LEFT JOIN users e ON e.id = t.ended_by
	LEFT JOIN device_commands c ON c.id = t.boost_command_id`

func scan(row database.Scanner) (*Mode, error) {
	var m Mode
	if err := row.Scan(&m.ID, &m.VehicleID, &m.ActivatedBy, &m.ActivatedByName, &m.ActivatedAt, &m.ExpiresAt,
		&m.RemindedAt, &m.BoostStatus, &m.BoostCommandID, &m.BoostCommandStatus, &m.BoostSMSAt,
		&m.EndedAt, &m.EndedBy, &m.EndedByName, &m.Outcome, &m.RestoreStatus); err != nil {
		return nil, database.MapError(err)
	}
	return &m, nil
}

func (s *Service) get(ctx context.Context, id uuid.UUID) (*Mode, error) {
	return scan(s.db.QueryRow(ctx, selectMode+` WHERE t.id = $1`, id))
}

// Active é o modo roubo ligado do veículo; nil se não houver.
func (s *Service) Active(ctx context.Context, vehicleID uuid.UUID) (*Mode, error) {
	m, err := scan(s.db.QueryRow(ctx, selectMode+` WHERE t.vehicle_id = $1 AND t.ended_at IS NULL`, vehicleID))
	if errors.Is(err, database.ErrNotFound) {
		return nil, nil
	}
	return m, err
}

// ActiveBriefs: quais destes veículos estão em modo roubo (a lista).
func (s *Service) ActiveBriefs(ctx context.Context, vehicleIDs []uuid.UUID) (map[uuid.UUID]Brief, error) {
	out := map[uuid.UUID]Brief{}
	if len(vehicleIDs) == 0 {
		return out, nil
	}
	rows, err := s.db.Query(ctx, `
		SELECT vehicle_id, activated_at, expires_at FROM theft_modes
		WHERE ended_at IS NULL AND vehicle_id = ANY($1)`, vehicleIDs)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var b Brief
		if err := rows.Scan(&id, &b.Since, &b.ExpiresAt); err != nil {
			return nil, database.MapError(err)
		}
		out[id] = b
	}
	return out, database.MapError(rows.Err())
}

// ---------------------------------------------------------------------------
// O link público (para a polícia)
// ---------------------------------------------------------------------------

var linkEncoding = base64.RawURLEncoding

// Token é a parte secreta do link: o id do modo e a assinatura dele. Nada
// secreto fica no banco; o link morre quando o modo encerra.
func (s *Service) Token(id uuid.UUID) string {
	mac := hmac.New(sha256.New, s.key)
	mac.Write(id[:])
	return linkEncoding.EncodeToString(append(id[:], mac.Sum(nil)[:16]...))
}

// PublicURL é o link da posição ao vivo.
func (s *Service) PublicURL(id uuid.UUID) string { return s.cfg.AppURL + "/localizar/" + s.Token(id) }

// ByToken é o modo roubo de um link público, enquanto ele estiver ligado.
func (s *Service) ByToken(ctx context.Context, token string) (*Mode, error) {
	raw, err := linkEncoding.DecodeString(strings.TrimSpace(token))
	if err != nil || len(raw) != 32 {
		return nil, ErrLinkEnded
	}
	id, err := uuid.FromBytes(raw[:16])
	if err != nil || !hmac.Equal([]byte(s.Token(id)), []byte(strings.TrimSpace(token))) {
		return nil, ErrLinkEnded
	}
	m, err := s.get(ctx, id)
	if errors.Is(err, database.ErrNotFound) {
		return nil, ErrLinkEnded
	}
	if err != nil {
		return nil, err
	}
	if !m.Active(s.now()) {
		return nil, ErrLinkEnded
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// Ligar e desligar
// ---------------------------------------------------------------------------

// Activate liga o modo roubo do veículo (ou devolve o que já está ligado,
// com created false): manda o intervalo curto ao rastreador e avisa quem tem
// acesso.
func (s *Service) Activate(ctx context.Context, vehicleID uuid.UUID, actor *uuid.UUID) (*Mode, bool, error) {
	var deviceID *uuid.UUID
	if err := s.db.QueryRow(ctx, `SELECT device_id FROM vehicles WHERE id = $1`, vehicleID).Scan(&deviceID); err != nil {
		return nil, false, database.MapError(err)
	}
	if deviceID == nil {
		return nil, false, RuleError{"este veículo ainda não tem rastreador instalado"}
	}
	now := s.now()
	var id uuid.UUID
	err := s.db.QueryRow(ctx, `
		INSERT INTO theft_modes (vehicle_id, activated_by, activated_at, expires_at) VALUES ($1, $2, $3, $4)
		ON CONFLICT (vehicle_id) WHERE ended_at IS NULL DO NOTHING
		RETURNING id`, vehicleID, actor, now, now.Add(s.cfg.Duration)).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		m, err := s.Active(ctx, vehicleID)
		if err == nil && m == nil {
			err = ErrNotActive
		}
		return m, false, err
	}
	if err != nil {
		return nil, false, database.MapError(err)
	}
	m, err := s.get(ctx, id)
	if err != nil {
		return nil, false, err
	}
	s.boost(ctx, m, true)
	if m, err = s.get(ctx, id); err != nil {
		return nil, false, err
	}
	s.log.Info("modo roubo ativado", "vehicle", vehicleID, "mode", id, "boost", m.BoostStatus)
	activated := *m
	s.runAsync(func() { s.notifyActivated(context.WithoutCancel(ctx), &activated) })
	return m, true, nil
}

// End encerra o modo roubo: volta o rastreador ao intervalo normal e avisa
// quem tem acesso. actor nil: o prazo acabou.
func (s *Service) End(ctx context.Context, vehicleID uuid.UUID, actor *uuid.UUID, outcome string) (*Mode, error) {
	switch outcome {
	case OutcomeRecovered, OutcomeCancelled, OutcomeExpired:
	default:
		return nil, RuleError{"desfecho inválido"}
	}
	var id uuid.UUID
	// Só volta o intervalo se ele mudou (ou pode ter mudado, pelo SMS).
	err := s.db.QueryRow(ctx, `
		UPDATE theft_modes SET ended_at = $2, ended_by = $3, outcome = $4,
			restore_status = CASE WHEN boost_status = 'SENT' OR boost_sms_at IS NOT NULL THEN 'PENDING' ELSE 'SKIPPED' END,
			updated_at = NOW()
		WHERE vehicle_id = $1 AND ended_at IS NULL
		RETURNING id`, vehicleID, s.now(), actor, outcome).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotActive
	}
	if err != nil {
		return nil, database.MapError(err)
	}
	m, err := s.get(ctx, id)
	if err != nil {
		return nil, err
	}
	s.restore(ctx, m, true)
	if m, err = s.get(ctx, id); err != nil {
		return nil, err
	}
	s.log.Info("modo roubo encerrado", "vehicle", vehicleID, "mode", id, "outcome", outcome, "restore", m.RestoreStatus)
	ended := *m
	s.runAsync(func() { s.notifyEnded(context.WithoutCancel(ctx), &ended) })
	return m, nil
}

// ---------------------------------------------------------------------------
// O rastreador
// ---------------------------------------------------------------------------

// timer é o comando do intervalo (TIMER,em movimento,parado#), testado em
// campo no J16 (GT06). Protocolo em branco: o aparelho ainda não conectou,
// e o SMS de configuração já trata como GT06. Outros modelos: não muda.
func (s *Service) timer(dev *devices.Device, parked int) (string, bool) {
	if dev.Protocol != "" && dev.Protocol != gt06.Name {
		return "", false
	}
	report := s.cfg.ReportSeconds
	if dev.ReportIntervalSeconds != nil && *dev.ReportIntervalSeconds > 0 {
		report = *dev.ReportIntervalSeconds
	}
	if report < 5 {
		report = 10
	}
	return fmt.Sprintf("TIMER,%d,%d#", report, parked), true
}

// vehicleDevice é o rastreador atual do veículo (nil sem rastreador).
func (s *Service) vehicleDevice(ctx context.Context, vehicleID uuid.UUID) (*devices.Device, error) {
	var deviceID *uuid.UUID
	if err := s.db.QueryRow(ctx, `SELECT device_id FROM vehicles WHERE id = $1`, vehicleID).Scan(&deviceID); err != nil {
		return nil, database.MapError(err)
	}
	if deviceID == nil {
		return nil, nil
	}
	return s.devices.Get(ctx, *deviceID)
}

// sendTimer manda o TIMER pela conexão do rastreador; false se ele não
// estava conectado (ou o envio falhou).
func (s *Service) sendTimer(ctx context.Context, dev *devices.Device, vehicleID uuid.UUID, text string) (*uuid.UUID, bool) {
	if dev.Protocol == "" || s.online == nil || !s.online(dev.IMEI) {
		return nil, false
	}
	cmd, err := s.commands.Send(ctx, commands.Request{
		Device: dev, VehicleID: &vehicleID, Type: protocols.CommandCustom, RawOverride: text,
	})
	if err != nil {
		s.log.Warn("falha ao mandar o intervalo do modo roubo", "vehicle", vehicleID, "err", err)
		return nil, false
	}
	if cmd.Status == commands.StatusFailed || cmd.Status == commands.StatusRejected {
		return nil, false
	}
	return &cmd.ID, true
}

// boost manda o intervalo curto; fora do ar, fica pendente (e, na ativação,
// vai também por SMS, se houver).
func (s *Service) boost(ctx context.Context, m *Mode, allowSMS bool) {
	dev, err := s.vehicleDevice(ctx, m.VehicleID)
	if err != nil || dev == nil {
		return
	}
	text, ok := s.timer(dev, s.cfg.ParkedSeconds)
	if !ok {
		s.setBoost(ctx, m.ID, StatusSkipped, nil)
		return
	}
	if commandID, sent := s.sendTimer(ctx, dev, m.VehicleID, text); sent {
		s.setBoost(ctx, m.ID, StatusSent, commandID)
		return
	}
	if allowSMS && m.BoostSMSAt == nil && s.texter != nil && s.texter.Enabled() {
		if err := s.texter.SendText(ctx, dev, text); err != nil {
			s.log.Warn("falha ao mandar o intervalo do modo roubo por SMS", "vehicle", m.VehicleID, "err", err)
			return
		}
		if _, err := s.db.Exec(ctx, `UPDATE theft_modes SET boost_sms_at = $2, updated_at = NOW() WHERE id = $1`,
			m.ID, s.now()); err != nil {
			s.log.Error("falha ao gravar o SMS do modo roubo", "mode", m.ID, "err", err)
		}
	}
}

func (s *Service) setBoost(ctx context.Context, id uuid.UUID, status string, commandID *uuid.UUID) {
	if _, err := s.db.Exec(ctx, `
		UPDATE theft_modes SET boost_status = $2, boost_command_id = COALESCE($3, boost_command_id), updated_at = NOW()
		WHERE id = $1 AND boost_status = 'PENDING'`, id, status, commandID); err != nil {
		s.log.Error("falha ao gravar o intervalo do modo roubo", "mode", id, "err", err)
	}
}

// restore volta o intervalo normal; fora do ar, fica pendente.
func (s *Service) restore(ctx context.Context, m *Mode, allowSMS bool) {
	if m.RestoreStatus != StatusPending {
		return
	}
	dev, err := s.vehicleDevice(ctx, m.VehicleID)
	if err != nil {
		return
	}
	if dev == nil {
		s.setRestore(ctx, m.ID, StatusSkipped, nil)
		return
	}
	text, ok := s.timer(dev, s.cfg.NormalParkedSeconds)
	if !ok {
		s.setRestore(ctx, m.ID, StatusSkipped, nil)
		return
	}
	if commandID, sent := s.sendTimer(ctx, dev, m.VehicleID, text); sent {
		s.setRestore(ctx, m.ID, StatusSent, commandID)
		return
	}
	if allowSMS && s.texter != nil && s.texter.Enabled() {
		if err := s.texter.SendText(ctx, dev, text); err != nil {
			s.log.Warn("falha ao voltar o intervalo do modo roubo por SMS", "vehicle", m.VehicleID, "err", err)
		}
	}
}

func (s *Service) setRestore(ctx context.Context, id uuid.UUID, status string, commandID *uuid.UUID) {
	if _, err := s.db.Exec(ctx, `
		UPDATE theft_modes SET restore_status = $2, restore_command_id = COALESCE($3, restore_command_id), updated_at = NOW()
		WHERE id = $1 AND restore_status = 'PENDING'`, id, status, commandID); err != nil {
		s.log.Error("falha ao gravar a volta do intervalo", "mode", id, "err", err)
	}
}

// ---------------------------------------------------------------------------
// A rotina
// ---------------------------------------------------------------------------

// Work: desliga os vencidos, lembra o dono uma vez por dia e manda ao
// rastreador o que ficou pendente (ele voltou a conectar).
func (s *Service) Work(ctx context.Context) {
	now := s.now()
	for _, vehicleID := range s.ids(ctx, `SELECT vehicle_id FROM theft_modes WHERE ended_at IS NULL AND expires_at <= $1`, now) {
		if _, err := s.End(ctx, vehicleID, nil, OutcomeExpired); err != nil && !errors.Is(err, ErrNotActive) {
			s.log.Error("falha ao desligar o modo roubo vencido", "vehicle", vehicleID, "err", err)
		}
	}
	if s.cfg.Reminder > 0 {
		for _, id := range s.ids(ctx, `
			UPDATE theft_modes SET reminded_at = $1, updated_at = NOW()
			WHERE ended_at IS NULL AND COALESCE(reminded_at, activated_at) <= $2
			RETURNING id`, now, now.Add(-s.cfg.Reminder)) {
			if m, err := s.get(ctx, id); err == nil {
				s.notifyReminder(ctx, m)
			}
		}
	}
	for _, id := range s.ids(ctx, `SELECT id FROM theft_modes WHERE ended_at IS NULL AND boost_status = 'PENDING'`) {
		if m, err := s.get(ctx, id); err == nil {
			s.boost(ctx, m, false)
		}
	}
	// A volta ao normal: tenta por um mês (um rastreador que nunca mais
	// conecta não fica para sempre na lista).
	for _, id := range s.ids(ctx, `SELECT id FROM theft_modes WHERE restore_status = 'PENDING' AND ended_at > $1`,
		now.Add(-30*24*time.Hour)) {
		if m, err := s.get(ctx, id); err == nil {
			s.restore(ctx, m, false)
		}
	}
}

func (s *Service) ids(ctx context.Context, query string, args ...any) []uuid.UUID {
	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		s.log.Error("falha ao ler os modos roubo", "err", err)
		return nil
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			s.log.Error("falha ao ler os modos roubo", "err", err)
			return out
		}
		out = append(out, id)
	}
	return out
}

// ---------------------------------------------------------------------------
// Os avisos
// ---------------------------------------------------------------------------

// person é quem recebe os avisos: o dono e quem tem acesso ao veículo.
type person struct {
	id    uuid.UUID
	name  string
	email string
	owner bool
}

func (s *Service) people(ctx context.Context, vehicleID uuid.UUID) ([]person, error) {
	// O acesso de terceiro só vale enquanto quem deu for o dono do veículo.
	rows, err := s.db.Query(ctx, `
		SELECT u.id, u.name, u.email, TRUE FROM vehicles v JOIN users u ON u.id = v.owner_id
		WHERE v.id = $1 AND u.active
		UNION ALL
		SELECT u.id, u.name, u.email, FALSE FROM vehicle_shares sh
		JOIN vehicles v ON v.id = sh.vehicle_id AND v.owner_id = sh.owner_id
		JOIN users u ON u.id = sh.guest_id
		WHERE sh.vehicle_id = $1 AND u.active`, vehicleID)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	var out []person
	for rows.Next() {
		var p person
		if err := rows.Scan(&p.id, &p.name, &p.email, &p.owner); err != nil {
			return nil, database.MapError(err)
		}
		out = append(out, p)
	}
	return out, database.MapError(rows.Err())
}

func (s *Service) notice(ctx context.Context, m *Mode, actor string) (mail.TheftNotice, error) {
	n := mail.TheftNotice{
		VehicleID: m.VehicleID.String(), ActorName: actor, ActivatedAt: m.ActivatedAt, ExpiresAt: m.ExpiresAt,
		Recovered: m.Outcome == OutcomeRecovered,
	}
	err := s.db.QueryRow(ctx, `SELECT name, COALESCE(plate, '') FROM vehicles WHERE id = $1`, m.VehicleID).
		Scan(&n.VehicleName, &n.VehiclePlate)
	if m.Active(s.now()) {
		n.PublicURL = s.PublicURL(m.ID)
	}
	return n, database.MapError(err)
}

// deliver manda o e-mail e o push a cada pessoa (skip: quem não recebe).
func (s *Service) deliver(ctx context.Context, m *Mode, kind string, people []person, n mail.TheftNotice,
	send func(context.Context, string, string, mail.TheftNotice) error, pushTitle, pushBody string, skipPush *uuid.UUID) {
	for _, p := range people {
		if p.email != "" {
			if err := send(ctx, p.email, p.name, n); err != nil {
				s.log.Warn("falha no e-mail do modo roubo", "kind", kind, "mode", m.ID, "err", err)
			}
		}
		if s.pusher == nil || (skipPush != nil && *skipPush == p.id) {
			continue
		}
		if _, err := s.pusher.Notify(ctx, p.id, push.Notification{
			Title: pushTitle, Body: pushBody, URL: "/app/veiculos/" + m.VehicleID.String(),
			Tag: "theft:" + m.VehicleID.String(), Severity: mail.SeverityCritical,
			Urgency: webpush.UrgencyHigh, TTL: 6 * time.Hour,
		}); err != nil {
			s.log.Warn("falha no push do modo roubo", "kind", kind, "mode", m.ID, "err", err)
		}
	}
}

// notifyActivated: todos recebem o e-mail (com o link e o que fazer); o
// push vai para os outros (quem ativou já está com a tela aberta).
func (s *Service) notifyActivated(ctx context.Context, m *Mode) {
	people, err := s.people(ctx, m.VehicleID)
	if err != nil {
		s.log.Error("falha ao listar quem avisar do modo roubo", "mode", m.ID, "err", err)
		return
	}
	n, err := s.notice(ctx, m, m.ActivatedByName)
	if err != nil {
		s.log.Error("falha ao montar o aviso do modo roubo", "mode", m.ID, "err", err)
		return
	}
	actor := strings.TrimSpace(m.ActivatedByName)
	if actor == "" {
		actor = "Alguém com acesso"
	}
	s.deliver(ctx, m, "activated", people, n, s.mailer.TheftActivated,
		"Modo roubo: "+n.VehicleName, actor+" ativou o modo roubo. Toque para ver a posição ao vivo e o que fazer.",
		m.ActivatedBy)
}

// notifyEnded: avisa todos (menos o push de quem encerrou) — ou, no prazo,
// que ele desligou sozinho.
func (s *Service) notifyEnded(ctx context.Context, m *Mode) {
	people, err := s.people(ctx, m.VehicleID)
	if err != nil {
		s.log.Error("falha ao listar quem avisar do fim do modo roubo", "mode", m.ID, "err", err)
		return
	}
	n, err := s.notice(ctx, m, m.EndedByName)
	if err != nil {
		s.log.Error("falha ao montar o aviso do fim do modo roubo", "mode", m.ID, "err", err)
		return
	}
	switch m.Outcome {
	case OutcomeExpired:
		s.deliver(ctx, m, "expired", people, n, s.mailer.TheftExpired,
			"Modo roubo desligado: "+n.VehicleName, "O prazo acabou e o modo roubo desligou sozinho. Se ainda precisar, ative de novo.", nil)
	case OutcomeRecovered:
		s.deliver(ctx, m, "recovered", people, n, s.mailer.TheftEnded,
			"Veículo recuperado: "+n.VehicleName, "O modo roubo foi encerrado: o veículo foi recuperado.", m.EndedBy)
	default:
		s.deliver(ctx, m, "cancelled", people, n, s.mailer.TheftEnded,
			"Modo roubo desativado: "+n.VehicleName, "O modo roubo foi desativado.", m.EndedBy)
	}
}

// notifyReminder: só o dono, uma vez por dia.
func (s *Service) notifyReminder(ctx context.Context, m *Mode) {
	people, err := s.people(ctx, m.VehicleID)
	if err != nil {
		s.log.Error("falha ao listar quem lembrar do modo roubo", "mode", m.ID, "err", err)
		return
	}
	owners := people[:0]
	for _, p := range people {
		if p.owner {
			owners = append(owners, p)
		}
	}
	n, err := s.notice(ctx, m, "")
	if err != nil {
		s.log.Error("falha ao montar o lembrete do modo roubo", "mode", m.ID, "err", err)
		return
	}
	s.deliver(ctx, m, "reminder", owners, n, s.mailer.TheftReminder,
		"O modo roubo continua ligado: "+n.VehicleName, "Se o veículo já foi recuperado, marque no app.", nil)
}
