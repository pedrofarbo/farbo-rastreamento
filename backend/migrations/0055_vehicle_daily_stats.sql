-- O resumo de cada dia (no fuso da central) de cada rastreador, para os
-- relatórios da frota: km, tempo andando, parado com a ignição ligada,
-- viagens, velocidade máxima e excessos. Calculado em segundo plano a partir
-- das posições — só os dias que receberam posições novas — e guardado mesmo
-- depois que as posições antigas são apagadas pela retenção. vehicle_id é o
-- veículo a que o rastreador estava ligado no cálculo: numa troca de
-- aparelho, cada veículo fica com os números dele.
CREATE TABLE vehicle_daily_stats (
    device_id     UUID NOT NULL REFERENCES devices (id) ON DELETE CASCADE,
    day           DATE NOT NULL,
    vehicle_id    UUID REFERENCES vehicles (id) ON DELETE SET NULL,
    distance_m    DOUBLE PRECISION NOT NULL DEFAULT 0,
    moving_s      INTEGER NOT NULL DEFAULT 0,
    idle_s        INTEGER NOT NULL DEFAULT 0,
    trips         INTEGER NOT NULL DEFAULT 0,
    max_speed_kmh DOUBLE PRECISION NOT NULL DEFAULT 0,
    overspeeds    INTEGER NOT NULL DEFAULT 0,
    first_move_at TIMESTAMPTZ,
    last_move_at  TIMESTAMPTZ,
    points        INTEGER NOT NULL DEFAULT 0,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (device_id, day)
);
CREATE INDEX idx_vehicle_daily_stats_vehicle ON vehicle_daily_stats (vehicle_id, day);

-- Até onde as posições já entraram no resumo (o id da última processada).
CREATE TABLE report_progress (
    name             TEXT PRIMARY KEY,
    last_position_id BIGINT NOT NULL DEFAULT 0,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
INSERT INTO report_progress (name) VALUES ('daily_stats');
