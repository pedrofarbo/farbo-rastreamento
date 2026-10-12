-- O hodômetro e a manutenção dos veículos.
--
-- O hodômetro é a leitura que o cliente informa mais os km medidos pelo GPS
-- desde então (os do resumo diário dos relatórios). Guardamos o ajuste:
-- odometer_offset_m = leitura informada - km medidos até aquele momento;
-- o hodômetro de agora é o ajuste + todos os km medidos.
ALTER TABLE vehicles ADD COLUMN odometer_offset_m DOUBLE PRECISION;
ALTER TABLE vehicles ADD COLUMN odometer_set_at TIMESTAMPTZ;

-- Cada manutenção de um veículo: a cada tantos km e/ou meses, contando da
-- última vez que foi feita. notified é até onde o aviso por e-mail já foi
-- (soon: perto; due: vencida); registrar a manutenção zera.
CREATE TABLE maintenance_items (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    vehicle_id   UUID        NOT NULL REFERENCES vehicles (id) ON DELETE CASCADE,
    name         TEXT        NOT NULL,
    every_km     INTEGER CHECK (every_km > 0),
    every_months INTEGER CHECK (every_months > 0),
    last_km      DOUBLE PRECISION,
    last_date    DATE        NOT NULL,
    notified     TEXT CHECK (notified IN ('soon', 'due')),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT maintenance_items_interval CHECK (every_km IS NOT NULL OR every_months IS NOT NULL)
);
CREATE INDEX idx_maintenance_items_vehicle ON maintenance_items (vehicle_id);

-- O histórico: cada vez que a manutenção foi feita (fica mesmo se o item
-- for apagado).
CREATE TABLE maintenance_records (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    vehicle_id UUID        NOT NULL REFERENCES vehicles (id) ON DELETE CASCADE,
    item_id    UUID        REFERENCES maintenance_items (id) ON DELETE SET NULL,
    name       TEXT        NOT NULL,
    done_date  DATE        NOT NULL,
    km         DOUBLE PRECISION,
    notes      TEXT        NOT NULL DEFAULT '',
    created_by UUID        REFERENCES users (id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_maintenance_records_vehicle ON maintenance_records (vehicle_id, done_date DESC);
