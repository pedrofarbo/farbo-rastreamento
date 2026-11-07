import type { CheckoutOffer, LoanOffer, Tracker } from '@/types';

/**
 * Sem clientes ativos ainda para depor: os depoimentos ficam fora da landing
 * (o componente TestimonialsSection continua no código). Com clientes de
 * verdade, false os traz de volta.
 */
export const PRE_LAUNCH = true;

/**
 * A contratação pelo site (/contratar): a pessoa física escolhe o rastreador
 * (incluso na mensalidade ou em comodato) e paga com Pix na hora. A Farbo não
 * vende o rastreador: as páginas nunca falam em comprá-lo. Nenhum preço fica
 * no código: as páginas mostram os da oferta do servidor (as opções do
 * pedido, na central), os mesmos cobrados. Fechada na central, os botões
 * abrem o pré-cadastro.
 */
export const CHECKOUT_PATH = '/contratar';

/**
 * Pelo link de um afiliado só de comodato: o rastreador não fica com a
 * pessoa, e as páginas mostram o comodato no lugar do incluso (com a
 * contratação aberta).
 */
export function loanOnly(offer: CheckoutOffer | undefined, open = true): offer is CheckoutOffer & { loan: LoanOffer } {
  return open && offer?.equipmentModes === 'loan' && !!offer.loan;
}
/** A seção da landing que chama para a contratação. */
export const CHECKOUT_ANCHOR = '#contratar';

/** O link do funil, com o evento do QR Code. */
export function checkoutPath({ event = '' }: { event?: string } = {}): string {
  return event ? `${CHECKOUT_PATH}?${new URLSearchParams({ evento: event })}` : CHECKOUT_PATH;
}

/** "R$ 120" ou "R$ 34,90": o preço como as páginas públicas escrevem. */
export function priceLabel(cents: number): string {
  const parts = priceParts(cents);
  return `R$ ${parts.reais}${parts.cents === ',00' ? '' : parts.cents}`;
}

/**
 * A mensalidade com o rastreador incluso: a das primeiras `months` (com a
 * parte do rastreador, quando ela é somada ao plano) e a de depois. No plano
 * do site, a parte já está no preço: as duas são iguais.
 */
export function includedMonthly(monthlyCents: number, tracker: Tracker | undefined): {
  first: number;
  after: number;
  months: number;
} {
  const added = tracker && tracker.added && tracker.cents > 0 && tracker.months > 0;
  return { first: monthlyCents + (added ? tracker.cents : 0), after: monthlyCents, months: added ? tracker.months : 0 };
}

/**
 * O rastreador como as páginas públicas dizem: "rastreador incluso" (no
 * plano do site, a parte dele já está no preço) ou, pela tabela do afiliado,
 * "rastreador incluso (+ R$ 12/mês nas 10 primeiras mensalidades)".
 */
export function includedLabel(offer: CheckoutOffer | undefined, open = true): string {
  const added = trackerAdded(offer, open);
  return added
    ? `rastreador incluso (+ ${priceLabel(added.cents)}/mês nas ${added.months} primeiras mensalidades)`
    : 'rastreador incluso';
}

/**
 * A parte do rastreador incluso somada à mensalidade (pela tabela do
 * afiliado), quando há; nula no plano do site, em que ela já está no preço.
 */
export function trackerAdded(offer: CheckoutOffer | undefined, open = true): { cents: number; months: number } | null {
  const t = offer?.tracker;
  return open && offer?.planByKind && t && t.added && t.cents > 0 && t.months > 0 ? t : null;
}

/**
 * "R$ 61,90/mês nas 10 primeiras mensalidades, depois R$ 49,90/mês": a
 * mensalidade com a parte do rastreador somada (vazio quando ela já está no
 * preço).
 */
export function includedPhrase(monthlyCents: number, tracker: Tracker | undefined): string {
  const m = includedMonthly(monthlyCents, tracker);
  return m.months > 0 ? `${priceLabel(m.first)}/mês nas ${m.months} primeiras mensalidades, depois ${priceLabel(m.after)}/mês` : '';
}

/** O endereço público do site: o QR Code dos eventos aponta para ele. */
export const SITE_URL = 'https://farborastreadores.com.br';

/**
 * O nome do evento como vai no link (o servidor faz igual): minúsculo, sem
 * acento, palavras com hífen. "Encontro Insanos MC — Out/26" →
 * "encontro-insanos-mc-out-26".
 */
export function eventSlug(name: string): string {
  return name
    .normalize('NFD')
    .replace(/[\u0300-\u036f]/g, '')
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+/, '')
    .slice(0, 60)
    .replace(/-+$/, '');
}

/** O link da tela de cadastro do evento (o que o QR Code abre). */
export function eventUrl(name: string): string {
  const slug = eventSlug(name);
  return `${SITE_URL}/evento${slug ? `/${slug}` : ''}`;
}

/** Para o preço grande: 2990 → { reais: '29', cents: ',90' }. */
export function priceParts(cents: number): { reais: string; cents: string } {
  return { reais: String(Math.floor(cents / 100)), cents: `,${String(cents % 100).padStart(2, '0')}` };
}

/** Homologação do rastreador J16 na Anatel (vai no selo do cartão do equipamento). */
export const ANATEL_HOMOLOGATION = '04895-25-16219';
/** A consulta pública de produtos homologados da Anatel (sem login): confere o número acima. */
export const ANATEL_LOOKUP_URL = 'https://informacoes.anatel.gov.br/paineis/certificacao-de-produtos/consulta-de-produtos';
