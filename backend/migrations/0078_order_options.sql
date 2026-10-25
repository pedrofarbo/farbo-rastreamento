-- As opções do pedido do cliente (quando a central não definiu um plano para
-- ele): o comodato — a mensalidade e as taxas de instalação e de
-- desinstalação, desligado até a central configurar — e a instalação pelos
-- instaladores da Farbo no rastreador comprado, pelo preço daqui (no
-- comodato, a instalação é sempre da Farbo, com a taxa do comodato).
CREATE TABLE order_settings (
    id                       BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (id),
    loan_enabled             BOOLEAN     NOT NULL DEFAULT FALSE,
    -- A mensalidade no comodato; nula: a do plano.
    loan_monthly_cents       INTEGER     CHECK (loan_monthly_cents BETWEEN 0 AND 10000000),
    loan_install_fee_cents   INTEGER     NOT NULL DEFAULT 0 CHECK (loan_install_fee_cents BETWEEN 0 AND 10000000),
    loan_uninstall_fee_cents INTEGER     NOT NULL DEFAULT 0 CHECK (loan_uninstall_fee_cents BETWEEN 0 AND 10000000),
    install_enabled          BOOLEAN     NOT NULL DEFAULT TRUE,
    install_cents            INTEGER     NOT NULL DEFAULT 15000 CHECK (install_cents BETWEEN 0 AND 10000000),
    updated_by               UUID        REFERENCES users (id) ON DELETE SET NULL,
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
INSERT INTO order_settings DEFAULT VALUES;

-- A instalação contratada com a Farbo (os instaladores dela agendam): o
-- comodato e quem escolheu a instalação paga; sem isso, o cliente instala
-- por conta própria.
ALTER TABLE subscriptions ADD COLUMN farbo_install BOOLEAN NOT NULL DEFAULT FALSE;
