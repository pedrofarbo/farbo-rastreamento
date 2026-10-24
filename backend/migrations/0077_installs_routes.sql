-- A agenda de instalação e de desinstalação: a central marca o dia e a hora
-- e manda o link ao instalador, que segue o passo a passo no celular (sem
-- login: o link é o id assinado). Na instalação: chegou, o IMEI, o teste do
-- sinal ao vivo, as fotos e a conclusão, que liga o rastreador ao veículo. Na
-- desinstalação: chegou, a foto do rastreador retirado e a conclusão, que
-- tira o rastreador do veículo (e a taxa entra na fatura da associação).
CREATE TABLE installation_appointments (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    kind           TEXT        NOT NULL DEFAULT 'install' CHECK (kind IN ('install', 'uninstall')),
    vehicle_id     UUID        NOT NULL REFERENCES vehicles (id) ON DELETE CASCADE,
    -- O prestador da lista de instaladores (ou só o nome, de fora da lista).
    installer_id   UUID        REFERENCES installers (id) ON DELETE SET NULL,
    installer_name TEXT        NOT NULL DEFAULT '',
    scheduled_for  TIMESTAMPTZ NOT NULL,
    duration_min   SMALLINT    NOT NULL DEFAULT 60 CHECK (duration_min BETWEEN 15 AND 480),
    address        TEXT        NOT NULL DEFAULT '',
    notes          TEXT        NOT NULL DEFAULT '',
    status         TEXT        NOT NULL DEFAULT 'scheduled'
                   CHECK (status IN ('scheduled', 'arrived', 'done', 'canceled', 'no_show')),
    arrived_at     TIMESTAMPTZ,
    -- O rastreador instalado (o IMEI conferido) e quando terminou.
    device_id      UUID        REFERENCES devices (id) ON DELETE SET NULL,
    completed_at   TIMESTAMPTZ,
    installer_note TEXT        NOT NULL DEFAULT '',
    created_by     UUID        REFERENCES users (id) ON DELETE SET NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_installation_appointments_day ON installation_appointments (scheduled_for);
CREATE INDEX idx_installation_appointments_vehicle ON installation_appointments (vehicle_id);
-- Um agendamento em aberto por veículo.
CREATE UNIQUE INDEX idx_installation_appointments_open ON installation_appointments (vehicle_id)
    WHERE status IN ('scheduled', 'arrived');

-- O pedido de desinstalação da associação (o associado trocou de carro, saiu):
-- o veículo entra na fila da central até o rastreador ser retirado. O
-- associado desligado entra na fila sem pedido.
ALTER TABLE vehicles
    ADD COLUMN uninstall_requested_at TIMESTAMPTZ,
    ADD COLUMN uninstall_note         TEXT NOT NULL DEFAULT '';

CREATE TABLE installation_photos (
    appointment_id UUID        NOT NULL REFERENCES installation_appointments (id) ON DELETE CASCADE,
    slot           TEXT        NOT NULL CHECK (slot IN ('tracker', 'vehicle', 'dashboard')),
    content_type   TEXT        NOT NULL CHECK (content_type IN ('image/jpeg', 'image/png')),
    size_bytes     INTEGER     NOT NULL,
    data           BYTEA       NOT NULL,
    taken_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (appointment_id, slot)
);

-- Os itinerários (as linhas de ônibus da empresa): os pontos em ordem, com o
-- minuto previsto desde a saída; as viagens de cada veículo na linha. O link
-- público (o id assinado) mostra ao passageiro onde o ônibus está.
CREATE TABLE bus_routes (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name       TEXT        NOT NULL,
    color      TEXT        NOT NULL DEFAULT '#22c55e' CHECK (color ~ '^#[0-9a-fA-F]{6}$'),
    -- Sair mais longe que isso do caminho entre os pontos é desvio.
    deviation_m INTEGER    NOT NULL DEFAULT 500 CHECK (deviation_m BETWEEN 100 AND 5000),
    public     BOOLEAN     NOT NULL DEFAULT FALSE,
    active     BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_bus_routes_account ON bus_routes (account_id);

CREATE TABLE bus_route_stops (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    route_id   UUID             NOT NULL REFERENCES bus_routes (id) ON DELETE CASCADE,
    seq        SMALLINT         NOT NULL,
    name       TEXT             NOT NULL,
    latitude   DOUBLE PRECISION NOT NULL CHECK (latitude BETWEEN -90 AND 90),
    longitude  DOUBLE PRECISION NOT NULL CHECK (longitude BETWEEN -180 AND 180),
    radius_m   INTEGER          NOT NULL DEFAULT 80 CHECK (radius_m BETWEEN 20 AND 1000),
    -- Os minutos previstos desde a saída da viagem.
    offset_min INTEGER          NOT NULL DEFAULT 0 CHECK (offset_min BETWEEN 0 AND 1440),
    UNIQUE (route_id, seq)
);

CREATE TABLE bus_trips (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    route_id   UUID        NOT NULL REFERENCES bus_routes (id) ON DELETE CASCADE,
    vehicle_id UUID        NOT NULL REFERENCES vehicles (id) ON DELETE CASCADE,
    departs_at TIMESTAMPTZ NOT NULL,
    canceled   BOOLEAN     NOT NULL DEFAULT FALSE,
    created_by UUID        REFERENCES users (id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_bus_trips_route ON bus_trips (route_id, departs_at DESC);
CREATE INDEX idx_bus_trips_vehicle ON bus_trips (vehicle_id, departs_at DESC);
