/**
 * A conta da contratação pelo site: a mensalidade (o plano; no comodato, mais
 * a garantia de troca do rastreador; com o rastreador incluso pela tabela do
 * afiliado, mais a parte dele nas primeiras) e o que se paga agora com Pix —
 * a 1ª mensalidade, a instalação e o frete. O rastreador não tem preço: a
 * Farbo não o vende.
 */
import { includedMonthly } from '@/config/landing';
import { kindPrice } from '@/services/kindPrices';
import { formatMoney } from '@/services/format';
import type { CheckoutOffer, CouponOffer, PeriodOffer, VehicleKind } from '@/types';

export interface CheckoutChoice {
  /** O rastreador incluso (sale) ou em comodato (loan). */
  equipmentMode: 'sale' | 'loan';
  /** O rastreador incluso instalado pelos instaladores da Farbo. */
  farboInstall: boolean;
  /** O semestral (6) ou o anual (12), quando o link os oferece; ausente ou 1, o mensal. */
  periodMonths?: number;
  /** A instalação com o relé do bloqueio do motor, quando o link oferece a escolha. */
  installBlocking?: boolean;
}

export interface CheckoutTotals {
  /** A mensalidade do plano, depois das primeiras (com a garantia de troca, no comodato). */
  monthlyCents: number;
  guaranteeCents: number;
  /** A 1ª mensalidade: com a parte do rastreador incluso, quando ela é somada. */
  firstMonthlyCents: number;
  /** Em quantas das primeiras mensalidades vai a parte do rastreador somada (0: nenhuma). */
  trackerMonths: number;
  /** A instalação: a do comodato ou a dos instaladores da Farbo. */
  installNowCents: number;
  freightCents: number;
  /** O que se paga agora, com Pix. */
  totalNowCents: number;
  /** O semestral (6) ou o anual (12): monthlyCents e firstMonthlyCents são o valor do período. 1: o mensal. */
  periodMonths: number;
  /** O cupom: o desconto em cada uma das primeiras cobranças (já fora de firstMonthlyCents) e em quantas. */
  couponCents: number;
  couponCharges: number;
}

/** O valor da cobrança com o cupom (nunca abaixo de zero): a porcentagem arredondada ao centavo, ou o valor fixo. */
export function couponDiscounted(coupon: CouponOffer, cents: number): number {
  if (coupon.percent) return cents - Math.floor((cents * coupon.percent + 50) / 100);
  if (coupon.amountCents) return Math.max(cents - coupon.amountCents, 0);
  return cents;
}

/** "10% de desconto na 1ª cobrança" ou "R$ 20,00 de desconto nas 3 primeiras cobranças". */
export function couponLabel(coupon: CouponOffer): string {
  const off = coupon.percent ? `${coupon.percent}%` : formatMoney(coupon.amountCents ?? 0);
  return `${off} de desconto ${coupon.charges === 1 ? 'na 1ª cobrança' : `nas ${coupon.charges} primeiras cobranças`}`;
}

/** O semestral ou o anual escolhido, se o link o oferece (com o rastreador incluso ou em comodato). */
export function chosenPeriod(offer: CheckoutOffer, choice: CheckoutChoice) {
  if (!choice.periodMonths || choice.periodMonths <= 1) return null;
  return offer.periods?.find((p) => p.months === choice.periodMonths) ?? null;
}

/** O valor do período: com o rastreador incluso ou, no comodato, com a garantia de troca de cada mês. */
export function periodPrice(period: PeriodOffer, loan: boolean): number {
  return loan ? (period.loanCents ?? period.cents) : period.cents;
}

/** O link oferece a instalação com o relé do bloqueio do motor, ao lado da sem bloqueio. */
export function blockingOffered(offer: CheckoutOffer): boolean {
  return (offer.install?.blockingCents ?? 0) > 0;
}

/**
 * A instalação escolhida (paga à Farbo ou, pelo link do representante, direto a ele): no
 * comodato, a taxa dele; no incluso, a contratada (zero por conta própria). Com o relé do
 * bloqueio, quando escolhido e o link oferece.
 */
export function chosenInstall(offer: CheckoutOffer, choice: CheckoutChoice): { cents: number; blocking: boolean } {
  const loan = choice.equipmentMode === 'loan' && offer.loan !== null;
  if (!loan && (!choice.farboInstall || !offer.install)) return { cents: 0, blocking: false };
  if (choice.installBlocking && blockingOffered(offer)) return { cents: offer.install?.blockingCents ?? 0, blocking: true };
  return { cents: loan ? (offer.loan?.installFeeCents ?? 0) : (offer.install?.priceCents ?? 0), blocking: false };
}

/**
 * Pelo link de um afiliado com tabela, a mensalidade é a do tipo do veículo. Com o cupom (só na contratação
 * direta), o desconto sai do valor do plano nas primeiras cobranças — a 1ª é a de agora.
 */
export function checkoutTotals(
  offer: CheckoutOffer,
  choice: CheckoutChoice,
  freightCents = 0,
  kind?: VehicleKind,
  coupon?: CouponOffer | null,
): CheckoutTotals {
  const loan = choice.equipmentMode === 'loan' && offer.loan !== null;
  // Pelo link de um representante, a instalação é paga direto a ele: fora do Pix.
  const installNowCents = offer.installDirect ? 0 : chosenInstall(offer, choice).cents;
  // O semestral ou o anual: o valor do período, com o rastreador incluso nele
  // (no comodato, com a garantia de troca de cada mês).
  const period = chosenPeriod(offer, choice);
  if (period) {
    const cents = periodPrice(period, loan);
    const couponCents = coupon ? cents - couponDiscounted(coupon, cents) : 0;
    return {
      monthlyCents: cents,
      guaranteeCents: loan ? cents - period.cents : 0,
      firstMonthlyCents: cents - couponCents,
      trackerMonths: 0,
      installNowCents,
      freightCents,
      totalNowCents: cents - couponCents + installNowCents + freightCents,
      periodMonths: period.months,
      couponCents,
      couponCharges: coupon?.charges ?? 0,
    };
  }
  const guaranteeCents = loan ? (offer.loan?.surchargeCents ?? 0) : 0;
  const monthlyCents = (offer.planByKind ? kindPrice(offer.planByKind, kind) : offer.planCents) + guaranteeCents;
  // O comodato não tem a parte do rastreador: ele volta à Farbo.
  const included = includedMonthly(monthlyCents, loan ? undefined : offer.tracker);
  // O cupom abate o valor do plano (a parte do rastreador somada, quando há, fica).
  const couponCents = coupon ? monthlyCents - couponDiscounted(coupon, monthlyCents) : 0;
  return {
    monthlyCents,
    guaranteeCents,
    firstMonthlyCents: included.first - couponCents,
    trackerMonths: included.months,
    installNowCents,
    freightCents,
    totalNowCents: included.first - couponCents + installNowCents + freightCents,
    periodMonths: 1,
    couponCents,
    couponCharges: coupon?.charges ?? 0,
  };
}

/**
 * O Pix do pedido, com a marca de cliente novo (a página de pagamento chama
 * para o app depois do pago): o caminho, quando o link é o da página de
 * pagamento (/pagar/..., que este site abre sem recarregar), ou o link inteiro.
 */
export function payDestination(payUrl: string, origin = window.location.origin): { path: string } | { url: string } {
  const url = new URL(payUrl, origin);
  url.searchParams.set('novo', '1');
  return url.pathname.startsWith('/pagar/') ? { path: url.pathname + url.search } : { url: url.toString() };
}

/** O nome do veículo, se a pessoa não deu um: "Fiat Uno" (ou "Meu carro"/"Minha moto"/"Meu caminhão"). */
export function vehicleName(kind: VehicleKind, brand: string, model: string): string {
  const name = [brand.trim(), model.trim()].filter(Boolean).join(' ');
  return name || (kind === 'MOTORCYCLE' ? 'Minha moto' : kind === 'TRUCK' ? 'Meu caminhão' : 'Meu carro');
}
