-- Cupons de desconto da contratação direta pelo site (sem o link de um
-- representante): a central cria; cada um abate a mensalidade, em
-- porcentagem ou num valor fixo, nas primeiras cobranças da assinatura (no
-- semestral e no anual, cada cobrança é um período). O desconto vai para o
-- preço promocional da assinatura (promo_price_cents até promo_until), e o
-- valor normal volta sozinho depois.
CREATE TABLE coupons (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- O código que a pessoa digita: maiúsculas, números, - e _.
    code         TEXT        NOT NULL CHECK (code ~ '^[A-Z0-9_-]{3,30}$'),
    description  TEXT        NOT NULL DEFAULT '' CHECK (length(description) <= 200),
    -- Um dos dois: a porcentagem ou o valor fixo abatido em cada cobrança.
    percent      INTEGER CHECK (percent BETWEEN 1 AND 100),
    amount_cents INTEGER CHECK (amount_cents BETWEEN 1 AND 10000000),
    -- Em quantas das primeiras cobranças o desconto vale.
    charges      INTEGER     NOT NULL CHECK (charges BETWEEN 1 AND 60),
    -- Até que dia (inclusive) dá para usar; nulo, sem validade.
    valid_until  DATE,
    -- Quantas contratações podem usar; nulo, sem limite.
    max_uses     INTEGER CHECK (max_uses BETWEEN 1 AND 1000000),
    active       BOOLEAN     NOT NULL DEFAULT TRUE,
    created_by   UUID REFERENCES users (id) ON DELETE SET NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT coupons_discount CHECK ((percent IS NULL) <> (amount_cents IS NULL))
);
CREATE UNIQUE INDEX coupons_code_key ON coupons (code);

-- O uso de um cupom: uma assinatura, uma linha. A contratação pelo site que
-- não foi paga sai com a conta, e o uso sai junto (a vaga volta).
CREATE TABLE coupon_redemptions (
    subscription_id UUID PRIMARY KEY REFERENCES subscriptions (id) ON DELETE CASCADE,
    coupon_id       UUID        NOT NULL REFERENCES coupons (id) ON DELETE RESTRICT,
    customer_id     UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    -- O desconto de cada cobrança, como ficou na assinatura.
    discount_cents  INTEGER     NOT NULL CHECK (discount_cents >= 0),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX coupon_redemptions_coupon ON coupon_redemptions (coupon_id);

-- O cupom da assinatura: o preço promocional dela é o do cupom (e não o da
-- promoção de pré-lançamento).
ALTER TABLE subscriptions ADD COLUMN coupon_code TEXT;
