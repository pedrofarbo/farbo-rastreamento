-- A adesão digital: o link da associação (ela liga no painel) por onde quem
-- quer entrar manda os dados e o veículo, faz a autovistoria de adesão e
-- aceita o termo do associado, com a preferência de dia e hora para a
-- instalação. A associação aprova: o associado e o veículo são cadastrados
-- (o convite do app vai por e-mail) e o veículo entra na fila de instalação
-- da central, com o endereço e a preferência.

ALTER TABLE organizations ADD COLUMN adhesion_enabled BOOLEAN NOT NULL DEFAULT FALSE;

CREATE TABLE adhesions (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id    UUID        NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    -- pending: falta a vistoria ou o termo; submitted: espera a associação;
    -- approved: cadastrado; rejected: recusada (com o motivo).
    status             TEXT        NOT NULL DEFAULT 'pending'
                       CHECK (status IN ('pending', 'submitted', 'approved', 'rejected')),
    name               TEXT        NOT NULL,
    document           TEXT        NOT NULL,
    email              TEXT        NOT NULL,
    phone              TEXT        NOT NULL,
    -- O endereço da instalação (o formato de addresses.Address).
    address            JSONB       NOT NULL,
    vehicle_type       TEXT        NOT NULL CHECK (vehicle_type IN ('car', 'motorcycle')),
    plate              TEXT        NOT NULL,
    brand              TEXT        NOT NULL DEFAULT '',
    model              TEXT        NOT NULL DEFAULT '',
    year               INTEGER,
    color              TEXT        NOT NULL DEFAULT '',
    install_preference TEXT        NOT NULL DEFAULT '',
    -- A vistoria de adesão (a original da série; a refação abre no mesmo link).
    inspection_id      UUID        REFERENCES inspections (id) ON DELETE SET NULL,
    -- O aceite do termo do associado: a versão, o hash do texto, quando, o IP e o navegador.
    term_version       TEXT        NOT NULL DEFAULT '',
    term_sha256        TEXT        NOT NULL DEFAULT '',
    accepted_at        TIMESTAMPTZ,
    accept_ip          TEXT        NOT NULL DEFAULT '',
    accept_user_agent  TEXT        NOT NULL DEFAULT '',
    -- O cadastro feito na aprovação.
    associate_id       UUID        REFERENCES users (id) ON DELETE SET NULL,
    vehicle_id         UUID        REFERENCES vehicles (id) ON DELETE SET NULL,
    review_note        TEXT        NOT NULL DEFAULT '',
    reviewed_by        UUID        REFERENCES users (id) ON DELETE SET NULL,
    reviewed_at        TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_adhesions_org ON adhesions (organization_id, created_at DESC);
-- Uma adesão em aberto por placa na organização.
CREATE UNIQUE INDEX idx_adhesions_open_plate ON adhesions (organization_id, plate) WHERE status IN ('pending', 'submitted');
CREATE INDEX idx_adhesions_vehicle ON adhesions (vehicle_id) WHERE vehicle_id IS NOT NULL;
