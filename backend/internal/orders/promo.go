package orders

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// Promoção de pré-lançamento: quem está na lista de lançamento (pelo e-mail
// da conta) — ou foi liberado pela central — paga menos no primeiro
// rastreador e na mensalidade dele por alguns meses. Uma vaga por cliente,
// para os primeiros a contratar.

// promoLockKey serializa as reservas de vaga: duas contratações ao mesmo
// tempo não passam do limite.
const promoLockKey = 0x46617262 // "Farb"

// PromoUnavailable: pediu a promoção sem ter direito a ela.
type PromoUnavailable struct{ Reason string }

func (e PromoUnavailable) Error() string { return e.Reason }

// PromoOffer é o que a promoção cobra. A mensalidade depende do plano:
// MonthlyCents, ou InsanosMonthlyCents no plano do Insanos MC
// (InsanosPlanName) — a central, que escolhe o plano no pedido, usa os dois;
// o catálogo do cliente já vem com a do plano dele.
type PromoOffer struct {
	EquipmentCents      int    `json:"equipmentCents"`
	MonthlyCents        int    `json:"monthlyCents"`
	InsanosMonthlyCents int    `json:"insanosMonthlyCents"`
	InsanosPlanName     string `json:"insanosPlanName"`
	Months              int    `json:"months"`
}

// PromoStatus diz se o cliente tem direito e, se não, por quê. OnList: o
// e-mail está na lista de lançamento; GrantedAt: a central liberou (nulo se
// não); Claimed: já usou a vaga.
type PromoStatus struct {
	Eligible bool   `json:"eligible"`
	Reason   string `json:"reason"`
	// Code é o motivo sem o texto: ENDED, CLAIMED, NOT_ON_LIST ou NO_SLOTS
	// (vazio: tem direito). Com NOT_ON_LIST, a central pode liberar.
	Code      string     `json:"code"`
	Offer     PromoOffer `json:"offer"`
	OnList    bool       `json:"onList"`
	GrantedAt *time.Time `json:"grantedAt"`
	Claimed   bool       `json:"claimed"`
}

// promoFacts é o que decide o direito à promoção.
type promoFacts struct {
	onList    bool
	grantedAt *time.Time
	claimed   bool
	used      int
}

func readPromoFacts(ctx context.Context, q database.Querier, customerID uuid.UUID) (promoFacts, error) {
	var f promoFacts
	err := q.QueryRow(ctx, `
		SELECT
			EXISTS (SELECT 1 FROM users u JOIN launch_waitlist w ON lower(w.email) = lower(u.email) WHERE u.id = $1),
			(SELECT created_at FROM launch_promo_grants WHERE customer_id = $1),
			EXISTS (SELECT 1 FROM launch_promo_claims WHERE customer_id = $1),
			(SELECT COUNT(*) FROM launch_promo_claims)`, customerID).Scan(&f.onList, &f.grantedAt, &f.claimed, &f.used)
	return f, database.MapError(err)
}

// reason é por que o cliente não tem direito: o código e o texto (vazios:
// tem direito).
func (f promoFacts) reason(p config.LaunchPromo) (code, text string) {
	switch {
	case !p.Enabled:
		return "ENDED", "a promoção de pré-lançamento foi encerrada"
	case f.claimed:
		return "CLAIMED", "a promoção de pré-lançamento já foi usada por este cliente (vale para 1 veículo)"
	case !f.onList && f.grantedAt == nil:
		return "NOT_ON_LIST", "o e-mail da conta não está na lista de lançamento"
	case f.used >= p.Slots:
		return "NO_SLOTS", "as vagas da promoção de pré-lançamento acabaram"
	}
	return "", ""
}

// PromoUsage é a contagem de vagas.
type PromoUsage struct {
	Enabled bool       `json:"enabled"`
	Slots   int        `json:"slots"`
	Used    int        `json:"used"`
	Offer   PromoOffer `json:"offer"`
}

func offerOf(p config.LaunchPromo) PromoOffer {
	return PromoOffer{
		EquipmentCents: p.EquipmentCents, MonthlyCents: p.MonthlyCents,
		InsanosMonthlyCents: p.InsanosMonthlyCents, InsanosPlanName: p.InsanosPlanName, Months: p.Months,
	}
}

// promoCheck confere o direito do cliente. Para reservar a vaga, roda na
// transação do pedido, depois de promoLockKey.
func promoCheck(ctx context.Context, q database.Querier, customerID uuid.UUID, p config.LaunchPromo) (string, error) {
	f, err := readPromoFacts(ctx, q, customerID)
	if err != nil {
		return "", err
	}
	_, reason := f.reason(p)
	return reason, nil
}

// PromoFor diz se o cliente pode contratar com a promoção agora.
func (s *Service) PromoFor(ctx context.Context, customerID uuid.UUID) (PromoStatus, error) {
	f, err := readPromoFacts(ctx, s.db, customerID)
	if err != nil {
		return PromoStatus{}, err
	}
	code, reason := f.reason(s.catalog.LaunchPromo)
	return PromoStatus{
		Eligible: reason == "", Reason: reason, Code: code, Offer: offerOf(s.catalog.LaunchPromo),
		OnList: f.onList, GrantedAt: f.grantedAt, Claimed: f.claimed,
	}, nil
}

// GrantPromo libera a promoção para o cliente que não está na lista de
// lançamento (by: quem liberou). Liberar de novo não muda nada.
func (s *Service) GrantPromo(ctx context.Context, customerID uuid.UUID, by *uuid.UUID) error {
	_, err := s.db.Exec(ctx, `
		INSERT INTO launch_promo_grants (customer_id, granted_by) VALUES ($1, $2)
		ON CONFLICT (customer_id) DO NOTHING`, customerID, by)
	return database.MapError(err)
}

// RevokePromo retira a liberação. A vaga já usada (pedido feito) continua.
func (s *Service) RevokePromo(ctx context.Context, customerID uuid.UUID) error {
	_, err := s.db.Exec(ctx, `DELETE FROM launch_promo_grants WHERE customer_id = $1`, customerID)
	return database.MapError(err)
}

// PromoUsage conta as vagas usadas.
func (s *Service) PromoUsage(ctx context.Context) (PromoUsage, error) {
	p := s.catalog.LaunchPromo
	out := PromoUsage{Enabled: p.Enabled, Slots: p.Slots, Offer: offerOf(p)}
	err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM launch_promo_claims`).Scan(&out.Used)
	return out, database.MapError(err)
}
