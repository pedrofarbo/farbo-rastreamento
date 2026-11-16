-- A autovistoria por link: a associação manda o link (sem login) para o
-- associado — ou para quem ainda vai entrar, só com o nome, o celular e a
-- placa — ou para um técnico, que tira as fotos no lugar dele. Cada tipo de
-- vistoria (adesão, revistoria, sinistro, troca de veículo) tem o roteiro de
-- fotos dele; as fotos saem da câmera ao vivo, com a hora e o lugar; no
-- envio, as conferências (o lugar e a hora das fotos, a foto repetida, a
-- posição do rastreador). A vistoria enviada entra na fatura da associação,
-- pelo preço dela.

ALTER TABLE inspections
    ALTER COLUMN associate_id DROP NOT NULL,
    ALTER COLUMN vehicle_id DROP NOT NULL,
    -- entry (adesão), periodic (revistoria), claim (sinistro), replacement (troca de veículo).
    ADD COLUMN kind TEXT NOT NULL DEFAULT 'periodic' CHECK (kind IN ('entry', 'periodic', 'claim', 'replacement')),
    -- Quem tira as fotos: o associado (self) ou um técnico (technician).
    ADD COLUMN mode TEXT NOT NULL DEFAULT 'self' CHECK (mode IN ('self', 'technician')),
    -- car ou motorcycle: o roteiro das fotos.
    ADD COLUMN vehicle_type TEXT NOT NULL DEFAULT 'car' CHECK (vehicle_type IN ('car', 'motorcycle')),
    -- Quem ainda não é associado (sem conta nem veículo): o nome, o celular,
    -- a placa e a descrição do veículo.
    ADD COLUMN person_name  TEXT NOT NULL DEFAULT '',
    ADD COLUMN person_phone TEXT NOT NULL DEFAULT '',
    ADD COLUMN plate        TEXT NOT NULL DEFAULT '',
    ADD COLUMN vehicle_desc TEXT NOT NULL DEFAULT '',
    -- Até quando o link abre (vazio: as do app, sem prazo).
    ADD COLUMN expires_at TIMESTAMPTZ,
    -- A refação aponta para a vistoria original (a primeira da série): o
    -- mesmo link abre a mais nova, e só a original é cobrada.
    ADD COLUMN redo_of UUID REFERENCES inspections (id) ON DELETE CASCADE,
    -- O preço na fatura da associação (o do pedido) e se já entrou nela.
    ADD COLUMN price_cents INTEGER NOT NULL DEFAULT 0 CHECK (price_cents BETWEEN 0 AND 10000000),
    ADD COLUMN billed BOOLEAN NOT NULL DEFAULT FALSE,
    -- No envio: as avarias que já existem (o que a pessoa declarou), o aceite
    -- da declaração e as conferências.
    ADD COLUMN damages_note TEXT NOT NULL DEFAULT '',
    ADD COLUMN accepted_at  TIMESTAMPTZ,
    ADD COLUMN checks       JSONB NOT NULL DEFAULT '[]';

ALTER TABLE inspections DROP CONSTRAINT inspections_status_check;
ALTER TABLE inspections ADD CONSTRAINT inspections_status_check
    CHECK (status IN ('pending', 'submitted', 'approved', 'rejected', 'canceled'));

-- A vistoria é de um veículo de associado ou de uma placa.
ALTER TABLE inspections ADD CONSTRAINT inspections_subject_check
    CHECK ((vehicle_id IS NOT NULL AND associate_id IS NOT NULL) OR (vehicle_id IS NULL AND plate <> ''));

-- Uma vistoria em aberto por placa sem veículo (a por veículo já existe).
CREATE UNIQUE INDEX idx_inspections_open_plate ON inspections (organization_id, plate)
    WHERE vehicle_id IS NULL AND status IN ('pending', 'submitted');
CREATE INDEX idx_inspections_redo ON inspections (redo_of) WHERE redo_of IS NOT NULL;
-- O que falta entrar na fatura da associação.
CREATE INDEX idx_inspections_unbilled ON inspections (organization_id) WHERE NOT billed AND price_cents > 0;

-- Os ângulos de cada roteiro (o serviço confere); a foto ganha a precisão da
-- posição, de onde veio (a câmera ao vivo ou o seletor de arquivos, que
-- pode trazer uma foto antiga) e o hash do arquivo (a foto repetida).
ALTER TABLE inspection_photos DROP CONSTRAINT inspection_photos_slot_check;
ALTER TABLE inspection_photos
    ADD COLUMN accuracy_m DOUBLE PRECISION,
    ADD COLUMN source     TEXT NOT NULL DEFAULT 'file' CHECK (source IN ('camera', 'file')),
    ADD COLUMN sha256     BYTEA;
CREATE INDEX idx_inspection_photos_sha256 ON inspection_photos (sha256) WHERE sha256 IS NOT NULL;

-- Os preços da vistoria para a associação (zero: sem cobrança).
ALTER TABLE organizations
    ADD COLUMN inspection_cents      INTEGER NOT NULL DEFAULT 0 CHECK (inspection_cents BETWEEN 0 AND 10000000),
    ADD COLUMN inspection_tech_cents INTEGER NOT NULL DEFAULT 0 CHECK (inspection_tech_cents BETWEEN 0 AND 10000000);
