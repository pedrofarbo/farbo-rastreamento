package api

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/payments"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/payments/abacatepay"
)

// O cartão de crédito do cliente (internal/payments/card.go): a chave pública
// que o navegador usa para montar o formulário do cartão, pagar faturas no
// cartão, o cartão salvo e o débito automático. O cartão é digitado num
// iframe da AbacatePay; aqui chega só o token de uso único.

// cardView é o cartão da conta: se o cartão está ligado, a chave pública, o
// cartão salvo (nulo: nenhum) e os dados do pagador que o formulário pede —
// os do titular, os mesmos da cobrança (nulo: falta algum no cadastro).
type cardView struct {
	Enabled   bool                 `json:"enabled"`
	PublicKey string               `json:"publicKey"`
	Saved     *payments.SavedCard  `json:"saved"`
	Payer     *abacatepay.Customer `json:"payer"`
}

func (s *Server) cardView(r *http.Request, customerID uuid.UUID) (*cardView, error) {
	view := &cardView{Enabled: s.Payments.CardEnabled(), PublicKey: s.Payments.CardPublicKey()}
	if view.Enabled {
		view.Payer = payments.CardPayer(s.payerFor(r, customerID))
	}
	if !s.Payments.Enabled() {
		return view, nil
	}
	saved, err := s.Payments.SavedCard(r.Context(), customerID)
	if err != nil {
		return nil, err
	}
	view.Saved = saved
	return view, nil
}

func (s *Server) handleMyCard(w http.ResponseWriter, r *http.Request) {
	customerID, _ := customerOf(r)
	view, err := s.cardView(r, customerID)
	if err != nil {
		handleStoreError(w, err, "cartão não encontrado")
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// cardPayRequest é o pagamento no cartão: o token do formulário (ou o
// cartão salvo) e se guarda o cartão com o débito automático.
type cardPayRequest struct {
	InvoiceIDs []uuid.UUID `json:"invoiceIds"`
	Token      string      `json:"token"`
	UseSaved   bool        `json:"useSaved"`
	Save       bool        `json:"save"`
	AutoDebit  bool        `json:"autoDebit"`
}

// handleMyInvoiceCard e handleMyInvoicesCard pagam no cartão uma fatura ou
// várias de uma vez (a do mês da frota). Também com o acesso suspenso.
func (s *Server) handleMyInvoiceCard(w http.ResponseWriter, r *http.Request) {
	inv, ok := s.myInvoice(w, r)
	if !ok {
		return
	}
	var req cardPayRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	s.payCard(w, r, []uuid.UUID{inv.ID}, req)
}

func (s *Server) handleMyInvoicesCard(w http.ResponseWriter, r *http.Request) {
	var req cardPayRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	s.payCard(w, r, req.InvoiceIDs, req)
}

func (s *Server) payCard(w http.ResponseWriter, r *http.Request, ids []uuid.UUID, req cardPayRequest) {
	customerID, _ := customerOf(r)
	by := personOf(r)
	in := payments.CardInput{Token: req.Token, UseSaved: req.UseSaved, Save: req.Save || req.AutoDebit,
		AutoDebit: req.AutoDebit, By: &by}
	charge, err := s.Payments.CardForInvoices(r.Context(), customerID, ids, in, s.payerFor(r, customerID))
	meta := map[string]any{"customerId": customerID, "invoiceIds": ids, "savedCard": req.UseSaved,
		"save": in.Save, "autoDebit": in.AutoDebit}
	if charge != nil {
		meta["chargeId"], meta["amountCents"], meta["status"] = charge.ID, charge.AmountCents, charge.Status
	}
	var apiErr *abacatepay.APIError
	switch {
	case errors.Is(err, payments.ErrCardUnknown):
		// Sem resposta do banco: a tela acompanha a cobrança até a conferência.
		s.recordBillingAudit(r, audit.ActionPaymentCardCharged, meta)
		writeJSON(w, http.StatusAccepted, charge)
		return
	case errors.As(err, &apiErr):
		meta["error"] = apiErr.Message
		s.recordBillingAudit(r, audit.ActionPaymentCardDeclined, meta)
		writeError(w, http.StatusPaymentRequired, "o cartão foi recusado: "+providerMessage(apiErr.Message))
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

type autoDebitRequest struct {
	Enabled bool `json:"enabled"`
}

// handleMyCardAutoDebit liga (com a autorização de quem pediu) ou desliga o
// débito automático no cartão salvo.
func (s *Server) handleMyCardAutoDebit(w http.ResponseWriter, r *http.Request) {
	var req autoDebitRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	customerID, _ := customerOf(r)
	s.setAutoDebit(w, r, customerID, req.Enabled)
}

func (s *Server) setAutoDebit(w http.ResponseWriter, r *http.Request, customerID uuid.UUID, enabled bool) {
	if err := s.Payments.SetAutoDebit(r.Context(), customerID, enabled, personOf(r)); err != nil {
		s.writePaymentError(w, err)
		return
	}
	s.recordBillingAudit(r, audit.ActionCardAutoDebitChanged, map[string]any{"customerId": customerID, "enabled": enabled})
	view, err := s.cardView(r, customerID)
	if err != nil {
		handleStoreError(w, err, "cartão não encontrado")
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// handleMyCardRemove apaga o cartão salvo (e o débito automático).
func (s *Server) handleMyCardRemove(w http.ResponseWriter, r *http.Request) {
	customerID, _ := customerOf(r)
	s.removeCard(w, r, customerID)
}

func (s *Server) removeCard(w http.ResponseWriter, r *http.Request, customerID uuid.UUID) {
	if err := s.Payments.RemoveCard(r.Context(), customerID); err != nil {
		s.writePaymentError(w, err)
		return
	}
	s.recordBillingAudit(r, audit.ActionCardRemoved, map[string]any{"customerId": customerID})
	w.WriteHeader(http.StatusNoContent)
}

// handleCustomerCardAutoDebit: a central só desliga o débito automático;
// ligar é autorização do cliente.
func (s *Server) handleCustomerCardAutoDebit(w http.ResponseWriter, r *http.Request) {
	id, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}
	var req autoDebitRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	if req.Enabled {
		writeError(w, http.StatusBadRequest, "só o cliente autoriza o débito automático")
		return
	}
	s.setAutoDebit(w, r, id, false)
}

func (s *Server) handleCustomerCardRemove(w http.ResponseWriter, r *http.Request) {
	id, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}
	s.removeCard(w, r, id)
}
