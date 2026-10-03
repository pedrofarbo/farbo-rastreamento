-- Lista de lançamento: quem quer ser avisado quando a Farbo lançar. Um
-- e-mail aparece uma vez só; o aviso do lançamento sai a partir daqui.
CREATE TABLE launch_waitlist (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT NOT NULL DEFAULT '',
    email      TEXT NOT NULL,
    -- Quando aceitou receber o aviso (LGPD).
    consent_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX idx_launch_waitlist_email ON launch_waitlist (lower(email));
