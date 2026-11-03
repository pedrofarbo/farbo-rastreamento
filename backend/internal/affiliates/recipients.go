package affiliates

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/payments"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/payments/pagarme"
)

// O afiliado como recebedor na Pagar.me: ele mesmo preenche, pela página
// dele, os dados que ela pede (o KYC) e a conta para o repasse, e depois faz
// a prova de vida pelo link dela. Os dados vão direto para a Pagar.me: aqui
// fica só o id do recebedor e a situação.

// RecipientGateway é o que se usa da Pagar.me. *pagarme.Client implementa.
type RecipientGateway interface {
	CreateRecipient(ctx context.Context, req pagarme.RecipientRequest) (*pagarme.Recipient, error)
	GetRecipient(ctx context.Context, id string) (*pagarme.Recipient, error)
	CreateKYCLink(ctx context.Context, id string) (*pagarme.KYCLink, error)
}

// SetRecipients liga o cadastro de recebedores (a Pagar.me).
func (s *Service) SetRecipients(g RecipientGateway) { s.recipients = g }

// recipientRecheck: a página consulta a situação do recebedor na Pagar.me
// no máximo uma vez nesse intervalo (o webhook avisa antes).
const recipientRecheck = time.Minute

// RecipientInput é o cadastro do recebedor. Kind individual (CPF): o nome, o
// nascimento (DD/MM/AAAA), a renda mensal e a profissão; company (CNPJ): a
// razão social, o nome fantasia e o faturamento anual. A conta é do próprio
// recebedor (o mesmo CPF/CNPJ).
type RecipientInput struct {
	Kind          string `json:"kind"`
	Document      string `json:"document"`
	Name          string `json:"name"`
	Email         string `json:"email"`
	Phone         string `json:"phone"`
	Birthdate     string `json:"birthdate"`
	MonthlyIncome int    `json:"monthlyIncomeCents"`
	Occupation    string `json:"occupation"`
	CompanyName   string `json:"companyName"`
	TradingName   string `json:"tradingName"`
	AnnualRevenue int    `json:"annualRevenueCents"`

	ZipCode        string `json:"zipCode"`
	Street         string `json:"street"`
	Number         string `json:"number"`
	Complement     string `json:"complement"`
	District       string `json:"district"`
	City           string `json:"city"`
	State          string `json:"state"`
	ReferencePoint string `json:"referencePoint"`

	Bank              string `json:"bank"`
	Branch            string `json:"branch"`
	BranchDigit       string `json:"branchDigit"`
	Account           string `json:"account"`
	AccountDigit      string `json:"accountDigit"`
	AccountType       string `json:"accountType"`
	AccountHolderName string `json:"accountHolderName"`
}

var (
	birthdatePattern = regexp.MustCompile(`^\d{2}/\d{2}/\d{4}$`)
	ufPattern        = regexp.MustCompile(`^[A-Z]{2}$`)
)

func digits(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, s)
}

func clean(s string) string { return strings.Join(strings.Fields(s), " ") }

// request confere o cadastro e monta o pedido da Pagar.me.
func (in RecipientInput) request(code string, now time.Time) (pagarme.RecipientRequest, error) {
	var out pagarme.RecipientRequest
	doc := digits(in.Document)
	phone := digits(in.Phone)
	if (len(phone) == 12 || len(phone) == 13) && strings.HasPrefix(phone, "55") {
		phone = phone[2:]
	}
	email := strings.ToLower(strings.TrimSpace(in.Email))
	zip := digits(in.ZipCode)
	state := strings.ToUpper(strings.TrimSpace(in.State))
	switch {
	case in.Kind != "individual" && in.Kind != "company":
		return out, invalid("Escolha: pessoa física ou jurídica.")
	case in.Kind == "individual" && !payments.ValidCPF(doc):
		return out, invalid("CPF inválido.")
	case in.Kind == "company" && !payments.ValidCNPJ(doc):
		return out, invalid("CNPJ inválido.")
	case len(phone) < 10 || len(phone) > 11:
		return out, invalid("Informe o celular com DDD.")
	case email == "":
		return out, invalid("Informe o e-mail.")
	case len(zip) != 8 || clean(in.Street) == "" || clean(in.Number) == "" || clean(in.District) == "" ||
		clean(in.City) == "" || !ufPattern.MatchString(state):
		return out, invalid("Preencha o endereço: CEP, rua, número, bairro, cidade e UF.")
	}
	if _, err := mail.ParseAddress(email); err != nil {
		return out, invalid("E-mail inválido.")
	}
	address := &pagarme.RecipientAddress{
		Street: clean(in.Street), Complementary: clean(in.Complement), StreetNumber: clean(in.Number),
		Neighborhood: clean(in.District), City: clean(in.City), State: state, ZipCode: zip,
		ReferencePoint: clean(in.ReferencePoint),
	}
	if address.Complementary == "" {
		address.Complementary = "Não tem"
	}
	if address.ReferencePoint == "" {
		address.ReferencePoint = "Não informado"
	}
	reg := pagarme.RegisterInformation{
		Type: "individual", Document: doc, Email: email,
		PhoneNumbers: []pagarme.RecipientPhone{{DDD: phone[:2], Number: phone[2:], Type: "mobile"}},
	}
	holderType, holderName := "individual", clean(in.AccountHolderName)
	if in.Kind == "individual" {
		birth, err := time.Parse("02/01/2006", strings.TrimSpace(in.Birthdate))
		switch {
		case clean(in.Name) == "" || utf8.RuneCountInString(in.Name) > 120:
			return out, invalid("Informe o nome completo.")
		case !birthdatePattern.MatchString(strings.TrimSpace(in.Birthdate)) || err != nil:
			return out, invalid("Data de nascimento inválida (DD/MM/AAAA).")
		case birth.AddDate(18, 0, 0).After(now):
			return out, invalid("O recebedor precisa ter 18 anos ou mais.")
		case in.MonthlyIncome <= 0:
			return out, invalid("Informe a renda mensal.")
		case clean(in.Occupation) == "":
			return out, invalid("Informe a profissão.")
		}
		reg.Name, reg.Birthdate, reg.MonthlyIncome = clean(in.Name), strings.TrimSpace(in.Birthdate), in.MonthlyIncome
		reg.ProfessionalOccupation, reg.Address = clean(in.Occupation), address
		if holderName == "" {
			holderName = reg.Name
		}
	} else {
		switch {
		case clean(in.CompanyName) == "" || clean(in.TradingName) == "":
			return out, invalid("Informe a razão social e o nome fantasia.")
		case in.AnnualRevenue <= 0:
			return out, invalid("Informe o faturamento anual.")
		}
		reg.Type, reg.CompanyName, reg.TradingName = "corporation", clean(in.CompanyName), clean(in.TradingName)
		reg.AnnualRevenue, reg.MainAddress = in.AnnualRevenue, address
		holderType = "company"
		if holderName == "" {
			holderName = reg.CompanyName
		}
	}
	bank, branch, account := digits(in.Bank), digits(in.Branch), digits(in.Account)
	accountDigit := strings.ToUpper(strings.TrimSpace(in.AccountDigit))
	switch {
	case len(bank) != 3:
		return out, invalid("O código do banco tem 3 números (ex.: 260, 341, 001).")
	case len(branch) < 1 || len(branch) > 4:
		return out, invalid("A agência tem até 4 números (sem o dígito).")
	case len(account) < 1 || len(account) > 13:
		return out, invalid("A conta tem até 13 números (sem o dígito).")
	case len(accountDigit) < 1 || len(accountDigit) > 2:
		return out, invalid("Informe o dígito da conta.")
	case in.AccountType != "checking" && in.AccountType != "savings":
		return out, invalid("Escolha: conta corrente ou poupança.")
	}
	out = pagarme.RecipientRequest{
		Code: code, RegisterInformation: reg,
		DefaultBankAccount: pagarme.BankAccount{
			HolderName: truncateRunes(holderName, 30), HolderType: holderType, HolderDocument: doc, Bank: bank,
			BranchNumber: branch, BranchCheckDigit: strings.TrimSpace(in.BranchDigit), AccountNumber: account,
			AccountCheckDigit: accountDigit, Type: in.AccountType,
		},
	}
	return out, nil
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) > max {
		return string(r[:max])
	}
	return s
}

// RecipientView é a situação do recebedor na página do afiliado.
type RecipientView struct {
	// Enabled: o split está ligado (a página mostra o cadastro).
	Enabled bool `json:"enabled"`
	// Status: "" (sem cadastro), registration (falta a prova de vida),
	// affiliation (em análise), active ou refused.
	Status string `json:"status"`
}

// recipientFor é o afiliado do link secreto, com o recebedor.
func (s *Service) recipientFor(ctx context.Context, token string) (uuid.UUID, string, string, *time.Time, error) {
	var id uuid.UUID
	var recipient, status string
	var checked *time.Time
	if len(token) < 20 || len(token) > 64 {
		return id, "", "", nil, database.ErrNotFound
	}
	err := s.db.QueryRow(ctx, `
		SELECT id, recipient_id, recipient_status, recipient_checked_at FROM affiliates WHERE report_token = $1 AND active`,
		token).Scan(&id, &recipient, &status, &checked)
	if errors.Is(err, pgx.ErrNoRows) {
		return id, "", "", nil, database.ErrNotFound
	}
	return id, recipient, status, checked, database.MapError(err)
}

// Recipient é a situação do recebedor do afiliado do link (consulta a
// Pagar.me se faz tempo).
func (s *Service) Recipient(ctx context.Context, token string) (*RecipientView, error) {
	view := &RecipientView{Enabled: s.split && s.recipients != nil}
	if !view.Enabled {
		return view, nil
	}
	_, recipient, status, checked, err := s.recipientFor(ctx, token)
	if err != nil {
		return nil, err
	}
	view.Status = status
	if recipient != "" && status != pagarme.RecipientActive && (checked == nil || s.now().Sub(*checked) > recipientRecheck) {
		if fresh, err := s.RefreshRecipient(ctx, recipient); err == nil {
			view.Status = fresh
		} else {
			s.log.Warn("falha ao consultar o recebedor na Pagar.me", "recipient", recipient, "err", err)
		}
	}
	return view, nil
}

// RegisterRecipient cria o recebedor do afiliado na Pagar.me.
func (s *Service) RegisterRecipient(ctx context.Context, token string, in RecipientInput) (*RecipientView, error) {
	if !s.split || s.recipients == nil {
		return nil, invalid("O recebimento pelo split ainda não está ligado.")
	}
	id, recipient, _, _, err := s.recipientFor(ctx, token)
	if err != nil {
		return nil, err
	}
	if recipient != "" {
		return nil, invalid("Seu cadastro de recebedor já foi enviado.")
	}
	req, err := in.request(id.String(), s.now())
	if err != nil {
		return nil, err
	}
	created, err := s.recipients.CreateRecipient(ctx, req)
	if err != nil {
		var apiErr *pagarme.APIError
		if errors.As(err, &apiErr) && apiErr.HTTPStatus < 500 {
			return nil, invalid("A Pagar.me recusou o cadastro: %s", apiErr.Message)
		}
		return nil, fmt.Errorf("cadastrando o recebedor na Pagar.me: %w", err)
	}
	if _, err := s.db.Exec(ctx, `
		UPDATE affiliates SET recipient_id = $2, recipient_status = $3, recipient_checked_at = NOW(), updated_at = NOW()
		WHERE id = $1`, id, created.ID, created.Status); err != nil {
		return nil, database.MapError(err)
	}
	s.log.Info("afiliado cadastrado como recebedor", "affiliate", id, "recipient", created.ID, "status", created.Status)
	return &RecipientView{Enabled: true, Status: created.Status}, nil
}

// RecipientKYCLink é o link da prova de vida do recebedor do afiliado.
func (s *Service) RecipientKYCLink(ctx context.Context, token string) (*pagarme.KYCLink, error) {
	if !s.split || s.recipients == nil {
		return nil, invalid("O recebimento pelo split ainda não está ligado.")
	}
	_, recipient, status, _, err := s.recipientFor(ctx, token)
	if err != nil {
		return nil, err
	}
	switch {
	case recipient == "":
		return nil, invalid("Envie o cadastro de recebedor antes da prova de vida.")
	case status == pagarme.RecipientActive:
		return nil, invalid("Seu cadastro já está ativo: não precisa de prova de vida.")
	}
	link, err := s.recipients.CreateKYCLink(ctx, recipient)
	if err != nil {
		var apiErr *pagarme.APIError
		if errors.As(err, &apiErr) && apiErr.HTTPStatus < 500 {
			return nil, invalid("A Pagar.me não gerou o link agora: %s", apiErr.Message)
		}
		return nil, fmt.Errorf("gerando o link da prova de vida: %w", err)
	}
	return link, nil
}

// RefreshRecipient consulta o recebedor na Pagar.me e guarda a situação (o
// webhook recipient.updated chama isto). Devolve a situação.
func (s *Service) RefreshRecipient(ctx context.Context, recipientID string) (string, error) {
	if s.recipients == nil || recipientID == "" {
		return "", nil
	}
	r, err := s.recipients.GetRecipient(ctx, recipientID)
	if err != nil {
		return "", err
	}
	tag, err := s.db.Exec(ctx, `
		UPDATE affiliates SET recipient_status = $2, recipient_checked_at = NOW(),
			updated_at = CASE WHEN recipient_status <> $2 THEN NOW() ELSE updated_at END
		WHERE recipient_id = $1`, recipientID, r.Status)
	if err != nil {
		return "", database.MapError(err)
	}
	if tag.RowsAffected() > 0 {
		s.log.Info("situação do recebedor", "recipient", recipientID, "status", r.Status)
	}
	return r.Status, nil
}
