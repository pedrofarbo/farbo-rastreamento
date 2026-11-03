-- A entrega em mãos pelo representante: ele recebe kits de instalação da
-- Farbo e entrega o rastreador em mãos a quem contrata pelo link dele, sem
-- frete (no site e nos pedidos pelo app).
ALTER TABLE affiliates ADD COLUMN hand_delivery BOOLEAN NOT NULL DEFAULT FALSE;

-- Quem entrega em mãos (o representante), na entrega sem frete.
ALTER TABLE fulfillments ADD COLUMN delivery_hand_by TEXT NOT NULL DEFAULT '';
