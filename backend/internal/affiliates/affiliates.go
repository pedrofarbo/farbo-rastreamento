// Package affiliates é o programa de afiliados (influenciadores). O admin
// cria cada afiliado com o link de cadastro (/indicacao/<code>) e a comissão
// por veículo ativo (um valor fixo ou um percentual da mensalidade); quem se
// cadastra pelo link fica com o afiliado e, ao virar
// cliente, rende a comissão por veículo, em cada mês em que a mensalidade
// dele foi paga (dois veículos, duas comissões). No
// fechamento do mês, as comissões de cada afiliado viram uma conta a pagar na
// Empresa; o afiliado acompanha os números por uma página com link secreto.
package affiliates

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/installers"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/orders"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/vehicles"
)

// MaxCommissionCents: R$ 1.000 por veículo por mês barra um zero a mais.
const MaxCommissionCents = 100_000

// Os tipos de comissão: um valor fixo por veículo ou um percentual da
// mensalidade paga do veículo (sem a parcela do rastreador).
const (
	CommissionFixed   = "fixed"
	CommissionPercent = "percent"
)

// MaxCommissionBps: o percentual vai em centésimos de ponto (1000 = 10%), até
// a mensalidade inteira.
const MaxCommissionBps = 10_000

// ValidationError é um dado recusado; a mensagem vai para a tela.
type ValidationError struct{ Message string }

func (e ValidationError) Error() string { return e.Message }

func invalid(format string, args ...any) error {
	return ValidationError{Message: fmt.Sprintf(format, args...)}
}

// Affiliate é um afiliado, com os números do programa.
type Affiliate struct {
	ID     uuid.UUID `json:"id"`
	Name   string    `json:"name"`
	Handle string    `json:"handle"`
	Code   string    `json:"code"`
	// ReportToken abre a página do afiliado (/parceiro/<token>).
	ReportToken string `json:"reportToken"`
	// CommissionType: fixed (CommissionCents por veículo) ou percent
	// (CommissionBps da mensalidade paga de cada veículo).
	CommissionType  string `json:"commissionType"`
	CommissionCents int    `json:"commissionCents"`
	CommissionBps   int    `json:"commissionBps"`
	// PlanPrices: a mensalidade de quem ele indica, por tipo de veículo (nil:
	// o preço normal do site).
	PlanPrices *vehicles.KindPrices `json:"planPrices"`
	// InstallPrices: a instalação e a desinstalação de quem ele indica (nil:
	// as da Farbo); no modo direto, o representante recebe do cliente.
	InstallPrices *orders.InstallPrices `json:"installPrices"`
	Email         string                `json:"email"`
	Phone         string                `json:"phone"`
	PixKey        string                `json:"pixKey"`
	Notes         string                `json:"notes"`
	SupplierID    *uuid.UUID            `json:"supplierId"`
	Active        bool                  `json:"active"`
	CreatedAt     time.Time             `json:"createdAt"`

	// Signups: pessoas que se cadastraram pelo link (lista ou pré-cadastro).
	Signups int `json:"signups"`
	// Customers: clientes indicados; ActiveCustomers: com assinatura ativa.
	Customers       int `json:"customers"`
	ActiveCustomers int `json:"activeCustomers"`
	// ActiveVehicles: as assinaturas ativas (veículos) dos clientes
	// indicados — cada uma rende a comissão no mês em que é paga.
	ActiveVehicles int `json:"activeVehicles"`
	// PendingCents: ainda não fechado (ou, com o split, ainda não pago);
	// OpenCents: fechado, a pagar; PaidCents: pago no fechamento;
	// SplitPaidCents: pago pelo split, no pagamento do cliente.
	PendingCents   int64 `json:"pendingCents"`
	OpenCents      int64 `json:"openCents"`
	PaidCents      int64 `json:"paidCents"`
	SplitPaidCents int64 `json:"splitPaidCents"`
	// RecipientStatus: a situação do recebedor na Pagar.me ("" sem cadastro).
	RecipientStatus string `json:"recipientStatus"`
	// ReportPassword: a página dele pede senha (a senha não sai daqui).
	ReportPassword bool `json:"reportPassword"`
	// EquipmentModes: o rastreador pelo link dele — vazio, comprado e
	// comodato; sale, só comprado; loan, só comodato.
	EquipmentModes string `json:"equipmentModes"`
	// ManageInstalls: ele cuida da instalação e da desinstalação dos clientes
	// que indicou, pela página dele (que então pede senha).
	ManageInstalls bool `json:"manageInstalls"`
	// HandDelivery: ele entrega o rastreador em mãos (com os kits dele) a
	// quem contrata pelo link, sem frete.
	HandDelivery bool `json:"handDelivery"`
	// Listed: aparece na landing como representante oficial autorizado, com a
	// cidade, a região que atende e o WhatsApp público (só dígitos, com o 55).
	Listed          bool   `json:"listed"`
	ListingCity     string `json:"listingCity"`
	ListingArea     string `json:"listingArea"`
	ListingWhatsApp string `json:"listingWhatsapp"`
	// Recurrence: o semestral e o anual pelo link dele (pagos por período,
	// com o rastreador incluso), ligados ou não, e os valores.
	Recurrence orders.Recurrence `json:"recurrence"`
}

// Input é o cadastro de um afiliado. Sem CommissionType, o afiliado novo fica
// no valor fixo e o que já existe mantém o tipo; sem CommissionCents, vale o
// padrão. Sem PlanPrices, quem ele indica paga o preço normal do site; sem
// InstallPrices, a instalação e a desinstalação da Farbo.
type Input struct {
	Name            string                `json:"name"`
	Handle          string                `json:"handle"`
	Code            string                `json:"code"`
	CommissionType  string                `json:"commissionType"`
	CommissionCents *int                  `json:"commissionCents"`
	CommissionBps   *int                  `json:"commissionBps"`
	PlanPrices      *vehicles.KindPrices  `json:"planPrices"`
	InstallPrices   *orders.InstallPrices `json:"installPrices"`
	Email           string                `json:"email"`
	Phone           string                `json:"phone"`
	PixKey          string                `json:"pixKey"`
	Notes           string                `json:"notes"`
	Active          bool                  `json:"active"`
	// ReportPassword: a senha da página do afiliado — nil não muda, vazia
	// tira a senha, preenchida define (ou troca).
	ReportPassword *string `json:"reportPassword"`
	EquipmentModes string  `json:"equipmentModes"`
	// ManageInstalls pede a senha da página (ela mostra os dados dos clientes).
	ManageInstalls bool `json:"manageInstalls"`
	HandDelivery   bool `json:"handDelivery"`
	// Listed pede a cidade; o WhatsApp público é opcional.
	Listed          bool   `json:"listed"`
	ListingCity     string `json:"listingCity"`
	ListingArea     string `json:"listingArea"`
	ListingWhatsApp string `json:"listingWhatsapp"`
	// Recurrence: o semestral e o anual pelo link (nil: no novo, desligado
	// com os valores padrão; na edição, ficam como estão).
	Recurrence *orders.Recurrence `json:"recurrence"`
}

var handlePattern = regexp.MustCompile(`^[A-Za-z0-9._]{1,30}$`)

// Normalize confere o cadastro. O código, se vier vazio, sai do @ (ou do nome).
func (in Input) Normalize() (Input, error) {
	in.Name = strings.Join(strings.Fields(in.Name), " ")
	in.Handle = strings.TrimPrefix(strings.TrimSpace(in.Handle), "@")
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	in.Phone = strings.TrimSpace(in.Phone)
	in.PixKey = strings.TrimSpace(in.PixKey)
	in.Notes = strings.TrimSpace(in.Notes)
	in.ListingCity = strings.Join(strings.Fields(in.ListingCity), " ")
	in.ListingArea = strings.Join(strings.Fields(in.ListingArea), " ")
	in.ListingWhatsApp = strings.TrimSpace(in.ListingWhatsApp)
	if strings.TrimSpace(in.Code) == "" {
		in.Code = in.Handle
		if in.Code == "" {
			in.Code = in.Name
		}
	}
	in.Code = Code(in.Code)
	switch {
	case in.Name == "" || utf8.RuneCountInString(in.Name) > 120:
		return in, invalid("Dê o nome do afiliado.")
	case in.Handle != "" && !handlePattern.MatchString(in.Handle):
		return in, invalid("O @ do Instagram tem só letras, números, ponto e sublinhado (até 30).")
	case len(in.Code) < 2:
		return in, invalid("O código do link precisa de pelo menos 2 letras ou números.")
	case in.CommissionType != "" && in.CommissionType != CommissionFixed && in.CommissionType != CommissionPercent:
		return in, invalid("Escolha a comissão: valor fixo ou percentual.")
	case in.CommissionCents != nil && (*in.CommissionCents < 0 || *in.CommissionCents > MaxCommissionCents):
		return in, invalid("Valor da comissão inválido.")
	case in.CommissionType == CommissionPercent && (in.CommissionBps == nil || *in.CommissionBps <= 0 || *in.CommissionBps > MaxCommissionBps):
		return in, invalid("O percentual da comissão vai de 0,01%% a 100%%.")
	case in.CommissionBps != nil && (*in.CommissionBps < 0 || *in.CommissionBps > MaxCommissionBps):
		return in, invalid("Percentual da comissão inválido.")
	case in.PlanPrices != nil && !in.PlanPrices.Valid():
		return in, invalid("Preencha a mensalidade da moto, do carro e do caminhão.")
	case in.InstallPrices != nil && !in.InstallPrices.Valid():
		return in, invalid("Preencha o preço da instalação e o da desinstalação.")
	case in.EquipmentModes != "" && in.EquipmentModes != orders.EquipmentSaleOnly && in.EquipmentModes != orders.EquipmentLoanOnly:
		return in, invalid("Escolha o rastreador pelo link: comprado, comodato ou os dois.")
	case in.Email != "":
		if _, err := mail.ParseAddress(in.Email); err != nil {
			return in, invalid("E-mail inválido.")
		}
	}
	if utf8.RuneCountInString(in.Phone) > 40 || utf8.RuneCountInString(in.PixKey) > 140 || utf8.RuneCountInString(in.Notes) > 2000 {
		return in, invalid("Telefone, chave Pix ou observações longos demais.")
	}
	if in.ReportPassword != nil && *in.ReportPassword != "" {
		if err := validReportPassword(*in.ReportPassword); err != nil {
			return in, err
		}
	}
	switch {
	case in.Listed && in.ListingCity == "":
		return in, invalid("Para aparecer na landing, diga a cidade do representante.")
	case utf8.RuneCountInString(in.ListingCity) > 80 || utf8.RuneCountInString(in.ListingArea) > 200:
		return in, invalid("A cidade (até 80) ou a região que atende (até 200) longas demais.")
	}
	if in.ListingWhatsApp != "" {
		phone, ok := installers.NormalizeWhatsApp(in.ListingWhatsApp)
		if !ok {
			return in, invalid("WhatsApp da landing inválido: informe DDD e número, ex.: (11) 99999-0000.")
		}
		in.ListingWhatsApp = phone
	}
	if r := in.Recurrence; r != nil {
		const max = 100_000_00
		switch {
		case r.SemiannualCents < 0 || r.SemiannualCents > max || r.AnnualCents < 0 || r.AnnualCents > max:
			return in, invalid("Os valores do semestral e do anual vão de R$ 0 a R$ 100.000.")
		case r.Enabled && (r.SemiannualCents == 0 || r.AnnualCents == 0):
			return in, invalid("Para ligar o semestral e o anual, informe os dois valores.")
		case r.Enabled && in.EquipmentModes == orders.EquipmentLoanOnly:
			return in, invalid("O semestral e o anual são com o rastreador incluso: o link só de comodato não os oferece.")
		}
	}
	return in, nil
}

// maxCode: o código no link.
const maxCode = 40

// Code deixa o código no formato do link: minúsculo, sem acento, com hífen
// ("Fulano da Silva" → "fulano-da-silva"; "@fulano.moto" → "fulano-moto").
func Code(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		if plain, ok := unaccent[r]; ok {
			r = plain
		}
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r)
			dash = false
		} else {
			dash = true
		}
		if b.Len() >= maxCode {
			break
		}
	}
	out := b.String()
	if len(out) > maxCode {
		out = out[:maxCode]
	}
	return strings.TrimRight(out, "-")
}

var unaccent = map[rune]rune{
	'á': 'a', 'à': 'a', 'â': 'a', 'ã': 'a', 'ä': 'a', 'é': 'e', 'è': 'e', 'ê': 'e', 'ë': 'e',
	'í': 'i', 'ì': 'i', 'î': 'i', 'ï': 'i', 'ó': 'o', 'ò': 'o', 'ô': 'o', 'õ': 'o', 'ö': 'o',
	'ú': 'u', 'ù': 'u', 'û': 'u', 'ü': 'u', 'ç': 'c', 'ñ': 'n',
}

// newToken é o segredo da página do afiliado: 144 bits, seguro no link.
func newToken() (string, error) {
	raw := make([]byte, 18)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

var months = [...]string{"jan", "fev", "mar", "abr", "mai", "jun", "jul", "ago", "set", "out", "nov", "dez"}

// monthLabel: 2026-10-01 → "out/2026".
func monthLabel(t time.Time) string {
	return fmt.Sprintf("%s/%d", months[t.Month()-1], t.Year())
}
