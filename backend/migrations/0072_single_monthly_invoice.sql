-- A fatura única do mês e o rastreador em comodato.
--
-- Tudo o que a conta paga vence no mesmo dia, o da conta: a mensalidade de
-- cada veículo, o equipamento (ou a instalação, no comodato), o frete e o
-- proporcional dos dias de quem contratou no meio do mês. Na contratação
-- nada é cobrado na hora; vai tudo para a próxima fatura.
--
-- O plano do cliente (definido pela central) ganha o rastreador em comodato
-- e as taxas de instalação e de desinstalação; sem plano, o rastreador é
-- vendido, como antes.
ALTER TABLE customer_plans
    ADD COLUMN equipment_mode      TEXT    NOT NULL DEFAULT 'sale' CHECK (equipment_mode IN ('sale', 'loan')),
    ADD COLUMN install_fee_cents   INTEGER NOT NULL DEFAULT 0 CHECK (install_fee_cents >= 0),
    ADD COLUMN uninstall_fee_cents INTEGER NOT NULL DEFAULT 0 CHECK (uninstall_fee_cents >= 0);

-- O que a assinatura contratou: o rastreador em comodato e a taxa de
-- desinstalação, cobrada no encerramento (os termos do dia da contratação).
ALTER TABLE subscriptions
    ADD COLUMN equipment_mode      TEXT    NOT NULL DEFAULT 'sale' CHECK (equipment_mode IN ('sale', 'loan')),
    ADD COLUMN uninstall_fee_cents INTEGER NOT NULL DEFAULT 0 CHECK (uninstall_fee_cents >= 0);
