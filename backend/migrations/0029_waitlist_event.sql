-- De qual evento veio a inscrição na lista de lançamento: a tela de cadastro
-- aberta pelo QR Code (/evento/<nome>) manda o nome do evento. Vazio: veio
-- pela landing. Vale o primeiro evento que trouxe a pessoa.
ALTER TABLE launch_waitlist ADD COLUMN event TEXT NOT NULL DEFAULT '';
