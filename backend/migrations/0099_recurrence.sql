-- Os planos semestral e anual: pagos por período (6 ou 12 meses de uma vez),
-- com o rastreador incluso no valor (nada somado). Ligados nas opções do
-- pedido (a contratação pelo site) e, cada um, no cadastro do representante
-- (o link dele); desligados até a central ligar.
ALTER TABLE order_settings
    ADD COLUMN recurrence_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN semiannual_cents   INTEGER NOT NULL DEFAULT 20000 CHECK (semiannual_cents BETWEEN 0 AND 10000000),
    ADD COLUMN annual_cents       INTEGER NOT NULL DEFAULT 40000 CHECK (annual_cents BETWEEN 0 AND 10000000);

ALTER TABLE affiliates
    ADD COLUMN recurrence_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN semiannual_cents   INTEGER NOT NULL DEFAULT 20000 CHECK (semiannual_cents BETWEEN 0 AND 10000000),
    ADD COLUMN annual_cents       INTEGER NOT NULL DEFAULT 40000 CHECK (annual_cents BETWEEN 0 AND 10000000);

-- O período da assinatura (1: mensal) e, no cancelamento do semestral ou do
-- anual já pago, o fim do período: ela fica ativa até lá e não renova (sem
-- devolução).
ALTER TABLE subscriptions
    ADD COLUMN period_months INTEGER NOT NULL DEFAULT 1 CHECK (period_months IN (1, 6, 12)),
    ADD COLUMN ends_on       DATE;

-- Os meses que a fatura cobre: a comissão do representante e o MRR saem por mês.
ALTER TABLE invoices
    ADD COLUMN period_months INTEGER NOT NULL DEFAULT 1 CHECK (period_months IN (1, 6, 12));
