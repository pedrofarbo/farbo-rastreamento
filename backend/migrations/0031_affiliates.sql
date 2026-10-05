-- Afiliados (influenciadores): cada um tem o link de cadastro
-- (/indicacao/<code>) e ganha um valor por mês por cliente indicado, em cada
-- mês em que o cliente pagou a mensalidade. O admin cria os links e muda os
-- valores; a comissão guarda o valor de quando nasceu.

CREATE TABLE affiliates (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name             TEXT        NOT NULL,
    -- O @ do Instagram, para a tela de cadastro ("Indicado por @fulano").
    handle           TEXT        NOT NULL DEFAULT '',
    -- O código do link: minúsculo, letras, números e hífen.
    code             TEXT        NOT NULL UNIQUE CHECK (code ~ '^[a-z0-9][a-z0-9-]{1,39}$'),
    -- O link secreto da página do afiliado (/parceiro/<report_token>).
    report_token     TEXT        NOT NULL UNIQUE,
    -- Por cliente indicado, por mês pago.
    commission_cents INTEGER     NOT NULL CHECK (commission_cents BETWEEN 0 AND 100000),
    email            TEXT        NOT NULL DEFAULT '',
    phone            TEXT        NOT NULL DEFAULT '',
    pix_key          TEXT        NOT NULL DEFAULT '',
    notes            TEXT        NOT NULL DEFAULT '',
    -- O fornecedor na Empresa: criado no primeiro fechamento, para a conta a pagar.
    supplier_id      UUID        REFERENCES suppliers (id) ON DELETE SET NULL,
    -- Inativo: o link para de indicar e as comissões novas param.
    active           BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- O valor padrão dos afiliados novos e a categoria das contas do fechamento.
INSERT INTO finance_categories (name, kind, dre_group)
VALUES ('Comissões de afiliados', 'EXPENSE', 'OPERATING')
ON CONFLICT DO NOTHING;

CREATE TABLE affiliate_settings (
    id                       BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (id),
    default_commission_cents INTEGER NOT NULL DEFAULT 600 CHECK (default_commission_cents BETWEEN 0 AND 100000),
    category_id              UUID REFERENCES finance_categories (id) ON DELETE SET NULL,
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
INSERT INTO affiliate_settings (category_id)
SELECT id FROM finance_categories WHERE kind = 'EXPENSE' AND lower(name) = lower('Comissões de afiliados');

-- Quem chegou pelo link: o pré-cadastro e a lista guardam o afiliado (o
-- primeiro que trouxe a pessoa).
ALTER TABLE leads ADD COLUMN affiliate_id UUID REFERENCES affiliates (id) ON DELETE SET NULL;
ALTER TABLE launch_waitlist ADD COLUMN affiliate_id UUID REFERENCES affiliates (id) ON DELETE SET NULL;

-- O cliente indicado: um afiliado por cliente. Nasce no cadastro do cliente
-- (pelo pré-cadastro ou pelo e-mail na lista) ou pela mão do admin.
CREATE TABLE affiliate_referrals (
    customer_id  UUID PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    affiliate_id UUID        NOT NULL REFERENCES affiliates (id) ON DELETE CASCADE,
    source       TEXT        NOT NULL CHECK (source IN ('lead', 'waitlist', 'admin')),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_affiliate_referrals_affiliate ON affiliate_referrals (affiliate_id);

-- O fechamento: as comissões de um afiliado viram uma conta a pagar na
-- Empresa; pagar a conta paga as comissões.
CREATE TABLE affiliate_payouts (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    affiliate_id     UUID        NOT NULL REFERENCES affiliates (id) ON DELETE CASCADE,
    -- O mês fechado (o primeiro dia); entram as comissões até ele.
    month            DATE        NOT NULL,
    amount_cents     BIGINT      NOT NULL CHECK (amount_cents > 0),
    commissions      INTEGER     NOT NULL CHECK (commissions > 0),
    finance_entry_id UUID        REFERENCES finance_entries (id) ON DELETE SET NULL,
    created_by       UUID        REFERENCES users (id) ON DELETE SET NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_affiliate_payouts_affiliate ON affiliate_payouts (affiliate_id, month DESC);

-- Uma comissão por afiliado, cliente e mês (o do vencimento da mensalidade
-- paga). Sem fechamento ainda (payout_id nulo), a comissão de uma
-- mensalidade estornada some.
CREATE TABLE affiliate_commissions (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    affiliate_id UUID        NOT NULL REFERENCES affiliates (id) ON DELETE CASCADE,
    customer_id  UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    month        DATE        NOT NULL,
    amount_cents INTEGER     NOT NULL CHECK (amount_cents > 0),
    payout_id    UUID        REFERENCES affiliate_payouts (id) ON DELETE SET NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (affiliate_id, customer_id, month)
);
CREATE INDEX idx_affiliate_commissions_open ON affiliate_commissions (affiliate_id, month) WHERE payout_id IS NULL;
