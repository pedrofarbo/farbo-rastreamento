package orders

import (
	"context"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// Promoção de pré-lançamento: quem está na lista de lançamento (pelo e-mail
// da conta) paga menos no primeiro rastreador e na mensalidade dele por
// alguns meses. Uma vaga por cliente, para os primeiros a contratar.

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

// PromoStatus diz se o cliente tem direito e, se não, por quê.
type PromoStatus struct {
	Eligible bool       `json:"eligible"`
	Reason   string     `json:"reason"`
	Offer    PromoOffer `json:"offer"`
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
	if !p.Enabled {
		return "a promoção de pré-lançamento foi encerrada", nil
	}
	var onList, claimed bool
	var used int
	err := q.QueryRow(ctx, `
		SELECT
			EXISTS (SELECT 1 FROM users u JOIN launch_waitlist w ON lower(w.email) = lower(u.email) WHERE u.id = $1),
			EXISTS (SELECT 1 FROM launch_promo_claims WHERE customer_id = $1),
			(SELECT COUNT(*) FROM launch_promo_claims)`, customerID).Scan(&onList, &claimed, &used)
	switch {
	case err != nil:
		return "", database.MapError(err)
	case claimed:
		return "a promoção de pré-lançamento já foi usada por este cliente (vale para 1 veículo)", nil
	case !onList:
		return "o e-mail da conta não está na lista de lançamento", nil
	case used >= p.Slots:
		return "as vagas da promoção de pré-lançamento acabaram", nil
	}
	return "", nil
}

// PromoFor diz se o cliente pode contratar com a promoção agora.
func (s *Service) PromoFor(ctx context.Context, customerID uuid.UUID) (PromoStatus, error) {
	reason, err := promoCheck(ctx, s.db, customerID, s.catalog.LaunchPromo)
	if err != nil {
		return PromoStatus{}, err
	}
	return PromoStatus{Eligible: reason == "", Reason: reason, Offer: offerOf(s.catalog.LaunchPromo)}, nil
}

// PromoUsage conta as vagas usadas.
func (s *Service) PromoUsage(ctx context.Context) (PromoUsage, error) {
	p := s.catalog.LaunchPromo
	out := PromoUsage{Enabled: p.Enabled, Slots: p.Slots, Offer: offerOf(p)}
	err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM launch_promo_claims`).Scan(&out.Used)
	return out, database.MapError(err)
}
