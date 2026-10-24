-- A gestão da frota das empresas (ônibus, locadoras, transportadoras): os
-- motoristas e quem dirige cada veículo, os custos (combustível, pedágio,
-- manutenção...), as multas e as locações. Tudo é da conta do cliente
-- (account_id: o titular), e os veículos são os dele.

CREATE TABLE drivers (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id       UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name             TEXT        NOT NULL,
    phone            TEXT        NOT NULL DEFAULT '',
    document         TEXT        NOT NULL DEFAULT '',
    license_number   TEXT        NOT NULL DEFAULT '',
    license_category TEXT        NOT NULL DEFAULT '',
    license_expires  DATE,
    active           BOOLEAN     NOT NULL DEFAULT TRUE,
    notes            TEXT        NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_drivers_account ON drivers (account_id, name);

-- Quem dirigiu cada veículo e quando (o atual tem ends_at nulo): é por aqui
-- que a jornada e as multas acham o motorista.
CREATE TABLE driver_assignments (
    id         BIGSERIAL PRIMARY KEY,
    vehicle_id UUID        NOT NULL REFERENCES vehicles (id) ON DELETE CASCADE,
    driver_id  UUID        NOT NULL REFERENCES drivers (id) ON DELETE CASCADE,
    starts_at  TIMESTAMPTZ NOT NULL,
    ends_at    TIMESTAMPTZ,
    created_by UUID        REFERENCES users (id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (ends_at IS NULL OR ends_at > starts_at)
);
CREATE INDEX idx_driver_assignments_vehicle ON driver_assignments (vehicle_id, starts_at DESC);
CREATE INDEX idx_driver_assignments_driver ON driver_assignments (driver_id, starts_at DESC);
CREATE UNIQUE INDEX idx_driver_assignments_open ON driver_assignments (vehicle_id) WHERE ends_at IS NULL;

-- Os custos de cada veículo. No combustível, os litros, o hodômetro e se
-- encheu o tanque (o consumo sai de um tanque cheio ao outro).
CREATE TABLE fleet_expenses (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id   UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    vehicle_id   UUID        NOT NULL REFERENCES vehicles (id) ON DELETE CASCADE,
    kind         TEXT        NOT NULL CHECK (kind IN ('fuel', 'maintenance', 'toll', 'parking', 'insurance', 'tax',
                                                       'wash', 'fine', 'other')),
    spent_on     DATE        NOT NULL,
    amount_cents INTEGER     NOT NULL CHECK (amount_cents > 0),
    liters       NUMERIC(8, 2) CHECK (liters IS NULL OR liters > 0),
    odometer_km  NUMERIC(10, 1) CHECK (odometer_km IS NULL OR odometer_km >= 0),
    full_tank    BOOLEAN     NOT NULL DEFAULT FALSE,
    description  TEXT        NOT NULL DEFAULT '',
    created_by   UUID        REFERENCES users (id) ON DELETE SET NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_fleet_expenses_account ON fleet_expenses (account_id, spent_on DESC);
CREATE INDEX idx_fleet_expenses_vehicle ON fleet_expenses (vehicle_id, spent_on);

-- As multas: o motorista sai de quem dirigia na hora (dá para trocar).
CREATE TABLE traffic_fines (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id       UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    vehicle_id       UUID        NOT NULL REFERENCES vehicles (id) ON DELETE CASCADE,
    driver_id        UUID        REFERENCES drivers (id) ON DELETE SET NULL,
    occurred_at      TIMESTAMPTZ NOT NULL,
    place            TEXT        NOT NULL DEFAULT '',
    description      TEXT        NOT NULL,
    code             TEXT        NOT NULL DEFAULT '',
    amount_cents     INTEGER     NOT NULL DEFAULT 0 CHECK (amount_cents >= 0),
    points           SMALLINT    NOT NULL DEFAULT 0 CHECK (points BETWEEN 0 AND 7),
    due_on           DATE,
    status           TEXT        NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'paid', 'appealed', 'canceled')),
    driver_indicated BOOLEAN     NOT NULL DEFAULT FALSE,
    notes            TEXT        NOT NULL DEFAULT '',
    created_by       UUID        REFERENCES users (id) ON DELETE SET NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_traffic_fines_account ON traffic_fines (account_id, occurred_at DESC);

-- As locações: o veículo com quem alugou, de quando a quando; passou da
-- hora sem devolver, a conta é avisada (uma vez).
CREATE TABLE rentals (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id          UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    vehicle_id          UUID        NOT NULL REFERENCES vehicles (id) ON DELETE CASCADE,
    renter_name         TEXT        NOT NULL,
    renter_phone        TEXT        NOT NULL DEFAULT '',
    renter_document     TEXT        NOT NULL DEFAULT '',
    starts_at           TIMESTAMPTZ NOT NULL,
    ends_at             TIMESTAMPTZ NOT NULL,
    returned_at         TIMESTAMPTZ,
    km_start            NUMERIC(10, 1),
    km_end              NUMERIC(10, 1),
    notes               TEXT        NOT NULL DEFAULT '',
    overdue_notified_at TIMESTAMPTZ,
    created_by          UUID        REFERENCES users (id) ON DELETE SET NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (ends_at > starts_at)
);
CREATE INDEX idx_rentals_account ON rentals (account_id, starts_at DESC);
CREATE UNIQUE INDEX idx_rentals_open ON rentals (vehicle_id) WHERE returned_at IS NULL;

-- A API para integrações (sistemas da empresa, da seguradora): a chave é
-- mostrada uma vez; aqui fica só o hash. owner_id é a conta do cliente ou a
-- organização.
CREATE TABLE api_keys (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id     UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name         TEXT        NOT NULL,
    -- O começo da chave, para a pessoa reconhecer na lista.
    prefix       TEXT        NOT NULL UNIQUE,
    hash         BYTEA       NOT NULL,
    created_by   UUID        REFERENCES users (id) ON DELETE SET NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_used_at TIMESTAMPTZ,
    revoked_at   TIMESTAMPTZ
);
CREATE INDEX idx_api_keys_owner ON api_keys (owner_id, created_at DESC);

-- Os webhooks: os eventos dos veículos vão por POST, assinados (HMAC-SHA256
-- com o segredo do webhook), para o endereço da empresa.
CREATE TABLE webhooks (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id     UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    url          TEXT        NOT NULL,
    secret       TEXT        NOT NULL,
    -- Os tipos de evento (vazio: todos).
    events       TEXT[]      NOT NULL DEFAULT '{}',
    active       BOOLEAN     NOT NULL DEFAULT TRUE,
    -- A última entrega: o status HTTP (0: não conectou) e o erro; as falhas
    -- seguidas desligam o webhook.
    last_status  INTEGER,
    last_error   TEXT        NOT NULL DEFAULT '',
    last_sent_at TIMESTAMPTZ,
    failures     INTEGER     NOT NULL DEFAULT 0,
    created_by   UUID        REFERENCES users (id) ON DELETE SET NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_webhooks_owner ON webhooks (owner_id);

-- Os corretores da seguradora: o membro da equipe com o papel broker vê só
-- os associados que são dele (broker_id).
ALTER TABLE organization_members DROP CONSTRAINT organization_members_role_check;
ALTER TABLE organization_members ADD CONSTRAINT organization_members_role_check
    CHECK (role IN ('admin', 'operator', 'viewer', 'broker'));
ALTER TABLE users ADD COLUMN broker_id UUID REFERENCES users (id) ON DELETE SET NULL;
CREATE INDEX idx_users_broker ON users (broker_id) WHERE broker_id IS NOT NULL;
