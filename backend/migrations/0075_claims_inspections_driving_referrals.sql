-- As ocorrências: o associado de uma associação ou seguradora abre pelo app
-- (batida, roubo, furto, vidro...), com as fotos e o boletim de ocorrência; a
-- equipe da organização acompanha e muda a situação, e cada mudança fica no
-- histórico (e avisa o associado).
CREATE TABLE claims (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID        NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    associate_id    UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    vehicle_id      UUID        REFERENCES vehicles (id) ON DELETE SET NULL,
    kind            TEXT        NOT NULL CHECK (kind IN ('collision', 'theft', 'robbery', 'glass', 'natural', 'other')),
    occurred_at     TIMESTAMPTZ NOT NULL,
    place           TEXT        NOT NULL DEFAULT '',
    description     TEXT        NOT NULL,
    -- O número do boletim de ocorrência (o arquivo vai em claim_files).
    police_report   TEXT        NOT NULL DEFAULT '',
    status          TEXT        NOT NULL DEFAULT 'open'
                    CHECK (status IN ('open', 'analyzing', 'approved', 'denied', 'closed')),
    created_by      UUID        REFERENCES users (id) ON DELETE SET NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_claims_org ON claims (organization_id, created_at DESC);
CREATE INDEX idx_claims_associate ON claims (associate_id, created_at DESC);

-- O histórico: a abertura, as notas do associado e as mudanças da equipe.
CREATE TABLE claim_updates (
    id          BIGSERIAL PRIMARY KEY,
    claim_id    UUID        NOT NULL REFERENCES claims (id) ON DELETE CASCADE,
    author_id   UUID        REFERENCES users (id) ON DELETE SET NULL,
    -- associate ou organization (quem escreveu, para a tela).
    author_kind TEXT        NOT NULL CHECK (author_kind IN ('associate', 'organization')),
    -- A situação nova (vazia: só uma nota).
    status      TEXT        NOT NULL DEFAULT '',
    note        TEXT        NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_claim_updates_claim ON claim_updates (claim_id, created_at);

-- As fotos e os documentos (o B.O. em PDF): no banco, como as notas fiscais.
CREATE TABLE claim_files (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    claim_id     UUID        NOT NULL REFERENCES claims (id) ON DELETE CASCADE,
    kind         TEXT        NOT NULL CHECK (kind IN ('photo', 'police_report', 'document')),
    filename     TEXT        NOT NULL DEFAULT '',
    content_type TEXT        NOT NULL CHECK (content_type IN ('image/jpeg', 'image/png', 'application/pdf')),
    size_bytes   INTEGER     NOT NULL,
    data         BYTEA       NOT NULL,
    uploaded_by  UUID        REFERENCES users (id) ON DELETE SET NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_claim_files_claim ON claim_files (claim_id, created_at);

-- A vistoria digital: a organização pede, o associado tira as fotos de cada
-- ângulo pelo app e manda; a equipe aprova ou devolve para refazer.
CREATE TABLE inspections (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID        NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    associate_id    UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    vehicle_id      UUID        NOT NULL REFERENCES vehicles (id) ON DELETE CASCADE,
    status          TEXT        NOT NULL DEFAULT 'pending'
                    CHECK (status IN ('pending', 'submitted', 'approved', 'rejected')),
    -- O pedido (o que a equipe quer ver) e a resposta da revisão.
    request_note    TEXT        NOT NULL DEFAULT '',
    review_note     TEXT        NOT NULL DEFAULT '',
    requested_by    UUID        REFERENCES users (id) ON DELETE SET NULL,
    reviewed_by     UUID        REFERENCES users (id) ON DELETE SET NULL,
    submitted_at    TIMESTAMPTZ,
    reviewed_at     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_inspections_org ON inspections (organization_id, created_at DESC);
CREATE INDEX idx_inspections_associate ON inspections (associate_id, created_at DESC);
-- Uma vistoria em aberto por veículo.
CREATE UNIQUE INDEX idx_inspections_open ON inspections (vehicle_id) WHERE status IN ('pending', 'submitted');

-- A foto de cada ângulo (refazer troca a foto).
CREATE TABLE inspection_photos (
    inspection_id UUID        NOT NULL REFERENCES inspections (id) ON DELETE CASCADE,
    slot          TEXT        NOT NULL CHECK (slot IN ('front', 'rear', 'left', 'right', 'dashboard', 'chassis')),
    content_type  TEXT        NOT NULL CHECK (content_type IN ('image/jpeg', 'image/png')),
    size_bytes    INTEGER     NOT NULL,
    data          BYTEA       NOT NULL,
    -- Onde a foto foi tirada (o navegador, se a pessoa deixar).
    latitude      DOUBLE PRECISION,
    longitude     DOUBLE PRECISION,
    taken_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (inspection_id, slot)
);

-- A nota de direção: o resumo do dia ganha os km à noite (22h às 6h), as
-- freadas e arrancadas bruscas e o tempo acima de 120 km/h. Os dias já
-- calculados ficam com zero até serem recalculados.
ALTER TABLE vehicle_daily_stats
    ADD COLUMN night_m      DOUBLE PRECISION NOT NULL DEFAULT 0,
    ADD COLUMN harsh_brakes INTEGER          NOT NULL DEFAULT 0,
    ADD COLUMN harsh_accels INTEGER          NOT NULL DEFAULT 0,
    ADD COLUMN high_speed_s INTEGER          NOT NULL DEFAULT 0;

-- Indique e ganhe: o link de indicação do cliente é um afiliado dele (sem
-- comissão em dinheiro). Quando o indicado paga a primeira mensalidade, quem
-- indicou ganha um crédito, que abate da próxima fatura dele.
ALTER TABLE affiliates ADD COLUMN customer_id UUID UNIQUE REFERENCES users (id) ON DELETE CASCADE;
ALTER TABLE affiliate_settings
    ADD COLUMN customer_reward_cents INTEGER NOT NULL DEFAULT 3000 CHECK (customer_reward_cents BETWEEN 0 AND 100000);

CREATE TABLE referral_credits (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- Quem indicou (recebe o crédito) e quem foi indicado (um crédito por indicado).
    customer_id     UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    referred_id     UUID        NOT NULL UNIQUE REFERENCES users (id) ON DELETE CASCADE,
    amount_cents    INTEGER     NOT NULL CHECK (amount_cents > 0),
    -- O que ainda falta abater.
    remaining_cents INTEGER     NOT NULL CHECK (remaining_cents >= 0),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_referral_credits_open ON referral_credits (customer_id) WHERE remaining_cents > 0;

-- Cada abatimento numa fatura.
CREATE TABLE referral_credit_uses (
    id           BIGSERIAL PRIMARY KEY,
    credit_id    UUID        NOT NULL REFERENCES referral_credits (id) ON DELETE CASCADE,
    invoice_id   UUID        NOT NULL REFERENCES invoices (id) ON DELETE CASCADE,
    amount_cents INTEGER     NOT NULL CHECK (amount_cents > 0),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_referral_credit_uses_invoice ON referral_credit_uses (invoice_id);
