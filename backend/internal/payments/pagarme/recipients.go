package pagarme

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// Os recebedores: quem recebe a parte de um pagamento pelo split (o
// afiliado). A Pagar.me pede os dados de cadastro (KYC) e a conta bancária;
// depois, a prova de vida pelo link (vale 20 minutos). Só o recebedor ativo
// movimenta o saldo; o repasse para a conta é automático (diário).

// As situações do recebedor.
const (
	RecipientRegistration = "registration"
	RecipientAffiliation  = "affiliation"
	RecipientActive       = "active"
	RecipientRefused      = "refused"
)

// RecipientPhone é o telefone do cadastro do recebedor.
type RecipientPhone struct {
	DDD    string `json:"ddd"`
	Number string `json:"number"`
	Type   string `json:"type"`
}

// RecipientAddress é o endereço do cadastro do recebedor.
type RecipientAddress struct {
	Street         string `json:"street"`
	Complementary  string `json:"complementary"`
	StreetNumber   string `json:"street_number"`
	Neighborhood   string `json:"neighborhood"`
	City           string `json:"city"`
	State          string `json:"state"`
	ZipCode        string `json:"zip_code"`
	ReferencePoint string `json:"reference_point"`
}

// RegisterInformation é o cadastro (KYC). Pessoa física (Type individual):
// nome, nascimento (DD/MM/AAAA), renda mensal e profissão; jurídica
// (corporation): razão social, nome fantasia e faturamento anual.
type RegisterInformation struct {
	Type                   string            `json:"type"`
	Document               string            `json:"document"`
	Email                  string            `json:"email"`
	PhoneNumbers           []RecipientPhone  `json:"phone_numbers"`
	Name                   string            `json:"name,omitempty"`
	Birthdate              string            `json:"birthdate,omitempty"`
	MonthlyIncome          int               `json:"monthly_income,omitempty"`
	ProfessionalOccupation string            `json:"professional_occupation,omitempty"`
	Address                *RecipientAddress `json:"address,omitempty"`
	CompanyName            string            `json:"company_name,omitempty"`
	TradingName            string            `json:"trading_name,omitempty"`
	AnnualRevenue          int               `json:"annual_revenue,omitempty"`
	MainAddress            *RecipientAddress `json:"main_address,omitempty"`
}

// BankAccount é a conta para onde o saldo é repassado. Type: checking ou
// savings; HolderType: individual ou company (o titular é o recebedor).
type BankAccount struct {
	HolderName        string `json:"holder_name"`
	HolderType        string `json:"holder_type"`
	HolderDocument    string `json:"holder_document"`
	Bank              string `json:"bank"`
	BranchNumber      string `json:"branch_number"`
	BranchCheckDigit  string `json:"branch_check_digit,omitempty"`
	AccountNumber     string `json:"account_number"`
	AccountCheckDigit string `json:"account_check_digit"`
	Type              string `json:"type"`
}

// RecipientRequest é o recebedor a criar. Code é o nosso id (o do afiliado).
type RecipientRequest struct {
	Code                string              `json:"code"`
	RegisterInformation RegisterInformation `json:"register_information"`
	DefaultBankAccount  BankAccount         `json:"default_bank_account"`
	TransferSettings    transferSettings    `json:"transfer_settings"`
}

type transferSettings struct {
	TransferEnabled  bool   `json:"transfer_enabled"`
	TransferInterval string `json:"transfer_interval"`
	TransferDay      int    `json:"transfer_day"`
}

// Recipient é o recebedor na Pagar.me.
type Recipient struct {
	ID     string `json:"id"`
	Code   string `json:"code"`
	Status string `json:"status"`
}

// KYCLink é o link da prova de vida (e o QR Code em base64), que vence em
// 20 minutos.
type KYCLink struct {
	URL            string     `json:"url"`
	Base64         string     `json:"base64"`
	ExpirationDate *time.Time `json:"expiration_date"`
}

// CreateRecipient cadastra o recebedor, com o repasse diário automático para
// a conta. O Code vai também como chave de idempotência.
func (c *Client) CreateRecipient(ctx context.Context, req RecipientRequest) (*Recipient, error) {
	req.TransferSettings = transferSettings{TransferEnabled: true, TransferInterval: "Daily", TransferDay: 0}
	var out Recipient
	if err := c.do(ctx, http.MethodPost, "/recipients", nil, "recipient-"+req.Code, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetRecipient consulta o recebedor (a situação do cadastro).
func (c *Client) GetRecipient(ctx context.Context, id string) (*Recipient, error) {
	var out Recipient
	if err := c.do(ctx, http.MethodGet, "/recipients/"+url.PathEscape(id), nil, "", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateKYCLink gera o link da prova de vida do recebedor.
func (c *Client) CreateKYCLink(ctx context.Context, id string) (*KYCLink, error) {
	var out KYCLink
	if err := c.do(ctx, http.MethodPost, "/recipients/"+url.PathEscape(id)+"/kyc_link", nil, "", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
