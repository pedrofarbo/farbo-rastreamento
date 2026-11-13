-- Mais duas opções do link de pagamento:
-- a entrega em mãos: a Farbo entrega o rastreador e combina o dia com o
-- cliente, sem frete (a entrega fica como combinada no acompanhamento);
-- a instalação paga direto ao instalador: o valor aparece para o cliente,
-- mas não entra na 1ª cobrança (o instalador recebe dele no dia).
ALTER TABLE checkout_links ADD COLUMN hand_delivery BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE checkout_links ADD COLUMN install_direct BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE checkout_links DROP CONSTRAINT checkout_links_own_install;
ALTER TABLE checkout_links ADD CONSTRAINT checkout_links_own_install
    CHECK (farbo_install OR (install_cents = 0 AND NOT install_blocking AND NOT install_direct));
