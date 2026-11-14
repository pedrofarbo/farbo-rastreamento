package api

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/payments"
)

// O cartão de crédito nos links de pagamento (sem login): o da fatura, o das
// faturas do mesmo vencimento e o de todas em aberto do cliente — também o
// que a contratação pelo site abre. Só pela Pagar.me: o navegador gera o
// token na API dela com a chave pública, e os dados do pagador (os do
// cadastro) entram aqui, sem aparecer na página. Sem login, não salva o
// cartão nem liga o débito automático (isso o cliente faz no app).

// publicCardView é o que a página do link precisa do cartão: se ele vale ali
// e a chave pública. PayerReady: o cadastro tem o que a Pagar.me pede (CPF ou
// CNPJ, e-mail, celular e endereço) — os dados não saem daqui.
type publicCardView struct {
	Enabled    bool   `json:"enabled"`
	Provider   string `json:"provider"`
	PublicKey  string `json:"publicKey"`
	PayerReady bool   `json:"payerReady"`
	PublicLink bool   `json:"publicLink"`
	// Saved e Payer: sempre nulos no link (o formulário é o do cartão novo).
	Saved *payments.SavedCard `json:"saved"`
	Payer *struct{}           `json:"payer"`
}

// publicCardEnabled diz se o cartão vale nos links: ligado e pela Pagar.me.
func (s *Server) publicCardEnabled() bool {
	return s.Payments != nil && s.Payments.CardEnabled() && s.Payments.CardProvider() == "pagarme"
}

func (s *Server) publicCard(r *http.Request, customerID uuid.UUID) publicCardView {
	view := publicCardView{Enabled: s.publicCardEnabled(), PublicLink: true}
	if !view.Enabled {
		return view
	}
	view.Provider, view.PublicKey = s.Payments.CardProvider(), s.Payments.CardPublicKey()
	payer := s.payerFor(r, customerID)
	// A mesma conferência da cobrança: o que passa aqui a Pagar.me aceita.
	view.PayerReady = len(payments.CardPayerMissing(payer)) == 0
	return view
}

// handlePublicInvoiceCard e handlePublicAccountCard: o cartão do link.
func (s *Server) handlePublicInvoiceCard(w http.ResponseWriter, r *http.Request) {
	if inv, ok := s.linkInvoice(w, r); ok {
		writeJSON(w, http.StatusOK, s.publicCard(r, inv.CustomerID))
	}
}

func (s *Server) handlePublicAccountCard(w http.ResponseWriter, r *http.Request) {
	if customerID, ok := s.accountCustomer(w, r); ok {
		writeJSON(w, http.StatusOK, s.publicCard(r, customerID))
	}
}

// publicCardRequest é o pagamento no cartão pelo link: só o token da Pagar.me.
type publicCardRequest struct {
	Token string `json:"token"`
}

// handlePublicInvoicePayCard paga no cartão a fatura do link;
// handlePublicInvoicePayCardGroup, as do mesmo vencimento; e
// handlePublicAccountPayCard, todas as em aberto do cliente.
func (s *Server) handlePublicInvoicePayCard(w http.ResponseWriter, r *http.Request) {
	inv, ok := s.linkInvoice(w, r)
	if !ok {
		return
	}
	s.publicPayCard(w, r, inv.CustomerID, []uuid.UUID{inv.ID})
}

func (s *Server) handlePublicInvoicePayCardGroup(w http.ResponseWriter, r *http.Request) {
	inv, ok := s.linkInvoice(w, r)
	if !ok {
		return
	}
	group, err := s.invoiceGroup(r, inv)
	if err != nil {
		handleStoreError(w, err, "link de pagamento não encontrado")
		return
	}
	ids := []uuid.UUID{inv.ID}
	if group != nil {
		ids = ids[:0]
		for _, g := range group {
			ids = append(ids, g.ID)
		}
	}
	s.publicPayCard(w, r, inv.CustomerID, ids)
}

func (s *Server) handlePublicAccountPayCard(w http.ResponseWriter, r *http.Request) {
	customerID, ok := s.accountCustomer(w, r)
	if !ok {
		return
	}
	open, err := s.openInvoices(r, customerID)
	if err != nil {
		handleStoreError(w, err, "link de pagamento não encontrado")
		return
	}
	if len(open) == 0 {
		writeError(w, http.StatusConflict, "não há fatura em aberto: está tudo pago")
		return
	}
	ids := make([]uuid.UUID, len(open))
	for i, inv := range open {
		ids[i] = inv.ID
	}
	s.publicPayCard(w, r, customerID, ids)
}

func (s *Server) publicPayCard(w http.ResponseWriter, r *http.Request, customerID uuid.UUID, ids []uuid.UUID) {
	if !s.publicCardEnabled() {
		writeError(w, http.StatusConflict, "o pagamento no cartão não está disponível por este link: pague com Pix ou boleto")
		return
	}
	var req publicCardRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	charge, err := s.Payments.CardForInvoices(r.Context(), customerID, ids, payments.CardInput{Token: req.Token},
		s.payerFor(r, customerID))
	meta := map[string]any{"customerId": customerID, "invoiceIds": ids, "publicLink": true}
	if charge != nil {
		meta["chargeId"], meta["amountCents"], meta["status"] = charge.ID, charge.AmountCents, charge.Status
	}
	_, declinedMsg, declined := payments.ProviderError(err)
	switch {
	case errors.Is(err, payments.ErrCardUnknown):
		// Sem resposta do banco: a página acompanha a cobrança até a conferência.
		s.recordBillingAudit(r, audit.ActionPaymentCardCharged, meta)
		writeJSON(w, http.StatusAccepted, charge)
		return
	case declined:
		meta["error"] = declinedMsg
		s.recordBillingAudit(r, audit.ActionPaymentCardDeclined, meta)
		writeError(w, http.StatusPaymentRequired, "o cartão foi recusado: "+providerMessage(declinedMsg))
		return
	case errors.Is(err, database.ErrNotFound):
		writeError(w, http.StatusNotFound, "fatura não encontrada")
		return
	case err != nil:
		s.writePaymentError(w, err)
		return
	}
	s.recordBillingAudit(r, audit.ActionPaymentCardCharged, meta)
	writeJSON(w, http.StatusOK, charge)
}
