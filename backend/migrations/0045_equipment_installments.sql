-- O rastreador parcelado em até 10x sem juros, por Pix: a 1ª parcela vai na
-- fatura do pedido (com o frete) e as demais, uma por mês, somadas às
-- mensalidades. A assinatura fica ativa até a última; encerrada antes, o
-- saldo vira uma fatura só (ou é dispensado: arrependimento, pedido desfeito).

-- Em quantas vezes o rastreador da assinatura foi parcelado (0: à vista) e o
-- vencimento da mensalidade que traz a última parcela (a permanência).
ALTER TABLE subscriptions
    ADD COLUMN installments     SMALLINT NOT NULL DEFAULT 0 CHECK (installments = 0 OR installments BETWEEN 2 AND 12),
    ADD COLUMN commitment_until DATE,
    ADD CHECK ((installments = 0) = (commitment_until IS NULL));

CREATE TABLE equipment_installments (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    subscription_id UUID        NOT NULL REFERENCES subscriptions (id) ON DELETE CASCADE,
    number          SMALLINT    NOT NULL CHECK (number BETWEEN 1 AND 12),
    amount_cents    INTEGER     NOT NULL CHECK (amount_cents > 0),
    -- A fatura que cobra a parcela; nula enquanto não foi cobrada (ou se a
    -- fatura foi cancelada: volta para a próxima mensalidade).
    invoice_id      UUID        REFERENCES invoices (id) ON DELETE SET NULL,
    -- Dispensada no encerramento da assinatura.
    waived_at       TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (subscription_id, number)
);

CREATE INDEX equipment_installments_invoice_idx ON equipment_installments (invoice_id) WHERE invoice_id IS NOT NULL;
CREATE INDEX equipment_installments_pending_idx ON equipment_installments (subscription_id, number)
    WHERE invoice_id IS NULL AND waived_at IS NULL;
