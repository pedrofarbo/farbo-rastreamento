-- O tipo do veículo (carro ou moto): o mapa desenha um ou outro. Os já
-- cadastrados ficam como carro, menos os que se dizem moto no nome.
ALTER TABLE vehicles
    ADD COLUMN kind TEXT NOT NULL DEFAULT 'CAR' CHECK (kind IN ('CAR', 'MOTORCYCLE'));

UPDATE vehicles SET kind = 'MOTORCYCLE' WHERE name ILIKE '%moto%' OR model ILIKE '%moto%';
