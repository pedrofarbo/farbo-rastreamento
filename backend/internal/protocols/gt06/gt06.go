// Package gt06 implementa o protocolo binário GT06/Concox.
//
// Nível de confiança: DOCUMENTED. O formato dos quadros, o CRC-ITU, o login
// por IMEI em BCD, o bloco de GPS e o pacote de comando 0x80 seguem a
// especificação pública GT06/GT06N/Concox.
//
// Vários rastreadores 4G vendidos como TKSTAR são OEM de plataformas Concox e
// falam este protocolo. Se o seu TK970 abrir a conexão com 78 78 ou 79 79,
// é este o adaptador que vai atendê-lo.
package gt06

import (
	"fmt"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/protocols"
)

const Name = "gt06"

// Protocol implementa protocols.TrackerProtocol.
type Protocol struct {
	// AckGPS responde também aos pacotes de posição. A especificação diz que
	// o servidor não precisa responder, mas alguns firmwares reenviam a
	// posição indefinidamente sem ACK.
	AckGPS bool
}

func New(ackGPS bool) *Protocol { return &Protocol{AckGPS: ackGPS} }

func (p *Protocol) Name() string { return Name }

// Detect reconhece os delimitadores de início do GT06.
func (p *Protocol) Detect(data []byte) bool {
	if len(data) < 2 {
		return false
	}
	return (data[0] == startShort && data[1] == startShort) ||
		(data[0] == startLong && data[1] == startLong)
}

func (p *Protocol) Describe() protocols.Descriptor {
	return protocols.Descriptor{
		Name:       Name,
		Label:      "GT06 / Concox (binário)",
		Vendor:     "Concox e OEMs (inclui parte da linha TKSTAR 4G)",
		Confidence: protocols.Documented,
		Commands: []protocols.CommandType{
			protocols.CommandEngineCut, protocols.CommandEngineResume,
			protocols.CommandRequestPosition, protocols.CommandRequestStatus,
			protocols.CommandSetInterval, protocols.CommandSetHeartbeat,
			protocols.CommandSetServer, protocols.CommandReboot, protocols.CommandCustom,
		},
		Notes: "Comandos viajam em pacotes 0x80 com server flag de 4 bytes, " +
			"o que permite casar o ACK 0x15 com o comando exato.",
	}
}

// Parse decodifica um quadro completo devolvido por NextFrame.
//
// Só o pacote de login carrega o IMEI: nos demais quem preenche esse campo é
// a sessão TCP, que já conhece o dispositivo autenticado.
func (p *Protocol) Parse(frame []byte) ([]protocols.TrackerMessage, error) {
	pkt, err := decodeFrame(frame)
	if err != nil {
		return nil, err
	}

	msg := protocols.TrackerMessage{
		Protocol:   Name,
		RawPayload: hexdump(frame),
		Attributes: map[string]any{"packetType": fmt.Sprintf("0x%02X", pkt.typ)},
		Kind:       protocols.KindOther,
	}

	switch {
	case pkt.typ == msgLogin:
		imei := decodeIMEI(pkt.content)
		if err := protocols.ValidateIMEI(imei); err != nil {
			return nil, fmt.Errorf("login GT06: %w", err)
		}
		msg.Kind = protocols.KindLogin
		msg.IMEI = imei
		msg.Ack = buildFrame(msgLogin, nil, pkt.serial)

	case pkt.typ == msgHeartbeat:
		msg.Kind = protocols.KindHeartbeat
		if err := p.decodeHeartbeat(&msg, pkt); err != nil {
			return nil, err
		}
		msg.Ack = buildFrame(msgHeartbeat, nil, pkt.serial)

	case pkt.typ == msgStringInfo:
		msg.Kind = protocols.KindCommandAck
		if err := decodeCommandReply(&msg, pkt); err != nil {
			return nil, err
		}

	case pkt.typ == msgTimeRequest:
		msg.Kind = protocols.KindOther
		msg.Ack = buildTimeReply(pkt.serial)

	case pkt.typ == msgInfo:
		// DOCUMENTED: o 0x94 não pede resposta do servidor.
		msg.Kind = protocols.KindOther
		decodeInfo(&msg, pkt.content)

	case hasGPS(pkt.typ):
		if err := p.decodeLocation(&msg, pkt); err != nil {
			return nil, err
		}
		if isAlarm(pkt.typ) || p.AckGPS {
			msg.Ack = buildFrame(pkt.typ, nil, pkt.serial)
		}

	default:
		// Pacote conhecido pelo enquadramento mas não interpretado.
		// UNKNOWN: fica registrado cru para análise (§38).
		msg.Kind = protocols.KindOther
		msg.Attributes["unhandled"] = true
		if p.AckGPS {
			msg.Ack = buildFrame(pkt.typ, nil, pkt.serial)
		}
	}

	return []protocols.TrackerMessage{msg}, nil
}

// heartbeatSize é o conteúdo do heartbeat no GT06 padrão.
const heartbeatSize = 5

func (p *Protocol) decodeHeartbeat(msg *protocols.TrackerMessage, pkt *packet) error {
	// DOCUMENTED: estado do terminal (1) + nível de bateria (1) + sinal GSM (1)
	// + alarme/idioma (2).
	if len(pkt.content) < 3 {
		return fmt.Errorf("heartbeat curto: %d bytes", len(pkt.content))
	}
	applyTerminalInfo(msg, pkt.content[0])
	msg.BatteryPercent = protocols.Ptr(batteryLevelPercent(pkt.content[1]))
	msg.GSMLevel = protocols.Ptr(int(pkt.content[2]))
	// UNKNOWN: firmware que acrescenta campos ao heartbeat (a voltagem
	// externa, em alguns J16): os bytes a mais ficam marcados para análise.
	if extra := len(pkt.content) - heartbeatSize; extra > 0 {
		msg.Attributes["extraBytes"] = extra
	}
	if msg.Alarm != "" {
		msg.Kind = protocols.KindAlarm
	}
	return nil
}

func (p *Protocol) decodeLocation(msg *protocols.TrackerMessage, pkt *packet) error {
	if err := decodeGPS(msg, pkt.content); err != nil {
		return err
	}
	msg.Kind = protocols.KindPosition

	rest := pkt.content[gpsBlockSize:]
	if n := lbsBlockSize(rest); n > 0 && len(rest) >= n {
		rest = rest[n:]
	}

	// DOCUMENTED (Concox GT06N): no 0x22, depois do LBS vêm a ignição (ACC),
	// o modo de envio e o reenvio em tempo real — não o estado do terminal.
	// Lidos como estado, o modo e o reenvio (zeros) viravam bateria 0% e
	// sinal 0 (o J16 passa a mandar o 0x22 com SZCS#GT06SEL=1#).
	if pkt.typ == msgGPSLBS2 {
		if len(rest) >= 1 {
			msg.ACC = protocols.Ptr(rest[0] == 0x01)
		}
		return nil
	}

	// Pacotes com bloco de estado trazem terminal info + bateria + GSM.
	if len(rest) >= 3 {
		applyTerminalInfo(msg, rest[0])
		msg.BatteryPercent = protocols.Ptr(batteryLevelPercent(rest[1]))
		msg.GSMLevel = protocols.Ptr(int(rest[2]))
		rest = rest[3:]
	}

	if isAlarm(pkt.typ) && len(rest) >= 1 {
		if label := alarmLabel(rest[0]); label != "" {
			msg.Alarm = label
			msg.Kind = protocols.KindAlarm
		}
	}
	return nil
}

// decodeCommandReply lê o pacote 0x15. DOCUMENTED: tamanho (1) +
// server flag (4) + conteúdo ASCII + idioma (2).
func decodeCommandReply(msg *protocols.TrackerMessage, pkt *packet) error {
	if len(pkt.content) < 5 {
		return fmt.Errorf("resposta de comando curta: %d bytes", len(pkt.content))
	}
	length := int(pkt.content[0])
	if length < 4 || 1+length > len(pkt.content) {
		length = len(pkt.content) - 1
	}
	msg.CorrelationKey = uint32(pkt.content[1])<<24 | uint32(pkt.content[2])<<16 |
		uint32(pkt.content[3])<<8 | uint32(pkt.content[4])
	msg.Response = decodeReplyText(pkt.content[5 : 1+length])
	return nil
}
