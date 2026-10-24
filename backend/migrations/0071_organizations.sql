-- Associações e seguradoras. A organização contrata o rastreamento para os
-- associados dela e paga uma fatura por mês (os veículos com rastreador
-- instalado, no preço combinado). Quem é da equipe dela entra num painel
-- próprio, com os veículos dos associados; o associado usa o app da Farbo
-- como qualquer cliente, sem faturas.
--
-- A organização é também um usuário (role 'organization', sem login): o
-- nome, o CNPJ, o e-mail e o telefone de cobrança ficam onde as faturas, o
-- Pix, o boleto e a régua de cobrança já procuram o pagador. Quem é da equipe
-- dela tem login próprio (role 'org_member').
ALTER TABLE users DROP CONSTRAINT users_role_check;
ALTER TABLE users ADD CONSTRAINT users_role_check
    CHECK (role IN ('admin', 'operator', 'viewer', 'customer', 'organization', 'org_member'));

-- O e-mail de cobrança da organização pode ser o mesmo do login de alguém
-- (o contato@ que também entra no painel): ele não é login, e fica fora do
-- índice único.
DROP INDEX idx_users_email;
CREATE UNIQUE INDEX idx_users_email ON users (lower(email)) WHERE role <> 'organization';

CREATE TABLE organizations (
    id          UUID PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    kind        TEXT        NOT NULL CHECK (kind IN ('association', 'insurer')),
    -- O preço por veículo ativo no mês; zero não cobra mensalidade (parceria).
    price_cents INTEGER     NOT NULL CHECK (price_cents >= 0),
    due_day     SMALLINT    NOT NULL CHECK (due_day BETWEEN 1 AND 28),
    -- O rastreador em comodato (loan: emprestado, volta na desinstalação) ou
    -- vendido (sale: equipment_cents por rastreador instalado). As taxas de
    -- instalação e de desinstalação entram na fatura do mês em que
    -- acontecem (zero: sem taxa).
    equipment_mode      TEXT    NOT NULL DEFAULT 'loan' CHECK (equipment_mode IN ('loan', 'sale')),
    equipment_cents     INTEGER NOT NULL DEFAULT 0 CHECK (equipment_cents >= 0),
    install_fee_cents   INTEGER NOT NULL DEFAULT 0 CHECK (install_fee_cents >= 0),
    uninstall_fee_cents INTEGER NOT NULL DEFAULT 0 CHECK (uninstall_fee_cents >= 0),
    notes       TEXT        NOT NULL DEFAULT '',
    created_by  UUID        REFERENCES users (id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- A equipe da organização: Administrador (tudo, inclusive a equipe e as
-- faturas), Operador (associados, veículos, bloqueio) e Visualizador (só
-- acompanha). Cada pessoa é de uma organização só.
CREATE TABLE organization_members (
    user_id         UUID PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    organization_id UUID        NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    role            TEXT        NOT NULL CHECK (role IN ('admin', 'operator', 'viewer')),
    invited_by      UUID        REFERENCES users (id) ON DELETE SET NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_organization_members_org ON organization_members (organization_id);

-- O associado: um cliente cujo rastreamento a organização paga.
ALTER TABLE users ADD COLUMN organization_id UUID REFERENCES organizations (id) ON DELETE SET NULL;
CREATE INDEX idx_users_organization ON users (organization_id) WHERE organization_id IS NOT NULL;

-- Cada instalação de rastreador num veículo de associado, como a cobrança
-- da organização enxerga: o dia em que passou a contar (rastreador no
-- veículo, associado ativo) e o dia em que deixou de contar. A rotina diária
-- mantém a lista; cada fatura marca o que já cobrou (o proporcional, a
-- instalação, o equipamento e a desinstalação), para não cobrar duas vezes.
CREATE TABLE organization_vehicles (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id  UUID        NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    -- Nulo depois que o veículo é apagado (a desinstalação ainda é cobrada).
    vehicle_id       UUID        REFERENCES vehicles (id) ON DELETE SET NULL,
    device_id        UUID,
    vehicle_name     TEXT        NOT NULL,
    plate            TEXT        NOT NULL DEFAULT '',
    owner_name       TEXT        NOT NULL DEFAULT '',
    installed_on     DATE        NOT NULL,
    removed_on       DATE,
    -- Saiu porque o rastreador foi tirado do veículo (cobra a desinstalação),
    -- e não só porque o associado foi desligado.
    uninstalled      BOOLEAN     NOT NULL DEFAULT FALSE,
    prorata_billed   BOOLEAN     NOT NULL DEFAULT FALSE,
    install_billed   BOOLEAN     NOT NULL DEFAULT FALSE,
    equipment_billed BOOLEAN     NOT NULL DEFAULT FALSE,
    uninstall_billed BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_organization_vehicles_org ON organization_vehicles (organization_id, removed_on);
CREATE UNIQUE INDEX idx_organization_vehicles_current ON organization_vehicles (vehicle_id) WHERE removed_on IS NULL;

-- As faturas mensais da organização: uma por vencimento, com o extrato (a
-- mensalidade de cada veículo, os proporcionais, as taxas e o equipamento).
CREATE TABLE organization_invoices (
    organization_id UUID        NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    due_date        DATE        NOT NULL,
    invoice_id      UUID        NOT NULL UNIQUE REFERENCES invoices (id) ON DELETE CASCADE,
    -- Os veículos na mensalidade do mês.
    vehicles        INTEGER     NOT NULL CHECK (vehicles >= 0),
    price_cents     INTEGER     NOT NULL CHECK (price_cents >= 0),
    -- [{"kind", "vehicleId", "name", "plate", "ownerName", "amountCents", "days"}]
    items           JSONB       NOT NULL DEFAULT '[]'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (organization_id, due_date)
);
