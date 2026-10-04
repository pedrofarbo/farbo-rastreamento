-- Acessos de terceiros: o cliente dá a outra pessoa (que entra com a própria
-- conta) o acompanhamento de um veículo dele — a posição ao vivo — e, se
-- quiser, o bloqueio do motor numa emergência (celular roubado junto com o
-- veículo, por exemplo). Desbloquear continua com o dono e com a central.
CREATE TABLE vehicle_shares (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    vehicle_id UUID NOT NULL REFERENCES vehicles (id) ON DELETE CASCADE,
    -- Quem deu o acesso. Vale só enquanto ele for o dono do veículo: se o
    -- veículo mudar de dono, o acesso para de valer sozinho.
    owner_id   UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    guest_id   UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    can_block  BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT vehicle_shares_not_self CHECK (owner_id <> guest_id),
    CONSTRAINT vehicle_shares_once UNIQUE (vehicle_id, guest_id)
);
CREATE INDEX idx_vehicle_shares_guest ON vehicle_shares (guest_id);
CREATE INDEX idx_vehicle_shares_owner ON vehicle_shares (owner_id);
