-- O andamento da instalação pelo link: o último teste do sinal (o rastreador
-- testado e se passou) e o aviso da instalação parada — o horário passou sem
-- a chegada (late) ou o instalador chegou e não concluiu (stalled). Um aviso
-- por situação; remarcar zera.
ALTER TABLE installation_appointments
    ADD COLUMN checked_at      TIMESTAMPTZ,
    ADD COLUMN check_ready     BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN check_device_id UUID REFERENCES devices (id) ON DELETE SET NULL,
    ADD COLUMN stall_notice    TEXT NOT NULL DEFAULT '' CHECK (stall_notice IN ('', 'late', 'stalled'));
