package api

import (
	"fmt"
	"net/http"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/addresses"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	ws "github.com/pedrofarbo/farbo-rastreamento/backend/internal/websocket"
)

// codeAccountSuspended acompanha o 402 para o painel mostrar a tela de
// acesso suspenso (e não um erro genérico).
const codeAccountSuspended = "ACCOUNT_SUSPENDED"

// requireActiveCustomer barra o cliente com fatura atrasada além do limite
// configurado. A equipe da central passa direto. O rastreamento continua
// sendo gravado; só o acesso do cliente fica bloqueado até o pagamento.
func (s *Server) requireActiveCustomer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		customerID, isCustomer := customerOf(r)
		if !isCustomer {
			next.ServeHTTP(w, r)
			return
		}

		suspended, err := s.Billing.IsSuspended(r.Context(), customerID)
		if err != nil {
			s.Log.Error("falha ao verificar suspensão do cliente", "user", customerID, "err", err)
			writeError(w, http.StatusInternalServerError, "erro interno")
			return
		}
		if suspended {
			writeJSON(w, http.StatusPaymentRequired, map[string]string{
				"error": fmt.Sprintf("acesso suspenso: há fatura vencida há mais de %d dias. "+
					"Regularize o pagamento para voltar a acompanhar seus veículos",
					s.Billing.SuspendAfterDays()),
				"code": codeAccountSuspended,
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// websocketScope define o que cada conexão recebe: a equipe, tudo; o
// cliente, só as mensagens dos veículos e rastreadores dele — e, dos que
// compartilharam com ele, só a posição e a situação (nada de eventos).
// Mensagem sem veículo nem rastreador não vai para cliente nenhum.
func (s *Server) websocketScope(r *http.Request) ws.Filter {
	customerID, isCustomer := customerOf(r)
	if !isCustomer {
		return nil
	}
	return func(msg ws.Message) bool {
		if s.Owners == nil {
			return false
		}
		if s.Owners.Owns(customerID, msg.VehicleID, msg.DeviceID) {
			return true
		}
		return sharedMessage[msg.Type] && s.Owners.SharedWith(customerID, msg.VehicleID, msg.DeviceID)
	}
}

// sharedMessage: o que quem acompanha um veículo compartilhado recebe ao
// vivo — a posição, a conexão do rastreador, o motor e o andamento dos
// comandos (o bloqueio que ele pediu).
var sharedMessage = map[string]bool{
	ws.TypePositionUpdated:     true,
	ws.TypeDeviceOnline:        true,
	ws.TypeDeviceOffline:       true,
	ws.TypeDeviceStale:         true,
	ws.TypeEngineStatusChanged: true,
	ws.TypeCommandSent:         true,
	ws.TypeCommandAcknowledged: true,
	ws.TypeCommandFailed:       true,
}

func (s *Server) handleMyAccount(w http.ResponseWriter, r *http.Request) {
	customerID, _ := customerOf(r)
	account, err := s.Billing.Account(r.Context(), customerID)
	if err != nil {
		handleStoreError(w, err, "conta não encontrada")
		return
	}
	address, err := s.Addresses.Get(r.Context(), customerID)
	if err != nil {
		handleStoreError(w, err, "conta não encontrada")
		return
	}
	writeJSON(w, http.StatusOK, struct {
		*billing.Account
		// OnlinePayment diz se a tela oferece "Pagar com Pix".
		OnlinePayment bool `json:"onlinePayment"`
		// DeliveryAddress é nulo até o cliente cadastrar; sem ele, não
		// contrata rastreador.
		DeliveryAddress *addresses.Address `json:"deliveryAddress"`
	}{account, s.Payments.Enabled(), address})
}

func (s *Server) handleMySubscriptions(w http.ResponseWriter, r *http.Request) {
	customerID, _ := customerOf(r)
	list, err := s.Billing.ListSubscriptions(r.Context(), customerID)
	if err != nil {
		handleStoreError(w, err, "assinaturas não encontradas")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleMyInvoices(w http.ResponseWriter, r *http.Request) {
	customerID, _ := customerOf(r)
	list, err := s.Billing.ListInvoices(r.Context(), customerID)
	if err != nil {
		handleStoreError(w, err, "faturas não encontradas")
		return
	}
	writeJSON(w, http.StatusOK, list)
}
