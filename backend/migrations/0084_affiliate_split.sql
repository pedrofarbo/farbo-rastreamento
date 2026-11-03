-- O split da Pagar.me: a parte do afiliado sai no próprio pagamento do
-- cliente, direto para a conta dele, sem passar pela conta da Farbo.
--
-- O afiliado vira recebedor na Pagar.me (o cadastro e a prova de vida, pela
-- página dele): aqui fica só o id e a situação, nada dos dados pessoais.
ALTER TABLE affiliates
    ADD COLUMN recipient_id         TEXT NOT NULL DEFAULT '',
    ADD COLUMN recipient_status     TEXT NOT NULL DEFAULT '',
    ADD COLUMN recipient_checked_at TIMESTAMPTZ;
CREATE UNIQUE INDEX idx_affiliates_recipient ON affiliates (recipient_id) WHERE recipient_id <> '';

-- A parte do afiliado em cada cobrança com split: o recebedor e o valor.
CREATE TABLE payment_charge_splits (
    charge_id    UUID PRIMARY KEY REFERENCES payment_charges (id) ON DELETE CASCADE,
    affiliate_id UUID        NOT NULL REFERENCES affiliates (id) ON DELETE CASCADE,
    recipient_id TEXT        NOT NULL,
    amount_cents INTEGER     NOT NULL CHECK (amount_cents > 0),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_payment_charge_splits_affiliate ON payment_charge_splits (affiliate_id, created_at DESC);

-- As comissões das faturas da própria cobrança (nascem quando ela é paga,
-- com este valor e já pagas pelo split).
CREATE TABLE payment_charge_split_items (
    charge_id       UUID    NOT NULL REFERENCES payment_charge_splits (charge_id) ON DELETE CASCADE,
    subscription_id UUID    NOT NULL REFERENCES subscriptions (id) ON DELETE CASCADE,
    month           DATE    NOT NULL,
    amount_cents    INTEGER NOT NULL CHECK (amount_cents > 0),
    PRIMARY KEY (charge_id, subscription_id, month)
);

-- A comissão paga (ou reservada) num split: a cobrança que leva o valor. As
-- acumuladas são reservadas antes de a cobrança existir (sem chave
-- estrangeira); a conferência solta a reserva da cobrança que não foi paga.
ALTER TABLE affiliate_commissions
    ADD COLUMN split_charge_id   UUID,
    ADD COLUMN split_reserved_at TIMESTAMPTZ;
CREATE INDEX idx_affiliate_commissions_split ON affiliate_commissions (split_charge_id) WHERE split_charge_id IS NOT NULL;
