package tcp

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

// Na produção os rastreadores chegam pelo Traefik (roteador TCP da porta
// 5000): sem o cabeçalho PROXY, todos teriam o IP do Traefik — o limite de
// conexões sem login por IP valeria para a frota inteira, e 100 sockets
// parados de qualquer um travariam a entrada de todos os rastreadores.
//
// O cabeçalho é opcional (conexão sem ele segue como veio) e só é lido de um
// proxy confiável (TRUSTED_PROXIES); de qualquer outro endereço, um cabeçalho
// forjado seria só lixo para o protocolo.

// proxyHeaderTimeout: o Traefik manda o cabeçalho junto com a abertura.
const proxyHeaderTimeout = 5 * time.Second

var (
	proxyV1Prefix = []byte("PROXY ")
	proxyV2Sig    = []byte("\r\n\r\n\x00\r\nQUIT\n")
)

// maxProxyV1 é o tamanho máximo da linha da versão 1 (pela especificação).
const maxProxyV1 = 107

// maxProxyV2Body limita endereços + TLVs da versão 2.
const maxProxyV2Body = 1024

// proxiedConn é a conexão com o endereço real do rastreador. Lê do buffer
// usado para examinar o começo do fluxo, que pode ter guardado os primeiros
// bytes do rastreador.
type proxiedConn struct {
	net.Conn
	r      *bufio.Reader
	remote net.Addr
}

func (c *proxiedConn) Read(p []byte) (int, error) { return c.r.Read(p) }

func (c *proxiedConn) RemoteAddr() net.Addr {
	if c.remote != nil {
		return c.remote
	}
	return c.Conn.RemoteAddr()
}

// readProxyHeader consome o cabeçalho PROXY (v1 ou v2), se houver, e devolve
// a conexão com o endereço de origem. Sem cabeçalho, a conexão segue com o
// endereço do socket e os bytes intactos.
func readProxyHeader(conn net.Conn, timeout time.Duration) (net.Conn, error) {
	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	defer func() { _ = conn.SetReadDeadline(time.Time{}) }()

	r := bufio.NewReaderSize(conn, 256)
	out := &proxiedConn{Conn: conn, r: r}
	// Um byte primeiro: protocolo de rastreador com pacote curto não pode
	// ficar esperando bytes que não vêm.
	first, err := r.Peek(1)
	if err != nil {
		return nil, err
	}
	switch first[0] {
	case proxyV1Prefix[0]:
		if head, err := r.Peek(len(proxyV1Prefix)); err != nil || !bytes.Equal(head, proxyV1Prefix) {
			return out, nil
		}
		out.remote, err = readProxyV1(r)
	case proxyV2Sig[0]:
		if head, err := r.Peek(len(proxyV2Sig)); err != nil || !bytes.Equal(head, proxyV2Sig) {
			return out, nil
		}
		out.remote, err = readProxyV2(r)
	default:
		return out, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cabeçalho PROXY inválido: %w", err)
	}
	return out, nil
}

// readProxyV1: "PROXY TCP4 <origem> <destino> <porta origem> <porta destino>\r\n".
func readProxyV1(r *bufio.Reader) (net.Addr, error) {
	line := make([]byte, 0, maxProxyV1)
	for {
		b, err := r.ReadByte()
		if err != nil {
			return nil, err
		}
		line = append(line, b)
		if b == '\n' {
			break
		}
		if len(line) >= maxProxyV1 {
			return nil, errors.New("linha longa demais")
		}
	}
	text, ok := strings.CutSuffix(string(line), "\r\n")
	if !ok {
		return nil, errors.New("linha sem CRLF")
	}
	fields := strings.Fields(text)
	if len(fields) >= 2 && fields[1] == "UNKNOWN" {
		return nil, nil // o proxy não sabe a origem: fica a do socket
	}
	if len(fields) != 6 || (fields[1] != "TCP4" && fields[1] != "TCP6") {
		return nil, fmt.Errorf("linha %q", text)
	}
	ip := net.ParseIP(fields[2])
	port, err := strconv.Atoi(fields[4])
	if ip == nil || err != nil || port < 0 || port > 65535 {
		return nil, fmt.Errorf("origem %q:%q", fields[2], fields[4])
	}
	return &net.TCPAddr{IP: ip, Port: port}, nil
}

// readProxyV2: assinatura, versão/comando, família, tamanho e os endereços.
func readProxyV2(r *bufio.Reader) (net.Addr, error) {
	var head [16]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return nil, err
	}
	if head[12]>>4 != 2 {
		return nil, fmt.Errorf("versão %d", head[12]>>4)
	}
	size := int(binary.BigEndian.Uint16(head[14:16]))
	if size > maxProxyV2Body {
		return nil, fmt.Errorf("corpo de %d bytes", size)
	}
	body := make([]byte, size)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	if head[12]&0x0F == 0 {
		return nil, nil // LOCAL: conexão do próprio proxy (checagem de saúde)
	}
	switch head[13] >> 4 {
	case 1: // IPv4
		if size < 12 {
			return nil, errors.New("endereço IPv4 curto")
		}
		return &net.TCPAddr{IP: net.IP(body[0:4]), Port: int(binary.BigEndian.Uint16(body[8:10]))}, nil
	case 2: // IPv6
		if size < 36 {
			return nil, errors.New("endereço IPv6 curto")
		}
		return &net.TCPAddr{IP: net.IP(body[0:16]), Port: int(binary.BigEndian.Uint16(body[32:34]))}, nil
	}
	return nil, nil // família sem endereço IP: fica a do socket
}
