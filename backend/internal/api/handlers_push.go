package api

import (
	"errors"
	"net/http"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/push"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/webpush"
)

// pushSubscriptionRequest é o PushSubscription.toJSON() do navegador.
type pushSubscriptionRequest struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

// handleMyPush: chave pública para o navegador se inscrever e os aparelhos
// em que o cliente já ligou as notificações.
func (s *Server) handleMyPush(w http.ResponseWriter, r *http.Request) {
	customerID := personOf(r) // os aparelhos são da pessoa
	devices := []push.Device{}
	if s.Push.Enabled() {
		var err error
		if devices, err = s.Push.Devices(r.Context(), customerID); err != nil {
			handleStoreError(w, err, "usuário não encontrado")
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": s.Push.Enabled(), "publicKey": s.Push.PublicKey(), "devices": devices,
	})
}

func (s *Server) handleMyPushSubscribe(w http.ResponseWriter, r *http.Request) {
	customerID := personOf(r) // os aparelhos são da pessoa
	var req pushSubscriptionRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	sub := webpush.Subscription{Endpoint: req.Endpoint, P256dh: req.Keys.P256dh, Auth: req.Keys.Auth}
	switch err := s.Push.Subscribe(r.Context(), customerID, sub, r.UserAgent()); {
	case errors.Is(err, push.ErrDisabled):
		writeError(w, http.StatusServiceUnavailable, err.Error())
	case errors.Is(err, push.ErrInvalidSubscription):
		writeError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		handleStoreError(w, err, "usuário não encontrado")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) handleMyPushUnsubscribe(w http.ResponseWriter, r *http.Request) {
	customerID := personOf(r) // os aparelhos são da pessoa
	var req pushSubscriptionRequest
	if err := decodeJSON(w, r, &req); err != nil || req.Endpoint == "" {
		writeError(w, http.StatusBadRequest, "informe o endpoint")
		return
	}
	if err := s.Push.Unsubscribe(r.Context(), customerID, req.Endpoint); err != nil {
		handleStoreError(w, err, "usuário não encontrado")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleMyPushTest manda uma notificação de teste para os aparelhos do cliente.
func (s *Server) handleMyPushTest(w http.ResponseWriter, r *http.Request) {
	customerID := personOf(r) // os aparelhos são da pessoa
	delivered, err := s.Push.SendTest(r.Context(), customerID, "/app/alertas")
	switch {
	case errors.Is(err, push.ErrDisabled):
		writeError(w, http.StatusServiceUnavailable, err.Error())
	case errors.Is(err, push.ErrTestTooSoon):
		writeError(w, http.StatusTooManyRequests, err.Error())
	case err != nil:
		s.Log.Warn("notificação de teste não saiu", "user", customerID, "err", err)
		writeError(w, http.StatusBadGateway, "o serviço de notificações do celular não respondeu; tente de novo")
	case delivered == 0:
		writeError(w, http.StatusConflict, "nenhum aparelho com notificações ligadas")
	default:
		writeJSON(w, http.StatusOK, map[string]int{"delivered": delivered})
	}
}
