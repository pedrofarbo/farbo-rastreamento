-- Promoção de pré-lançamento: quem está na lista de lançamento paga menos no
-- primeiro rastreador e na mensalidade dele por um período, enquanto houver
-- vaga (os primeiros a contratar).

-- Preço promocional da assinatura: as faturas que vencem antes de
-- promo_until saem por promo_price_cents; as seguintes, por price_cents.
ALTER TABLE subscriptions
    ADD COLUMN promo_price_cents INTEGER CHECK (promo_price_cents >= 0),
    ADD COLUMN promo_until       DATE,
    ADD CONSTRAINT subscriptions_promo_pair CHECK ((promo_price_cents IS NULL) = (promo_until IS NULL));

-- Uma vaga por cliente: quem já usou não usa de novo, e a contagem limita o
-- total.
CREATE TABLE launch_promo_claims (
    customer_id     UUID PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    subscription_id UUID REFERENCES subscriptions (id) ON DELETE SET NULL,
    claimed_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
