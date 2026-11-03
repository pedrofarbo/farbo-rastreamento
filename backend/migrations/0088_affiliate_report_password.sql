-- A senha da página do afiliado (/parceiro/<token>), que a central define no
-- cadastro: com ela, o link secreto só mostra os números depois da senha.
-- Vazio: sem senha (só o link).
ALTER TABLE affiliates ADD COLUMN report_password_hash TEXT NOT NULL DEFAULT '';
