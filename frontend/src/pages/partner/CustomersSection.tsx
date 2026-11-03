import { useEffect, useState } from 'react';

import { publicApi } from '@/api/resources';
import { monthLabel } from '@/services/referral';
import type { CustomerPaymentStatus, PartnerCustomer } from '@/types';

import styles from '../Partner.module.css';
import base from '../PreLaunch.module.css';

const PAYMENT: Record<CustomerPaymentStatus, { label: string; className: string }> = {
  paid: { label: 'pago', className: styles.paid },
  open: { label: 'em aberto', className: styles.pending },
  overdue: { label: 'vencido', className: styles.overdue },
  none: { label: 'sem fatura', className: styles.none },
};

/** "2026-11" → "nov/26": o mês curto, nos selos. */
function shortMonth(month: string): string {
  return monthLabel(month).replace(/\/\d{2}(\d{2})$/, '/$1');
}

/**
 * Os clientes que o afiliado indicou, na página dele (só com a senha): o
 * nome, os veículos com a placa e o número do rastreador, e o pagamento dos
 * últimos meses — pago, em aberto, vencido ou sem fatura.
 */
export function CustomersSection({ token }: { token: string }) {
  const [customers, setCustomers] = useState<PartnerCustomer[] | null>(null);
  const [error, setError] = useState('');

  useEffect(() => {
    let alive = true;
    publicApi
      .partnerCustomers(token)
      .then((list) => alive && setCustomers(list))
      .catch((err) => alive && setError(err instanceof Error ? err.message : 'Não deu para carregar os clientes agora.'));
    return () => {
      alive = false;
    };
  }, [token]);

  return (
    <section className={styles.qrCard} aria-label="Seus clientes">
      <h2 className={styles.heading}>Seus clientes</h2>
      <p className={styles.qrHint}>O pagamento de cada um nos últimos meses, com os veículos e o rastreador de cada.</p>
      {error && (
        <p className={base.error} role="alert">
          {error}
        </p>
      )}
      {!customers ? (
        !error && (
          <p className={styles.qrHint} role="status">
            Carregando…
          </p>
        )
      ) : customers.length === 0 ? (
        <p className={styles.qrHint}>Nenhum cliente ainda. Quem contratar pelo seu link aparece aqui depois de pagar.</p>
      ) : (
        <ul className={styles.installList}>
          {customers.map((c, i) => (
            <li key={`${c.name}-${i}`} className={styles.installItem}>
              <strong>{c.name}</strong>
              <span className={styles.muted}>cliente desde {monthLabel(c.since.slice(0, 7))}</span>
              {c.vehicles.map((v, j) => (
                <span key={j}>
                  {v.name}
                  {v.plate && ` · ${v.plate}`}
                  <span className={styles.muted}> · {v.imei ? `rastreador ${v.imei}` : 'aguardando o rastreador'}</span>
                </span>
              ))}
              <div className={styles.paymentMonths} aria-label="Pagamento mês a mês">
                {c.months.map((m) => (
                  <span key={m.month} className={`${styles.status} ${PAYMENT[m.status].className}`}>
                    {shortMonth(m.month)} · {PAYMENT[m.status].label}
                  </span>
                ))}
              </div>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
