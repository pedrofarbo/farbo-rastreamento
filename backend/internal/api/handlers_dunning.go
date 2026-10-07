package api

import (
	"errors"
	"net/http"
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
	writeJSON(w, http.StatusOK, view)
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
	if err != nil || charge.InvoiceID != inv.ID {
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
