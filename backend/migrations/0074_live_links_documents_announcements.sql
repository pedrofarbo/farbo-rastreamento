-- Os alertas novos (bateria do carro fraca e impacto) entram ligados para
-- quem já tinha personalizado os alertas; quem nunca abriu a tela recebe os
-- padrões do backend.
UPDATE alert_settings SET kinds = kinds || ARRAY['CAR_BATTERY']::TEXT[], updated_at = NOW()
WHERE NOT ('CAR_BATTERY' = ANY (kinds));
UPDATE alert_settings SET kinds = kinds || ARRAY['IMPACT']::TEXT[], updated_at = NOW()
WHERE NOT ('IMPACT' = ANY (kinds));

-- A localização ao vivo por um tempo: o link público (sem login) que o
-- cliente manda para alguém acompanhar o veículo por algumas horas. O link é
-- o id assinado; nada secreto fica aqui.
CREATE TABLE live_links (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    vehicle_id     UUID        NOT NULL REFERENCES vehicles (id) ON DELETE CASCADE,
    created_by     UUID        REFERENCES users (id) ON DELETE SET NULL,
    -- Para quem é ("Mãe", "Entregador"), só para o cliente se achar.
    label          TEXT        NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at     TIMESTAMPTZ NOT NULL,
    revoked_at     TIMESTAMPTZ,
    views          INTEGER     NOT NULL DEFAULT 0,
    last_viewed_at TIMESTAMPTZ,
    CHECK (expires_at > created_at)
);
CREATE INDEX idx_live_links_vehicle ON live_links (vehicle_id, expires_at DESC);

-- Os documentos do veículo (IPVA, licenciamento, seguro, CNH...) entram na
-- manutenção por data: vencem a cada tantos meses, contando do vencimento
-- anterior, e avisam antes como as manutenções.
ALTER TABLE maintenance_items
    ADD COLUMN kind TEXT NOT NULL DEFAULT 'service' CHECK (kind IN ('service', 'document'));

-- Os comunicados de uma associação ou seguradora aos associados: vão para a
-- área de notificações do app (e o celular) e, se pedido, por e-mail.
CREATE TABLE org_announcements (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID        NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    title           TEXT        NOT NULL,
    body            TEXT        NOT NULL,
    email           BOOLEAN     NOT NULL DEFAULT FALSE,
    -- Quantos associados (ativos) iam receber e quantos receberam.
    recipients      INTEGER     NOT NULL DEFAULT 0,
    delivered       INTEGER     NOT NULL DEFAULT 0,
    emailed         INTEGER     NOT NULL DEFAULT 0,
    finished_at     TIMESTAMPTZ,
    created_by      UUID        REFERENCES users (id) ON DELETE SET NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_org_announcements_org ON org_announcements (organization_id, created_at DESC);
