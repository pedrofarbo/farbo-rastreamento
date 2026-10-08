// Package devices trata o cadastro dos rastreadores e o seu estado de conexão.
package devices

import (
	"time"

	"github.com/google/uuid"
)

// Status de conexão do dispositivo (§17). Derivado do último pacote recebido,
// não apenas da existência de um socket aberto.
const (
	StatusOnline  = "ONLINE"
	StatusStale   = "STALE"
	StatusOffline = "OFFLINE"
)

type Device struct {
	ID           uuid.UUID `json:"id"`
	IMEI         string    `json:"imei"`
	Model        string    `json:"model"`
	Manufacturer string    `json:"manufacturer"`
	Protocol     string    `json:"protocol"`
	Firmware     string    `json:"firmware"`
	PhoneNumber  string    `json:"phoneNumber"`
	// ICCID é o número de série do chip (só dígitos; vazio se não informado).
	ICCID string `json:"iccid"`

	Status     string     `json:"status"`
	LastSeenAt *time.Time `json:"lastSeenAt"`

	// Provisionamento do aparelho (§30).
	APN     string `json:"apn"`
	APNUser string `json:"apnUser"`
	// APNPassword é só de escrita: nunca sai da API (ver View).
	APNPassword              string `json:"-"`
	ServerHost               string `json:"serverHost"`
	ServerPort               *int   `json:"serverPort"`
	ReportIntervalSeconds    *int   `json:"reportIntervalSeconds"`
	HeartbeatIntervalSeconds *int   `json:"heartbeatIntervalSeconds"`

	// CommandPassword é exigida por alguns firmwares ao receber comandos.
	// Também só de escrita.
	CommandPassword string `json:"-"`
	// CommandOverrides substitui o texto padrão de um comando, por tipo.
	// É o escape para firmware divergente sem recompilar nada. Pode carregar
	// a senha no meio do texto, por isso só sai redigido (ver View).
	CommandOverrides map[string]string `json:"-"`

	Notes string `json:"notes"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Audience diz para quem o rastreador vai ser mostrado.
type Audience int

const (
	// AudienceAdmin: o cadastro inteiro, menos as senhas (só indicadores) e
	// com os textos de comando redigidos.
	AudienceAdmin Audience = iota
	// AudienceStaff: operador e visualizador. Identificação, situação,
	// linha do chip e anotações; nada de credencial nem de configuração do
	// aparelho.
	AudienceStaff
	// AudienceCustomer: o cliente final vê só identificação e situação.
	AudienceCustomer
)

// View é o rastreador como sai da API. Os campos são copiados um a um de
// Device (lista fechada): um campo novo no cadastro não aparece em resposta
// nenhuma sem alguém decidir quem pode vê-lo.
//
// As senhas não existem aqui, para nenhum perfil — nem para o admin. No lugar
// delas vão os indicadores APNPasswordSet e CommandPasswordSet, só para o
// admin. Os campos que um perfil não vê chegam vazios.
type View struct {
	ID           uuid.UUID `json:"id"`
	IMEI         string    `json:"imei"`
	Model        string    `json:"model"`
	Manufacturer string    `json:"manufacturer"`
	Protocol     string    `json:"protocol"`
	Firmware     string    `json:"firmware"`
	PhoneNumber  string    `json:"phoneNumber"`
	ICCID        string    `json:"iccid"`

	Status     string     `json:"status"`
	LastSeenAt *time.Time `json:"lastSeenAt"`

	APN                      string `json:"apn"`
	APNUser                  string `json:"apnUser"`
	APNPasswordSet           bool   `json:"apnPasswordSet,omitempty"`
	ServerHost               string `json:"serverHost"`
	ServerPort               *int   `json:"serverPort"`
	ReportIntervalSeconds    *int   `json:"reportIntervalSeconds"`
	HeartbeatIntervalSeconds *int   `json:"heartbeatIntervalSeconds"`

	CommandPasswordSet bool              `json:"commandPasswordSet,omitempty"`
	CommandOverrides   map[string]string `json:"commandOverrides"`

	Notes string `json:"notes"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// View monta a representação pública do rastreador para o perfil informado.
func (d *Device) View(audience Audience) *View {
	if d == nil {
		return nil
	}
	v := &View{
		ID:               d.ID,
		IMEI:             d.IMEI,
		Model:            d.Model,
		Manufacturer:     d.Manufacturer,
		Protocol:         d.Protocol,
		Firmware:         d.Firmware,
		Status:           d.Status,
		LastSeenAt:       d.LastSeenAt,
		CommandOverrides: map[string]string{},
		CreatedAt:        d.CreatedAt,
		UpdatedAt:        d.UpdatedAt,
	}
	if audience == AudienceCustomer {
		return v
	}

	v.PhoneNumber = d.PhoneNumber
	v.ICCID = d.ICCID
	v.Notes = d.Notes
	if audience != AudienceAdmin {
		return v
	}

	v.APN = d.APN
	v.APNUser = d.APNUser
	v.APNPasswordSet = d.APNPassword != ""
	v.ServerHost = d.ServerHost
	v.ServerPort = d.ServerPort
	v.ReportIntervalSeconds = d.ReportIntervalSeconds
	v.HeartbeatIntervalSeconds = d.HeartbeatIntervalSeconds
	v.CommandPasswordSet = d.CommandPassword != ""
	secrets := d.Secrets()
	for cmdType, text := range d.CommandOverrides {
		v.CommandOverrides[cmdType] = RedactText(text, secrets)
	}
	return v
}

// Views aplica View a uma lista.
func Views(list []*Device, audience Audience) []*View {
	out := make([]*View, 0, len(list))
	for _, d := range list {
		out = append(out, d.View(audience))
	}
	return out
}

// Input é o payload aceito na criação/edição de um dispositivo.
//
// As senhas são só de escrita: na edição, campo vazio (ou ausente) mantém a
// senha atual — a tela nunca a recebe para devolver — e ClearAPNPassword /
// ClearCommandPassword apagam. CommandOverrides ausente (nulo) também mantém
// os textos atuais; um objeto vazio apaga todos.
type Input struct {
	IMEI                     string            `json:"imei"`
	Model                    string            `json:"model"`
	Manufacturer             string            `json:"manufacturer"`
	Protocol                 string            `json:"protocol"`
	Firmware                 string            `json:"firmware"`
	PhoneNumber              string            `json:"phoneNumber"`
	ICCID                    string            `json:"iccid"`
	APN                      string            `json:"apn"`
	APNUser                  string            `json:"apnUser"`
	APNPassword              string            `json:"apnPassword"`
	ClearAPNPassword         bool              `json:"clearApnPassword"`
	ServerHost               string            `json:"serverHost"`
	ServerPort               *int              `json:"serverPort"`
	ReportIntervalSeconds    *int              `json:"reportIntervalSeconds"`
	HeartbeatIntervalSeconds *int              `json:"heartbeatIntervalSeconds"`
	CommandPassword          string            `json:"commandPassword"`
	ClearCommandPassword     bool              `json:"clearCommandPassword"`
	CommandOverrides         map[string]string `json:"commandOverrides"`
	Notes                    string            `json:"notes"`
}

// StatusChange descreve uma transição detectada pela varredura de status.
type StatusChange struct {
	DeviceID uuid.UUID
	IMEI     string
	From     string
	To       string
}
