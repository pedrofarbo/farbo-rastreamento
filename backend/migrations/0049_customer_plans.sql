-- O plano do cliente, definido pela central (ex.: o Especial Insanos MC)
-- antes de ele cadastrar os veículos: vale para todo veículo novo que ele
-- contratar, pelo app ou pela central. Sem ele, o veículo novo segue a
-- assinatura ativa do cliente ou o plano padrão do catálogo.
CREATE TABLE customer_plans (
    customer_id UUID PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    plan_name   TEXT        NOT NULL,
    price_cents INTEGER     NOT NULL CHECK (price_cents >= 0),
    due_day     SMALLINT    NOT NULL CHECK (due_day BETWEEN 1 AND 28),
    updated_by  UUID        REFERENCES users (id) ON DELETE SET NULL,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
