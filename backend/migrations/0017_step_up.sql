-- Confirmação extra antes de ações sensíveis (o cliente desligar o motor):
-- biometria do aparelho (WebAuthn: Face ID, digital) ou a senha da conta.

-- Credenciais de biometria, uma por aparelho cadastrado. A chave privada
-- nunca sai do aparelho; aqui fica só a pública.
CREATE TABLE webauthn_credentials (
    id            BIGSERIAL PRIMARY KEY,
    user_id       UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    credential_id BYTEA NOT NULL UNIQUE,
    -- SubjectPublicKeyInfo (DER) e o algoritmo COSE: -7 (ES256) ou -257 (RS256).
    public_key    BYTEA NOT NULL,
    algorithm     INT NOT NULL,
    -- Contador do autenticador: voltar para trás indica credencial clonada.
    sign_count    BIGINT NOT NULL DEFAULT 0,
    name          TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_used_at  TIMESTAMPTZ
);
CREATE INDEX idx_webauthn_credentials_user ON webauthn_credentials (user_id);

-- Desafios do WebAuthn: aleatórios, de uso único e curtos.
CREATE TABLE step_up_challenges (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    purpose    TEXT NOT NULL,
    challenge  BYTEA NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at    TIMESTAMPTZ
);
CREATE INDEX idx_step_up_challenges_expires ON step_up_challenges (expires_at);

-- Comprovantes de confirmação: valem uma vez, por poucos minutos, para uma
-- ação. Guardamos só o hash do token.
CREATE TABLE step_up_grants (
    token_hash BYTEA PRIMARY KEY,
    user_id    UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    purpose    TEXT NOT NULL,
    -- biometric ou password.
    method     TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at    TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_step_up_grants_expires ON step_up_grants (expires_at);
