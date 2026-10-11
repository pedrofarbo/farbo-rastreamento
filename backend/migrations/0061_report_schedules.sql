-- Os relatórios da frota por e-mail: cada pessoa da conta (o titular ou
-- alguém da equipe) agenda o resumo semanal ou mensal, da conta toda ou de
-- um grupo, para si e, se puder mexer na conta, para mais alguns e-mails.
-- O envio usa o acesso da pessoa na hora: quem está restrito a grupos só
-- recebe os veículos deles, e quem saiu da conta deixa de receber.
CREATE TABLE report_schedules (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id       UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    user_id          UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    frequency        TEXT        NOT NULL CHECK (frequency IN ('weekly', 'monthly')),
    -- O grupo do relatório (nulo: a conta toda). Apagado o grupo, o
    -- agendamento sai junto: nunca passa sozinho a mandar a frota inteira.
    group_id         UUID        REFERENCES vehicle_groups (id) ON DELETE CASCADE,
    extra_recipients TEXT[]      NOT NULL DEFAULT '{}',
    -- O início do último período enviado (o primeiro dia da semana ou do mês).
    last_period      DATE,
    last_sent_at     TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_report_schedules_user ON report_schedules (user_id);
CREATE INDEX idx_report_schedules_account ON report_schedules (account_id);
