package tracking

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/commands"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/devices"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/events"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/geofences"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/protocols"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/tcp"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/telemetry"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/vehicles"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/websocket"
)

// touchInterval evita um UPDATE em devices a cada pacote: o último contato só
// é regravado neste intervalo, exceto quando o dispositivo não está ONLINE.
const touchInterval = 15 * time.Second

// cacheTTL é a validade dos caches de dispositivo e veículo.
const cacheTTL = 60 * time.Second

// Publisher entrega atualizações ao WebSocket.
type Publisher interface {
	PublishFor(eventType string, vehicleID, deviceID *uuid.UUID, data any)
}

// Ingestor é a pipeline: telemetria normalizada entra, estado, histórico,
// eventos e tempo real saem. Implementa tcp.Ingestor.
type Ingestor struct {
	devices   *devices.Service
	vehicles  *vehicles.Service
	positions *Repository
	states    *StateStore
	events    *events.Service
	geofences *geofences.Service
	commands  *commands.Service
	raw       *RawPacketRepository
	publisher Publisher
	cfg       config.Tracking
	metrics   *telemetry.Metrics
	log       *slog.Logger

	// positionObserver recebe cada posição gravada, com a ignição conhecida
	// naquele momento (os alertas por e-mail). Precisa voltar na hora.
	positionObserver func(deviceID uuid.UUID, pos *Position, acc *bool)

	mu           sync.Mutex
	deviceCache  map[string]cachedDevice
	vehicleCache map[uuid.UUID]cachedVehicle
	lastTouch    map[uuid.UUID]time.Time
}

type cachedDevice struct {
	device *devices.Device
	at     time.Time
}

type cachedVehicle struct {
	vehicle *vehicles.Vehicle
	at      time.Time
}

func NewIngestor(
	deviceSvc *devices.Service,
	vehicleSvc *vehicles.Service,
	positions *Repository,
	states *StateStore,
	eventSvc *events.Service,
	geofenceSvc *geofences.Service,
	commandSvc *commands.Service,
	raw *RawPacketRepository,
	publisher Publisher,
	cfg config.Tracking,
	metrics *telemetry.Metrics,
	log *slog.Logger,
) *Ingestor {
	return &Ingestor{
		devices: deviceSvc, vehicles: vehicleSvc, positions: positions, states: states,
		events: eventSvc, geofences: geofenceSvc, commands: commandSvc, raw: raw,
		publisher: publisher, cfg: cfg, metrics: metrics,
		log:          log.With("component", "ingest"),
		deviceCache:  map[string]cachedDevice{},
		vehicleCache: map[uuid.UUID]cachedVehicle{},
		lastTouch:    map[uuid.UUID]time.Time{},
	}
}

// HandleMessage é chamado pela sessão TCP para cada mensagem decodificada.
func (i *Ingestor) HandleMessage(ctx context.Context, conn *tcp.DeviceConnection, msg protocols.TrackerMessage) error {
	if msg.IMEI == "" {
		return fmt.Errorf("mensagem sem IMEI (protocolo %s, tipo %s)", msg.Protocol, msg.Kind)
	}

	dev, err := i.resolveDevice(ctx, msg.IMEI)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			// IMEI não cadastrado. Encerramos a sessão, mas o IMEI aparece no
			// log (mascarado) para o operador saber o que cadastrar.
			i.metrics.PacketsInvalid.WithLabelValues("unknown_device").Inc()
			return fmt.Errorf("%w: IMEI %s não está cadastrado",
				tcp.ErrRejectSession, telemetry.IMEI(msg.IMEI))
		}
		return err
	}

	firstMessage := conn.DeviceID() == uuid.Nil
	conn.Identify(dev.IMEI, dev.ID)

	if firstMessage {
		i.onConnected(ctx, conn, dev, msg.Protocol)
	}
	i.touch(ctx, dev)

	switch msg.Kind {
	case protocols.KindLogin:
		return nil
	case protocols.KindCommandAck:
		vehicleID := i.vehicleIDFor(ctx, dev.ID)
		i.commands.HandleAck(ctx, dev, vehicleID, msg.CorrelationKey,
			msg.Response, !failedResponse(msg.Response))
		return nil
	}

	i.processTelemetry(ctx, dev, msg)
	return nil
}

// HandleInvalid guarda o tráfego que não foi possível interpretar (§38).
func (i *Ingestor) HandleInvalid(ctx context.Context, conn *tcp.DeviceConnection, payload []byte, protocolName, reason string) {
	packet := &RawPacket{
		RemoteAddr:   conn.RemoteAddr(),
		IMEI:         conn.IMEI(),
		Protocol:     protocolName,
		Reason:       reason,
		PayloadHex:   ToHex(payload),
		PayloadASCII: ToASCII(payload),
		ByteCount:    len(payload),
	}

	// Sem cancelamento: a captura precisa sobreviver ao fechamento da conexão.
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	if err := i.raw.Insert(saveCtx, packet); err != nil {
		i.log.Error("falha ao guardar pacote cru", "err", err, "reason", reason)
		return
	}
	i.log.Warn("pacote não interpretado foi capturado para análise",
		"reason", reason, "protocol", protocolName, "bytes", len(payload),
		"remote", conn.RemoteAddr(), "hex", truncateHex(packet.PayloadHex))
}

// HandleDisconnect registra a queda da sessão.
//
// Desconectar não é o mesmo que estar offline (§17): o status continua sendo
// decidido pelo último pacote recebido, na varredura periódica.
func (i *Ingestor) HandleDisconnect(ctx context.Context, conn *tcp.DeviceConnection) {
	deviceID := conn.DeviceID()
	if deviceID == uuid.Nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	i.events.Record(ctx, &events.Event{
		DeviceID:  deviceID,
		Type:      events.DeviceDisconnected,
		Timestamp: time.Now().UTC(),
		Metadata: map[string]any{
			"protocol":    conn.ProtocolName(),
			"connectedAt": conn.ConnectedAt,
			"remoteAddr":  conn.RemoteAddr(),
		},
	})
	i.log.Info("sessão encerrada", "imei", telemetry.IMEI(conn.IMEI()),
		"duration", time.Since(conn.ConnectedAt).Round(time.Second))
}

func (i *Ingestor) onConnected(ctx context.Context, conn *tcp.DeviceConnection, dev *devices.Device, protocolName string) {
	i.events.Record(ctx, &events.Event{
		DeviceID:  dev.ID,
		Type:      events.DeviceConnected,
		Timestamp: time.Now().UTC(),
		Metadata: map[string]any{
			"protocol":   protocolName,
			"remoteAddr": conn.RemoteAddr(),
		},
	})

	// O protocolo efetivamente falado pelo aparelho vale mais que o que foi
	// digitado no cadastro: é ele que decide como os comandos serão montados.
	if protocolName != "" && dev.Protocol != protocolName {
		if err := i.devices.SetProtocol(ctx, dev.ID, protocolName); err != nil {
			i.log.Warn("falha ao gravar o protocolo detectado", "err", err)
		} else {
			i.log.Info("protocolo do dispositivo atualizado",
				"imei", telemetry.IMEI(dev.IMEI), "from", dev.Protocol, "to", protocolName)
			dev.Protocol = protocolName
			i.cacheDevice(dev)
		}
	}
}

// processTelemetry aplica estado, histórico e eventos.
func (i *Ingestor) processTelemetry(ctx context.Context, dev *devices.Device, msg protocols.TrackerMessage) {
	before, after := i.states.Update(ctx, dev.ID, func(st *State) {
		if msg.ACC != nil {
			st.ACC = msg.ACC
		}
		if msg.RelayOn != nil {
			st.RelayOn = msg.RelayOn
		}
		if msg.BatteryPercent != nil {
			st.BatteryPercent = msg.BatteryPercent
		}
		if msg.BatteryVoltage != nil {
			st.BatteryVoltage = msg.BatteryVoltage
		}
		if msg.GSMLevel != nil {
			st.GSMLevel = msg.GSMLevel
		}
		if msg.HasLocation {
			valid := msg.GPSValid
			st.GPSValid = &valid
		}
		if msg.Kind == protocols.KindHeartbeat {
			now := time.Now().UTC()
			st.LastHeartbeatAt = &now
		}
	})

	vehicle := i.vehicleFor(ctx, dev.ID)
	var vehicleID *uuid.UUID
	if vehicle != nil {
		vehicleID = &vehicle.ID
	}

	i.emitStateEvents(ctx, dev, vehicleID, before, after, msg)

	if msg.Alarm != "" {
		i.recordAlarm(ctx, dev, msg)
	}

	if !msg.HasLocation {
		// Heartbeat e status não trazem coordenada. Publicamos o estado para
		// o painel atualizar bateria/ACC/sinal sem inventar uma posição (§34).
		i.publisher.PublishFor(websocket.TypeDeviceOnline, vehicleID, &dev.ID, map[string]any{
			"deviceId": dev.ID,
			"state":    after,
		})
		return
	}

	position := i.buildPosition(dev, msg)
	if err := i.positions.Insert(ctx, position); err != nil {
		i.log.Error("falha ao gravar posição", "device", dev.ID, "err", err)
		return
	}
	i.metrics.PositionsStored.WithLabelValues(msg.Protocol).Inc()

	i.evaluateOverspeed(ctx, dev, vehicle, vehicleID, position)
	i.evaluateGeofences(ctx, dev, vehicle, position)

	i.publisher.PublishFor(websocket.TypePositionUpdated, vehicleID, &dev.ID, position)
	if i.positionObserver != nil {
		i.positionObserver(dev.ID, position, after.ACC)
	}
}

// SetPositionObserver registra quem recebe cada posição gravada. Chame antes
// de a ingestão começar.
func (i *Ingestor) SetPositionObserver(fn func(deviceID uuid.UUID, pos *Position, acc *bool)) {
	i.positionObserver = fn
}

func (i *Ingestor) buildPosition(dev *devices.Device, msg protocols.TrackerMessage) *Position {
	timestamp := msg.Timestamp
	if timestamp.IsZero() {
		// Sem relógio confiável no pacote registramos o horário do servidor,
		// mas não fingimos que isso é hora de GPS.
		timestamp = time.Now().UTC()
		i.log.Warn("pacote sem data/hora; usando o horário do servidor",
			"imei", telemetry.IMEI(dev.IMEI), "protocol", msg.Protocol)
	}

	gpsValid := msg.GPSValid

	return &Position{
		DeviceID:     dev.ID,
		GPSTimestamp: timestamp.UTC(),
		Latitude:     msg.Latitude,
		Longitude:    msg.Longitude,
		SpeedKmh:     msg.SpeedKmh,
		Heading:      validHeading(msg.Heading),
		Altitude:     msg.Altitude,
		GPSValid:     &gpsValid,
		Satellites:   msg.Satellites,
		HDOP:         msg.HDOP,
		ACC:          msg.ACC,

		BatteryVoltage: msg.BatteryVoltage,
		BatteryPercent: msg.BatteryPercent,
		GSMLevel:       msg.GSMLevel,
		RelayOn:        msg.RelayOn,

		Protocol:   msg.Protocol,
		Source:     SourceGPS,
		RawPayload: i.rawPayload(msg),
	}
}

// rawPayload só vai para a posição com POSITIONS_STORE_RAW ligado: é ~25%
// do disco de cada posição e só serve para investigar um modelo novo.
func (i *Ingestor) rawPayload(msg protocols.TrackerMessage) string {
	if !i.cfg.StoreRawPayload {
		return ""
	}
	return msg.RawPayload
}

// emitStateEvents compara o estado anterior com o novo e registra transições.
func (i *Ingestor) emitStateEvents(ctx context.Context, dev *devices.Device, vehicleID *uuid.UUID, before, after *State, msg protocols.TrackerMessage) {
	at := msg.Timestamp
	if at.IsZero() {
		at = time.Now().UTC()
	}

	if changedBool(before.ACC, after.ACC) {
		eventType := events.IgnitionOff
		if *after.ACC {
			eventType = events.IgnitionOn
		}
		i.recordEvent(ctx, dev, eventType, at, msg, nil)
	}

	if changedBool(before.RelayOn, after.RelayOn) {
		// O aparelho confirmou a mudança do relé. Vai para o tempo real porque
		// o painel precisa refletir bloqueio feito por qualquer caminho,
		// inclusive por SMS fora do sistema.
		i.publisher.PublishFor(websocket.TypeEngineStatusChanged, vehicleID, &dev.ID, map[string]any{
			"deviceId": dev.ID,
			"relayOn":  *after.RelayOn,
		})
	}

	if changedBool(before.GPSValid, after.GPSValid) {
		eventType := events.GPSLost
		if *after.GPSValid {
			eventType = events.GPSRecovered
		}
		i.recordEvent(ctx, dev, eventType, at, msg, nil)
	}

	// Sinal GSM zerado é o que o aparelho reporta quando está sem cobertura
	// utilizável; não é inferência nossa sobre a conexão.
	if before.GSMLevel != nil && after.GSMLevel != nil {
		lost := *before.GSMLevel > 0 && *after.GSMLevel == 0
		recovered := *before.GSMLevel == 0 && *after.GSMLevel > 0
		if lost {
			i.recordEvent(ctx, dev, events.GSMLost, at, msg, map[string]any{"gsmLevel": 0})
		} else if recovered {
			i.recordEvent(ctx, dev, events.GSMRecovered, at, msg,
				map[string]any{"gsmLevel": *after.GSMLevel})
		}
	}
}

func (i *Ingestor) recordAlarm(ctx context.Context, dev *devices.Device, msg protocols.TrackerMessage) {
	eventType := events.DeviceAlarm
	switch msg.Alarm {
	case "SOS":
		eventType = events.SOS
	case "POWER_LOSS":
		eventType = events.PowerLoss
	case "LOW_BATTERY":
		eventType = events.LowBattery
	case "VIBRATION":
		eventType = events.Vibration
	case "OVERSPEED":
		eventType = events.Overspeed
	case "IGNITION_ON":
		eventType = events.IgnitionOn
	case "IGNITION_OFF":
		eventType = events.IgnitionOff
	}

	metadata := map[string]any{"alarm": msg.Alarm, "protocol": msg.Protocol}
	if eventType == events.DeviceAlarm {
		// Alarme sem mapeamento canônico: guardamos o rótulo cru em vez de
		// forçá-lo num tipo conhecido (§38).
		metadata["unmapped"] = true
	}
	i.recordEvent(ctx, dev, eventType, msg.Timestamp, msg, metadata)
}

// evaluateOverspeed gera um evento ao entrar em excesso e outro ao voltar ao
// normal, com histerese para não disparar em oscilação (§21).
func (i *Ingestor) evaluateOverspeed(ctx context.Context, dev *devices.Device, vehicle *vehicles.Vehicle, vehicleID *uuid.UUID, pos *Position) {
	limit := i.cfg.DefaultSpeedLimitKmh
	if vehicle != nil && vehicle.SpeedLimitKmh != nil {
		limit = *vehicle.SpeedLimitKmh
	}
	if limit <= 0 {
		return // sem limite configurado não há o que avaliar
	}

	before, after := i.states.Update(ctx, dev.ID, func(st *State) {
		if st.Overspeed {
			// Só sai do estado quando cair abaixo do limite menos a histerese.
			if pos.SpeedKmh <= limit-i.cfg.OverspeedHysteresisKmh {
				st.Overspeed = false
			}
			return
		}
		if pos.SpeedKmh > limit {
			st.Overspeed = true
		}
	})

	if before.Overspeed == after.Overspeed {
		return
	}

	eventType := events.OverspeedEnd
	if after.Overspeed {
		eventType = events.Overspeed
	}
	speed := pos.SpeedKmh
	i.events.Record(ctx, &events.Event{
		DeviceID:  dev.ID,
		Type:      eventType,
		Timestamp: pos.GPSTimestamp,
		Latitude:  &pos.Latitude,
		Longitude: &pos.Longitude,
		SpeedKmh:  &speed,
		Metadata:  map[string]any{"limitKmh": limit, "vehicleId": vehicleID},
	})
}

// evaluateGeofences compara as cercas em que o veículo estava com as atuais.
// Cada veículo só é avaliado nas cercas que o vigiam: as da central e as que o
// dono dele criou para ele.
func (i *Ingestor) evaluateGeofences(ctx context.Context, dev *devices.Device, vehicle *vehicles.Vehicle, pos *Position) {
	if i.geofences == nil {
		return
	}
	if pos.GPSValid != nil && !*pos.GPSValid {
		return // sem fix confiável não avaliamos entrada/saída
	}

	var subject geofences.Subject
	if vehicle != nil {
		subject = geofences.Subject{VehicleID: &vehicle.ID, OwnerID: vehicle.OwnerID}
	}
	current := i.geofences.Inside(subject, pos.Latitude, pos.Longitude)
	before, _ := i.states.Update(ctx, dev.ID, func(st *State) {
		st.InsideFences = current
	})

	emit := func(fence *geofences.Geofence, eventType string) {
		speed := pos.SpeedKmh
		metadata := map[string]any{
			"geofenceId":   fence.ID,
			"geofenceName": fence.Name,
			"vehicleId":    subject.VehicleID,
		}
		// Cerca do cliente: o aviso vai para o dono, se ele quis esse lado.
		if fence.OwnerID != nil {
			metadata["geofenceOwnerId"] = fence.OwnerID.String()
			metadata["notify"] = (eventType == events.GeofenceEnter && fence.NotifyEnter) ||
				(eventType == events.GeofenceExit && fence.NotifyExit)
		}
		i.events.Record(ctx, &events.Event{
			DeviceID:  dev.ID,
			Type:      eventType,
			Timestamp: pos.GPSTimestamp,
			Latitude:  &pos.Latitude,
			Longitude: &pos.Longitude,
			SpeedKmh:  &speed,
			Metadata:  metadata,
		})
	}

	for _, fenceID := range current {
		if !slices.Contains(before.InsideFences, fenceID) {
			if fence, ok := i.geofences.Lookup(fenceID); ok {
				emit(fence, events.GeofenceEnter)
			}
		}
	}
	// Cerca apagada, desligada ou que deixou de vigiar o veículo some da
	// lista sem "saída": o veículo não saiu de lugar nenhum.
	for _, fenceID := range before.InsideFences {
		if !slices.Contains(current, fenceID) && i.geofences.Applies(fenceID, subject) {
			if fence, ok := i.geofences.Lookup(fenceID); ok {
				emit(fence, events.GeofenceExit)
			}
		}
	}
}

func (i *Ingestor) recordEvent(ctx context.Context, dev *devices.Device, eventType string, at time.Time, msg protocols.TrackerMessage, metadata map[string]any) {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadata["protocol"] = msg.Protocol

	event := &events.Event{
		DeviceID:  dev.ID,
		Type:      eventType,
		Timestamp: at.UTC(),
		Metadata:  metadata,
	}
	if msg.HasLocation {
		lat, lon, speed := msg.Latitude, msg.Longitude, msg.SpeedKmh
		event.Latitude = &lat
		event.Longitude = &lon
		event.SpeedKmh = &speed
	}
	i.events.Record(ctx, event)
}

// SweepStatuses recalcula ONLINE/STALE/OFFLINE e publica as mudanças (§17).
func (i *Ingestor) SweepStatuses(ctx context.Context) {
	changes, err := i.devices.SweepStatuses(ctx, i.cfg.StaleAfter, i.cfg.OfflineAfter)
	if err != nil {
		i.log.Error("falha na varredura de status", "err", err)
		return
	}

	for _, change := range changes {
		i.invalidateDevice(change.IMEI)

		topic := websocket.TypeDeviceOnline
		switch change.To {
		case devices.StatusStale:
			topic = websocket.TypeDeviceStale
		case devices.StatusOffline:
			topic = websocket.TypeDeviceOffline
		}

		deviceID := change.DeviceID
		i.publisher.PublishFor(topic, i.vehicleIDFor(ctx, deviceID), &deviceID, map[string]any{
			"deviceId": deviceID,
			"status":   change.To,
			"previous": change.From,
		})

		if change.To == devices.StatusOffline {
			i.events.Record(ctx, &events.Event{
				DeviceID:  deviceID,
				Type:      events.DeviceDisconnected,
				Timestamp: time.Now().UTC(),
				Metadata:  map[string]any{"reason": "sem pacotes além do limite", "previous": change.From},
			})
		} else if change.To == devices.StatusStale {
			i.events.Record(ctx, &events.Event{
				DeviceID:  deviceID,
				Type:      events.DeviceStale,
				Timestamp: time.Now().UTC(),
				Metadata:  map[string]any{"previous": change.From},
			})
		}

		i.log.Info("status do dispositivo mudou",
			"imei", telemetry.IMEI(change.IMEI), "from", change.From, "to", change.To)
	}

	if online, err := i.devices.CountOnline(ctx); err == nil {
		i.metrics.OnlineDevices.Set(float64(online))
	}
}

// ---------------------------------------------------------------------------
// Caches
// ---------------------------------------------------------------------------

func (i *Ingestor) resolveDevice(ctx context.Context, imei string) (*devices.Device, error) {
	i.mu.Lock()
	if cached, ok := i.deviceCache[imei]; ok && time.Since(cached.at) < cacheTTL {
		i.mu.Unlock()
		return cached.device, nil
	}
	i.mu.Unlock()

	dev, err := i.devices.GetByIMEI(ctx, imei)
	if err != nil {
		return nil, err
	}
	i.cacheDevice(dev)
	return dev, nil
}

func (i *Ingestor) cacheDevice(dev *devices.Device) {
	i.mu.Lock()
	i.deviceCache[dev.IMEI] = cachedDevice{device: dev, at: time.Now()}
	i.mu.Unlock()
}

// InvalidateDevice limpa o cache após alteração no cadastro.
func (i *Ingestor) InvalidateDevice(imei string) { i.invalidateDevice(imei) }

func (i *Ingestor) invalidateDevice(imei string) {
	i.mu.Lock()
	delete(i.deviceCache, imei)
	i.mu.Unlock()
}

// InvalidateVehicles limpa o cache de vínculo veículo/dispositivo.
func (i *Ingestor) InvalidateVehicles() {
	i.mu.Lock()
	i.vehicleCache = map[uuid.UUID]cachedVehicle{}
	i.mu.Unlock()
}

func (i *Ingestor) vehicleFor(ctx context.Context, deviceID uuid.UUID) *vehicles.Vehicle {
	i.mu.Lock()
	if cached, ok := i.vehicleCache[deviceID]; ok && time.Since(cached.at) < cacheTTL {
		i.mu.Unlock()
		return cached.vehicle
	}
	i.mu.Unlock()

	vehicle, err := i.vehicles.GetByDeviceID(ctx, deviceID)
	if err != nil && !errors.Is(err, database.ErrNotFound) {
		i.log.Warn("falha ao buscar veículo do dispositivo", "device", deviceID, "err", err)
		return nil
	}
	if errors.Is(err, database.ErrNotFound) {
		vehicle = nil
	}

	i.mu.Lock()
	i.vehicleCache[deviceID] = cachedVehicle{vehicle: vehicle, at: time.Now()}
	i.mu.Unlock()
	return vehicle
}

func (i *Ingestor) vehicleIDFor(ctx context.Context, deviceID uuid.UUID) *uuid.UUID {
	if v := i.vehicleFor(ctx, deviceID); v != nil {
		return &v.ID
	}
	return nil
}

// touch grava o último contato, com intervalo mínimo para não escrever no
// banco a cada pacote.
func (i *Ingestor) touch(ctx context.Context, dev *devices.Device) {
	now := time.Now()

	i.mu.Lock()
	last, seen := i.lastTouch[dev.ID]
	fresh := seen && now.Sub(last) < touchInterval
	if !fresh {
		i.lastTouch[dev.ID] = now
	}
	i.mu.Unlock()

	if fresh && dev.Status == devices.StatusOnline {
		return
	}
	if err := i.devices.Touch(ctx, dev.ID, now); err != nil {
		i.log.Warn("falha ao atualizar o último contato", "device", dev.ID, "err", err)
		return
	}
	if dev.Status != devices.StatusOnline {
		dev.Status = devices.StatusOnline
		i.cacheDevice(dev)
		i.publisher.PublishFor(websocket.TypeDeviceOnline, i.vehicleIDFor(ctx, dev.ID), &dev.ID,
			map[string]any{"deviceId": dev.ID, "status": devices.StatusOnline})
	}
}

// validHeading descarta rumo impossível (o GT06 reserva 10 bits, até 1023):
// melhor mostrar o veículo sem rumo do que girado para um ângulo inventado.
func validHeading(h float64) *float64 {
	if math.IsNaN(h) || h < 0 || h > 360 {
		return nil
	}
	return &h
}

func changedBool(before, after *bool) bool {
	if after == nil {
		return false
	}
	if before == nil {
		return true
	}
	return *before != *after
}

// failedResponse reconhece as negativas mais comuns nas respostas de comando.
func failedResponse(response string) bool {
	lowered := strings.ToLower(response)
	for _, needle := range []string{"fail", "error", "invalid", "password err", "not support"} {
		if strings.Contains(lowered, needle) {
			return true
		}
	}
	return false
}

func truncateHex(hexStr string) string {
	const maxLen = 120
	if len(hexStr) <= maxLen {
		return hexStr
	}
	return hexStr[:maxLen] + "..."
}
