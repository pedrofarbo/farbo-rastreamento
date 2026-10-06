-- Configuração do rastreador por SMS (Twilio) na ativação: os comandos (APN,
-- servidor, fuso, intervalo) vão para o número do chip, um por vez; quando o
-- rastreador conecta no servidor, o pedido passa a "Configurado".

CREATE TABLE sms_setup_sessions (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id      UUID        NOT NULL REFERENCES devices (id) ON DELETE CASCADE,
    -- O pedido em configuração (para marcar "Configurado" quando conectar).
    fulfillment_id UUID        REFERENCES fulfillments (id) ON DELETE SET NULL,
    -- O número do chip, em E.164.
    phone          TEXT        NOT NULL,
    -- SENDING: mandando os comandos; WAITING: todos enviados, esperando o
    -- rastreador conectar; DONE: conectou; FAILED: um SMS não chegou;
    -- TIMEOUT: não conectou a tempo; CANCELED: interrompida.
    status         TEXT        NOT NULL DEFAULT 'SENDING'
                   CHECK (status IN ('SENDING', 'WAITING', 'DONE', 'FAILED', 'TIMEOUT', 'CANCELED')),
    -- Os passos, na ordem: [{kind, label, text}] com o texto redigido (as
    -- senhas nunca são gravadas aqui; o texto de verdade sai do cadastro na
    -- hora de enviar).
    steps          JSONB       NOT NULL,
    next_step      INTEGER     NOT NULL DEFAULT 0,
    -- Tentativas seguidas sem resposta do Twilio no passo atual.
    attempts       INTEGER     NOT NULL DEFAULT 0,
    error          TEXT        NOT NULL DEFAULT '',
    -- O que aconteceu no fim (ex.: o pedido foi marcado como configurado).
    note           TEXT        NOT NULL DEFAULT '',
    last_sent_at   TIMESTAMPTZ,
    waiting_since  TIMESTAMPTZ,
    connected_at   TIMESTAMPTZ,
    finished_at    TIMESTAMPTZ,
    started_by     UUID        REFERENCES users (id) ON DELETE SET NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
-- Uma configuração por vez em cada rastreador.
CREATE UNIQUE INDEX idx_sms_setup_live ON sms_setup_sessions (device_id) WHERE status IN ('SENDING', 'WAITING');
CREATE INDEX idx_sms_setup_device ON sms_setup_sessions (device_id, created_at DESC);

-- Os SMS enviados (OUT) e as respostas do rastreador (IN). O texto fica
-- redigido: as senhas do cadastro viram ***.
CREATE TABLE sms_messages (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id    UUID        REFERENCES sms_setup_sessions (id) ON DELETE CASCADE,
    device_id     UUID        REFERENCES devices (id) ON DELETE CASCADE,
    direction     TEXT        NOT NULL CHECK (direction IN ('OUT', 'IN')),
    -- O passo da configuração (OUT).
    step          INTEGER,
    phone         TEXT        NOT NULL,
    body          TEXT        NOT NULL,
    provider_sid  TEXT        UNIQUE,
    -- O status do Twilio (queued, sent, delivered, undelivered, failed, received...).
    status        TEXT        NOT NULL,
    error_code    TEXT        NOT NULL DEFAULT '',
    error_message TEXT        NOT NULL DEFAULT '',
    checked_at    TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_sms_messages_session ON sms_messages (session_id, created_at);
CREATE INDEX idx_sms_messages_device ON sms_messages (device_id, created_at DESC);
