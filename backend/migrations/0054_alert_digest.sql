-- Os alertas comuns que passam do teto por hora de um destinatário (numa
-- frota, dezenas) não se perdem: a cada hora saem juntos num e-mail de
-- resumo. digested_at marca os que já entraram num resumo (cada um entra num
-- só, mesmo com mais de uma instância).
ALTER TABLE alert_notifications ADD COLUMN digested_at TIMESTAMPTZ;
CREATE INDEX idx_alert_notifications_digest ON alert_notifications (created_at)
    WHERE status = 'SUPPRESSED' AND reason = 'hourly_cap' AND digested_at IS NULL;
