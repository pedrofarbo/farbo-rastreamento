package finance

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/payments"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/payments/abacatepay"
)

// As chaves Pix dos fornecedores e o Pix copia-e-cola das contas, como a
// AbacatePay recebe no envio.

var (
	randomKeyPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	cpfPattern       = regexp.MustCompile(`^\d{3}\.\d{3}\.\d{3}-\d{2}$`)
	cnpjPattern      = regexp.MustCompile(`^\d{2}\.\d{3}\.\d{3}/\d{4}-\d{2}$`)
	phonePattern     = regexp.MustCompile(`^\+?[\d\s().-]+$`)
)

// DetectKeyType deduz o tipo da chave pelo formato. Vazio quando não dá para
// saber: 11 dígitos soltos podem ser CPF ou celular.
func DetectKeyType(key string) string {
	key = strings.TrimSpace(key)
	d := digits(key)
	switch {
	case key == "":
		return ""
	case strings.Contains(key, "@"):
		return abacatepay.KeyEmail
	case randomKeyPattern.MatchString(key):
		return abacatepay.KeyRandom
	case cpfPattern.MatchString(key):
		return abacatepay.KeyCPF
	case cnpjPattern.MatchString(key), d == key && len(d) == 14:
		return abacatepay.KeyCNPJ
	case strings.HasPrefix(key, "+"), strings.ContainsAny(key, "() "), d == key && len(d) == 10:
		if phonePattern.MatchString(key) {
			return abacatepay.KeyPhone
		}
	}
	return ""
}

// PixKeyFor confere a chave no tipo dito e a devolve como vai para a
// AbacatePay (CPF e CNPJ só os dígitos; celular com DDD, sem o 55).
func PixKeyFor(key, keyType string) (string, error) {
	key = strings.TrimSpace(key)
	d := digits(key)
	switch keyType {
	case abacatepay.KeyCPF:
		if !payments.ValidCPF(d) {
			return "", invalid("A chave Pix não é um CPF válido.")
		}
		return d, nil
	case abacatepay.KeyCNPJ:
		if !payments.ValidCNPJ(d) {
			return "", invalid("A chave Pix não é um CNPJ válido.")
		}
		return d, nil
	case abacatepay.KeyPhone:
		if !phonePattern.MatchString(key) {
			return "", invalid("A chave Pix não é um celular válido.")
		}
		if (len(d) == 12 || len(d) == 13) && strings.HasPrefix(d, "55") {
			d = d[2:]
		}
		if len(d) != 10 && len(d) != 11 {
			return "", invalid("O celular da chave Pix precisa do DDD (10 ou 11 dígitos).")
		}
		return d, nil
	case abacatepay.KeyEmail:
		email := strings.ToLower(key)
		if at := strings.Index(email, "@"); at < 1 || at == len(email)-1 || len(email) > 77 || strings.ContainsAny(email, " ") {
			return "", invalid("A chave Pix não é um e-mail válido.")
		}
		return email, nil
	case abacatepay.KeyRandom:
		if !randomKeyPattern.MatchString(key) {
			return "", invalid("A chave aleatória tem o formato 123e4567-e89b-12d3-a456-426614174000.")
		}
		return strings.ToLower(key), nil
	}
	return "", invalid("Tipo de chave Pix inválido.")
}

// BRCode é o Pix copia-e-cola de uma conta (o QR Code do boleto ou da
// cobrança do fornecedor).
type BRCode struct {
	// Name é o recebedor como está no código.
	Name string
	City string
	// AmountCents: o valor fixo do código; 0 se quem paga escolhe o valor.
	AmountCents int64
}

// ParseBRCode lê um Pix copia-e-cola. ok é false se não for um (uma linha
// digitável de boleto, por exemplo) ou se o código estiver cortado (o CRC
// não confere).
func ParseBRCode(code string) (*BRCode, bool) {
	code = strings.TrimSpace(code)
	if !strings.HasPrefix(code, "000201") || !strings.Contains(strings.ToLower(code), "br.gov.bcb.pix") || len(code) < 30 {
		return nil, false
	}
	// O CRC (campo 63) fecha o código: os 4 últimos caracteres.
	if !strings.HasSuffix(code[:len(code)-4], "6304") ||
		!strings.EqualFold(code[len(code)-4:], fmt.Sprintf("%04X", crc16(code[:len(code)-4]))) {
		return nil, false
	}
	out := &BRCode{}
	for i := 0; i+4 <= len(code); {
		id := code[i : i+2]
		size, err := strconv.Atoi(code[i+2 : i+4])
		if err != nil || i+4+size > len(code) {
			return nil, false
		}
		value := code[i+4 : i+4+size]
		switch id {
		case "54":
			reais, err := strconv.ParseFloat(value, 64)
			if err != nil || reais < 0 {
				return nil, false
			}
			out.AmountCents = int64(reais*100 + 0.5)
		case "59":
			out.Name = value
		case "60":
			out.City = value
		}
		i += 4 + size
	}
	return out, true
}

// crc16 é o CRC-16/CCITT-FALSE do padrão do Pix (polinômio 0x1021, início 0xFFFF).
func crc16(s string) uint16 {
	crc := uint16(0xFFFF)
	for i := 0; i < len(s); i++ {
		crc ^= uint16(s[i]) << 8
		for range 8 {
			if crc&0x8000 != 0 {
				crc = crc<<1 ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}
