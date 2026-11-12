import { adjustmentNoticeVisible } from '@/services/adjustment';
import { formatDateOnly, formatMoney } from '@/services/format';
import type { PromoOffer, Subscription } from '@/types';

import styles from './Billing.module.css';

type Priced = Pick<Subscription, 'priceCents' | 'promoPriceCents' | 'promoUntil' | 'nextDueDate'> &
  Partial<
    Pick<
      Subscription,
      'nextPriceCents' | 'nextPriceFrom' | 'trackerCents' | 'trackerUntil' | 'trackerAdded' | 'periodMonths' | 'endsOn' | 'couponCode'
    >
  >;

/** "/mês", "/semestre" ou "/ano": de quanto em quanto tempo a assinatura é cobrada. */
export function periodUnit(months?: number): string {
  if (months === 12) return '/ano';
  if (months === 6) return '/semestre';
  return '/mês';
}

/** "Mensal", "Semestral" ou "Anual". */
export function periodLabel(months?: number): string {
  if (months === 12) return 'Anual';
  if (months === 6) return 'Semestral';
  return 'Mensal';
}

/**
 * A parte do rastreador incluso somada à próxima mensalidade (zero: já no
 * preço, fora das primeiras ou sem rastreador incluso).
 */
export function trackerPart(sub: Priced): number {
  return sub.trackerAdded && sub.trackerCents && sub.trackerUntil && sub.nextDueDate < sub.trackerUntil ? sub.trackerCents : 0;
}

/** A próxima fatura ainda sai pelo preço da promoção de pré-lançamento? */
export function promoActive(sub: Priced): boolean {
  return sub.promoPriceCents !== null && sub.promoUntil !== null && sub.nextDueDate < sub.promoUntil;
}

/** De onde vem o preço promocional: o cupom da contratação pelo site ou a promoção de pré-lançamento. */
export function promoName(sub: Priced): string {
  return sub.couponCode ? `cupom ${sub.couponCode}` : 'pré-lançamento';
}

/** O cupom para sempre: o desconto já está no valor da assinatura (sem o preço promocional). */
export function foreverCoupon(sub: Priced): boolean {
  return !!sub.couponCode && sub.promoPriceCents === null;
}

/** Assinatura grátis (cortesia): mensalidade zero, sem promoção, sem cobrança. */
export function isFree(sub: Priced): boolean {
  return sub.priceCents === 0 && !promoActive(sub) && !sub.nextPriceCents;
}

/** A mensalidade da promoção de pré-lançamento no plano: o do Insanos MC tem a sua. */
export function promoMonthlyFor(offer: PromoOffer, planName: string): number {
  const insanos = offer.insanosPlanName.trim().toLowerCase();
  return insanos && planName.trim().toLowerCase() === insanos ? offer.insanosMonthlyCents : offer.monthlyCents;
}

/**
 * A mensalidade como o cliente paga agora: com a promoção de pré-lançamento,
 * o valor dela e quando passa ao do plano; com a parte do rastreador incluso
 * somada nas primeiras, o valor delas e quando volta ao do plano; a de valor
 * zero, "Grátis". Sai num elemento só, para a linha da tabela no celular não
 * separar o valor do "/mês".
 */
export function MonthlyPrice({ sub, staff = false }: { sub: Priced; staff?: boolean }) {
  // O reajuste anual já avisado: o valor novo e desde quando. A equipe vê
  // sempre; o cliente, só nos 30 dias antes de valer.
  const adjustment =
    sub.nextPriceCents && sub.nextPriceFrom && (staff || adjustmentNoticeVisible(sub)) ? (
      <span className={styles.muted}>
        {' '}
        (reajuste anual pelo IPCA: {formatMoney(sub.nextPriceCents)} a partir de {formatDateOnly(sub.nextPriceFrom)})
      </span>
    ) : null;
  if (isFree(sub)) {
    return <span className={styles.amount}>Grátis</span>;
  }
  // O semestral e o anual: o valor do período (o rastreador já está nele);
  // cancelado com o período pago, até quando fica ativo.
  if ((sub.periodMonths ?? 1) > 1) {
    // Com o cupom ainda valendo: o próximo período sai com o desconto.
    const price = promoActive(sub) ? (sub.promoPriceCents as number) : sub.priceCents;
    return (
      <span>
        <span className={styles.amount}>{formatMoney(price)}</span>
        {periodUnit(sub.periodMonths)}{' '}
        <span className={styles.muted}>
          ({formatMoney(Math.floor(price / (sub.periodMonths as number)))} por mês
          {promoActive(sub) ? `; ${promoName(sub)}: a partir de ${formatDateOnly(sub.promoUntil)}, ${formatMoney(sub.priceCents)}` : ''}
          {foreverCoupon(sub) ? `; com o cupom ${sub.couponCode}, para sempre` : ''}
          {sub.endsOn ? `; encerra em ${formatDateOnly(sub.endsOn)}, no fim do período pago` : ''})
        </span>
        {adjustment}
      </span>
    );
  }
  const tracker = trackerPart(sub);
  if (tracker > 0) {
    const base = promoActive(sub) ? (sub.promoPriceCents as number) : sub.priceCents;
    return (
      <span>
        <span className={styles.amount}>{formatMoney(base + tracker)}</span>/mês{' '}
        <span className={styles.muted}>
          (com o rastreador incluso; a partir de {formatDateOnly(sub.trackerUntil as string)}, {formatMoney(base)})
        </span>
        {adjustment}
      </span>
    );
  }
  if (!promoActive(sub)) {
    return (
      <span>
        <span className={styles.amount}>{formatMoney(sub.priceCents)}</span>/mês
        {foreverCoupon(sub) && <span className={styles.muted}> (com o cupom {sub.couponCode}, para sempre)</span>}
        {adjustment}
      </span>
    );
  }
  return (
    <span>
      <span className={styles.amount}>{formatMoney(sub.promoPriceCents as number)}</span>/mês{' '}
      <span className={styles.muted}>
        ({promoName(sub)}; a partir de {formatDateOnly(sub.promoUntil)}, {formatMoney(sub.priceCents)})
      </span>
    </span>
  );
}
