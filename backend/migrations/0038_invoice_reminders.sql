-- A régua de cobrança: os lembretes de fatura que saíram (e-mail e push).
-- Cada etapa sai uma vez por fatura — quando é gerada, 3 dias antes, no dia,
-- 3 dias depois e perto da suspensão; o lembrete pedido pela central
-- (MANUAL) pode repetir.
CREATE TABLE invoice_reminders (
    id         BIGSERIAL PRIMARY KEY,
    invoice_id UUID        NOT NULL REFERENCES invoices (id) ON DELETE CASCADE,
    kind       TEXT        NOT NULL
               CHECK (kind IN ('ISSUED', 'DUE_SOON', 'DUE_TODAY', 'OVERDUE', 'SUSPENSION_SOON', 'MANUAL')),
    -- Quem pediu (só no MANUAL).
    sent_by    UUID        REFERENCES users (id) ON DELETE SET NULL,
    emailed    BOOLEAN     NOT NULL DEFAULT FALSE,
    -- Em quantos aparelhos o push chegou.
    pushed     INTEGER     NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_invoice_reminders_once ON invoice_reminders (invoice_id, kind) WHERE kind <> 'MANUAL';
CREATE INDEX idx_invoice_reminders_invoice ON invoice_reminders (invoice_id, created_at DESC);
