package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/fulfillment"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/leads"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/melhorenvio"
)

// codeNotConnected leva o painel ao botão "Conectar Melhor Envios".
const codeNotConnected = "MELHORENVIO_NOT_CONNECTED"

// writeFulfillmentError traduz os erros do acompanhamento e do envio.
func writeFulfillmentError(w http.ResponseWriter, err error) {
	var rule fulfillment.RuleError
	var apiErr *melhorenvio.APIError
	switch {
	case errors.As(err, &rule):
		writeError(w, http.StatusConflict, rule.Message)
	case errors.Is(err, database.ErrNotFound):
		writeError(w, http.StatusNotFound, "acompanhamento não encontrado")
	case errors.Is(err, fulfillment.ErrShippingDisabled):
		writeError(w, http.StatusServiceUnavailable, err.Error())
	case errors.Is(err, melhorenvio.ErrNotConnected):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error(), "code": codeNotConnected})
	case errors.As(err, &apiErr):
		// Recusa do Melhor Envios (saldo, documento, CEP...): a mensagem
		// dele vai para a tela.
		writeError(w, http.StatusBadGateway, apiErr.Error())
	default:
		handleStoreError(w, err, "acompanhamento não encontrado")
	}
}

func (s *Server) fulfillmentFromURL(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return uuid.Nil, false
	}
	return id, true
}

func actorOf(r *http.Request) uuid.UUID {
	principal, _ := auth.FromContext(r.Context())
	return principal.UserID
}

// handleListFulfillments é a fila da central: em andamento (padrão) ou todos.
func (s *Server) handleListFulfillments(w http.ResponseWriter, r *http.Request) {
	list, err := s.Fulfillment.Repo().List(r.Context(), r.URL.Query().Get("status") != "all")
	if err != nil {
		writeFulfillmentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleGetFulfillment(w http.ResponseWriter, r *http.Request) {
	id, ok := s.fulfillmentFromURL(w, r)
	if !ok {
		return
	}
	f, err := s.Fulfillment.Repo().Get(r.Context(), id)
	if err != nil {
		writeFulfillmentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, f)
}

type fulfillmentChangeRequest struct {
	Track  string `json:"track"`
	Status string `json:"status"`
	Note   string `json:"note"`
	// DeviceID vincula o aparelho configurado ao marcar "Configurado".
	DeviceID *uuid.UUID `json:"deviceId"`
}

// handleChangeFulfillment: a central avança (ou corrige) uma das linhas do
// tempo.
func (s *Server) handleChangeFulfillment(w http.ResponseWriter, r *http.Request) {
	id, ok := s.fulfillmentFromURL(w, r)
	if !ok {
		return
	}
	var req fulfillmentChangeRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	f, err := s.Fulfillment.Change(r.Context(), id, fulfillment.ChangeRequest{
		Track: req.Track, Status: req.Status, Note: req.Note, DeviceID: req.DeviceID,
	}, actorOf(r))
	if err != nil {
		writeFulfillmentError(w, err)
		return
	}
	if req.DeviceID != nil {
		s.vehiclesChanged(r)
	}
	s.recordAudit(r, audit.ActionFulfillmentChanged, &f.VehicleID, req.DeviceID, map[string]any{
		"fulfillmentId": f.ID, "customerId": f.CustomerID, "track": req.Track, "status": req.Status,
	})
	writeJSON(w, http.StatusOK, f)
}

// handleQuoteShipping cota o frete do rastreador configurado.
func (s *Server) handleQuoteShipping(w http.ResponseWriter, r *http.Request) {
	id, ok := s.fulfillmentFromURL(w, r)
	if !ok {
		return
	}
	quotes, err := s.Fulfillment.Quote(r.Context(), id)
	if err != nil {
		writeFulfillmentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, quotes)
}

// handleBuyLabel compra a etiqueta (saldo da carteira do Melhor Envios) e
// marca o rastreador como enviado.
func (s *Server) handleBuyLabel(w http.ResponseWriter, r *http.Request) {
	id, ok := s.fulfillmentFromURL(w, r)
	if !ok {
		return
	}
	var req struct {
		ServiceID int `json:"serviceId"`
	}
	// ServiceID 0 continua uma etiqueta já paga ("Concluir envio").
	if err := decodeJSON(w, r, &req); err != nil || req.ServiceID < 0 {
		writeError(w, http.StatusBadRequest, "escolha o serviço de frete")
		return
	}
	f, err := s.Fulfillment.BuyLabel(r.Context(), id, req.ServiceID, actorOf(r))
	if err != nil {
		writeFulfillmentError(w, err)
		return
	}
	if req.ServiceID > 0 {
		s.recordAudit(r, audit.ActionShippingLabelBought, &f.VehicleID, nil, map[string]any{
			"fulfillmentId": f.ID, "customerId": f.CustomerID, "service": f.ShippingService,
			"priceCents": f.ShippingPriceCents, "protocol": f.ShippingProtocol,
		})
	}
	writeJSON(w, http.StatusOK, f)
}

// handleSyncShipping consulta o rastreio agora.
func (s *Server) handleSyncShipping(w http.ResponseWriter, r *http.Request) {
	id, ok := s.fulfillmentFromURL(w, r)
	if !ok {
		return
	}
	f, err := s.Fulfillment.SyncOne(r.Context(), id)
	if err != nil {
		writeFulfillmentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, f)
}

// codeLabelNotPDF: a etiqueta veio como página para imprimir (o painel abre).
const codeLabelNotPDF = "LABEL_NOT_PDF"

// handleLabelPDF baixa a etiqueta comprada em PDF, com o nome do veículo e o
// código de rastreio no arquivo. Se o Melhor Envios entregar a página de
// impressão em vez do PDF, responde LABEL_NOT_PDF com o link, e o painel abre
// a impressão.
func (s *Server) handleLabelPDF(w http.ResponseWriter, r *http.Request) {
	id, ok := s.fulfillmentFromURL(w, r)
	if !ok {
		return
	}
	f, pdf, link, err := s.Fulfillment.Label(r.Context(), id)
	if errors.Is(err, fulfillment.ErrLabelNotPDF) {
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": "o Melhor Envios entregou a etiqueta como página para imprimir", "code": codeLabelNotPDF, "url": link,
		})
		return
	}
	if err != nil {
		writeFulfillmentError(w, err)
		return
	}
	name := "etiqueta"
	for _, part := range []string{f.VehicleName, f.TrackingCode} {
		if slug := leads.EventSlug(part); slug != "" {
			name += "-" + slug
		}
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name + ".pdf"}))
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(pdf)
}

// handleMyFulfillments: o cliente acompanha os próprios pedidos.
func (s *Server) handleMyFulfillments(w http.ResponseWriter, r *http.Request) {
	customerID, _ := customerOf(r)
	list, err := s.Fulfillment.Repo().ListByCustomer(r.Context(), customerID)
	if err != nil {
		writeFulfillmentError(w, err)
		return
	}
	views := make([]fulfillment.CustomerView, 0, len(list))
	for _, f := range list {
		views = append(views, f.CustomerView())
	}
	writeJSON(w, http.StatusOK, views)
}

// ---------------------------------------------------------------------------
// Conexão com o Melhor Envios (OAuth) e webhook
// ---------------------------------------------------------------------------

type shippingIntegration struct {
	// Configured: há credencial (aplicativo ou token pessoal).
	Configured bool `json:"configured"`
	// CanConnect: dá para autorizar pelo painel (aplicativo, sem token fixo).
	CanConnect   bool       `json:"canConnect"`
	StaticToken  bool       `json:"staticToken"`
	Connected    bool       `json:"connected"`
	Sandbox      bool       `json:"sandbox"`
	ExpiresAt    *time.Time `json:"expiresAt"`
	AccountName  string     `json:"accountName"`
	AccountEmail string     `json:"accountEmail"`
	// AccountError: conectado, mas a API recusou (token revogado...).
	AccountError  string   `json:"accountError"`
	MissingOrigin []string `json:"missingOrigin"`
	RedirectURL   string   `json:"redirectUrl"`
}

func (s *Server) handleShippingIntegration(w http.ResponseWriter, r *http.Request) {
	cfg := s.Config.Shipping
	out := shippingIntegration{
		Configured: cfg.Enabled(), Sandbox: cfg.Sandbox, MissingOrigin: cfg.MissingOrigin(), RedirectURL: cfg.RedirectURL,
	}
	if s.Carrier != nil {
		out.CanConnect = s.Carrier.CanAuthorize() && !s.Carrier.UsesStaticToken()
		out.StaticToken = s.Carrier.UsesStaticToken()
		connected, expires, err := s.Carrier.Status(r.Context())
		if err != nil {
			handleStoreError(w, err, "")
			return
		}
		out.Connected, out.ExpiresAt = connected, expires
		if connected {
			ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
			defer cancel()
			if account, err := s.Carrier.Account(ctx); err == nil {
				out.AccountName = account.FirstName + " " + account.LastName
				out.AccountEmail = account.Email
			} else {
				out.AccountError = err.Error()
				if errors.Is(err, melhorenvio.ErrNotConnected) {
					out.Connected = false
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// handleConnectShipping devolve a URL de autorização do Melhor Envios.
func (s *Server) handleConnectShipping(w http.ResponseWriter, r *http.Request) {
	if s.Carrier == nil || !s.Carrier.CanAuthorize() {
		writeError(w, http.StatusServiceUnavailable,
			"configure MELHORENVIO_CLIENT_ID e MELHORENVIO_CLIENT_SECRET no servidor")
		return
	}
	state, err := s.CarrierStore.NewState(r.Context(), actorOf(r))
	if err != nil {
		handleStoreError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": s.Carrier.AuthorizeURL(state)})
}

// handleShippingCallback recebe o navegador de volta do Melhor Envios, troca
// o code pelo token e volta para a fila de pedidos com o resultado.
func (s *Server) handleShippingCallback(w http.ResponseWriter, r *http.Request) {
	back := func(result, reason string) {
		q := url.Values{"melhorenvio": {result}}
		if reason != "" {
			q.Set("motivo", reason)
		}
		http.Redirect(w, r, s.Config.Mail.AppURL+"/pedidos?"+q.Encode(), http.StatusFound)
	}
	if s.Carrier == nil {
		back("erro", "integração não configurada no servidor")
		return
	}
	query := r.URL.Query()
	valid, err := s.CarrierStore.ConsumeState(r.Context(), query.Get("state"))
	if err != nil || !valid {
		s.Log.Warn("retorno do Melhor Envios com state inválido", "ip", clientIP(r))
		back("erro", "autorização expirada ou inválida; clique em Conectar de novo")
		return
	}
	if denied := query.Get("error"); denied != "" {
		back("erro", "autorização negada no Melhor Envios")
		return
	}
	if err := s.Carrier.ExchangeCode(r.Context(), query.Get("code")); err != nil {
		s.Log.Error("falha ao trocar o code do Melhor Envios", "err", err)
		back("erro", err.Error())
		return
	}
	s.Log.Info("Melhor Envios conectado")
	back("conectado", "")
}

func (s *Server) handleDisconnectShipping(w http.ResponseWriter, r *http.Request) {
	if s.Carrier == nil {
		writeError(w, http.StatusServiceUnavailable, "integração não configurada")
		return
	}
	if err := s.Carrier.Disconnect(r.Context()); err != nil {
		handleStoreError(w, err, "")
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}

// handleShippingWebhook recebe as mudanças de status das etiquetas. Só vale
// com a assinatura HMAC do Secret do aplicativo.
func (s *Server) handleShippingWebhook(w http.ResponseWriter, r *http.Request) {
	secret := s.Config.Shipping.ClientSecret
	if s.Carrier == nil || secret == "" {
		writeError(w, http.StatusServiceUnavailable, "integração não configurada")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	if !melhorenvio.VerifySignature(secret, body, r.Header.Get("X-ME-Signature")) {
		s.Log.Warn("webhook do Melhor Envios com assinatura inválida", "ip", clientIP(r))
		writeError(w, http.StatusUnauthorized, "assinatura inválida")
		return
	}
	var event struct {
		Event string                   `json:"event"`
		Data  melhorenvio.TrackingInfo `json:"data"`
	}
	if err := json.Unmarshal(body, &event); err != nil || event.Data.ID == "" {
		// Assinado pelo Melhor Envios, mas sem etiqueta: é o teste que ele
		// manda ao cadastrar o webhook (e que precisa de 200, senão o cadastro
		// falha com E-WBH-0002). Nada a aplicar.
		s.Log.Info("webhook do Melhor Envios sem etiqueta (teste de cadastro): ignorado", "event", event.Event)
		writeJSON(w, http.StatusOK, map[string]string{"status": "ignorado"})
		return
	}
	if err := s.Fulfillment.ApplyWebhook(r.Context(), event.Data); err != nil {
		if errors.Is(err, database.ErrNotFound) {
			// Etiqueta que não é de um pedido nosso: nada a fazer.
			writeJSON(w, http.StatusOK, map[string]string{"status": "ignorado"})
			return
		}
		s.Log.Error("falha ao aplicar o webhook do Melhor Envios", "order", event.Data.ID, "err", err)
		writeError(w, http.StatusInternalServerError, "erro interno")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
