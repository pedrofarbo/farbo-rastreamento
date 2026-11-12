package api

import (
	"errors"
	"net/http"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/orders"
)

// Os cupons de desconto da contratação direta pelo site: a central cria e
// acompanha os usos; o site confere o código antes de mostrar o desconto.

// handleCheckoutCoupon confere o cupom digitado no site: o desconto e em
// quantas cobranças, ou o motivo de não valer. Pelo link de um
// representante, não vale.
func (s *Server) handleCheckoutCoupon(w http.ResponseWriter, r *http.Request) {
	if s.referralFor(r, r.URL.Query().Get("ref")) != nil {
		writeError(w, http.StatusBadRequest, "o cupom vale só na contratação direta pelo site, sem o link de um representante")
		return
	}
	coupon, err := s.Orders.CheckoutCoupon(r.Context(), r.URL.Query().Get("code"))
	if err != nil {
		writeOrderError(w, r, err, "cupom não encontrado")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, coupon.Offer())
}

func (s *Server) handleListCoupons(w http.ResponseWriter, r *http.Request) {
	list, err := s.Orders.Coupons(r.Context())
	if err != nil {
		handleStoreError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleCreateCoupon(w http.ResponseWriter, r *http.Request) {
	var in orders.CouponInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	coupon, err := s.Orders.CreateCoupon(r.Context(), in, actor(r))
	if errors.Is(err, database.ErrConflict) {
		writeError(w, http.StatusConflict, "já existe um cupom com este código")
		return
	}
	if err != nil {
		writeOrderError(w, r, err, "")
		return
	}
	s.recordBillingAudit(r, audit.ActionCouponCreated, couponAudit(coupon))
	writeJSON(w, http.StatusCreated, coupon)
}

func (s *Server) handleUpdateCoupon(w http.ResponseWriter, r *http.Request) {
	id, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}
	var in orders.CouponInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	coupon, err := s.Orders.UpdateCoupon(r.Context(), id, in)
	if err != nil {
		writeOrderError(w, r, err, "cupom não encontrado")
		return
	}
	s.recordBillingAudit(r, audit.ActionCouponUpdated, couponAudit(coupon))
	writeJSON(w, http.StatusOK, coupon)
}

func (s *Server) handleDeleteCoupon(w http.ResponseWriter, r *http.Request) {
	id, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}
	err = s.Orders.DeleteCoupon(r.Context(), id)
	switch {
	case errors.Is(err, orders.ErrCouponUsed):
		writeError(w, http.StatusConflict, err.Error())
		return
	case err != nil:
		handleStoreError(w, err, "cupom não encontrado")
		return
	}
	s.recordBillingAudit(r, audit.ActionCouponDeleted, map[string]any{"couponId": id})
	w.WriteHeader(http.StatusNoContent)
}

func couponAudit(c *orders.Coupon) map[string]any {
	return map[string]any{
		"couponId": c.ID, "code": c.Code, "percent": c.Percent, "amountCents": c.AmountCents, "charges": c.Charges,
		"validUntil": c.ValidUntil, "maxUses": c.MaxUses, "active": c.Active,
	}
}
