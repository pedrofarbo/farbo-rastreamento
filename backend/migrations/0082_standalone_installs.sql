-- A instalação avulsa: a central escolhe o rastreador e manda o link ao
-- instalador, sem agendamento nem veículo. Pelo link (sem login: o id
-- assinado), o instalador testa o sinal e conclui. O link expira na
-- conclusão ou quando a central encerra.
CREATE TABLE standalone_installs (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id      UUID        NOT NULL REFERENCES devices (id) ON DELETE CASCADE,
    -- O prestador da lista de instaladores (ou só o nome, de fora da lista).
    installer_id   UUID        REFERENCES installers (id) ON DELETE SET NULL,
    installer_name TEXT        NOT NULL DEFAULT '',
    notes          TEXT        NOT NULL DEFAULT '',
    status         TEXT        NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'done', 'closed')),
    -- O último teste do sinal feito pelo link.
    checked_at     TIMESTAMPTZ,
    check_ready    BOOLEAN     NOT NULL DEFAULT FALSE,
    completed_at   TIMESTAMPTZ,
    installer_note TEXT        NOT NULL DEFAULT '',
    closed_at      TIMESTAMPTZ,
    closed_by      UUID        REFERENCES users (id) ON DELETE SET NULL,
    created_by     UUID        REFERENCES users (id) ON DELETE SET NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
-- Um link aberto por rastreador.
CREATE UNIQUE INDEX idx_standalone_installs_open ON standalone_installs (device_id) WHERE status = 'open';
CREATE INDEX idx_standalone_installs_updated ON standalone_installs (updated_at DESC);
