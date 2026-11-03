-- Quem contrata pelo site só vira cliente quando paga. A conta nasce na
-- contratação (para a pessoa ver e pagar o Pix), marcada com checkout_at, e,
-- enquanto não há nenhuma fatura paga, não aparece como cliente: nem na
-- central, nem nos números do afiliado, em Pedidos, na receita ou na régua de
-- cobrança. Vencido o pagamento sem pagar, o cadastro é apagado.
ALTER TABLE users ADD COLUMN checkout_at TIMESTAMPTZ;

-- A contratação pelo site esperando o pagamento: a conta veio dela e ainda
-- não tem fatura paga.
CREATE FUNCTION checkout_pending(customer UUID) RETURNS BOOLEAN LANGUAGE sql STABLE AS $$
    SELECT EXISTS (SELECT 1 FROM users u WHERE u.id = customer AND u.checkout_at IS NOT NULL)
       AND NOT EXISTS (SELECT 1 FROM invoices i WHERE i.customer_id = customer AND i.status = 'PAID')
$$;

-- As contas que já vieram do site.
UPDATE users u SET checkout_at = u.created_at
FROM leads l
WHERE l.customer_id = u.id AND l.plan LIKE 'Contratação pelo site%' AND u.checkout_at IS NULL;
