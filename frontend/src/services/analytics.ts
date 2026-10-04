/**
 * Visitas da landing page, sem cookies e sem dados pessoais: o navegador
 * manda só o que aconteceu (a visita, a seção vista, o botão clicado); quem
 * vira um código anônimo, que muda todo dia, é o servidor. Nada é guardado
 * no aparelho.
 */

const API_URL = import.meta.env.VITE_API_URL ?? '';
const ENDPOINT = `${API_URL}/api/public/analytics`;

/** Os eventos que o painel conta (o servidor ignora o resto). */
export type AnalyticsEvent =
  | 'section_view'
  | 'cta_click'
  | 'outbound_click'
  | 'lead_open'
  | 'lead_submit'
  | 'waitlist_submit'
  | 'installers_open';

// A visita conta uma vez por carregamento da página (o React, em
// desenvolvimento, monta a página duas vezes).
let pageviewSent = '';

/** A visita: com a origem (de fora) e a campanha (UTM) do endereço. */
export function trackPageview(): void {
  if (pageviewSent === window.location.href) return;
  pageviewSent = window.location.href;
  const params = new URLSearchParams(window.location.search);
  send({
    name: 'pageview',
    referrer: externalReferrer(),
    utmSource: params.get('utm_source') ?? '',
    utmMedium: params.get('utm_medium') ?? '',
    utmCampaign: params.get('utm_campaign') ?? '',
  });
}

/** Um evento da página: a seção vista, o botão clicado, o cadastro feito... */
export function track(name: AnalyticsEvent, label = ''): void {
  send({ name, label });
}

/** De onde a pessoa veio, se de outro site (navegar dentro do site não conta). */
export function externalReferrer(referrer = document.referrer, host = window.location.host): string {
  if (!referrer) return '';
  try {
    return new URL(referrer).host === host ? '' : referrer;
  } catch {
    return '';
  }
}

function send(hit: Record<string, string>): void {
  // Nem nos testes, nem com o navegador comandado por robô.
  if (import.meta.env.MODE === 'test' || typeof navigator === 'undefined' || navigator.webdriver) return;
  const body = JSON.stringify({
    label: '',
    ...hit,
    path: window.location.pathname,
    screenWidth: window.innerWidth,
  });
  try {
    // Sai mesmo se a pessoa fechar a página logo depois (clique num link).
    if (navigator.sendBeacon?.(ENDPOINT, new Blob([body], { type: 'application/json' }))) return;
  } catch {
    /* segue para o fetch */
  }
  void fetch(ENDPOINT, {
    method: 'POST',
    body,
    headers: { 'Content-Type': 'application/json' },
    keepalive: true,
  }).catch(() => undefined);
}

/**
 * O que um clique num elemento marcado com data-analytics registra: sair do
 * site (Instagram, e-mail) ou um botão da página.
 */
export function clickEvent(element: Element, host = window.location.host): { name: AnalyticsEvent; label: string } | null {
  const target = element.closest('[data-analytics]');
  if (!target) return null;
  const label = target.getAttribute('data-analytics') ?? '';
  const href = target.getAttribute('href') ?? '';
  let outbound = href.startsWith('mailto:') || href.startsWith('tel:');
  if (!outbound && /^https?:\/\//.test(href)) {
    try {
      outbound = new URL(href).host !== host;
    } catch {
      outbound = false;
    }
  }
  return { name: outbound ? 'outbound_click' : 'cta_click', label };
}
