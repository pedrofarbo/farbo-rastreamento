-- Um Pix pode pagar várias faturas do mesmo cliente de uma vez: a fatura do
-- mês de uma frota (uma por veículo, todas no mesmo vencimento) sai num Pix
-- só. As faturas de cada Pix assim ficam aqui (com o valor de cada uma na
-- hora); o Pix de uma fatura só continua só em payment_charges, e o
-- invoice_id dele é a primeira fatura do grupo.
CREATE TABLE payment_charge_invoices (
    charge_id    UUID    NOT NULL REFERENCES payment_charges (id) ON DELETE CASCADE,
    invoice_id   UUID    NOT NULL REFERENCES invoices (id) ON DELETE CASCADE,
    amount_cents INTEGER NOT NULL CHECK (amount_cents > 0),
    PRIMARY KEY (charge_id, invoice_id)
);
CREATE INDEX idx_payment_charge_invoices_invoice ON payment_charge_invoices (invoice_id);
