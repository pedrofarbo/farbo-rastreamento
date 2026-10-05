// Package finance é a gestão da empresa, só para os administradores: contas
// a pagar e receitas avulsas (com fornecedores, categorias, parcelas, contas
// recorrentes e anexos), o estoque de rastreadores e chips com o custo
// médio, o fluxo de caixa com a projeção e o resultado do mês (DRE).
//
// As faturas dos clientes continuam no faturamento (billing): aqui elas
// entram como receita no dia em que são pagas. O caixa é uma visão só (sem
// separar por banco), a partir de um saldo inicial numa data.
package finance

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
)

// Lançamentos.
const (
	KindPayable    = "PAYABLE"    // conta a pagar
	KindReceivable = "RECEIVABLE" // receita avulsa (as faturas ficam no billing)

	StatusOpen     = "OPEN"
	StatusPaid     = "PAID"
	StatusCanceled = "CANCELED"
)

// Categorias: o tipo e a linha no resultado do mês (ver a migração 0028).
const (
	CategoryExpense = "EXPENSE"
	CategoryIncome  = "INCOME"

	GroupRevenue     = "REVENUE"
	GroupOtherIncome = "OTHER_INCOME"
	GroupCapitalIn   = "CAPITAL_IN"
	GroupTax         = "TAX"
	GroupCost        = "COST"
	GroupOperating   = "OPERATING"
	GroupFinancial   = "FINANCIAL"
	GroupInvestment  = "INVESTMENT"
	GroupCapitalOut  = "CAPITAL_OUT"
)

var groupKind = map[string]string{
	GroupRevenue: CategoryIncome, GroupOtherIncome: CategoryIncome, GroupCapitalIn: CategoryIncome,
	GroupTax: CategoryExpense, GroupCost: CategoryExpense, GroupOperating: CategoryExpense,
	GroupFinancial: CategoryExpense, GroupInvestment: CategoryExpense, GroupCapitalOut: CategoryExpense,
}

// Formas de pagamento.
var methods = map[string]bool{"PIX": true, "BOLETO": true, "CARD": true, "TRANSFER": true, "CASH": true, "DEBIT": true}

// Estoque.
const (
	StockTracker   = "TRACKER"
	StockSIM       = "SIM"
	StockAccessory = "ACCESSORY"
	StockOther     = "OTHER"

	MoveIn     = "IN"     // compra ou entrada
	MoveOut    = "OUT"    // instalação ou venda
	MoveLoss   = "LOSS"   // perda ou defeito
	MoveAdjust = "ADJUST" // acerto de contagem (+ ou -)
)

var stockKinds = map[string]bool{StockTracker: true, StockSIM: true, StockAccessory: true, StockOther: true}

const (
	maxText         = 200
	maxNotes        = 2000
	maxInstallments = 60
	// maxAmountCents: R$ 10 milhões por lançamento barra um zero a mais.
	maxAmountCents = 1_000_000_000
	maxQuantity    = 1_000_000
)

// ValidationError é um dado recusado; a mensagem vai para a tela.
type ValidationError struct{ Message string }

func (e ValidationError) Error() string { return e.Message }

func invalid(format string, args ...any) error {
	return ValidationError{Message: fmt.Sprintf(format, args...)}
}

// ErrNotOpen: a conta já foi paga ou cancelada (mexa depois de reabrir).
var ErrNotOpen = ValidationError{Message: "a conta não está em aberto"}

// ---------------------------------------------------------------------------
// Cadastros
// ---------------------------------------------------------------------------

type Category struct {
	ID     uuid.UUID `json:"id"`
	Name   string    `json:"name"`
	Kind   string    `json:"kind"`
	Group  string    `json:"group"`
	Active bool      `json:"active"`
}

type CategoryInput struct {
	Name   string `json:"name"`
	Group  string `json:"group"`
	Active bool   `json:"active"`
}

// Normalize confere a categoria; o tipo (despesa ou receita) vem da linha.
func (in CategoryInput) Normalize() (CategoryInput, string, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || utf8.RuneCountInString(in.Name) > 80 {
		return in, "", invalid("Dê um nome à categoria (até 80 letras).")
	}
	kind, ok := groupKind[in.Group]
	if !ok {
		return in, "", invalid("Escolha em que linha do resultado a categoria entra.")
	}
	return in, kind, nil
}

type Supplier struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Document  string    `json:"document"`
	Email     string    `json:"email"`
	Phone     string    `json:"phone"`
	PixKey    string    `json:"pixKey"`
	Notes     string    `json:"notes"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"createdAt"`
}

type SupplierInput struct {
	Name     string `json:"name"`
	Document string `json:"document"`
	Email    string `json:"email"`
	Phone    string `json:"phone"`
	PixKey   string `json:"pixKey"`
	Notes    string `json:"notes"`
	Active   bool   `json:"active"`
}

func (in SupplierInput) Normalize() (SupplierInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	in.Phone = strings.TrimSpace(in.Phone)
	in.PixKey = strings.TrimSpace(in.PixKey)
	in.Notes = strings.TrimSpace(in.Notes)
	in.Document = digits(in.Document)
	switch {
	case in.Name == "" || utf8.RuneCountInString(in.Name) > maxText:
		return in, invalid("Dê o nome do fornecedor.")
	case in.Document != "" && len(in.Document) != 11 && len(in.Document) != 14:
		return in, invalid("O documento precisa ser um CPF (11 dígitos) ou CNPJ (14 dígitos).")
	case in.Email != "" && !strings.Contains(in.Email, "@"):
		return in, invalid("E-mail inválido.")
	case utf8.RuneCountInString(in.Phone) > 40 || utf8.RuneCountInString(in.PixKey) > 140:
		return in, invalid("Telefone ou chave Pix longos demais.")
	case utf8.RuneCountInString(in.Notes) > maxNotes:
		return in, invalid("Observações longas demais.")
	}
	return in, nil
}

// ---------------------------------------------------------------------------
// Lançamentos
// ---------------------------------------------------------------------------

// Entry é uma conta a pagar ou uma receita avulsa.
type Entry struct {
	ID           uuid.UUID     `json:"id"`
	Kind         string        `json:"kind"`
	Description  string        `json:"description"`
	CategoryID   uuid.UUID     `json:"categoryId"`
	CategoryName string        `json:"categoryName"`
	Group        string        `json:"group"`
	SupplierID   *uuid.UUID    `json:"supplierId"`
	SupplierName string        `json:"supplierName"`
	AmountCents  int64         `json:"amountCents"`
	DueDate      billing.Date  `json:"dueDate"`
	Status       string        `json:"status"`
	PaidOn       *billing.Date `json:"paidOn"`
	PaidCents    *int64        `json:"paidCents"`
	// Overdue: em aberto com o vencimento no passado (calculado na leitura).
	Overdue         bool         `json:"overdue"`
	PaymentMethod   string       `json:"paymentMethod"`
	PaymentCode     string       `json:"paymentCode"`
	Notes           string       `json:"notes"`
	RecurrenceID    *uuid.UUID   `json:"recurrenceId"`
	Installment     *int         `json:"installment"`
	Installments    *int         `json:"installments"`
	StockMovementID *uuid.UUID   `json:"stockMovementId"`
	Attachments     []Attachment `json:"attachments"`
	CreatedAt       time.Time    `json:"createdAt"`
	UpdatedAt       time.Time    `json:"updatedAt"`
}

// EntryInput é um lançamento novo. O valor é o total: com parcelas, ele é
// dividido e cada parcela vence um mês depois da anterior.
type EntryInput struct {
	Kind         string       `json:"kind"`
	Description  string       `json:"description"`
	CategoryID   uuid.UUID    `json:"categoryId"`
	SupplierID   *uuid.UUID   `json:"supplierId"`
	AmountCents  int64        `json:"amountCents"`
	DueDate      billing.Date `json:"dueDate"`
	Installments int          `json:"installments"`
	PaymentCode  string       `json:"paymentCode"`
	Notes        string       `json:"notes"`
	// PaidOn: lançar já pago (uma despesa que já saiu), nessa data e forma.
	PaidOn        *billing.Date `json:"paidOn"`
	PaymentMethod string        `json:"paymentMethod"`
}

func (in EntryInput) Normalize() (EntryInput, error) {
	in.Description = strings.TrimSpace(in.Description)
	in.PaymentCode = strings.TrimSpace(in.PaymentCode)
	in.Notes = strings.TrimSpace(in.Notes)
	if in.Installments == 0 {
		in.Installments = 1
	}
	switch {
	case in.Kind != KindPayable && in.Kind != KindReceivable:
		return in, invalid("Tipo de lançamento inválido.")
	case in.Description == "" || utf8.RuneCountInString(in.Description) > maxText:
		return in, invalid("Descreva o lançamento (até %d letras).", maxText)
	case in.CategoryID == uuid.Nil:
		return in, invalid("Escolha a categoria.")
	case in.AmountCents <= 0 || in.AmountCents > maxAmountCents:
		return in, invalid("Informe o valor.")
	case in.DueDate.IsZero():
		return in, invalid("Informe o vencimento.")
	case in.Installments < 1 || in.Installments > maxInstallments:
		return in, invalid("Parcelas: de 1 a %d.", maxInstallments)
	case int64(in.Installments) > in.AmountCents:
		return in, invalid("Valor pequeno demais para tantas parcelas.")
	case in.PaidOn != nil && in.Installments > 1:
		return in, invalid("Lance o parcelado em aberto e dê baixa em cada parcela.")
	case in.PaymentMethod != "" && !methods[in.PaymentMethod]:
		return in, invalid("Forma de pagamento inválida.")
	case utf8.RuneCountInString(in.PaymentCode) > 500 || utf8.RuneCountInString(in.Notes) > maxNotes:
		return in, invalid("Código de pagamento ou observações longos demais.")
	}
	return in, nil
}

// EntryUpdate muda uma conta em aberto (a paga se reabre antes).
type EntryUpdate struct {
	Description string       `json:"description"`
	CategoryID  uuid.UUID    `json:"categoryId"`
	SupplierID  *uuid.UUID   `json:"supplierId"`
	AmountCents int64        `json:"amountCents"`
	DueDate     billing.Date `json:"dueDate"`
	PaymentCode string       `json:"paymentCode"`
	Notes       string       `json:"notes"`
}

func (in EntryUpdate) Normalize(kind string) (EntryUpdate, error) {
	n, err := EntryInput{
		Kind: kind, Description: in.Description, CategoryID: in.CategoryID, AmountCents: in.AmountCents,
		DueDate: in.DueDate, PaymentCode: in.PaymentCode, Notes: in.Notes,
	}.Normalize()
	in.Description, in.PaymentCode, in.Notes = n.Description, n.PaymentCode, n.Notes
	return in, err
}

// PayInput dá baixa: quando, quanto (com juros ou desconto) e como.
type PayInput struct {
	PaidOn    billing.Date `json:"paidOn"`
	PaidCents int64        `json:"paidCents"`
	Method    string       `json:"method"`
}

func (in PayInput) Normalize(amount int64, today billing.Date) (PayInput, error) {
	if in.PaidOn.IsZero() {
		in.PaidOn = today
	}
	if in.PaidCents == 0 {
		in.PaidCents = amount
	}
	switch {
	case in.PaidCents < 0 || in.PaidCents > maxAmountCents:
		return in, invalid("Valor pago inválido.")
	case in.Method != "" && !methods[in.Method]:
		return in, invalid("Forma de pagamento inválida.")
	case today.Before(in.PaidOn):
		return in, invalid("A data do pagamento não pode ser no futuro.")
	}
	return in, nil
}

// Attachment é um arquivo de uma conta (sem o conteúdo).
type Attachment struct {
	ID          uuid.UUID `json:"id"`
	EntryID     uuid.UUID `json:"entryId"`
	Filename    string    `json:"filename"`
	ContentType string    `json:"contentType"`
	SizeBytes   int       `json:"sizeBytes"`
	CreatedAt   time.Time `json:"createdAt"`
}

// MaxAttachmentBytes: um boleto ou uma nota escaneada cabem com folga.
const MaxAttachmentBytes = 5 << 20

// AttachmentType confere o arquivo pelo conteúdo (não pelo nome nem pelo que
// o navegador diz): só PDF, PNG e JPEG. Devolve "" para o resto.
func AttachmentType(data []byte) string {
	switch {
	case len(data) >= 5 && string(data[:5]) == "%PDF-":
		return "application/pdf"
	case len(data) >= 8 && string(data[:8]) == "\x89PNG\r\n\x1a\n":
		return "image/png"
	case len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF:
		return "image/jpeg"
	}
	return ""
}

// CleanFilename tira caminho e caracteres de controle do nome do arquivo.
func CleanFilename(name string) string {
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 || r == '"' {
			return -1
		}
		return r
	}, strings.TrimSpace(name))
	if name == "" {
		return "anexo"
	}
	if utf8.RuneCountInString(name) > 120 {
		name = string([]rune(name)[:120])
	}
	return name
}

// ---------------------------------------------------------------------------
// Recorrências
// ---------------------------------------------------------------------------

type Recurrence struct {
	ID           uuid.UUID     `json:"id"`
	Kind         string        `json:"kind"`
	Description  string        `json:"description"`
	CategoryID   uuid.UUID     `json:"categoryId"`
	CategoryName string        `json:"categoryName"`
	SupplierID   *uuid.UUID    `json:"supplierId"`
	SupplierName string        `json:"supplierName"`
	AmountCents  int64         `json:"amountCents"`
	DueDay       int           `json:"dueDay"`
	NextDueDate  billing.Date  `json:"nextDueDate"`
	EndsOn       *billing.Date `json:"endsOn"`
	Active       bool          `json:"active"`
	CreatedAt    time.Time     `json:"createdAt"`
}

// RecurrenceInput: o primeiro vencimento define o dia de todo mês.
type RecurrenceInput struct {
	Kind         string        `json:"kind"`
	Description  string        `json:"description"`
	CategoryID   uuid.UUID     `json:"categoryId"`
	SupplierID   *uuid.UUID    `json:"supplierId"`
	AmountCents  int64         `json:"amountCents"`
	FirstDueDate billing.Date  `json:"firstDueDate"`
	EndsOn       *billing.Date `json:"endsOn"`
}

func (in RecurrenceInput) Normalize() (RecurrenceInput, error) {
	n, err := EntryInput{
		Kind: in.Kind, Description: in.Description, CategoryID: in.CategoryID, AmountCents: in.AmountCents,
		DueDate: in.FirstDueDate,
	}.Normalize()
	if err != nil {
		return in, err
	}
	in.Description = n.Description
	switch {
	case in.FirstDueDate.Day() > 28:
		return in, invalid("Escolha um dia de vencimento até 28 (para existir em todos os meses).")
	case in.EndsOn != nil && in.EndsOn.Before(in.FirstDueDate):
		return in, invalid("O fim não pode ser antes do primeiro vencimento.")
	}
	return in, nil
}

// RecurrenceUpdate muda o que vem pela frente (e as contas em aberto que a
// recorrência já gerou e ainda não venceram).
type RecurrenceUpdate struct {
	Description string        `json:"description"`
	CategoryID  uuid.UUID     `json:"categoryId"`
	SupplierID  *uuid.UUID    `json:"supplierId"`
	AmountCents int64         `json:"amountCents"`
	EndsOn      *billing.Date `json:"endsOn"`
}

// ---------------------------------------------------------------------------
// Estoque
// ---------------------------------------------------------------------------

type StockItem struct {
	ID           uuid.UUID `json:"id"`
	Name         string    `json:"name"`
	Kind         string    `json:"kind"`
	MinQuantity  int       `json:"minQuantity"`
	Quantity     int       `json:"quantity"`
	AvgCostCents int64     `json:"avgCostCents"`
	// ValueCents: quanto vale o que está parado (quantidade × custo médio).
	ValueCents int64     `json:"valueCents"`
	Low        bool      `json:"low"`
	Active     bool      `json:"active"`
	CreatedAt  time.Time `json:"createdAt"`
}

type StockItemInput struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	MinQuantity int    `json:"minQuantity"`
	Active      bool   `json:"active"`
}

func (in StockItemInput) Normalize() (StockItemInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	switch {
	case in.Name == "" || utf8.RuneCountInString(in.Name) > 80:
		return in, invalid("Dê um nome ao item (até 80 letras).")
	case !stockKinds[in.Kind]:
		return in, invalid("Escolha o tipo do item.")
	case in.MinQuantity < 0 || in.MinQuantity > maxQuantity:
		return in, invalid("Estoque mínimo inválido.")
	}
	return in, nil
}

type StockMovement struct {
	ID            uuid.UUID    `json:"id"`
	ItemID        uuid.UUID    `json:"itemId"`
	ItemName      string       `json:"itemName"`
	Type          string       `json:"type"`
	Quantity      int          `json:"quantity"`
	UnitCostCents int64        `json:"unitCostCents"`
	TotalCents    int64        `json:"totalCents"`
	OccurredOn    billing.Date `json:"occurredOn"`
	SupplierID    *uuid.UUID   `json:"supplierId"`
	SupplierName  string       `json:"supplierName"`
	Notes         string       `json:"notes"`
	CreatedAt     time.Time    `json:"createdAt"`
}

// MovementInput é um movimento novo. A quantidade vem sempre positiva,
// menos no acerto (ADJUST), em que o sinal diz se sobrou ou faltou.
type MovementInput struct {
	ItemID        uuid.UUID    `json:"itemId"`
	Type          string       `json:"type"`
	Quantity      int          `json:"quantity"`
	UnitCostCents int64        `json:"unitCostCents"`
	OccurredOn    billing.Date `json:"occurredOn"`
	SupplierID    *uuid.UUID   `json:"supplierId"`
	Notes         string       `json:"notes"`
	// Payable lança junto a conta a pagar da compra (só na entrada).
	Payable *PurchasePayable `json:"payable"`
}

// PurchasePayable é a conta da compra: o valor é quantidade × custo.
type PurchasePayable struct {
	CategoryID    uuid.UUID     `json:"categoryId"`
	DueDate       billing.Date  `json:"dueDate"`
	Installments  int           `json:"installments"`
	PaidOn        *billing.Date `json:"paidOn"`
	PaymentMethod string        `json:"paymentMethod"`
}

func (in MovementInput) Normalize(today billing.Date) (MovementInput, error) {
	in.Notes = strings.TrimSpace(in.Notes)
	if in.OccurredOn.IsZero() {
		in.OccurredOn = today
	}
	switch {
	case in.ItemID == uuid.Nil:
		return in, invalid("Escolha o item.")
	case in.Type != MoveIn && in.Type != MoveOut && in.Type != MoveLoss && in.Type != MoveAdjust:
		return in, invalid("Tipo de movimento inválido.")
	case in.Quantity == 0 || in.Quantity > maxQuantity || in.Quantity < -maxQuantity:
		return in, invalid("Informe a quantidade.")
	case in.Type != MoveAdjust && in.Quantity < 0:
		return in, invalid("A quantidade é sempre positiva; o tipo diz se entra ou sai.")
	case in.Type == MoveIn && (in.UnitCostCents <= 0 || in.UnitCostCents > maxAmountCents):
		return in, invalid("Informe o custo por unidade da compra.")
	case in.Type != MoveIn && in.Payable != nil:
		return in, invalid("A conta a pagar só vai junto com a entrada (compra).")
	case today.Before(in.OccurredOn):
		return in, invalid("O movimento não pode ser no futuro.")
	case utf8.RuneCountInString(in.Notes) > maxNotes:
		return in, invalid("Observações longas demais.")
	}
	if in.Type != MoveIn {
		in.UnitCostCents = 0 // as saídas saem pelo custo médio
	}
	return in, nil
}

// signedQuantity: entrada positiva, saída negativa.
func (in MovementInput) signedQuantity() int {
	if in.Type == MoveOut || in.Type == MoveLoss {
		return -in.Quantity
	}
	return in.Quantity
}

// AverageCost é o custo médio ponderado depois de uma entrada: o que havia,
// pelo custo médio, mais o que entrou, pelo custo da compra. Com o estoque
// zerado, vale o custo da compra.
func AverageCost(quantity int, avg int64, inQuantity int, inCost int64) int64 {
	if quantity <= 0 {
		return inCost
	}
	total := int64(quantity)*avg + int64(inQuantity)*inCost
	units := int64(quantity + inQuantity)
	return (total + units/2) / units // arredonda para o centavo mais próximo
}

// ---------------------------------------------------------------------------
// Datas e parcelas
// ---------------------------------------------------------------------------

// AddMonths soma meses mantendo o dia, ou o último dia do mês quando ele não
// existe (31/01 + 1 mês = 28/02).
func AddMonths(d billing.Date, months int) billing.Date {
	first := billing.NewDate(d.Year(), d.Month()+time.Month(months), 1)
	last := first.AddDate(0, 1, -1).Day()
	return billing.NewDate(first.Year(), first.Month(), min(d.Day(), last))
}

// SplitInstallments divide o total em n parcelas iguais; os centavos que
// sobram vão para a primeira.
func SplitInstallments(total int64, n int) []int64 {
	if n < 1 {
		n = 1
	}
	each := total / int64(n)
	out := make([]int64, n)
	for i := range out {
		out[i] = each
	}
	out[0] += total - each*int64(n)
	return out
}

// Month é "2026-10".
func Month(d billing.Date) string { return d.Format("2006-01") }

func digits(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, s)
}

// BRL formata centavos: 123456 → "R$ 1.234,56".
func BRL(cents int64) string {
	sign := ""
	if cents < 0 {
		sign, cents = "-", -cents
	}
	reais := cents / 100
	groups := []string{}
	for reais >= 1000 {
		groups = append([]string{fmt.Sprintf("%03d", reais%1000)}, groups...)
		reais /= 1000
	}
	groups = append([]string{fmt.Sprintf("%d", reais)}, groups...)
	return fmt.Sprintf("%sR$ %s,%02d", sign, strings.Join(groups, "."), cents%100)
}
