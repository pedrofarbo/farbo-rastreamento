// Package affiliates é o programa de afiliados (influenciadores). O admin
// cria cada afiliado com o link de cadastro (/indicacao/<code>) e o valor por
// cliente indicado; quem se cadastra pelo link fica com o afiliado e, ao
// virar cliente, rende a comissão em cada mês em que pagou a mensalidade. No
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
)

// MaxCommissionCents: R$ 1.000 por cliente por mês barra um zero a mais.
const MaxCommissionCents = 100_000

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
	ReportToken     string     `json:"reportToken"`
	CommissionCents int        `json:"commissionCents"`
	Email           string     `json:"email"`
	Phone           string     `json:"phone"`
	PixKey          string     `json:"pixKey"`
	Notes           string     `json:"notes"`
	SupplierID      *uuid.UUID `json:"supplierId"`
	Active          bool       `json:"active"`
	CreatedAt       time.Time  `json:"createdAt"`

	// Signups: pessoas que se cadastraram pelo link (lista ou pré-cadastro).
	Signups int `json:"signups"`
	// Customers: clientes indicados; ActiveCustomers: com assinatura ativa.
	Customers       int `json:"customers"`
	ActiveCustomers int `json:"activeCustomers"`
	// PendingCents: ainda não fechado; OpenCents: fechado, a pagar; PaidCents: pago.
	PendingCents int64 `json:"pendingCents"`
	OpenCents    int64 `json:"openCents"`
	PaidCents    int64 `json:"paidCents"`
}

// Input é o cadastro de um afiliado. Sem CommissionCents, vale o padrão.
type Input struct {
	Name            string `json:"name"`
	Handle          string `json:"handle"`
	Code            string `json:"code"`
	CommissionCents *int   `json:"commissionCents"`
	Email           string `json:"email"`
	Phone           string `json:"phone"`
	PixKey          string `json:"pixKey"`
	Notes           string `json:"notes"`
	Active          bool   `json:"active"`
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
	case in.CommissionCents != nil && (*in.CommissionCents < 0 || *in.CommissionCents > MaxCommissionCents):
		return in, invalid("Valor da comissão inválido.")
	case in.Email != "":
		if _, err := mail.ParseAddress(in.Email); err != nil {
			return in, invalid("E-mail inválido.")
		}
	}
	if utf8.RuneCountInString(in.Phone) > 40 || utf8.RuneCountInString(in.PixKey) > 140 || utf8.RuneCountInString(in.Notes) > 2000 {
		return in, invalid("Telefone, chave Pix ou observações longos demais.")
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
