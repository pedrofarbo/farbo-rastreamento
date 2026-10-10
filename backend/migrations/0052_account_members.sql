-- A conta empresa: o cliente (o titular) dá às pessoas da equipe, cada uma
-- com o próprio login, o acesso aos veículos da conta. Gestor opera tudo
-- (comandos, cercas, faturas), menos a própria conta (equipe, cadastro,
-- contrato, pedidos); Visualizador só acompanha. Cada pessoa é de uma conta
-- só, e quem é da equipe não tem veículos próprios.
CREATE TABLE account_members (
    user_id    UUID PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    account_id UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role       TEXT        NOT NULL CHECK (role IN ('manager', 'viewer')),
    invited_by UUID        REFERENCES users (id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT account_members_not_self CHECK (user_id <> account_id)
);
CREATE INDEX idx_account_members_account ON account_members (account_id);
