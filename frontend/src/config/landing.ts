/**
 * Pré-lançamento: enquanto não há clientes ativos, no lugar dos depoimentos a
 * landing mostra a lista de quem quer ser avisado do lançamento. Com clientes
 * de verdade para depor, false traz os depoimentos de volta (o componente
 * TestimonialsSection continua no código para isso).
 */
export const PRE_LAUNCH = true;

/** A seção que fica no lugar dos depoimentos: âncora e nome no menu. */
export const SOCIAL_SECTION = PRE_LAUNCH
  ? { id: 'lancamento', label: 'Lançamento' }
  : { id: 'depoimentos', label: 'Depoimentos' };

/**
 * A promoção de pré-lançamento como a landing mostra. Os valores cobrados
 * vêm do servidor (LAUNCH_PROMO_*): mude os dois juntos.
 */
export const LAUNCH_OFFER = {
  equipment: 'R$ 120',
  equipmentRegular: 'R$ 150',
  monthly: 'R$ 34,90',
  monthlyRegular: 'R$ 69,90',
  /** Integrantes do Insanos MC: a mensalidade deles na promoção (depois, INSANOS_MONTHLY). */
  insanosMonthly: 'R$ 27,90',
  /** Os mesmos valores em centavos, para o preço grande dos planos. */
  equipmentCents: 12000,
  equipmentRegularCents: 15000,
  monthlyCents: 3490,
  monthlyRegularCents: 6990,
  insanosMonthlyCents: 2790,
  months: 12,
  slots: 500,
};

/** A âncora da seção de pré-lançamento (lista de lançamento). */
export const LAUNCH_ANCHOR = '#lancamento';

/** Para o preço grande: 2990 → { reais: '29', cents: ',90' }. */
export function priceParts(cents: number): { reais: string; cents: string } {
  return { reais: String(Math.floor(cents / 100)), cents: `,${String(cents % 100).padStart(2, '0')}` };
}

/** Homologação do rastreador J16 na Anatel (vai no selo do cartão do equipamento). */
export const ANATEL_HOMOLOGATION = '04895-25-16219';

/** Preço especial dos integrantes do Insanos MC (depois da promoção, é o deles). */
export const INSANOS_MONTHLY = { label: 'R$ 39,90', cents: 3990 };
