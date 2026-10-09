-- O telefone dos usuários num formato só (o de phone.Format): celular
-- "(11) 9-8888-7777", fixo "(11) 3333-4444". O 55 do país sai e o celular
-- antigo, sem o 9 (10 dígitos começando com 6 a 9), ganha o 9. O que não é
-- telefone brasileiro fica como está.
WITH raw AS (
    SELECT id, regexp_replace(phone, '\D', '', 'g') AS d FROM users WHERE phone <> ''
), national AS (
    SELECT id, CASE WHEN length(d) IN (12, 13) AND d LIKE '55%' THEN substr(d, 3) ELSE d END AS d FROM raw
), mobile AS (
    SELECT id, CASE WHEN length(d) = 10 AND substr(d, 3, 1) IN ('6', '7', '8', '9') THEN substr(d, 1, 2) || '9' || substr(d, 3)
        ELSE d END AS d FROM national
)
UPDATE users u SET phone = CASE
        WHEN length(m.d) = 11 AND substr(m.d, 3, 1) = '9'
            THEN '(' || substr(m.d, 1, 2) || ') 9-' || substr(m.d, 4, 4) || '-' || substr(m.d, 8, 4)
        WHEN length(m.d) = 10
            THEN '(' || substr(m.d, 1, 2) || ') ' || substr(m.d, 3, 4) || '-' || substr(m.d, 7, 4)
        ELSE u.phone
    END
FROM mobile m
WHERE m.id = u.id;
