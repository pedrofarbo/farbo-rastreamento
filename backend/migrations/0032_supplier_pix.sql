-- Pagar fornecedores por Pix pela AbacatePay: sai do saldo da conta, para a
-- chave Pix do fornecedor ou para o Pix copia-e-cola da conta.

-- O tipo da chave (a AbacatePay exige; 11 dígitos podem ser CPF ou celular).
ALTER TABLE suppliers ADD COLUMN pix_key_type TEXT NOT NULL DEFAULT ''
    CHECK (pix_key_type IN ('', 'CPF', 'CNPJ', 'PHONE', 'EMAIL', 'RANDOM'));

-- Cada envio. O registro nasce antes de chamar a AbacatePay (SENDING): uma
-- conta não tem dois envios ao mesmo tempo, e um envio sem resposta (a rede
-- caiu) fica UNKNOWN até alguém conferir no painel da AbacatePay — nunca é
-- reenviado sozinho.
CREATE TABLE finance_pix_transfers (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    entry_id     UUID        NOT NULL REFERENCES finance_entries (id),
    -- O identificador do envio na AbacatePay (tran_...), quando ela responde.
    provider_id  TEXT        UNIQUE,
    -- O nosso identificador, mandado como externalId.
    external_id  TEXT        NOT NULL UNIQUE,
    status       TEXT        NOT NULL DEFAULT 'SENDING'
                 CHECK (status IN ('SENDING', 'COMPLETE', 'FAILED', 'UNKNOWN')),
    amount_cents BIGINT      NOT NULL CHECK (amount_cents > 0),
    fee_cents    BIGINT      NOT NULL DEFAULT 0,
    -- Para onde foi: a chave (e o tipo) ou o copia-e-cola (BR_CODE).
    pix_key      TEXT        NOT NULL,
    key_type     TEXT        NOT NULL CHECK (key_type IN ('CPF', 'CNPJ', 'PHONE', 'EMAIL', 'RANDOM', 'BR_CODE')),
    receipt_url  TEXT        NOT NULL DEFAULT '',
    dev_mode     BOOLEAN     NOT NULL DEFAULT FALSE,
    -- O motivo da recusa ou da falha (a mensagem da AbacatePay).
    error        TEXT        NOT NULL DEFAULT '',
    -- A tarifa lançada nas contas (paga), se houve.
    fee_entry_id UUID        REFERENCES finance_entries (id) ON DELETE SET NULL,
    created_by   UUID        REFERENCES users (id) ON DELETE SET NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ,
    -- Última consulta à AbacatePay (o envio pode falhar depois de enviado).
    checked_at   TIMESTAMPTZ
);
-- No máximo um envio vivo por conta: dois cliques, duas abas ou duas
-- instâncias não pagam a mesma conta duas vezes.
CREATE UNIQUE INDEX idx_finance_pix_transfers_live ON finance_pix_transfers (entry_id) WHERE status <> 'FAILED';
CREATE INDEX idx_finance_pix_transfers_entry ON finance_pix_transfers (entry_id, created_at DESC);
CREATE INDEX idx_finance_pix_transfers_watch ON finance_pix_transfers (checked_at)
    WHERE status IN ('SENDING', 'COMPLETE');
