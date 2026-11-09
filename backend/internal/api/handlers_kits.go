package api

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/kits"
)

// Os kits de instalação dos representantes: a central monta e acompanha as
// remessas (/kit-shipments); o representante pede, acompanha e confirma que
// recebeu pela página dele (/public/partner/{token}/kits).

func writeKitError(w http.ResponseWriter, err error) bool {
	var v kits.ValidationError
	switch {
	case err == nil:
		return true
	case errors.As(err, &v):
		writeError(w, http.StatusBadRequest, v.Message)
	case errors.Is(err, kits.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	default:
		handleStoreError(w, err, "remessa não encontrada")
	}
	return false
}

func (s *Server) handleKitShipments(w http.ResponseWriter, r *http.Request) {
	list, err := s.Kits.List(r.Context(), r.URL.Query().Get("status"))
	if !writeKitError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleKitDevices(w http.ResponseWriter, r *http.Request) {
	list, err := s.Kits.Available(r.Context())
	if !writeKitError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// handleKitChips: os chips que podem ir avulsos num kit.
func (s *Server) handleKitChips(w http.ResponseWriter, r *http.Request) {
	list, err := s.Kits.AvailableChips(r.Context())
	if !writeKitError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// kitInput é a remessa montada na central (ou o pedido atendido).
type kitInput struct {
	AffiliateID uuid.UUID   `json:"affiliateId"`
	DeviceIDs   []uuid.UUID `json:"deviceIds"`
	ChipIDs     []uuid.UUID `json:"chipIds"`
	Notes       string      `json:"notes"`
}

func (s *Server) handleCreateKitShipment(w http.ResponseWriter, r *http.Request) {
	var in kitInput
	if !decodeOr400(w, r, &in) {
		return
	}
	k, err := s.Kits.Create(r.Context(), personOf(r), in.AffiliateID, in.DeviceIDs, in.ChipIDs, in.Notes)
	if !writeKitError(w, err) {
		return
	}
	s.auditAffiliate(r, "kits-create", map[string]any{"affiliateId": k.AffiliateID, "shipmentId": k.ID, "devices": len(k.Devices),
		"chips": len(k.Chips)})
	writeJSON(w, http.StatusCreated, k)
}

func (s *Server) handlePrepareKitShipment(w http.ResponseWriter, r *http.Request) {
	id, ok := idOr400(w, r)
	if !ok {
		return
	}
	var in kitInput
	if !decodeOr400(w, r, &in) {
		return
	}
	k, err := s.Kits.Prepare(r.Context(), id, in.DeviceIDs, in.ChipIDs, in.Notes)
	if !writeKitError(w, err) {
		return
	}
	s.auditAffiliate(r, "kits-prepare", map[string]any{"affiliateId": k.AffiliateID, "shipmentId": k.ID, "devices": len(k.Devices),
		"chips": len(k.Chips)})
	writeJSON(w, http.StatusOK, k)
}

func (s *Server) handleShipKitShipment(w http.ResponseWriter, r *http.Request) {
	id, ok := idOr400(w, r)
	if !ok {
		return
	}
	var in struct {
		Carrier      string `json:"carrier"`
		TrackingCode string `json:"trackingCode"`
	}
	if !decodeOr400(w, r, &in) {
		return
	}
	k, err := s.Kits.Ship(r.Context(), id, in.Carrier, in.TrackingCode)
	if !writeKitError(w, err) {
		return
	}
	s.auditAffiliate(r, "kits-ship", map[string]any{"affiliateId": k.AffiliateID, "shipmentId": k.ID, "carrier": k.Carrier})
	writeJSON(w, http.StatusOK, k)
}

func (s *Server) handleDeliverKitShipment(w http.ResponseWriter, r *http.Request) {
	id, ok := idOr400(w, r)
	if !ok {
		return
	}
	k, err := s.Kits.Deliver(r.Context(), id)
	if !writeKitError(w, err) {
		return
	}
	s.auditAffiliate(r, "kits-deliver", map[string]any{"affiliateId": k.AffiliateID, "shipmentId": k.ID})
	writeJSON(w, http.StatusOK, k)
}

func (s *Server) handleCancelKitShipment(w http.ResponseWriter, r *http.Request) {
	id, ok := idOr400(w, r)
	if !ok {
		return
	}
	k, err := s.Kits.Cancel(r.Context(), id)
	if !writeKitError(w, err) {
		return
	}
	s.auditAffiliate(r, "kits-cancel", map[string]any{"affiliateId": k.AffiliateID, "shipmentId": k.ID})
	writeJSON(w, http.StatusOK, k)
}

// partnerKitsAccount é o representante da página (o link e a senha
// conferidos) e se ela tem senha.
func (s *Server) partnerKitsAccount(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool, bool) {
	if s.Kits == nil {
		writeError(w, http.StatusNotFound, "os kits estão desligados")
		return uuid.Nil, false, false
	}
	if !s.partnerAllowed(w, r) {
		return uuid.Nil, false, false
	}
	id, protected, err := s.Affiliates.PartnerAccount(r.Context(), chi.URLParam(r, "token"))
	if err != nil {
		affiliateError(w, err, "link inválido: peça o novo à Farbo")
		return uuid.Nil, false, false
	}
	w.Header().Set("Cache-Control", "no-store")
	return id, protected, true
}

// handlePartnerKits: os kits do representante — o estoque, os instalados (com
// o cliente, na página com senha) e as remessas.
func (s *Server) handlePartnerKits(w http.ResponseWriter, r *http.Request) {
	id, protected, ok := s.partnerKitsAccount(w, r)
	if !ok {
		return
	}
	out, err := s.Kits.ForPartner(r.Context(), id, protected)
	if !writeKitError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handlePartnerKitRequest(w http.ResponseWriter, r *http.Request) {
	id, _, ok := s.partnerKitsAccount(w, r)
	if !ok {
		return
	}
	var in struct {
		Quantity int    `json:"quantity"`
		Note     string `json:"note"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	k, err := s.Kits.Request(r.Context(), id, in.Quantity, in.Note)
	if !writeKitError(w, err) {
		return
	}
	s.Log.Info("representante pediu kits", "affiliate", id, "quantity", in.Quantity)
	writeJSON(w, http.StatusCreated, k)
}

func (s *Server) handlePartnerKitReceived(w http.ResponseWriter, r *http.Request) {
	affiliateID, protected, ok := s.partnerKitsAccount(w, r)
	if !ok {
		return
	}
	id, ok := idOr400(w, r)
	if !ok {
		return
	}
	k, err := s.Kits.Received(r.Context(), affiliateID, id, protected)
	if !writeKitError(w, err) {
		return
	}
	s.Log.Info("representante recebeu os kits", "affiliate", affiliateID, "shipment", id)
	writeJSON(w, http.StatusOK, k)
}

func (s *Server) handlePartnerKitCancel(w http.ResponseWriter, r *http.Request) {
	affiliateID, _, ok := s.partnerKitsAccount(w, r)
	if !ok {
		return
	}
	id, ok := idOr400(w, r)
	if !ok {
		return
	}
	k, err := s.Kits.CancelRequest(r.Context(), affiliateID, id)
	if !writeKitError(w, err) {
		return
	}
	s.Log.Info("representante cancelou o pedido de kits", "affiliate", affiliateID, "shipment", id)
	writeJSON(w, http.StatusOK, k)
}
