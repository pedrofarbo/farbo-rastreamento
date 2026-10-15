-- A promoção de pré-lançamento passa a valer para todos os veículos do
-- cliente (antes, um por cliente): uma linha por assinatura contratada com a
-- promoção. As vagas continuam contando clientes — os primeiros a contratar
-- —, e quem já tem a vaga põe quantos veículos quiser.
ALTER TABLE launch_promo_claims DROP CONSTRAINT launch_promo_claims_pkey;
ALTER TABLE launch_promo_claims ADD COLUMN id BIGSERIAL PRIMARY KEY;
CREATE INDEX launch_promo_claims_customer ON launch_promo_claims (customer_id);
