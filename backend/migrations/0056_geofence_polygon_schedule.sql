-- Cercas em polígono (a área desenhada no mapa: um pátio, um bairro, uma
-- rota) e com horário de aviso.
ALTER TABLE geofences ADD COLUMN shape TEXT NOT NULL DEFAULT 'circle' CHECK (shape IN ('circle', 'polygon'));
-- Os vértices do polígono, [[lat, lon], ...] (só em polygon). latitude,
-- longitude e radius_meters guardam o centro e o raio que cobre a área: a
-- conta rápida de "longe demais" e o enquadramento no mapa.
ALTER TABLE geofences ADD COLUMN polygon JSONB;
-- O horário dos avisos: os dias da semana (0 = domingo) e das/até (minutos do
-- dia; até antes de das atravessa a meia-noite). Vazio/nulo: sempre. A
-- entrada e a saída ficam no histórico a qualquer hora: o horário só decide
-- se avisa.
ALTER TABLE geofences ADD COLUMN schedule_days SMALLINT[] NOT NULL DEFAULT '{}';
ALTER TABLE geofences ADD COLUMN schedule_start SMALLINT CHECK (schedule_start BETWEEN 0 AND 1439);
ALTER TABLE geofences ADD COLUMN schedule_end SMALLINT CHECK (schedule_end BETWEEN 0 AND 1439);
