-- Os links de pagamento: a central gera um link com uma oferta fechada (o
-- rastreador incluso ou em comodato, o mensal, o semestral ou o anual, o
-- valor de cada cobrança, a instalação com ou sem bloqueio e as taxas) e
-- manda a quem vai contratar. Quem abre /contratar?link=<token> vê só essa
-- oferta: informa o veículo e os dados e paga. O próximo veículo do cliente
-- sai nas condições normais.
CREATE TABLE checkout_links (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- O código do link (aleatório, difícil de adivinhar).
    token            TEXT        NOT NULL CHECK (token ~ '^[a-z0-9]{10,40}$'),
    -- Para quem ou para quê (só a central vê).
    label            TEXT        NOT NULL CHECK (length(label) BETWEEN 1 AND 100),
    equipment_mode   TEXT        NOT NULL CHECK (equipment_mode IN ('sale', 'loan')),
    -- 1: o mensal; 6: o semestral; 12: o anual.
    period_months    INTEGER     NOT NULL DEFAULT 1 CHECK (period_months IN (1, 6, 12)),
    -- O valor de cada cobrança (o mês, o semestre ou o ano), com tudo dentro.
    price_cents      INTEGER     NOT NULL CHECK (price_cents BETWEEN 100 AND 10000000),
    -- Quem instala: a Farbo (no comodato, sempre) ou o cliente, por conta
    -- própria (só no incluso).
    farbo_install    BOOLEAN     NOT NULL DEFAULT FALSE,
    install_cents    INTEGER     NOT NULL DEFAULT 0 CHECK (install_cents BETWEEN 0 AND 10000000),
    -- A desinstalação no fim do comodato.
    uninstall_cents  INTEGER     NOT NULL DEFAULT 0 CHECK (uninstall_cents BETWEEN 0 AND 10000000),
    -- A instalação com o relé do bloqueio do motor.
    install_blocking BOOLEAN     NOT NULL DEFAULT FALSE,
    -- Até que dia (inclusive) dá para usar; nulo, sem validade.
    valid_until      DATE,
    -- Quantas contratações podem usar; nulo, sem limite.
    max_uses         INTEGER CHECK (max_uses BETWEEN 1 AND 100000),
    active           BOOLEAN     NOT NULL DEFAULT TRUE,
    created_by       UUID REFERENCES users (id) ON DELETE SET NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT checkout_links_loan_install CHECK (equipment_mode = 'sale' OR farbo_install),
    CONSTRAINT checkout_links_sale_uninstall CHECK (equipment_mode = 'loan' OR uninstall_cents = 0),
    CONSTRAINT checkout_links_own_install CHECK (farbo_install OR (install_cents = 0 AND NOT install_blocking))
);
CREATE UNIQUE INDEX checkout_links_token_key ON checkout_links (token);

-- O uso de um link: uma assinatura, uma linha. A contratação que não foi
-- paga sai com a conta, e o uso sai junto (a vaga volta).
CREATE TABLE checkout_link_uses (
    subscription_id UUID PRIMARY KEY REFERENCES subscriptions (id) ON DELETE CASCADE,
    link_id         UUID        NOT NULL REFERENCES checkout_links (id) ON DELETE RESTRICT,
    customer_id     UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX checkout_link_uses_link ON checkout_link_uses (link_id);
