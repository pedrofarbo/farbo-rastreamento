-- A garantia de troca do rastreador no comodato (somada à mensalidade da
-- pessoa física) pelo link do afiliado com a instalação dele: nula, a da
-- Farbo; zero, sem garantia. A Farbo cobra na mensalidade, também quando o
-- representante recebe a instalação direto.
ALTER TABLE affiliates
    ADD COLUMN loan_surcharge_cents INTEGER CHECK (loan_surcharge_cents BETWEEN 0 AND 10000000),
    ADD CONSTRAINT affiliates_loan_surcharge_check CHECK (install_mode <> '' OR loan_surcharge_cents IS NULL);
