-- A origem de cada visita (Instagram, Google, direto...), para ver o funil
-- por origem em Clientes → Visitas do site. Só a visita (pageview) tem; os
-- outros eventos herdam a do visitante no dia.
ALTER TABLE analytics_events ADD COLUMN channel TEXT NOT NULL DEFAULT '';

-- As visitas já gravadas, com as mesmas regras de analytics.Channel: a
-- campanha (utm_source) primeiro, depois o navegador de dentro do app,
-- depois o domínio de onde veio; sem nada, direto.
UPDATE analytics_events SET channel = CASE
    WHEN utm_source <> '' THEN CASE
        WHEN utm_source LIKE 'instagram%' OR utm_source IN ('ig', 'insta') THEN 'instagram'
        WHEN utm_source LIKE 'facebook%' OR utm_source IN ('fb', 'meta') THEN 'facebook'
        WHEN utm_source LIKE 'whatsapp%' OR utm_source IN ('wa', 'zap') THEN 'whatsapp'
        WHEN utm_source LIKE 'tiktok%' THEN 'tiktok'
        WHEN utm_source LIKE 'youtube%' OR utm_source = 'yt' THEN 'youtube'
        WHEN utm_source LIKE 'google%' THEN 'google'
        WHEN utm_source LIKE 'bing%' THEN 'busca'
        ELSE utm_source
    END
    WHEN browser = 'Instagram' THEN 'instagram'
    WHEN browser = 'Facebook' THEN 'facebook'
    WHEN browser = 'TikTok' THEN 'tiktok'
    WHEN referrer_host = '' THEN 'direto'
    WHEN referrer_host LIKE 'google.%' OR referrer_host LIKE '%.google.%' THEN 'google'
    WHEN referrer_host ~ '(^|\.)instagram\.com$' THEN 'instagram'
    WHEN referrer_host ~ '(^|\.)(facebook\.com|fb\.com|fb\.me)$' THEN 'facebook'
    WHEN referrer_host ~ '(^|\.)(whatsapp\.com|wa\.me)$' THEN 'whatsapp'
    WHEN referrer_host ~ '(^|\.)tiktok\.com$' THEN 'tiktok'
    WHEN referrer_host ~ '(^|\.)(youtube\.com|youtu\.be)$' THEN 'youtube'
    WHEN referrer_host ~ '(^|\.)(bing\.com|duckduckgo\.com|yahoo\.com|ecosia\.org|search\.brave\.com)$' THEN 'busca'
    ELSE referrer_host
END
WHERE name = 'pageview';
