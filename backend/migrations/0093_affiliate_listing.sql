-- Os representantes oficiais autorizados na landing: a central marca no
-- afiliado; aparecem o nome, a cidade, a região que atende e o WhatsApp
-- público (só o que a central preencheu aqui, não o telefone do cadastro).
ALTER TABLE affiliates
    ADD COLUMN listed BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN listing_city TEXT NOT NULL DEFAULT '',
    ADD COLUMN listing_area TEXT NOT NULL DEFAULT '',
    ADD COLUMN listing_whatsapp TEXT NOT NULL DEFAULT '';
