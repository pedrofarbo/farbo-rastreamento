-- Os chips avulsos das remessas de kits: o rastreador que vai sem chip (ou
-- um chip a mais) leva o chip separado, e o instalador do representante
-- encaixa na hora de ativar. Um chip está numa remessa só (a cancelada solta
-- os dela); no rastreador, ele sai do estoque do representante sozinho.
CREATE TABLE kit_shipment_chips (
    shipment_id UUID NOT NULL REFERENCES kit_shipments (id) ON DELETE CASCADE,
    chip_id     UUID NOT NULL UNIQUE REFERENCES chips (id) ON DELETE CASCADE,
    PRIMARY KEY (shipment_id, chip_id)
);
