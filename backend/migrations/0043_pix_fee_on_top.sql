-- A AbacatePay desconta a tarifa do valor enviado: para o fornecedor receber
-- o valor da conta, o envio leva a conta + a tarifa (paga pela empresa).
-- amount_cents continua sendo o que o fornecedor deve receber; sent_cents é
-- o que foi pedido à AbacatePay. Os envios de antes saíram sem a tarifa
-- somada (o fornecedor recebeu o valor menos a tarifa).
ALTER TABLE finance_pix_transfers ADD COLUMN sent_cents BIGINT;
UPDATE finance_pix_transfers SET sent_cents = amount_cents;
ALTER TABLE finance_pix_transfers ALTER COLUMN sent_cents SET NOT NULL,
    ADD CHECK (sent_cents >= amount_cents);
