-- Gestão da empresa (só administradores): contas a pagar e receitas avulsas,
-- com fornecedores, categorias, parcelas, contas recorrentes e anexos; o
-- estoque de rastreadores e chips com o custo médio; o saldo inicial do
-- caixa e os avisos de vencimento. As faturas dos clientes continuam em
-- invoices e entram no caixa e no resultado quando pagas.

-- Categoria de cada lançamento e a linha dela no resultado do mês (DRE):
--   receitas (INCOME):
--     REVENUE      receita da operação (venda de equipamento, por exemplo)
--     OTHER_INCOME outras receitas (rendimentos)
--     CAPITAL_IN   aportes e empréstimos recebidos: entram no caixa, não no resultado
--   despesas (EXPENSE):
--     TAX          impostos sobre a receita
--     COST         custo do serviço (dados dos chips, servidor, instalação, frete)
--     OPERATING    despesas operacionais (marketing, contador, software, pessoal)
--     FINANCIAL    despesas financeiras (tarifas, taxas de pagamento, juros)
--     INVESTMENT   compras para o estoque: saem do caixa; o equipamento vira
--                  custo quando é instalado (ver stock_movements)
--     CAPITAL_OUT  retiradas dos sócios e empréstimos pagos: saem do caixa, não do resultado
CREATE TABLE finance_categories (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT        NOT NULL,
    kind       TEXT        NOT NULL CHECK (kind IN ('EXPENSE', 'INCOME')),
    dre_group  TEXT        NOT NULL CHECK (dre_group IN ('REVENUE', 'OTHER_INCOME', 'CAPITAL_IN',
                           'TAX', 'COST', 'OPERATING', 'FINANCIAL', 'INVESTMENT', 'CAPITAL_OUT')),
    active     BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK ((kind = 'INCOME') = (dre_group IN ('REVENUE', 'OTHER_INCOME', 'CAPITAL_IN')))
);
CREATE UNIQUE INDEX idx_finance_categories_name ON finance_categories (kind, lower(name));

-- As mais comuns, para começar (dá para renomear, desativar e criar outras).
INSERT INTO finance_categories (name, kind, dre_group) VALUES
    ('Venda de equipamentos', 'INCOME', 'REVENUE'),
    ('Outras receitas', 'INCOME', 'OTHER_INCOME'),
    ('Aporte dos sócios', 'INCOME', 'CAPITAL_IN'),
    ('Empréstimo recebido', 'INCOME', 'CAPITAL_IN'),
    ('Impostos (DAS/Simples)', 'EXPENSE', 'TAX'),
    ('Plano de dados dos chips', 'EXPENSE', 'COST'),
    ('Servidor e plataforma', 'EXPENSE', 'COST'),
    ('Instalação (prestadores)', 'EXPENSE', 'COST'),
    ('Frete e envio', 'EXPENSE', 'COST'),
    ('Marketing e anúncios', 'EXPENSE', 'OPERATING'),
    ('Contabilidade', 'EXPENSE', 'OPERATING'),
    ('Software e assinaturas', 'EXPENSE', 'OPERATING'),
    ('Aluguel e escritório', 'EXPENSE', 'OPERATING'),
    ('Pessoal e pró-labore', 'EXPENSE', 'OPERATING'),
    ('Outras despesas', 'EXPENSE', 'OPERATING'),
    ('Taxas de pagamento', 'EXPENSE', 'FINANCIAL'),
    ('Tarifas bancárias e juros', 'EXPENSE', 'FINANCIAL'),
    ('Compra de rastreadores e chips', 'EXPENSE', 'INVESTMENT'),
    ('Retirada dos sócios', 'EXPENSE', 'CAPITAL_OUT'),
    ('Pagamento de empréstimo', 'EXPENSE', 'CAPITAL_OUT');

CREATE TABLE suppliers (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT        NOT NULL,
    -- CNPJ ou CPF, só dígitos.
    document   TEXT        NOT NULL DEFAULT '',
    email      TEXT        NOT NULL DEFAULT '',
    phone      TEXT        NOT NULL DEFAULT '',
    pix_key    TEXT        NOT NULL DEFAULT '',
    notes      TEXT        NOT NULL DEFAULT '',
    active     BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Contas que se repetem todo mês (aluguel, internet, dados dos chips). O
-- gerador cria os lançamentos que vencem nos próximos dias e avança
-- next_due_date, como o das faturas.
CREATE TABLE finance_recurrences (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    kind          TEXT        NOT NULL CHECK (kind IN ('PAYABLE', 'RECEIVABLE')),
    description   TEXT        NOT NULL,
    category_id   UUID        NOT NULL REFERENCES finance_categories (id),
    supplier_id   UUID        REFERENCES suppliers (id) ON DELETE SET NULL,
    amount_cents  BIGINT      NOT NULL CHECK (amount_cents > 0),
    -- Até 28 para existir em todos os meses.
    due_day       SMALLINT    NOT NULL CHECK (due_day BETWEEN 1 AND 28),
    next_due_date DATE        NOT NULL,
    -- Último vencimento (NULL: sem fim).
    ends_on       DATE,
    active        BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_finance_recurrences_due ON finance_recurrences (next_due_date) WHERE active;

-- Estoque: o saldo e o custo médio de cada item, atualizados a cada
-- movimento (com o item travado), e os movimentos, que não se editam — um
-- erro se corrige com um acerto.
CREATE TABLE stock_items (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name           TEXT        NOT NULL,
    kind           TEXT        NOT NULL CHECK (kind IN ('TRACKER', 'SIM', 'ACCESSORY', 'OTHER')),
    -- Abaixo disso, o painel avisa para comprar.
    min_quantity   INTEGER     NOT NULL DEFAULT 0 CHECK (min_quantity >= 0),
    quantity       INTEGER     NOT NULL DEFAULT 0 CHECK (quantity >= 0),
    avg_cost_cents BIGINT      NOT NULL DEFAULT 0 CHECK (avg_cost_cents >= 0),
    active         BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX idx_stock_items_name ON stock_items (lower(name));

-- IN: compra ou entrada; OUT: saída (instalação, venda); LOSS: perda ou
-- defeito; ADJUST: acerto de contagem. A quantidade tem sinal (entrada
-- positiva) e o custo por unidade é o da compra na entrada e o custo médio
-- do momento nas saídas — é ele que vai para o resultado do mês.
CREATE TABLE stock_movements (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    item_id         UUID        NOT NULL REFERENCES stock_items (id) ON DELETE CASCADE,
    type            TEXT        NOT NULL CHECK (type IN ('IN', 'OUT', 'LOSS', 'ADJUST')),
    quantity        INTEGER     NOT NULL CHECK (quantity <> 0),
    unit_cost_cents BIGINT      NOT NULL CHECK (unit_cost_cents >= 0),
    occurred_on     DATE        NOT NULL,
    supplier_id     UUID        REFERENCES suppliers (id) ON DELETE SET NULL,
    notes           TEXT        NOT NULL DEFAULT '',
    created_by      UUID        REFERENCES users (id) ON DELETE SET NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK ((type = 'IN' AND quantity > 0) OR (type IN ('OUT', 'LOSS') AND quantity < 0) OR type = 'ADJUST')
);
CREATE INDEX idx_stock_movements_item ON stock_movements (item_id, occurred_on DESC, created_at DESC);
CREATE INDEX idx_stock_movements_day ON stock_movements (occurred_on);

-- Contas a pagar (PAYABLE) e receitas avulsas (RECEIVABLE). "Vencida" não é
-- um status gravado: é OPEN com o vencimento no passado.
CREATE TABLE finance_entries (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    kind              TEXT        NOT NULL CHECK (kind IN ('PAYABLE', 'RECEIVABLE')),
    description       TEXT        NOT NULL,
    category_id       UUID        NOT NULL REFERENCES finance_categories (id),
    supplier_id       UUID        REFERENCES suppliers (id) ON DELETE SET NULL,
    amount_cents      BIGINT      NOT NULL CHECK (amount_cents > 0),
    due_date          DATE        NOT NULL,
    status            TEXT        NOT NULL DEFAULT 'OPEN' CHECK (status IN ('OPEN', 'PAID', 'CANCELED')),
    -- O pagamento (ou recebimento): a data, o valor de fato (com juros ou
    -- desconto) e a forma.
    paid_on           DATE,
    paid_cents        BIGINT      CHECK (paid_cents > 0),
    payment_method    TEXT        NOT NULL DEFAULT '' CHECK (payment_method IN
                      ('', 'PIX', 'BOLETO', 'CARD', 'TRANSFER', 'CASH', 'DEBIT')),
    -- Linha digitável do boleto ou Pix copia-e-cola, para pagar.
    payment_code      TEXT        NOT NULL DEFAULT '',
    notes             TEXT        NOT NULL DEFAULT '',
    recurrence_id     UUID        REFERENCES finance_recurrences (id) ON DELETE SET NULL,
    -- Parcela "2 de 10" (sempre os dois, ou nenhum).
    installment       SMALLINT,
    installments      SMALLINT,
    -- A compra do estoque que gerou a conta.
    stock_movement_id UUID        REFERENCES stock_movements (id) ON DELETE SET NULL,
    created_by        UUID        REFERENCES users (id) ON DELETE SET NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- Uma conta por vencimento de cada recorrência: o gerador pode rodar de
    -- novo, ou em várias instâncias, sem duplicar.
    UNIQUE (recurrence_id, due_date),
    CHECK ((status = 'PAID') = (paid_on IS NOT NULL AND paid_cents IS NOT NULL)),
    CHECK ((installment IS NULL) = (installments IS NULL)),
    CHECK (installment IS NULL OR (installment BETWEEN 1 AND installments))
);
CREATE INDEX idx_finance_entries_due ON finance_entries (kind, status, due_date);
CREATE INDEX idx_finance_entries_paid ON finance_entries (paid_on) WHERE status = 'PAID';

-- Boleto, nota fiscal ou comprovante. Ficam no banco: entram nos backups e
-- na cópia fora da VPS junto com o resto.
CREATE TABLE finance_attachments (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    entry_id     UUID        NOT NULL REFERENCES finance_entries (id) ON DELETE CASCADE,
    filename     TEXT        NOT NULL,
    content_type TEXT        NOT NULL CHECK (content_type IN ('application/pdf', 'image/png', 'image/jpeg')),
    size_bytes   INTEGER     NOT NULL CHECK (size_bytes > 0),
    data         BYTEA       NOT NULL,
    created_by   UUID        REFERENCES users (id) ON DELETE SET NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_finance_attachments_entry ON finance_attachments (entry_id);

-- O ponto de partida do caixa: o saldo da empresa numa data. Uma linha só.
CREATE TABLE finance_settings (
    id                    BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (id),
    opening_balance_cents BIGINT      NOT NULL DEFAULT 0,
    opening_date          DATE        NOT NULL DEFAULT CURRENT_DATE,
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
INSERT INTO finance_settings DEFAULT VALUES;

-- O resumo diário de vencimentos para os administradores: um por dia, mesmo
-- com várias instâncias.
CREATE TABLE finance_reminders (
    day     DATE PRIMARY KEY,
    sent_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    bills   INTEGER     NOT NULL DEFAULT 0
);
