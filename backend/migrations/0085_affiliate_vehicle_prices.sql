-- O caminhão como tipo de veículo (o mapa desenha um caminhão), também no
-- pré-cliente.
ALTER TABLE vehicles DROP CONSTRAINT IF EXISTS vehicles_kind_check;
ALTER TABLE vehicles ADD CONSTRAINT vehicles_kind_check CHECK (kind IN ('CAR', 'MOTORCYCLE', 'TRUCK'));
ALTER TABLE leads DROP CONSTRAINT IF EXISTS leads_vehicle_type_check;
ALTER TABLE leads ADD CONSTRAINT leads_vehicle_type_check CHECK (vehicle_type IN ('', 'moto', 'carro', 'caminhao', 'frota'));

-- A mensalidade pelo link do afiliado, por tipo de veículo (moto, carro e
-- caminhão): quem ele indica paga por essa tabela em todos os veículos,
-- também nos que pedir depois pelo app. Nula: o preço normal do site. O
-- plano que a central define para o cliente vale acima dela.
ALTER TABLE affiliates
    ADD COLUMN plan_motorcycle_cents INTEGER CHECK (plan_motorcycle_cents BETWEEN 1 AND 10000000),
    ADD COLUMN plan_car_cents        INTEGER CHECK (plan_car_cents BETWEEN 1 AND 10000000),
    ADD COLUMN plan_truck_cents      INTEGER CHECK (plan_truck_cents BETWEEN 1 AND 10000000),
    ADD CONSTRAINT affiliates_plan_prices_check CHECK (
        (plan_motorcycle_cents IS NULL) = (plan_car_cents IS NULL)
        AND (plan_car_cents IS NULL) = (plan_truck_cents IS NULL));
