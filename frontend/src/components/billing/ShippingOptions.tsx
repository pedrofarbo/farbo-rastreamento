import { Button } from '@/components/ui/Button';
import { formatMoney } from '@/services/format';
import type { ShippingQuote, ShippingQuoteView } from '@/types';

import styles from './Billing.module.css';

/** O valor da opção "Combinar entrega" (em vez de um serviço do Melhor Envios). */
export const ARRANGE_DELIVERY = -2;

/** "Correios PAC". */
export function quoteName(q: Pick<ShippingQuote, 'company' | 'service'>): string {
  return `${q.company} ${q.service}`.trim();
}

/** "até 7 dias úteis". */
export function deliveryLabel(days: number): string {
  return days > 0 ? `até ${days} ${days === 1 ? 'dia útil' : 'dias úteis'}` : 'prazo a confirmar';
}

/**
 * As formas de entrega do rastreador até o endereço do cliente (Melhor
 * Envios), com preço e prazo; o escolhido é cobrado junto com o
 * equipamento. Perto da base, o cliente pode combinar a entrega (sem frete).
 * A central pode marcar "sem frete" (entrega em mãos).
 */
export function ShippingOptions({
  view,
  loading,
  value,
  onChange,
  onRetry,
  allowNone,
}: {
  view: ShippingQuoteView | undefined;
  loading: boolean;
  /** O serviço escolhido; 0: sem frete; ARRANGE_DELIVERY: combinar a entrega. */
  value: number;
  onChange: (serviceId: number) => void;
  onRetry: () => void;
  /** A central pode não cobrar o frete. */
  allowNone?: boolean;
}) {
  if (loading) {
    return (
      <div className={styles.shipping} role="status">
        <span className={styles.deliveryLabel}>Entrega</span>
        <span className={styles.muted}>Calculando o frete…</span>
      </div>
    );
  }
  if (!view?.enabled) return null;
  return (
    <fieldset className={styles.shipping}>
      <legend className={styles.deliveryLabel}>Entrega{view.zipCode ? ` para o CEP ${view.zipCode.replace(/^(\d{5})(\d{3})$/, '$1-$2')}` : ''}</legend>
      {view.problem && (
        <div className={styles.shippingProblem}>
          <span>Não deu para calcular o frete: {view.problem}.</span>
          <Button size="small" variant="secondary" onClick={onRetry}>
            Tentar de novo
          </Button>
        </div>
      )}
      {view.quotes.map((q) => (
        <label key={q.serviceId} className={`${styles.shippingOption} ${value === q.serviceId ? styles.shippingActive : ''}`}>
          <input type="radio" name="shipping" checked={value === q.serviceId} onChange={() => onChange(q.serviceId)} />
          <span className={styles.shippingName}>
            <strong>{quoteName(q)}</strong>
            <span className={styles.muted}>{deliveryLabel(q.deliveryDays)}</span>
          </span>
          <strong className={styles.shippingPrice}>{formatMoney(q.priceCents)}</strong>
        </label>
      ))}
      {view.arrange && !allowNone && (
        <label className={`${styles.shippingOption} ${value === ARRANGE_DELIVERY ? styles.shippingActive : ''}`}>
          <input type="radio" name="shipping" checked={value === ARRANGE_DELIVERY} onChange={() => onChange(ARRANGE_DELIVERY)} />
          <span className={styles.shippingName}>
            <strong>Combinar entrega</strong>
            <span className={styles.muted}>a gente fala com você para combinar o dia e o local</span>
          </span>
          <strong className={styles.shippingPrice}>sem frete</strong>
        </label>
      )}
      {allowNone && (
        <label className={`${styles.shippingOption} ${value === 0 ? styles.shippingActive : ''}`}>
          <input type="radio" name="shipping" checked={value === 0} onChange={() => onChange(0)} />
          <span className={styles.shippingName}>
            <strong>Sem frete</strong>
            <span className={styles.muted}>entrega em mãos ou retirada na base</span>
          </span>
          <strong className={styles.shippingPrice}>—</strong>
        </label>
      )}
    </fieldset>
  );
}
