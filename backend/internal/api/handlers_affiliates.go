package api

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/affiliates"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
)

// Programa de afiliados: o admin cria os links e muda os valores; quem se
// cadastra pelo link fica com o afiliado; o fechamento do mês vira conta a
// pagar na Empresa. Ver o pacote affiliates.

func affiliateError(w http.ResponseWriter, err error, notFound string) {
	var v affiliates.ValidationError
	if errors.As(err, &v) {
		writeError(w, http.StatusBadRequest, v.Message)
		return
	}
	handleStoreError(w, err, notFound)
}

func (s *Server) auditAffiliate(r *http.Request, op string, meta map[string]any) {
	meta["op"] = op
	s.recordAudit(r, audit.ActionAffiliateChanged, nil, nil, meta)
}

// referralFor é o afiliado do link de indicação (nil se não há, ou se o
// link é de um afiliado inativo: a pessoa entra normalmente).
func (s *Server) referralFor(r *http.Request, code string) *uuid.UUID {
	if s.Affiliates == nil || code == "" {
		return nil
	}
	_, id, err := s.Affiliates.Lookup(r.Context(), code)
	if err != nil {
		s.Log.Error("falha ao achar o afiliado do link", "code", code, "err", err)
		return nil
	}
	return id
}

// handlePublicAffiliate: o "Indicado por @fulano" da tela de cadastro.
func (s *Server) handlePublicAffiliate(w http.ResponseWriter, r *http.Request) {
	p, _, err := s.Affiliates.Lookup(r.Context(), chi.URLParam(r, "code"))
	if err != nil {
		affiliateError(w, err, "")
		return
	}
	if p == nil {
		writeError(w, http.StatusNotFound, "link de indicação não encontrado")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// handlePartnerReport: a página do afiliado, pelo link secreto. Sem dado
// pessoal de ninguém: só os números.
func (s *Server) handlePartnerReport(w http.ResponseWriter, r *http.Request) {
	report, err := s.Affiliates.Report(r.Context(), chi.URLParam(r, "token"))
	if err != nil {
		affiliateError(w, err, "")
		return
	}
	if report == nil {
		writeError(w, http.StatusNotFound, "link inválido: peça o novo à Farbo")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	writeJSON(w, http.StatusOK, report)
}

// handlePartnerQR: o QR Code do link de indicação do afiliado, pela página
// dele (link secreto), para o flyer: ?format=svg (vetor, para a gráfica) ou
// png; ?download=1 baixa o arquivo. Afiliado pausado não tem QR.
func (s *Server) handlePartnerQR(w http.ResponseWriter, r *http.Request) {
	code, err := s.Affiliates.ReferralCode(r.Context(), chi.URLParam(r, "token"))
	if err != nil {
		affiliateError(w, err, "")
		return
	}
	if code == "" {
		writeError(w, http.StatusNotFound, "link inválido ou pausado: fale com a Farbo")
		return
	}
	w.Header().Set("Referrer-Policy", "no-referrer")
	writeQR(w, s.siteURL()+"/indicacao/"+code, "qrcode-indicacao-"+code, r.URL.Query().Get("format"),
		r.URL.Query().Get("download") == "1")
}

// siteURL é o endereço público do site (os links de indicação).
func (s *Server) siteURL() string {
	if s.Config != nil && s.Config.Leads.SiteURL != "" {
		return s.Config.Leads.SiteURL
	}
	return "https://farborastreadores.com.br"
}

func (s *Server) handleListAffiliates(w http.ResponseWriter, r *http.Request) {
	list, err := s.Affiliates.List(r.Context())
	if err != nil {
		affiliateError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleCreateAffiliate(w http.ResponseWriter, r *http.Request) {
	var in affiliates.Input
	if !decodeOr400(w, r, &in) {
		return
	}
	a, err := s.Affiliates.Create(r.Context(), in)
	if err != nil {
		affiliateError(w, err, "")
		return
	}
	s.auditAffiliate(r, "create", map[string]any{"affiliateId": a.ID, "code": a.Code, "commissionCents": a.CommissionCents})
	writeJSON(w, http.StatusCreated, a)
}

func (s *Server) handleUpdateAffiliate(w http.ResponseWriter, r *http.Request) {
	id, ok := idOr400(w, r)
	if !ok {
		return
	}
	var in affiliates.Input
	if !decodeOr400(w, r, &in) {
		return
	}
	a, err := s.Affiliates.Update(r.Context(), id, in)
	if err != nil {
		affiliateError(w, err, "afiliado não encontrado")
		return
	}
	s.auditAffiliate(r, "update", map[string]any{
		"affiliateId": a.ID, "code": a.Code, "commissionCents": a.CommissionCents, "active": a.Active,
	})
	writeJSON(w, http.StatusOK, a)
}

// handleAffiliateToken troca o link secreto da página do afiliado.
func (s *Server) handleAffiliateToken(w http.ResponseWriter, r *http.Request) {
	id, ok := idOr400(w, r)
	if !ok {
		return
	}
	a, err := s.Affiliates.RegenerateToken(r.Context(), id)
	if err != nil {
		affiliateError(w, err, "afiliado não encontrado")
		return
	}
	s.auditAffiliate(r, "report-token", map[string]any{"affiliateId": a.ID})
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) handleAffiliateSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.Affiliates.Settings(r.Context())
	if err != nil {
		affiliateError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (s *Server) handleSaveAffiliateSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DefaultCommissionCents int `json:"defaultCommissionCents"`
		// ApplyToAll: o valor novo vale também para os afiliados de agora.
		ApplyToAll bool `json:"applyToAll"`
	}
	if !decodeOr400(w, r, &req) {
		return
	}
	settings, err := s.Affiliates.SaveSettings(r.Context(), req.DefaultCommissionCents, req.ApplyToAll)
	if err != nil {
		affiliateError(w, err, "")
		return
	}
	s.auditAffiliate(r, "settings", map[string]any{
		"defaultCommissionCents": req.DefaultCommissionCents, "applyToAll": req.ApplyToAll,
	})
	writeJSON(w, http.StatusOK, settings)
}

func (s *Server) handleAffiliateClosingPreview(w http.ResponseWriter, r *http.Request) {
	lines, err := s.Affiliates.ClosingPreview(r.Context(), r.URL.Query().Get("month"))
	if err != nil {
		affiliateError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, lines)
}

func (s *Server) handleAffiliateClose(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Month   string       `json:"month"`
		DueDate billing.Date `json:"dueDate"`
	}
	if !decodeOr400(w, r, &req) {
		return
	}
	payouts, err := s.Affiliates.Close(r.Context(), req.Month, req.DueDate, actor(r))
	if err != nil {
		affiliateError(w, err, "")
		return
	}
	var total int64
	for _, p := range payouts {
		total += p.AmountCents
	}
	s.auditAffiliate(r, "close", map[string]any{"month": req.Month, "payouts": len(payouts), "amountCents": total})
	writeJSON(w, http.StatusCreated, payouts)
}

func (s *Server) handleAffiliatePayouts(w http.ResponseWriter, r *http.Request) {
	list, err := s.Affiliates.Payouts(r.Context())
	if err != nil {
		affiliateError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleUndoAffiliatePayout(w http.ResponseWriter, r *http.Request) {
	id, ok := idOr400(w, r)
	if !ok {
		return
	}
	if err := s.Affiliates.UndoPayout(r.Context(), id); err != nil {
		affiliateError(w, err, "fechamento não encontrado")
		return
	}
	s.auditAffiliate(r, "undo-payout", map[string]any{"payoutId": id})
	w.WriteHeader(http.StatusNoContent)
}

// handleSetCustomerAffiliate: o admin diz quem indicou o cliente (nulo tira).
func (s *Server) handleSetCustomerAffiliate(w http.ResponseWriter, r *http.Request) {
	id, ok := idOr400(w, r)
	if !ok {
		return
	}
	var req struct {
		AffiliateID *uuid.UUID `json:"affiliateId"`
	}
	if !decodeOr400(w, r, &req) {
		return
	}
	referral, err := s.Affiliates.SetCustomer(r.Context(), id, req.AffiliateID)
	if err != nil {
		affiliateError(w, err, "cliente não encontrado")
		return
	}
	s.auditAffiliate(r, "customer", map[string]any{"customerId": id, "affiliateId": req.AffiliateID})
	writeJSON(w, http.StatusOK, map[string]any{"affiliate": referral})
}
