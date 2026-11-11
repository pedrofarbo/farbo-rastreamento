-- A comissão do representante por tipo de assinatura: o mensal é a regra de
-- sempre (commission_type, commission_cents, commission_bps); o semestral e
-- o anual podem ter a própria — valor fixo por período pago ou percentual do
-- valor pago, de uma vez no mês do pagamento. Vazio: seguem a do mensal pelos
-- meses do período.
ALTER TABLE affiliates
    ADD COLUMN semiannual_commission_type  TEXT    NOT NULL DEFAULT '' CHECK (semiannual_commission_type IN ('', 'fixed', 'percent')),
    ADD COLUMN semiannual_commission_cents INTEGER NOT NULL DEFAULT 0 CHECK (semiannual_commission_cents BETWEEN 0 AND 10000000),
    ADD COLUMN semiannual_commission_bps   INTEGER NOT NULL DEFAULT 0 CHECK (semiannual_commission_bps BETWEEN 0 AND 10000),
    ADD COLUMN annual_commission_type      TEXT    NOT NULL DEFAULT '' CHECK (annual_commission_type IN ('', 'fixed', 'percent')),
    ADD COLUMN annual_commission_cents     INTEGER NOT NULL DEFAULT 0 CHECK (annual_commission_cents BETWEEN 0 AND 10000000),
    ADD COLUMN annual_commission_bps       INTEGER NOT NULL DEFAULT 0 CHECK (annual_commission_bps BETWEEN 0 AND 10000);
