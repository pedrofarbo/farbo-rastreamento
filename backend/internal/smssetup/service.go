package smssetup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/devices"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/smsdev"
)

// Sender é o SMSDev (ou o falso dos testes): manda o SMS, consulta a entrega
// e lê as respostas recebidas.
type Sender interface {
	Send(ctx context.Context, to, body string) (*smsdev.Message, error)
	Fetch(ctx context.Context, id string) (*smsdev.Message, error)
	Inbox(ctx context.Context, since time.Time) ([]smsdev.Reply, error)
	Balance(ctx context.Context) (int, error)
}

// Activator marca o pedido como "Configurado" (com o aparelho vinculado ao
// veículo) quando o rastreador conecta.
type Activator interface {
	AutoConfigure(ctx context.Context, fulfillmentID, deviceID uuid.UUID, note string) error
}

// Status de uma configuração.
const (
	StatusSending  = "SENDING"
	StatusWaiting  = "WAITING"
	StatusDone     = "DONE"
	StatusFailed   = "FAILED"
	StatusTimeout  = "TIMEOUT"
	StatusCanceled = "CANCELED"
)

const (
	// O próximo comando sai quando o rastreador responde o anterior, quando
	// o anterior foi entregue há deliveredGap ou, sem notícia nenhuma, depois
	// de silentGap (no Brasil, a confirmação de entrega nem sempre volta).
	deliveredGap = 15 * time.Second
	silentGap    = 90 * time.Second
	// connectTimeout: quanto esperar o rastreador conectar depois do último SMS.
	connectTimeout = 20 * time.Minute
	// maxAttempts: falhas seguidas do SMSDev (rede) antes de desistir.
	maxAttempts = 3
	// fetchAfter: depois de quanto tempo consulta a entrega no SMSDev.
	fetchAfter = 20 * time.Second
	// inboxWindow: quanto tempo depois do último SMS enviado ainda lê as
	// respostas (a do modo roubo também).
	inboxWindow = 6 * time.Hour
	// ActivationSMS: quantos SMS uma ativação padrão manda (APN, servidor,
	// fuso e intervalo; os opcionais somam 1 cada).
	ActivationSMS = 4
	// balanceTTL: por quanto tempo o saldo lido no SMSDev vale.
	balanceTTL = time.Minute
)

// ValidationError é um pedido recusado; a mensagem vai para a tela.
type ValidationError struct{ Message string }

func (e ValidationError) Error() string { return e.Message }

func invalid(format string, args ...any) error {
	return ValidationError{Message: fmt.Sprintf(format, args...)}
}

// ErrDisabled: o SMSDev não está configurado.
var ErrDisabled = ValidationError{Message: "O envio de SMS não está configurado (SMSDEV_API_KEY)."}

type Service struct {
	// O último saldo lido no SMSDev (vale balanceTTL).
	balanceMu sync.Mutex
	balance   *int
	balanceAt time.Time

	db        *database.DB
	devices   *devices.Service
	sender    Sender
	activator Activator
	defaults  Defaults
	from      string
	log       *slog.Logger
	now       func() time.Time
}

func NewService(db *database.DB, devs *devices.Service, sender Sender, activator Activator, defaults Defaults,
	from string, log *slog.Logger) *Service {
	return &Service{
		db: db, devices: devs, sender: sender, activator: activator, defaults: defaults,
		from: from, log: log.With("component", "smssetup"), now: time.Now,
	}
}

// SetClock troca o relógio (testes).
func (s *Service) SetClock(now func() time.Time) { s.now = now }

// Enabled diz se dá para mandar SMS.
func (s *Service) Enabled() bool { return s != nil && s.sender != nil }

// From é quem envia (para a tela).
func (s *Service) From() string { return s.from }

// Usage é o contador da página de rastreadores: o saldo no SMSDev, quantas
// ativações ele paga e o que saiu nos últimos 30 dias.
type Usage struct {
	Enabled bool `json:"enabled"`
	// Balance é o saldo em SMS (nulo se não deu para ler; BalanceError diz por quê).
	Balance      *int   `json:"balance"`
	BalanceError string `json:"balanceError"`
	// ActivationSMS: SMS por ativação padrão; Activations: quantas o saldo paga.
	ActivationSMS int `json:"activationSms"`
	Activations   int `json:"activations"`
	// SentLast30Days: SMS que saíram (sem os recusados) nos últimos 30 dias.
	SentLast30Days int `json:"sentLast30Days"`
}

// Usage lê o saldo (guardado por um minuto) e conta os SMS do mês.
func (s *Service) Usage(ctx context.Context) (*Usage, error) {
	out := &Usage{Enabled: s.Enabled(), ActivationSMS: ActivationSMS}
	if err := s.db.QueryRow(ctx, `
		SELECT count(*) FROM sms_messages WHERE direction = 'OUT' AND status <> 'failed' AND created_at > $1`,
		s.now().AddDate(0, 0, -30)).Scan(&out.SentLast30Days); err != nil {
		return nil, database.MapError(err)
	}
	if !out.Enabled {
		return out, nil
	}
	s.balanceMu.Lock()
	defer s.balanceMu.Unlock()
	if s.balance == nil || s.now().Sub(s.balanceAt) >= balanceTTL {
		n, err := s.sender.Balance(ctx)
		if err != nil {
			s.log.Warn("falha ao ler o saldo no SMSDev", "err", err)
			out.BalanceError = providerMessage(err)
			return out, nil
		}
		s.balance, s.balanceAt = &n, s.now()
	}
	balance := *s.balance
	out.Balance, out.Activations = &balance, balance/ActivationSMS
	return out, nil
}

// Plan é o que vai sair, antes de confirmar.
type Plan struct {
	Phone    string   `json:"phone"`
	Steps    []Step   `json:"steps"`
	Problems []string `json:"problems"`
	// Os opcionais, para a tela oferecer.
	Unlock Step `json:"unlock"`
	Query  Step `json:"query"`
}

// Plan mostra os comandos do rastreador (redigidos) e o que falta.
func (s *Service) Plan(ctx context.Context, deviceID uuid.UUID) (*Plan, error) {
	dev, err := s.devices.Get(ctx, deviceID)
	if err != nil {
		return nil, err
	}
	plan := &Plan{Phone: E164(dev.PhoneNumber), Steps: []Step{}, Problems: []string{}}
	if plan.Phone == "" {
		plan.Problems = append(plan.Problems, "Cadastre o número do chip (com DDD) no rastreador.")
	}
	steps, problems := buildSteps(dev, s.defaults, Options{Unlock: true, Query: true})
	plan.Problems = append(plan.Problems, problems...)
	for _, st := range steps {
		switch st.Kind {
		case StepUnlock:
			plan.Unlock = st.Step
		case StepParam:
			plan.Query = st.Step
		default:
			plan.Steps = append(plan.Steps, st.Step)
		}
	}
	return plan, nil
}

// Start começa a configuração: grava os passos e manda o primeiro SMS na hora
// (uma recusa do SMSDev — número inválido, conta sem saldo — aparece já).
func (s *Service) Start(ctx context.Context, deviceID uuid.UUID, fulfillmentID *uuid.UUID, opts Options, by *uuid.UUID) (*Session, error) {
	if !s.Enabled() {
		return nil, ErrDisabled
	}
	dev, err := s.devices.Get(ctx, deviceID)
	if err != nil {
		return nil, err
	}
	phone := E164(dev.PhoneNumber)
	if phone == "" {
		return nil, invalid("Cadastre o número do chip (com DDD) no rastreador.")
	}
	steps, problems := buildSteps(dev, s.defaults, opts)
	if len(problems) > 0 {
		return nil, invalid("%s", strings.Join(problems, " "))
	}
	if fulfillmentID != nil {
		var tracker string
		err := s.db.QueryRow(ctx, `SELECT tracker_status FROM fulfillments WHERE id = $1`, *fulfillmentID).Scan(&tracker)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, invalid("Pedido não encontrado.")
		}
		if err != nil {
			return nil, database.MapError(err)
		}
		if tracker != "CONFIGURING" {
			return nil, invalid("O pedido precisa estar em \"Rastreador em configuração\".")
		}
	}
	shown := make([]Step, len(steps))
	for i, st := range steps {
		shown[i] = st.Step
	}
	raw, _ := json.Marshal(shown)
	var id uuid.UUID
	err = s.db.QueryRow(ctx, `
		INSERT INTO sms_setup_sessions (device_id, fulfillment_id, phone, steps, started_by)
		VALUES ($1, $2, $3, $4, $5) RETURNING id`, deviceID, fulfillmentID, phone, raw, by).Scan(&id)
	if errors.Is(database.MapError(err), database.ErrConflict) {
		return nil, invalid("Este rastreador já está sendo configurado por SMS.")
	}
	if err != nil {
		return nil, database.MapError(err)
	}
	s.log.Info("configuração por SMS iniciada", "session", id, "device", deviceID, "steps", len(steps))
	// O primeiro SMS sai agora; os outros, pelo Work.
	callCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if err := s.advance(callCtx, id, false); err != nil {
		return nil, err
	}
	return s.Session(callCtx, id)
}

// Cancel interrompe a configuração (os SMS já enviados não voltam).
func (s *Service) Cancel(ctx context.Context, id uuid.UUID) (*Session, error) {
	tag, err := s.db.Exec(ctx, `
		UPDATE sms_setup_sessions SET status = 'CANCELED', finished_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND status IN ('SENDING', 'WAITING')`, id)
	if err != nil {
		return nil, database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		if _, err := s.Session(ctx, id); err != nil {
			return nil, err
		}
		return nil, invalid("Esta configuração já terminou.")
	}
	return s.Session(ctx, id)
}

// Work avança as configurações em andamento: o próximo SMS, o rastreador que
// conectou, o tempo esgotado. Antes, consulta no SMSDev a entrega dos SMS e
// as respostas recebidas.
func (s *Service) Work(ctx context.Context) {
	if !s.Enabled() {
		return
	}
	s.refreshStatuses(ctx)
	s.pollInbox(ctx)
	rows, err := s.db.Query(ctx, `SELECT id FROM sms_setup_sessions WHERE status IN ('SENDING', 'WAITING') ORDER BY created_at LIMIT 50`)
	if err != nil {
		s.log.Error("falha ao listar as configurações por SMS", "err", err)
		return
	}
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		if err := s.advance(ctx, id, true); err != nil {
			s.log.Error("falha ao avançar a configuração por SMS", "session", id, "err", err)
		}
	}
}

type sessionRow struct {
	id           uuid.UUID
	deviceID     uuid.UUID
	fulfillment  *uuid.UUID
	phone        string
	status       string
	steps        []Step
	next         int
	attempts     int
	lastSentAt   *time.Time
	waitingSince *time.Time
}

// advance faz o que dá na configuração agora. skipLocked: o Work não espera
// por uma configuração que outra instância (ou o Start) está mexendo.
func (s *Service) advance(ctx context.Context, id uuid.UUID, skipLocked bool) error {
	var connected *struct {
		fulfillment *uuid.UUID
		device      uuid.UUID
	}
	lock := "FOR UPDATE"
	if skipLocked {
		lock = "FOR UPDATE SKIP LOCKED"
	}
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var r sessionRow
		var raw []byte
		err := tx.QueryRow(ctx, `
			SELECT id, device_id, fulfillment_id, phone, status, steps, next_step, attempts, last_sent_at, waiting_since
			FROM sms_setup_sessions WHERE id = $1 `+lock, id).
			Scan(&r.id, &r.deviceID, &r.fulfillment, &r.phone, &r.status, &raw, &r.next, &r.attempts, &r.lastSentAt, &r.waitingSince)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // outra instância está nela
		}
		if err != nil {
			return err
		}
		if err := json.Unmarshal(raw, &r.steps); err != nil {
			return err
		}
		switch r.status {
		case StatusSending:
			return s.sendNext(ctx, tx, &r)
		case StatusWaiting:
			ok, err := s.checkConnected(ctx, tx, &r)
			if ok {
				connected = &struct {
					fulfillment *uuid.UUID
					device      uuid.UUID
				}{r.fulfillment, r.deviceID}
			}
			return err
		}
		return nil
	})
	if err != nil {
		return mapErr(err)
	}
	if connected != nil && connected.fulfillment != nil && s.activator != nil {
		note := "Conectou no servidor depois da configuração por SMS"
		result := "O rastreador conectou e o pedido passou para \"Configurado\"."
		if err := s.activator.AutoConfigure(ctx, *connected.fulfillment, connected.device, note); err != nil {
			result = "O rastreador conectou, mas o pedido não mudou: " + err.Error()
			s.log.Warn("rastreador configurado por SMS, mas o pedido não mudou", "session", id, "err", err)
		}
		_, _ = s.db.Exec(ctx, `UPDATE sms_setup_sessions SET note = $2, updated_at = NOW() WHERE id = $1`, id, result)
	}
	return nil
}

// sendNext manda o próximo comando, se o anterior já teve resposta, entrega
// ou tempo; com todos enviados, passa a esperar a conexão.
func (s *Service) sendNext(ctx context.Context, tx pgx.Tx, r *sessionRow) error {
	now := s.now()
	if r.next > 0 {
		var status, code, message string
		var sentAt, updatedAt time.Time
		err := tx.QueryRow(ctx, `
			SELECT status, error_code, error_message, created_at, updated_at FROM sms_messages
			WHERE session_id = $1 AND direction = 'OUT' AND step = $2 ORDER BY created_at DESC LIMIT 1`, r.id, r.next-1).
			Scan(&status, &code, &message, &sentAt, &updatedAt)
		if err != nil {
			return err
		}
		if smsdev.Failed(status) {
			return s.finish(ctx, tx, r.id, StatusFailed, failureText(r.steps[r.next-1].Label, code, message))
		}
		var replied bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM sms_messages WHERE session_id = $1 AND direction = 'IN' AND created_at >= $2)`,
			r.id, sentAt).Scan(&replied); err != nil {
			return err
		}
		ready := replied || (status == smsdev.StatusDelivered && now.Sub(updatedAt) >= deliveredGap) || now.Sub(sentAt) >= silentGap
		if !ready {
			return nil
		}
	}
	if r.next >= len(r.steps) {
		_, err := tx.Exec(ctx, `
			UPDATE sms_setup_sessions SET status = 'WAITING', waiting_since = $2, updated_at = NOW() WHERE id = $1`, r.id, now)
		return err
	}
	// O texto de verdade sai do cadastro agora (as senhas não ficam gravadas).
	dev, err := s.devices.Get(ctx, r.deviceID)
	if err != nil {
		return err
	}
	steps, problems := buildSteps(dev, s.defaults, optionsOf(r.steps))
	if len(problems) > 0 || len(steps) != len(r.steps) || steps[r.next].Kind != r.steps[r.next].Kind {
		return s.finish(ctx, tx, r.id, StatusFailed, "O cadastro do rastreador mudou durante a configuração: comece de novo.")
	}
	step := steps[r.next]
	msg, sendErr := s.sender.Send(ctx, r.phone, step.real)
	if sendErr != nil {
		if smsdev.IsDefinitive(sendErr) || r.attempts+1 >= maxAttempts {
			s.log.Warn("SMS de configuração recusado", "session", r.id, "step", step.Kind, "err", sendErr)
			if _, err := tx.Exec(ctx, `
				INSERT INTO sms_messages (session_id, device_id, direction, step, phone, body, status, error_message)
				VALUES ($1, $2, 'OUT', $3, $4, $5, 'failed', $6)`,
				r.id, r.deviceID, r.next, r.phone, step.Text, sendErr.Error()); err != nil {
				return err
			}
			return s.finish(ctx, tx, r.id, StatusFailed, "O SMSDev não enviou o SMS ("+step.Label+"): "+providerMessage(sendErr))
		}
		s.log.Warn("SMSDev sem resposta; tenta de novo", "session", r.id, "step", step.Kind, "err", sendErr)
		_, err := tx.Exec(ctx, `UPDATE sms_setup_sessions SET attempts = attempts + 1, updated_at = NOW() WHERE id = $1`, r.id)
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO sms_messages (session_id, device_id, direction, step, phone, body, provider_sid, status, error_code, error_message)
		VALUES ($1, $2, 'OUT', $3, $4, $5, NULLIF($6, ''), $7, $8, $9)`,
		r.id, r.deviceID, r.next, r.phone, step.Text, msg.ID, orDefault(msg.Status, smsdev.StatusQueued), msg.ErrorCode, msg.ErrorMessage); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		UPDATE sms_setup_sessions SET next_step = next_step + 1, attempts = 0, last_sent_at = $2, updated_at = NOW()
		WHERE id = $1`, r.id, now)
	return err
}

// checkConnected: o rastreador mandou algo para o servidor depois do último
// SMS — a configuração pegou. Sem isso no prazo, desiste.
func (s *Service) checkConnected(ctx context.Context, tx pgx.Tx, r *sessionRow) (bool, error) {
	var seen *time.Time
	if err := tx.QueryRow(ctx, `SELECT last_seen_at FROM devices WHERE id = $1`, r.deviceID).Scan(&seen); err != nil {
		return false, err
	}
	if seen != nil && r.lastSentAt != nil && seen.After(*r.lastSentAt) {
		_, err := tx.Exec(ctx, `
			UPDATE sms_setup_sessions SET status = 'DONE', connected_at = $2, finished_at = NOW(), updated_at = NOW()
			WHERE id = $1`, r.id, *seen)
		s.log.Info("rastreador conectou depois da configuração por SMS", "session", r.id, "device", r.deviceID)
		return err == nil, err
	}
	// Um comando essencial que não chegou encerra a espera.
	var failedLabel, code, message string
	err := tx.QueryRow(ctx, `
		SELECT step::text, error_code, error_message FROM sms_messages
		WHERE session_id = $1 AND direction = 'OUT' AND status IN ('failed', 'undelivered', 'canceled')
		ORDER BY created_at LIMIT 1`, r.id).Scan(&failedLabel, &code, &message)
	if err == nil {
		label := failedLabel
		var idx int
		if _, scanErr := fmt.Sscan(failedLabel, &idx); scanErr == nil && idx < len(r.steps) {
			label = r.steps[idx].Label
			if r.steps[idx].Kind == StepParam {
				label = ""
			}
		}
		if label != "" {
			return false, s.finish(ctx, tx, r.id, StatusFailed, failureText(label, code, message))
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return false, err
	}
	if r.waitingSince != nil && s.now().Sub(*r.waitingSince) >= connectTimeout {
		return false, s.finish(ctx, tx, r.id, StatusTimeout,
			"O rastreador não conectou em 20 minutos. Confira o chip (sinal, crédito e SMS liberado), "+
				"mande de novo com \"Destravar o canal de comandos\" ou peça a configuração de volta (PARAM#).")
	}
	return false, nil
}

func (s *Service) finish(ctx context.Context, tx pgx.Tx, id uuid.UUID, status, message string) error {
	_, err := tx.Exec(ctx, `
		UPDATE sms_setup_sessions SET status = $2, error = $3, finished_at = NOW(), updated_at = NOW() WHERE id = $1`,
		id, status, message)
	return err
}

func failureText(label, code, message string) string {
	text := "O SMS \"" + label + "\" não chegou ao chip"
	if message != "" {
		text += ": " + message
	} else if code != "" {
		text += " (código " + code + " do SMSDev)"
	}
	return text + ". Confira se o chip está ativo e aceita SMS."
}

// optionsOf reconstrói as opções a partir dos passos gravados.
func optionsOf(steps []Step) Options {
	var o Options
	for _, st := range steps {
		o.Unlock = o.Unlock || st.Kind == StepUnlock
		o.Query = o.Query || st.Kind == StepParam
	}
	return o
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func providerMessage(err error) string {
	var apiErr *smsdev.APIError
	if errors.As(err, &apiErr) {
		return apiErr.Message
	}
	return err.Error()
}

func mapErr(err error) error {
	var v ValidationError
	if errors.As(err, &v) {
		return v
	}
	return database.MapError(err)
}

// ---------------------------------------------------------------------------
// Status dos SMS e respostas do rastreador (consultados no SMSDev)
// ---------------------------------------------------------------------------

// rank: a ordem dos status de um SMS enviado (um aviso atrasado não volta atrás).
func rank(status string) int {
	switch status {
	case "accepted", "scheduled", "queued":
		return 1
	case "sending":
		return 2
	case "sent":
		return 3
	case "delivered", "undelivered", "failed", "canceled":
		return 4
	}
	return 0
}

// ErrNoSMS: sem o SMSDev configurado, ou o chip sem número.
var ErrNoSMS = errors.New("SMS indisponível para este rastreador")

// SendText manda um comando avulso por SMS ao chip do rastreador (o modo
// roubo, com o rastreador fora do ar) e guarda no histórico de SMS.
func (s *Service) SendText(ctx context.Context, dev *devices.Device, text string) error {
	phone := E164(dev.PhoneNumber)
	if !s.Enabled() || phone == "" {
		return ErrNoSMS
	}
	msg, sendErr := s.sender.Send(ctx, phone, text)
	status, sid, code, message := smsdev.StatusFailed, "", "", ""
	if sendErr != nil {
		message = providerMessage(sendErr)
	} else {
		status, sid, code, message = orDefault(msg.Status, smsdev.StatusQueued), msg.ID, msg.ErrorCode, msg.ErrorMessage
	}
	if _, err := s.db.Exec(ctx, `
		INSERT INTO sms_messages (device_id, direction, phone, body, provider_sid, status, error_code, error_message)
		VALUES ($1, 'OUT', $2, $3, NULLIF($4, ''), $5, $6, $7)`,
		dev.ID, phone, devices.RedactText(text, dev.Secrets()), sid, status, code, message); err != nil {
		s.log.Error("falha ao guardar o SMS avulso", "device", dev.ID, "err", err)
	}
	return sendErr
}

// UpdateStatus grava o status de um SMS enviado (da consulta ao SMSDev).
func (s *Service) UpdateStatus(ctx context.Context, sid, status, errorCode, errorMessage string) error {
	status = strings.ToLower(strings.TrimSpace(status))
	if sid == "" || rank(status) == 0 {
		return nil
	}
	var current string
	err := s.db.QueryRow(ctx, `SELECT status FROM sms_messages WHERE provider_sid = $1 AND direction = 'OUT'`, sid).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return database.MapError(err)
	}
	if rank(status) < rank(current) || (rank(current) == 4 && status != current) {
		return nil
	}
	_, err = s.db.Exec(ctx, `
		UPDATE sms_messages SET status = $2, error_code = $3, error_message = COALESCE(NULLIF($4, ''), error_message),
			checked_at = NOW(), updated_at = NOW()
		WHERE provider_sid = $1`, sid, status, errorCode, errorMessage)
	return database.MapError(err)
}

// refreshStatuses consulta no SMSDev a entrega dos SMS recentes sem status
// final.
func (s *Service) refreshStatuses(ctx context.Context) {
	now := s.now()
	rows, err := s.db.Query(ctx, `
		SELECT provider_sid FROM sms_messages
		WHERE direction = 'OUT' AND provider_sid IS NOT NULL
			AND status NOT IN ('delivered', 'undelivered', 'failed', 'canceled')
			AND created_at > $1 AND created_at < $2 AND (checked_at IS NULL OR checked_at < $2)
		ORDER BY created_at LIMIT 20`, now.Add(-time.Hour), now.Add(-fetchAfter))
	if err != nil {
		s.log.Warn("falha ao listar SMS sem status", "err", err)
		return
	}
	var sids []string
	for rows.Next() {
		var sid string
		if rows.Scan(&sid) == nil {
			sids = append(sids, sid)
		}
	}
	rows.Close()
	for _, sid := range sids {
		msg, err := s.sender.Fetch(ctx, sid)
		if err != nil {
			_, _ = s.db.Exec(ctx, `UPDATE sms_messages SET checked_at = NOW() WHERE provider_sid = $1`, sid)
			continue
		}
		if err := s.UpdateStatus(ctx, sid, msg.Status, msg.ErrorCode, msg.ErrorMessage); err != nil {
			s.log.Warn("falha ao gravar o status do SMS", "sid", sid, "err", err)
		}
		_, _ = s.db.Exec(ctx, `UPDATE sms_messages SET checked_at = NOW() WHERE provider_sid = $1`, sid)
	}
}

// Inbound grava um SMS recebido: a resposta do rastreador (pelo número do
// chip), redigida com as senhas dele. De um número desconhecido, nada.
func (s *Service) Inbound(ctx context.Context, from, body, sid string) (bool, error) {
	digits := E164(from)
	if digits == "" {
		return false, nil
	}
	tail := digits[len(digits)-min(11, len(digits)-3):]
	var deviceID uuid.UUID
	err := s.db.QueryRow(ctx, `
		SELECT id FROM devices
		WHERE length(regexp_replace(phone_number, '\D', '', 'g')) >= 10
			AND right(regexp_replace(phone_number, '\D', '', 'g'), $2) = $1
		ORDER BY updated_at DESC LIMIT 1`, tail, len(tail)).Scan(&deviceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, database.MapError(err)
	}
	dev, err := s.devices.Get(ctx, deviceID)
	if err != nil {
		return false, err
	}
	secrets := dev.Secrets()
	if s.defaults.APNPassword != "" {
		secrets = append(secrets, s.defaults.APNPassword)
	}
	text := devices.RedactText(body, secrets)
	if r := []rune(text); len(r) > 1000 {
		text = string(r[:1000])
	}
	var session *uuid.UUID
	err = s.db.QueryRow(ctx, `
		SELECT id FROM sms_setup_sessions WHERE device_id = $1 AND created_at > $2
		ORDER BY created_at DESC LIMIT 1`, deviceID, s.now().Add(-6*time.Hour)).Scan(&session)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, database.MapError(err)
	}
	tag, err := s.db.Exec(ctx, `
		INSERT INTO sms_messages (session_id, device_id, direction, phone, body, provider_sid, status)
		VALUES ($1, $2, 'IN', $3, $4, NULLIF($5, ''), 'received')
		ON CONFLICT (provider_sid) DO NOTHING`, session, deviceID, digits, text, sid)
	if err != nil {
		return false, database.MapError(err)
	}
	if tag.RowsAffected() > 0 {
		s.log.Info("resposta do rastreador por SMS", "device", deviceID, "session", session)
	}
	return true, nil
}

// pollInbox lê no SMSDev as respostas recebidas desde ontem e grava as que
// ainda não estão aqui (o id delas leva "mo-", para não cruzar com o dos
// enviados). Só lê com configuração em andamento ou SMS enviado nas últimas
// inboxWindow: sem nada no ar, ninguém vai responder.
func (s *Service) pollInbox(ctx context.Context) {
	var active bool
	if err := s.db.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM sms_setup_sessions WHERE status IN ('SENDING', 'WAITING'))
			OR EXISTS (SELECT 1 FROM sms_messages WHERE direction = 'OUT' AND created_at > $1)`,
		s.now().Add(-inboxWindow)).Scan(&active); err != nil || !active {
		return
	}
	replies, err := s.sender.Inbox(ctx, s.now().Add(-24*time.Hour))
	if err != nil {
		s.log.Warn("falha ao ler as respostas no SMSDev", "err", err)
		return
	}
	if len(replies) == 0 {
		return
	}
	ids := make([]string, len(replies))
	for i, r := range replies {
		ids[i] = "mo-" + r.ID
	}
	known := map[string]bool{}
	rows, err := s.db.Query(ctx, `SELECT provider_sid FROM sms_messages WHERE provider_sid = ANY($1)`, ids)
	if err != nil {
		s.log.Warn("falha ao conferir as respostas já gravadas", "err", err)
		return
	}
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			known[id] = true
		}
	}
	rows.Close()
	for i, r := range replies {
		if known[ids[i]] {
			continue
		}
		if _, err := s.Inbound(ctx, r.From, strings.TrimSpace(r.Body), ids[i]); err != nil {
			s.log.Error("falha ao gravar o SMS recebido", "err", err)
		}
	}
}

// ---------------------------------------------------------------------------
// Leitura
// ---------------------------------------------------------------------------

// StepView é um passo com o andamento.
type StepView struct {
	Step
	// Status: pending, queued, sending, sent, delivered, undelivered, failed.
	Status string     `json:"status"`
	Error  string     `json:"error"`
	SentAt *time.Time `json:"sentAt"`
}

// Reply é uma resposta do rastreador.
type Reply struct {
	Body string    `json:"body"`
	At   time.Time `json:"at"`
}

// Session é a configuração como a tela mostra.
type Session struct {
	ID            uuid.UUID  `json:"id"`
	DeviceID      uuid.UUID  `json:"deviceId"`
	FulfillmentID *uuid.UUID `json:"fulfillmentId"`
	Phone         string     `json:"phone"`
	Status        string     `json:"status"`
	Error         string     `json:"error"`
	Note          string     `json:"note"`
	Steps         []StepView `json:"steps"`
	Replies       []Reply    `json:"replies"`
	WaitingSince  *time.Time `json:"waitingSince"`
	ConnectedAt   *time.Time `json:"connectedAt"`
	FinishedAt    *time.Time `json:"finishedAt"`
	CreatedAt     time.Time  `json:"createdAt"`
}

// Session lê uma configuração com os SMS e as respostas.
func (s *Service) Session(ctx context.Context, id uuid.UUID) (*Session, error) {
	var out Session
	var raw []byte
	err := s.db.QueryRow(ctx, `
		SELECT id, device_id, fulfillment_id, phone, status, error, note, steps, waiting_since, connected_at, finished_at, created_at
		FROM sms_setup_sessions WHERE id = $1`, id).
		Scan(&out.ID, &out.DeviceID, &out.FulfillmentID, &out.Phone, &out.Status, &out.Error, &out.Note, &raw,
			&out.WaitingSince, &out.ConnectedAt, &out.FinishedAt, &out.CreatedAt)
	if err != nil {
		return nil, database.MapError(err)
	}
	var steps []Step
	if err := json.Unmarshal(raw, &steps); err != nil {
		return nil, err
	}
	out.Steps = make([]StepView, len(steps))
	for i, st := range steps {
		out.Steps[i] = StepView{Step: st, Status: "pending"}
	}
	out.Replies = []Reply{}
	rows, err := s.db.Query(ctx, `
		SELECT direction, COALESCE(step, -1), body, status, error_code, error_message, created_at
		FROM sms_messages WHERE session_id = $1 ORDER BY created_at`, id)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var direction, body, status, code, message string
		var step int
		var at time.Time
		if err := rows.Scan(&direction, &step, &body, &status, &code, &message, &at); err != nil {
			return nil, err
		}
		if direction == "IN" {
			out.Replies = append(out.Replies, Reply{Body: body, At: at})
			continue
		}
		if step >= 0 && step < len(out.Steps) {
			sent := at
			out.Steps[step].Status, out.Steps[step].SentAt = status, &sent
			if message == "" && code != "" {
				message = "código " + code + " do SMSDev"
			}
			out.Steps[step].Error = message
		}
	}
	return &out, rows.Err()
}

// Latest é a última configuração do rastreador (nil se nunca houve).
func (s *Service) Latest(ctx context.Context, deviceID uuid.UUID) (*Session, error) {
	var id uuid.UUID
	err := s.db.QueryRow(ctx, `SELECT id FROM sms_setup_sessions WHERE device_id = $1 ORDER BY created_at DESC LIMIT 1`, deviceID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, database.MapError(err)
	}
	return s.Session(ctx, id)
}
