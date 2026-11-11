-- A instalação com o relé do bloqueio do motor: o preço dela no link do
-- representante, ao lado da instalação sem bloqueio (install_cents); nulo, o
-- link não oferece a escolha. E a escolha do cliente na assinatura, que a
-- agenda, a página do representante e o link do instalador mostram.
ALTER TABLE affiliates
    ADD COLUMN install_blocking_cents INTEGER CHECK (install_blocking_cents BETWEEN 0 AND 10000000),
    ADD CONSTRAINT affiliates_install_blocking_check CHECK (install_mode <> '' OR install_blocking_cents IS NULL);
ALTER TABLE subscriptions ADD COLUMN install_blocking BOOLEAN NOT NULL DEFAULT FALSE;
