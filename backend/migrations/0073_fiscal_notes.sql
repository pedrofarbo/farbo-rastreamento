-- O fechamento do mês: a nota fiscal de cada pagador — o cliente ou a
-- associação/seguradora — pelo que foi faturado no mês (as faturas que vencem
-- nele, pagas ou não). A central emite a nota no emissor dela e registra aqui
-- o número, a data, o valor e o PDF; o cliente e a organização baixam na tela
-- de faturas. O PDF fica no banco (entra nos backups com o resto).
CREATE TABLE fiscal_notes (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id  UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    -- O mês de competência (o primeiro dia dele).
    month        DATE        NOT NULL CHECK (EXTRACT(DAY FROM month) = 1),
    number       TEXT        NOT NULL,
    issued_on    DATE        NOT NULL,
    amount_cents INTEGER     NOT NULL CHECK (amount_cents >= 0),
    filename     TEXT        NOT NULL DEFAULT '',
    content_type TEXT        NOT NULL DEFAULT '' CHECK (content_type IN ('', 'application/pdf', 'image/png', 'image/jpeg', 'application/xml', 'text/xml')),
    size_bytes   INTEGER     NOT NULL DEFAULT 0,
    data         BYTEA,
    created_by   UUID        REFERENCES users (id) ON DELETE SET NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (customer_id, month)
);
CREATE INDEX idx_fiscal_notes_month ON fiscal_notes (month);
