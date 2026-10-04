-- Visitas da landing page, sem cookies e sem dados pessoais. Cada visitante
-- vira um código (hash do IP e do navegador com um sal que muda todo dia): o
-- IP não é gravado e o sal do dia anterior é apagado, então ninguém refaz o
-- código nem segue a pessoa de um dia para o outro. Por isso "visitantes" é
-- a soma dos visitantes únicos de cada dia.
CREATE TABLE analytics_events (
    id            BIGSERIAL PRIMARY KEY,
    occurred_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- O dia (no fuso de Brasília) do sal com que o visitante foi calculado.
    day           DATE NOT NULL,
    visitor       TEXT NOT NULL,
    -- pageview, section_view, cta_click, lead_open, lead_submit,
    -- waitlist_submit, installers_open, outbound_click
    name          TEXT NOT NULL,
    -- A seção vista, o botão clicado, o plano escolhido...
    label         TEXT NOT NULL DEFAULT '',
    path          TEXT NOT NULL DEFAULT '',
    -- De onde veio (só o domínio de fora) e a campanha (UTM), na visita.
    referrer_host TEXT NOT NULL DEFAULT '',
    utm_source    TEXT NOT NULL DEFAULT '',
    utm_medium    TEXT NOT NULL DEFAULT '',
    utm_campaign  TEXT NOT NULL DEFAULT '',
    device        TEXT NOT NULL DEFAULT '' CHECK (device IN ('', 'mobile', 'tablet', 'desktop')),
    browser       TEXT NOT NULL DEFAULT '',
    os            TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_analytics_events_day ON analytics_events (day, name);
CREATE INDEX idx_analytics_events_recent ON analytics_events (occurred_at DESC);

-- O sal de cada dia. Só o de hoje fica guardado.
CREATE TABLE analytics_salts (
    day  DATE PRIMARY KEY,
    salt BYTEA NOT NULL
);
