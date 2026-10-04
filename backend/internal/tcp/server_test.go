package tcp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/protocols"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/protocols/gt06"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/telemetry"
)

const testIMEI = "869247061234567"

// fakeIngestor grava o que a sessão entrega, para os testes conferirem.
type fakeIngestor struct {
	mu sync.Mutex

	messages   []protocols.TrackerMessage
	invalid    []invalidRecord
	disconnect int

	// rejectAfter faz o ingestor recusar a sessão a partir da N-ésima mensagem.
	rejectAfter int
	deviceID    uuidLike
}

type invalidRecord struct {
	protocol string
	reason   string
	payload  []byte
}

// uuidLike evita importar google/uuid só para o identificador do teste.
type uuidLike = [16]byte

func (f *fakeIngestor) HandleMessage(_ context.Context, conn *DeviceConnection, msg protocols.TrackerMessage) error {
	f.mu.Lock()
	f.messages = append(f.messages, msg)
	count := len(f.messages)
	reject := f.rejectAfter > 0 && count >= f.rejectAfter
	f.mu.Unlock()

	if reject {
		return fmt.Errorf("%w: IMEI desconhecido", ErrRejectSession)
	}
	if msg.IMEI != "" {
		conn.Identify(msg.IMEI, f.deviceID)
	}
	return nil
}

func (f *fakeIngestor) HandleInvalid(_ context.Context, _ *DeviceConnection, payload []byte, protocolName, reason string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.invalid = append(f.invalid, invalidRecord{
		protocol: protocolName, reason: reason,
		payload: append([]byte(nil), payload...),
	})
}

func (f *fakeIngestor) HandleDisconnect(context.Context, *DeviceConnection) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.disconnect++
}

func (f *fakeIngestor) snapshot() ([]protocols.TrackerMessage, []invalidRecord) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]protocols.TrackerMessage(nil), f.messages...),
		append([]invalidRecord(nil), f.invalid...)
}

func (f *fakeIngestor) waitForMessages(t *testing.T, want int) []protocols.TrackerMessage {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		msgs, _ := f.snapshot()
		if len(msgs) >= want {
			return msgs
		}
		time.Sleep(10 * time.Millisecond)
	}
	msgs, _ := f.snapshot()
	t.Fatalf("esperava %d mensagens, recebi %d", want, len(msgs))
	return nil
}

func (f *fakeIngestor) waitForInvalid(t *testing.T, want int) []invalidRecord {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		_, invalid := f.snapshot()
		if len(invalid) >= want {
			return invalid
		}
		time.Sleep(10 * time.Millisecond)
	}
	_, invalid := f.snapshot()
	t.Fatalf("esperava %d pacotes inválidos, recebi %d", want, len(invalid))
	return nil
}

func startServer(t *testing.T, ingestor Ingestor, tune func(*config.TCP)) (string, *Manager) {
	t.Helper()
	addr, manager, _ := startTestServer(t, ingestor, tune)
	return addr, manager
}

// startTestServer também devolve o servidor, para o teste olhar por dentro.
func startTestServer(t *testing.T, ingestor Ingestor, tune func(*config.TCP)) (string, *Manager, *Server) {
	t.Helper()

	cfg := config.TCP{
		Port:           0,
		MaxPacketSize:  4096,
		ReadTimeout:    5 * time.Second,
		WriteTimeout:   2 * time.Second,
		MaxConnections: 10,
		KeepAlive:      30 * time.Second,

		IdentifyTimeout: 5 * time.Second,
	}
	if tune != nil {
		tune(&cfg)
	}

	registry := protocols.NewRegistry(gt06.New(false))
	manager := NewManager(nil)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := NewServer(cfg, registry, manager, ingestor, log, telemetry.NewMetrics())

	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan string, 1)
	done := make(chan struct{})

	go func() {
		defer close(done)
		// Porta 0 deixa o SO escolher; descobrimos o endereço depois do listen.
		go func() {
			for range 200 {
				if addr := server.Addr(); addr != "" {
					ready <- addr
					return
				}
				time.Sleep(5 * time.Millisecond)
			}
			ready <- ""
		}()
		_ = server.ListenAndServe(ctx, "127.0.0.1:0")
	}()

	addr := <-ready
	if addr == "" {
		t.Fatal("servidor não subiu a tempo")
	}

	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Log("servidor demorou a encerrar")
		}
	})
	return addr, manager, server
}

func dial(t *testing.T, addr string) net.Conn {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("falha ao conectar: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func loginFrame(t *testing.T, serial uint16) []byte {
	t.Helper()
	frame, err := gt06.BuildLoginFrame(testIMEI, serial)
	if err != nil {
		t.Fatalf("falha ao montar login: %v", err)
	}
	return frame
}

func positionFrame() []byte {
	return gt06.BuildPositionFrame(gt06.Fix{
		Time:     time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC),
		Latitude: -23.5505, Longitude: -46.6333,
		SpeedKmh: 42, CourseDeg: 90, Satellites: 9, Valid: true,
	}, gt06.DeviceStatus{ACC: true, BatteryLevel: 5, GSMLevel: 4}, 2)
}

// ---------------------------------------------------------------------------

func TestServerHandlesLoginAndAcknowledges(t *testing.T) {
	ingestor := &fakeIngestor{}
	addr, manager := startServer(t, ingestor, nil)

	conn := dial(t, addr)
	if _, err := conn.Write(loginFrame(t, 1)); err != nil {
		t.Fatalf("falha ao escrever: %v", err)
	}

	msgs := ingestor.waitForMessages(t, 1)
	if msgs[0].Kind != protocols.KindLogin || msgs[0].IMEI != testIMEI {
		t.Fatalf("mensagem inesperada: %+v", msgs[0])
	}

	// O servidor precisa responder o login, senão o rastreador desiste.
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	ack := make([]byte, 32)
	n, err := conn.Read(ack)
	if err != nil {
		t.Fatalf("esperava ACK do servidor: %v", err)
	}
	if n < 10 || ack[0] != 0x78 || ack[1] != 0x78 {
		t.Fatalf("ACK malformado: % X", ack[:n])
	}

	// Após identificar-se, a sessão fica disponível para receber comandos.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := manager.Get(testIMEI); ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("a sessão não foi registrada no gerenciador de conexões")
}

func TestServerHandlesPacketSplitAcrossReads(t *testing.T) {
	ingestor := &fakeIngestor{}
	addr, _ := startServer(t, ingestor, nil)

	conn := dial(t, addr)
	frame := loginFrame(t, 1)

	// Um pacote partido em pedaços de 3 bytes, com pausa entre eles:
	// é o caso em que 1 Read() traz menos de um pacote (§31).
	for i := 0; i < len(frame); i += 3 {
		end := min(i+3, len(frame))
		if _, err := conn.Write(frame[i:end]); err != nil {
			t.Fatalf("falha ao escrever: %v", err)
		}
		time.Sleep(15 * time.Millisecond)
	}

	msgs := ingestor.waitForMessages(t, 1)
	if msgs[0].Kind != protocols.KindLogin {
		t.Fatalf("esperava login, recebi %q", msgs[0].Kind)
	}
	if _, invalid := ingestor.snapshot(); len(invalid) > 0 {
		t.Fatalf("pacote partido não deveria gerar inválido: %+v", invalid)
	}
}

func TestServerHandlesMultiplePacketsInSingleRead(t *testing.T) {
	ingestor := &fakeIngestor{}
	addr, _ := startServer(t, ingestor, nil)

	conn := dial(t, addr)

	// Três pacotes numa única escrita: 1 Read() pode trazer mais de um (§31).
	stream := append([]byte{}, loginFrame(t, 1)...)
	stream = append(stream, positionFrame()...)
	stream = append(stream, gt06.BuildHeartbeatFrame(
		gt06.DeviceStatus{ACC: true, BatteryLevel: 4, GSMLevel: 3}, 3)...)

	if _, err := conn.Write(stream); err != nil {
		t.Fatalf("falha ao escrever: %v", err)
	}

	msgs := ingestor.waitForMessages(t, 3)
	kinds := []protocols.MessageKind{msgs[0].Kind, msgs[1].Kind, msgs[2].Kind}
	want := []protocols.MessageKind{
		protocols.KindLogin, protocols.KindPosition, protocols.KindHeartbeat,
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("mensagem %d: esperava %q, recebi %q", i, want[i], kinds[i])
		}
	}

	// A posição precisa chegar íntegra ao domínio, não só enquadrada.
	position := msgs[1]
	if !position.HasLocation || position.SpeedKmh != 42 {
		t.Fatalf("posição inesperada: %+v", position)
	}
	// O IMEI é preenchido pela sessão, já que só o login o carrega.
	if position.IMEI != testIMEI {
		t.Fatalf("a sessão deveria preencher o IMEI, veio %q", position.IMEI)
	}
}

func TestServerCapturesInvalidChecksum(t *testing.T) {
	ingestor := &fakeIngestor{}
	addr, _ := startServer(t, ingestor, nil)

	conn := dial(t, addr)
	frame := loginFrame(t, 1)
	corrupted := append([]byte(nil), frame...)
	corrupted[len(corrupted)-3] ^= 0xFF // estraga o CRC

	if _, err := conn.Write(corrupted); err != nil {
		t.Fatalf("falha ao escrever: %v", err)
	}

	invalid := ingestor.waitForInvalid(t, 1)
	if invalid[0].protocol != "gt06" {
		t.Fatalf("protocolo do registro: %q", invalid[0].protocol)
	}
	if len(invalid[0].payload) == 0 {
		t.Fatal("o payload precisa ser capturado para análise")
	}

	// Um quadro ruim não pode derrubar a sessão: o próximo quadro bom passa.
	if _, err := conn.Write(loginFrame(t, 2)); err != nil {
		t.Fatalf("falha ao escrever: %v", err)
	}
	msgs := ingestor.waitForMessages(t, 1)
	if msgs[0].Kind != protocols.KindLogin {
		t.Fatalf("esperava login após o quadro inválido, recebi %q", msgs[0].Kind)
	}
}

func TestServerCapturesUnknownProtocol(t *testing.T) {
	ingestor := &fakeIngestor{}
	addr, _ := startServer(t, ingestor, nil)

	conn := dial(t, addr)
	// Tráfego que nenhum adaptador registrado reconhece.
	if _, err := conn.Write([]byte("$$XXUNKNOWNPROTOCOLDATA\r\n")); err != nil {
		t.Fatalf("falha ao escrever: %v", err)
	}

	invalid := ingestor.waitForInvalid(t, 1)
	if invalid[0].protocol != "" {
		t.Fatalf("sem detecção o protocolo deve ficar vazio, veio %q", invalid[0].protocol)
	}
	if invalid[0].reason == "" {
		t.Fatal("o motivo precisa ser registrado")
	}

	// A sessão é encerrada: não há como conversar com quem não se entende.
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 8)
	if _, err := conn.Read(buf); !errors.Is(err, io.EOF) {
		t.Fatalf("esperava a conexão fechada, recebi %v", err)
	}
}

func TestServerClosesSessionWhenIngestorRejects(t *testing.T) {
	ingestor := &fakeIngestor{rejectAfter: 1}
	addr, manager := startServer(t, ingestor, nil)

	conn := dial(t, addr)
	if _, err := conn.Write(loginFrame(t, 1)); err != nil {
		t.Fatalf("falha ao escrever: %v", err)
	}

	ingestor.waitForMessages(t, 1)

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 64)
	for {
		_, err := conn.Read(buf)
		if err != nil {
			break // o servidor fechou, que é o esperado
		}
	}

	if _, ok := manager.Get(testIMEI); ok {
		t.Fatal("sessão recusada não pode ficar registrada")
	}
}

func TestServerRejectsOversizedStream(t *testing.T) {
	ingestor := &fakeIngestor{}
	addr, _ := startServer(t, ingestor, func(cfg *config.TCP) {
		cfg.MaxPacketSize = 1024
	})

	conn := dial(t, addr)

	// Cabeçalho válido do GT06 seguido de bytes que nunca fecham o quadro:
	// o buffer não pode crescer sem limite (§7).
	if _, err := conn.Write([]byte{0x79, 0x79, 0x0F, 0xFF}); err != nil {
		t.Fatalf("falha ao escrever: %v", err)
	}
	flood := make([]byte, 2048)
	_, _ = conn.Write(flood)

	invalid := ingestor.waitForInvalid(t, 1)
	found := false
	for _, record := range invalid {
		if record.reason != "" {
			found = true
		}
	}
	if !found {
		t.Fatal("o excesso precisa ser registrado como pacote inválido")
	}
}

func TestManagerReplacesPreviousSession(t *testing.T) {
	manager := NewManager(nil)

	first := &DeviceConnection{Conn: newFakeConn()}
	first.Identify(testIMEI, uuidLike{})
	second := &DeviceConnection{Conn: newFakeConn()}
	second.Identify(testIMEI, uuidLike{})

	manager.Register(first)
	manager.Register(second)

	if manager.Count() != 1 {
		t.Fatalf("esperava 1 sessão, recebi %d", manager.Count())
	}
	current, _ := manager.Get(testIMEI)
	if current != second {
		t.Fatal("a sessão nova deveria substituir a anterior")
	}

	// A sessão antiga, ao terminar, não pode apagar o registro da nova.
	manager.UnregisterConn(first)
	if _, ok := manager.Get(testIMEI); !ok {
		t.Fatal("a sessão corrente foi removida indevidamente")
	}

	manager.UnregisterConn(second)
	if manager.Count() != 0 {
		t.Fatalf("esperava 0 sessões, recebi %d", manager.Count())
	}
}

// fakeConn é uma conexão de mentira para testar o gerenciador sem rede.
type fakeConn struct{ net.Conn }

func newFakeConn() net.Conn {
	client, server := net.Pipe()
	go func() { _, _ = io.Copy(io.Discard, server) }()
	return fakeConn{client}
}

// Quem abre o socket e não manda o login é derrubado no prazo curto, sem
// ocupar a vaga pelo tempo inteiro de um rastreador.
func TestServerDropsUnidentifiedConnection(t *testing.T) {
	addr, _ := startServer(t, &fakeIngestor{}, func(c *config.TCP) { c.IdentifyTimeout = 300 * time.Millisecond })
	conn := dial(t, addr)
	start := time.Now()
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := conn.Read(make([]byte, 16)); err == nil {
		t.Fatal("esperava a conexão fechada pelo servidor")
	}
	if waited := time.Since(start); waited > 2*time.Second {
		t.Fatalf("derrubou só em %s; o prazo de identificação é 300ms", waited)
	}
}

// Identificado, o rastreador passa a ter o prazo normal (não cai no curto).
func TestServerKeepsIdentifiedConnection(t *testing.T) {
	ingestor := &fakeIngestor{}
	addr, _ := startServer(t, ingestor, func(c *config.TCP) { c.IdentifyTimeout = 300 * time.Millisecond })
	conn := dial(t, addr)
	if _, err := conn.Write(loginFrame(t, 1)); err != nil {
		t.Fatal(err)
	}
	ingestor.waitForMessages(t, 1)
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _ = conn.Read(make([]byte, 64)) // ACK do login
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	_, err := conn.Read(make([]byte, 16))
	if ne, ok := err.(net.Error); !ok || !ne.Timeout() {
		t.Fatalf("a conexão identificada devia continuar aberta, veio %v", err)
	}
}

// Um mesmo IP não segura mais que MaxPendingPerIP conexões sem login.
func TestServerLimitsPendingPerIP(t *testing.T) {
	addr, _ := startServer(t, &fakeIngestor{}, func(c *config.TCP) { c.MaxPendingPerIP = 2 })
	first, second := dial(t, addr), dial(t, addr)
	defer first.Close()
	defer second.Close()
	third := dial(t, addr)
	_ = third.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := third.Read(make([]byte, 16)); err == nil {
		t.Fatal("a terceira conexão sem login do mesmo IP devia ser recusada")
	} else if ne, ok := err.(net.Error); ok && ne.Timeout() {
		t.Fatal("a terceira conexão continuou aberta")
	}
	// Liberou uma vaga: volta a aceitar.
	first.Close()
	time.Sleep(200 * time.Millisecond)
	fourth := dial(t, addr)
	defer fourth.Close()
	_ = fourth.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	_, err := fourth.Read(make([]byte, 16))
	if ne, ok := err.(net.Error); !ok || !ne.Timeout() {
		t.Fatalf("com vaga livre a conexão devia ser aceita, veio %v", err)
	}
}
