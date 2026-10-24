package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/apikeys"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/tracking"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/vehicles"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/webhooks"
)

// As integrações: as chaves da API e os webhooks da conta do cliente (o
// titular, em /me) e da associação ou seguradora (o Administrador, em /org),
// e a API em si (/api/v1), que só lê: os veículos, as posições e os eventos.

// integrationOwner é a conta das integrações de quem chama: a organização
// ou a conta do cliente.
func integrationOwner(r *http.Request) uuid.UUID {
	if orgID, ok := orgOf(r); ok {
		return orgID
	}
	account, _ := customerOf(r)
	return account
}

func writeIntegrationError(w http.ResponseWriter, err error, notFound string) bool {
	var keyErr *apikeys.InputError
	var hookErr *webhooks.InputError
	switch {
	case err == nil:
		return true
	case errors.As(err, &keyErr):
		writeError(w, http.StatusBadRequest, keyErr.Message)
	case errors.As(err, &hookErr):
		writeError(w, http.StatusBadRequest, hookErr.Message)
	case errors.Is(err, database.ErrNotFound):
		writeError(w, http.StatusNotFound, notFound)
	default:
		handleStoreError(w, err, notFound)
	}
	return false
}

func (s *Server) handleAPIKeys(w http.ResponseWriter, r *http.Request) {
	list, err := s.APIKeys.List(r.Context(), integrationOwner(r))
	if !writeIntegrationError(w, err, "chaves não encontradas") {
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleCreateAPIKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	key, secret, err := s.APIKeys.Create(r.Context(), integrationOwner(r), personOf(r), req.Name)
	if !writeIntegrationError(w, err, "chave não encontrada") {
		return
	}
	s.recordAudit(r, audit.ActionIntegrationChanged, nil, nil, map[string]any{"apiKeyId": key.ID, "prefix": key.Prefix, "change": "key created"})
	writeJSON(w, http.StatusCreated, map[string]any{"key": key, "secret": secret})
}

func (s *Server) handleRevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	id, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}
	if !writeIntegrationError(w, s.APIKeys.Revoke(r.Context(), integrationOwner(r), id), "chave não encontrada ou já revogada") {
		return
	}
	s.recordAudit(r, audit.ActionIntegrationChanged, nil, nil, map[string]any{"apiKeyId": id, "change": "key revoked"})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleWebhooks(w http.ResponseWriter, r *http.Request) {
	list, err := s.Webhooks.List(r.Context(), integrationOwner(r))
	if !writeIntegrationError(w, err, "webhooks não encontrados") {
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleCreateWebhook(w http.ResponseWriter, r *http.Request) {
	var in webhooks.Input
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	hook, err := s.Webhooks.Create(r.Context(), integrationOwner(r), personOf(r), in)
	if !writeIntegrationError(w, err, "webhook não encontrado") {
		return
	}
	s.recordAudit(r, audit.ActionIntegrationChanged, nil, nil, map[string]any{"webhookId": hook.ID, "url": hook.URL, "change": "webhook created"})
	writeJSON(w, http.StatusCreated, hook)
}

func (s *Server) handleUpdateWebhook(w http.ResponseWriter, r *http.Request) {
	id, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}
	var in webhooks.Input
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	hook, err := s.Webhooks.Update(r.Context(), integrationOwner(r), id, in)
	if !writeIntegrationError(w, err, "webhook não encontrado") {
		return
	}
	s.recordAudit(r, audit.ActionIntegrationChanged, nil, nil, map[string]any{"webhookId": hook.ID, "url": hook.URL, "change": "webhook updated"})
	writeJSON(w, http.StatusOK, hook)
}

func (s *Server) handleDeleteWebhook(w http.ResponseWriter, r *http.Request) {
	id, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}
	if !writeIntegrationError(w, s.Webhooks.Delete(r.Context(), integrationOwner(r), id), "webhook não encontrado") {
		return
	}
	s.recordAudit(r, audit.ActionIntegrationChanged, nil, nil, map[string]any{"webhookId": id, "change": "webhook deleted"})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleTestWebhook(w http.ResponseWriter, r *http.Request) {
	id, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}
	hook, err := s.Webhooks.Test(r.Context(), integrationOwner(r), id)
	if !writeIntegrationError(w, err, "webhook não encontrado") {
		return
	}
	writeJSON(w, http.StatusOK, hook)
}

// ---------------------------------------------------------------------------
// A API (/api/v1), pela chave
// ---------------------------------------------------------------------------

type apiOwnerKey struct{}

// requireAPIKey confere a chave (Authorization: Bearer fk_... ou X-Api-Key)
// e põe a conta dona no contexto. Conta desativada ou suspensa: 403.
func (s *Server) requireAPIKey(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("X-Api-Key")
		if key == "" {
			key = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		}
		owner, err := s.APIKeys.Authenticate(r.Context(), key)
		if errors.Is(err, apikeys.ErrInvalid) {
			writeError(w, http.StatusUnauthorized, "chave da API inválida ou revogada")
			return
		}
		if err != nil {
			handleStoreError(w, err, "")
			return
		}
		user, err := s.Auth.GetUser(r.Context(), owner)
		if err != nil || !user.Active {
			writeError(w, http.StatusForbidden, "a conta desta chave está desativada")
			return
		}
		if suspended, err := s.Billing.IsSuspended(r.Context(), owner); err != nil || suspended {
			writeError(w, http.StatusPaymentRequired, "conta suspensa por fatura vencida")
			return
		}
		ctx := context.WithValue(r.Context(), apiOwnerKey{}, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// apiVehicles são os veículos da conta da chave (na organização, os dos associados).
func (s *Server) apiVehicles(r *http.Request) ([]*vehicles.Vehicle, error) {
	user := r.Context().Value(apiOwnerKey{}).(*auth.User)
	if user.Role == auth.RoleOrganization {
		return s.Vehicles.ListByOrganization(r.Context(), user.ID)
	}
	return s.Vehicles.ListByOwner(r.Context(), user.ID)
}

// apiVehicle é a visão do veículo na API.
type apiVehicle struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Plate string `json:"plate"`
	Brand string `json:"brand"`
	Model string `json:"model"`
	Year  *int   `json:"year"`
	Color string `json:"color"`
	// OwnerID: o associado, na chave da organização.
	OwnerID   *uuid.UUID   `json:"ownerId,omitempty"`
	Installed bool         `json:"installed"`
	Online    bool         `json:"online"`
	Ignition  *bool        `json:"ignition"`
	Voltage   *float64     `json:"batteryVoltage"`
	Position  *apiPosition `json:"position"`
}

type apiPosition struct {
	Latitude     float64   `json:"latitude"`
	Longitude    float64   `json:"longitude"`
	SpeedKmh     float64   `json:"speedKmh"`
	Heading      *float64  `json:"heading"`
	GPSTimestamp time.Time `json:"gpsTimestamp"`
}

func toAPIPosition(p *tracking.Position) *apiPosition {
	if p == nil {
		return nil
	}
	return &apiPosition{Latitude: p.Latitude, Longitude: p.Longitude, SpeedKmh: p.SpeedKmh, Heading: p.Heading, GPSTimestamp: p.GPSTimestamp}
}

func (s *Server) handleAPIVehicles(w http.ResponseWriter, r *http.Request) {
	list, err := s.apiVehicles(r)
	if err != nil {
		handleStoreError(w, err, "veículos não encontrados")
		return
	}
	deviceIDs := []uuid.UUID{}
	for _, v := range list {
		if v.DeviceID != nil {
			deviceIDs = append(deviceIDs, *v.DeviceID)
		}
	}
	latest, err := s.Positions.LatestFor(r.Context(), deviceIDs)
	if err != nil {
		handleStoreError(w, err, "posições não encontradas")
		return
	}
	devs, err := s.Devices.ListByIDs(r.Context(), deviceIDs)
	if err != nil {
		handleStoreError(w, err, "rastreadores não encontrados")
		return
	}
	online := map[uuid.UUID]bool{}
	for _, d := range devs {
		online[d.ID] = d.Status == "ONLINE"
	}
	out := make([]apiVehicle, 0, len(list))
	for _, v := range list {
		item := apiVehicle{ID: v.ID.String(), Name: v.Name, Plate: v.Plate, Brand: v.Brand, Model: v.Model, Year: v.Year,
			Color: v.Color, Installed: v.DeviceID != nil}
		if user, _ := r.Context().Value(apiOwnerKey{}).(*auth.User); user != nil && user.Role == auth.RoleOrganization {
			item.OwnerID = v.OwnerID
		}
		if v.DeviceID != nil {
			item.Online = online[*v.DeviceID]
			item.Position = toAPIPosition(latest[*v.DeviceID])
			if st := s.States.Get(*v.DeviceID); st != nil {
				item.Ignition, item.Voltage = st.ACC, st.BatteryVoltage
			}
		}
		out = append(out, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"vehicles": out})
}

// apiVehicleFromURL é o veículo da rota, se for da conta da chave.
func (s *Server) apiVehicleFromURL(w http.ResponseWriter, r *http.Request) (*vehicles.Vehicle, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return nil, false
	}
	list, err := s.apiVehicles(r)
	if err != nil {
		handleStoreError(w, err, "veículo não encontrado")
		return nil, false
	}
	for _, v := range list {
		if v.ID == id {
			return v, true
		}
	}
	writeError(w, http.StatusNotFound, "veículo não encontrado")
	return nil, false
}

// handleAPIPositions: as posições do veículo de ?from= a ?to= (RFC 3339; até 7 dias).
func (s *Server) handleAPIPositions(w http.ResponseWriter, r *http.Request) {
	v, ok := s.apiVehicleFromURL(w, r)
	if !ok {
		return
	}
	to := time.Now()
	from := to.Add(-24 * time.Hour)
	var err error
	if raw := r.URL.Query().Get("to"); raw != "" {
		if to, err = time.Parse(time.RFC3339, raw); err != nil {
			writeError(w, http.StatusBadRequest, "to inválido (RFC 3339)")
			return
		}
	}
	if raw := r.URL.Query().Get("from"); raw != "" {
		if from, err = time.Parse(time.RFC3339, raw); err != nil {
			writeError(w, http.StatusBadRequest, "from inválido (RFC 3339)")
			return
		}
	}
	if !to.After(from) || to.Sub(from) > 7*24*time.Hour {
		writeError(w, http.StatusBadRequest, "o período vai até 7 dias (from antes de to)")
		return
	}
	if v.DeviceID == nil {
		writeJSON(w, http.StatusOK, map[string]any{"positions": []apiPosition{}})
		return
	}
	result, err := s.Positions.History(r.Context(), tracking.HistoryQuery{DeviceID: *v.DeviceID, From: from, To: to, Limit: 10000, OnlyValid: true})
	if err != nil {
		handleStoreError(w, err, "posições não encontradas")
		return
	}
	out := make([]*apiPosition, 0, len(result.Positions))
	for _, p := range result.Positions {
		out = append(out, toAPIPosition(p))
	}
	writeJSON(w, http.StatusOK, map[string]any{"positions": out, "total": result.Total, "sampled": result.Sampled})
}

type apiEvent struct {
	ID        int64           `json:"id"`
	Type      string          `json:"type"`
	Timestamp time.Time       `json:"timestamp"`
	VehicleID string          `json:"vehicleId"`
	Latitude  *float64        `json:"latitude"`
	Longitude *float64        `json:"longitude"`
	SpeedKmh  *float64        `json:"speedKmh"`
	Metadata  json.RawMessage `json:"metadata"`
}

// handleAPIEvents: os eventos dos veículos depois de ?after= (o id do último
// que o sistema já leu; 0 traz os mais antigos das últimas 24 h), em ordem,
// até ?limit= (500).
func (s *Server) handleAPIEvents(w http.ResponseWriter, r *http.Request) {
	list, err := s.apiVehicles(r)
	if err != nil {
		handleStoreError(w, err, "veículos não encontrados")
		return
	}
	ids := make([]uuid.UUID, 0, len(list))
	for _, v := range list {
		ids = append(ids, v.ID)
	}
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	rows, err := s.DB.Query(r.Context(), `
		SELECT e.id, e.type, e.timestamp, v.id, e.latitude, e.longitude, e.speed_kmh, COALESCE(e.metadata, '{}'::jsonb)
		FROM vehicle_events e JOIN vehicles v ON v.device_id = e.device_id
		WHERE v.id = ANY($1) AND e.id > $2 AND ($2 > 0 OR e.created_at > NOW() - INTERVAL '24 hours')
		ORDER BY e.id LIMIT $3`, ids, after, limit)
	if err != nil {
		handleStoreError(w, err, "eventos não encontrados")
		return
	}
	defer rows.Close()
	out := []apiEvent{}
	for rows.Next() {
		var e apiEvent
		var vehicleID uuid.UUID
		if err := rows.Scan(&e.ID, &e.Type, &e.Timestamp, &vehicleID, &e.Latitude, &e.Longitude, &e.SpeedKmh, &e.Metadata); err != nil {
			handleStoreError(w, err, "eventos não encontrados")
			return
		}
		e.VehicleID = vehicleID.String()
		out = append(out, e)
	}
	next := after
	if len(out) > 0 {
		next = out[len(out)-1].ID
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": out, "next": next})
}

// integrationRoutes são as rotas das chaves e dos webhooks (em /me e /org).
func (s *Server) integrationRoutes(r chi.Router) {
	r.Get("/api-keys", s.handleAPIKeys)
	r.Post("/api-keys", s.handleCreateAPIKey)
	r.Delete("/api-keys/{id}", s.handleRevokeAPIKey)
	r.Get("/webhooks", s.handleWebhooks)
	r.Post("/webhooks", s.handleCreateWebhook)
	r.Patch("/webhooks/{id}", s.handleUpdateWebhook)
	r.Delete("/webhooks/{id}", s.handleDeleteWebhook)
	r.Post("/webhooks/{id}/test", s.handleTestWebhook)
}
