-- O boleto (com Pix junto) como forma de pagamento das faturas, pela
-- AbacatePay. method diz o que foi gerado; no boleto, a linha digitável, o
-- código de barras e o link para ver e imprimir. br_code e qr_code_image
-- guardam o Pix que vem no boleto.
ALTER TABLE payment_charges
    ADD COLUMN method         TEXT NOT NULL DEFAULT 'PIX' CHECK (method IN ('PIX', 'BOLETO')),
    ADD COLUMN boleto_line    TEXT NOT NULL DEFAULT '',
    ADD COLUMN boleto_barcode TEXT NOT NULL DEFAULT '',
    ADD COLUMN boleto_url     TEXT NOT NULL DEFAULT '';

-- A varredura dos boletos: pendentes até o vencimento e, depois dele, os
-- dias em que o pagamento do último dia ainda compensa.
CREATE INDEX idx_payment_charges_boleto ON payment_charges (expires_at) WHERE method = 'BOLETO';
