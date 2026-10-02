// Package alerts transforma eventos e posições dos rastreadores em alertas
// por e-mail.
//
// "Inteligente" quer dizer que ele filtra o que não merece e-mail:
//   - evento velho (o rastreador descarregando o que guardou sem sinal) não
//     vira alerta;
//   - o mesmo alerta, do mesmo veículo, para o mesmo destinatário, sai no
//     máximo uma vez a cada intervalo (ALERTS_COOLDOWN); os repetidos viram
//     contagem no próximo e-mail;
//   - cada destinatário tem um teto de e-mails por hora;
//   - ignição só incomoda no horário de vigilância escolhido pelo cliente;
//   - reboque exige deslocamento real a partir de onde o veículo estacionou,
//     não o ruído do GPS parado;
//   - sem sinal com o veículo em movimento avisa na hora; parado, só depois
//     de horas (garagem subterrânea não é alarme).
//
// Nada disso roda no caminho da ingestão: os ganchos só enfileiram, e o
// envio é assíncrono.
package alerts

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Tipos de alerta configuráveis pelo cliente.
const (
	KindSOS           = "SOS"
	KindPowerCut      = "POWER_CUT"
	KindTowing        = "TOWING"
	KindIgnitionGuard = "IGNITION_GUARD"
	KindOverspeed     = "OVERSPEED"
	KindSignalLost    = "SIGNAL_LOST"
	KindLowBattery    = "LOW_BATTERY"
	KindEngineBlock   = "ENGINE_BLOCK"
	KindIgnition      = "IGNITION"
)

// KindTest é o e-mail de teste pedido pela tela de alertas (não configurável).
const KindTest = "TEST"

// KindGeofence é a entrada ou saída de uma cerca do cliente. Não entra no
// catálogo: quem escolhe avisar (e de que lado) é a própria cerca. No
// histórico vira GEOFENCE_ENTER ou GEOFENCE_EXIT.
const KindGeofence = "GEOFENCE"

// KindInfo descreve um tipo de alerta para a tela de configuração.
type KindInfo struct {
	Kind        string `json:"kind"`
	Label       string `json:"label"`
	Description string `json:"description"`
	// Security: alerta de segurança. Também vai para a central
	// (ALERTS_CENTRAL_EMAILS), qualquer que seja o dono do veículo.
	Security bool `json:"security"`
	// Default: ligado para quem nunca mexeu nas preferências.
	Default bool `json:"default"`
}

// Catalog é a lista, na ordem em que a tela mostra.
var Catalog = []KindInfo{
	{KindSOS, "Botão de pânico (SOS)", "O botão SOS do rastreador foi acionado.", true, true},
	{KindPowerCut, "Bateria do veículo desconectada",
		"O rastreador deixou de receber energia do veículo: pode ser manutenção ou alguém tentando desligá-lo.", true, true},
	{KindTowing, "Movimento com a ignição desligada",
		"O veículo saiu do lugar onde estacionou sem a ignição ligada: possível reboque ou furto.", true, true},
	{KindIgnitionGuard, "Ignição ligada no horário de vigilância",
		"A ignição foi ligada dentro do horário que você definiu (por exemplo, de madrugada).", false, true},
	{KindOverspeed, "Excesso de velocidade",
		"O veículo passou do limite de velocidade cadastrado para ele.", false, true},
	{KindSignalLost, "Rastreador sem sinal",
		"O rastreador parou de se comunicar: na hora, se o veículo estava em movimento; depois de algumas horas, se estava parado.", false, true},
	{KindLowBattery, "Bateria do rastreador fraca",
		"A bateria interna do rastreador está acabando (costuma vir depois de ele ficar sem a energia do veículo).", false, true},
	{KindEngineBlock, "Bloqueio e desbloqueio do motor",
		"O rastreador confirmou que o motor foi bloqueado ou liberado.", false, true},
	{KindIgnition, "Ignição ligada (qualquer horário)",
		"Toda vez que a ignição for ligada, fora do horário de vigilância também. Pode gerar muitos e-mails.", false, false},
}

// Info devolve a descrição de um tipo configurável.
func Info(kind string) (KindInfo, bool) {
	i := slices.IndexFunc(Catalog, func(k KindInfo) bool { return k.Kind == kind })
	if i < 0 {
		return KindInfo{}, false
	}
	return Catalog[i], true
}

func isSecurity(kind string) bool {
	info, ok := Info(kind)
	return ok && info.Security
}

// Horário de vigilância padrão: das 22:00 às 06:00.
const (
	DefaultGuardStart = 22 * 60
	DefaultGuardEnd   = 6 * 60
)

// Settings são as escolhas de um usuário.
type Settings struct {
	// Kinds ligados. Ausente = desligado.
	Kinds map[string]bool
	// GuardStart/GuardEnd em minutos desde a meia-noite.
	GuardStart int
	GuardEnd   int
	// Custom: o usuário já salvou preferências (senão valem os padrões).
	Custom bool
}

// DefaultSettings valem para quem nunca abriu a tela de alertas.
func DefaultSettings() Settings {
	s := Settings{Kinds: map[string]bool{}, GuardStart: DefaultGuardStart, GuardEnd: DefaultGuardEnd}
	for _, k := range Catalog {
		if k.Default {
			s.Kinds[k.Kind] = true
		}
	}
	return s
}

// Enabled diz se o usuário quer receber o tipo.
func (s Settings) Enabled(kind string) bool { return s.Kinds[kind] }

// EnabledList devolve os tipos ligados, na ordem do catálogo.
func (s Settings) EnabledList() []string {
	out := []string{}
	for _, k := range Catalog {
		if s.Kinds[k.Kind] {
			out = append(out, k.Kind)
		}
	}
	return out
}

// InGuard diz se o minuto do dia cai no horário de vigilância. Início maior
// que o fim atravessa a meia-noite (22:00–06:00).
func (s Settings) InGuard(minuteOfDay int) bool {
	if s.GuardStart < s.GuardEnd {
		return minuteOfDay >= s.GuardStart && minuteOfDay < s.GuardEnd
	}
	return minuteOfDay >= s.GuardStart || minuteOfDay < s.GuardEnd
}

// NewSettings valida o que veio da tela.
func NewSettings(kinds []string, guardStart, guardEnd string) (Settings, error) {
	s := Settings{Kinds: map[string]bool{}, Custom: true}
	for _, kind := range kinds {
		if _, ok := Info(kind); !ok {
			return Settings{}, fmt.Errorf("tipo de alerta desconhecido: %q", kind)
		}
		s.Kinds[kind] = true
	}
	var err error
	if s.GuardStart, err = ParseClock(guardStart); err != nil {
		return Settings{}, fmt.Errorf("início do horário de vigilância: %w", err)
	}
	if s.GuardEnd, err = ParseClock(guardEnd); err != nil {
		return Settings{}, fmt.Errorf("fim do horário de vigilância: %w", err)
	}
	if s.GuardStart == s.GuardEnd {
		return Settings{}, fmt.Errorf("o horário de vigilância precisa ter início e fim diferentes")
	}
	return s, nil
}

// ParseClock lê "HH:MM" em minutos desde a meia-noite.
func ParseClock(v string) (int, error) {
	h, m, ok := strings.Cut(strings.TrimSpace(v), ":")
	if !ok || len(h) == 0 || len(h) > 2 || len(m) != 2 {
		return 0, fmt.Errorf("use o formato HH:MM")
	}
	hh, err1 := strconv.Atoi(h)
	mm, err2 := strconv.Atoi(m)
	if err1 != nil || err2 != nil || hh < 0 || hh > 23 || mm < 0 || mm > 59 {
		return 0, fmt.Errorf("use um horário entre 00:00 e 23:59")
	}
	return hh*60 + mm, nil
}

// FormatClock escreve minutos desde a meia-noite como "HH:MM".
func FormatClock(minutes int) string {
	return fmt.Sprintf("%02d:%02d", minutes/60, minutes%60)
}
