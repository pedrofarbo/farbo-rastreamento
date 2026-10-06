package alerts

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/events"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/mail"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/push"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/tracking"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/webpush"
)

// Mailer entrega o e-mail de alerta (mail.AlertMailer).
type Mailer interface {
	Alert(ctx context.Context, a mail.Alert) error
}

// Geocoder põe o endereço no e-mail (geocoding.Service). Opcional.
type Geocoder interface {
	Reverse(ctx context.Context, lat, lon float64) (string, error)
}

// Pusher entrega a notificação no celular do cliente (push.Service).
type Pusher interface {
	Notify(ctx context.Context, userID uuid.UUID, n push.Notification) (int, error)
}

// StateReader dá a última ignição conhecida de um rastreador
// (tracking.StateStore).
type StateReader interface {
	Get(deviceID uuid.UUID) *tracking.State
}

const (
	// Filas: os ganchos da ingestão nunca esperam; fila cheia descarta. A de
	// eventos comporta a frota inteira caindo de uma vez (no teste de carga,
	// 5000 desconexões juntas estouravam as 2048 vagas de antes).
	eventQueue     = 16384
	candidateQueue = 512
	sendQueue      = 256
	senders        = 3

	// movingSpeedKmh separa veículo andando de ruído de GPS parado.
	movingSpeedKmh = 5
	// towingSpeedKmh: acima disso, com a ignição desligada, um único ponto
	// fora do raio já basta (é deslocamento, não ruído).
	towingSpeedKmh = 10
	// towingStrikes: sem velocidade, quantos pontos seguidos fora do raio.
	towingStrikes = 2
	// movingWindow: esteve em movimento há menos que isso = "em movimento"
	// quando o sinal cai.
	movingWindow = 10 * time.Minute

	// signalLostGrace: com o veículo andando, a queda da conexão só vira
	// alerta se o rastreador não voltar nesse prazo. Troca de antena e
	// reinício do servidor derrubam a conexão por segundos; perda de sinal de
	// verdade (bloqueador) nem fecha o socket — o servidor só percebe pelo
	// keepalive, minutos depois —, então a espera quase não atrasa o aviso.
	signalLostGrace = time.Minute
	lostSweep       = 5 * time.Second
	// Queda em massa: pelo menos massMinimum desconexões, e massShare % dos
	// conectados, dentro de massWindow. É o servidor ou a rede (reinício do
	// Traefik, operadora fora), não os veículos: ninguém recebe "sinal
	// perdido" na hora; a equipe vê o aviso no log, e quem não voltar cai no
	// alerta de sem comunicação (OfflineParkedAfter).
	massWindow  = 30 * time.Second
	massMinimum = 20
	massShare   = 10

	sweepInterval   = time.Minute
	historyKeep     = 90 * 24 * time.Hour
	testMinInterval = time.Minute
	sendTimeout     = 30 * time.Second
	geocodeTimeout  = 5 * time.Second
	retryDelay      = 5 * time.Second
)

// ignitionOn é o candidato de ignição ligada: vira IGNITION_GUARD ou
// IGNITION conforme o horário e as escolhas de cada destinatário.
const ignitionOn = "IGNITION_ON"

// Variantes de alguns alertas.
const (
	variantCut    = "cut"
	variantResume = "resume"
	variantMoving = "moving"
	variantParked = "parked"
	variantAlarm  = "alarm"
	variantEnter  = "enter"
	variantExit   = "exit"
)

// ErrTestTooSoon: o e-mail de teste tem limite de um por minuto.
var ErrTestTooSoon = errors.New("aguarde um minuto para enviar outro e-mail de teste")

// candidate é um acontecimento que pode virar alerta.
type candidate struct {
	kind     string
	variant  string
	deviceID uuid.UUID
	eventID  *int64
	at       time.Time

	lat, lon *float64
	speed    *float64
	limit    *float64
	distance float64
	offline  time.Duration

	// Cerca do cliente: qual, o nome e de quem é.
	fenceID, fenceName, fenceOwner string
}

type recipient struct {
	email    string
	name     string
	userID   *uuid.UUID
	kind     string
	settings Settings
	central  bool
}

type delivery struct {
	id  int64
	msg mail.Alert
	// userID e notification: a mesma notícia no celular do cliente (a
	// central não tem app).
	userID       *uuid.UUID
	notification push.Notification
}

// lostSignal é a queda com o veículo andando, esperando signalLostGrace.
type lostSignal struct {
	c     candidate
	noted time.Time
	// mass: fez parte de uma queda em massa.
	mass bool
}

type parkedSpot struct {
	lat, lon float64
	strikes  int
	alerted  bool
}

// Engine avalia eventos e posições e envia os alertas.
type Engine struct {
	cfg      config.Alerts
	appURL   string
	store    Store
	mailer   Mailer
	states   StateReader
	geocoder Geocoder
	pusher   Pusher
	loc      *time.Location
	now      func() time.Time
	log      *slog.Logger

	events     chan *events.Event
	candidates chan candidate
	sends      chan delivery

	mu      sync.Mutex
	parked  map[uuid.UUID]*parkedSpot
	moving  map[uuid.UUID]time.Time
	offline map[uuid.UUID]time.Time
	lost    map[uuid.UUID]*lostSignal
	// drops são os instantes das desconexões recentes (massWindow); a queda
	// em massa em curso vai de massFrom a massLast.
	drops     []time.Time
	massFrom  time.Time
	massLast  time.Time
	massCount int
	// connected conta os rastreadores conectados (tcp.Manager.Count).
	connected func() int
	dropped   atomic.Int64
}

func NewEngine(cfg config.Alerts, appURL string, store Store, mailer Mailer, states StateReader,
	geocoder Geocoder, log *slog.Logger) *Engine {
	loc, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		loc = time.FixedZone("BRT", -3*60*60)
	}
	return &Engine{
		cfg: cfg, appURL: strings.TrimRight(appURL, "/"), store: store, mailer: mailer, states: states,
		geocoder: geocoder, loc: loc, now: time.Now, log: log.With("component", "alerts"),
		events:     make(chan *events.Event, eventQueue),
		candidates: make(chan candidate, candidateQueue),
		sends:      make(chan delivery, sendQueue),
		parked:     map[uuid.UUID]*parkedSpot{},
		moving:     map[uuid.UUID]time.Time{},
		offline:    map[uuid.UUID]time.Time{},
		lost:       map[uuid.UUID]*lostSignal{},
	}
}

// SetPusher liga a entrega no celular (app do cliente). Chame antes do Run.
func (e *Engine) SetPusher(p Pusher) { e.pusher = p }

// SetConnectedCount dá o total de rastreadores conectados, para medir a
// queda em massa contra o tamanho da frota. Chame antes do Run.
func (e *Engine) SetConnectedCount(fn func() int) { e.connected = fn }

// ---------------------------------------------------------------------------
// Ganchos da ingestão: só enfileiram, nunca esperam.
// ---------------------------------------------------------------------------

// OnEvent recebe cada evento gravado (events.Service.SetObserver).
func (e *Engine) OnEvent(ev *events.Event) {
	switch ev.Type {
	case events.SOS, events.PowerLoss, events.LowBattery, events.Overspeed, events.IgnitionOn,
		events.EngineCutAck, events.EngineResumeAck, events.DeviceAlarm,
		events.DeviceDisconnected, events.DeviceConnected,
		events.GeofenceEnter, events.GeofenceExit:
	default:
		return // o resto não vira alerta: nem entra na fila
	}
	select {
	case e.events <- ev:
	default:
		e.drop("evento")
	}
}

// OnPosition recebe cada posição gravada (tracking.Ingestor.SetPositionObserver).
// É aqui que o reboque é detectado: a conta é feita em memória e só um
// alerta vai para a fila.
func (e *Engine) OnPosition(deviceID uuid.UUID, pos *tracking.Position, acc *bool) {
	now := e.now()
	if now.Sub(pos.GPSTimestamp) > e.cfg.MaxEventAge {
		return // posição guardada enquanto estava sem sinal: não é o agora
	}
	if pos.GPSValid != nil && !*pos.GPSValid {
		return
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.offline, deviceID) // voltou a falar
	delete(e.lost, deviceID)
	if (acc != nil && *acc) || pos.SpeedKmh > movingSpeedKmh {
		e.moving[deviceID] = now
	}
	if acc == nil {
		return // sem saber da ignição não dá para falar em reboque
	}
	if *acc {
		delete(e.parked, deviceID)
		return
	}

	spot := e.parked[deviceID]
	if spot == nil {
		e.parked[deviceID] = &parkedSpot{lat: pos.Latitude, lon: pos.Longitude}
		return
	}
	if spot.alerted {
		return // um alerta por estacionamento
	}
	distance := haversineMeters(spot.lat, spot.lon, pos.Latitude, pos.Longitude)
	if distance < e.cfg.TowingDistanceMeters {
		spot.strikes = 0
		return
	}
	spot.strikes++
	if spot.strikes < towingStrikes && pos.SpeedKmh < towingSpeedKmh {
		return // um ponto fora do raio parado pode ser salto do GPS
	}
	spot.alerted = true
	lat, lon, speed := pos.Latitude, pos.Longitude, pos.SpeedKmh
	e.enqueue(candidate{kind: KindTowing, deviceID: deviceID, at: pos.GPSTimestamp,
		lat: &lat, lon: &lon, speed: &speed, distance: distance})
}

func (e *Engine) enqueue(c candidate) {
	select {
	case e.candidates <- c:
	default:
		e.drop("alerta")
	}
}

// drop registra fila cheia sem inundar o log.
func (e *Engine) drop(what string) {
	if n := e.dropped.Add(1); n == 1 || n%100 == 0 {
		e.log.Warn("fila de alertas cheia: "+what+" descartado", "descartados", n)
	}
}

// ---------------------------------------------------------------------------
// Execução
// ---------------------------------------------------------------------------

// Run processa as filas até o contexto acabar.
func (e *Engine) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for range senders {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.sender(ctx)
		}()
	}

	sweep := time.NewTicker(sweepInterval)
	defer sweep.Stop()
	lostTick := time.NewTicker(lostSweep)
	defer lostTick.Stop()
	lastPrune := time.Time{}
	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			return
		case ev := <-e.events:
			e.handleEvent(ctx, ev)
		case c := <-e.candidates:
			e.process(ctx, c)
		case <-lostTick.C:
			e.sweepLost(ctx)
		case <-sweep.C:
			e.sweepOffline(ctx)
			if e.now().Sub(lastPrune) > 24*time.Hour {
				lastPrune = e.now()
				if n, err := e.store.Prune(ctx, e.now().Add(-historyKeep)); err != nil {
					e.log.Warn("falha ao limpar o histórico de alertas", "err", err)
				} else if n > 0 {
					e.log.Info("histórico de alertas antigo apagado", "linhas", n)
				}
			}
		}
	}
}

func (e *Engine) handleEvent(ctx context.Context, ev *events.Event) {
	c := candidate{deviceID: ev.DeviceID, at: ev.Timestamp, lat: ev.Latitude, lon: ev.Longitude, speed: ev.SpeedKmh}
	if ev.ID != 0 {
		id := ev.ID
		c.eventID = &id
	}
	switch ev.Type {
	case events.SOS:
		c.kind = KindSOS
	case events.PowerLoss:
		c.kind = KindPowerCut
	case events.LowBattery:
		c.kind = KindLowBattery
	case events.Overspeed:
		c.kind = KindOverspeed
		if limit, ok := number(ev.Metadata["limitKmh"]); ok {
			c.limit = &limit
		}
	case events.IgnitionOn:
		c.kind = ignitionOn
	case events.EngineCutAck:
		c.kind, c.variant = KindEngineBlock, variantCut
	case events.EngineResumeAck:
		c.kind, c.variant = KindEngineBlock, variantResume
	case events.DeviceAlarm:
		// O alarme de deslocamento do GT06: o próprio aparelho percebeu que
		// saiu do lugar estacionado.
		if alarm, _ := ev.Metadata["alarm"].(string); alarm != "DISPLACEMENT" {
			return
		}
		c.kind, c.variant = KindTowing, variantAlarm
	case events.DeviceConnected:
		e.mu.Lock()
		delete(e.offline, ev.DeviceID)
		delete(e.lost, ev.DeviceID)
		e.mu.Unlock()
		return
	case events.DeviceDisconnected:
		// Só memória: a frota inteira caindo de uma vez não pode encher a
		// fila com consultas ao banco.
		moving := e.wasMoving(ev.DeviceID)
		e.mu.Lock()
		defer e.mu.Unlock()
		e.noteDrop()
		if !moving {
			// Parado: garagem sem sinal é comum. Só avisa se continuar
			// assim por ALERTS_OFFLINE_PARKED_AFTER (sweepOffline).
			e.offline[ev.DeviceID] = ev.Timestamp
			return
		}
		c.kind, c.variant = KindSignalLost, variantMoving
		e.lost[ev.DeviceID] = &lostSignal{c: c, noted: e.now(), mass: e.inMass()}
		return
	case events.GeofenceEnter, events.GeofenceExit:
		// Só as cercas do cliente avisam, e só do lado que ele escolheu; as
		// da central ficam nos eventos.
		c.fenceOwner = text(ev.Metadata["geofenceOwnerId"])
		if notify, _ := ev.Metadata["notify"].(bool); c.fenceOwner == "" || !notify {
			return
		}
		c.kind, c.variant = KindGeofence, variantEnter
		if ev.Type == events.GeofenceExit {
			c.variant = variantExit
		}
		c.fenceID, c.fenceName = text(ev.Metadata["geofenceId"]), text(ev.Metadata["geofenceName"])
	default:
		return
	}
	e.process(ctx, c)
}

// noteDrop conta a desconexão e reconhece a queda em massa. Com e.mu.
func (e *Engine) noteDrop() {
	now := e.now()
	cut := 0
	for cut < len(e.drops) && now.Sub(e.drops[cut]) > massWindow {
		cut++
	}
	e.drops = append(e.drops[cut:], now)
	if e.inMass() {
		e.massLast = now
		e.massCount++
		return
	}
	population := len(e.drops)
	if e.connected != nil {
		population += e.connected() // os que caíram já saíram da conta
	}
	if len(e.drops) < max(massMinimum, population*massShare/100) {
		return
	}
	e.massFrom, e.massLast, e.massCount = e.drops[0], now, len(e.drops)
	// As quedas desta janela que esperavam o prazo também são da massa.
	for _, l := range e.lost {
		if !l.noted.Before(e.massFrom) {
			l.mass = true
		}
	}
	e.log.Warn("queda em massa de conexões: os alertas de sinal perdido ficam segurados",
		"rastreadores", len(e.drops), "janela", massWindow.String(), "conectados", population-len(e.drops))
}

// inMass: há uma queda em massa em curso. Com e.mu.
func (e *Engine) inMass() bool {
	return !e.massLast.IsZero() && e.now().Sub(e.massLast) <= massWindow
}

// sweepLost avisa das quedas com o veículo andando que passaram do prazo
// sem o rastreador voltar. As da queda em massa vão para o alerta de sem
// comunicação, que só sai se ficarem fora pelo prazo longo.
func (e *Engine) sweepLost(ctx context.Context) {
	now := e.now()
	e.mu.Lock()
	due := []candidate{}
	for id, l := range e.lost {
		if now.Sub(l.noted) < signalLostGrace {
			continue
		}
		delete(e.lost, id)
		if l.mass {
			e.offline[id] = l.c.at
			continue
		}
		due = append(due, l.c)
	}
	if !e.massLast.IsZero() && !e.inMass() {
		e.log.Info("queda em massa de conexões terminou", "rastreadores", e.massCount,
			"duracao", e.massLast.Sub(e.massFrom).Round(time.Second).String())
		e.massFrom, e.massLast, e.massCount = time.Time{}, time.Time{}, 0
	}
	e.mu.Unlock()

	for _, c := range due {
		e.process(ctx, c)
	}
}

// wasMoving: ignição ligada ou deslocamento recente quando o sinal caiu.
func (e *Engine) wasMoving(deviceID uuid.UUID) bool {
	if e.states != nil {
		if st := e.states.Get(deviceID); st != nil && st.ACC != nil && *st.ACC {
			return true
		}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	last, ok := e.moving[deviceID]
	return ok && e.now().Sub(last) < movingWindow
}

// sweepOffline avisa dos rastreadores parados que continuam sem sinal.
func (e *Engine) sweepOffline(ctx context.Context) {
	now := e.now()
	e.mu.Lock()
	due := []uuid.UUID{}
	for id, since := range e.offline {
		if now.Sub(since) >= e.cfg.OfflineParkedAfter {
			due = append(due, id)
			delete(e.offline, id)
		}
	}
	e.mu.Unlock()

	for _, id := range due {
		offline, err := e.store.DeviceOffline(ctx, id)
		if err != nil {
			e.log.Warn("falha ao conferir rastreador sem sinal", "device", id, "err", err)
			continue
		}
		if offline {
			e.process(ctx, candidate{kind: KindSignalLost, variant: variantParked, deviceID: id, at: now,
				offline: e.cfg.OfflineParkedAfter})
		}
	}
}

// process decide quem recebe e aplica os filtros de cada destinatário.
func (e *Engine) process(ctx context.Context, c candidate) {
	if age := e.now().Sub(c.at); age > e.cfg.MaxEventAge {
		e.log.Debug("evento antigo não vira alerta", "kind", c.kind, "device", c.deviceID, "age", age.Round(time.Second))
		return
	}
	target, err := e.store.Target(ctx, c.deviceID)
	if err != nil {
		e.log.Error("falha ao buscar o veículo do alerta", "device", c.deviceID, "err", err)
		return
	}
	if target == nil {
		return // rastreador sem veículo (em estoque): ninguém para avisar
	}
	for _, r := range e.recipients(target, c) {
		e.deliver(ctx, target, c, r)
	}
}

// resolveKind aplica as escolhas do destinatário. Devolve "" quando ele não
// quer esse alerta.
func (e *Engine) resolveKind(c candidate, s Settings) string {
	if c.kind == ignitionOn {
		local := c.at.In(e.loc)
		if s.Enabled(KindIgnitionGuard) && s.InGuard(local.Hour()*60+local.Minute()) {
			return KindIgnitionGuard
		}
		if s.Enabled(KindIgnition) {
			return KindIgnition
		}
		return ""
	}
	if s.Enabled(c.kind) {
		return c.kind
	}
	return ""
}

func (e *Engine) recipients(t *Target, c candidate) []recipient {
	if c.kind == KindGeofence {
		// A cerca é do cliente: o aviso é só dele, e só enquanto o veículo
		// for dele.
		o := t.Owner
		if o == nil || !o.Active || o.Suspended || o.UserID.String() != c.fenceOwner {
			return nil
		}
		id := o.UserID
		return []recipient{{email: o.Email, name: o.Name, userID: &id, kind: KindGeofence, settings: o.Settings}}
	}
	out := []recipient{}
	ownerEmail := ""
	if o := t.Owner; o != nil {
		ownerEmail = strings.ToLower(o.Email)
		// Cliente inativo ou com o acesso suspenso por atraso não recebe.
		if o.Active && !o.Suspended {
			if kind := e.resolveKind(c, o.Settings); kind != "" {
				id := o.UserID
				out = append(out, recipient{email: o.Email, name: o.Name, userID: &id, kind: kind, settings: o.Settings})
			}
		}
	}
	if len(e.cfg.CentralEmails) == 0 {
		return out
	}
	// A central recebe os alertas dos veículos dela (sem dono) e os de
	// segurança de todos, com as escolhas padrão.
	defaults := DefaultSettings()
	kind := e.resolveKind(c, defaults)
	if kind == "" || (t.Owner != nil && !isSecurity(kind)) {
		return out
	}
	for _, email := range e.cfg.CentralEmails {
		if strings.ToLower(email) == ownerEmail {
			continue
		}
		out = append(out, recipient{email: email, kind: kind, settings: defaults, central: true})
	}
	return out
}

// notificationKind separa no histórico o que precisa de intervalo próprio:
// bloquear e depois liberar o motor são dois e-mails, não um repetido; e cada
// cerca tem o seu (chegar ao trabalho logo depois de deixar o filho na escola
// não pode ser segurado como repetição). O id da cerca vai depois de ":".
func notificationKind(c candidate, kind string) string {
	switch kind {
	case KindEngineBlock:
		if c.variant == variantResume {
			return "ENGINE_UNBLOCKED"
		}
		return "ENGINE_BLOCKED"
	case KindGeofence:
		if c.variant == variantExit {
			return "GEOFENCE_EXIT:" + c.fenceID
		}
		return "GEOFENCE_ENTER:" + c.fenceID
	}
	return kind
}

func (e *Engine) deliver(ctx context.Context, t *Target, c candidate, r recipient) {
	now := e.now()
	deviceID := c.deviceID
	n := &Notification{
		Recipient: r.email, UserID: r.userID, VehicleID: t.VehicleID, DeviceID: &deviceID,
		Kind: notificationKind(c, r.kind), EventID: c.eventID, OccurredAt: c.at,
	}

	last, sent, err := e.store.LastSent(ctx, r.email, deviceID, n.Kind)
	if err != nil {
		e.log.Error("falha ao consultar o histórico de alertas", "err", err)
		return
	}
	if sent && now.Sub(last) < e.cfg.Cooldown {
		e.suppress(ctx, n, ReasonCooldown)
		return
	}
	count, err := e.store.CountSentSince(ctx, r.email, now.Add(-time.Hour))
	if err != nil {
		e.log.Error("falha ao consultar o histórico de alertas", "err", err)
		return
	}
	if count >= e.cfg.MaxPerHour {
		e.suppress(ctx, n, ReasonHourlyCap)
		return
	}
	if n.SuppressedCount, err = e.store.CountSuppressedSinceLastSent(ctx, r.email, deviceID, n.Kind); err != nil {
		e.log.Error("falha ao consultar o histórico de alertas", "err", err)
		return
	}
	n.Status = StatusPending
	if err := e.store.Insert(ctx, n); err != nil {
		e.log.Error("falha ao registrar alerta", "err", err)
		return
	}

	msg := e.compose(t, c, r, n.SuppressedCount)
	d := delivery{id: n.ID, msg: msg}
	if r.userID != nil {
		d.userID = r.userID
		d.notification = e.notification(t, n.Kind, msg)
	}
	select {
	case e.sends <- d:
	case <-ctx.Done():
	}
}

func (e *Engine) suppress(ctx context.Context, n *Notification, reason string) {
	n.Status, n.Reason = StatusSuppressed, reason
	if err := e.store.Insert(ctx, n); err != nil {
		e.log.Error("falha ao registrar alerta segurado", "err", err)
	}
}

func (e *Engine) sender(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case d := <-e.sends:
			e.send(ctx, d)
		}
	}
}

func (e *Engine) send(ctx context.Context, d delivery) {
	// O celular primeiro: é o canal mais rápido, e não depende do SMTP.
	if e.pusher != nil && d.userID != nil {
		if count, err := e.pusher.Notify(ctx, *d.userID, d.notification); err != nil {
			e.log.Warn("alerta não chegou ao celular", "err", err)
		} else if count > 0 {
			markCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			if err := e.store.MarkPushed(markCtx, d.id, count); err != nil {
				e.log.Error("falha ao registrar alerta no celular", "err", err)
			}
			cancel()
		}
	}

	msg := d.msg
	if e.geocoder != nil && msg.Latitude != nil && msg.Longitude != nil {
		geoCtx, cancel := context.WithTimeout(ctx, geocodeTimeout)
		if address, err := e.geocoder.Reverse(geoCtx, *msg.Latitude, *msg.Longitude); err == nil {
			msg.Address = address
		}
		cancel()
	}

	err := e.sendOnce(ctx, msg)
	if err != nil && ctx.Err() == nil {
		select {
		case <-time.After(retryDelay):
			err = e.sendOnce(ctx, msg)
		case <-ctx.Done():
		}
	}
	// O registro do resultado não pode morrer junto com o contexto.
	markCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err != nil {
		e.log.Warn("alerta não enviado", "to", maskEmail(msg.To), "err", err)
		if markErr := e.store.MarkFailed(markCtx, d.id, err.Error()); markErr != nil {
			e.log.Error("falha ao registrar alerta não enviado", "err", markErr)
		}
		return
	}
	if markErr := e.store.MarkSent(markCtx, d.id, e.now()); markErr != nil {
		e.log.Error("falha ao registrar alerta enviado", "err", markErr)
	}
	e.log.Info("alerta enviado", "to", maskEmail(msg.To), "subject", msg.Subject)
}

func (e *Engine) sendOnce(ctx context.Context, msg mail.Alert) error {
	sendCtx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	return e.mailer.Alert(sendCtx, msg)
}

// SendTest envia um e-mail de teste para o próprio usuário, na hora, e diz
// se saiu. Limite de um por minuto.
func (e *Engine) SendTest(ctx context.Context, userID uuid.UUID, email, name string) error {
	last, ok, err := e.store.LastTest(ctx, userID)
	if err != nil {
		return err
	}
	if ok && e.now().Sub(last) < testMinInterval {
		return ErrTestTooSoon
	}
	now := e.now()
	n := &Notification{Recipient: email, UserID: &userID, Kind: KindTest, OccurredAt: now, Status: StatusPending}
	if err := e.store.Insert(ctx, n); err != nil {
		return err
	}
	title := "E-mail de teste dos alertas"
	msg := mail.Alert{
		To: email, Name: name, Subject: "Teste: " + title + " — Farbo Rastreadores",
		Severity: mail.SeverityInfo, Title: title,
		Summary: "Tudo certo: os alertas do seu painel chegam a este endereço. Quando algo acontecer com os seus veículos, é assim que você vai saber.",
		Vehicle: "—", When: e.formatWhen(now), ActionURL: e.appURL + "/alertas",
		SettingsURL: e.appURL + "/alertas", AppURL: e.appURL,
	}
	err = e.sendOnce(ctx, msg)
	markCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err != nil {
		_ = e.store.MarkFailed(markCtx, n.ID, err.Error())
		return err
	}
	return e.store.MarkSent(markCtx, n.ID, e.now())
}

// notification é o alerta como aparece no celular. O toque abre o veículo no
// app; a etiqueta faz um alerta novo do mesmo tipo e veículo substituir o
// anterior na bandeja, em vez de empilhar.
func (e *Engine) notification(t *Target, kind string, a mail.Alert) push.Notification {
	url, vehicle := "/app/", "central"
	if t.VehicleID != nil {
		vehicle = t.VehicleID.String()
		url = "/app/veiculos/" + vehicle + theftQuery(kind)
	}
	body := a.Summary
	if a.Suppressed > 0 {
		body += " (+" + plural(a.Suppressed, "repetição", "repetições") + " desde o último aviso)"
	}
	n := push.Notification{
		Title: a.Title, Body: body, URL: url, Tag: kind + ":" + vehicle, Severity: a.Severity,
		Urgency: webpush.UrgencyNormal,
		// Um alerta de mais de uma hora atrás já não ajuda: o serviço de push
		// descarta se o celular ficar desligado por mais que isso.
		TTL: time.Hour,
		// Tópico (até 32 caracteres base64url): um aviso ainda não entregue
		// é trocado pelo mais novo do mesmo tipo e veículo.
		Topic: topic(kind, vehicle),
	}
	if a.Severity == mail.SeverityCritical {
		n.Urgency = webpush.UrgencyHigh
	}
	return n
}

func topic(kind, vehicle string) string {
	kind, _, _ = strings.Cut(kind, ":") // a cerca não cabe nos 32 caracteres
	t := strings.ReplaceAll(kind, "_", "") + "-" + strings.ReplaceAll(vehicle, "-", "")
	if len(t) > 32 {
		t = t[:32]
	}
	return t
}

// ---------------------------------------------------------------------------
// Auxiliares
// ---------------------------------------------------------------------------

// text lê um valor do metadata como texto (o id da cerca chega como
// uuid.UUID dentro do processo e como string se vier do banco).
func text(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case fmt.Stringer:
		return t.String()
	}
	return ""
}

func number(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}

// haversineMeters é a distância em metros entre duas coordenadas.
func haversineMeters(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadius = 6371000.0
	rad := math.Pi / 180
	dLat := (lat2 - lat1) * rad
	dLon := (lon2 - lon1) * rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * earthRadius * math.Asin(math.Min(1, math.Sqrt(a)))
}

// maskEmail esconde o endereço no log: "an***@exemplo.com".
func maskEmail(email string) string {
	local, domain, ok := strings.Cut(email, "@")
	if !ok || len(local) < 2 {
		return "***"
	}
	return local[:2] + "***@" + domain
}
