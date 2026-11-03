-- O rastreador pelo link do afiliado: comprado e comodato (vazio), só
-- comprado (sale) ou só comodato (loan) — no site e nos pedidos pelo app de
-- quem ele indica.
--
-- E a gestão da instalação pelo representante: ele agenda, remarca e conclui
-- (pelo link do instalador) a instalação e a desinstalação dos clientes que
-- indicou, pela página dele — que então mostra os dados desses clientes e
-- por isso pede senha. A central continua vendo tudo na agenda.
ALTER TABLE affiliates
    ADD COLUMN equipment_modes TEXT    NOT NULL DEFAULT '' CHECK (equipment_modes IN ('', 'sale', 'loan')),
    ADD COLUMN manage_installs BOOLEAN NOT NULL DEFAULT FALSE;
