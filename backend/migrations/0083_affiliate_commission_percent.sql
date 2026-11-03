-- A comissão do afiliado pode ser um valor fixo por veículo (como antes) ou
-- um percentual da mensalidade paga de cada veículo (sem a parcela do
-- rastreador). O percentual vai em centésimos de ponto: 1000 = 10%.
ALTER TABLE affiliates
    ADD COLUMN commission_type TEXT    NOT NULL DEFAULT 'fixed' CHECK (commission_type IN ('fixed', 'percent')),
    ADD COLUMN commission_bps  INTEGER NOT NULL DEFAULT 0 CHECK (commission_bps BETWEEN 0 AND 10000);
