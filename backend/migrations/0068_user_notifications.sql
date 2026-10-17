-- A área de notificações do app do cliente: cada aviso que vai para o
-- celular (alertas dos veículos, lembretes de fatura, modo roubo, bloqueio
-- por quem tem acesso) fica guardado para a pessoa — mesmo sem o celular
-- inscrito, ou com o push desligado. read_at nulo: ainda não lida. As de mais
-- de 90 dias saem na limpeza.
CREATE TABLE user_notifications (
    id         BIGSERIAL PRIMARY KEY,
    user_id    UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    title      TEXT        NOT NULL,
    body       TEXT        NOT NULL DEFAULT '',
    -- Para onde a notificação leva no app (/app/veiculos/..., /app/faturas).
    url        TEXT        NOT NULL DEFAULT '',
    -- info, warning ou critical.
    severity   TEXT        NOT NULL DEFAULT '',
    read_at    TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_user_notifications_user ON user_notifications (user_id, created_at DESC);
CREATE INDEX idx_user_notifications_unread ON user_notifications (user_id) WHERE read_at IS NULL;
