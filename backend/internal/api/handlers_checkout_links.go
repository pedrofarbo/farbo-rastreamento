package api

import (
	"errors"
	"net/http"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/orders"
)

// Os links de pagamento: a central gera um link com uma oferta fechada (o
// rastreador, a recorrência, o valor, a instalação e as taxas) e acompanha
// os usos; quem abre /contratar?link=<token> contrata nela.

func (s *Server) handleListCheckoutLinks(w http.ResponseWriter, r *http.Request) {
	list, err := s.Orders.CheckoutLinks(r.Context())
	if err != nil {
		handleStoreError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleCreateCheckoutLink(w http.ResponseWriter, r *http.Request) {
	var in orders.CheckoutLinkInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	link, err := s.Orders.CreateCheckoutLink(r.Context(), in, actor(r))
	if err != nil {
		writeOrderError(w, r, err, "")
		return
	}
	s.recordBillingAudit(r, audit.ActionCheckoutLinkCreated, checkoutLinkAudit(link))
	writeJSON(w, http.StatusCreated, link)
}

func (s *Server) handleUpdateCheckoutLink(w http.ResponseWriter, r *http.Request) {
	id, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}
	var in orders.CheckoutLinkInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	link, err := s.Orders.UpdateCheckoutLink(r.Context(), id, in)
	if err != nil {
		writeOrderError(w, r, err, "link de pagamento não encontrado")
		return
	}
	s.recordBillingAudit(r, audit.ActionCheckoutLinkUpdated, checkoutLinkAudit(link))
	writeJSON(w, http.StatusOK, link)
}

func (s *Server) handleDeleteCheckoutLink(w http.ResponseWriter, r *http.Request) {
	id, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}
	err = s.Orders.DeleteCheckoutLink(r.Context(), id)
	switch {
	case errors.Is(err, orders.ErrCheckoutLinkUsed):
		writeError(w, http.StatusConflict, err.Error())
		return
	case err != nil:
		handleStoreError(w, err, "link de pagamento não encontrado")
		return
	}
	s.recordBillingAudit(r, audit.ActionCheckoutLinkDeleted, map[string]any{"checkoutLinkId": id})
	w.WriteHeader(http.StatusNoContent)
}

func checkoutLinkAudit(l *orders.CheckoutLink) map[string]any {
	return map[string]any{
		"checkoutLinkId": l.ID, "label": l.Label, "equipmentMode": l.EquipmentMode, "periodMonths": l.PeriodMonths,
		"priceCents": l.PriceCents, "farboInstall": l.FarboInstall, "installCents": l.InstallCents,
		"uninstallCents": l.UninstallCents, "installBlocking": l.InstallBlocking, "installDirect": l.InstallDirect,
		"handDelivery": l.HandDelivery,
		"validUntil":   l.ValidUntil,
		"maxUses":      l.MaxUses, "active": l.Active,
	}
}
