/**
 * A conta da contratação pelo site: a mensalidade (o plano; no comodato, mais
 * a garantia de troca do rastreador; com o rastreador incluso pela tabela do
 * afiliado, mais a parte dele nas primeiras) e o que se paga agora com Pix —
 * a 1ª mensalidade, a instalação e o frete. O rastreador não tem preço: a
 * Farbo não o vende.
 */
import { includedMonthly } from '@/config/landing';
import { kindPrice } from '@/services/kindPrices';
import type { CheckoutOffer, VehicleKind } from '@/types';

export interface CheckoutChoice {
  /** O rastreador incluso (sale) ou em comodato (loan). */
  equipmentMode: 'sale' | 'loan';
  /** O rastreador incluso instalado pelos instaladores da Farbo. */
  farboInstall: boolean;
  /** O semestral (6) ou o anual (12), quando o link os oferece; ausente ou 1, o mensal. */
  periodMonths?: number;
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

/** O semestral ou o anual escolhido, se o link o oferece (só com o rastreador incluso). */
export function chosenPeriod(offer: CheckoutOffer, choice: CheckoutChoice) {
  if (choice.equipmentMode === 'loan' || !choice.periodMonths || choice.periodMonths <= 1) return null;
  return offer.periods?.find((p) => p.months === choice.periodMonths) ?? null;
}

/** Pelo link de um afiliado com tabela, a mensalidade é a do tipo do veículo. */
export function checkoutTotals(offer: CheckoutOffer, choice: CheckoutChoice, freightCents = 0, kind?: VehicleKind): CheckoutTotals {
  const loan = choice.equipmentMode === 'loan' && offer.loan !== null;
  const installFarbo = !offer.installDirect && !loan && choice.farboInstall && offer.install ? offer.install.priceCents : 0;
  // O semestral ou o anual: o valor do período, com o rastreador incluso nele.
  const period = chosenPeriod(offer, choice);
  if (period) {
    return {
      monthlyCents: period.cents,
      guaranteeCents: 0,
      firstMonthlyCents: period.cents,
      trackerMonths: 0,
      installNowCents: installFarbo,
      freightCents,
      totalNowCents: period.cents + installFarbo + freightCents,
      periodMonths: period.months,
    };
  }
  const guaranteeCents = loan ? (offer.loan?.surchargeCents ?? 0) : 0;
  const monthlyCents = (offer.planByKind ? kindPrice(offer.planByKind, kind) : offer.planCents) + guaranteeCents;
  // O comodato não tem a parte do rastreador: ele volta à Farbo.
  const included = includedMonthly(monthlyCents, loan ? undefined : offer.tracker);
  // Pelo link de um representante, a instalação é paga direto a ele: fora do Pix.
  const installNowCents = offer.installDirect
    ? 0
    : loan
      ? (offer.loan?.installFeeCents ?? 0)
      : choice.farboInstall && offer.install
        ? offer.install.priceCents
        : 0;
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
