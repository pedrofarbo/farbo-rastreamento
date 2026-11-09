-- A ativação do rastreador pelo link do instalador, a senha do link e o
-- representante que agendou.
--
-- A ativação: o rastreador livre (sem
-- veículo) que nunca conectou ganha o chip escolhido e a configuração por SMS
-- antes de ser instalado. O link guarda a ativação — o rastreador reservado
-- (no agendamento, o instalador escolhe; na avulsa, é o do link) e a sessão
-- de SMS — e quantas já fez: cada uma gasta SMS, e há um teto por link.
ALTER TABLE installation_appointments
    ADD COLUMN activation_device_id  UUID REFERENCES devices (id) ON DELETE SET NULL,
    ADD COLUMN activation_session_id UUID REFERENCES sms_setup_sessions (id) ON DELETE SET NULL,
    ADD COLUMN activations           INTEGER NOT NULL DEFAULT 0 CHECK (activations >= 0);
CREATE INDEX idx_installation_appointments_activation ON installation_appointments (activation_device_id)
    WHERE activation_device_id IS NOT NULL;

ALTER TABLE standalone_installs
    ADD COLUMN activation_session_id UUID REFERENCES sms_setup_sessions (id) ON DELETE SET NULL,
    ADD COLUMN activations           INTEGER NOT NULL DEFAULT 0 CHECK (activations >= 0);

-- O agendamento feito pelo representante, pela página dele: o estoque do
-- link é o kit dele (e não o da central) e o link abre sem senha. Nulo: a
-- central agendou.
ALTER TABLE installation_appointments ADD COLUMN affiliate_id UUID REFERENCES affiliates (id) ON DELETE SET NULL;

-- A senha do link do instalador que não é representante: a central passa
-- junto com o link, e a tela só abre com ela. Errada demais (pin_failures), o
-- link trava até a central gerar outra. Nula: o link abre sem senha (o do
-- representante e os de antes dela).
ALTER TABLE installation_appointments
    ADD COLUMN access_pin   TEXT CHECK (access_pin ~ '^[0-9]{6}$'),
    ADD COLUMN pin_failures INTEGER NOT NULL DEFAULT 0 CHECK (pin_failures >= 0);
ALTER TABLE standalone_installs
    ADD COLUMN access_pin   TEXT CHECK (access_pin ~ '^[0-9]{6}$'),
    ADD COLUMN pin_failures INTEGER NOT NULL DEFAULT 0 CHECK (pin_failures >= 0);
