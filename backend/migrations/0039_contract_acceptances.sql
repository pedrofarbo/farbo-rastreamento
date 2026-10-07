-- O aceite do contrato de prestação de serviços pelo cliente (no primeiro
-- acesso e a cada versão nova): o que foi aceito (versão e hash do texto),
-- por quem (nome e CPF/CNPJ informados) e de onde (IP e navegador). É a
-- prova do aceite eletrônico.
CREATE TABLE contract_acceptances (
    id             BIGSERIAL PRIMARY KEY,
    user_id        UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    version        TEXT        NOT NULL,
    content_sha256 TEXT        NOT NULL,
    name           TEXT        NOT NULL,
    document       TEXT        NOT NULL,
    ip             TEXT        NOT NULL DEFAULT '',
    user_agent     TEXT        NOT NULL DEFAULT '',
    accepted_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT contract_acceptances_once UNIQUE (user_id, version)
);
