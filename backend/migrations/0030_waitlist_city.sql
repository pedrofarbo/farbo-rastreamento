-- A cidade da instalação, pedida na tela de cadastro dos eventos (QR Code):
-- onde a pessoa quer instalar o rastreador. Vazio: não informou (a landing
-- não pede).
ALTER TABLE launch_waitlist ADD COLUMN city TEXT NOT NULL DEFAULT '';
