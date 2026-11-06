package api

import (
	"errors"
	"net/http"
	"strings"

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

// handlePublicRepresentatives: os representantes oficiais autorizados, na
// landing (os que a central marcou no afiliado).
func (s *Server) handlePublicRepresentatives(w http.ResponseWriter, r *http.Request) {
	list, err := s.Affiliates.Representatives(r.Context())
	if err != nil {
		affiliateError(w, err, "")
		return
	}
	// A lista muda pouco: um minuto de cache poupa o banco em dia de tráfego.
	w.Header().Set("Cache-Control", "public, max-age=60")
	writeJSON(w, http.StatusOK, list)
}

// partnerSessionHeader leva a sessão da página do afiliado com senha.
const partnerSessionHeader = "X-Partner-Session"

// partnerAllowed confere a senha da página do afiliado: sem senha, o link
// basta; com senha, a sessão da entrada (sem ela, 401 com PARTNER_PASSWORD,
// e a página pede a senha). O QR Code fica fora: ele só mostra o link de
// indicação, que é público.
func (s *Server) partnerAllowed(w http.ResponseWriter, r *http.Request) bool {
	err := s.Affiliates.CheckPartner(r.Context(), chi.URLParam(r, "token"), r.Header.Get(partnerSessionHeader))
	if errors.Is(err, affiliates.ErrPartnerPassword) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error(), "code": "PARTNER_PASSWORD"})
		return false
	}
	if err != nil {
		affiliateError(w, err, "")
		return false
	}
	return true
}

// handlePartnerSession: a entrada na página do afiliado com a senha; devolve
// a sessão (30 dias), que a página manda em X-Partner-Session.
func (s *Server) handlePartnerSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	session, expires, err := s.Affiliates.PartnerLogin(r.Context(), chi.URLParam(r, "token"), req.Password)
	if errors.Is(err, affiliates.ErrWrongPartnerPassword) {
		s.Log.Info("senha errada na página do afiliado", "ip", clientIP(r))
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error(), "code": "WRONG_PASSWORD"})
		return
	}
	if err != nil {
		affiliateError(w, err, "link inválido: peça o novo à Farbo")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"session": session, "expiresAt": expires})
}

// handlePartnerInstallPrices: o representante que recebe direto define os
// valores da instalação e da desinstalação, pela página dele (com a senha,
// se a página pede). Os valores só aparecem para o cliente.
func (s *Server) handlePartnerInstallPrices(w http.ResponseWriter, r *http.Request) {
	if !s.partnerAllowed(w, r) {
		return
	}
	var req struct {
		InstallCents   int `json:"installCents"`
		UninstallCents int `json:"uninstallCents"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	prices, err := s.Affiliates.SetPartnerInstall(r.Context(), chi.URLParam(r, "token"), req.InstallCents, req.UninstallCents)
	if err != nil {
		affiliateError(w, err, "link inválido: peça o novo à Farbo")
		return
	}
	s.Log.Info("representante mudou os valores da instalação", "install", prices.InstallCents, "uninstall", prices.UninstallCents)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, prices)
}

// handlePartnerCustomers: os clientes do afiliado na página dele (com a
// senha): o nome, os veículos com a placa e o rastreador, e o pagamento mês
// a mês.
func (s *Server) handlePartnerCustomers(w http.ResponseWriter, r *http.Request) {
	if !s.partnerAllowed(w, r) {
		return
	}
	list, err := s.Affiliates.PartnerCustomers(r.Context(), chi.URLParam(r, "token"))
	if errors.Is(err, affiliates.ErrCustomersNeedPassword) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error(), "code": "PASSWORD_REQUIRED"})
		return
	}
	if err != nil {
		affiliateError(w, err, "link inválido: peça o novo à Farbo")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, list)
}

// handlePartnerReport: a página do afiliado, pelo link secreto. Sem dado
// pessoal de ninguém: só os números.
func (s *Server) handlePartnerReport(w http.ResponseWriter, r *http.Request) {
	if !s.partnerAllowed(w, r) {
		return
	}
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
	s.auditAffiliate(r, "create", map[string]any{"affiliateId": a.ID, "code": a.Code, "commissionCents": a.CommissionCents,
		"reportPassword": a.ReportPassword})
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
	// A senha nunca vai para a auditoria: só se a página pede senha e se ela mudou.
	s.auditAffiliate(r, "update", map[string]any{
		"affiliateId": a.ID, "code": a.Code, "commissionCents": a.CommissionCents, "active": a.Active,
		"reportPassword": a.ReportPassword, "reportPasswordChanged": in.ReportPassword != nil,
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
		// CustomerRewardCents: o crédito do indique e ganhe (sem ele, fica).
		CustomerRewardCents *int `json:"customerRewardCents"`
		// CommissionTaxBps: os impostos que saem da mensalidade antes do
		// percentual dos afiliados (sem ele, fica).
		CommissionTaxBps *int `json:"commissionTaxBps"`
	}
	if !decodeOr400(w, r, &req) {
		return
	}
	if req.CustomerRewardCents != nil {
		if err := s.Affiliates.SaveCustomerReward(r.Context(), *req.CustomerRewardCents); err != nil {
			affiliateError(w, err, "")
			return
		}
	}
	if req.CommissionTaxBps != nil {
		if err := s.Affiliates.SaveCommissionTax(r.Context(), *req.CommissionTaxBps); err != nil {
			affiliateError(w, err, "")
			return
		}
	}
	settings, err := s.Affiliates.SaveSettings(r.Context(), req.DefaultCommissionCents, req.ApplyToAll)
	if err != nil {
		affiliateError(w, err, "")
		return
	}
	s.auditAffiliate(r, "settings", map[string]any{
		"defaultCommissionCents": req.DefaultCommissionCents, "applyToAll": req.ApplyToAll,
		"customerRewardCents": settings.CustomerRewardCents, "commissionTaxBps": settings.CommissionTaxBps,
	})
	writeJSON(w, http.StatusOK, settings)
}

// handleMyReferral: o indique e ganhe do cliente (o link, quem veio por ele
// e os créditos). Só o titular, e não o associado (quem paga é a
// organização).
func (s *Server) handleMyReferral(w http.ResponseWriter, r *http.Request) {
	customerID, _ := customerOf(r)
	program, err := s.Affiliates.CustomerProgram(r.Context(), customerID)
	if errors.Is(err, affiliates.ErrNotEligible) {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	if err != nil {
		affiliateError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"program": program, "url": strings.TrimRight(s.Config.Mail.AppURL, "/") + "/indicacao/" + program.Code,
	})
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

// handlePartnerRecipient: o afiliado se cadastra como recebedor do split,
// pela página dele (link secreto). Os dados vão direto para a Pagar.me.
func (s *Server) handlePartnerRecipient(w http.ResponseWriter, r *http.Request) {
	if !s.partnerAllowed(w, r) {
		return
	}
	var in affiliates.RecipientInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	view, err := s.Affiliates.RegisterRecipient(r.Context(), chi.URLParam(r, "token"), in)
	if err != nil {
		affiliateError(w, err, "link inválido: peça o novo à Farbo")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, view)
}

// handlePartnerKYCLink: o link da prova de vida (vale 20 minutos).
func (s *Server) handlePartnerKYCLink(w http.ResponseWriter, r *http.Request) {
	if !s.partnerAllowed(w, r) {
		return
	}
	link, err := s.Affiliates.RecipientKYCLink(r.Context(), chi.URLParam(r, "token"))
	if err != nil {
		affiliateError(w, err, "link inválido: peça o novo à Farbo")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"url": link.URL, "base64": link.Base64, "expirationDate": link.ExpirationDate})
}
