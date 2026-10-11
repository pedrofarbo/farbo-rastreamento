-- Mais destinatários dos alertas da conta: e-mails sem login (o sócio, a
-- central de operações da frota) que recebem os mesmos alertas que o titular
-- escolheu — só por e-mail.
CREATE TABLE alert_extra_recipients (
    customer_id UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    email       TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (customer_id, email)
);
