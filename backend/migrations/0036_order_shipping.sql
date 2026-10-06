-- O frete escolhido no pedido (e cobrado na fatura do equipamento): o
-- serviço, o preço e o prazo cotados no Melhor Envios. A etiqueta sai, por
-- padrão, por esse serviço. delivery_arranged: o cliente (da região da base)
-- escolheu combinar a entrega com a central, sem frete nem etiqueta.
ALTER TABLE fulfillments
    ADD COLUMN quoted_service_id  INTEGER,
    ADD COLUMN quoted_service     TEXT NOT NULL DEFAULT '',
    ADD COLUMN quoted_price_cents INTEGER CHECK (quoted_price_cents >= 0),
    ADD COLUMN quoted_days        INTEGER,
    ADD COLUMN delivery_arranged  BOOLEAN NOT NULL DEFAULT FALSE;
