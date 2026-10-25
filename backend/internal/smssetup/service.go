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

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/commands"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/devices"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/protocols"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/smsmarket"
)

// Sender é a SMS Market (ou o falso dos testes): manda o SMS, consulta a entrega
// e lê as respostas recebidas.
type Sender interface {
	Send(ctx context.Context, to, body string) (*smsmarket.Message, error)
	Fetch(ctx context.Context, id string) (*smsmarket.Message, error)
	Inbox(ctx context.Context, since time.Time) ([]smsmarket.Reply, error)
	Balance(ctx context.Context) (int, error)
}

// Activator marca o pedido como "Configurado" (com o aparelho vinculado ao
// veículo) quando o rastreador conecta.
type Activator interface {
	AutoConfigure(ctx context.Context, fulfillmentID, deviceID uuid.UUID, note string) error
}

// Commander manda um comando pela conexão do rastreador e lê como ele ficou
// (commands.Service).
type Commander interface {
	Send(ctx context.Context, req commands.Request) (*commands.Command, error)
	Get(ctx context.Context, id uuid.UUID) (*commands.Command, error)
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
	// maxAttempts: falhas seguidas da SMS Market (rede) antes de desistir.
	maxAttempts = 3
	// fetchAfter: depois de quanto tempo consulta a entrega na SMS Market.
	fetchAfter = 20 * time.Second
	// inboxWindow: quanto tempo depois do último SMS enviado ainda lê as
	// respostas (a do modo roubo também).
	inboxWindow = 6 * time.Hour
	// ActivationSMS: quantos SMS uma ativação padrão manda (o APN e o
	// servidor; o destravamento soma 1). O resto vai pela conexão.
	ActivationSMS = 2
	// Os passos pela conexão: com o rastreador fora do ar, tenta de novo
	// offlineTries vezes, a cada retryWait; sem resposta em ackWait, manda
	// mais uma vez; depois disso, vai por SMS. Um passo que ficou para trás
	// (o servidor reiniciou no meio) é retomado até onlineWindow depois da
	// conexão.
	offlineTries = 6
	retryWait    = 20 * time.Second
	ackWait      = 45 * time.Second
	onlineWindow = time.Hour
	// balanceTTL: por quanto tempo o saldo lido na SMS Market vale.
	balanceTTL = time.Minute
)

// ValidationError é um pedido recusado; a mensagem vai para a tela.
type ValidationError struct{ Message string }

func (e ValidationError) Error() string { return e.Message }

func invalid(format string, args ...any) error {
	return ValidationError{Message: fmt.Sprintf(format, args...)}
}

// ErrDisabled: a SMS Market não está configurada.
var ErrDisabled = ValidationError{Message: "O envio de SMS não está configurado (SMSMARKET_USER e SMSMARKET_PASSWORD)."}

type Service struct {
	// O último saldo lido na SMS Market (vale balanceTTL).
	balanceMu sync.Mutex
	balance   *int
	balanceAt time.Time

	// Os passos pela conexão: as configurações com eles rodando agora; inline
	// roda na chamada do Work, sem esperar entre as tentativas (testes).
	onlineMu  sync.Mutex
	inflight  map[uuid.UUID]bool
	inline    bool
	retryWait time.Duration
	pollEvery time.Duration

	db        *database.DB
	devices   *devices.Service
	sender    Sender
	activator Activator
	commander Commander
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
		inflight: map[uuid.UUID]bool{}, retryWait: retryWait, pollEvery: time.Second,
	}
}

// SetClock troca o relógio (testes).
func (s *Service) SetClock(now func() time.Time) { s.now = now }

// SetCommander liga os passos pela conexão. Sem ele, eles vão por SMS depois
// que o rastreador conecta.
func (s *Service) SetCommander(c Commander) { s.commander = c }

// RunOnlineInline faz os passos pela conexão na chamada do Work, sem esperar
// entre as tentativas (testes).
func (s *Service) RunOnlineInline() { s.inline, s.retryWait, s.pollEvery = true, 0, 0 }

// Enabled diz se dá para mandar SMS.
func (s *Service) Enabled() bool { return s != nil && s.sender != nil }

// From é quem envia (para a tela).
func (s *Service) From() string { return s.from }

// Usage é o contador da página de rastreadores: o saldo na SMS Market, quantas
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
			s.log.Warn("falha ao ler o saldo na SMS Market", "err", err)
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
// (uma recusa da SMS Market — número inválido, conta sem saldo — aparece já).
func (s *Service) Start(ctx context.Context, deviceID uuid.UUID, fulfillmentID *uuid.UUID, opts Options, by *uuid.UUID) (*Session, error) {
	id, err := s.create(ctx, deviceID, fulfillmentID, opts, by)
	if err != nil {
		return nil, err
	}
	// O primeiro SMS sai agora; os outros, pelo Work.
	callCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if err := s.advance(callCtx, id, false); err != nil {
		return nil, err
	}
	return s.Session(callCtx, id)
}

// Queue grava a configuração sem mandar nada: o primeiro SMS sai pelo Work,
// em até 10 s. É o caminho do lote — esperar a resposta da SMS Market de cada
// rastreador, um atrás do outro, passaria do tempo da requisição.
func (s *Service) Queue(ctx context.Context, deviceID uuid.UUID, opts Options, by *uuid.UUID) (*Session, error) {
	id, err := s.create(ctx, deviceID, nil, opts, by)
	if err != nil {
		return nil, err
	}
	return s.Session(ctx, id)
}

// create confere o rastreador (e o pedido) e grava a sessão com os passos,
// ainda sem nenhum SMS.
func (s *Service) create(ctx context.Context, deviceID uuid.UUID, fulfillmentID *uuid.UUID, opts Options, by *uuid.UUID) (uuid.UUID, error) {
	if !s.Enabled() {
		return uuid.Nil, ErrDisabled
	}
	dev, err := s.devices.Get(ctx, deviceID)
	if err != nil {
		return uuid.Nil, err
	}
	phone := E164(dev.PhoneNumber)
	if phone == "" {
		return uuid.Nil, invalid("Cadastre o número do chip (com DDD) no rastreador.")
	}
	steps, problems := buildSteps(dev, s.defaults, opts)
	if len(problems) > 0 {
		return uuid.Nil, invalid("%s", strings.Join(problems, " "))
	}
	if fulfillmentID != nil {
		var tracker string
		err := s.db.QueryRow(ctx, `SELECT tracker_status FROM fulfillments WHERE id = $1`, *fulfillmentID).Scan(&tracker)
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, invalid("Pedido não encontrado.")
		}
		if err != nil {
			return uuid.Nil, database.MapError(err)
		}
		if tracker != "CONFIGURING" {
			return uuid.Nil, invalid("O pedido precisa estar em \"Rastreador em configuração\".")
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
		return uuid.Nil, invalid("Este rastreador já está sendo configurado por SMS.")
	}
	if err != nil {
		return uuid.Nil, database.MapError(err)
	}
	s.log.Info("configuração por SMS iniciada", "session", id, "device", deviceID, "steps", len(steps))
	return id, nil
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
// conectou, o tempo esgotado. Antes, consulta na SMS Market a entrega dos SMS e
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
	// Os passos pela conexão que ficaram para trás (o servidor reiniciou no
	// meio deles).
	rows, err = s.db.Query(ctx, `
		SELECT id FROM sms_setup_sessions
		WHERE status = 'DONE' AND connected_at > $1 AND jsonb_array_length(online) <
			(SELECT count(*) FROM jsonb_array_elements(steps) st WHERE st->>'via' = 'connection')
		ORDER BY created_at LIMIT 50`, s.now().Add(-onlineWindow))
	if err != nil {
		s.log.Error("falha ao listar os passos pela conexão", "err", err)
		return
	}
	ids = ids[:0]
	for rows.Next() {
		var id uuid.UUID
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		s.startOnline(ctx, id)
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
	if connected != nil {
		s.startOnline(ctx, id)
	}
	return nil
}

// sendNext manda o próximo comando, se o anterior já teve resposta, entrega
// ou tempo; com todos os SMS enviados, passa a esperar a conexão (os passos
// pela conexão vêm depois dela).
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
		if smsmarket.Failed(status) {
			return s.finish(ctx, tx, r.id, StatusFailed, failureText(r.steps[r.next-1].Label, code, message))
		}
		var replied bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM sms_messages WHERE session_id = $1 AND direction = 'IN' AND created_at >= $2)`,
			r.id, sentAt).Scan(&replied); err != nil {
			return err
		}
		ready := replied || (status == smsmarket.StatusDelivered && now.Sub(updatedAt) >= deliveredGap) || now.Sub(sentAt) >= silentGap
		if !ready {
			return nil
		}
	}
	if r.next >= len(r.steps) || r.steps[r.next].Via == ViaConnection {
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
		if smsmarket.IsDefinitive(sendErr) || r.attempts+1 >= maxAttempts {
			s.log.Warn("SMS de configuração recusado", "session", r.id, "step", step.Kind, "err", sendErr)
			if _, err := tx.Exec(ctx, `
				INSERT INTO sms_messages (session_id, device_id, direction, step, phone, body, status, error_message)
				VALUES ($1, $2, 'OUT', $3, $4, $5, 'failed', $6)`,
				r.id, r.deviceID, r.next, r.phone, step.Text, sendErr.Error()); err != nil {
				return err
			}
			return s.finish(ctx, tx, r.id, StatusFailed, "A SMS Market não enviou o SMS ("+step.Label+"): "+providerMessage(sendErr))
		}
		s.log.Warn("SMS Market sem resposta; tenta de novo", "session", r.id, "step", step.Kind, "err", sendErr)
		_, err := tx.Exec(ctx, `UPDATE sms_setup_sessions SET attempts = attempts + 1, updated_at = NOW() WHERE id = $1`, r.id)
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO sms_messages (session_id, device_id, direction, step, phone, body, provider_sid, status, error_code, error_message)
		VALUES ($1, $2, 'OUT', $3, $4, $5, NULLIF($6, ''), $7, $8, $9)`,
		r.id, r.deviceID, r.next, r.phone, step.Text, msg.ID, orDefault(msg.Status, smsmarket.StatusQueued), msg.ErrorCode, msg.ErrorMessage); err != nil {
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
		text += " (código " + code + " da SMS Market)"
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
	var apiErr *smsmarket.APIError
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
// Passos pela conexão (depois que o rastreador conecta)
// ---------------------------------------------------------------------------

// Como ficou um passo pela conexão.
const (
	// OnlineConfirmed: o rastreador respondeu pela conexão.
	OnlineConfirmed = "confirmed"
	// OnlineRejected: respondeu, mas com erro (por SMS seria igual).
	OnlineRejected = "rejected"
	// OnlineSMS: sem resposta pela conexão, foi por SMS.
	OnlineSMS = "sms"
	// OnlineFailed: nem foi (o cadastro mudou no meio).
	OnlineFailed = "failed"
)

// onlineResult é o desfecho de um passo pela conexão (a coluna online).
type onlineResult struct {
	Step   int       `json:"step"`
	Status string    `json:"status"`
	Reply  string    `json:"reply,omitempty"`
	Error  string    `json:"error,omitempty"`
	At     time.Time `json:"at"`
}

// startOnline manda os passos pela conexão fora do Work (cada um espera a
// resposta do rastreador). Uma configuração com eles rodando não começa de
// novo.
func (s *Service) startOnline(ctx context.Context, id uuid.UUID) {
	s.onlineMu.Lock()
	if s.inflight[id] {
		s.onlineMu.Unlock()
		return
	}
	s.inflight[id] = true
	s.onlineMu.Unlock()
	run := func() {
		defer func() {
			s.onlineMu.Lock()
			delete(s.inflight, id)
			s.onlineMu.Unlock()
		}()
		ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
		defer cancel()
		if err := s.configureOnline(ctx, id); err != nil {
			s.log.Error("falha nos passos pela conexão", "session", id, "err", err)
		}
	}
	if s.inline {
		run()
		return
	}
	go run()
}

// configureOnline manda pela conexão, na ordem, os passos que ainda não foram,
// e grava o desfecho de cada um assim que sai.
func (s *Service) configureOnline(ctx context.Context, id uuid.UUID) error {
	var deviceID uuid.UUID
	var phone, status string
	var rawSteps, rawOnline []byte
	if err := s.db.QueryRow(ctx, `SELECT device_id, phone, status, steps, online FROM sms_setup_sessions WHERE id = $1`, id).
		Scan(&deviceID, &phone, &status, &rawSteps, &rawOnline); err != nil {
		return err
	}
	if status != StatusDone {
		return nil
	}
	var steps []Step
	var results []onlineResult
	if err := json.Unmarshal(rawSteps, &steps); err != nil {
		return err
	}
	if err := json.Unmarshal(rawOnline, &results); err != nil {
		return err
	}
	finished := map[int]bool{}
	for _, r := range results {
		finished[r.Step] = true
	}
	// O texto de verdade sai do cadastro agora (as senhas não ficam gravadas).
	dev, err := s.devices.Get(ctx, deviceID)
	if err != nil {
		return err
	}
	built, problems := buildSteps(dev, s.defaults, optionsOf(steps))
	same := len(problems) == 0 && len(built) == len(steps)
	for i := 0; same && i < len(steps); i++ {
		same = built[i].Kind == steps[i].Kind
	}
	for i, st := range steps {
		if st.Via != ViaConnection || finished[i] {
			continue
		}
		res := onlineResult{Step: i, Status: OnlineFailed, Error: "O cadastro do rastreador mudou durante a configuração: mande este comando de novo.", At: s.now()}
		if same {
			res = s.runOnline(ctx, id, dev, phone, i, built[i])
		}
		if ctx.Err() != nil {
			return ctx.Err() // sem desfecho: o Work retoma
		}
		raw, _ := json.Marshal(res)
		if _, err := s.db.Exec(ctx, `
			UPDATE sms_setup_sessions SET online = online || jsonb_build_array($2::jsonb), updated_at = NOW() WHERE id = $1`,
			id, string(raw)); err != nil {
			return err
		}
	}
	return nil
}

// runOnline manda um passo pela conexão e espera a resposta. Fora do ar, tenta
// de novo daqui a pouco; sem resposta, manda mais uma vez; sem resposta de
// novo (ou fora do ar por muito tempo), vai por SMS.
func (s *Service) runOnline(ctx context.Context, session uuid.UUID, dev *devices.Device, phone string, idx int, step built) onlineResult {
	why := "a conexão não está ligada no servidor"
	if s.commander != nil {
		offline, silent := 0, 0
	attempts:
		for ctx.Err() == nil {
			cmd, err := s.commander.Send(ctx, commands.Request{Device: dev, Type: protocols.CommandCustom, RawOverride: step.real})
			if err != nil {
				why = err.Error()
				break
			}
			cmd = s.await(ctx, cmd)
			switch {
			case cmd.Status == commands.StatusAcknowledged:
				return onlineResult{Step: idx, Status: OnlineConfirmed, Reply: cmd.Response, At: s.now()}
			case cmd.Status == commands.StatusFailed && cmd.SentAt == nil:
				// Nem chegou ao rastreador: está fora do ar (a voltagem,
				// por exemplo, faz o J16 reconectar).
				why = "o rastreador saiu do ar"
				if offline++; offline >= offlineTries || !sleep(ctx, s.retryWait) {
					break attempts
				}
			case cmd.Status == commands.StatusFailed || cmd.Status == commands.StatusRejected:
				return onlineResult{Step: idx, Status: OnlineRejected, Reply: cmd.Error, At: s.now()}
			default:
				why = "sem resposta pela conexão"
				if silent++; silent >= 2 {
					break attempts
				}
			}
		}
	}
	if ctx.Err() != nil {
		return onlineResult{}
	}
	s.log.Info("passo pela conexão vai por SMS", "session", session, "step", step.Kind, "motivo", why)
	res := onlineResult{Step: idx, Status: OnlineSMS, Error: why, At: s.now()}
	if err := s.text(ctx, &session, &idx, dev.ID, phone, step.real, step.Text); err != nil {
		s.log.Warn("SMS de reserva recusado", "session", session, "step", step.Kind, "err", err)
	}
	return res
}

// await espera o comando terminar: resposta, recusa ou tempo.
func (s *Service) await(ctx context.Context, cmd *commands.Command) *commands.Command {
	deadline := time.Now().Add(ackWait)
	for !commands.Terminal(cmd.Status) && time.Now().Before(deadline) && sleep(ctx, s.pollEvery) {
		if next, err := s.commander.Get(ctx, cmd.ID); err == nil {
			cmd = next
		}
	}
	return cmd
}

// sleep espera d (ou o contexto acabar: false).
func sleep(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// ---------------------------------------------------------------------------
// Status dos SMS e respostas do rastreador (consultados na SMS Market)
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

// ErrNoSMS: sem a SMS Market configurada, ou o chip sem número.
var ErrNoSMS = errors.New("SMS indisponível para este rastreador")

// SendText manda um comando avulso por SMS ao chip do rastreador (o modo
// roubo, com o rastreador fora do ar) e guarda no histórico de SMS.
func (s *Service) SendText(ctx context.Context, dev *devices.Device, text string) error {
	phone := E164(dev.PhoneNumber)
	if !s.Enabled() || phone == "" {
		return ErrNoSMS
	}
	return s.text(ctx, nil, nil, dev.ID, phone, text, devices.RedactText(text, dev.Secrets()))
}

// text manda um SMS fora da sequência da configuração (o avulso e a reserva
// de um passo pela conexão) e guarda no histórico, com o texto redigido.
func (s *Service) text(ctx context.Context, session *uuid.UUID, step *int, deviceID uuid.UUID, phone, real, shown string) error {
	msg, sendErr := s.sender.Send(ctx, phone, real)
	status, sid, code, message := smsmarket.StatusFailed, "", "", ""
	if sendErr != nil {
		message = providerMessage(sendErr)
	} else {
		status, sid, code, message = orDefault(msg.Status, smsmarket.StatusQueued), msg.ID, msg.ErrorCode, msg.ErrorMessage
	}
	if _, err := s.db.Exec(ctx, `
		INSERT INTO sms_messages (session_id, device_id, direction, step, phone, body, provider_sid, status, error_code, error_message)
		VALUES ($1, $2, 'OUT', $3, $4, $5, NULLIF($6, ''), $7, $8, $9)`,
		session, deviceID, step, phone, shown, sid, status, code, message); err != nil {
		s.log.Error("falha ao guardar o SMS", "device", deviceID, "err", err)
	}
	return sendErr
}

// UpdateStatus grava o status de um SMS enviado (da consulta à SMS Market).
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

// refreshStatuses consulta na SMS Market a entrega dos SMS recentes sem status
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

// pollInbox lê na SMS Market as respostas recebidas desde ontem e grava as que
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
		s.log.Warn("falha ao ler as respostas na SMS Market", "err", err)
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
	// Status: pending, queued, sending, sent, delivered, undelivered, failed;
	// pela conexão, confirmed ou rejected.
	Status string     `json:"status"`
	Error  string     `json:"error"`
	SentAt *time.Time `json:"sentAt"`
	// Reply: a resposta do rastreador pela conexão. Fallback: sem resposta
	// por ela, o passo foi por SMS (o status é o do SMS).
	Reply    string `json:"reply"`
	Fallback bool   `json:"fallback"`
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
	// Online: o rastreador conectou e os passos pela conexão ainda estão indo.
	Online       bool       `json:"online"`
	Replies      []Reply    `json:"replies"`
	WaitingSince *time.Time `json:"waitingSince"`
	ConnectedAt  *time.Time `json:"connectedAt"`
	FinishedAt   *time.Time `json:"finishedAt"`
	CreatedAt    time.Time  `json:"createdAt"`
	// WrongIMEI: um rastreador com o IMEI parecido com o do cadastro está
	// tentando conectar e sendo recusado — o IMEI do cadastro está errado.
	WrongIMEI *WrongIMEI `json:"wrongImei"`
}

// WrongIMEI é o rastreador conectando com um IMEI diferente do cadastrado.
type WrongIMEI struct {
	Registered string    `json:"registered"`
	Connecting string    `json:"connecting"`
	LastSeenAt time.Time `json:"lastSeenAt"`
	Attempts   int       `json:"attempts"`
}

// Session lê uma configuração com os SMS e as respostas.
func (s *Service) Session(ctx context.Context, id uuid.UUID) (*Session, error) {
	var out Session
	var raw, rawOnline []byte
	err := s.db.QueryRow(ctx, `
		SELECT id, device_id, fulfillment_id, phone, status, error, note, steps, online, waiting_since, connected_at, finished_at, created_at
		FROM sms_setup_sessions WHERE id = $1`, id).
		Scan(&out.ID, &out.DeviceID, &out.FulfillmentID, &out.Phone, &out.Status, &out.Error, &out.Note, &raw, &rawOnline,
			&out.WaitingSince, &out.ConnectedAt, &out.FinishedAt, &out.CreatedAt)
	if err != nil {
		return nil, database.MapError(err)
	}
	var steps []Step
	var results []onlineResult
	if err := json.Unmarshal(raw, &steps); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(rawOnline, &results); err != nil {
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
				message = "código " + code + " da SMS Market"
			}
			out.Steps[step].Error = message
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	finished := map[int]bool{}
	for _, r := range results {
		if r.Step < 0 || r.Step >= len(out.Steps) {
			continue
		}
		finished[r.Step] = true
		v := &out.Steps[r.Step]
		if r.Status == OnlineSMS {
			v.Fallback = true // o status e o erro são os do SMS
			continue
		}
		at := r.At
		v.Status, v.Error, v.Reply, v.SentAt = r.Status, r.Error, r.Reply, &at
	}
	if out.Status == StatusDone && out.ConnectedAt != nil && s.now().Sub(*out.ConnectedAt) < onlineWindow {
		for i, st := range steps {
			out.Online = out.Online || (st.Via == ViaConnection && !finished[i])
		}
	}
	// O SMS pegou, mas o rastreador conecta com o IMEI de verdade e o do
	// cadastro está errado: o servidor o recusa, e a tela mostra qual é.
	if out.Status != StatusDone && out.Status != StatusCanceled {
		if dev, err := s.devices.Get(ctx, out.DeviceID); err == nil {
			attempt, err := s.devices.UnknownLike(ctx, dev.IMEI, out.CreatedAt.Add(-24*time.Hour))
			if err != nil {
				s.log.Warn("falha ao conferir o rastreador recusado", "session", out.ID, "err", err)
			} else if attempt != nil {
				out.WrongIMEI = &WrongIMEI{Registered: dev.IMEI, Connecting: attempt.IMEI,
					LastSeenAt: attempt.LastSeenAt, Attempts: attempt.Attempts}
			}
		}
	}
	return &out, nil
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
