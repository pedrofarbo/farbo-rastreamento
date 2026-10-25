/**
 * Sem clientes ativos ainda para depor: os depoimentos ficam fora da landing
 * (o componente TestimonialsSection continua no código). Com clientes de
 * verdade, false os traz de volta.
 */
export const PRE_LAUNCH = true;

/**
 * A contratação pelo site (/contratar): a pessoa física escolhe o plano e o
 * rastreador (comprado ou em comodato) e paga com Pix na hora. Nenhum preço
 * fica no código: as páginas mostram os da oferta do servidor (as opções do
 * pedido, na central), os mesmos cobrados. Fechada na central, os botões
 * abrem o pré-cadastro.
 */
export const CHECKOUT_PATH = '/contratar';
/** A seção da landing que chama para a contratação. */
export const CHECKOUT_ANCHOR = '#contratar';

/** O link do funil, com o plano do Insanos MC já marcado e o evento do QR Code. */
export function checkoutPath({ insanos = false, event = '' }: { insanos?: boolean; event?: string } = {}): string {
  const params = new URLSearchParams();
  if (insanos) params.set('plano', 'insanos');
  if (event) params.set('evento', event);
  const query = params.toString();
  return query ? `${CHECKOUT_PATH}?${query}` : CHECKOUT_PATH;
}

/** "R$ 120" ou "R$ 34,90": o preço como as páginas públicas escrevem. */
export function priceLabel(cents: number): string {
  const parts = priceParts(cents);
  return `R$ ${parts.reais}${parts.cents === ',00' ? '' : parts.cents}`;
}

/** "10x de R$ 12,00": a parcela do rastreador em n vezes (o teto vem da oferta). */
export function installmentsOf(cents: number, n: number): string {
  const parcel = Math.floor(cents / n);
  return `${n}x de R$ ${Math.floor(parcel / 100)},${String(parcel % 100).padStart(2, '0')}`;
}

/**
 * "10x de R$ 12,00 sem juros no Pix, direto na mensalidade": o rastreador
 * parcelado como as páginas públicas dizem (as parcelas vêm somadas às
 * mensalidades, uma por mês). Vazio se ele é só à vista.
 */
export function installmentsPhrase(cents: number, n: number): string {
  return n > 1 ? `${installmentsOf(cents, n)} sem juros no Pix, direto na mensalidade` : '';
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

/**
 * Para quem é o QR Code do evento: o do Insanos MC destaca o preço deles; o
 * do público geral, o do plano normal (sem falar do Insanos).
 */
export type EventAudience = 'insanos' | 'geral';

/** O link da tela de cadastro do evento (o que o QR Code abre). O do Insanos
 *  MC é o link sem nada a mais — o dos QR Codes já impressos. */
export function eventUrl(name: string, audience: EventAudience = 'insanos'): string {
  const slug = eventSlug(name);
  return `${SITE_URL}/evento${slug ? `/${slug}` : ''}${audience === 'geral' ? '?publico=geral' : ''}`;
}

/** Para o preço grande: 2990 → { reais: '29', cents: ',90' }. */
export function priceParts(cents: number): { reais: string; cents: string } {
  return { reais: String(Math.floor(cents / 100)), cents: `,${String(cents % 100).padStart(2, '0')}` };
}

/** Homologação do rastreador J16 na Anatel (vai no selo do cartão do equipamento). */
export const ANATEL_HOMOLOGATION = '04895-25-16219';
/** A consulta pública de produtos homologados da Anatel (sem login): confere o número acima. */
export const ANATEL_LOOKUP_URL = 'https://informacoes.anatel.gov.br/paineis/certificacao-de-produtos/consulta-de-produtos';
