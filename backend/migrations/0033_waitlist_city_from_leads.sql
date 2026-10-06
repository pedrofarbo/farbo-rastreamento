-- Quem entrou na lista de lançamento pelo pré-cadastro da landing (a opção
-- "entrar também na lista") ficou sem a cidade que informou: ela não era
-- repassada. Vem do pré-cadastro mais recente com o mesmo e-mail.
UPDATE launch_waitlist w
SET city = l.city, updated_at = NOW()
FROM (
    SELECT DISTINCT ON (lower(email)) lower(email) AS email, city
    FROM leads
    WHERE city <> ''
    ORDER BY lower(email), created_at DESC
) l
WHERE w.city = '' AND lower(w.email) = l.email;
