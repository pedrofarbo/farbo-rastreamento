-- Rastreadores que tentaram conectar sem estar cadastrados (o IMEI que eles
-- mandam não está em devices): a tela de rastreadores avisa, com o cadastro
-- de IMEI parecido quando houver — o caso típico é um número digitado errado
-- no cadastro, com o rastreador já configurado e sendo recusado. Um por IMEI;
-- attempts conta as tentativas (gravadas no máximo a cada 30 s).
CREATE TABLE unknown_device_attempts (
    imei          TEXT PRIMARY KEY,
    protocol      TEXT        NOT NULL DEFAULT '',
    remote_addr   TEXT        NOT NULL DEFAULT '',
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    attempts      INTEGER     NOT NULL DEFAULT 1
);

CREATE INDEX unknown_device_attempts_last_seen ON unknown_device_attempts (last_seen_at DESC);
