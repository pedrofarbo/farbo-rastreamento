import { adjustmentNoticeVisible } from '@/services/adjustment';
import { formatDateOnly, formatMoney } from '@/services/format';
import type { PromoOffer, Subscription } from '@/types';

import styles from './Billing.module.css';

type Priced = Pick<Subscription, 'priceCents' | 'promoPriceCents' | 'promoUntil' | 'nextDueDate'> &
  Partial<Pick<Subscription, 'nextPriceCents' | 'nextPriceFrom'>>;

/** A próxima fatura ainda sai pelo preço da promoção de pré-lançamento? */
export function promoActive(sub: Priced): boolean {
  return sub.promoPriceCents !== null && sub.promoUntil !== null && sub.nextDueDate < sub.promoUntil;
}

/** A mensalidade da promoção de pré-lançamento no plano: o do Insanos MC tem a sua. */
export function promoMonthlyFor(offer: PromoOffer, planName: string): number {
  const insanos = offer.insanosPlanName.trim().toLowerCase();
  return insanos && planName.trim().toLowerCase() === insanos ? offer.insanosMonthlyCents : offer.monthlyCents;
}

/**
 * A mensalidade como o cliente paga agora: com a promoção de pré-lançamento,
 * o valor dela e quando passa ao do plano.
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
  if (!promoActive(sub)) {
    return (
      <>
        <span className={styles.amount}>{formatMoney(sub.priceCents)}</span>/mês{adjustment}
      </>
    );
  }
  return (
    <>
      <span className={styles.amount}>{formatMoney(sub.promoPriceCents as number)}</span>/mês{' '}
      <span className={styles.muted}>
        (pré-lançamento; a partir de {formatDateOnly(sub.promoUntil)}, {formatMoney(sub.priceCents)})
      </span>
    </>
  );
}
