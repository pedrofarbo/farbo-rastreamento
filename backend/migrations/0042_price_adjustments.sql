-- Reajuste anual pelo IPCA: todo agosto, a mensalidade das assinaturas com
-- pelo menos 12 meses é corrigida pelo IPCA acumulado nos 12 meses até maio
-- (o último publicado antes do aviso, em 1º de julho). Índice zero ou
-- negativo mantém o preço.

-- O preço novo de cada assinatura, a partir de uma data: as faturas que
-- vencem desde então já saem com ele (mesmo geradas antes); passada a data,
-- ele vira o preço da assinatura.
ALTER TABLE subscriptions
    ADD COLUMN next_price_cents INTEGER CHECK (next_price_cents > 0),
    ADD COLUMN next_price_from  DATE,
    ADD CHECK ((next_price_cents IS NULL) = (next_price_from IS NULL));

-- Um reajuste por ano.
CREATE TABLE price_adjustments (
    year           INTEGER PRIMARY KEY,
    -- O acumulado: do primeiro ao último mês (dia 1), e a variação em
    -- milionésimos (4,23% = 42300).
    period_start   DATE        NOT NULL,
    period_end     DATE        NOT NULL,
    rate_millionths INTEGER    NOT NULL,
    effective_from DATE        NOT NULL,
    -- NOTIFIED: clientes avisados, preço novo agendado; APPLIED: em vigor;
    -- CANCELED: o admin desistiu; NO_CHANGE: índice zero ou negativo.
    status         TEXT        NOT NULL CHECK (status IN ('NOTIFIED', 'APPLIED', 'CANCELED', 'NO_CHANGE')),
    subscriptions  INTEGER     NOT NULL DEFAULT 0,
    notified_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    applied_at     TIMESTAMPTZ,
    canceled_at    TIMESTAMPTZ,
    canceled_by    UUID        REFERENCES users (id) ON DELETE SET NULL
);

-- Cada assinatura reajustada no ano, e se o cliente recebeu o aviso.
CREATE TABLE price_adjustment_items (
    year            INTEGER     NOT NULL REFERENCES price_adjustments (year) ON DELETE CASCADE,
    subscription_id UUID        NOT NULL REFERENCES subscriptions (id) ON DELETE CASCADE,
    customer_id     UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    old_price_cents INTEGER     NOT NULL,
    new_price_cents INTEGER     NOT NULL,
    notified        BOOLEAN     NOT NULL DEFAULT FALSE,
    PRIMARY KEY (year, subscription_id)
);
CREATE INDEX idx_price_adjustment_items_customer ON price_adjustment_items (customer_id);

-- O aviso por e-mail de cada versão nova do contrato (cláusula 14), uma vez
-- por cliente.
CREATE TABLE contract_change_notices (
    user_id UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    version TEXT        NOT NULL,
    sent_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, version)
);
