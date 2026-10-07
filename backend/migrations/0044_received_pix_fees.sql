-- A tarifa da AbacatePay sobre o Pix recebido (R$ 0,80 por transação): ela
-- desconta do valor pago, e o saldo recebe o líquido. A tarifa vira uma
-- despesa paga (Taxas de pagamento) no dia do pagamento, para o caixa bater
-- com a AbacatePay. platform_fee_cents é o que ela informou ao gerar o Pix
-- (0: não informou; vale a tabela); fee_entry_id, a despesa lançada.
ALTER TABLE payment_charges
    ADD COLUMN platform_fee_cents INTEGER NOT NULL DEFAULT 0 CHECK (platform_fee_cents >= 0),
    ADD COLUMN fee_entry_id UUID REFERENCES finance_entries (id) ON DELETE SET NULL;
