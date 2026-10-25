-- A contratação rápida pela landing (pessoa física, pago com Pix na hora):
-- os preços dela, que a central ajusta — o plano normal, o dos integrantes do
-- Insanos MC e o rastreador comprado. O preço do plano vale enquanto a
-- assinatura durar (com o reajuste anual).
ALTER TABLE order_settings
    ADD COLUMN checkout_enabled         BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN checkout_plan_cents      INTEGER NOT NULL DEFAULT 3490 CHECK (checkout_plan_cents BETWEEN 0 AND 10000000),
    ADD COLUMN checkout_insanos_cents   INTEGER NOT NULL DEFAULT 2790 CHECK (checkout_insanos_cents BETWEEN 0 AND 10000000),
    ADD COLUMN checkout_equipment_cents INTEGER NOT NULL DEFAULT 12000 CHECK (checkout_equipment_cents BETWEEN 0 AND 10000000),
    -- O comodato da pessoa física custa a mensalidade do plano mais a
    -- "garantia de troca do rastreador" (no lugar de uma mensalidade própria).
    ADD COLUMN loan_surcharge_cents     INTEGER NOT NULL DEFAULT 300 CHECK (loan_surcharge_cents BETWEEN 0 AND 10000000),
    DROP COLUMN loan_monthly_cents;

-- O comodato que a Farbo passa a oferecer: instalação de R$ 150,00 e
-- desinstalação de R$ 85,00.
UPDATE order_settings SET loan_enabled = TRUE, loan_install_fee_cents = 15000, loan_uninstall_fee_cents = 8500;

-- A garantia de troca que a mensalidade da assinatura inclui (o comodato da
-- pessoa física): o próximo veículo do cliente parte do plano sem ela.
ALTER TABLE subscriptions ADD COLUMN guarantee_cents INTEGER NOT NULL DEFAULT 0 CHECK (guarantee_cents >= 0);
