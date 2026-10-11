-- A frota organizada: grupos de veículos de cada conta (ex.: "Entregas Zona
-- Sul"; cada veículo em no máximo um) e dois campos personalizados, que a
-- conta nomeia (ex.: "Nº do contrato com a prefeitura") e cada veículo
-- preenche. Os dois servem de filtro e de busca.
CREATE TABLE vehicle_groups (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id   UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX idx_vehicle_groups_owner_name ON vehicle_groups (owner_id, lower(name));

ALTER TABLE vehicles ADD COLUMN group_id UUID REFERENCES vehicle_groups (id) ON DELETE SET NULL;
CREATE INDEX idx_vehicles_group ON vehicles (group_id);
ALTER TABLE vehicles ADD COLUMN custom1 TEXT NOT NULL DEFAULT '';
ALTER TABLE vehicles ADD COLUMN custom2 TEXT NOT NULL DEFAULT '';

-- Os nomes dos dois campos personalizados de cada conta.
CREATE TABLE vehicle_field_labels (
    customer_id UUID PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    label1      TEXT NOT NULL DEFAULT '',
    label2      TEXT NOT NULL DEFAULT '',
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
