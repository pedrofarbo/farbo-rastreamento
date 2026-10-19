-- Desconectar na hora. O token de acesso dura 15 minutos e não passa pelo
-- banco; o emitido antes de sessions_valid_after é recusado na próxima
-- requisição. Sobe quando a pessoa sai dos outros aparelhos (o dela ganha um
-- token novo), quando a central desconecta todas as sessões ou troca a
-- senha, e na senha nova pelo e-mail.
ALTER TABLE users ADD COLUMN sessions_valid_after TIMESTAMPTZ;
