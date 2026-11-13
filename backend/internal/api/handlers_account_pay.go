package api

import (
	"net/http"
	"slices"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/payments"
)

// O link de todas as faturas em aberto do cliente (sem login): a central
// copia na ficha e manda; a página mostra o primeiro nome, as faturas em
// aberto na hora (de qualquer vencimento) e o total, e gera um Pix ou um
// boleto só para todas. O link vale enquanto a conta existir.

// publicAccountView é o que a página do link da conta mostra.
type publicAccountView struct {
	FirstName   string              `json:"firstName"`
	AmountCents int                 `json:"amountCents"`
	Items       []publicAccountItem `json:"items"`
	// OnlinePayment: o Pix e o boleto saem pelo gateway; sem ele, a página
	// manda falar com a Farbo.
	OnlinePayment bool `json:"onlinePayment"`
}

type publicAccountItem struct {
	Description string       `json:"description"`
	AmountCents int          `json:"amountCents"`
	DueDate     billing.Date `json:"dueDate"`
	Overdue     bool         `json:"overdue"`
	DaysOverdue int          `json:"daysOverdue"`
}

// accountCustomer é o cliente do link da conta da rota (link inválido: 404).
func (s *Server) accountCustomer(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	if s.Dunning == nil {
		writeError(w, http.StatusNotFound, "link de pagamento não encontrado")
		return uuid.Nil, false
	}
	id, ok := s.Dunning.AccountCustomerID(chi.URLParam(r, "token"))
	if !ok {
		writeError(w, http.StatusNotFound, "link de pagamento não encontrado")
		return uuid.Nil, false
	}
	return id, true
}

// openInvoices são as faturas em aberto do cliente (as de valor zero não
// contam), da que vence antes à que vence depois.
func (s *Server) openInvoices(r *http.Request, customerID uuid.UUID) ([]*billing.Invoice, error) {
	list, err := s.Billing.ListInvoices(r.Context(), customerID)
	if err != nil {
		return nil, err
	}
	var out []*billing.Invoice
	for _, inv := range list {
		if inv.Status == billing.InvoiceOpen && inv.AmountCents > 0 {
			out = append(out, inv)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].DueDate.Before(out[j].DueDate) })
	return out, nil
}

// handlePublicAccount: as faturas em aberto do link da conta.
func (s *Server) handlePublicAccount(w http.ResponseWriter, r *http.Request) {
	customerID, ok := s.accountCustomer(w, r)
	if !ok {
		return
	}
	user, err := s.Auth.GetUser(r.Context(), customerID)
	if err != nil {
		handleStoreError(w, err, "link de pagamento não encontrado")
		return
	}
	open, err := s.openInvoices(r, customerID)
	if err != nil {
		handleStoreError(w, err, "link de pagamento não encontrado")
		return
	}
	view := publicAccountView{Items: []publicAccountItem{}, OnlinePayment: s.Payments != nil && s.Payments.Enabled()}
	if fields := strings.Fields(user.Name); len(fields) > 0 {
		view.FirstName = fields[0]
	}
	for _, inv := range open {
		view.AmountCents += inv.AmountCents
		view.Items = append(view.Items, publicAccountItem{Description: inv.Description, AmountCents: inv.AmountCents,
			DueDate: inv.DueDate, Overdue: inv.Overdue, DaysOverdue: inv.DaysOverdue})
	}
	writeJSON(w, http.StatusOK, view)
}

// handlePublicAccountPix e handlePublicAccountBoleto geram (ou reaproveitam)
// um Pix ou um boleto só para todas as faturas em aberto do cliente.
func (s *Server) handlePublicAccountPix(w http.ResponseWriter, r *http.Request) {
	s.accountCharge(w, r, payments.MethodPix)
}

func (s *Server) handlePublicAccountBoleto(w http.ResponseWriter, r *http.Request) {
	s.accountCharge(w, r, payments.MethodBoleto)
}

func (s *Server) accountCharge(w http.ResponseWriter, r *http.Request, method string) {
	customerID, ok := s.accountCustomer(w, r)
	if !ok {
		return
	}
	open, err := s.openInvoices(r, customerID)
	if err != nil {
		handleStoreError(w, err, "link de pagamento não encontrado")
		return
	}
	switch len(open) {
	case 0:
		writeError(w, http.StatusConflict, "não há fatura em aberto: está tudo pago")
	case 1:
		s.createCharge(w, r, open[0], method)
	default:
		ids := make([]uuid.UUID, len(open))
		for i, inv := range open {
			ids[i] = inv.ID
		}
		s.createGroupCharge(w, r, customerID, ids, method)
	}
}

// accountChargeID confere que a cobrança da rota é de uma fatura do cliente
// do link.
func (s *Server) accountChargeID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	customerID, ok := s.accountCustomer(w, r)
	if !ok {
		return uuid.Nil, false
	}
	id, err := urlUUID(r, "chargeId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return uuid.Nil, false
	}
	charge, err := s.Payments.Charge(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "cobrança não encontrada")
		return uuid.Nil, false
	}
	invoices := append([]uuid.UUID{charge.InvoiceID}, charge.InvoiceIDs...)
	mine := slices.ContainsFunc(invoices, func(invoiceID uuid.UUID) bool {
		inv, err := s.Billing.GetInvoice(r.Context(), invoiceID)
		return err == nil && inv.CustomerID == customerID
	})
	if !mine {
		writeError(w, http.StatusNotFound, "cobrança não encontrada")
		return uuid.Nil, false
	}
	return id, true
}

// handlePublicAccountCharge: o status da cobrança (a página pergunta a cada poucos segundos).
func (s *Server) handlePublicAccountCharge(w http.ResponseWriter, r *http.Request) {
	if id, ok := s.accountChargeID(w, r); ok {
		s.refreshCharge(w, r, id)
	}
}

// handlePublicAccountChargeSimulate paga de mentira (só com a chave de testes).
func (s *Server) handlePublicAccountChargeSimulate(w http.ResponseWriter, r *http.Request) {
	if id, ok := s.accountChargeID(w, r); ok {
		s.simulateCharge(w, r, id)
	}
}
