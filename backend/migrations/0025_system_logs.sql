-- Avisos e erros do servidor da API, para o painel de infraestrutura
-- (Diagnóstico → Servidor). Gravados em segundo plano a partir do próprio log;
-- ficam 30 dias.
CREATE TABLE system_logs (
    id         BIGSERIAL PRIMARY KEY,
    logged_at  TIMESTAMPTZ NOT NULL,
    level      TEXT NOT NULL CHECK (level IN ('WARN', 'ERROR')),
    -- O "component" do logger (ex.: auth, shares), quando houver.
    component  TEXT NOT NULL DEFAULT '',
    message    TEXT NOT NULL,
    attrs      JSONB NOT NULL DEFAULT '{}'::jsonb
);
CREATE INDEX idx_system_logs_recent ON system_logs (logged_at DESC);
