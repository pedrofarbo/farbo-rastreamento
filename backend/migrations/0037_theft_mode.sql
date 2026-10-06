-- Modo roubo: o dono (ou quem ele autorizou a bloquear o motor) avisa que o
-- veículo foi roubado. O rastreador passa a mandar a posição com mais
-- frequência (também parado), quem tem acesso ao veículo é avisado e um link
-- público mostra a posição ao vivo (para a polícia), até o dono encerrar ou o
-- prazo acabar. Sem central: quem age é o cliente.
CREATE TABLE theft_modes (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    vehicle_id      UUID        NOT NULL REFERENCES vehicles (id) ON DELETE CASCADE,
    activated_by    UUID        REFERENCES users (id) ON DELETE SET NULL,
    activated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- Desliga sozinho depois do prazo (a bateria da moto não aguenta para sempre).
    expires_at      TIMESTAMPTZ NOT NULL,
    reminded_at     TIMESTAMPTZ,
    -- O intervalo curto no rastreador: PENDING até ele receber (fora do ar,
    -- vai quando ele voltar); SKIPPED se o modelo não muda o intervalo.
    boost_status     TEXT        NOT NULL DEFAULT 'PENDING' CHECK (boost_status IN ('PENDING', 'SENT', 'SKIPPED')),
    boost_command_id UUID        REFERENCES device_commands (id) ON DELETE SET NULL,
    boost_sms_at     TIMESTAMPTZ,
    ended_at         TIMESTAMPTZ,
    ended_by         UUID        REFERENCES users (id) ON DELETE SET NULL,
    outcome          TEXT        CHECK (outcome IN ('RECOVERED', 'CANCELLED', 'EXPIRED')),
    -- A volta ao intervalo normal, depois de encerrar.
    restore_status     TEXT        CHECK (restore_status IN ('PENDING', 'SENT', 'SKIPPED')),
    restore_command_id UUID        REFERENCES device_commands (id) ON DELETE SET NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT theft_modes_ended CHECK ((ended_at IS NULL) = (outcome IS NULL))
);

-- Um modo roubo ligado por veículo.
CREATE UNIQUE INDEX idx_theft_modes_active ON theft_modes (vehicle_id) WHERE ended_at IS NULL;
-- O que a rotina ainda tem a mandar ao rastreador.
CREATE INDEX idx_theft_modes_pending ON theft_modes (id)
    WHERE (ended_at IS NULL AND boost_status = 'PENDING') OR restore_status = 'PENDING';
