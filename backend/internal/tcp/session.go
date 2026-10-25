package tcp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"time"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/protocols"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/telemetry"
)

// readChunk é o tamanho de cada leitura do socket.
const readChunk = 2048

// detectMinBytes é o mínimo acumulado antes de desistir de identificar o
// protocolo. Abaixo disso o pacote ainda pode estar chegando pela metade.
const detectMinBytes = 8

// session é o laço de leitura de uma conexão.
//
// O ponto central aqui é que uma leitura do socket NÃO corresponde a um pacote
// (§31): os bytes entram num buffer acumulado e é o Framer do protocolo que
// diz onde cada quadro começa e termina.
type session struct {
	server *Server
	conn   *DeviceConnection
	log    *slog.Logger

	buf        []byte
	registered bool
	// ip é o endereço do rastreador (conta nas conexões não identificadas).
	ip string
	// sampled: quando cada tipo de pacote fora do padrão foi para o log.
	sampled map[string]time.Time
}

// sampleEvery limita o log dos pacotes fora do padrão: um de cada tipo por
// conexão nesse intervalo.
const sampleEvery = 10 * time.Minute

// sample põe no log, cru, o pacote que o protocolo não interpretou por
// inteiro (tipo desconhecido, informação 0x94, bytes a mais): é por ele que
// se descobre um formato novo de firmware (a voltagem externa do J16, por
// exemplo). Normalmente não acontece; quando acontece, sai uma amostra.
func (s *session) sample(msg protocols.TrackerMessage) {
	unhandled, _ := msg.Attributes["unhandled"].(bool)
	info, hasInfo := msg.Attributes["infoType"]
	extra, hasExtra := msg.Attributes["extraBytes"]
	if !unhandled && !hasInfo && !hasExtra {
		return
	}
	key := fmt.Sprint(msg.Attributes["packetType"], info)
	if last, ok := s.sampled[key]; ok && time.Since(last) < sampleEvery {
		return
	}
	if s.sampled == nil {
		s.sampled = map[string]time.Time{}
	}
	s.sampled[key] = time.Now()
	s.log.Info("pacote fora do padrão, para análise", "imei", msg.IMEI, "packetType", msg.Attributes["packetType"],
		"infoType", info, "extraBytes", extra, "unhandled", unhandled, "raw", msg.RawPayload)
}

func (s *session) run(ctx context.Context) {
	chunk := make([]byte, readChunk)

	// Fecha o socket quando o servidor está desligando, desbloqueando o Read.
	stop := context.AfterFunc(ctx, func() { _ = s.conn.Close() })
	defer stop()

	for {
		// Até se identificar, o prazo é curto: não dá para segurar a vaga
		// sem mandar o login.
		timeout := s.server.cfg.ReadTimeout
		if !s.registered {
			timeout = s.server.cfg.IdentifyTimeout
		}
		if err := s.conn.Conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
			return
		}

		n, err := s.conn.Conn.Read(chunk)
		if n > 0 {
			s.conn.touch()
			s.buf = append(s.buf, chunk[:n]...)
			if !s.process(ctx) {
				return
			}
		}
		if err != nil {
			s.logDisconnect(err)
			return
		}
	}
}

func (s *session) logDisconnect(err error) {
	switch {
	case errors.Is(err, io.EOF):
		s.log.Debug("rastreador encerrou a conexão")
	case errors.Is(err, net.ErrClosed):
		s.log.Debug("conexão fechada pelo servidor")
	case isTimeout(err):
		s.log.Info("conexão sem tráfego pelo tempo limite, encerrando",
			"imei", telemetry.IMEI(s.conn.IMEI()), "timeout", s.server.cfg.ReadTimeout)
	default:
		s.log.Debug("leitura encerrada", "err", err)
	}
}

func isTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

// process consome o buffer acumulado. Devolve false para encerrar a sessão.
func (s *session) process(ctx context.Context) bool {
	if len(s.buf) > s.server.cfg.MaxPacketSize {
		// Protege contra exaustão de memória: um fluxo que nunca fecha um
		// quadro não pode crescer indefinidamente (§7).
		s.server.metrics.PacketsInvalid.WithLabelValues("oversized").Inc()
		s.server.ingestor.HandleInvalid(ctx, s.conn, s.truncate(s.buf),
			s.conn.ProtocolName(), "buffer excedeu o tamanho máximo de pacote")
		s.log.Warn("buffer acima do limite, encerrando sessão",
			"bytes", len(s.buf), "limit", s.server.cfg.MaxPacketSize)
		return false
	}

	if s.conn.Protocol() == nil {
		switch s.detect(ctx) {
		case detectPending:
			return true // poucos bytes ainda: espera o resto do cabeçalho
		case detectFailed:
			return false
		}
	}

	proto := s.conn.Protocol()
	for len(s.buf) > 0 {
		frame, consumed, err := proto.NextFrame(s.buf)

		switch {
		case errors.Is(err, protocols.ErrIncompleteFrame):
			return true // aguarda o resto do quadro
		case err != nil:
			s.server.metrics.PacketsInvalid.WithLabelValues("framing").Inc()
			bad := s.buf[:min(consumed, len(s.buf))]
			s.server.ingestor.HandleInvalid(ctx, s.conn, s.truncate(bad), proto.Name(), err.Error())
			s.log.Warn("quadro inválido descartado", "err", err, "bytes", consumed)
			if consumed <= 0 {
				return false // protocolo não conseguiu avançar: evita laço infinito
			}
			s.buf = s.buf[consumed:]
			continue
		case consumed <= 0:
			return true
		}

		s.buf = s.buf[consumed:]
		if frame == nil {
			continue // bytes de separação descartados
		}
		if !s.dispatch(ctx, proto, frame) {
			return false
		}
	}
	return true
}

// detectResult diz o que fazer depois da tentativa de identificação.
type detectResult int

const (
	detectOK detectResult = iota
	detectPending
	detectFailed
)

// detect escolhe o protocolo da conexão a partir dos primeiros bytes.
func (s *session) detect(ctx context.Context) detectResult {
	if proto, ok := s.server.registry.Detect(s.buf); ok {
		s.conn.setProtocol(proto)
		s.log = s.log.With("protocol", proto.Name())
		s.log.Debug("protocolo identificado")
		return detectOK
	}

	if len(s.buf) < detectMinBytes {
		return detectPending // ainda pode ser um cabeçalho pela metade
	}

	// Nenhum adaptador reconheceu o tráfego. É exatamente o caso que a captura
	// crua existe para resolver: guardamos tudo e encerramos (§38).
	s.server.metrics.PacketsInvalid.WithLabelValues("unknown_protocol").Inc()
	s.server.ingestor.HandleInvalid(ctx, s.conn, s.truncate(s.buf), "",
		"nenhum protocolo registrado reconheceu o tráfego")
	s.log.Warn("protocolo não identificado, tráfego capturado para análise",
		"bytes", len(s.buf))
	s.buf = nil
	return detectFailed
}

// dispatch decodifica um quadro e entrega as mensagens ao domínio.
func (s *session) dispatch(ctx context.Context, proto protocols.TrackerProtocol, frame []byte) bool {
	msgs, err := proto.Parse(frame)
	if err != nil {
		s.server.metrics.PacketsInvalid.WithLabelValues("parse").Inc()
		s.server.ingestor.HandleInvalid(ctx, s.conn, s.truncate(frame), proto.Name(), err.Error())
		s.log.Warn("falha ao interpretar quadro", "err", err)
		return true // um quadro ruim não derruba a sessão
	}

	for _, msg := range msgs {
		if msg.Protocol == "" {
			msg.Protocol = proto.Name()
		}
		// Só o login costuma trazer o IMEI; nos demais a sessão já sabe quem é.
		if msg.IMEI == "" {
			msg.IMEI = s.conn.IMEI()
		}

		s.server.metrics.PacketsReceived.WithLabelValues(proto.Name(), string(msg.Kind)).Inc()
		s.sample(msg)

		// O ACK vai antes do processamento: firmware que não recebe resposta
		// rápida reenvia o pacote ou derruba a conexão.
		if len(msg.Ack) > 0 {
			if err := s.conn.Send(msg.Ack); err != nil {
				s.log.Warn("falha ao enviar ACK", "err", err)
				return false
			}
		}

		if err := s.server.ingestor.HandleMessage(ctx, s.conn, msg); err != nil {
			if errors.Is(err, ErrRejectSession) {
				s.log.Warn("sessão recusada pelo ingestor", "err", err)
				return false
			}
			s.log.Error("falha ao processar mensagem", "err", err, "kind", msg.Kind)
			continue
		}

		// O primeiro pacote que identifica o dispositivo publica a sessão no
		// registro, tornando-a alvo de comandos.
		if !s.registered && s.conn.IMEI() != "" {
			s.server.manager.Register(s.conn)
			s.server.metrics.Connections.Set(float64(s.server.manager.Count()))
			s.registered = true
			s.server.releasePending(s.ip)
			s.log = s.log.With("imei", telemetry.IMEI(s.conn.IMEI()))
			s.log.Info("sessão de rastreador registrada")
		}
	}
	return true
}

// truncate limita o payload guardado na captura crua.
func (s *session) truncate(b []byte) []byte {
	const maxCapture = 2048
	if len(b) <= maxCapture {
		out := make([]byte, len(b))
		copy(out, b)
		return out
	}
	out := make([]byte, maxCapture)
	copy(out, b[:maxCapture])
	return out
}
