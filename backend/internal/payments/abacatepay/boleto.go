package abacatepay

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// O boleto (checkout transparente, método BOLETO). Vem com um Pix na mesma
// cobrança: o cliente paga pelo código de barras (compensa em até 3 dias
// úteis) ou pelo Pix (na hora). Diferente do Pix, a AbacatePay não estorna
// boleto pela API nem simula o pagamento dele em testes; o status sai de
// /transparents/get (o /transparents/check é só para Pix).

// BoletoReceiveFeeCents é a tarifa da AbacatePay por boleto pago (R$ 2,50),
// descontada do valor pago: vale quando a resposta não informa.
const BoletoReceiveFeeCents = 250

// BoletoCustomer é o pagador do boleto: nome e CPF/CNPJ obrigatórios; e-mail
// e celular, se houver.
type BoletoCustomer struct {
	Name      string `json:"name"`
	TaxID     string `json:"taxId"`
	Email     string `json:"email,omitempty"`
	Cellphone string `json:"cellphone,omitempty"`
}

// BoletoRequest é o boleto a criar.
type BoletoRequest struct {
	AmountCents int
	Description string
	// DueDate (AAAA-MM-DD): vazio, a AbacatePay põe 3 dias úteis. Não pode
	// ser no passado.
	DueDate    string
	ExternalID string
	Customer   BoletoCustomer
	Metadata   map[string]string
}

// Boleto é a cobrança criada. BarCode são os 44 dígitos do código de
// barras; Line, a linha digitável (47 dígitos), que se digita no banco.
type Boleto struct {
	ID           string            `json:"id"`
	Amount       int               `json:"amount"`
	Status       string            `json:"status"`
	DevMode      bool              `json:"devMode"`
	BarCode      string            `json:"barCode"`
	Line         string            `json:"identificationNumber"`
	URL          string            `json:"url"`
	BrCode       string            `json:"brCode"`
	BrCodeBase64 string            `json:"brCodeBase64"`
	PlatformFee  int               `json:"platformFee"`
	ExpiresAt    *time.Time        `json:"expiresAt"`
	CreatedAt    time.Time         `json:"createdAt"`
	Metadata     map[string]string `json:"metadata"`
}

// CreateBoleto gera o boleto com o Pix junto.
func (c *Client) CreateBoleto(ctx context.Context, req BoletoRequest) (*Boleto, error) {
	data := map[string]any{"amount": req.AmountCents, "customer": req.Customer}
	if description := sanitizeDescription(req.Description); description != "" {
		data["description"] = truncate(description, 500)
	}
	if req.DueDate != "" {
		data["dueDate"] = req.DueDate
	}
	if req.ExternalID != "" {
		data["externalId"] = req.ExternalID
	}
	if len(req.Metadata) > 0 {
		data["metadata"] = req.Metadata
	}

	var boleto Boleto
	err := c.do(ctx, http.MethodPost, "/transparents/create", nil,
		map[string]any{"method": "BOLETO", "data": data}, &boleto)
	if isDisallowedCharacter(err) {
		if description := plainDescription(req.Description); description != "" {
			data["description"] = truncate(description, 500)
		} else {
			delete(data, "description")
		}
		err = c.do(ctx, http.MethodPost, "/transparents/create", nil,
			map[string]any{"method": "BOLETO", "data": data}, &boleto)
	}
	if err != nil {
		return nil, err
	}
	return &boleto, nil
}

// GetCharge consulta qualquer cobrança do checkout transparente (é por onde
// se vê o status do boleto).
func (c *Client) GetCharge(ctx context.Context, id string) (*PixStatus, error) {
	var status PixStatus
	if err := c.do(ctx, http.MethodGet, "/transparents/get", url.Values{"id": {id}}, nil, &status); err != nil {
		return nil, err
	}
	return &status, nil
}
