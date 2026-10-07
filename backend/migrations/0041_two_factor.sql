-- Verificação em duas etapas: depois da senha, o código do app autenticador
-- (TOTP) ou do e-mail. Obrigatória para a equipe; opcional para o cliente.

-- O método de cada pessoa. enabled_at nulo: ainda não ativou (pode estar no
-- meio da ativação, com o pendente). O segredo do app fica cifrado (AES-GCM,
-- chave derivada do JWT_SECRET).
CREATE TABLE user_two_factor (
    user_id          UUID PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    method           TEXT        NOT NULL DEFAULT '' CHECK (method IN ('', 'totp', 'email')),
    totp_secret      TEXT        NOT NULL DEFAULT '',
    -- O último passo de 30 s aceito: o mesmo código não vale duas vezes.
    totp_last_step   BIGINT      NOT NULL DEFAULT 0,
    enabled_at       TIMESTAMPTZ,
    -- A ativação (ou troca de método) em andamento.
    pending_method   TEXT        NOT NULL DEFAULT '' CHECK (pending_method IN ('', 'totp', 'email')),
    pending_secret   TEXT        NOT NULL DEFAULT '',
    pending_code     TEXT        NOT NULL DEFAULT '',
    pending_expires  TIMESTAMPTZ,
    pending_attempts INTEGER     NOT NULL DEFAULT 0,
    -- O código por e-mail para confirmar uma mudança (desativar, códigos
    -- novos) de quem usa o e-mail.
    action_code      TEXT        NOT NULL DEFAULT '',
    action_expires   TIMESTAMPTZ,
    action_attempts  INTEGER     NOT NULL DEFAULT 0,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK ((enabled_at IS NULL) = (method = ''))
);

-- Os códigos de recuperação (uso único), guardados só como hash.
CREATE TABLE two_factor_recovery_codes (
    id        BIGSERIAL PRIMARY KEY,
    user_id   UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    code_hash TEXT        NOT NULL,
    used_at   TIMESTAMPTZ,
    UNIQUE (user_id, code_hash)
);

-- O login que passou pela senha e espera o segundo fator (verify), ou a
-- ativação obrigatória da equipe (setup: primeiro o código do e-mail, depois
-- o app). O token vai para a tela; aqui fica só o hash.
CREATE TABLE two_factor_challenges (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    token_hash     TEXT        NOT NULL UNIQUE,
    user_id        UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind           TEXT        NOT NULL CHECK (kind IN ('verify', 'setup')),
    method         TEXT        NOT NULL DEFAULT '' CHECK (method IN ('', 'totp', 'email')),
    -- O código mandado por e-mail (hash), quando e quantas vezes.
    code_hash      TEXT        NOT NULL DEFAULT '',
    code_sent_at   TIMESTAMPTZ,
    sends          INTEGER     NOT NULL DEFAULT 0,
    email_verified BOOLEAN     NOT NULL DEFAULT FALSE,
    pending_secret TEXT        NOT NULL DEFAULT '',
    attempts       INTEGER     NOT NULL DEFAULT 0,
    expires_at     TIMESTAMPTZ NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_two_factor_challenges_expires ON two_factor_challenges (expires_at);

-- "Confiar neste aparelho por 30 dias": com o token dele, a senha basta.
CREATE TABLE two_factor_devices (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash   TEXT        NOT NULL UNIQUE,
    user_agent   TEXT        NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_used_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at   TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_two_factor_devices_user ON two_factor_devices (user_id);
