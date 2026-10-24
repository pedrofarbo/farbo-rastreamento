package gt06

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/protocols"
)

// Números de protocolo. DOCUMENTED: especificação GT06/GT06N/Concox.
const (
	msgLogin        = 0x01
	msgGPS          = 0x12
	msgHeartbeat    = 0x13
	msgStringInfo   = 0x15 // resposta do rastreador a um comando do servidor
	msgAlarmGPS     = 0x16
	msgGPSLBS2      = 0x22
	msgAlarm2       = 0x26
	msgAlarm3       = 0x27
	msgGPSLBSStatus = 0x2D
	msgCommand      = 0x80 // servidor -> rastreador
	msgTimeRequest  = 0x8A
	msgInfo         = 0x94 // transmissão de informação (quadro longo)
	msgGPSExtended  = 0xA0
)

// gpsBlockSize é o tamanho do bloco data/hora + coordenadas. DOCUMENTED.
const gpsBlockSize = 18

func isAlarm(typ byte) bool {
	return typ == msgAlarmGPS || typ == msgAlarm2 || typ == msgAlarm3
}

func hasGPS(typ byte) bool {
	switch typ {
	case msgGPS, msgGPSLBS2, msgGPSLBSStatus, msgGPSExtended, msgAlarmGPS, msgAlarm2, msgAlarm3:
		return true
	}
	return false
}

// decodeIMEI converte os 8 bytes BCD do login em 15 dígitos. DOCUMENTED.
func decodeIMEI(b []byte) string {
	if len(b) < 8 {
		return ""
	}
	// O primeiro nibble é enchimento: 0x0869247061234567 -> "869247061234567".
	return fmt.Sprintf("%016x", binary.BigEndian.Uint64(b[:8]))[1:]
}

// decodeGPS lê o bloco data/hora + coordenadas comum aos pacotes de posição.
// DOCUMENTED: lat/lon em unidades de 1/1800000 de grau; bits 10/11/12 do campo
// de curso indicam hemisfério norte, longitude oeste e fix válido.
func decodeGPS(msg *protocols.TrackerMessage, b []byte) error {
	if len(b) < gpsBlockSize {
		return fmt.Errorf("bloco de GPS curto: %d bytes", len(b))
	}

	msg.Timestamp = time.Date(
		2000+int(b[0]), time.Month(b[1]), int(b[2]),
		int(b[3]), int(b[4]), int(b[5]), 0, time.UTC,
	)
	if b[1] < 1 || b[1] > 12 || b[2] < 1 || b[2] > 31 {
		return fmt.Errorf("data inválida no pacote: %02X %02X %02X", b[0], b[1], b[2])
	}

	msg.Satellites = protocols.Ptr(int(b[6] & 0x0F))

	lat := float64(binary.BigEndian.Uint32(b[7:11])) / 1800000.0
	lon := float64(binary.BigEndian.Uint32(b[11:15])) / 1800000.0
	msg.SpeedKmh = float64(b[15]) // DOCUMENTED: o GT06 já reporta em km/h

	flags := binary.BigEndian.Uint16(b[16:18])
	msg.Heading = float64(flags & 0x03FF)
	msg.GPSValid = flags&0x1000 != 0
	if flags&0x0400 == 0 { // bit 10 ligado = hemisfério norte
		lat = -lat
	}
	if flags&0x0800 != 0 { // bit 11 ligado = longitude oeste
		lon = -lon
	}

	if !protocols.ValidCoordinates(lat, lon) {
		return fmt.Errorf("coordenadas fora de faixa: %f,%f", lat, lon)
	}
	msg.Latitude = lat
	msg.Longitude = lon
	msg.HasLocation = true
	return nil
}

// lbsBlockSize devolve o tamanho do bloco LBS. DOCUMENTED: MNC ocupa 2 bytes
// quando o bit alto do MCC está ligado.
func lbsBlockSize(b []byte) int {
	if len(b) < 2 {
		return 0
	}
	if binary.BigEndian.Uint16(b[:2])&0x8000 != 0 {
		return 9
	}
	return 8
}

// applyTerminalInfo traduz o byte de estado do terminal. DOCUMENTED.
//
//	bit7 óleo/energia cortados (relé acionado)
//	bit6 GPS rastreando
//	bits 5-3 estado de alarme
//	bit2 carregando
//	bit1 ACC
//	bit0 armado
func applyTerminalInfo(msg *protocols.TrackerMessage, info byte) {
	msg.RelayOn = protocols.Ptr(info&0x80 != 0)
	msg.ACC = protocols.Ptr(info&0x02 != 0)
	msg.Attributes["charging"] = info&0x04 != 0
	msg.Attributes["armed"] = info&0x01 != 0

	switch (info >> 3) & 0x07 {
	case 1:
		msg.Alarm = "VIBRATION"
	case 2:
		msg.Alarm = "POWER_LOSS"
	case 3:
		msg.Alarm = "LOW_BATTERY"
	case 4:
		msg.Alarm = "SOS"
	}
}

// batteryLevelPercent converte o nível 0-6 do GT06 em porcentagem.
// DOCUMENTED como nível relativo: o GT06 não informa tensão neste campo,
// por isso preenchemos BatteryPercent aqui; a tensão (da bateria do veículo)
// vem só do pacote 0x94 (decodeInfo).
func batteryLevelPercent(level byte) int {
	if level > 6 {
		level = 6
	}
	return int(level) * 100 / 6
}

// Tipos de informação do pacote 0x94. DOCUMENTED: especificação Concox
// GT06N, "Information Transmission Packet".
const infoExternalVoltage = 0x00

// decodeInfo lê o pacote 0x94: tipo da informação (1) + conteúdo. Do tipo
// 0x00 sai a tensão da alimentação externa (a bateria do veículo), em
// centésimos de volt (2 bytes). Os outros tipos ficam crus (§38).
func decodeInfo(msg *protocols.TrackerMessage, content []byte) {
	if len(content) < 1 {
		return
	}
	msg.Attributes["infoType"] = fmt.Sprintf("0x%02X", content[0])
	if content[0] == infoExternalVoltage && len(content) >= 3 {
		volts := float64(binary.BigEndian.Uint16(content[1:3])) / 100
		msg.BatteryVoltage = &volts
		msg.Kind = protocols.KindStatus
		return
	}
	msg.Attributes["unhandled"] = true
}

// alarmLabel mapeia o byte de alarme dos pacotes 0x16/0x26/0x27. DOCUMENTED
// para os códigos abaixo; os demais viram ALARM_0xNN sem interpretação.
func alarmLabel(code byte) string {
	switch code {
	case 0x00:
		return ""
	case 0x01:
		return "SOS"
	case 0x02:
		return "POWER_LOSS"
	case 0x03:
		return "VIBRATION"
	case 0x04:
		return "GEOFENCE_ENTER"
	case 0x05:
		return "GEOFENCE_EXIT"
	case 0x06:
		return "OVERSPEED"
	case 0x09:
		return "DISPLACEMENT"
	case 0x0A:
		return "GPS_LOST"
	case 0x0B:
		return "GPS_RECOVERED"
	case 0x0E, 0x0F:
		return "LOW_BATTERY"
	case 0xFE:
		return "IGNITION_ON"
	case 0xFF:
		return "IGNITION_OFF"
	default:
		return fmt.Sprintf("ALARM_0x%02X", code)
	}
}

// decodeReplyText trata respostas em ASCII e em UTF-16BE (idioma 0x0002).
func decodeReplyText(b []byte) string {
	if len(b) >= 2 && b[0] == 0x00 && b[1] != 0x00 {
		out := make([]byte, 0, len(b)/2)
		for i := 1; i < len(b); i += 2 {
			out = append(out, b[i])
		}
		return strings.TrimSpace(string(out))
	}
	return strings.TrimSpace(string(b))
}

func hexdump(b []byte) string { return strings.ToUpper(hex.EncodeToString(b)) }
