// Package tcp implementa o servidor que atende os rastreadores e o registro de
// conexões vivas usado para enviar comandos.
package tcp

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/protocols"
)

// ErrNotConnected indica que o dispositivo não tem sessão aberta no momento.
var ErrNotConnected = errors.New("dispositivo não está conectado")

// DeviceConnection é a sessão TCP de um rastreador.
type DeviceConnection struct {
	ID          uuid.UUID
	Conn        net.Conn
	ConnectedAt time.Time

	mu       sync.RWMutex
	imei     string
	deviceID uuid.UUID
	protocol protocols.TrackerProtocol
	lastSeen time.Time
	// replaced: o rastreador abriu outra sessão, e esta foi derrubada por
	// isso (não é uma queda de sinal).
	replaced bool

	writeMu      sync.Mutex
	writeTimeout time.Duration
}

func newConnection(conn net.Conn, writeTimeout time.Duration) *DeviceConnection {
	now := time.Now()
	return &DeviceConnection{
		ID:           uuid.New(),
		Conn:         conn,
		ConnectedAt:  now,
		lastSeen:     now,
		writeTimeout: writeTimeout,
	}
}

func (c *DeviceConnection) IMEI() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.imei
}

func (c *DeviceConnection) DeviceID() uuid.UUID {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.deviceID
}

// Identify grava a identidade do dispositivo autenticado nesta sessão.
func (c *DeviceConnection) Identify(imei string, deviceID uuid.UUID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.imei = imei
	c.deviceID = deviceID
}

func (c *DeviceConnection) Protocol() protocols.TrackerProtocol {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.protocol
}

func (c *DeviceConnection) setProtocol(p protocols.TrackerProtocol) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.protocol = p
}

func (c *DeviceConnection) ProtocolName() string {
	if p := c.Protocol(); p != nil {
		return p.Name()
	}
	return ""
}

func (c *DeviceConnection) LastSeenAt() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastSeen
}

// Replaced diz se a sessão foi derrubada porque o rastreador abriu outra.
func (c *DeviceConnection) Replaced() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.replaced
}

func (c *DeviceConnection) markReplaced() {
	c.mu.Lock()
	c.replaced = true
	c.mu.Unlock()
}

func (c *DeviceConnection) touch() {
	c.mu.Lock()
	c.lastSeen = time.Now()
	c.mu.Unlock()
}

func (c *DeviceConnection) RemoteAddr() string {
	if c.Conn == nil {
		return ""
	}
	return c.Conn.RemoteAddr().String()
}

// Send escreve bytes na conexão. É seguro para uso concorrente.
func (c *DeviceConnection) Send(payload []byte) error {
	if len(payload) == 0 {
		return fmt.Errorf("payload vazio")
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if err := c.Conn.SetWriteDeadline(time.Now().Add(c.writeTimeout)); err != nil {
		return err
	}
	_, err := c.Conn.Write(payload)
	return err
}

func (c *DeviceConnection) Close() error { return c.Conn.Close() }

// ConnectionInfo é a visão somente leitura de uma sessão, para a API.
type ConnectionInfo struct {
	IMEI        string    `json:"imei"`
	DeviceID    uuid.UUID `json:"deviceId"`
	Protocol    string    `json:"protocol"`
	RemoteAddr  string    `json:"remoteAddr"`
	ConnectedAt time.Time `json:"connectedAt"`
	LastSeenAt  time.Time `json:"lastSeenAt"`
}

func (c *DeviceConnection) Info() ConnectionInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	name := ""
	if c.protocol != nil {
		name = c.protocol.Name()
	}
	return ConnectionInfo{
		IMEI:        c.imei,
		DeviceID:    c.deviceID,
		Protocol:    name,
		RemoteAddr:  c.RemoteAddr(),
		ConnectedAt: c.ConnectedAt,
		LastSeenAt:  c.lastSeen,
	}
}

// ConnectionManager guarda as sessões vivas indexadas por IMEI (§7).
type ConnectionManager interface {
	Register(conn *DeviceConnection)
	Unregister(imei string)
	Get(imei string) (*DeviceConnection, bool)
	Send(imei string, payload []byte) error
}

// Manager é a implementação em memória do ConnectionManager.
type Manager struct {
	mu    sync.RWMutex
	conns map[string]*DeviceConnection

	// onChange é chamado quando o número de sessões muda (métrica).
	onChange func(total int)
}

func NewManager(onChange func(total int)) *Manager {
	return &Manager{conns: make(map[string]*DeviceConnection), onChange: onChange}
}

// Register registra a sessão. Se já houver outra sessão do mesmo IMEI, a
// anterior é derrubada: o rastreador reconectou e a antiga é um fantasma.
func (m *Manager) Register(conn *DeviceConnection) {
	imei := conn.IMEI()
	if imei == "" {
		return
	}

	m.mu.Lock()
	previous, existed := m.conns[imei]
	m.conns[imei] = conn
	total := len(m.conns)
	m.mu.Unlock()

	if existed && previous != conn {
		previous.markReplaced()
		_ = previous.Close()
	}
	m.notify(total)
}

// Unregister remove a sessão pelo IMEI.
func (m *Manager) Unregister(imei string) {
	m.mu.Lock()
	delete(m.conns, imei)
	total := len(m.conns)
	m.mu.Unlock()
	m.notify(total)
}

// UnregisterConn remove a sessão apenas se ela ainda for a corrente, evitando
// que uma sessão antiga apague o registro da nova durante uma reconexão.
func (m *Manager) UnregisterConn(conn *DeviceConnection) {
	imei := conn.IMEI()
	if imei == "" {
		return
	}

	m.mu.Lock()
	current, ok := m.conns[imei]
	removed := false
	if ok && current == conn {
		delete(m.conns, imei)
		removed = true
	}
	total := len(m.conns)
	m.mu.Unlock()

	if removed {
		m.notify(total)
	}
}

func (m *Manager) Get(imei string) (*DeviceConnection, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c, ok := m.conns[imei]
	return c, ok
}

func (m *Manager) Send(imei string, payload []byte) error {
	conn, ok := m.Get(imei)
	if !ok {
		return ErrNotConnected
	}
	return conn.Send(payload)
}

func (m *Manager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.conns)
}

func (m *Manager) List() []ConnectionInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]ConnectionInfo, 0, len(m.conns))
	for _, c := range m.conns {
		out = append(out, c.Info())
	}
	return out
}

func (m *Manager) notify(total int) {
	if m.onChange != nil {
		m.onChange(total)
	}
}
