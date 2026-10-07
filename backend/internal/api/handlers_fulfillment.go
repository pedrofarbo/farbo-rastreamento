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
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/topups"
)

// codeNotConnected leva o painel ao botão "Conectar Melhor Envios".
const codeNotConnected = "MELHORENVIO_NOT_CONNECTED"

// codeInsufficientBalance leva o painel a oferecer "Adicionar saldo".
const codeInsufficientBalance = "INSUFFICIENT_BALANCE"

// writeFulfillmentError traduz os erros do acompanhamento e do envio.
func writeFulfillmentError(w http.ResponseWriter, err error) {
	var rule fulfillment.RuleError
	var short fulfillment.BalanceError
	var apiErr *melhorenvio.APIError
	switch {
	case errors.As(err, &rule):
		writeError(w, http.StatusConflict, rule.Message)
	case errors.As(err, &short):
		writeJSON(w, http.StatusConflict, map[string]any{
			"error": short.Error(), "code": codeInsufficientBalance,
			"balanceCents": short.BalanceCents, "priceCents": short.PriceCents,
		})
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
	// PanelURL é o painel do Melhor Envios (onde fica a carteira).
	PanelURL string `json:"panelUrl"`
	// PayWithAbacate: a recarga por Pix pode sair do saldo da AbacatePay.
	PayWithAbacate bool `json:"payWithAbacate"`
}

func (s *Server) handleShippingIntegration(w http.ResponseWriter, r *http.Request) {
	cfg := s.Config.Shipping
	out := shippingIntegration{
		Configured: cfg.Enabled(), Sandbox: cfg.Sandbox, MissingOrigin: cfg.MissingOrigin(), RedirectURL: cfg.RedirectURL,
	}
	if s.Carrier != nil {
		out.CanConnect = s.Carrier.CanAuthorize() && !s.Carrier.UsesStaticToken()
		out.StaticToken = s.Carrier.UsesStaticToken()
		out.PanelURL = s.Carrier.PanelURL()
		out.PayWithAbacate = s.TopUps != nil && s.TopUps.PayWithAbacate()
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

// codeBalanceForbidden: o token não tem a permissão da carteira (conexão
// feita antes dela); conectar de novo autoriza.
const codeBalanceForbidden = "BALANCE_FORBIDDEN"

type shippingBalance struct {
	BalanceCents  int       `json:"balanceCents"`
	ReservedCents int       `json:"reservedCents"`
	DebtsCents    int       `json:"debtsCents"`
	CheckedAt     time.Time `json:"checkedAt"`
	PanelURL      string    `json:"panelUrl"`
}

// writeWalletError traduz as recusas da carteira do Melhor Envios.
func (s *Server) writeWalletError(w http.ResponseWriter, err error) {
	var apiErr *melhorenvio.APIError
	switch {
	case errors.Is(err, melhorenvio.ErrNotConnected):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error(), "code": codeNotConnected})
	case errors.As(err, &apiErr) && (apiErr.Status == http.StatusUnauthorized || apiErr.Status == http.StatusForbidden):
		s.Log.Warn("Melhor Envios recusou o acesso à carteira", "status", apiErr.Status, "err", apiErr.Message)
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": "o Melhor Envios não liberou a carteira para o aplicativo (" + apiErr.Message +
				"): desconecte e conecte de novo para autorizar",
			"code": codeBalanceForbidden,
		})
	case errors.As(err, &apiErr):
		writeError(w, http.StatusBadGateway, apiErr.Error())
	default:
		s.Log.Error("falha na carteira do Melhor Envios", "err", err)
		writeError(w, http.StatusBadGateway, "Melhor Envios indisponível; tente de novo")
	}
}

// handleShippingBalance mostra o saldo da carteira do Melhor Envios.
func (s *Server) handleShippingBalance(w http.ResponseWriter, r *http.Request) {
	if s.Carrier == nil {
		writeError(w, http.StatusServiceUnavailable, "integração não configurada")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	wallet, err := s.Carrier.Balance(ctx)
	if err != nil {
		s.writeWalletError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, shippingBalance{
		BalanceCents: wallet.Balance.Cents(), ReservedCents: wallet.Reserved.Cents(), DebtsCents: wallet.Debts.Cents(),
		CheckedAt: time.Now().UTC(), PanelURL: s.Carrier.PanelURL(),
	})
}

// Limites da recarga pelo painel: o máximo só evita um zero a mais.
const (
	minTopUpCents = 100
	maxTopUpCents = 1_000_000
)

// handleAddShippingBalance gera o Pix (ou o boleto, em nome da empresa) para
// pôr saldo na carteira. O pagamento é feito no Melhor Envios; depois de
// pago, ele volta para Pedidos.
func (s *Server) handleAddShippingBalance(w http.ResponseWriter, r *http.Request) {
	if s.Carrier == nil || s.TopUps == nil {
		writeError(w, http.StatusServiceUnavailable, "integração não configurada")
		return
	}
	var req struct {
		ValueCents int    `json:"valueCents"`
		Method     string `json:"method"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	if req.Method != melhorenvio.TopUpPix && req.Method != melhorenvio.TopUpBoleto {
		writeError(w, http.StatusBadRequest, "escolha Pix ou boleto")
		return
	}
	if req.ValueCents < minTopUpCents || req.ValueCents > maxTopUpCents {
		writeError(w, http.StatusBadRequest, "informe um valor entre R$ 1,00 e R$ 10.000,00")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	top, err := s.TopUps.Create(ctx, req.Method, req.ValueCents, actor(r))
	if err != nil {
		var apiErr *melhorenvio.APIError
		if errors.As(err, &apiErr) || errors.Is(err, melhorenvio.ErrNotConnected) {
			s.writeWalletError(w, err)
			return
		}
		handleStoreError(w, err, "")
		return
	}
	s.recordAudit(r, audit.ActionShippingBalanceAdded, nil, nil, map[string]any{
		"topUp": top.ID, "valueCents": top.ValueCents, "method": top.Method, "protocol": top.Protocol,
		"status": top.Status, "pixCode": top.PixCode != "",
	})
	writeJSON(w, http.StatusOK, struct {
		*topups.TopUp
		PanelURL string `json:"panelUrl"`
	}{top, s.Carrier.PanelURL()})
}

// handleTopUpEntry devolve a conta a pagar da recarga por Pix (criada na
// primeira vez), para pagar pela AbacatePay com o Pix dos fornecedores (a
// confirmação extra é pedida no envio). O copia-e-cola vem da resposta do
// Melhor Envios ou colado no painel.
func (s *Server) handleTopUpEntry(w http.ResponseWriter, r *http.Request) {
	if s.TopUps == nil || s.Finance == nil {
		writeError(w, http.StatusServiceUnavailable, "integração não configurada")
		return
	}
	id, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}
	var req struct {
		PixCode string `json:"pixCode"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	entryID, err := s.TopUps.Entry(r.Context(), id, req.PixCode, actor(r))
	if err != nil {
		financeError(w, err, "recarga não encontrada")
		return
	}
	entry, err := s.Finance.Entry(r.Context(), entryID)
	if err != nil {
		financeError(w, err, "conta não encontrada")
		return
	}
	writeJSON(w, http.StatusOK, entry)
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
