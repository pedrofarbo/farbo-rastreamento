package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/payments"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/payments/abacatepay"
)

// writePaymentError traduz os erros de pagamento em respostas HTTP.
func (s *Server) writePaymentError(w http.ResponseWriter, err error) {
	var apiErr *abacatepay.APIError
	switch {
	case errors.Is(err, payments.ErrDisabled):
		writeError(w, http.StatusServiceUnavailable, "o pagamento online não está disponível no momento")
	case errors.Is(err, payments.ErrInvoiceNotOpen):
		writeError(w, http.StatusConflict, "esta fatura não está mais em aberto")
	case errors.Is(err, payments.ErrNotDevMode),
		errors.Is(err, payments.ErrNotRefundable),
		errors.Is(err, payments.ErrRefundPending):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, database.ErrNotFound):
		writeError(w, http.StatusNotFound, "cobrança não encontrada")
	case errors.As(err, &apiErr):
		s.Log.Warn("AbacatePay recusou a operação", "status", apiErr.HTTPStatus, "err", apiErr.Message)
		// Recusa de regra de negócio (4xx, menos credencial) é conflito que a
		// pessoa entende; o resto é falha do provedor.
		status := http.StatusBadGateway
		if apiErr.HTTPStatus >= 400 && apiErr.HTTPStatus < 500 && apiErr.HTTPStatus != http.StatusUnauthorized {
			status = http.StatusConflict
		}
		writeError(w, status, "o provedor de pagamento recusou a operação: "+providerMessage(apiErr.Message))
	default:
		s.Log.Error("falha no pagamento online", "err", err)
		writeError(w, http.StatusBadGateway, "não foi possível falar com o provedor de pagamento agora; tente de novo em instantes")
	}
}

// providerMessage traduz os códigos de erro conhecidos da AbacatePay; as
// mensagens que ela já manda em português passam como estão.
func providerMessage(message string) string {
	switch strings.TrimSpace(message) {
	case "INSUFFICIENT_FUNDS":
		return "saldo insuficiente na conta da AbacatePay para devolver este valor"
	case "TRANSACTION_UNDER_DISPUTE":
		return "o pagamento está em disputa e não pode ser estornado agora"
	}
	return message
}

// payerFor monta os dados do pagador a partir do cadastro do cliente.
func (s *Server) payerFor(r *http.Request, customerID uuid.UUID) *payments.Payer {
	user, err := s.Auth.GetUser(r.Context(), customerID)
	if err != nil {
		return nil
	}
	return &payments.Payer{Name: user.Name, Email: user.Email, Document: user.Document, Phone: user.Phone}
}

// ---------------------------------------------------------------------------
// Cliente
// ---------------------------------------------------------------------------

// myInvoice confere que a fatura da rota é do cliente logado. Fatura alheia
// responde 404, como se não existisse.
func (s *Server) myInvoice(w http.ResponseWriter, r *http.Request) (*billing.Invoice, bool) {
	customerID, _ := customerOf(r)
	id, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return nil, false
	}
	inv, err := s.Billing.GetInvoice(r.Context(), id)
	if err != nil || inv.CustomerID != customerID {
		writeError(w, http.StatusNotFound, "fatura não encontrada")
		return nil, false
	}
	return inv, true
}

// myCharge confere que a cobrança da rota é de uma fatura do cliente logado.
func (s *Server) myCharge(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	customerID, _ := customerOf(r)
	id, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return uuid.Nil, false
	}
	charge, err := s.Payments.Charge(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "cobrança não encontrada")
		return uuid.Nil, false
	}
	inv, err := s.Billing.GetInvoice(r.Context(), charge.InvoiceID)
	if err != nil || inv.CustomerID != customerID {
		writeError(w, http.StatusNotFound, "cobrança não encontrada")
		return uuid.Nil, false
	}
	return id, true
}

// handleMyInvoicePix gera (ou reaproveita) o Pix para o cliente pagar a
// fatura. Continua disponível com o acesso suspenso: é assim que ele volta.
func (s *Server) handleMyInvoicePix(w http.ResponseWriter, r *http.Request) {
	inv, ok := s.myInvoice(w, r)
	if !ok {
		return
	}
	s.createPix(w, r, inv)
}

// handleMyCharge devolve o status do Pix, consultando o provedor. A tela
// chama a cada poucos segundos enquanto o QR Code está aberto.
func (s *Server) handleMyCharge(w http.ResponseWriter, r *http.Request) {
	id, ok := s.myCharge(w, r)
	if !ok {
		return
	}
	s.refreshCharge(w, r, id)
}

func (s *Server) handleMyChargeSimulate(w http.ResponseWriter, r *http.Request) {
	id, ok := s.myCharge(w, r)
	if !ok {
		return
	}
	s.simulateCharge(w, r, id)
}

// ---------------------------------------------------------------------------
// Central
// ---------------------------------------------------------------------------

// handleAdminInvoicePix gera o Pix pela central — para mandar ao cliente por
// outro canal, por exemplo.
func (s *Server) handleAdminInvoicePix(w http.ResponseWriter, r *http.Request) {
	id, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}
	inv, err := s.Billing.GetInvoice(r.Context(), id)
	if err != nil {
		handleStoreError(w, err, "fatura não encontrada")
		return
	}
	s.createPix(w, r, inv)
}

func (s *Server) handleAdminCharge(w http.ResponseWriter, r *http.Request) {
	id, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}
	s.refreshCharge(w, r, id)
}

func (s *Server) handleAdminChargeSimulate(w http.ResponseWriter, r *http.Request) {
	id, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}
	s.simulateCharge(w, r, id)
}

type refundRequest struct {
	Reason string `json:"reason"`
}

// handleAdminChargeRefund devolve ao cliente o valor de um Pix pago (estorno
// integral na AbacatePay). Se foi o Pix que quitou a fatura, ela volta a
// ficar em aberto.
func (s *Server) handleAdminChargeRefund(w http.ResponseWriter, r *http.Request) {
	id, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}
	var req refundRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" || len([]rune(reason)) > 500 {
		writeError(w, http.StatusBadRequest, "informe o motivo do estorno (até 500 caracteres)")
		return
	}

	charge, err := s.Payments.RefundCharge(r.Context(), id, reason)
	if err != nil {
		s.writePaymentError(w, err)
		return
	}
	s.recordBillingAudit(r, audit.ActionPaymentRefundAsked, map[string]any{
		"invoiceId": charge.InvoiceID, "chargeId": charge.ID, "providerChargeId": charge.ProviderChargeID,
		"refundId": charge.RefundID, "amountCents": charge.AmountCents, "reason": reason,
		"status": charge.Status,
	})
	writeJSON(w, http.StatusOK, charge)
}

// ---------------------------------------------------------------------------
// Comum
// ---------------------------------------------------------------------------

func (s *Server) createPix(w http.ResponseWriter, r *http.Request, inv *billing.Invoice) {
	charge, err := s.Payments.PixForInvoice(r.Context(), inv.ID, s.payerFor(r, inv.CustomerID))
	if err != nil {
		s.writePaymentError(w, err)
		return
	}
	s.recordBillingAudit(r, audit.ActionPaymentPixCreated, map[string]any{
		"customerId": inv.CustomerID, "invoiceId": inv.ID, "chargeId": charge.ID,
		"amountCents": charge.AmountCents, "devMode": charge.DevMode,
	})
	writeJSON(w, http.StatusOK, charge)
}

func (s *Server) refreshCharge(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	charge, err := s.Payments.Refresh(r.Context(), id, false)
	if err != nil {
		s.writePaymentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, charge)
}

func (s *Server) simulateCharge(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	charge, err := s.Payments.Simulate(r.Context(), id)
	if err != nil {
		s.writePaymentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, charge)
}

// ---------------------------------------------------------------------------
// Webhook da AbacatePay
// ---------------------------------------------------------------------------

// handleAbacatePayWebhook recebe as notificações de pagamento. Duas
// verificações antes de qualquer coisa: o segredo em ?webhookSecret= (que
// só nós e a AbacatePay conhecemos) e a assinatura HMAC do corpo. Mesmo
// assim o conteúdo não decide nada — o serviço reconsulta a API.
func (s *Server) handleAbacatePayWebhook(w http.ResponseWriter, r *http.Request) {
	if !s.Payments.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "pagamento online não configurado")
		return
	}
	cfg := s.Config.Payments
	if !abacatepay.VerifySecret(r.URL.Query().Get("webhookSecret"), cfg.WebhookSecret) {
		s.Log.Warn("webhook da AbacatePay com segredo inválido ou ausente", "ip", clientIP(r))
		writeError(w, http.StatusUnauthorized, "webhook não autorizado")
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	key := cfg.WebhookPublicKey
	if key == "" {
		key = abacatepay.PublicWebhookKey
	}
	if !abacatepay.VerifySignature(body, r.Header.Get(abacatepay.SignatureHeader), key) {
		s.Log.Warn("webhook da AbacatePay com assinatura inválida", "ip", clientIP(r))
		writeError(w, http.StatusUnauthorized, "assinatura inválida")
		return
	}

	event, err := abacatepay.ParseEvent(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "evento inválido")
		return
	}

	// A AbacatePay reenvia até 7 vezes; evento já processado só confirma.
	if seen, err := s.Payments.WebhookSeen(r.Context(), event.ID); err == nil && seen {
		writeJSON(w, http.StatusOK, map[string]string{"status": "duplicado"})
		return
	}
	handle := s.Payments.HandleWebhook
	if event.IsTransferEvent() {
		// Os Pix enviados a fornecedores (Empresa).
		handle = func(ctx context.Context, ev *abacatepay.Event) error {
			if s.Finance == nil {
				return nil
			}
			return s.Finance.HandleTransferWebhook(ctx, ev.TransferIDs())
		}
	}
	if err := handle(r.Context(), event); err != nil {
		// 500 faz a AbacatePay tentar de novo mais tarde.
		s.Log.Error("falha ao processar webhook da AbacatePay", "event", event.Event, "id", event.ID, "err", err)
		writeError(w, http.StatusInternalServerError, "falha ao processar o evento")
		return
	}
	if err := s.Payments.RecordWebhook(r.Context(), event); err != nil {
		s.Log.Warn("webhook processado, mas não registrado", "id", event.ID, "err", err)
	}
	s.Log.Info("webhook da AbacatePay processado", "event", event.Event, "id", event.ID, "dev_mode", event.DevMode)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
