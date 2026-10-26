-- O estoque de chips M2M, cada um pelo ICCID impresso nele. O chip em uso é
-- o que está num rastreador (devices.iccid); escolhido no cadastro do
-- rastreador, a linha, o APN e a senha dele vão para o rastreador.
CREATE TABLE chips (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    iccid        VARCHAR(22) NOT NULL UNIQUE,
    phone_number TEXT        NOT NULL DEFAULT '',
    carrier      TEXT        NOT NULL DEFAULT '',
    apn          TEXT        NOT NULL DEFAULT '',
    apn_user     TEXT        NOT NULL DEFAULT '',
    apn_password TEXT        NOT NULL DEFAULT '',
    notes        TEXT        NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Os chips que já estão nos rastreadores entram no estoque (a operadora,
-- pelo APN).
INSERT INTO chips (iccid, phone_number, carrier, apn, apn_user, apn_password, created_at)
SELECT iccid, COALESCE(phone_number, ''),
       CASE WHEN apn ILIKE '%algar%' THEN 'Algar' WHEN apn ILIKE '%vivo%' THEN 'Vivo'
            WHEN apn ILIKE '%claro%' THEN 'Claro' WHEN apn ILIKE '%tim%' THEN 'TIM' ELSE '' END,
       COALESCE(apn, ''), COALESCE(apn_user, ''), COALESCE(apn_password, ''), created_at
FROM devices WHERE iccid IS NOT NULL
ON CONFLICT (iccid) DO NOTHING;
