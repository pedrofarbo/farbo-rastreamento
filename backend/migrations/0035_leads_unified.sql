-- Pré-clientes e lista de lançamento numa lista só: todo mundo que se
-- inscreve vira pré-cliente (com a situação, as anotações e o "cadastrar
-- como cliente"); estar na lista de lançamento — o direito à promoção — é
-- uma marca dele. A launch_waitlist continua sendo o registro da lista.

-- O evento (QR Code) em que a pessoa se inscreveu.
ALTER TABLE leads ADD COLUMN event TEXT NOT NULL DEFAULT '';

-- Quem está só na lista vira pré-cliente, com a origem: lancamento (a caixa
-- da landing), evento ou indicacao. Quem já é cliente entra como convertido.
INSERT INTO leads (name, email, phone, city, event, affiliate_id, source, status, customer_id, consent_at, created_at, updated_at)
SELECT w.name, w.email, w.phone, w.city, w.event, w.affiliate_id,
       CASE WHEN w.affiliate_id IS NOT NULL THEN 'indicacao' WHEN w.event <> '' THEN 'evento' ELSE 'lancamento' END,
       CASE WHEN u.id IS NOT NULL THEN 'CONVERTED' ELSE 'NEW' END,
       u.id, w.consent_at, w.created_at, w.updated_at
FROM launch_waitlist w
LEFT JOIN LATERAL (
    SELECT id FROM users WHERE lower(email) = lower(w.email) AND role = 'customer' LIMIT 1
) u ON TRUE
WHERE NOT EXISTS (SELECT 1 FROM leads l WHERE lower(l.email) = lower(w.email));

-- O evento dos pré-cadastros que também se inscreveram por um.
UPDATE leads l SET event = w.event
FROM launch_waitlist w
WHERE lower(w.email) = lower(l.email) AND l.event = '' AND w.event <> '';
