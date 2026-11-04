-- Os kits de instalação dos representantes: a central manda rastreadores (já
-- com o chip) para o representante que entrega em mãos e instala. Cada
-- remessa tem os rastreadores, o envio (a transportadora e o código de
-- rastreio) e a entrega; o representante também pede kits e acompanha tudo
-- pela página dele. O estoque dele são os rastreadores das remessas ainda
-- sem veículo.
CREATE TABLE kit_shipments (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    affiliate_id       UUID        NOT NULL REFERENCES affiliates (id) ON DELETE CASCADE,
    status             TEXT        NOT NULL DEFAULT 'preparing'
                       CHECK (status IN ('requested', 'preparing', 'shipped', 'delivered', 'canceled')),
    -- O pedido do representante: quantos kits e a observação dele.
    requested_quantity INTEGER     CHECK (requested_quantity BETWEEN 1 AND 500),
    request_note       TEXT        NOT NULL DEFAULT '',
    carrier            TEXT        NOT NULL DEFAULT '',
    tracking_code      TEXT        NOT NULL DEFAULT '',
    notes              TEXT        NOT NULL DEFAULT '',
    shipped_at         TIMESTAMPTZ,
    delivered_at       TIMESTAMPTZ,
    canceled_at        TIMESTAMPTZ,
    -- Quem criou na central (nulo: o pedido do representante, pela página).
    created_by         UUID        REFERENCES users (id) ON DELETE SET NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_kit_shipments_affiliate ON kit_shipments (affiliate_id, created_at DESC);

-- Os rastreadores de cada remessa: um rastreador está numa remessa só (a
-- cancelada solta os dela).
CREATE TABLE kit_shipment_devices (
    shipment_id UUID NOT NULL REFERENCES kit_shipments (id) ON DELETE CASCADE,
    device_id   UUID NOT NULL UNIQUE REFERENCES devices (id) ON DELETE CASCADE,
    PRIMARY KEY (shipment_id, device_id)
);
