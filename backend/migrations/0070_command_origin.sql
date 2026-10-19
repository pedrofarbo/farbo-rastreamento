-- O rastreio de bloqueio e desbloqueio no app do cliente: de qual aparelho
-- saiu o comando (o navegador da sessão) e como quem pediu confirmou
-- (biometric ou password).
ALTER TABLE device_commands
    ADD COLUMN user_agent     TEXT NOT NULL DEFAULT '',
    ADD COLUMN confirmed_with TEXT NOT NULL DEFAULT '';

-- Os bloqueios e desbloqueios de antes ganham o que dá para deduzir: a
-- confirmação de quem pediu logo antes do comando (auditoria)...
UPDATE device_commands c SET confirmed_with = COALESCE((
    SELECT a.metadata->>'method' FROM audit_logs a
    WHERE a.user_id = c.requested_by AND a.action = 'AUTH_STEP_UP' AND a.result = 'OK'
        AND a.metadata->>'purpose' = CASE c.command WHEN 'ENGINE_CUT' THEN 'engine_cut' ELSE 'engine_resume' END
        AND a.created_at BETWEEN c.requested_at - interval '3 minutes' AND c.requested_at + interval '5 seconds'
    ORDER BY a.created_at DESC LIMIT 1), '')
WHERE c.command IN ('ENGINE_CUT', 'ENGINE_RESUME') AND c.requested_by IS NOT NULL;

-- ...e o aparelho: o token de acesso dura 15 minutos e só nasce no login ou
-- na renovação, então a sessão que pediu é uma das abertas ou renovadas nos
-- 15 minutos antes — se foi uma só, é ela.
UPDATE device_commands c SET user_agent = COALESCE((
    SELECT min(t.user_agent) FROM refresh_tokens t
    WHERE t.user_id = c.requested_by
        AND t.created_at > c.requested_at - interval '15 minutes' AND t.created_at <= c.requested_at
    HAVING count(*) = 1), '')
WHERE c.command IN ('ENGINE_CUT', 'ENGINE_RESUME') AND c.requested_by IS NOT NULL;
