import { useCallback, useEffect, useMemo, useState } from 'react';
import { useParams } from 'react-router-dom';

import { ApiError } from '@/api/client';
import { payLinkApi } from '@/api/resources';
import { PixCheckout, usePixCheckout } from '@/components/billing/PixCheckout';
import { Button } from '@/components/ui/Button';
import { formatDateOnly, formatDateTime, formatMoney } from '@/services/format';
import type { PublicInvoice } from '@/types';

import base from './PreLaunch.module.css';
import styles from './PublicPay.module.css';

/** O valor sem o espaço que não quebra ("R$ 69,90"). */
const money = (cents: number) => formatMoney(cents).replace(/\u00a0/g, ' ');

/**
 * O link de pagamento da fatura (/pagar/<link secreto>), que vai nos
 * lembretes: abre o Pix daquela fatura sem login — o primeiro nome, o que é,
 * o valor e o vencimento, nada além. Quando o pagamento cai, mostra "pago".
 */
export function PublicPayPage() {
  const { token = '' } = useParams();
  const [invoice, setInvoice] = useState<PublicInvoice | null>(null);
  const [missing, setMissing] = useState(false);
  const [error, setError] = useState('');
  const [justPaid, setJustPaid] = useState(false);

  useEffect(() => {
    document.title = 'Pagar fatura · Farbo Rastreadores';
    const metas = [
      Object.assign(document.createElement('meta'), { name: 'robots', content: 'noindex, nofollow' }),
      Object.assign(document.createElement('meta'), { name: 'referrer', content: 'no-referrer' }),
    ];
    metas.forEach((m) => document.head.appendChild(m));
    return () => metas.forEach((m) => m.remove());
  }, []);

  const load = useCallback(() => {
    payLinkApi
      .invoice(token)
      .then((inv) => {
        setInvoice(inv);
        setError('');
      })
      .catch((err) => {
        if (err instanceof ApiError && err.status === 404) setMissing(true);
        else setError('Não deu para carregar agora. Tente de novo em instantes.');
      });
  }, [token]);
  useEffect(load, [load]);

  const api = useMemo(() => payLinkApi.pix(token), [token]);
  const open = invoice?.status === 'OPEN' && invoice.onlinePayment;
  const onPaid = useCallback(() => {
    setJustPaid(true);
    load();
  }, [load]);
  const checkout = usePixCheckout(open ? token : null, api, onPaid);

  const copyManual = async () => {
    try {
      await navigator.clipboard.writeText(invoice?.pixCode ?? '');
    } catch {
      /* o código está na tela para copiar à mão */
    }
  };

  return (
    <main className={`${base.page} ${styles.page}`} data-theme="dark">
      <header className={base.header}>
        <img src="/assets/logo-header.png" width={956} height={176} alt="Farbo Rastreadores" className={base.logo} />
        <span className={base.badge}>Fatura</span>
      </header>

      {missing ? (
        <section className={styles.card}>
          <h1 className={styles.title}>Link de pagamento não encontrado</h1>
          <p className={styles.text}>Confira se o endereço está completo ou pague pelo app, em Faturas.</p>
        </section>
      ) : !invoice ? (
        <p className={base.hint} role="status">
          {error || 'Carregando a fatura…'}
        </p>
      ) : (
        <>
          <section className={styles.card}>
            <p className={styles.hello}>Olá{invoice.firstName ? `, ${invoice.firstName}` : ''}!</p>
            <h1 className={styles.title}>{invoice.description}</h1>
            <div className={styles.facts}>
              <span className={styles.amount}>{money(invoice.amountCents)}</span>
              <span className={invoice.overdue && invoice.status === 'OPEN' ? styles.late : styles.due}>
                {invoice.status === 'OPEN'
                  ? invoice.overdue
                    ? `vencida há ${invoice.daysOverdue} ${invoice.daysOverdue === 1 ? 'dia' : 'dias'}`
                    : `vence em ${formatDateOnly(invoice.dueDate)}`
                  : `vencimento ${formatDateOnly(invoice.dueDate)}`}
              </span>
            </div>
          </section>

          {invoice.status === 'PAID' && (
            <section className={`${styles.card} ${styles.paid}`} role="status">
              <span className={styles.check} aria-hidden="true">
                ✓
              </span>
              <h2 className={styles.title}>{justPaid ? 'Pagamento recebido. Obrigado!' : 'Esta fatura já está paga'}</h2>
              <p className={styles.text}>
                {invoice.paidAt ? `Paga em ${formatDateTime(invoice.paidAt)}. ` : ''}Se o acesso estava suspenso, ele já foi
                liberado.
              </p>
            </section>
          )}

          {invoice.status === 'CANCELED' && (
            <section className={styles.card}>
              <h2 className={styles.title}>Esta fatura foi cancelada</h2>
              <p className={styles.text}>Não há nada a pagar por ela.</p>
            </section>
          )}

          {open && (
            <section className={styles.card}>
              <PixCheckout state={checkout} paidText="Pagamento recebido. Obrigado!" />
            </section>
          )}

          {invoice.status === 'OPEN' && !invoice.onlinePayment && (
            <section className={styles.card}>
              {invoice.paymentUrl || invoice.pixCode ? (
                <>
                  {invoice.paymentUrl && (
                    <a className={styles.button} href={invoice.paymentUrl} target="_blank" rel="noreferrer noopener">
                      Pagar a fatura
                    </a>
                  )}
                  {invoice.pixCode && (
                    <>
                      <code className={styles.code}>{invoice.pixCode}</code>
                      <Button variant="primary" onClick={() => void copyManual()}>
                        Copiar o Pix
                      </Button>
                    </>
                  )}
                </>
              ) : (
                <p className={styles.text}>O pagamento desta fatura é combinado com a Farbo. Fale com a gente.</p>
              )}
            </section>
          )}

          <p className={styles.muted}>
            Pagamento por Pix processado pela AbacatePay. A baixa é automática: não precisa mandar comprovante.
          </p>
        </>
      )}
    </main>
  );
}
