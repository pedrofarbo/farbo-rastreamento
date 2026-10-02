-- Cercas do cliente.
--
-- Sem dono (owner_id nulo), a cerca é da central e vale para todos os
-- veículos, como antes. Com dono, é do cliente: vale só para os veículos dele
-- escolhidos em geofence_vehicles, e só ele vê e recebe os avisos.
ALTER TABLE geofences ADD COLUMN owner_id UUID REFERENCES users (id) ON DELETE CASCADE;
CREATE INDEX idx_geofences_owner ON geofences (owner_id);

-- Avisar (e-mail e celular) na entrada e/ou na saída. Só valem para as cercas
-- do cliente: a central acompanha as dela pelos eventos.
ALTER TABLE geofences ADD COLUMN notify_enter BOOLEAN NOT NULL DEFAULT TRUE;
ALTER TABLE geofences ADD COLUMN notify_exit  BOOLEAN NOT NULL DEFAULT TRUE;

CREATE TABLE geofence_vehicles (
    geofence_id UUID NOT NULL REFERENCES geofences (id) ON DELETE CASCADE,
    vehicle_id  UUID NOT NULL REFERENCES vehicles (id) ON DELETE CASCADE,
    PRIMARY KEY (geofence_id, vehicle_id)
);
CREATE INDEX idx_geofence_vehicles_vehicle ON geofence_vehicles (vehicle_id);
