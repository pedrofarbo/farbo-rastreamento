/**
 * A conta da contratação pelo site: a mensalidade (o plano; no comodato, mais
 * a garantia de troca do rastreador; com o rastreador incluso pela tabela do
 * afiliado, mais a parte dele nas primeiras) e o que se paga agora com Pix —
 * a 1ª mensalidade, a instalação e o frete. O rastreador não tem preço: a
 * Farbo não o vende.
 */
import { includedMonthly } from '@/config/landing';
import { kindPrice } from '@/services/kindPrices';
import type { CheckoutOffer, PeriodOffer, VehicleKind } from '@/types';

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

/** Pelo link de um afiliado com tabela, a mensalidade é a do tipo do veículo. */
export function checkoutTotals(offer: CheckoutOffer, choice: CheckoutChoice, freightCents = 0, kind?: VehicleKind): CheckoutTotals {
  const loan = choice.equipmentMode === 'loan' && offer.loan !== null;
  // Pelo link de um representante, a instalação é paga direto a ele: fora do Pix.
  const installNowCents = offer.installDirect ? 0 : chosenInstall(offer, choice).cents;
  // O semestral ou o anual: o valor do período, com o rastreador incluso nele
  // (no comodato, com a garantia de troca de cada mês).
  const period = chosenPeriod(offer, choice);
  if (period) {
    const cents = periodPrice(period, loan);
    return {
      monthlyCents: cents,
      guaranteeCents: loan ? cents - period.cents : 0,
      firstMonthlyCents: cents,
      trackerMonths: 0,
      installNowCents,
      freightCents,
      totalNowCents: cents + installNowCents + freightCents,
      periodMonths: period.months,
    };
  }
  const guaranteeCents = loan ? (offer.loan?.surchargeCents ?? 0) : 0;
  const monthlyCents = (offer.planByKind ? kindPrice(offer.planByKind, kind) : offer.planCents) + guaranteeCents;
  // O comodato não tem a parte do rastreador: ele volta à Farbo.
  const included = includedMonthly(monthlyCents, loan ? undefined : offer.tracker);
  return {
    monthlyCents,
    guaranteeCents,
    firstMonthlyCents: included.first,
    trackerMonths: included.months,
    installNowCents,
    freightCents,
    totalNowCents: included.first + installNowCents + freightCents,
    periodMonths: 1,
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
