-- Pré-clientes: quem deixou o interesse na landing e ainda não tem conta. A
-- equipe entra em contato e, fechando, cadastra o cliente a partir daqui.
CREATE TABLE leads (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name          TEXT NOT NULL,
    email         TEXT NOT NULL,
    phone         TEXT NOT NULL DEFAULT '',
    city          TEXT NOT NULL DEFAULT '',
    -- O plano escolhido na landing, como ela mostra.
    plan          TEXT NOT NULL DEFAULT '',
    vehicle_type  TEXT NOT NULL DEFAULT '' CHECK (vehicle_type IN ('', 'moto', 'carro', 'frota')),
    vehicle_count INTEGER NOT NULL DEFAULT 1 CHECK (vehicle_count BETWEEN 1 AND 500),
    message       TEXT NOT NULL DEFAULT '',
    -- NEW: ninguém falou com ele ainda; CONTACTED: em conversa; CONVERTED:
    -- virou cliente (customer_id); DISCARDED: não vai fechar.
    status        TEXT NOT NULL DEFAULT 'NEW'
                  CHECK (status IN ('NEW', 'CONTACTED', 'CONVERTED', 'DISCARDED')),
    notes         TEXT NOT NULL DEFAULT '',
    customer_id   UUID REFERENCES users (id) ON DELETE SET NULL,
    source        TEXT NOT NULL DEFAULT 'landing',
    -- Quando aceitou ser contatado (LGPD).
    consent_at    TIMESTAMPTZ NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_leads_recent ON leads (created_at DESC);
CREATE INDEX idx_leads_email ON leads (lower(email));
