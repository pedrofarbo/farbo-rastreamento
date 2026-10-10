-- A comissão do afiliado passa a ser por veículo (assinatura) e não por
-- cliente: cada mensalidade paga de um veículo do cliente indicado rende uma
-- comissão no mês dela. Uma por afiliado, assinatura e mês.

ALTER TABLE affiliate_commissions
    ADD COLUMN subscription_id UUID REFERENCES subscriptions (id) ON DELETE CASCADE;

-- As que já existem (uma por cliente e mês) ficam com a assinatura da
-- mensalidade paga no mês; o acerto (de hora em hora e antes dos relatórios)
-- cria as dos outros veículos do cliente.
UPDATE affiliate_commissions c SET subscription_id = (
    SELECT i.subscription_id FROM invoices i
    WHERE i.customer_id = c.customer_id AND i.status = 'PAID' AND i.subscription_id IS NOT NULL
        AND date_trunc('month', i.due_date)::date = c.month
    ORDER BY i.due_date, i.created_at
    LIMIT 1);

ALTER TABLE affiliate_commissions
    DROP CONSTRAINT IF EXISTS affiliate_commissions_affiliate_id_customer_id_month_key;
ALTER TABLE affiliate_commissions
    ADD CONSTRAINT affiliate_commissions_affiliate_subscription_month_key UNIQUE (affiliate_id, subscription_id, month);
