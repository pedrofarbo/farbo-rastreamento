-- As recargas da carteira do Melhor Envios geradas pelo painel: o Pix (ou o
-- boleto) que põe saldo para as etiquetas. Paga pela AbacatePay, a recarga
-- vira uma conta a pagar (Frete e envio) e sai pelo mesmo Pix dos
-- fornecedores — uma conta por recarga, para não pagar duas vezes.
CREATE TABLE shipping_topups (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- O pagamento no Melhor Envios (id e protocolo PAY-...).
    provider_id TEXT        NOT NULL DEFAULT '',
    protocol    TEXT        NOT NULL DEFAULT '',
    method      TEXT        NOT NULL CHECK (method IN ('pix', 'boleto')),
    value_cents INTEGER     NOT NULL CHECK (value_cents > 0),
    -- A página do Pix (QR Code) ou o PDF do boleto, e a linha do boleto.
    link        TEXT        NOT NULL DEFAULT '',
    digitable   TEXT        NOT NULL DEFAULT '',
    -- O Pix copia-e-cola: da resposta do Melhor Envios ou colado no painel.
    pix_code    TEXT        NOT NULL DEFAULT '',
    entry_id    UUID        REFERENCES finance_entries (id) ON DELETE SET NULL,
    created_by  UUID        REFERENCES users (id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_shipping_topups_created ON shipping_topups (created_at DESC);
