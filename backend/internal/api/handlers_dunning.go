package api

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/dunning"
)

// publicInvoiceView é a fatura no link de pagamento (sem login): o primeiro
// nome, o que é, o valor e o vencimento — nada além disso.
type publicInvoiceView struct {
	FirstName   string       `json:"firstName"`
	Description string       `json:"description"`
	AmountCents int          `json:"amountCents"`
	DueDate     billing.Date `json:"dueDate"`
	Status      string       `json:"status"`
	Overdue     bool         `json:"overdue"`
	DaysOverdue int          `json:"daysOverdue"`
	PaidAt      *time.Time   `json:"paidAt"`
	// OnlinePayment: o Pix sai pela AbacatePay; sem ela, o link ou o Pix
	// informados pela central na fatura (se houver).
	OnlinePayment bool   `json:"onlinePayment"`
	PaymentURL    string `json:"paymentUrl"`
	PixCode       string `json:"pixCode"`
	// Group: as faturas em aberto do cliente no mesmo vencimento (a fatura do
	// mês de uma frota, uma por veículo), para pagar todas com um Pix só.
	// Nulo quando é só esta.
	Group *publicInvoiceGroup `json:"group"`
}

// publicInvoiceGroup é a fatura do mês: o total e o que entra nele.
type publicInvoiceGroup struct {
	AmountCents int                 `json:"amountCents"`
	Items       []publicInvoiceItem `json:"items"`
}

type publicInvoiceItem struct {
	Description string `json:"description"`
	AmountCents int    `json:"amountCents"`
}

// invoiceGroup são as faturas em aberto do cliente que vencem no mesmo dia
// que esta (ela inclusa), na ordem em que foram geradas; nil se é só ela ou
// se ela não está em aberto.
func (s *Server) invoiceGroup(r *http.Request, inv *billing.Invoice) ([]*billing.Invoice, error) {
	if inv.Status != billing.InvoiceOpen {
		return nil, nil
	}
	list, err := s.Billing.ListInvoices(r.Context(), inv.CustomerID)
	if err != nil {
		return nil, err
	}
	var out []*billing.Invoice
	for _, other := range list {
		if other.Status == billing.InvoiceOpen && other.AmountCents > 0 && other.DueDate.Equal(inv.DueDate.Time) {
			out = append(out, other)
		}
	}
	if len(out) < 2 {
		return nil, nil
	}
	return out, nil
}

// linkInvoice é a fatura do link de pagamento da rota (link inválido: 404).
func (s *Server) linkInvoice(w http.ResponseWriter, r *http.Request) (*billing.Invoice, bool) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	if s.Dunning == nil {
		writeError(w, http.StatusNotFound, "link de pagamento não encontrado")
		return nil, false
	}
	id, ok := s.Dunning.InvoiceID(chi.URLParam(r, "token"))
	if !ok {
		writeError(w, http.StatusNotFound, "link de pagamento não encontrado")
		return nil, false
	}
	inv, err := s.Billing.GetInvoice(r.Context(), id)
	if err != nil {
		handleStoreError(w, err, "link de pagamento não encontrado")
		return nil, false
	}
	return inv, true
}

// handlePublicInvoice: a fatura do link de pagamento.
func (s *Server) handlePublicInvoice(w http.ResponseWriter, r *http.Request) {
	inv, ok := s.linkInvoice(w, r)
	if !ok {
		return
	}
	view := publicInvoiceView{
		Description: inv.Description, AmountCents: inv.AmountCents, DueDate: inv.DueDate, Status: inv.Status,
		Overdue: inv.Overdue, DaysOverdue: inv.DaysOverdue, PaidAt: inv.PaidAt,
		OnlinePayment: s.Payments.Enabled(), PaymentURL: inv.PaymentURL, PixCode: inv.PixCode,
	}
	if user, err := s.Auth.GetUser(r.Context(), inv.CustomerID); err == nil {
		if fields := strings.Fields(user.Name); len(fields) > 0 {
			view.FirstName = fields[0]
		}
	}
	group, err := s.invoiceGroup(r, inv)
	if err != nil {
		handleStoreError(w, err, "link de pagamento não encontrado")
		return
	}
	if group != nil {
		view.Group = &publicInvoiceGroup{Items: []publicInvoiceItem{}}
		for _, g := range group {
			view.Group.AmountCents += g.AmountCents
			view.Group.Items = append(view.Group.Items, publicInvoiceItem{Description: g.Description, AmountCents: g.AmountCents})
		}
	}
	writeJSON(w, http.StatusOK, view)
}

// handlePublicInvoiceGroupPix gera (ou reaproveita) um Pix só para todas as
// faturas em aberto do cliente que vencem no mesmo dia que a do link.
func (s *Server) handlePublicInvoiceGroupPix(w http.ResponseWriter, r *http.Request) {
	inv, ok := s.linkInvoice(w, r)
	if !ok {
		return
	}
	group, err := s.invoiceGroup(r, inv)
	if err != nil {
		handleStoreError(w, err, "link de pagamento não encontrado")
		return
	}
	if group == nil {
		// Só esta (ou já não está em aberto): o Pix dela.
		s.createPix(w, r, inv)
		return
	}
	ids := make([]uuid.UUID, len(group))
	for i, g := range group {
		ids[i] = g.ID
	}
	s.createGroupPix(w, r, inv.CustomerID, ids)
}

// handlePublicInvoicePix gera (ou reaproveita) o Pix da fatura do link.
func (s *Server) handlePublicInvoicePix(w http.ResponseWriter, r *http.Request) {
	inv, ok := s.linkInvoice(w, r)
	if !ok {
		return
	}
	s.createPix(w, r, inv)
}

// linkCharge confere que o Pix da rota é da fatura do link.
func (s *Server) linkCharge(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	inv, ok := s.linkInvoice(w, r)
	if !ok {
		return uuid.Nil, false
	}
	id, err := urlUUID(r, "chargeId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return uuid.Nil, false
	}
	charge, err := s.Payments.Charge(r.Context(), id)
	if err != nil || (charge.InvoiceID != inv.ID && !slices.Contains(charge.InvoiceIDs, inv.ID)) {
		writeError(w, http.StatusNotFound, "cobrança não encontrada")
		return uuid.Nil, false
	}
	return id, true
}

// handlePublicCharge: o status do Pix (a página pergunta a cada poucos segundos).
func (s *Server) handlePublicCharge(w http.ResponseWriter, r *http.Request) {
	if id, ok := s.linkCharge(w, r); ok {
		s.refreshCharge(w, r, id)
	}
}

// handlePublicChargeSimulate paga de mentira (só com a chave de testes).
func (s *Server) handlePublicChargeSimulate(w http.ResponseWriter, r *http.Request) {
	if id, ok := s.linkCharge(w, r); ok {
		s.simulateCharge(w, r, id)
	}
}

// handleRemindInvoice: a central manda um lembrete da fatura agora (e-mail e
// push, com o link de pagamento).
func (s *Server) handleRemindInvoice(w http.ResponseWriter, r *http.Request) {
	if s.Dunning == nil {
		writeError(w, http.StatusNotFound, "lembretes indisponíveis")
		return
	}
	id, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}
	principal, _ := auth.FromContext(r.Context())
	actor := principal.UserID
	reminder, err := s.Dunning.SendNow(r.Context(), id, &actor)
	if errors.Is(err, dunning.ErrNotOpen) {
		writeError(w, http.StatusConflict, "a fatura não está em aberto")
		return
	}
	if err != nil {
		handleStoreError(w, err, "fatura não encontrada")
		return
	}
	s.recordBillingAudit(r, audit.ActionInvoiceReminded, map[string]any{
		"invoiceId": id, "emailed": reminder.Emailed, "pushed": reminder.Pushed,
	})
	writeJSON(w, http.StatusOK, reminder)
}
