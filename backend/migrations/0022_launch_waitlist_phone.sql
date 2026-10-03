-- O WhatsApp passa a ser obrigatório na inscrição da lista de lançamento
-- (quem se inscreveu antes fica sem).
ALTER TABLE launch_waitlist ADD COLUMN phone TEXT NOT NULL DEFAULT '';
