-- A ativação manda por SMS só o que o rastreador precisa para conectar (APN e
-- servidor); o fuso, o intervalo, a voltagem e a consulta vão pela própria
-- conexão, depois que ele conecta. O desfecho de cada um fica aqui:
-- [{step, status, reply, error, at}] (status: confirmed, sms, failed).
ALTER TABLE sms_setup_sessions ADD COLUMN online JSONB NOT NULL DEFAULT '[]'::jsonb;
