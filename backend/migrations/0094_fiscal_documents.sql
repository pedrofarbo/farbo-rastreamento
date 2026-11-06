-- As notas fiscais de serviço (NFS-e) das faturas pagas, emitidas pela
-- Notaas: uma por fatura (a mensalidade, o proporcional, a instalação e a
-- desinstalação, a fatura do mês da organização). pending espera a emissão;
-- processing está na prefeitura; blocked, o limite de notas do plano
-- acabou (tenta de novo no mês seguinte); cancel_pending e cancelled, a
-- fatura estornada depois da nota.
CREATE TABLE fiscal_documents (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    invoice_id        UUID        NOT NULL UNIQUE REFERENCES invoices (id) ON DELETE CASCADE,
    customer_id       UUID        REFERENCES users (id) ON DELETE SET NULL,
    provider          TEXT        NOT NULL DEFAULT 'notaas',
    provider_id       TEXT        NOT NULL DEFAULT '',
    status            TEXT        NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'processing', 'issued', 'error', 'blocked', 'cancel_pending', 'cancelled')),
    number            TEXT        NOT NULL DEFAULT '',
    verification_code TEXT        NOT NULL DEFAULT '',
    amount_cents      INTEGER     NOT NULL CHECK (amount_cents > 0),
    pdf_url           TEXT        NOT NULL DEFAULT '',
    xml_url           TEXT        NOT NULL DEFAULT '',
    error             TEXT        NOT NULL DEFAULT '',
    attempts          INTEGER     NOT NULL DEFAULT 0,
    next_try_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    issued_at         TIMESTAMPTZ,
    cancelled_at      TIMESTAMPTZ,
    emailed_at        TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_fiscal_documents_work ON fiscal_documents (status, next_try_at);
CREATE INDEX idx_fiscal_documents_customer ON fiscal_documents (customer_id);
CREATE INDEX idx_fiscal_documents_provider ON fiscal_documents (provider_id) WHERE provider_id <> '';

-- Desde quando as faturas pagas ganham nota: o primeiro trabalho com a
-- emissão ligada marca (as pagas antes não ganham nota retroativa).
CREATE TABLE fiscal_settings (
    id         BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (id),
    started_at TIMESTAMPTZ
);
INSERT INTO fiscal_settings DEFAULT VALUES;
