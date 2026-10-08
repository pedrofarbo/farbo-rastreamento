-- O ICCID do chip (o número de série impresso nele, 19 ou 20 dígitos
-- começando com 89), ao lado da linha: é por ele que o chip aparece no
-- portal da operadora (Meu IoT, da Algar). Um chip, um rastreador.
ALTER TABLE devices ADD COLUMN iccid VARCHAR(22);

CREATE UNIQUE INDEX devices_iccid_key ON devices (iccid) WHERE iccid IS NOT NULL;
