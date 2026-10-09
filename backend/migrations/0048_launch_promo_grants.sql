-- A central libera a promoção de pré-lançamento para um cliente que não se
-- inscreveu na lista de lançamento: para a promoção, vale como se estivesse
-- nela (as vagas e o limite de 1 por cliente continuam). Fica fora da
-- launch_waitlist, que é de quem pediu o aviso do lançamento (consentimento).
CREATE TABLE launch_promo_grants (
    customer_id UUID PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    granted_by  UUID REFERENCES users (id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
