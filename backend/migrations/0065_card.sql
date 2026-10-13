-- O cartão de crédito (AbacatePay) como forma de pagamento das faturas, e o
-- cartão salvo para o débito automático no vencimento.
ALTER TABLE payment_charges DROP CONSTRAINT payment_charges_method_check;
ALTER TABLE payment_charges ADD CONSTRAINT payment_charges_method_check CHECK (method IN ('PIX', 'BOLETO', 'CARD'));
ALTER TABLE payment_charges
    ADD COLUMN card_brand TEXT NOT NULL DEFAULT '',
    ADD COLUMN card_last4 TEXT NOT NULL DEFAULT '';

-- O cartão salvo de cada conta (um por conta). card_token é o cardToken da
-- AbacatePay, cifrado (AES-GCM, chave derivada do segredo do servidor): com
-- ele dá para cobrar sem o cliente presente, por isso nunca sai daqui.
-- auto_debit é a autorização do débito automático (quem deu e quando);
-- failed_at e last_error, a última recusa (limpos quando passa).
CREATE TABLE customer_cards (
    customer_id UUID PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    card_token  TEXT        NOT NULL,
    brand       TEXT        NOT NULL DEFAULT '',
    last4       TEXT        NOT NULL DEFAULT '',
    auto_debit  BOOLEAN     NOT NULL DEFAULT FALSE,
    consent_at  TIMESTAMPTZ,
    consent_by  UUID REFERENCES users (id) ON DELETE SET NULL,
    failed_at   TIMESTAMPTZ,
    last_error  TEXT        NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- As tentativas do débito automático: uma por conta, vencimento e dia
-- (attempt 0 no vencimento, 1 e 2 nos dias seguintes). A linha reserva a
-- tentativa antes de cobrar: duas instâncias não cobram duas vezes. UNKNOWN
-- é o erro de rede no meio da cobrança (pode ter passado): a próxima rodada
-- confere na AbacatePay antes de qualquer coisa.
CREATE TABLE card_debits (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id  UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    due_date     DATE        NOT NULL,
    attempt      INTEGER     NOT NULL,
    status       TEXT        NOT NULL CHECK (status IN ('RUNNING', 'APPROVED', 'PENDING', 'DECLINED', 'UNKNOWN', 'FAILED')),
    amount_cents INTEGER     NOT NULL DEFAULT 0,
    charge_id    UUID REFERENCES payment_charges (id) ON DELETE SET NULL,
    error        TEXT        NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (customer_id, due_date, attempt)
);
CREATE INDEX idx_card_debits_open ON card_debits (created_at) WHERE status IN ('RUNNING', 'UNKNOWN');
