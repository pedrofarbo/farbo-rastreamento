package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/orders"
)

// A central libera a promoção de pré-lançamento para quem não se inscreveu
// na lista: o cliente passa a vê-la no próprio catálogo e contrata com ela
// (a vaga e o limite de 1 por cliente continuam); retirar a liberação não
// desfaz o que já foi contratado. Precisa de FARBO_TEST_DATABASE_URL.
func TestLaunchPromoGrant(t *testing.T) {
	env, _ := newLeadsEnv(t)
	ctx := context.Background()
	authSvc := auth.NewService(auth.NewRepository(env.db), config.Auth{BcryptCost: bcrypt.MinCost}, nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	caio, err := authSvc.CreateUser(ctx, "caio@fora.test", "Caio", auth.RoleCustomer, userPassword)
	if err != nil {
		t.Fatal(err)
	}
	admin := env.login(auth.RoleAdmin + "@leads.test")
	operator := env.login(auth.RoleOperator + "@leads.test")
	path := "/api/customers/" + caio.ID.String() + "/launch-promo"

	var status orders.PromoStatus
	_ = json.Unmarshal(env.must(admin, http.MethodGet, path, nil, http.StatusOK), &status)
	if status.Eligible || status.OnList || status.GrantedAt != nil || status.Claimed || status.Code != "NOT_ON_LIST" {
		t.Fatalf("antes = %+v", status)
	}
	var catalog struct {
		LaunchPromo *orders.PromoOffer `json:"launchPromo"`
	}
	caioToken := env.login("caio@fora.test")
	_ = json.Unmarshal(env.must(caioToken, http.MethodGet, "/api/catalog", nil, http.StatusOK), &catalog)
	if catalog.LaunchPromo != nil {
		t.Fatalf("catálogo antes = %+v", catalog.LaunchPromo)
	}

	// Só o admin libera; liberar de novo não muda nada.
	env.must(operator, http.MethodPost, path+"/grant", nil, http.StatusForbidden)
	_ = json.Unmarshal(env.must(admin, http.MethodPost, path+"/grant", nil, http.StatusOK), &status)
	if !status.Eligible || status.OnList || status.GrantedAt == nil || status.Code != "" {
		t.Fatalf("liberado = %+v", status)
	}
	first := *status.GrantedAt
	_ = json.Unmarshal(env.must(admin, http.MethodPost, path+"/grant", nil, http.StatusOK), &status)
	if !status.GrantedAt.Equal(first) {
		t.Errorf("liberar de novo mudou a data: %v → %v", first, status.GrantedAt)
	}

	// O cliente vê a promoção no catálogo e a central contrata com ela.
	_ = json.Unmarshal(env.must(caioToken, http.MethodGet, "/api/catalog", nil, http.StatusOK), &catalog)
	if catalog.LaunchPromo == nil || catalog.LaunchPromo.EquipmentCents != 12000 {
		t.Fatalf("catálogo liberado = %+v", catalog.LaunchPromo)
	}
	var result struct {
		Subscription struct {
			PromoPriceCents *int `json:"promoPriceCents"`
		} `json:"subscription"`
		SetupInvoice *struct {
			AmountCents int `json:"amountCents"`
		} `json:"setupInvoice"`
	}
	_ = json.Unmarshal(env.must(admin, http.MethodPost, "/api/customers/"+caio.ID.String()+"/trackers", map[string]any{
		"vehicle": map[string]any{"name": "Moto do Caio", "plate": "LIB1A11"}, "equipmentCents": 15000,
		"plan": map[string]any{"planName": "Plano Mensal", "priceCents": 6990, "dueDay": 10}, "launchPromo": true,
	}, http.StatusCreated), &result)
	if result.SetupInvoice == nil || result.SetupInvoice.AmountCents != 12000 || result.Subscription.PromoPriceCents == nil ||
		*result.Subscription.PromoPriceCents != 3490 {
		t.Fatalf("pedido com a promoção = %+v", result)
	}

	// Usada: sem direito a outra; retirar a liberação não desfaz a usada.
	_ = json.Unmarshal(env.must(admin, http.MethodGet, path, nil, http.StatusOK), &status)
	if status.Eligible || !status.Claimed || status.GrantedAt == nil || status.Code != "CLAIMED" {
		t.Errorf("depois de usar = %+v", status)
	}
	_ = json.Unmarshal(env.must(admin, http.MethodDelete, path+"/grant", nil, http.StatusOK), &status)
	if status.GrantedAt != nil || !status.Claimed {
		t.Errorf("retirada = %+v", status)
	}
	var claims, granted, revoked int
	_ = env.db.QueryRow(ctx, `SELECT count(*) FROM launch_promo_claims WHERE customer_id = $1`, caio.ID).Scan(&claims)
	_ = env.db.QueryRow(ctx, `SELECT count(*) FILTER (WHERE action = 'LAUNCH_PROMO_GRANTED'), count(*) FILTER (WHERE action = 'LAUNCH_PROMO_REVOKED')
		FROM audit_logs`).Scan(&granted, &revoked)
	if claims != 1 || granted != 2 || revoked != 1 {
		t.Errorf("vaga = %d, auditoria = %d liberações e %d retiradas", claims, granted, revoked)
	}
}
