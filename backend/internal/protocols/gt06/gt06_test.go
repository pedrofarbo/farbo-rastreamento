package gt06

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/protocols"
)

// ---------------------------------------------------------------------------
// Fixtures
//
// Os quadros abaixo são SINTÉTICOS: montados pelo nosso próprio encoder, que
// segue a especificação pública do GT06 (delimitadores, campo de tamanho,
// CRC-ITU e bytes de parada). Eles provam a coerência do par encoder/decoder e
// as regras de enquadramento.
//
// TODO: VERIFY AGAINST DEVICE PROTOCOL — assim que houver captura real de um
// TK910/TK970, os bytes crus devem ser colados aqui como fixture adicional.
// A tabela raw_packets do banco existe justamente para produzir essa captura.
// ---------------------------------------------------------------------------

// gpsFlags monta o campo de curso/estado do bloco de GPS.
func gpsFlags(course uint16, valid, north, west bool) uint16 {
	flags := course & 0x03FF
	if valid {
		flags |= 0x1000
	}
	if north {
		flags |= 0x0400
	}
	if west {
		flags |= 0x0800
	}
	return flags
}

type gpsFixture struct {
	when       time.Time
	lat, lon   float64
	speedKmh   byte
	course     uint16
	satellites byte
	valid      bool
}

func (f gpsFixture) content() []byte {
	out := []byte{
		byte(f.when.Year() - 2000), byte(f.when.Month()), byte(f.when.Day()),
		byte(f.when.Hour()), byte(f.when.Minute()), byte(f.when.Second()),
		0xC0 | (f.satellites & 0x0F),
	}
	out = binary.BigEndian.AppendUint32(out, uint32(math.Round(math.Abs(f.lat)*1800000)))
	out = binary.BigEndian.AppendUint32(out, uint32(math.Round(math.Abs(f.lon)*1800000)))
	out = append(out, f.speedKmh)
	out = binary.BigEndian.AppendUint16(out, gpsFlags(f.course, f.valid, f.lat >= 0, f.lon < 0))
	return out
}

// lbs devolve um bloco LBS de 8 bytes (MCC 724 = Brasil, MNC 06 = Vivo).
func lbs() []byte {
	out := binary.BigEndian.AppendUint16(nil, 724)
	out = append(out, 6)
	out = binary.BigEndian.AppendUint16(out, 0x1234)
	return append(out, 0x00, 0xAB, 0xCD)
}

// statusBlock monta terminal info + nível de bateria + sinal GSM.
func statusBlock(terminalInfo, batteryLevel, gsm byte) []byte {
	return []byte{terminalInfo, batteryLevel, gsm}
}

func mustParseOne(t *testing.T, p *Protocol, frame []byte) protocols.TrackerMessage {
	t.Helper()
	msgs, err := p.Parse(frame)
	if err != nil {
		t.Fatalf("Parse falhou: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("esperava 1 mensagem, recebi %d", len(msgs))
	}
	return msgs[0]
}

func newProtocol() *Protocol { return New(false) }

// ---------------------------------------------------------------------------
// CRC
// ---------------------------------------------------------------------------

func TestCRCITUCheckValue(t *testing.T) {
	// Vetor de verificação padrão do CRC-16/X-25.
	if got := crcITU([]byte("123456789")); got != 0x906E {
		t.Fatalf("esperava 0x906E, recebi 0x%04X", got)
	}
}

func TestParseInvalidChecksum(t *testing.T) {
	frame := buildFrame(msgLogin, mustHex(t, "0869247061234567"), 1)
	corrupted := append([]byte(nil), frame...)
	corrupted[len(corrupted)-3] ^= 0xFF // estraga o CRC

	if _, err := newProtocol().Parse(corrupted); err == nil {
		t.Fatal("esperava erro de CRC")
	}
}

// ---------------------------------------------------------------------------
// Login
// ---------------------------------------------------------------------------

func TestParseLogin(t *testing.T) {
	frame := buildFrame(msgLogin, mustHex(t, "0869247061234567"), 0x0001)

	msg := mustParseOne(t, newProtocol(), frame)
	if msg.Kind != protocols.KindLogin {
		t.Fatalf("esperava login, recebi %q", msg.Kind)
	}
	if msg.IMEI != "869247061234567" {
		t.Fatalf("IMEI incorreto: %q", msg.IMEI)
	}
	if len(msg.Ack) == 0 {
		t.Fatal("login precisa responder ACK")
	}
	// O ACK precisa ecoar o número de série recebido.
	ack, err := decodeFrame(msg.Ack)
	if err != nil {
		t.Fatalf("ACK malformado: %v", err)
	}
	if ack.typ != msgLogin || ack.serial != 0x0001 {
		t.Fatalf("ACK inesperado: tipo 0x%02X serial %d", ack.typ, ack.serial)
	}
}

func TestParseLoginWithInvalidIMEI(t *testing.T) {
	// IMEI todo zerado não passa na validação.
	frame := buildFrame(msgLogin, make([]byte, 8), 1)
	if _, err := newProtocol().Parse(frame); err == nil {
		t.Fatal("esperava recusa de IMEI inválido")
	}
}

// ---------------------------------------------------------------------------
// Posição
// ---------------------------------------------------------------------------

func TestParsePosition(t *testing.T) {
	when := time.Date(2026, 9, 17, 13, 45, 30, 0, time.UTC)
	fx := gpsFixture{when: when, lat: -23.5505, lon: -46.6333, speedKmh: 62, course: 275, satellites: 9, valid: true}

	content := append(fx.content(), lbs()...)
	frame := buildFrame(msgGPS, content, 7)

	msg := mustParseOne(t, newProtocol(), frame)

	if msg.Kind != protocols.KindPosition {
		t.Fatalf("esperava posição, recebi %q", msg.Kind)
	}
	if !msg.HasLocation {
		t.Fatal("esperava HasLocation")
	}
	if !msg.Timestamp.Equal(when) {
		t.Fatalf("timestamp incorreto: %s", msg.Timestamp)
	}
	if !msg.GPSValid {
		t.Fatal("esperava fix válido")
	}
	if msg.Heading != 275 {
		t.Fatalf("heading incorreto: %v", msg.Heading)
	}
	if msg.Satellites == nil || *msg.Satellites != 9 {
		t.Fatalf("satélites incorretos: %v", msg.Satellites)
	}
	if msg.RawPayload != hexdump(frame) {
		t.Fatal("RawPayload deveria conter o quadro original")
	}
	// Pacote 0x12 puro não exige resposta do servidor.
	if len(msg.Ack) != 0 {
		t.Fatal("pacote de GPS não deveria gerar ACK com AckGPS desligado")
	}
}

func TestParseCoordinates(t *testing.T) {
	tests := []struct {
		name     string
		lat, lon float64
	}{
		{name: "sudoeste (São Paulo)", lat: -23.5505, lon: -46.6333},
		{name: "nordeste (Berlim)", lat: 52.5200, lon: 13.4050},
		{name: "sudeste (Sydney)", lat: -33.8688, lon: 151.2093},
		{name: "noroeste (Nova York)", lat: 40.7128, lon: -74.0060},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fx := gpsFixture{when: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
				lat: tc.lat, lon: tc.lon, satellites: 8, valid: true}
			frame := buildFrame(msgGPS, append(fx.content(), lbs()...), 1)

			msg := mustParseOne(t, newProtocol(), frame)

			// A resolução do GT06 é 1/1800000 de grau (~0,06 m).
			if math.Abs(msg.Latitude-tc.lat) > 1e-5 {
				t.Fatalf("latitude: esperava %f, recebi %f", tc.lat, msg.Latitude)
			}
			if math.Abs(msg.Longitude-tc.lon) > 1e-5 {
				t.Fatalf("longitude: esperava %f, recebi %f", tc.lon, msg.Longitude)
			}
		})
	}
}

func TestParseSpeed(t *testing.T) {
	for _, speed := range []byte{0, 1, 62, 120, 255} {
		fx := gpsFixture{when: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			lat: -23.5, lon: -46.6, speedKmh: speed, satellites: 5, valid: true}
		frame := buildFrame(msgGPS, append(fx.content(), lbs()...), 1)

		msg := mustParseOne(t, newProtocol(), frame)
		// O GT06 reporta velocidade já em km/h: nenhuma conversão deve ocorrer.
		if msg.SpeedKmh != float64(speed) {
			t.Fatalf("esperava %d km/h, recebi %v", speed, msg.SpeedKmh)
		}
	}
}

func TestParseInvalidPosition(t *testing.T) {
	tests := []struct {
		name    string
		content []byte
	}{
		{name: "bloco de GPS truncado", content: []byte{0x1A, 0x09, 0x11, 0x0D, 0x2D, 0x1E}},
		{
			name: "mês fora de faixa",
			content: append([]byte{0x1A, 0x00, 0x11, 0x0D, 0x2D, 0x1E, 0xC8},
				make([]byte, 11)...),
		},
		{
			name: "latitude fora do globo",
			content: func() []byte {
				out := []byte{0x1A, 0x09, 0x11, 0x0D, 0x2D, 0x1E, 0xC8}
				out = binary.BigEndian.AppendUint32(out, 200*1800000) // 200 graus
				out = binary.BigEndian.AppendUint32(out, 46*1800000)
				out = append(out, 10)
				return binary.BigEndian.AppendUint16(out, gpsFlags(90, true, true, false))
			}(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			frame := buildFrame(msgGPS, tc.content, 1)
			if _, err := newProtocol().Parse(frame); err == nil {
				t.Fatal("esperava erro de decodificação")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ACC, bateria, relé e alarmes
// ---------------------------------------------------------------------------

func TestParseACC(t *testing.T) {
	fx := gpsFixture{when: time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC),
		lat: -23.5, lon: -46.6, satellites: 7, valid: true}

	tests := []struct {
		name         string
		terminalInfo byte
		wantACC      bool
		wantRelay    bool
	}{
		{name: "ignição desligada", terminalInfo: 0x00, wantACC: false, wantRelay: false},
		{name: "ignição ligada", terminalInfo: 0x02, wantACC: true, wantRelay: false},
		{name: "ignição ligada e relé acionado", terminalInfo: 0x82, wantACC: true, wantRelay: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			content := append(fx.content(), lbs()...)
			content = append(content, statusBlock(tc.terminalInfo, 5, 4)...)
			frame := buildFrame(msgGPSLBSStatus, content, 3)

			msg := mustParseOne(t, newProtocol(), frame)
			if msg.ACC == nil || *msg.ACC != tc.wantACC {
				t.Fatalf("ACC: esperava %v, recebi %v", tc.wantACC, msg.ACC)
			}
			if msg.RelayOn == nil || *msg.RelayOn != tc.wantRelay {
				t.Fatalf("relé: esperava %v, recebi %v", tc.wantRelay, msg.RelayOn)
			}
		})
	}
}

func TestParseBattery(t *testing.T) {
	fx := gpsFixture{when: time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC),
		lat: -23.5, lon: -46.6, satellites: 7, valid: true}
	content := append(fx.content(), lbs()...)
	content = append(content, statusBlock(0x02, 6, 3)...)
	frame := buildFrame(msgGPSLBSStatus, content, 3)

	msg := mustParseOne(t, newProtocol(), frame)

	if msg.BatteryPercent == nil || *msg.BatteryPercent != 100 {
		t.Fatalf("bateria: esperava 100%%, recebi %v", msg.BatteryPercent)
	}
	// O GT06 informa nível relativo (0-6), não tensão: não podemos inventar volts (§34).
	if msg.BatteryVoltage != nil {
		t.Fatalf("BatteryVoltage deveria ficar vazio, recebi %v", *msg.BatteryVoltage)
	}
	if msg.GSMLevel == nil || *msg.GSMLevel != 3 {
		t.Fatalf("sinal GSM: esperava 3, recebi %v", msg.GSMLevel)
	}
}

func TestParseHeartbeat(t *testing.T) {
	// terminal info 0x42: GPS rastreando + ACC ligado.
	frame := buildFrame(msgHeartbeat, []byte{0x42, 0x04, 0x03, 0x00, 0x01}, 12)

	msg := mustParseOne(t, newProtocol(), frame)

	if msg.Kind != protocols.KindHeartbeat {
		t.Fatalf("esperava heartbeat, recebi %q", msg.Kind)
	}
	if msg.HasLocation {
		t.Fatal("heartbeat não carrega coordenada")
	}
	if msg.ACC == nil || !*msg.ACC {
		t.Fatalf("ACC: esperava ligado, recebi %v", msg.ACC)
	}
	if msg.BatteryPercent == nil || *msg.BatteryPercent != 66 {
		t.Fatalf("bateria: esperava 66%%, recebi %v", msg.BatteryPercent)
	}
	if len(msg.Ack) == 0 {
		t.Fatal("heartbeat exige ACK, senão o rastreador derruba a conexão")
	}
}

func TestParseStatusAlarm(t *testing.T) {
	// Bits 5-3 == 100 no terminal info sinalizam SOS.
	frame := buildFrame(msgHeartbeat, []byte{0x22, 0x06, 0x04, 0x00, 0x01}, 12)

	msg := mustParseOne(t, newProtocol(), frame)
	if msg.Kind != protocols.KindAlarm {
		t.Fatalf("esperava alarme, recebi %q", msg.Kind)
	}
	if msg.Alarm != "SOS" {
		t.Fatalf("esperava SOS, recebi %q", msg.Alarm)
	}
}

func TestParseAlarmPacket(t *testing.T) {
	fx := gpsFixture{when: time.Date(2026, 5, 5, 5, 5, 5, 0, time.UTC),
		lat: -23.5, lon: -46.6, satellites: 6, valid: true}
	content := append(fx.content(), lbs()...)
	content = append(content, statusBlock(0x02, 5, 4)...)
	content = append(content, 0x02, 0x01) // alarme: corte de energia + idioma
	frame := buildFrame(msgAlarmGPS, content, 21)

	msg := mustParseOne(t, newProtocol(), frame)
	if msg.Alarm != "POWER_LOSS" {
		t.Fatalf("esperava POWER_LOSS, recebi %q", msg.Alarm)
	}
	if len(msg.Ack) == 0 {
		t.Fatal("pacote de alarme exige ACK")
	}
}

// ---------------------------------------------------------------------------
// Comandos e ACK
// ---------------------------------------------------------------------------

func TestEncodeEngineCommands(t *testing.T) {
	p := newProtocol()

	tests := []struct {
		name     string
		cmd      protocols.Command
		wantText string
	}{
		{
			name:     "corte sem senha",
			cmd:      protocols.Command{Type: protocols.CommandEngineCut, CorrelationKey: 42},
			wantText: "DYD#",
		},
		{
			name:     "corte com senha",
			cmd:      protocols.Command{Type: protocols.CommandEngineCut, Password: "123456", CorrelationKey: 42},
			wantText: "DYD,123456#",
		},
		{
			name:     "liberação",
			cmd:      protocols.Command{Type: protocols.CommandEngineResume, CorrelationKey: 43},
			wantText: "HFYD#",
		},
		{
			name:     "posição",
			cmd:      protocols.Command{Type: protocols.CommandRequestPosition, CorrelationKey: 44},
			wantText: "WHERE#",
		},
		{
			name:     "status",
			cmd:      protocols.Command{Type: protocols.CommandRequestStatus, CorrelationKey: 45},
			wantText: "STATUS#",
		},
		{
			name: "override do dispositivo tem precedência",
			cmd: protocols.Command{Type: protocols.CommandEngineCut, Raw: "RELAY,1#",
				Password: "123456", CorrelationKey: 46},
			wantText: "RELAY,1#",
		},
		{
			name: "intervalo",
			cmd: protocols.Command{Type: protocols.CommandSetInterval, CorrelationKey: 47,
				Params: map[string]string{"seconds": "30"}},
			wantText: "TIMER,30#",
		},
		{
			name: "servidor",
			cmd: protocols.Command{Type: protocols.CommandSetServer, CorrelationKey: 48,
				Params: map[string]string{"host": "rastreador.exemplo.com", "port": "5000"}},
			wantText: "SERVER,1,rastreador.exemplo.com,5000,0#",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			frame, err := p.EncodeCommand(tc.cmd)
			if err != nil {
				t.Fatalf("EncodeCommand falhou: %v", err)
			}
			pkt, err := decodeFrame(frame)
			if err != nil {
				t.Fatalf("quadro gerado é inválido: %v", err)
			}
			if pkt.typ != msgCommand {
				t.Fatalf("esperava pacote 0x80, recebi 0x%02X", pkt.typ)
			}
			// conteúdo: tamanho (1) + server flag (4) + texto + idioma (2)
			gotFlag := binary.BigEndian.Uint32(pkt.content[1:5])
			if gotFlag != tc.cmd.CorrelationKey {
				t.Fatalf("server flag: esperava %d, recebi %d", tc.cmd.CorrelationKey, gotFlag)
			}
			gotText := string(pkt.content[5 : len(pkt.content)-2])
			if gotText != tc.wantText {
				t.Fatalf("texto: esperava %q, recebi %q", tc.wantText, gotText)
			}
			if int(pkt.content[0]) != len(tc.wantText)+4 {
				t.Fatalf("campo de tamanho incoerente: %d", pkt.content[0])
			}
		})
	}
}

func TestEncodeCommandRejectsInjection(t *testing.T) {
	p := newProtocol()

	bad := []protocols.Command{
		{Type: protocols.CommandEngineCut, Password: "123;RESET#"},
		{Type: protocols.CommandSetServer, Params: map[string]string{"host": "a b;c", "port": "5000"}},
		{Type: protocols.CommandSetInterval, Params: map[string]string{"seconds": "0"}},
		{Type: protocols.CommandSetInterval, Params: map[string]string{"seconds": "abc"}},
		{Type: protocols.CommandCustom},
		{Type: protocols.CommandEngineCut, Raw: "DYD\x00#"},
	}

	for i, cmd := range bad {
		if _, err := p.EncodeCommand(cmd); err == nil {
			t.Fatalf("caso %d: esperava recusa", i)
		}
	}
}

func TestParseEngineAck(t *testing.T) {
	const text = "DYD=Success!"
	content := []byte{byte(len(text) + 4), 0x00, 0x00, 0x30, 0x39} // correlação 12345
	content = append(content, text...)
	content = append(content, 0x00, 0x02)
	frame := buildFrame(msgStringInfo, content, 99)

	msg := mustParseOne(t, newProtocol(), frame)

	if msg.Kind != protocols.KindCommandAck {
		t.Fatalf("esperava command_ack, recebi %q", msg.Kind)
	}
	if msg.CorrelationKey != 12345 {
		t.Fatalf("correlação incorreta: %d", msg.CorrelationKey)
	}
	if msg.Response != text {
		t.Fatalf("resposta incorreta: %q", msg.Response)
	}
}

func TestEncodeDecodeCommandRoundTrip(t *testing.T) {
	p := newProtocol()
	frame, err := p.EncodeCommand(protocols.Command{
		Type: protocols.CommandEngineResume, CorrelationKey: 0xDEADBEEF,
	})
	if err != nil {
		t.Fatalf("EncodeCommand falhou: %v", err)
	}

	// O quadro gerado precisa ser enquadrável e validável pelo próprio parser.
	got, consumed, err := p.NextFrame(frame)
	if err != nil || consumed != len(frame) || len(got) != len(frame) {
		t.Fatalf("quadro gerado não passa no enquadramento: consumed=%d err=%v", consumed, err)
	}
}

// ---------------------------------------------------------------------------
// Enquadramento: 1 Read() != 1 pacote (§31)
// ---------------------------------------------------------------------------

func TestDetect(t *testing.T) {
	p := newProtocol()
	if !p.Detect([]byte{0x78, 0x78, 0x0D}) {
		t.Fatal("deveria reconhecer 7878")
	}
	if !p.Detect([]byte{0x79, 0x79, 0x00}) {
		t.Fatal("deveria reconhecer 7979")
	}
	if p.Detect([]byte("*HQ,86924706,V1,")) {
		t.Fatal("não deveria reconhecer protocolo de texto")
	}
	if p.Detect([]byte{0x78}) {
		t.Fatal("não deveria decidir com 1 byte")
	}
}

func TestNextFrameIncomplete(t *testing.T) {
	p := newProtocol()
	frame := buildFrame(msgLogin, mustHex(t, "0869247061234567"), 1)

	for cut := 1; cut < len(frame); cut++ {
		got, consumed, err := p.NextFrame(frame[:cut])
		if !errors.Is(err, protocols.ErrIncompleteFrame) {
			t.Fatalf("corte em %d: esperava ErrIncompleteFrame, recebi got=%v consumed=%d err=%v",
				cut, got, consumed, err)
		}
		if consumed != 0 {
			t.Fatalf("corte em %d: nada deveria ser consumido, consumiu %d", cut, consumed)
		}
	}
}

func TestNextFrameMultiplePacketsInOneRead(t *testing.T) {
	p := newProtocol()
	login := buildFrame(msgLogin, mustHex(t, "0869247061234567"), 1)
	heartbeat := buildFrame(msgHeartbeat, []byte{0x42, 0x04, 0x03, 0x00, 0x01}, 2)

	buf := append(append([]byte{}, login...), heartbeat...)

	first, consumed, err := p.NextFrame(buf)
	if err != nil {
		t.Fatalf("primeiro quadro: %v", err)
	}
	if consumed != len(login) || hexdump(first) != hexdump(login) {
		t.Fatalf("primeiro quadro incorreto: consumed=%d", consumed)
	}

	second, consumed, err := p.NextFrame(buf[consumed:])
	if err != nil {
		t.Fatalf("segundo quadro: %v", err)
	}
	if consumed != len(heartbeat) || hexdump(second) != hexdump(heartbeat) {
		t.Fatalf("segundo quadro incorreto: consumed=%d", consumed)
	}
}

func TestNextFrameSplitAcrossReads(t *testing.T) {
	p := newProtocol()
	frame := buildFrame(msgHeartbeat, []byte{0x42, 0x04, 0x03, 0x00, 0x01}, 5)

	// Simula o socket entregando 3 bytes por vez.
	var buf []byte
	var got []byte
	for i := 0; i < len(frame); i += 3 {
		end := min(i+3, len(frame))
		buf = append(buf, frame[i:end]...)

		f, consumed, err := p.NextFrame(buf)
		if errors.Is(err, protocols.ErrIncompleteFrame) {
			continue
		}
		if err != nil {
			t.Fatalf("erro inesperado: %v", err)
		}
		got = f
		buf = buf[consumed:]
	}

	if hexdump(got) != hexdump(frame) {
		t.Fatalf("quadro remontado difere do original: %s", hexdump(got))
	}
	if len(buf) != 0 {
		t.Fatalf("sobraram %d bytes no buffer", len(buf))
	}
}

func TestNextFrameSkipsGarbage(t *testing.T) {
	p := newProtocol()
	frame := buildFrame(msgHeartbeat, []byte{0x42, 0x04, 0x03, 0x00, 0x01}, 5)
	buf := append([]byte{0x00, 0xFF, 0xAB}, frame...)

	// O lixo é reportado como erro (para contabilizar pacote inválido) mas
	// consumido, de modo que a próxima chamada encontre o quadro real.
	_, consumed, err := p.NextFrame(buf)
	if err == nil {
		t.Fatal("esperava erro reportando o lixo")
	}
	if consumed != 3 {
		t.Fatalf("deveria consumir os 3 bytes de lixo, consumiu %d", consumed)
	}

	got, consumed, err := p.NextFrame(buf[consumed:])
	if err != nil {
		t.Fatalf("quadro após o lixo: %v", err)
	}
	if consumed != len(frame) || hexdump(got) != hexdump(frame) {
		t.Fatal("quadro após o lixo veio errado")
	}
}

func TestNextFrameRejectsBadStopBytes(t *testing.T) {
	p := newProtocol()
	frame := buildFrame(msgHeartbeat, []byte{0x42, 0x04, 0x03, 0x00, 0x01}, 5)
	broken := append([]byte(nil), frame...)
	broken[len(broken)-1] = 0x00

	_, consumed, err := p.NextFrame(broken)
	if err == nil {
		t.Fatal("esperava erro de bytes de parada")
	}
	if consumed != 2 {
		t.Fatalf("deveria consumir só o delimitador para ressincronizar, consumiu %d", consumed)
	}
}

func TestNextFrameRejectsOversizedLength(t *testing.T) {
	p := newProtocol()
	// 0x79 0x79 com tamanho declarado acima do limite.
	buf := []byte{0x79, 0x79, 0xFF, 0xFF, 0x01}

	_, consumed, err := p.NextFrame(buf)
	if err == nil {
		t.Fatal("esperava recusa de tamanho absurdo")
	}
	if consumed != 2 {
		t.Fatalf("deveria consumir só o delimitador, consumiu %d", consumed)
	}
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("hex inválido: %v", err)
	}
	return b
}

// longFrame monta o quadro de 2 bytes de tamanho (79 79), o do 0x94.
func longFrame(typ byte, content []byte, serial uint16) []byte {
	length := 1 + len(content) + 2 + 2
	out := []byte{startLong, startLong}
	out = binary.BigEndian.AppendUint16(out, uint16(length))
	out = append(out, typ)
	out = append(out, content...)
	out = binary.BigEndian.AppendUint16(out, serial)
	out = binary.BigEndian.AppendUint16(out, crcITU(out[2:]))
	return append(out, stopCR, stopLF)
}

// O 0x94 do tipo 0x00 traz a tensão da bateria do veículo (centésimos de
// volt); os outros tipos ficam crus. Nenhum pede resposta.
func TestParseExternalVoltage(t *testing.T) {
	msg := mustParseOne(t, newProtocol(), longFrame(msgInfo, []byte{0x00, 0x04, 0xD3}, 21))
	if msg.BatteryVoltage == nil || math.Abs(*msg.BatteryVoltage-12.35) > 1e-9 || msg.Kind != protocols.KindStatus || msg.Ack != nil {
		t.Fatalf("tensão externa = %+v (kind %s, ack %v)", msg.BatteryVoltage, msg.Kind, msg.Ack)
	}
	other := mustParseOne(t, newProtocol(), longFrame(msgInfo, []byte{0x05, 0x01}, 22))
	if other.BatteryVoltage != nil || other.Attributes["unhandled"] != true || other.Attributes["infoType"] != "0x05" {
		t.Fatalf("outro tipo de informação = %+v", other)
	}
}
