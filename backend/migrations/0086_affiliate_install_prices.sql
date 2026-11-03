-- A instalação e a desinstalação pelo link do afiliado: as da Farbo (modo
-- vazio), preços próprios cobrados pela Farbo (custom) ou o representante,
-- que instala, desinstala e recebe direto do cliente, sem passar pela
-- plataforma (direct: a Farbo não cobra nada disso). Vale para quem ele
-- indica, também nos veículos que pedir depois pelo app; o plano que a
-- central define para o cliente vale acima.
ALTER TABLE affiliates
    ADD COLUMN install_mode    TEXT NOT NULL DEFAULT '' CHECK (install_mode IN ('', 'custom', 'direct')),
    ADD COLUMN install_cents   INTEGER CHECK (install_cents BETWEEN 0 AND 10000000),
    ADD COLUMN uninstall_cents INTEGER CHECK (uninstall_cents BETWEEN 0 AND 10000000),
    ADD CONSTRAINT affiliates_install_prices_check CHECK (
        (install_mode = '') = (install_cents IS NULL) AND (install_mode = '') = (uninstall_cents IS NULL));
