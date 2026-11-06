-- Os impostos da Farbo sobre a receita, em centésimos de ponto (600 = 6%):
-- as comissões em percentual saem da mensalidade líquida deles (o afiliado
-- arca com a parte dele do imposto). Zero: sobre o valor cheio.
ALTER TABLE affiliate_settings
    ADD COLUMN commission_tax_bps INTEGER NOT NULL DEFAULT 0 CHECK (commission_tax_bps BETWEEN 0 AND 3000);
