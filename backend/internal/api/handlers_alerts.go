package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/alerts"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
)

// historyLimit é quantos alertas a tela mostra.
const historyLimit = 30

type alertSettingsRequest struct {
	Kinds      []string `json:"kinds"`
	GuardStart string   `json:"guardStart"`
	GuardEnd   string   `json:"guardEnd"`
}

type alertSettingsResponse struct {
	// Enabled: os alertas estão ligados no servidor (ALERTS_ENABLED).
	Enabled bool `json:"enabled"`
	// MailConfigured: há SMTP; sem ele nenhum e-mail sai.
	MailConfigured  bool                   `json:"mailConfigured"`
	Email           string                 `json:"email"`
	Kinds           []string               `json:"kinds"`
	GuardStart      string                 `json:"guardStart"`
	GuardEnd        string                 `json:"guardEnd"`
	Custom          bool                   `json:"custom"`
	Suspended       bool                   `json:"suspended"`
	CooldownMinutes int                    `json:"cooldownMinutes"`
	Timezone        string                 `json:"timezone"`
	Catalog         []alerts.KindInfo      `json:"catalog"`
	History         []*alerts.Notification `json:"history"`
}

func (s *Server) alertSettings(ctx context.Context, userID uuid.UUID) (*alertSettingsResponse, error) {
	user, err := s.Auth.GetUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	settings, err := s.AlertStore.LoadSettings(ctx, userID)
	if err != nil {
		return nil, err
	}
	history, err := s.AlertStore.History(ctx, userID, historyLimit)
	if err != nil {
		return nil, err
	}
	suspended, err := s.Billing.IsSuspended(ctx, userID)
	if err != nil {
		return nil, err
	}
	cfg := s.Config.Alerts
	return &alertSettingsResponse{
		Enabled: cfg.Enabled, MailConfigured: s.Config.Mail.Enabled(), Email: user.Email,
		Kinds: settings.EnabledList(), GuardStart: alerts.FormatClock(settings.GuardStart),
		GuardEnd: alerts.FormatClock(settings.GuardEnd), Custom: settings.Custom, Suspended: suspended,
		CooldownMinutes: int(cfg.Cooldown.Minutes()), Timezone: cfg.Timezone,
		Catalog: alerts.Catalog, History: history,
	}, nil
}

// saveAlertSettings valida e grava; devolve false se já respondeu com erro.
func (s *Server) saveAlertSettings(w http.ResponseWriter, r *http.Request, userID uuid.UUID) bool {
	var req alertSettingsRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return false
	}
	settings, err := alerts.NewSettings(req.Kinds, req.GuardStart, req.GuardEnd)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return false
	}
	if err := s.AlertStore.SaveSettings(r.Context(), userID, settings); err != nil {
		handleStoreError(w, err, "usuário não encontrado")
		return false
	}
	s.recordAudit(r, audit.ActionAlertSettingsChanged, nil, nil, map[string]any{
		"userId": userID, "kinds": settings.EnabledList(),
		"guardStart": req.GuardStart, "guardEnd": req.GuardEnd,
	})
	return true
}

func (s *Server) writeAlertSettings(w http.ResponseWriter, r *http.Request, userID uuid.UUID) {
	resp, err := s.alertSettings(r.Context(), userID)
	if err != nil {
		handleStoreError(w, err, "usuário não encontrado")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// --- Cliente -----------------------------------------------------------------

// As escolhas de alertas são de cada pessoa: na conta empresa, cada um da
// equipe escolhe o que recebe.
func (s *Server) handleMyAlerts(w http.ResponseWriter, r *http.Request) {
	s.writeAlertSettings(w, r, personOf(r))
}

func (s *Server) handleSaveMyAlerts(w http.ResponseWriter, r *http.Request) {
	customerID := personOf(r)
	if s.saveAlertSettings(w, r, customerID) {
		s.writeAlertSettings(w, r, customerID)
	}
}

// handleMyAlertsTest manda um e-mail de teste para o próprio cliente, na hora.
func (s *Server) handleMyAlertsTest(w http.ResponseWriter, r *http.Request) {
	customerID := personOf(r)
	user, err := s.Auth.GetUser(r.Context(), customerID)
	if err != nil {
		handleStoreError(w, err, "usuário não encontrado")
		return
	}
	switch err := s.Alerts.SendTest(r.Context(), user.ID, user.Email, user.Name); {
	case errors.Is(err, alerts.ErrTestTooSoon):
		writeError(w, http.StatusTooManyRequests, err.Error())
	case err != nil:
		s.Log.Warn("e-mail de teste de alertas não saiu", "user", user.ID, "err", err)
		writeError(w, http.StatusBadGateway, "não foi possível enviar o e-mail de teste agora; tente de novo em instantes")
	default:
		s.writeAlertSettings(w, r, customerID)
	}
}

// --- Central -----------------------------------------------------------------

// handleGetCustomerAlerts: a central vê o que o cliente escolheu e o que
// foi enviado a ele (para responder "não recebi o alerta").
func (s *Server) handleGetCustomerAlerts(w http.ResponseWriter, r *http.Request) {
	customerID, ok := s.customerFromURL(w, r)
	if !ok {
		return
	}
	s.writeAlertSettings(w, r, customerID)
}

func (s *Server) handleSaveCustomerAlerts(w http.ResponseWriter, r *http.Request) {
	customerID, ok := s.customerFromURL(w, r)
	if !ok {
		return
	}
	if s.saveAlertSettings(w, r, customerID) {
		s.writeAlertSettings(w, r, customerID)
	}
}
