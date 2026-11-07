-- O rastreador incluso: a Farbo não vende mais o rastreador (só emite nota de
-- serviço). Quem não fica no comodato recebe o rastreador, que fica com ele,
-- e a parte dele está nas primeiras mensalidades: somada ao plano (os
-- afiliados e os Insanos, + R$ 12 nas 10 primeiras) ou já dentro do preço
-- (o plano do site, R$ 69,90). Encerrada antes, o que falta dessa parte vence
-- numa fatura só.

-- tracker_cents é a parte do rastreador em cada uma das tracker_months
-- primeiras mensalidades, as que vencem antes de tracker_until; tracker_added
-- diz se ela é somada ao preço do plano. Zero: sem rastreador incluso (o
-- comprado antes, o comodato, o aparelho do cliente).
ALTER TABLE subscriptions
    ADD COLUMN tracker_cents  INTEGER NOT NULL DEFAULT 0 CHECK (tracker_cents BETWEEN 0 AND 10000000),
    ADD COLUMN tracker_months INTEGER NOT NULL DEFAULT 0 CHECK (tracker_months BETWEEN 0 AND 60),
    ADD COLUMN tracker_until  DATE,
    ADD COLUMN tracker_added  BOOLEAN NOT NULL DEFAULT FALSE;

-- A regra dos pedidos novos: a parte do rastreador e em quantas mensalidades.
-- O rastreador não tem mais preço (checkout_equipment_cents fica sem uso).
ALTER TABLE order_settings
    ADD COLUMN tracker_cents  INTEGER NOT NULL DEFAULT 1200 CHECK (tracker_cents BETWEEN 0 AND 10000000),
    ADD COLUMN tracker_months INTEGER NOT NULL DEFAULT 10 CHECK (tracker_months BETWEEN 1 AND 60);

-- Os preços novos: o plano do site com o rastreador incluso e o dos Insanos
-- depois das 10 primeiras (nelas, + R$ 12: R$ 39,90).
UPDATE order_settings SET checkout_plan_cents = 6990, checkout_insanos_cents = 2790;

-- O plano dos Insanos definido pela central no preço antigo (R$ 39,90 com o
-- rastreador comprado à parte): os veículos novos saem por R$ 27,90, mais a
-- parte do rastreador nas 10 primeiras. Os que já existem não mudam.
UPDATE customer_plans SET price_cents = 2790, updated_at = NOW()
WHERE equipment_mode = 'sale' AND price_cents = 3990 AND lower(btrim(plan_name)) = 'especial insanos mc';
