-- A saúde dos rastreadores no painel da associação: os veículos sem sinal,
-- o corte de energia (e o corte seguido de silêncio, quando o rastreador
-- pode ter sido desligado ou retirado). O associado é avisado no app — pela
-- equipe, na hora, ou sozinho, uma vez por dia, no máximo a cada 3 dias por
-- veículo; cada aviso fica aqui.
CREATE TABLE tracker_health_notices (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID        NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    vehicle_id      UUID        NOT NULL REFERENCES vehicles (id) ON DELETE CASCADE,
    -- silent: sem sinal; removed: o corte de energia seguido de silêncio.
    kind            TEXT        NOT NULL CHECK (kind IN ('silent', 'removed')),
    -- Sozinho (a rotina diária) ou pela equipe (sent_by).
    automatic       BOOLEAN     NOT NULL,
    sent_by         UUID        REFERENCES users (id) ON DELETE SET NULL,
    -- Em quantos aparelhos o aviso chegou (zero: o associado sem o app ou sem as notificações).
    delivered       INTEGER     NOT NULL DEFAULT 0,
    notified_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_tracker_health_notices_vehicle ON tracker_health_notices (vehicle_id, notified_at DESC);

-- O aviso sozinho ao associado (a associação pode desligar).
ALTER TABLE organizations ADD COLUMN health_auto_notice BOOLEAN NOT NULL DEFAULT TRUE;
