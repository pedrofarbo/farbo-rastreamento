package tcp

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
)

// proxyV2 monta o cabeçalho que o Traefik manda (PROXY, TCP sobre IPv4/IPv6).
func proxyV2(t *testing.T, src string, port uint16) []byte {
	t.Helper()
	ip := net.ParseIP(src)
	var family byte = 0x11
	addrs := ip.To4()
	if addrs == nil {
		family, addrs = 0x21, ip.To16()
	}
	body := append(append([]byte{}, addrs...), make([]byte, len(addrs))...) // destino zerado
	body = binary.BigEndian.AppendUint16(body, port)
	body = binary.BigEndian.AppendUint16(body, 5000)
	head := append(append([]byte{}, proxyV2Sig...), 0x21, family)
	head = binary.BigEndian.AppendUint16(head, uint16(len(body)))
	return append(head, body...)
}

// readThrough passa o fluxo pelo leitor do cabeçalho e devolve o endereço e
// o que sobrou para o protocolo do rastreador.
func readThrough(t *testing.T, stream []byte) (net.Addr, []byte, error) {
	t.Helper()
	client, server := net.Pipe()
	defer server.Close()
	go func() {
		_, _ = client.Write(stream)
		_ = client.Close()
	}()
	conn, err := readProxyHeader(server, time.Second)
	if err != nil {
		return nil, nil, err
	}
	rest, _ := io.ReadAll(conn)
	return conn.RemoteAddr(), rest, nil
}

func TestReadProxyHeader(t *testing.T) {
	login := []byte{0x78, 0x78, 0x0D, 0x01, 0x08, 0x69}
	cases := []struct {
		name   string
		stream []byte
		addr   string // "" = o do socket
	}{
		{"v2 IPv4", append(proxyV2(t, "200.1.2.3", 4567), login...), "200.1.2.3:4567"},
		{"v2 IPv6", append(proxyV2(t, "2804:14d::1", 40000), login...), "[2804:14d::1]:40000"},
		{"v1", append([]byte("PROXY TCP4 189.10.20.30 172.18.0.5 51000 5000\r\n"), login...), "189.10.20.30:51000"},
		{"v1 UNKNOWN", append([]byte("PROXY UNKNOWN\r\n"), login...), ""},
		{"sem cabeçalho", login, ""},
		{"texto que começa com P", []byte("POSITION,1#"), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			addr, rest, err := readThrough(t, tc.stream)
			if err != nil {
				t.Fatal(err)
			}
			if tc.addr != "" && addr.String() != tc.addr {
				t.Errorf("origem = %s, quer %s", addr, tc.addr)
			}
			if tc.addr == "" && addr.String() != "pipe" {
				t.Errorf("sem origem no cabeçalho devia ficar a do socket, veio %s", addr)
			}
			want := login
			if tc.name == "texto que começa com P" {
				want = tc.stream
			}
			if !bytes.Equal(rest, want) {
				t.Errorf("bytes do rastreador = %x, quer %x", rest, want)
			}
		})
	}

	// LOCAL (checagem de saúde do proxy): consome o cabeçalho, fica o socket.
	local := proxyV2(t, "200.1.2.3", 1)
	local[12] = 0x20
	if addr, rest, err := readThrough(t, append(local, login...)); err != nil || addr.String() != "pipe" || !bytes.Equal(rest, login) {
		t.Errorf("LOCAL = %v %x %v", addr, rest, err)
	}

	for name, stream := range map[string][]byte{
		"v1 sem CRLF":     []byte("PROXY TCP4 1.2.3.4 5.6.7.8 1 2\n"),
		"v1 sem endereço": []byte("PROXY TCP4 x y 1 2\r\n"),
		"v2 versão 1":     append(append([]byte{}, proxyV2Sig...), 0x11, 0x11, 0, 0),
		"v2 corpo enorme": append(append([]byte{}, proxyV2Sig...), 0x21, 0x11, 0xFF, 0xFF),
		"v2 cortado":      proxyV2(t, "200.1.2.3", 1)[:20],
	} {
		if _, _, err := readThrough(t, stream); err == nil {
			t.Errorf("%s: devia recusar", name)
		}
	}
}

// Atrás do Traefik: o servidor vê o IP de cada rastreador (não o do proxy),
// o limite de conexões sem login vale por rastreador, e quem chega sem
// cabeçalho continua funcionando.
func TestServerReadsProxyProtocol(t *testing.T) {
	ingestor := &fakeIngestor{}
	addr, manager, server := startTestServer(t, ingestor, func(c *config.TCP) {
		c.ProxyProtocol = true
		c.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}
		c.MaxPendingPerIP = 1
	})
	// Cada conexão lê o cabeçalho na própria goroutine: espera a vaga do IP
	// ser ocupada antes de testar o limite.
	waitPending := func(ip string) {
		t.Helper()
		for deadline := time.Now().Add(3 * time.Second); ; time.Sleep(10 * time.Millisecond) {
			server.pendingMu.Lock()
			n := server.pending[ip]
			server.pendingMu.Unlock()
			if n == 1 {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("o servidor não registrou a conexão de %s", ip)
			}
		}
	}

	// Dois rastreadores atrás do mesmo proxy, ainda sem login: um por IP.
	first, second := dial(t, addr), dial(t, addr)
	if _, err := first.Write(proxyV2(t, "200.1.2.3", 4567)); err != nil {
		t.Fatal(err)
	}
	if _, err := second.Write(proxyV2(t, "201.9.8.7", 1234)); err != nil {
		t.Fatal(err)
	}
	waitPending("200.1.2.3")
	waitPending("201.9.8.7")
	// Um segundo do mesmo IP sem login passa do limite.
	again := dial(t, addr)
	_, _ = again.Write(proxyV2(t, "200.1.2.3", 9999))
	_ = again.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := again.Read(make([]byte, 16)); err == nil {
		t.Fatal("o segundo sem login do mesmo IP devia ser recusado")
	} else if ne, ok := err.(net.Error); ok && ne.Timeout() {
		t.Fatal("o segundo sem login do mesmo IP continuou aberto")
	}

	// O primeiro se identifica e aparece com o IP real.
	if _, err := first.Write(loginFrame(t, 1)); err != nil {
		t.Fatal(err)
	}
	ingestor.waitForMessages(t, 1)
	_ = first.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := first.Read(make([]byte, 64)); err != nil {
		t.Fatalf("sem ACK do login atrás do proxy: %v", err)
	}
	list := manager.List()
	if len(list) != 1 || list[0].RemoteAddr != "200.1.2.3:4567" {
		t.Fatalf("conexões = %+v", list)
	}

	// Sem cabeçalho (proxy mal configurado): segue com o IP do socket.
	plain := dial(t, addr)
	if _, err := plain.Write(positionFrame()); err != nil {
		t.Fatal(err)
	}
	_ = plain.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	if _, err := plain.Read(make([]byte, 16)); err != nil {
		if ne, ok := err.(net.Error); !ok || !ne.Timeout() {
			t.Fatalf("conexão sem cabeçalho devia continuar aberta, veio %v", err)
		}
	}
}
