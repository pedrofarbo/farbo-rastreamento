-- Histórico mais longo: além de 7, 14 e 30 dias, 90, 180 e 365. Os últimos
-- 30 dias ficam com todos os pontos; depois, o trajeto é compactado (ver o
-- pacote retention) e os eventos ficam todos até o fim do prazo.
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_history_retention_days_check;
ALTER TABLE users ADD CONSTRAINT users_history_retention_days_check
    CHECK (history_retention_days IN (7, 14, 30, 90, 180, 365));
ALTER TABLE vehicles DROP CONSTRAINT IF EXISTS vehicles_history_retention_days_check;
ALTER TABLE vehicles ADD CONSTRAINT vehicles_history_retention_days_check
    CHECK (history_retention_days IN (7, 14, 30, 90, 180, 365));
