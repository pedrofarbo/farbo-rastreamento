-- Atendimento pelo WhatsApp (API oficial da Meta): cada contato é uma
-- conversa, respondida pelo atendente de IA ou pela equipe.
CREATE TABLE whatsapp_conversations (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- Número do contato como o WhatsApp manda: só dígitos, com o 55.
    wa_id          TEXT NOT NULL UNIQUE,
    contact_name   TEXT NOT NULL DEFAULT '',
    -- Cliente com este telefone no cadastro, quando há.
    customer_id    UUID REFERENCES users (id) ON DELETE SET NULL,
    -- Quem responde: a IA ou a equipe (que assumiu, ou para quem a IA
    -- transferiu).
    mode           TEXT NOT NULL DEFAULT 'BOT' CHECK (mode IN ('BOT', 'HUMAN')),
    handoff_reason TEXT NOT NULL DEFAULT '',
    -- Transferida para a equipe e ainda sem resposta de alguém.
    needs_attention BOOLEAN NOT NULL DEFAULT FALSE,
    -- Última mensagem do contato: abre as 24 h em que se pode responder
    -- com texto livre.
    last_inbound_at TIMESTAMPTZ,
    last_message_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_whatsapp_conversations_recent ON whatsapp_conversations (last_message_at DESC);
CREATE INDEX idx_whatsapp_conversations_attention ON whatsapp_conversations (needs_attention)
    WHERE needs_attention;

CREATE TABLE whatsapp_messages (
    id              BIGSERIAL PRIMARY KEY,
    conversation_id UUID NOT NULL REFERENCES whatsapp_conversations (id) ON DELETE CASCADE,
    direction       TEXT NOT NULL CHECK (direction IN ('IN', 'OUT')),
    -- CONTACT escreveu; BOT é a IA; AGENT, alguém da equipe (agent_id).
    author          TEXT NOT NULL CHECK (author IN ('CONTACT', 'BOT', 'AGENT')),
    agent_id        UUID REFERENCES users (id) ON DELETE SET NULL,
    -- Id da mensagem na Meta: o webhook repetido não grava duas vezes, e os
    -- status (entregue, lida) acham a mensagem enviada.
    wa_message_id   TEXT UNIQUE,
    -- text, audio, image, document...; o que não é texto chega sem corpo.
    kind            TEXT NOT NULL DEFAULT 'text',
    body            TEXT NOT NULL DEFAULT '',
    -- Das enviadas: sent, delivered, read ou failed (com o motivo).
    status          TEXT NOT NULL DEFAULT '',
    error           TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_whatsapp_messages_conversation ON whatsapp_messages (conversation_id, id);
