-- O horímetro (horas de motor) e a manutenção por horas.
--
-- As horas de motor são o tempo andando mais o parado com a ignição ligada,
-- do resumo diário dos relatórios (moving_s + idle_s). Sem a leitura do
-- horímetro do veículo, valem as horas medidas pelo rastreador; com ela,
-- guardamos o ajuste, como no hodômetro: engine_offset_s = leitura - horas
-- medidas até aquele momento.
ALTER TABLE vehicles ADD COLUMN engine_offset_s DOUBLE PRECISION;
ALTER TABLE vehicles ADD COLUMN engine_set_at TIMESTAMPTZ;

-- A manutenção vence também a cada tantas horas de motor, contando das
-- horas da última vez (last_hours, sempre conhecidas: sem horímetro, as
-- medidas).
ALTER TABLE maintenance_items ADD COLUMN every_hours INTEGER CHECK (every_hours > 0);
ALTER TABLE maintenance_items ADD COLUMN last_hours DOUBLE PRECISION;
ALTER TABLE maintenance_items DROP CONSTRAINT maintenance_items_interval;
ALTER TABLE maintenance_items ADD CONSTRAINT maintenance_items_interval
    CHECK (every_km IS NOT NULL OR every_months IS NOT NULL OR every_hours IS NOT NULL);

-- As que já existem contam das horas de agora.
UPDATE maintenance_items i SET last_hours = COALESCE((
    SELECT sum(st.moving_s + st.idle_s) FROM vehicle_daily_stats st WHERE st.vehicle_id = i.vehicle_id), 0) / 3600.0;

ALTER TABLE maintenance_records ADD COLUMN hours DOUBLE PRECISION;
