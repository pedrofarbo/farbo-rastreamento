-- A equipe por grupo: quem é da equipe de uma conta pode ficar restrito a
-- alguns grupos da frota (o supervisor de uma região, por exemplo) e então
-- só vê, opera e recebe alertas dos veículos desses grupos.
--
-- restricted marca a restrição. Se os grupos dela forem apagados, a pessoa
-- fica sem veículo nenhum: nunca volta a ver a frota inteira sozinha.
ALTER TABLE account_members ADD COLUMN restricted BOOLEAN NOT NULL DEFAULT FALSE;

CREATE TABLE account_member_groups (
    user_id  UUID NOT NULL REFERENCES account_members (user_id) ON DELETE CASCADE,
    group_id UUID NOT NULL REFERENCES vehicle_groups (id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, group_id)
);
CREATE INDEX idx_account_member_groups_group ON account_member_groups (group_id);
