import { useCallback, useEffect, useMemo, useState } from 'react';
import { useParams } from 'react-router-dom';

import { ApiError } from '@/api/client';
import { accountPayApi } from '@/api/resources';
import { InvoiceLines } from '@/components/billing/InvoiceLines';
import { PixCheckout, usePixCheckout } from '@/components/billing/PixCheckout';
import { formatDateOnly, formatMoney } from '@/services/format';
import type { PublicAccountPay } from '@/types';

import base from './PreLaunch.module.css';
import styles from './PublicPay.module.css';

/** O valor sem o espaço que não quebra ("R$ 69,90"). */
const money = (cents: number) => formatMoney(cents).replace(/ /g, ' ');

/** "vence em 10/12" ou "vencida há 3 dias". */
export function dueLabel(item: PublicAccountPay['items'][number]): string {
  if (item.overdue) return `vencida há ${item.daysOverdue} ${item.daysOverdue === 1 ? 'dia' : 'dias'}`;
  return `vence em ${formatDateOnly(item.dueDate)}`;
}

/**
 * O link de todas as faturas em aberto do cliente (/pagar/conta/<link secreto>), que a central manda: abre sem
 * login, com o primeiro nome, cada fatura em aberto (de qualquer vencimento) e o total, e paga todas num Pix ou
 * num boleto só. O link continua valendo: mostra o que estiver em aberto quando é aberto; tudo pago, avisa.
 */
export function PublicAccountPayPage() {
  const { token = '' } = useParams();
  const [view, setView] = useState<PublicAccountPay | null>(null);
  const [missing, setMissing] = useState(false);
  const [error, setError] = useState('');
  const [justPaid, setJustPaid] = useState(false);

  useEffect(() => {
    document.title = 'Pagar faturas · Farbo Rastreadores';
    const metas = [
      Object.assign(document.createElement('meta'), { name: 'robots', content: 'noindex, nofollow' }),
      Object.assign(document.createElement('meta'), { name: 'referrer', content: 'no-referrer' }),
    ];
    metas.forEach((m) => document.head.appendChild(m));
    return () => metas.forEach((m) => m.remove());
  }, []);

  const load = useCallback(() => {
    accountPayApi
      .view(token)
      .then((v) => {
        setView(v);
        setError('');
      })
      .catch((err) => {
        if (err instanceof ApiError && err.status === 404) setMissing(true);
        else setError('Não deu para carregar agora. Tente de novo em instantes.');
      });
  }, [token]);
  useEffect(load, [load]);

  const api = useMemo(() => accountPayApi.pix(token), [token]);
  const open = Boolean(view && view.items.length > 0 && view.onlinePayment);
  const onPaid = useCallback(() => {
    setJustPaid(true);
    load();
  }, [load]);
  // A chave da cobrança: as faturas da tela (outra lista, outra cobrança).
  const key = open && view ? `conta:${view.items.length}:${view.amountCents}` : null;
  const checkout = usePixCheckout(key, api, onPaid, { amountCents: view?.amountCents ?? 0 });
  const count = view?.items.length ?? 0;

  return (
    <main className={`${base.page} ${styles.page}`} data-theme="dark">
      <header className={base.header}>
        <img src="/assets/logo-header.png" width={956} height={176} alt="Farbo Rastreadores" className={base.logo} />
        <span className={base.badge}>Faturas</span>
      </header>

      {missing ? (
        <section className={styles.card}>
          <h1 className={styles.title}>Link de pagamento não encontrado</h1>
          <p className={styles.text}>Confira se o endereço está completo ou pague pelo app, em Faturas.</p>
        </section>
      ) : !view ? (
        <p className={base.hint} role="status">
          {error || 'Carregando as faturas…'}
        </p>
      ) : count === 0 ? (
        <section className={`${styles.card} ${styles.paid}`} role="status">
          <span className={styles.check} aria-hidden="true">
            ✓
          </span>
          <h1 className={styles.title}>{justPaid ? 'Pagamento recebido. Obrigado!' : 'Está tudo pago'}</h1>
          <p className={styles.text}>
            {view.firstName ? `${view.firstName}, não` : 'Não'} há nenhuma fatura em aberto. Se o acesso estava suspenso, ele já
            foi liberado.
          </p>
        </section>
      ) : (
        <>
          <section className={styles.card}>
            <p className={styles.hello}>Olá{view.firstName ? `, ${view.firstName}` : ''}!</p>
            <h1 className={styles.title}>
              {count === 1 ? '1 fatura em aberto' : `${count} faturas em aberto`} · {money(view.amountCents)}
            </h1>
            <InvoiceLines
              lines={view.items.map((item, i) => ({
                key: String(i),
                description: `${item.description} · ${dueLabel(item)}`,
                amountCents: item.amountCents,
              }))}
              className={styles.items}
              money={money}
            />
          </section>

          {open && (
            <section className={styles.card}>
              <PixCheckout
                state={checkout}
                description={count > 1 ? `${count} faturas num pagamento só` : undefined}
                paidText="Pagamento recebido. Obrigado!"
              />
            </section>
          )}

          {!view.onlinePayment && (
            <section className={styles.card}>
              <p className={styles.text}>O pagamento destas faturas é combinado com a Farbo. Fale com a gente.</p>
            </section>
          )}

          <p className={styles.muted}>
            Pagamento por Pix, boleto ou cartão processado pela AbacatePay ou pela Pagar.me. A baixa é automática: não precisa mandar
            comprovante.
          </p>
        </>
      )}
    </main>
  );
}
